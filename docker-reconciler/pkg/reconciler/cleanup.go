package reconciler

import (
	"context"
	"fmt"
	"strings"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	log "github.com/sirupsen/logrus"
)

// HasOldGenerationContainers checks if any containers have old generation labels
func (r *Reconciler) HasOldGenerationContainers(blueprint *core.Blueprint) (bool, error) {
	ctx := context.Background()
	currentGeneration := blueprint.Metadata.Generation

	// List ALL containers for this deployment
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})

	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, cont := range containers {
		generationStr, hasLabel := cont.Labels["colonies.generation"]
		if !hasLabel {
			// Container without generation label - consider it old
			return true, nil
		}

		var generation int64
		if _, err := fmt.Sscanf(generationStr, "%d", &generation); err != nil {
			// Unparseable generation - consider it old
			return true, nil
		}

		if generation < currentGeneration {
			return true, nil
		}
	}

	return false, nil
}

// CleanupOldGenerationContainers removes containers from old generations as a safety net
// This handles orphaned containers that might have been created during rapid blueprint updates
func (r *Reconciler) CleanupOldGenerationContainers(blueprint *core.Blueprint) error {
	ctx := context.Background()
	currentGeneration := blueprint.Metadata.Generation

	// List ALL containers for this deployment (including stopped)
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true, // Include stopped containers
		Filters: filterArgs,
	})

	if err != nil {
		return fmt.Errorf("failed to list containers for generation cleanup: %w", err)
	}

	removedCount := 0
	for _, cont := range containers {
		// Check generation label
		generationStr, hasLabel := cont.Labels["colonies.generation"]
		if !hasLabel {
			// Old container without generation label - skip for safety
			continue
		}

		// Parse generation
		var generation int64
		if _, err := fmt.Sscanf(generationStr, "%d", &generation); err != nil {
			log.WithFields(log.Fields{
				"ContainerID":   truncateID(cont.ID, 12),
				"GenerationStr": generationStr,
				"Error":         err,
			}).Warn("Failed to parse generation label")
			continue
		}

		// Remove containers from old generations
		if generation < currentGeneration {
			// Get container name for executor deregistration
			containerName := ""
			if len(cont.Names) > 0 {
				containerName = cont.Names[0]
				if len(containerName) > 0 && containerName[0] == '/' {
					containerName = containerName[1:]
				}
			}

			log.WithFields(log.Fields{
				"ContainerID":       truncateID(cont.ID, 12),
				"ContainerName":     containerName,
				"OldGeneration":     generation,
				"CurrentGeneration": currentGeneration,
				"State":             cont.State,
			}).Info("Removing old generation container")

			// Deregister executor if it's an ExecutorDeployment
			if blueprint.Kind == "ExecutorDeployment" && containerName != "" {
				if err := r.client.RemoveExecutor(r.colonyName, containerName, r.colonyOwnerKey); err != nil {
					log.WithFields(log.Fields{
						"Error":        err,
						"ExecutorName": containerName,
					}).Debug("Failed to deregister executor (may already be removed)")
					// Continue anyway - executor might already be deregistered
				}
			}

			// Remove the container
			if err := r.dockerClient.ContainerRemove(ctx, cont.ID, container.RemoveOptions{Force: true}); err != nil {
				log.WithFields(log.Fields{
					"Error":       err,
					"ContainerID": truncateID(cont.ID, 12),
				}).Warn("Failed to remove old generation container")
			} else {
				removedCount++
			}
		}
	}

	if removedCount > 0 {
		log.WithFields(log.Fields{
			"Count":             removedCount,
			"BlueprintName":     blueprint.Metadata.Name,
			"CurrentGeneration": currentGeneration,
		}).Info("Cleaned up old generation containers")
	}

	return nil
}

// CleanupStoppedContainers removes all stopped/exited containers managed by the reconciler
func (r *Reconciler) CleanupStoppedContainers() error {
	ctx := context.Background()

	// List ALL containers (including stopped) with the managed label
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.managed=true")

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true, // Include stopped containers
		Filters: filterArgs,
	})

	if err != nil {
		return fmt.Errorf("failed to list managed containers: %w", err)
	}

	stoppedCount := 0
	for _, cont := range containers {
		// Check if container is not running
		if cont.State != "running" {
			deploymentName := cont.Labels["colonies.deployment"]
			log.WithFields(log.Fields{
				"ContainerID":   truncateID(cont.ID, 12),
				"ContainerName": cont.Names[0],
				"State":         cont.State,
				"Deployment":    deploymentName,
			}).Info("Removing stopped container")

			// Remove the container
			if err := r.dockerClient.ContainerRemove(ctx, cont.ID, container.RemoveOptions{Force: true}); err != nil {
				log.WithFields(log.Fields{
					"Error":       err,
					"ContainerID": truncateID(cont.ID, 12),
				}).Warn("Failed to remove stopped container")
				continue
			}
			stoppedCount++
		}
	}

	if stoppedCount > 0 {
		log.WithFields(log.Fields{"Count": stoppedCount}).Info("Cleaned up stopped containers")
	}

	return nil
}

// CleanupStaleExecutors removes executor registrations for containers that no longer exist
func (r *Reconciler) CleanupStaleExecutors(deploymentName string, executorType string) error {
	// Get all executors of the given type
	executors, err := r.client.GetExecutors(r.colonyName, r.colonyOwnerKey)
	if err != nil {
		return fmt.Errorf("failed to list executors: %w", err)
	}

	// Get all managed containers (running only)
	ctx := context.Background()
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.managed=true")
	if deploymentName != "" {
		filterArgs.Add("label", "colonies.deployment="+deploymentName)
	}

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: filterArgs,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Build a set of container names for quick lookup
	containerNames := make(map[string]bool)
	for _, cont := range containers {
		// Container names from Docker API have a leading slash
		name := cont.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
		containerNames[name] = true
	}

	// Check each executor and remove if container doesn't exist
	removedCount := 0
	for _, executor := range executors {
		// Only check executors of the specified type
		if executorType != "" && executor.Type != executorType {
			continue
		}

		// Check if executor name matches pattern (starts with deployment name)
		if deploymentName != "" && !strings.HasPrefix(executor.Name, deploymentName+"-") {
			continue
		}

		// Check if container exists for this executor
		if !containerNames[executor.Name] {
			log.WithFields(log.Fields{
				"ExecutorName": executor.Name,
				"ExecutorID":   executor.ID,
				"ExecutorType": executor.Type,
			}).Info("Removing stale executor registration (container not found)")

			// Remove the executor registration
			if err := r.client.RemoveExecutor(r.colonyName, executor.Name, r.colonyOwnerKey); err != nil {
				log.WithFields(log.Fields{
					"Error":      err,
					"ExecutorID": executor.ID,
				}).Warn("Failed to remove stale executor")
				continue
			}
			removedCount++
		}
	}

	if removedCount > 0 {
		log.WithFields(log.Fields{"Count": removedCount}).Info("Cleaned up stale executor registrations")
	}

	return nil
}
