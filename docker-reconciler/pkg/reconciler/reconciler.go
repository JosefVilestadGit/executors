package reconciler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
)

type DeploymentSpec struct {
	Image        string                 `json:"image"`
	Replicas     int                    `json:"replicas"`
	ExecutorType string                 `json:"executorType"`
	ExecutorName string                 `json:"executorName,omitempty"` // Target reconciler name (optional - if not set, any reconciler can handle)
	CPU          string                 `json:"cpu"`
	Memory       string                 `json:"memory"`
	Env          map[string]interface{} `json:"env"`
	Ports        []PortSpec             `json:"ports"`
	Volumes      []VolumeSpec           `json:"volumes"`
	Privileged   bool                   `json:"privileged"`
	Command      []string               `json:"command,omitempty"`      // Override container command
	Args         []string               `json:"args,omitempty"`         // Override container args
	Entrypoint   []string               `json:"entrypoint,omitempty"`   // Override container entrypoint
}

type PortSpec struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type VolumeSpec struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	ReadOnly  bool   `json:"readOnly"`
}

// DockerDeployment types
type DockerDeploymentSpec struct {
	Instances []ContainerInstance    `json:"instances"`
	Network   *NetworkSpec           `json:"network,omitempty"`
}

type ContainerInstance struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Image         string                 `json:"image"`
	Command       []string               `json:"command,omitempty"`
	Args          []string               `json:"args,omitempty"`
	Environment   map[string]string      `json:"environment,omitempty"`
	EnvFile       string                 `json:"envFile,omitempty"`
	Ports         []DockerPortSpec       `json:"ports,omitempty"`
	Volumes       []DockerVolumeSpec     `json:"volumes,omitempty"`
	DependsOn     []string               `json:"dependsOn,omitempty"`
	RestartPolicy string                 `json:"restartPolicy,omitempty"`
	Healthcheck   *HealthcheckSpec       `json:"healthcheck,omitempty"`
	Resources     *ResourcesSpec         `json:"resources,omitempty"`
	Labels        map[string]string      `json:"labels,omitempty"`
	Privileged    bool                   `json:"privileged"`
	User          string                 `json:"user,omitempty"`
	WorkingDir    string                 `json:"workingDir,omitempty"`
	Hostname      string                 `json:"hostname,omitempty"`
}

type DockerPortSpec struct {
	Container int    `json:"container"`
	Host      int    `json:"host,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type DockerVolumeSpec struct {
	Type      string `json:"type"`          // bind, named, tmpfs
	HostPath  string `json:"hostPath,omitempty"`
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly"`
}

type NetworkSpec struct {
	Name   string `json:"name"`
	Create bool   `json:"create"`
	Driver string `json:"driver,omitempty"`
}

type HealthcheckSpec struct {
	Test        string `json:"test"`
	Interval    string `json:"interval"`
	Timeout     string `json:"timeout"`
	Retries     int    `json:"retries"`
	StartPeriod string `json:"startPeriod"`
}

type ResourcesSpec struct {
	CPUs   string `json:"cpus"`
	Memory string `json:"memory"`
}

type Reconciler struct {
	dockerHandler  *docker.DockerHandler
	dockerClient   *dockerclient.Client
	client         *client.ColoniesClient
	executorPrvKey string
}

// getNodeEnvVars returns environment variables with node metadata for containers
func getNodeEnvVars() []string {
	// Use COLONIES_NODE_NAME if set, otherwise fall back to hostname
	nodeName := os.Getenv("COLONIES_NODE_NAME")
	if nodeName == "" {
		hostname, _ := os.Hostname()
		nodeName = hostname
	}

	location := os.Getenv("COLONIES_NODE_LOCATION")
	if location == "" {
		location = "default"
	}

	envVars := []string{
		"COLONIES_NODE_NAME=" + nodeName,
		"COLONIES_NODE_LOCATION=" + location,
	}

	return envVars
}

func CreateReconciler(client *client.ColoniesClient, executorPrvKey string) (*Reconciler, error) {
	dockerHandler, err := docker.CreateDockerHandler()
	if err != nil {
		return nil, err
	}

	dockerCli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &Reconciler{
		dockerHandler:  dockerHandler,
		dockerClient:   dockerCli,
		client:         client,
		executorPrvKey: executorPrvKey,
	}, nil
}

