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

// HasOrphanedContainers checks if any running containers don't have corresponding executor registrations
// This is a lightweight check used by checkReconciliationNeeded to determine if reconciliation is needed
func (r *Reconciler) HasOrphanedContainers(blueprint *core.Blueprint, executorType string) (bool, error) {
	ctx := context.Background()

	// List running containers for this deployment
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: filterArgs,
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return false, nil
	}

	// Get all executors of the given type
	executors, err := r.client.GetExecutors(r.colonyName, r.executorPrvKey)
	if err != nil {
		return false, fmt.Errorf("failed to list executors: %w", err)
	}

	// Build a set of registered executor names
	registeredExecutors := make(map[string]bool)
	for _, executor := range executors {
		if executorType == "" || executor.Type == executorType {
			registeredExecutors[executor.Name] = true
		}
	}

	// Check each container
	for _, cont := range containers {
		// Get container name (remove leading slash if present)
		containerName := ""
		if len(cont.Names) > 0 {
			containerName = cont.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
		}

		// Get generation from container label
		generationStr := cont.Labels["colonies.generation"]
		if generationStr == "" {
			continue // Can't determine executor name without generation
		}

		// Construct expected executor name
		executorName := fmt.Sprintf("%s-%s", containerName, generationStr)

		// Check if executor is registered
		if !registeredExecutors[executorName] {
			log.WithFields(log.Fields{
				"ContainerName":    containerName,
				"ExpectedExecutor": executorName,
				"Blueprint":        blueprint.Metadata.Name,
			}).Debug("Found orphaned container during check")
			return true, nil
		}
	}

	return false, nil
}

// FindOrphanedContainers finds running containers that don't have a corresponding executor registration
// This detects containers where the executor registration was lost but the container is still running
func (r *Reconciler) FindOrphanedContainers(process *core.Process, blueprint *core.Blueprint, executorType string) ([]string, error) {
	ctx := context.Background()
	var orphanedContainerIDs []string

	// List running containers for this deployment
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	// Get all executors of the given type
	executors, err := r.client.GetExecutors(r.colonyName, r.executorPrvKey)
	if err != nil {
		return nil, fmt.Errorf("failed to list executors: %w", err)
	}

	// Build a set of registered executor names
	registeredExecutors := make(map[string]bool)
	for _, executor := range executors {
		if executorType == "" || executor.Type == executorType {
			registeredExecutors[executor.Name] = true
		}
	}

	// Check each container
	for _, cont := range containers {
		// Get container name (remove leading slash if present)
		containerName := ""
		if len(cont.Names) > 0 {
			containerName = cont.Names[0]
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
		}

		// Get generation from container label
		generationStr := cont.Labels["colonies.generation"]
		if generationStr == "" {
			continue // Can't determine executor name without generation
		}

		// Construct expected executor name
		executorName := fmt.Sprintf("%s-%s", containerName, generationStr)

		// Check if executor is registered
		if !registeredExecutors[executorName] {
			r.addLog(process, fmt.Sprintf("[ORPHAN_DETECTION] Found orphaned container: %s (expected executor: %s not registered)",
				containerName, executorName))
			log.WithFields(log.Fields{
				"ContainerID":      truncateID(cont.ID, 12),
				"ContainerName":    containerName,
				"ExpectedExecutor": executorName,
				"Blueprint":        blueprint.Metadata.Name,
			}).Warn("Found orphaned container without executor registration")
			orphanedContainerIDs = append(orphanedContainerIDs, cont.ID)
		}
	}

	return orphanedContainerIDs, nil
}

