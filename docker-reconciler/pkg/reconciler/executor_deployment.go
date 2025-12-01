package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/colonies/pkg/security/crypto"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
)

// reconcileExecutorDeployment handles ExecutorDeployment blueprints
func (r *Reconciler) reconcileExecutorDeployment(process *core.Process, blueprint *core.Blueprint) error {
	// Parse the deployment spec
	var spec DeploymentSpec
	specBytes, err := json.Marshal(blueprint.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal spec: %w", err)
	}

	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("failed to unmarshal deployment spec: %w", err)
	}

	// Validate required fields
	if spec.Image == "" {
		return fmt.Errorf("image is required")
	}
	if spec.Replicas < 0 {
		spec.Replicas = 1 // Default to 1 replica if negative
	}

	log.WithFields(log.Fields{
		"Image":    spec.Image,
		"Replicas": spec.Replicas,
	}).Info("Reconciling deployment")

	// Add logs to the process
	r.addLog(process, fmt.Sprintf("Reconciling ExecutorDeployment: %s", blueprint.Metadata.Name))
	r.addLog(process, fmt.Sprintf("Image: %s, Replicas: %d", spec.Image, spec.Replicas))

	// Pull the container image
	if err := r.pullImage(process, spec.Image); err != nil {
		r.addLog(process, fmt.Sprintf("ERROR: Failed to pull image %s: %v", spec.Image, err))
		return fmt.Errorf("failed to pull image: %w", err)
	}

	// Get currently running containers for this deployment
	existingContainers, err := r.listContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		r.addLog(process, fmt.Sprintf("Warning: Failed to list existing containers: %v", err))
		existingContainers = []string{} // Continue with empty list
	}

	// Check for dirty containers (generation mismatch) and recreate them
	dirtyContainers, err := r.findDirtyContainers(existingContainers, blueprint.Metadata.Generation)
	if err != nil {
		r.addLog(process, fmt.Sprintf("Warning: Failed to check for dirty containers: %v", err))
	} else if len(dirtyContainers) > 0 {
		r.addLog(process, fmt.Sprintf("Found %d dirty container(s) with outdated generation, recreating...", len(dirtyContainers)))

		for _, containerID := range dirtyContainers {
			// Get container info before removing
			inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
			if err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to inspect dirty container %s: %v", containerID, err))
				continue
			}

			containerName := inspect.Name
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}

			// With generation-based naming, the old executor needs to be deregistered
			// because the new container will have a different executor name (with new generation number)

			// Extract the generation from the container's label to construct the correct executor name
			if blueprint.Kind == "ExecutorDeployment" {
				oldGeneration := inspect.Config.Labels["colonies.generation"]
				if oldGeneration != "" {
					oldExecutorName := fmt.Sprintf("%s-%s", containerName, oldGeneration)
					r.addLog(process, fmt.Sprintf("Deregistering old executor: %s (generation %s)", oldExecutorName, oldGeneration))
					if err := r.client.RemoveExecutor(r.colonyName, oldExecutorName, r.colonyOwnerKey); err != nil {
						r.addLog(process, fmt.Sprintf("Warning: Failed to deregister executor %s: %v", oldExecutorName, err))
					} else {
						r.addLog(process, fmt.Sprintf("Deregistered executor: %s", oldExecutorName))
					}
				}
			}

			// Stop and remove the dirty container
			r.addLog(process, fmt.Sprintf("Removing dirty container: %s (generation mismatch)", containerName))
			if err := r.stopAndRemoveContainer(containerID); err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to remove dirty container %s: %v", containerID, err))
				continue
			}

			// Recreate the container with new spec and generation
			if err := r.startContainer(process, spec, containerName, blueprint); err != nil {
				r.addLog(process, fmt.Sprintf("ERROR: Failed to recreate container %s: %v", containerName, err))
				return fmt.Errorf("failed to recreate container %s: %w", containerName, err)
			}
			r.addLog(process, fmt.Sprintf("Recreated container: %s with generation %d", containerName, blueprint.Metadata.Generation))
		}

		// Refresh the container list after recreating dirty ones
		existingContainers, err = r.listContainersByLabel(blueprint.Metadata.Name)
		if err != nil {
			r.addLog(process, fmt.Sprintf("Warning: Failed to refresh container list: %v", err))
			existingContainers = []string{}
		}
	}

	// For ExecutorDeployments, check for orphaned containers (containers without executor registrations)
	// This handles the case where containers are running but their executors were removed
	if blueprint.Kind == "ExecutorDeployment" {
		orphanedContainers, err := r.FindOrphanedContainers(process, blueprint, spec.ExecutorType)
		if err != nil {
			r.addLog(process, fmt.Sprintf("Warning: Failed to check for orphaned containers: %v", err))
		} else if len(orphanedContainers) > 0 {
			r.addLog(process, fmt.Sprintf("Found %d orphaned container(s) without executor registrations, recreating...", len(orphanedContainers)))

			for _, containerID := range orphanedContainers {
				// Get container info before removing
				inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
				if err != nil {
					r.addLog(process, fmt.Sprintf("Warning: Failed to inspect orphaned container %s: %v", containerID, err))
					continue
				}

				containerName := inspect.Name
				if len(containerName) > 0 && containerName[0] == '/' {
					containerName = containerName[1:]
				}

				// Stop and remove the orphaned container
				r.addLog(process, fmt.Sprintf("Removing orphaned container: %s (executor not registered)", containerName))
				if err := r.stopAndRemoveContainer(containerID); err != nil {
					r.addLog(process, fmt.Sprintf("Warning: Failed to remove orphaned container %s: %v", containerID, err))
					continue
				}

				// Recreate the container with new spec and generation (this will also register the executor)
				if err := r.startContainer(process, spec, containerName, blueprint); err != nil {
					r.addLog(process, fmt.Sprintf("ERROR: Failed to recreate orphaned container %s: %v", containerName, err))
					return fmt.Errorf("failed to recreate orphaned container %s: %w", containerName, err)
				}
				r.addLog(process, fmt.Sprintf("Recreated orphaned container: %s with generation %d", containerName, blueprint.Metadata.Generation))
			}

			// Refresh the container list after recreating orphaned ones
			existingContainers, err = r.listContainersByLabel(blueprint.Metadata.Name)
			if err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to refresh container list: %v", err))
				existingContainers = []string{}
			}
		}
	}

	// Count only RUNNING containers for replica comparison (stopped containers shouldn't count)
	runningContainers, err := r.listRunningContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		r.addLog(process, fmt.Sprintf("Warning: Failed to list running containers: %v", err))
		runningContainers = []string{}
	}

	currentReplicas := len(runningContainers)
	r.addLog(process, fmt.Sprintf("Current replicas: %d, Desired replicas: %d", currentReplicas, spec.Replicas))

	// Scale up or down
	if currentReplicas < spec.Replicas {
		// Scale up - start new containers
		containersToStart := spec.Replicas - currentReplicas
		r.addLog(process, fmt.Sprintf("Scaling up: starting %d new container(s)", containersToStart))

		for i := 0; i < containersToStart; i++ {
			// Generate unique executor name that will be used as both container name and executor name
			var containerName string
			if blueprint.Kind == "ExecutorDeployment" {
				// For executor deployments, generate unique name based on blueprint name
				uniqueExecutorName, err := r.generateUniqueExecutorName(blueprint.Metadata.ColonyName, blueprint.Metadata.Name)
				if err != nil {
					r.addLog(process, fmt.Sprintf("Error generating unique executor name: %v", err))
					return fmt.Errorf("failed to generate unique executor name: %w", err)
				}
				containerName = uniqueExecutorName
			} else {
				// For non-executor deployments, use index-based naming
				containerName = fmt.Sprintf("%s-%d", blueprint.Metadata.Name, currentReplicas+i)
			}

			if err := r.startContainer(process, spec, containerName, blueprint); err != nil {
				r.addLog(process, fmt.Sprintf("ERROR: Failed to start container %s: %v", containerName, err))
				return fmt.Errorf("failed to start container %s: %w", containerName, err)
			}
			r.addLog(process, fmt.Sprintf("Started container: %s", containerName))
		}
	} else if currentReplicas > spec.Replicas {
		// Scale down - stop excess containers
		containersToStop := currentReplicas - spec.Replicas
		r.addLog(process, fmt.Sprintf("Scaling down: stopping %d container(s)", containersToStop))

		// Get full container info to get names for deregistration
		ctx := context.Background()
		filterArgs := filters.NewArgs()
		filterArgs.Add("label", "colonies.deployment="+blueprint.Metadata.Name)
		containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
			All:     false,
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
				// Extract the generation from the container's label
				// The executor name uses the original generation, not the current blueprint generation
				containerGeneration := cont.Labels["colonies.generation"]
				if containerGeneration != "" {
					executorName := fmt.Sprintf("%s-%s", containerName, containerGeneration)

					log.WithFields(log.Fields{
						"ExecutorName":        executorName,
						"ContainerID":         truncateID(containerID, 12),
						"ContainerGeneration": containerGeneration,
						"BlueprintGeneration": blueprint.Metadata.Generation,
					}).Info("Deregistering executor before stopping container")

					if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
						log.WithFields(log.Fields{
							"Error":        err,
							"ExecutorName": executorName,
						}).Warn("Failed to deregister executor")
						r.addLog(process, fmt.Sprintf("Warning: Failed to deregister executor %s: %v", executorName, err))
						// Continue anyway to stop the container
					} else {
						r.addLog(process, fmt.Sprintf("Deregistered executor: %s (generation %s)", executorName, containerGeneration))
					}
				} else {
					log.WithFields(log.Fields{
						"ContainerID": truncateID(containerID, 12),
					}).Warn("Could not extract generation label from container")
					r.addLog(process, "Warning: Could not extract generation from container label, skipping deregistration")
				}
			}

			// Now stop and remove the container
			if err := r.stopAndRemoveContainer(containerID); err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to stop container %s: %v", truncateID(containerID, 12), err))
			} else {
				r.addLog(process, fmt.Sprintf("Stopped and removed container: %s", containerName))
			}
		}
	} else {
		r.addLog(process, "Deployment is at desired state")
	}

	// Cleanup old generation containers first (safety net for rapid updates)
	if err := r.CleanupOldGenerationContainers(process, blueprint); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup old generation containers")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup old generation containers: %v", err))
	}

	// Cleanup stopped containers and stale executor registrations
	r.addLog(process, "Running cleanup of stopped containers and stale executors...")
	if err := r.CleanupStoppedContainers(process); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup stopped containers")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup stopped containers: %v", err))
	}

	if err := r.CleanupStaleExecutors(process, blueprint.Metadata.Name, spec.ExecutorType); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup stale executors")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup stale executors: %v", err))
	}

	r.addLog(process, "Reconciliation completed successfully")
	return nil
}