// generateUniqueHash generates a random 5-character alphanumeric hash
// With 5 alphanumeric characters (62^5 = ~916 million combinations),
// we can safely support 100K+ executors with very low collision probability
func generateUniqueHash() string {
	// Generate 4 random bytes (we'll use 5 chars from base62 encoding)
	bytes := make([]byte, 4)
	rand.Read(bytes)

	// Convert to hex and take first 5 characters
	hash := hex.EncodeToString(bytes)
	if len(hash) > 5 {
		hash = hash[:5]
	}

	return hash
}

// isExecutorNameTaken checks if an executor with the given name already exists in the colony
func (r *Reconciler) isExecutorNameTaken(colonyName, executorName string) (bool, error) {
	// Try to get the executor from colonies server
	executor, err := r.client.GetExecutor(colonyName, executorName, r.executorPrvKey)
	if err != nil {
		// If error is "not found", name is available
		return false, nil
	}
	// If we got an executor, name is taken
	return executor != nil, nil
}

// generateUniqueExecutorName generates a unique executor name with hash suffix
// It will retry up to 10 times to find an available name
func (r *Reconciler) generateUniqueExecutorName(colonyName, baseExecutorName string) (string, error) {
	const maxRetries = 10

	for i := 0; i < maxRetries; i++ {
		hash := generateUniqueHash()
		executorName := fmt.Sprintf("%s-%s", baseExecutorName, hash)

		taken, err := r.isExecutorNameTaken(colonyName, executorName)
		if err != nil {
			log.WithFields(log.Fields{"Error": err, "ExecutorName": executorName}).Warn("Failed to check if executor name is taken")
			continue
		}

		if !taken {
			return executorName, nil
		}

		log.WithFields(log.Fields{"ExecutorName": executorName}).Debug("Executor name collision, retrying...")
	}

	return "", fmt.Errorf("failed to generate unique executor name after %d retries", maxRetries)
}

