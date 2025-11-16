package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
)

// reconcileDockerDeployment handles DockerDeployment blueprints
func (r *Reconciler) reconcileDockerDeployment(process *core.Process, blueprint *core.Blueprint) error {
	// Parse the docker deployment spec
	var spec DockerDeploymentSpec
	specBytes, err := json.Marshal(blueprint.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal spec: %w", err)
	}

	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("failed to unmarshal docker deployment spec: %w", err)
	}

	// Validate required fields
	if len(spec.Instances) == 0 {
		return fmt.Errorf("at least one instance is required")
	}

	// Determine number of replicas
	replicas := spec.Replicas
	if replicas <= 0 {
		replicas = len(spec.Instances) // Default to number of instances if not specified
	}

	r.addLog(process, fmt.Sprintf("Reconciling DockerDeployment: %s with %d instance(s) and %d replicas", blueprint.Metadata.Name, len(spec.Instances), replicas))

	// Get currently running containers for this deployment
	existingContainers, err := r.listContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		r.addLog(process, fmt.Sprintf("Warning: Failed to list existing containers: %v", err))
		existingContainers = []string{} // Continue with empty list
	}

	// Build a map of existing containers by name
	existingContainersByName := make(map[string]string) // name -> containerID
	for _, containerID := range existingContainers {
		inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
		if err != nil {
			r.addLog(process, fmt.Sprintf("Warning: Failed to inspect container %s: %v", containerID, err))
			continue
		}

		containerName := inspect.Name
		if len(containerName) > 0 && containerName[0] == '/' {
			containerName = containerName[1:]
		}
		existingContainersByName[containerName] = containerID
	}

	// Process each instance with replicas
	for _, instance := range spec.Instances {
		// Validate instance
		if instance.Name == "" {
			return fmt.Errorf("instance name is required")
		}
		if instance.Image == "" {
			return fmt.Errorf("instance image is required for %s", instance.Name)
		}

		r.addLog(process, fmt.Sprintf("Processing instance: %s (image: %s) with %d replica(s)", instance.Name, instance.Image, replicas))

		// Pull the image
		if err := r.pullImage(process, instance.Image); err != nil {
			return fmt.Errorf("failed to pull image for instance %s: %w", instance.Name, err)
		}

		// Create replicas for this instance
		for i := 0; i < replicas; i++ {
			var containerName string
			if replicas == 1 {
				// Single replica: use instance name as-is
				containerName = instance.Name
			} else {
				// Multiple replicas: append replica number
				containerName = fmt.Sprintf("%s-%d", instance.Name, i)
			}

			// Check if container already exists
			existingContainerID, exists := existingContainersByName[containerName]

			if exists {
				// Check if container is dirty (generation mismatch)
				inspect, err := r.dockerClient.ContainerInspect(context.Background(), existingContainerID)
				if err != nil {
					r.addLog(process, fmt.Sprintf("Warning: Failed to inspect container %s: %v", containerName, err))
				} else {
					// Check generation
					generationStr, hasLabel := inspect.Config.Labels["colonies.generation"]
					isDirty := false

					if !hasLabel {
						isDirty = true
						r.addLog(process, fmt.Sprintf("Container %s has no generation label, recreating", containerName))
					} else {
						var containerGeneration int64
						if _, err := fmt.Sscanf(generationStr, "%d", &containerGeneration); err != nil {
							isDirty = true
							r.addLog(process, fmt.Sprintf("Container %s has invalid generation label, recreating", containerName))
						} else if containerGeneration < blueprint.Metadata.Generation {
							isDirty = true
							r.addLog(process, fmt.Sprintf("Container %s has outdated generation (%d < %d), recreating",
								containerName, containerGeneration, blueprint.Metadata.Generation))
						}
					}

					if isDirty {
						// Remove dirty container
						if err := r.stopAndRemoveContainer(existingContainerID); err != nil {
							return fmt.Errorf("failed to remove dirty container %s: %w", containerName, err)
						}
						// Create new container with custom name
						if err := r.startDockerDeploymentInstanceWithName(process, instance, blueprint, containerName); err != nil {
							return fmt.Errorf("failed to start instance %s: %w", containerName, err)
						}
						r.addLog(process, fmt.Sprintf("Recreated container: %s with generation %d", containerName, blueprint.Metadata.Generation))
					} else {
						r.addLog(process, fmt.Sprintf("Container %s is up to date", containerName))
					}
				}
			} else {
				// Container doesn't exist, create it
				if err := r.startDockerDeploymentInstanceWithName(process, instance, blueprint, containerName); err != nil {
					return fmt.Errorf("failed to start instance %s: %w", containerName, err)
				}
				r.addLog(process, fmt.Sprintf("Created container: %s", containerName))
			}

			// Remove from map so we can track which containers to remove
			delete(existingContainersByName, containerName)
		}
	}

	// Remove any containers that are no longer in the spec
	for containerName, containerID := range existingContainersByName {
		r.addLog(process, fmt.Sprintf("Removing obsolete container: %s", containerName))
		if err := r.stopAndRemoveContainer(containerID); err != nil {
			r.addLog(process, fmt.Sprintf("Warning: Failed to remove container %s: %v", containerName, err))
		}
	}

	// Cleanup stopped containers (don't cleanup stale executors for DockerDeployment as they might not be executors)
	r.addLog(process, "Running cleanup of stopped containers...")
	if err := r.CleanupStoppedContainers(); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup stopped containers")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup stopped containers: %v", err))
	}

	r.addLog(process, "Docker deployment reconciliation completed successfully")
	return nil
}