// startContainer creates and starts a container for an ExecutorDeployment
func (r *Reconciler) startContainer(process *core.Process, spec DeploymentSpec, containerName string, blueprint *core.Blueprint) error {
	ctx := context.Background()

	// Check if a container with this name already exists (running or stopped)
	// and remove it if it does
	existingContainers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true, // Include stopped containers
		Filters: filters.NewArgs(filters.Arg("name", "^/"+containerName+"$")),
	})
	if err != nil {
		log.WithFields(log.Fields{"Error": err, "ContainerName": containerName}).Warn("Failed to check for existing container")
	} else if len(existingContainers) > 0 {
		// Container exists - remove it
		containerID := existingContainers[0].ID
		log.WithFields(log.Fields{
			"ContainerID":   containerID,
			"ContainerName": containerName,
		}).Info("Removing existing container before creating new one")

		// Stop if running
		if existingContainers[0].State == "running" {
			timeout := 10
			if err := r.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
				log.WithFields(log.Fields{"Error": err, "ContainerID": containerID}).Warn("Failed to stop existing container")
			}
		}

		// Remove the container
		if err := r.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
			log.WithFields(log.Fields{"Error": err, "ContainerID": containerID}).Warn("Failed to remove existing container")
		}
	}

	// Get executor type from spec (for child executors)
	executorType := "container-executor" // default
	if spec.ExecutorType != "" {
		executorType = spec.ExecutorType
	}

	// Start with auto-injected defaults from reconciler config
	envMap := r.getDefaultEnvVars(executorType)

	// Override/merge with user-provided env vars from blueprint
	if spec.Env != nil {
		for k, v := range spec.Env {
			envMap[k] = fmt.Sprintf("%v", v)
		}
	}

	// Add image-specific metadata
	envMap["EXECUTOR_SW_NAME"] = spec.Image
	envMap["EXECUTOR_SW_VERSION"] = spec.Image

	// Always set these (cannot be overridden)
	envMap["COLONIES_DEPLOYMENT"] = blueprint.Metadata.Name
	envMap["COLONIES_CONTAINER_NAME"] = containerName

	// Convert merged env map to string slice
	envVars := []string{}
	for k, v := range envMap {
		envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
	}

	// If this is a colony executor deployment, generate keys and register executor
	// (the container name is now the unique executor name generated in Reconcile())
	if blueprint.Kind == "ExecutorDeployment" {
		// Generate unique keypair for this executor
		cryptoInstance := crypto.CreateCrypto()
		executorPrvKey, err := cryptoInstance.GeneratePrivateKey()
		if err != nil {
			return fmt.Errorf("failed to generate executor private key: %w", err)
		}

		// Generate executor ID from private key
		executorID, err := cryptoInstance.GenerateID(executorPrvKey)
		if err != nil {
			return fmt.Errorf("failed to generate executor ID: %w", err)
		}

		// Register executor with Colonies server using colony owner privileges
		// executorType was already determined above from spec.ExecutorType
		// Append generation number to executor name for unique identification across generations
		executorName := fmt.Sprintf("%s-%d", containerName, blueprint.Metadata.Generation)
		newExecutor := core.CreateExecutor(executorID, executorType, executorName, r.colonyName, time.Now(), time.Now())

		// Populate hardware capabilities from blueprint env vars, falling back to auto-detection
		populateCapabilitiesFromEnv(newExecutor, envMap)

		// Override location with reconciler's location setting (if not specified in env)
		locName := envMap["EXECUTOR_LOCATION_NAME"]
		if locName == "" {
			locName = os.Getenv("EXECUTOR_LOCATION_NAME")
		}
		locDesc := envMap["EXECUTOR_LOCATION_DESC"]
		if locDesc == "" {
			locDesc = r.location
		}
		newExecutor.Location = core.Location{Name: locName, Description: locDesc}

		addedExecutor, err := r.client.AddExecutor(newExecutor, r.colonyOwnerKey)
		if err != nil {
			return fmt.Errorf("failed to register executor %s: %w", containerName, err)
		}

		// Auto-approve the executor (reconciler has colony owner privileges)
		log.WithFields(log.Fields{
			"ExecutorName":   executorName,
			"ColonyName":     r.colonyName,
			"ColonyOwnerKey": r.colonyOwnerKey[:8] + "...",
		}).Debug("Approving executor with colony owner key")

		err = r.client.ApproveExecutor(r.colonyName, executorName, r.colonyOwnerKey)
		if err != nil {
			return fmt.Errorf("failed to approve executor %s: %w", containerName, err)
		}

		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"ExecutorName":  executorName,
			"ColonyName":    r.colonyName,
		}).Info("Executor approved successfully")

		// Verify the executor state after approval (use executor's own key to query)
		fetchedExecutor, fetchErr := r.client.GetExecutor(r.colonyName, executorName, executorPrvKey)
		if fetchErr != nil {
			log.WithFields(log.Fields{
				"Error":        fetchErr,
				"ExecutorName": executorName,
			}).Error("Failed to fetch executor after approval")
		} else {
			log.WithFields(log.Fields{
				"ExecutorName":  executorName,
				"AddedID":       addedExecutor.ID,
				"FetchedID":     fetchedExecutor.ID,
				"FetchedState":  fetchedExecutor.State,
				"IDMatch":       addedExecutor.ID == fetchedExecutor.ID,
			}).Info("Executor state after approval")
		}

		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"ExecutorName":  executorName,
			"ExecutorID":    addedExecutor.ID,
			"ExecutorType":  executorType,
		}).Info("Generated and registered executor with Colonies server")
		r.addLog(process, fmt.Sprintf("Registered executor: %s (ID: %s)", executorName, addedExecutor.ID))

		// Inject the generated private key and executor ID into container environment
		envVars = append(envVars, "COLONIES_PRVKEY="+executorPrvKey)
		// Append generation number to executor name for unique identification across generations
		envVars = append(envVars, "COLONIES_EXECUTOR_NAME="+executorName)
		envVars = append(envVars, "COLONIES_EXECUTOR_ID="+executorID)

		// Add node metadata environment variables for executor registration
		nodeEnvVars := getNodeEnvVars()
		envVars = append(envVars, nodeEnvVars...)

		log.WithFields(log.Fields{
			"BlueprintName": blueprint.Metadata.Name,
			"ExecutorName":  executorName,
			"ContainerName": containerName,
		}).Info("Using container name with generation suffix as executor name")
		r.addLog(process, fmt.Sprintf("Executor name: %s (generation %d)", executorName, blueprint.Metadata.Generation))
	}

	// Create container config
	config := &container.Config{
		Image: spec.Image,
		Env:   envVars,
		Labels: map[string]string{
			"colonies.deployment": blueprint.Metadata.Name,
			"colonies.managed":    "true",
			"colonies.generation": fmt.Sprintf("%d", blueprint.Metadata.Generation),
		},
	}

	// Override entrypoint if specified
	if len(spec.Entrypoint) > 0 {
		config.Entrypoint = spec.Entrypoint
	}

	// Override command if specified
	if len(spec.Command) > 0 {
		config.Cmd = spec.Command
	} else if len(spec.Args) > 0 {
		config.Cmd = spec.Args
	}

	// Create host config (for port bindings, volumes, etc.)
	hostConfig := &container.HostConfig{
		Privileged: spec.Privileged,
	}

	// Add volume mounts
	if len(spec.Volumes) > 0 {
		binds := make([]string, len(spec.Volumes))
		for i, vol := range spec.Volumes {
			bind := vol.Host + ":" + vol.Container
			if vol.ReadOnly {
				bind += ":ro"
			}
			binds[i] = bind
		}
		hostConfig.Binds = binds
	}

	// Add port bindings
	if len(spec.Ports) > 0 {
		portBindings := nat.PortMap{}
		exposedPorts := nat.PortSet{}

		for _, port := range spec.Ports {
			protocol := port.Protocol
			if protocol == "" {
				protocol = "TCP"
			}

			containerPort, err := nat.NewPort(strings.ToLower(protocol), fmt.Sprintf("%d", port.Port))
			if err != nil {
				log.WithFields(log.Fields{"Error": err, "Port": port.Port}).Warn("Failed to create port binding")
				continue
			}

			hostPort := fmt.Sprintf("%d", port.Port)

			portBindings[containerPort] = []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: hostPort,
				},
			}
			exposedPorts[containerPort] = struct{}{}
		}

		hostConfig.PortBindings = portBindings
		config.ExposedPorts = exposedPorts
	}

	// Create network config to attach to colonies network
	// Add network alias using the deployment name so containers can reach each other
	// by blueprint name (e.g., c1-database) instead of random container name (c1-database-e9328)
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			r.dockerNetwork: {
				Aliases: []string{blueprint.Metadata.Name},
			},
		},
	}

	// Create the container
	resp, err := r.dockerClient.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start the container
	if err := r.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for container to be running (with 30 second timeout)
	if err := r.waitForContainerRunning(resp.ID, 30*time.Second); err != nil {
		return fmt.Errorf("container failed to start: %w", err)
	}

	log.WithFields(log.Fields{
		"ContainerID":   resp.ID,
		"ContainerName": containerName,
		"Image":         spec.Image,
	}).Info("Container started and running")

	return nil
}

