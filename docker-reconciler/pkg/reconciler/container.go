package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	log "github.com/sirupsen/logrus"
)

// pullImage pulls a container image from a registry
// It checks if the image exists locally first, and only pulls if necessary
func (r *Reconciler) pullImage(process *core.Process, image string) error {
	// Check if image exists locally first
	ctx := context.Background()
	_, _, err := r.dockerClient.ImageInspectWithRaw(ctx, image)
	if err == nil {
		// Image exists locally, skip pulling
		r.addLog(process, fmt.Sprintf("Image already exists locally: %s", image))
		return nil
	}

	// Image doesn't exist, pull it
	return r.doPullImage(process, image)
}

// forcePullImage always pulls a container image from a registry, even if it exists locally
// This is used during force reconciliation to ensure we get the latest image
func (r *Reconciler) forcePullImage(process *core.Process, image string) error {
	r.addLog(process, fmt.Sprintf("Force pulling image (ignoring local cache): %s", image))
	return r.doPullImage(process, image)
}

// doPullImage performs the actual image pull
func (r *Reconciler) doPullImage(process *core.Process, image string) error {
	r.addLog(process, fmt.Sprintf("Pulling image: %s", image))

	logChan := make(chan docker.LogMessage, 100)
	errChan := make(chan error, 1)

	go func() {
		err := r.dockerHandler.PullImage(image, logChan)
		if err != nil {
			errChan <- err
		}
	}()

	for {
		select {
		case err := <-errChan:
			return err
		case msg := <-logChan:
			if msg.Log != "" {
				r.addLog(process, msg.Log)
			}
			if msg.EOF {
				return nil
			}
		}
	}
}

// waitForContainerRunning waits for a container to reach running state
// Returns error if container doesn't start within timeout
func (r *Reconciler) waitForContainerRunning(containerID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for container %s to start", truncateID(containerID, 12))
		case <-ticker.C:
			inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
			if err != nil {
				return fmt.Errorf("failed to inspect container %s: %w", truncateID(containerID, 12), err)
			}

			if inspect.State.Running {
				return nil
			}

			// Check if container exited with error
			if inspect.State.Status == "exited" || inspect.State.Status == "dead" {
				return fmt.Errorf("container %s failed to start, status: %s, exit code: %d",
					truncateID(containerID, 12), inspect.State.Status, inspect.State.ExitCode)
			}
		}
	}
}

// listContainersByLabel returns a list of container IDs that match a deployment label
// This includes ALL containers (both running and stopped)
func (r *Reconciler) listContainersByLabel(deploymentName string) ([]string, error) {
	ctx := context.Background()

	// List all containers with the deployment label
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+deploymentName)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true, // Include all containers (running and stopped)
		Filters: filterArgs,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	containerIDs := make([]string, len(containers))
	for i, container := range containers {
		containerIDs[i] = container.ID
	}

	return containerIDs, nil
}

// listRunningContainersByLabel returns a list of RUNNING container IDs that match a deployment label
// This excludes stopped/exited containers
func (r *Reconciler) listRunningContainersByLabel(deploymentName string) ([]string, error) {
	ctx := context.Background()

	// List only running containers with the deployment label
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+deploymentName)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: filterArgs,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list running containers: %w", err)
	}

	containerIDs := make([]string, len(containers))
	for i, container := range containers {
		containerIDs[i] = container.ID
	}

	return containerIDs, nil
}

// stopContainer stops a running container
func (r *Reconciler) stopContainer(containerID string) error {
	ctx := context.Background()
	timeout := 10 // seconds
	return r.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})
}

// stopAndRemoveContainer stops and removes a container
func (r *Reconciler) stopAndRemoveContainer(containerID string) error {
	ctx := context.Background()

	// Stop the container first
	timeout := 10
	if err := r.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		log.WithFields(log.Fields{"Error": err, "ContainerID": containerID}).Warn("Failed to stop container before removal")
	}

	// Remove the container
	return r.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// forceRemoveContainer stops, removes a container and deregisters its executor (for ExecutorDeployments)
func (r *Reconciler) forceRemoveContainer(process *core.Process, containerID string, blueprint *core.Blueprint) error {
	ctx := context.Background()

	// Inspect container to get name and generation
	inspect, err := r.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	containerName := inspect.Name
	if len(containerName) > 0 && containerName[0] == '/' {
		containerName = containerName[1:]
	}

	// For ExecutorDeployments, deregister the executor first
	if blueprint.Kind == "ExecutorDeployment" {
		containerGeneration := inspect.Config.Labels["colonies.generation"]
		if containerGeneration != "" {
			executorName := fmt.Sprintf("%s-%s", containerName, containerGeneration)
			r.addLog(process, fmt.Sprintf("Deregistering executor: %s", executorName))

			if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to deregister executor %s: %v", executorName, err))
			} else {
				r.addLog(process, fmt.Sprintf("Deregistered executor: %s", executorName))
			}
		}
	}

	// Stop and remove the container
	r.addLog(process, fmt.Sprintf("Stopping and removing container: %s", containerName))
	if err := r.stopAndRemoveContainer(containerID); err != nil {
		return fmt.Errorf("failed to stop/remove container: %w", err)
	}

	r.addLog(process, fmt.Sprintf("Removed container: %s", containerName))
	return nil
}

// findDirtyContainers checks which containers have an outdated generation
func (r *Reconciler) findDirtyContainers(containerIDs []string, currentGeneration int64) ([]string, error) {
	ctx := context.Background()
	dirtyContainers := []string{}

	for _, containerID := range containerIDs {
		inspect, err := r.dockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			log.WithFields(log.Fields{"Error": err, "ContainerID": containerID}).Warn("Failed to inspect container for generation check")
			continue
		}

		// Check if container has generation label
		generationStr, hasLabel := inspect.Config.Labels["colonies.generation"]
		if !hasLabel {
			// Old container without generation label - mark as dirty
			log.WithFields(log.Fields{"ContainerID": containerID}).Info("Container missing generation label, marking as dirty")
			dirtyContainers = append(dirtyContainers, containerID)
			continue
		}

		// Parse generation from label
		var containerGeneration int64
		if _, err := fmt.Sscanf(generationStr, "%d", &containerGeneration); err != nil {
			log.WithFields(log.Fields{"Error": err, "ContainerID": containerID, "GenerationLabel": generationStr}).Warn("Failed to parse generation label")
			// Mark as dirty if we can't parse
			dirtyContainers = append(dirtyContainers, containerID)
			continue
		}

		// Check if container generation is outdated
		if containerGeneration < currentGeneration {
			log.WithFields(log.Fields{
				"ContainerID":         containerID,
				"ContainerGeneration": containerGeneration,
				"CurrentGeneration":   currentGeneration,
			}).Info("Container has outdated generation, marking as dirty")
			dirtyContainers = append(dirtyContainers, containerID)
		}
	}

	return dirtyContainers, nil
}
