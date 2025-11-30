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

// shouldHandleBlueprint returns true if this reconciler should handle the given blueprint
func (r *Reconciler) shouldHandleBlueprint(blueprint *core.Blueprint) bool {
	if blueprint.Handler == nil {
		return false
	}

	// Check single executor name
	if blueprint.Handler.ExecutorName != "" {
		return blueprint.Handler.ExecutorName == r.executorName
	}

	// Check list of executor names
	if len(blueprint.Handler.ExecutorNames) > 0 {
		for _, name := range blueprint.Handler.ExecutorNames {
			if name == r.executorName {
				return true
			}
		}
		return false
	}

	return false
}

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
			// Get container name for logging
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
			// Use the generation from the container's label to construct the correct executor name
			if blueprint.Kind == "ExecutorDeployment" && containerName != "" {
				// The executor name includes the generation it was created with
				executorName := fmt.Sprintf("%s-%d", containerName, generation)

				if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
					log.WithFields(log.Fields{
						"Error":        err,
						"ExecutorName": executorName,
					}).Debug("Failed to deregister executor (may already be removed)")
					// Continue anyway - executor might already be deregistered
				} else {
					log.WithFields(log.Fields{
						"ExecutorName": executorName,
						"Generation":   generation,
					}).Debug("Deregistered old generation executor")
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
// It only cleans up executors for blueprints this reconciler is responsible for handling
func (r *Reconciler) CleanupStaleExecutors(deploymentName string, executorType string) error {
	// First, verify this reconciler should handle this deployment by checking the blueprint
	if deploymentName != "" {
		blueprint, err := r.client.GetBlueprint(r.colonyName, deploymentName, r.executorPrvKey)
		if err != nil {
			// If blueprint doesn't exist, skip cleanup - another reconciler may have deleted it
			log.WithFields(log.Fields{
				"DeploymentName": deploymentName,
				"Error":          err,
			}).Debug("Blueprint not found, skipping stale executor cleanup")
			return nil
		}

		// Check if this reconciler should handle this blueprint
		if !r.shouldHandleBlueprint(blueprint) {
			log.WithFields(log.Fields{
				"DeploymentName": deploymentName,
				"ReconcilerName": r.executorName,
				"HandlerName":    blueprint.Handler.ExecutorName,
			}).Debug("Skipping stale executor cleanup - blueprint not handled by this reconciler")
			return nil
		}
	}

	// Get all executors of the given type (use executor key, not colony owner key)
	executors, err := r.client.GetExecutors(r.colonyName, r.executorPrvKey)
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

		// Check if executor name matches pattern (must be exactly <deploymentName>-<hash>-<generation>)
		// Use a more precise check to avoid matching deployments that share a prefix
		// e.g., "ollama" should not match "ollama-ultra-xxx-1"
		if deploymentName != "" {
			// Split the executor name to extract the deployment part
			// Format: <deployment>-<hash>-<generation>
			parts := strings.Split(executor.Name, "-")
			if len(parts) < 3 {
				continue // Not a valid executor name format
			}
			// The deployment name might contain hyphens, so we need to check if the
			// executor name starts with the deployment name followed by exactly 2 more parts
			// For simple deployment names without hyphens (like "ollama"), this is straightforward
			// For deployment names with hyphens (like "ollama-ultra"), we need to reconstruct
			deploymentParts := strings.Split(deploymentName, "-")

			// Check if the executor name has the deployment prefix followed by hash and generation
			if len(parts) != len(deploymentParts)+2 {
				continue // Wrong number of parts for this deployment
			}

			// Verify the deployment name matches exactly
			execDeployment := strings.Join(parts[:len(deploymentParts)], "-")
			if execDeployment != deploymentName {
				continue
			}
		}

		// Executor name format: <deployment>-<hash>-<generation>
		// Container name format: <deployment>-<hash>
		// Strip the generation suffix to get the container name
		containerName := executor.Name
		if lastDash := strings.LastIndex(executor.Name, "-"); lastDash != -1 {
			containerName = executor.Name[:lastDash]
		}

		// Check if container exists for this executor
		if !containerNames[containerName] {
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

// CleanupDeletedBlueprint removes all containers and executors for a deleted blueprint
func (r *Reconciler) CleanupDeletedBlueprint(blueprintName string) error {
	ctx := context.Background()

	// List ALL containers for this deployment (including stopped)
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprintName)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})

	if err != nil {
		return fmt.Errorf("failed to list containers for cleanup: %w", err)
	}

	if len(containers) == 0 {
		log.WithFields(log.Fields{
			"BlueprintName": blueprintName,
		}).Info("No containers found for deleted blueprint")
		return nil
	}

	removedCount := 0
	for _, cont := range containers {
		// Get container name for logging
		containerName := ""
		if len(cont.Names) > 0 {
			containerName = cont.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
		}

		// Get generation for executor deregistration
		generationStr := cont.Labels["colonies.generation"]
		var generation int64
		if generationStr != "" {
			fmt.Sscanf(generationStr, "%d", &generation)
		}

		log.WithFields(log.Fields{
			"ContainerID":   truncateID(cont.ID, 12),
			"ContainerName": containerName,
			"State":         cont.State,
		}).Info("Removing container for deleted blueprint")

		// Deregister executor if this was an ExecutorDeployment
		if containerName != "" && generation > 0 {
			executorName := fmt.Sprintf("%s-%d", containerName, generation)
			if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
				log.WithFields(log.Fields{
					"Error":        err,
					"ExecutorName": executorName,
				}).Debug("Failed to deregister executor (may already be removed)")
			} else {
				log.WithFields(log.Fields{
					"ExecutorName": executorName,
				}).Debug("Deregistered executor for deleted blueprint")
			}
		}

		// Remove the container
		if err := r.dockerClient.ContainerRemove(ctx, cont.ID, container.RemoveOptions{Force: true}); err != nil {
			log.WithFields(log.Fields{
				"Error":       err,
				"ContainerID": truncateID(cont.ID, 12),
			}).Warn("Failed to remove container")
		} else {
			removedCount++
		}
	}

	log.WithFields(log.Fields{
		"Count":         removedCount,
		"BlueprintName": blueprintName,
	}).Info("Cleaned up containers for deleted blueprint")

	return nil
}
