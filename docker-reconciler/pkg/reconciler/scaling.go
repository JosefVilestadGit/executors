package reconciler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	log "github.com/sirupsen/logrus"
)

// AdjustReplicas scales the deployment up or down to match the desired replica count
// This is a simplified version used by self-healing that doesn't require a process
func (r *Reconciler) AdjustReplicas(blueprint *core.Blueprint) error {
	// Only works for ExecutorDeployment (DockerDeployment uses instances array)
	if blueprint.Kind != "ExecutorDeployment" {
		return nil // Skip for DockerDeployment
	}

	// Parse the deployment spec
	var spec DeploymentSpec
	specBytes, err := json.Marshal(blueprint.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal spec: %w", err)
	}

	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("failed to unmarshal deployment spec: %w", err)
	}

	// Get current running containers (only count running, not stopped)
	runningContainers, err := r.listRunningContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to list running containers for replica adjustment")
		return err
	}

	currentReplicas := len(runningContainers)
	desiredReplicas := spec.Replicas

	if currentReplicas == desiredReplicas {
		return nil // Already at desired state
	}

	if currentReplicas < desiredReplicas {
		// Scale up
		return r.scaleUp(blueprint, spec, currentReplicas, desiredReplicas)
	}

	// Scale down
	return r.scaleDown(blueprint, currentReplicas, desiredReplicas)
}

// scaleUp starts new containers to reach desired replica count
func (r *Reconciler) scaleUp(blueprint *core.Blueprint, spec DeploymentSpec, current, desired int) error {
	containersToStart := desired - current
	r.addLog(nil, fmt.Sprintf("Self-healing: Scaling up %s from %d to %d replicas (starting %d container(s))",
		blueprint.Metadata.Name, current, desired, containersToStart))

	// Pull image first
	if err := r.pullImage(nil, spec.Image); err != nil {
		r.addErrorLog(nil, fmt.Sprintf("Self-healing: Failed to pull image %s: %v", spec.Image, err))
		return fmt.Errorf("failed to pull image: %w", err)
	}

	for i := 0; i < containersToStart; i++ {
		// Generate unique executor name
		uniqueExecutorName, err := r.generateUniqueExecutorName(blueprint.Metadata.ColonyName, blueprint.Metadata.Name)
		if err != nil {
			r.addErrorLog(nil, fmt.Sprintf("Self-healing: Failed to generate executor name: %v", err))
			return fmt.Errorf("failed to generate unique executor name: %w", err)
		}

		r.addLog(nil, fmt.Sprintf("Self-healing: Starting container %s for %s", uniqueExecutorName, blueprint.Metadata.Name))
		if err := r.startContainer(nil, spec, uniqueExecutorName, blueprint); err != nil {
			r.addErrorLog(nil, fmt.Sprintf("Self-healing: Failed to start container %s: %v", uniqueExecutorName, err))
			return fmt.Errorf("failed to start container %s: %w", uniqueExecutorName, err)
		}

		r.addLog(nil, fmt.Sprintf("Self-healing: Container %s started successfully", uniqueExecutorName))
	}

	r.addLog(nil, fmt.Sprintf("Self-healing: Scale up complete for %s", blueprint.Metadata.Name))
	return nil
}

// scaleDown stops and removes excess containers to reach desired replica count
func (r *Reconciler) scaleDown(blueprint *core.Blueprint, current, desired int) error {
	containersToStop := current - desired
	r.addLog(nil, fmt.Sprintf("Self-healing: Scaling down %s from %d to %d replicas (stopping %d container(s))",
		blueprint.Metadata.Name, current, desired, containersToStop))

	ctx := context.Background()
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)
	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: filterArgs,
	})
	if err != nil {
		r.addErrorLog(nil, fmt.Sprintf("Self-healing: Failed to list containers for scale down: %v", err))
		return fmt.Errorf("failed to list containers for scale down: %w", err)
	}

	for i := 0; i < containersToStop && i < len(containers); i++ {
		cont := containers[i]
		containerID := cont.ID

		// Get container name (remove leading slash if present)
		containerName := ""
		if len(cont.Names) > 0 {
			containerName = cont.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
		}

		// Deregister executor BEFORE stopping container (if it's an ExecutorDeployment)
		if blueprint.Kind == "ExecutorDeployment" && containerName != "" {
			// Get the generation from the container's label, not the blueprint
			// The executor was registered with the generation at the time the container was created
			containerGeneration := cont.Labels["colonies.generation"]
			if containerGeneration == "" {
				// Fallback to blueprint generation if label not found (shouldn't happen)
				containerGeneration = fmt.Sprintf("%d", blueprint.Metadata.Generation)
				r.addWarnLog(nil, fmt.Sprintf("Self-healing: Container %s missing generation label, using blueprint generation", containerName))
			}

			// Executor name includes generation suffix (e.g., "docker-executor-abc-5")
			executorName := fmt.Sprintf("%s-%s", containerName, containerGeneration)
			r.addLog(nil, fmt.Sprintf("Self-healing: Deregistering executor %s before stopping container", executorName))

			if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
				r.addWarnLog(nil, fmt.Sprintf("Self-healing: Failed to deregister executor %s: %v", executorName, err))
				// Continue anyway to stop the container
			} else {
				r.addLog(nil, fmt.Sprintf("Self-healing: Deregistered executor %s", executorName))
			}
		}

		// Stop and remove the container
		r.addLog(nil, fmt.Sprintf("Self-healing: Stopping container %s", containerName))
		if err := r.stopAndRemoveContainer(containerID); err != nil {
			r.addWarnLog(nil, fmt.Sprintf("Self-healing: Failed to stop container %s: %v", containerName, err))
		} else {
			r.addLog(nil, fmt.Sprintf("Self-healing: Stopped and removed container %s", containerName))
		}
	}

	r.addLog(nil, fmt.Sprintf("Self-healing: Scale down complete for %s", blueprint.Metadata.Name))
	return nil
}