// shouldHandleBlueprint returns true if this reconciler should handle the given blueprint
// Process routing already handles executor type and location targeting
// Handler config is now verified by the server via BlueprintDefinition
func (r *Reconciler) shouldHandleBlueprint(blueprint *core.Blueprint) bool {
	return true
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
func (r *Reconciler) CleanupOldGenerationContainers(process *core.Process, blueprint *core.Blueprint) error {
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

				r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupOldGenerationContainers: reconciler=%s removing executor=%s reason=old_generation(gen=%d, current=%d) blueprint=%s",
					r.executorName, executorName, generation, currentGeneration, blueprint.Metadata.Name))

				if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
					log.WithFields(log.Fields{
						"Error":        err,
						"ExecutorName": executorName,
					}).Debug("Failed to deregister executor (may already be removed)")
					r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupOldGenerationContainers: failed to remove executor=%s error=%v", executorName, err))
					// Continue anyway - executor might already be deregistered
				} else {
					log.WithFields(log.Fields{
						"ExecutorName": executorName,
						"Generation":   generation,
					}).Debug("Deregistered old generation executor")
					r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupOldGenerationContainers: successfully removed executor=%s generation=%d", executorName, generation))
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
func (r *Reconciler) CleanupStoppedContainers(process *core.Process) error {
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
func (r *Reconciler) CleanupStaleExecutors(process *core.Process, deploymentName string, executorType string) error {
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

	// Build a set of executor names from container labels for direct matching
	containerExecutors := make(map[string]bool)
	for _, cont := range containers {
		if execName, ok := cont.Labels["colonies.executor"]; ok && execName != "" {
			containerExecutors[execName] = true
		}
	}

	// Check each executor and remove if container doesn't exist
	removedCount := 0
	for _, executor := range executors {
		// Only check executors of the specified type
		if executorType != "" && executor.Type != executorType {
			continue
		}

		// Filter by deployment using BlueprintID if available (preferred over name parsing)
		// This avoids fragile string parsing of executor names with hyphens
		if deploymentName != "" {
			// First, try to match using colonies.executor label (direct match)
			if len(containerExecutors) > 0 {
				// If we have executor labels, use them for precise matching
				if containerExecutors[executor.Name] {
					continue // Container exists for this executor
				}
			}

			// Fallback: use BlueprintID if set, otherwise use legacy name parsing
			// Legacy name parsing kept for backward compatibility with executors
			// created before BlueprintID was set
			if executor.BlueprintID == "" {
				// Legacy: parse executor name to check deployment membership
				// Format: <deployment>-<hash>-<generation>
				parts := strings.Split(executor.Name, "-")
				if len(parts) < 3 {
					continue // Not a valid executor name format
				}
				deploymentParts := strings.Split(deploymentName, "-")
				if len(parts) != len(deploymentParts)+2 {
					continue // Wrong number of parts for this deployment
				}
				execDeployment := strings.Join(parts[:len(deploymentParts)], "-")
				if execDeployment != deploymentName {
					continue
				}
			}
			// Note: BlueprintID matching would require looking up the blueprint by name
			// to get its ID, which adds complexity. The colonies.executor label approach
			// is simpler and more reliable.
		}

		// For executors without direct label match, derive container name from executor name
		// Executor name format: <deployment>-<hash>-<generation>
		// Container name format: <deployment>-<hash>
		containerName := executor.Name
		if lastDash := strings.LastIndex(executor.Name, "-"); lastDash != -1 {
			containerName = executor.Name[:lastDash]
		}

		// Check if container exists for this executor (using both methods)
		if containerExecutors[executor.Name] || containerNames[containerName] {
			continue // Container exists
		}

		// Container doesn't exist - remove stale executor
		log.WithFields(log.Fields{
			"ExecutorName": executor.Name,
			"ExecutorID":   executor.ID,
			"ExecutorType": executor.Type,
		}).Info("Removing stale executor registration (container not found)")

		r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupStaleExecutors: reconciler=%s removing executor=%s type=%s reason=container_not_found(expected_container=%s) deployment=%s",
			r.executorName, executor.Name, executor.Type, containerName, deploymentName))

		// Remove the executor registration
		if err := r.client.RemoveExecutor(r.colonyName, executor.Name, r.colonyOwnerKey); err != nil {
			log.WithFields(log.Fields{
				"Error":      err,
				"ExecutorID": executor.ID,
			}).Warn("Failed to remove stale executor")
			r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupStaleExecutors: failed to remove executor=%s error=%v", executor.Name, err))
			continue
		}
		r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupStaleExecutors: successfully removed executor=%s", executor.Name))
		removedCount++
	}

	if removedCount > 0 {
		log.WithFields(log.Fields{"Count": removedCount}).Info("Cleaned up stale executor registrations")
	}

	return nil
}

// CleanupDeletedBlueprint removes all containers and executors for a deleted blueprint
func (r *Reconciler) CleanupDeletedBlueprint(process *core.Process, blueprintName string, kind string) error {
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
			"Generation":    generation,
		}).Info("Removing container for deleted blueprint")

		// Deregister executor if this was an ExecutorDeployment
		// The executor name format is containerName-generation
		if kind == "ExecutorDeployment" && containerName != "" {
			executorName := fmt.Sprintf("%s-%d", containerName, generation)

			r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupDeletedBlueprint: reconciler=%s removing executor=%s reason=blueprint_deleted blueprint=%s generation=%d",
				r.executorName, executorName, blueprintName, generation))

			if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
				log.WithFields(log.Fields{
					"Error":        err,
					"ExecutorName": executorName,
				}).Warn("Failed to deregister executor")
				r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupDeletedBlueprint: failed to remove executor=%s error=%v", executorName, err))
			} else {
				log.WithFields(log.Fields{
					"ExecutorName": executorName,
				}).Info("Deregistered executor for deleted blueprint")
				r.addLog(process, fmt.Sprintf("[EXECUTOR_REMOVAL] CleanupDeletedBlueprint: successfully removed executor=%s", executorName))
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