// Reconcile processes a blueprint and ensures the desired state
func (r *Reconciler) Reconcile(process *core.Process, blueprint *core.Blueprint) error {
	log.WithFields(log.Fields{
		"ResourceName": blueprint.Metadata.Name,
		"ResourceKind": blueprint.Kind,
	}).Info("Starting reconciliation")

	// Branch based on blueprint kind
	switch blueprint.Kind {
	case "ExecutorDeployment":
		return r.reconcileExecutorDeployment(process, blueprint)
	case "DockerDeployment":
		return r.reconcileDockerDeployment(process, blueprint)
	default:
		return fmt.Errorf("unsupported blueprint kind: %s", blueprint.Kind)
	}
}

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
			// Get container name before removing
			inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
			if err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to inspect dirty container %s: %v", containerID, err))
				continue
			}

			containerName := inspect.Name
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}

			// Stop and remove the dirty container
			r.addLog(process, fmt.Sprintf("Removing dirty container: %s (generation mismatch)", containerName))
			if err := r.stopAndRemoveContainer(containerID); err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to remove dirty container %s: %v", containerID, err))
				continue
			}

			// Recreate the container with new spec and generation
			if err := r.startContainer(process, spec, containerName, blueprint); err != nil {
				r.addLog(process, fmt.Sprintf("Error recreating container %s: %v", containerName, err))
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

	currentReplicas := len(existingContainers)
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
				uniqueExecutorName, err := r.generateUniqueExecutorName(blueprint.Metadata.Namespace, blueprint.Metadata.Name)
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
				r.addLog(process, fmt.Sprintf("Error starting container %s: %v", containerName, err))
				return fmt.Errorf("failed to start container %s: %w", containerName, err)
			}
			r.addLog(process, fmt.Sprintf("Started container: %s", containerName))
		}
	} else if currentReplicas > spec.Replicas {
		// Scale down - stop excess containers
		containersToStop := currentReplicas - spec.Replicas
		r.addLog(process, fmt.Sprintf("Scaling down: stopping %d container(s)", containersToStop))

		for i := 0; i < containersToStop && i < len(existingContainers); i++ {
			containerID := existingContainers[i]
			if err := r.stopContainer(containerID); err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to stop container %s: %v", containerID, err))
			} else {
				r.addLog(process, fmt.Sprintf("Stopped container: %s", containerID))
			}
		}
	} else {
		r.addLog(process, "Deployment is at desired state")
	}

	r.addLog(process, "Reconciliation completed successfully")
	return nil
}

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

	r.addLog(process, fmt.Sprintf("Reconciling DockerDeployment: %s with %d instance(s)", blueprint.Metadata.Name, len(spec.Instances)))

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

	// Process each instance
	for _, instance := range spec.Instances {
		// Validate instance
		if instance.Name == "" {
			return fmt.Errorf("instance name is required")
		}
		if instance.Image == "" {
			return fmt.Errorf("instance image is required for %s", instance.Name)
		}

		r.addLog(process, fmt.Sprintf("Processing instance: %s (image: %s)", instance.Name, instance.Image))

		// Pull the image
		if err := r.pullImage(process, instance.Image); err != nil {
			return fmt.Errorf("failed to pull image for instance %s: %w", instance.Name, err)
		}

		// Check if container already exists
		existingContainerID, exists := existingContainersByName[instance.Name]

		if exists {
			// Check if container is dirty (generation mismatch)
			inspect, err := r.dockerClient.ContainerInspect(context.Background(), existingContainerID)
			if err != nil {
				r.addLog(process, fmt.Sprintf("Warning: Failed to inspect container %s: %v", instance.Name, err))
			} else {
				// Check generation
				generationStr, hasLabel := inspect.Config.Labels["colonies.generation"]
				isDirty := false

				if !hasLabel {
					isDirty = true
					r.addLog(process, fmt.Sprintf("Container %s has no generation label, recreating", instance.Name))
				} else {
					var containerGeneration int64
					if _, err := fmt.Sscanf(generationStr, "%d", &containerGeneration); err != nil {
						isDirty = true
						r.addLog(process, fmt.Sprintf("Container %s has invalid generation label, recreating", instance.Name))
					} else if containerGeneration < blueprint.Metadata.Generation {
						isDirty = true
						r.addLog(process, fmt.Sprintf("Container %s has outdated generation (%d < %d), recreating",
							instance.Name, containerGeneration, blueprint.Metadata.Generation))
					}
				}

				if isDirty {
					// Remove dirty container
					if err := r.stopAndRemoveContainer(existingContainerID); err != nil {
						return fmt.Errorf("failed to remove dirty container %s: %w", instance.Name, err)
					}
					// Create new container
					if err := r.startDockerDeploymentInstance(process, instance, blueprint); err != nil {
						return fmt.Errorf("failed to start instance %s: %w", instance.Name, err)
					}
					r.addLog(process, fmt.Sprintf("Recreated container: %s with generation %d", instance.Name, blueprint.Metadata.Generation))
				} else {
					r.addLog(process, fmt.Sprintf("Container %s is up to date", instance.Name))
				}
			}
		} else {
			// Container doesn't exist, create it
			if err := r.startDockerDeploymentInstance(process, instance, blueprint); err != nil {
				return fmt.Errorf("failed to start instance %s: %w", instance.Name, err)
			}
			r.addLog(process, fmt.Sprintf("Created container: %s", instance.Name))
		}

		// Remove from map so we can track which containers to remove
		delete(existingContainersByName, instance.Name)
	}

	// Remove any containers that are no longer in the spec
	for containerName, containerID := range existingContainersByName {
		r.addLog(process, fmt.Sprintf("Removing obsolete container: %s", containerName))
		if err := r.stopAndRemoveContainer(containerID); err != nil {
			r.addLog(process, fmt.Sprintf("Warning: Failed to remove container %s: %v", containerName, err))
		}
	}

	r.addLog(process, "Docker deployment reconciliation completed successfully")
	return nil
}