// populateCapabilitiesFromEnv populates executor capabilities from environment variables only
func populateCapabilitiesFromEnv(executor *core.Executor, envMap map[string]string) {
	// Initialize hardware array with one entry
	executor.Capabilities.Hardware = []core.Hardware{{}}
	hw := &executor.Capabilities.Hardware[0]

	// Populate from env vars
	hw.Model = envMap["EXECUTOR_HW_MODEL"]
	hw.CPU = envMap["EXECUTOR_HW_CPU"]
	if coresStr := envMap["EXECUTOR_HW_CPU_CORES"]; coresStr != "" {
		if cores, err := strconv.Atoi(coresStr); err == nil {
			hw.Cores = cores
		}
	}
	hw.Memory = envMap["EXECUTOR_HW_MEM"]
	hw.Storage = envMap["EXECUTOR_HW_STORAGE"]
	hw.Platform = envMap["EXECUTOR_HW_PLATFORM"]
	hw.Architecture = envMap["EXECUTOR_HW_ARCHITECTURE"]
	if network := envMap["EXECUTOR_HW_NETWORK"]; network != "" {
		hw.Network = strings.Split(network, ",")
	}
	if nodesStr := envMap["EXECUTOR_HW_NODES"]; nodesStr != "" {
		if nodes, err := strconv.Atoi(nodesStr); err == nil {
			hw.Nodes = nodes
		}
	}

	// GPU settings
	hw.GPU.Name = envMap["EXECUTOR_HW_GPU_NAME"]
	hw.GPU.Memory = envMap["EXECUTOR_HW_GPU_MEM"]
	if gpuCountStr := envMap["EXECUTOR_HW_GPU_COUNT"]; gpuCountStr != "" {
		if gpuCount, err := strconv.Atoi(gpuCountStr); err == nil {
			hw.GPU.Count = gpuCount
		}
	}
	if gpuNodeCountStr := envMap["EXECUTOR_HW_GPU_NODES_COUNT"]; gpuNodeCountStr != "" {
		if gpuNodeCount, err := strconv.Atoi(gpuNodeCountStr); err == nil {
			hw.GPU.NodeCount = gpuNodeCount
		}
	}
}
