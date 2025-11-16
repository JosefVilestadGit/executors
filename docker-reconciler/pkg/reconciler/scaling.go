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
	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Current":       current,
		"Desired":       desired,
		"ToStart":       containersToStart,
	}).Info("Self-healing: scaling up")

	// Pull image first
	if err := r.pullImage(nil, spec.Image); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	for i := 0; i < containersToStart; i++ {
		// Generate unique executor name
		uniqueExecutorName, err := r.generateUniqueExecutorName(blueprint.Metadata.Namespace, blueprint.Metadata.Name)
		if err != nil {
			return fmt.Errorf("failed to generate unique executor name: %w", err)
		}

		if err := r.startContainer(nil, spec, uniqueExecutorName, blueprint); err != nil {
			return fmt.Errorf("failed to start container %s: %w", uniqueExecutorName, err)
		}

		log.WithFields(log.Fields{
			"ContainerName": uniqueExecutorName,
			"BlueprintName": blueprint.Metadata.Name,
		}).Info("Self-healing: started container")
	}

	return nil
}

// scaleDown stops and removes excess containers to reach desired replica count
func (r *Reconciler) scaleDown(blueprint *core.Blueprint, current, desired int) error {
	containersToStop := current - desired
	log.WithFields(log.Fields{
		"BlueprintName": blueprint.Metadata.Name,
		"Current":       current,
		"Desired":       desired,
		"ToStop":        containersToStop,
	}).Info("Self-healing: scaling down")

	ctx := context.Background()
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)
	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: filterArgs,
	})
	if err != nil {
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
			// Executor name includes generation suffix (e.g., "docker-executor-abc-5")
			executorName := fmt.Sprintf("%s-%d", containerName, blueprint.Metadata.Generation)

			log.WithFields(log.Fields{
				"ExecutorName": executorName,
				"ContainerID":  truncateID(containerID, 12),
			}).Info("Self-healing: deregistering executor before stopping container")

			if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
				log.WithFields(log.Fields{
					"Error":        err,
					"ExecutorName": executorName,
				}).Warn("Failed to deregister executor")
				// Continue anyway to stop the container
			}
		}

		// Stop and remove the container
		if err := r.stopAndRemoveContainer(containerID); err != nil {
			log.WithFields(log.Fields{
				"Error":         err,
				"ContainerID":   truncateID(containerID, 12),
				"ContainerName": containerName,
			}).Warn("Self-healing: failed to stop container")
		} else {
			log.WithFields(log.Fields{
				"ContainerName": containerName,
				"BlueprintName": blueprint.Metadata.Name,
			}).Info("Self-healing: stopped and removed container")
		}
	}

	return nil
}