// CollectStatus gathers current status of instances for a blueprint
func (r *Reconciler) CollectStatus(blueprint *core.Blueprint) (map[string]interface{}, error) {
	// Get list of containers for this deployment
	containerIDs, err := r.listContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	instances := make([]map[string]interface{}, 0)
	running := 0
	stopped := 0

	for _, containerID := range containerIDs {
		inspect, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
		if err != nil {
			log.WithError(err).WithField("containerID", containerID).Warn("Failed to inspect container")
			continue
		}

		state := "stopped"
		if inspect.State.Running {
			state = "running"
			running++
		} else {
			stopped++
		}

		// Extract container name (remove leading "/")
		containerName := inspect.Name
		if len(containerName) > 0 && containerName[0] == '/' {
			containerName = containerName[1:]
		}

		instances = append(instances, map[string]interface{}{
			"id":        inspect.ID[:12], // Short ID
			"name":      containerName,
			"type":      "container",
			"state":     state,
			"created":   inspect.Created,
			"image":     inspect.Config.Image,
			"lastCheck": time.Now().Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"instances":        instances,
		"runningInstances": running,
		"stoppedInstances": stopped,
		"totalInstances":   len(instances),
		"lastUpdated":      time.Now().Format(time.RFC3339),
	}, nil
}

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

	// Convert env map to string slice
	envVars := []string{}
	for k, v := range spec.Env {
		envVars = append(envVars, fmt.Sprintf("%s=%v", k, v))
	}
	envVars = append(envVars, "COLONIES_DEPLOYMENT="+blueprint.Metadata.Name)
	envVars = append(envVars, "COLONIES_CONTAINER_NAME="+containerName)

	// If this is a colony executor deployment, use the container name as executor name
	// (the container name is now the unique executor name generated in Reconcile())
	if blueprint.Kind == "ExecutorDeployment" {
		envVars = append(envVars, "COLONIES_EXECUTOR_NAME="+containerName)

		// Add node metadata environment variables for executor registration
		nodeEnvVars := getNodeEnvVars()
		envVars = append(envVars, nodeEnvVars...)

		log.WithFields(log.Fields{
			"BlueprintName":   blueprint.Metadata.Name,
			"ExecutorName":    containerName,
			"ContainerName":   containerName,
		}).Info("Using container name as executor name")
		r.addLog(process, fmt.Sprintf("Executor name: %s", containerName))
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
			"colonies_default": {
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

	log.WithFields(log.Fields{
		"ContainerID":   resp.ID,
		"ContainerName": containerName,
		"Image":         spec.Image,
	}).Info("Container started")

	return nil
}

// startDockerDeploymentInstance creates and starts a container from a ContainerInstance spec
func (r *Reconciler) startDockerDeploymentInstance(process *core.Process, instance ContainerInstance, blueprint *core.Blueprint) error {
	ctx := context.Background()
	containerName := instance.Name

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

	log.WithFields(log.Fields{
		"ContainerID":   resp.ID,
		"ContainerName": containerName,
		"Image":         instance.Image,
	}).Info("Docker deployment instance started")

	return nil
}

func (r *Reconciler) listContainersByLabel(deploymentName string) ([]string, error) {
	ctx := context.Background()

	// List all containers with the deployment label
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "colonies.deployment="+deploymentName)

	containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
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

func (r *Reconciler) stopContainer(containerID string) error {
	ctx := context.Background()
	timeout := 10 // seconds
	return r.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})
}

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
				"ContainerID":          containerID,
				"ContainerGeneration":  containerGeneration,
				"CurrentGeneration":    currentGeneration,
			}).Info("Container has outdated generation, marking as dirty")
			dirtyContainers = append(dirtyContainers, containerID)
		}
	}

	return dirtyContainers, nil
}

func (r *Reconciler) addLog(process *core.Process, message string) {
	log.Info(message)
	err := r.client.AddLog(process.ID, message+"\n", r.executorPrvKey)
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("Failed to add log to process")
	}
}

// GetDeploymentStatus returns the current status of containers for a deployment
func (r *Reconciler) GetDeploymentStatus(deploymentName string) (map[string]interface{}, error) {
	containers, err := r.listContainersByLabel(deploymentName)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	status := map[string]interface{}{
		"deployment":      deploymentName,
		"currentReplicas": len(containers),
		"containerIDs":    containers,
	}

	return status, nil
}