// startDockerDeploymentInstance creates and starts a container from a ContainerInstance spec
func (r *Reconciler) startDockerDeploymentInstance(process *core.Process, instance ContainerInstance, blueprint *core.Blueprint) error {
	return r.startDockerDeploymentInstanceWithName(process, instance, blueprint, instance.Name)
}

// startDockerDeploymentInstanceWithName creates and starts a container with a custom name
func (r *Reconciler) startDockerDeploymentInstanceWithName(process *core.Process, instance ContainerInstance, blueprint *core.Blueprint, containerName string) error {
	ctx := context.Background()

	// Convert environment map to string slice
	envVars := []string{}
	for k, v := range instance.Environment {
		envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
	}
	envVars = append(envVars, "COLONIES_DEPLOYMENT="+blueprint.Metadata.Name)
	envVars = append(envVars, "COLONIES_CONTAINER_NAME="+containerName)

	// If this instance is a colony executor (has COLONIES_EXECUTOR_NAME), add node metadata
	if _, hasExecutorName := instance.Environment["COLONIES_EXECUTOR_NAME"]; hasExecutorName {
		nodeEnvVars := getNodeEnvVars()
		envVars = append(envVars, nodeEnvVars...)
		log.WithField("ContainerName", containerName).Debug("Added node metadata for executor instance")
	}

	// Create container config
	config := &container.Config{
		Image: instance.Image,
		Env:   envVars,
		Labels: map[string]string{
			"colonies.deployment": blueprint.Metadata.Name,
			"colonies.managed":    "true",
			"colonies.generation": fmt.Sprintf("%d", blueprint.Metadata.Generation),
		},
	}

	// Add custom labels if provided
	if instance.Labels != nil {
		for k, v := range instance.Labels {
			config.Labels[k] = v
		}
	}

	// Override command if specified
	if len(instance.Command) > 0 {
		config.Cmd = instance.Command
	}

	// Override args if specified (append to Cmd)
	if len(instance.Args) > 0 {
		config.Cmd = append(config.Cmd, instance.Args...)
	}

	// Set working directory
	if instance.WorkingDir != "" {
		config.WorkingDir = instance.WorkingDir
	}

	// Set user
	if instance.User != "" {
		config.User = instance.User
	}

	// Set hostname
	if instance.Hostname != "" {
		config.Hostname = instance.Hostname
	}

	// Configure ports
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, port := range instance.Ports {
		protocol := "tcp"
		if port.Protocol != "" {
			protocol = port.Protocol
		}

		natPort := nat.Port(fmt.Sprintf("%d/%s", port.Container, protocol))
		exposedPorts[natPort] = struct{}{}

		if port.Host > 0 {
			portBindings[natPort] = []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: fmt.Sprintf("%d", port.Host),
				},
			}
		}
	}
	config.ExposedPorts = exposedPorts

	// Create host config
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Privileged:   instance.Privileged,
	}

	// Set restart policy if specified
	if instance.RestartPolicy != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(instance.RestartPolicy),
		}
	}

	// Configure volumes
	binds := []string{}
	for _, vol := range instance.Volumes {
		switch vol.Type {
		case "bind":
			bind := fmt.Sprintf("%s:%s", vol.HostPath, vol.MountPath)
			if vol.ReadOnly {
				bind += ":ro"
			}
			binds = append(binds, bind)
		case "named":
			bind := fmt.Sprintf("%s:%s", vol.Name, vol.MountPath)
			if vol.ReadOnly {
				bind += ":ro"
			}
			binds = append(binds, bind)
		}
	}
	hostConfig.Binds = binds

	// Set resource limits if specified
	if instance.Resources != nil {
		// CPU limits would go here (not implemented in this version)
		// Memory limits would go here (not implemented in this version)
	}

	// Network configuration - use blueprint name as network alias
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"colonies_default": {
				Aliases: []string{blueprint.Metadata.Name, containerName},
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
		"Image":         instance.Image,
	}).Info("Docker deployment instance started and running")

	return nil
}
