package reconciler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

type DeploymentSpec struct {
	Image        string                 `json:"image"`
	Replicas     int                    `json:"replicas"`
	ExecutorType string                 `json:"executorType"`
	ExecutorName string                 `json:"executorName,omitempty"` // Base executor name for colony executors
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

type Reconciler struct {
	dockerHandler  *docker.DockerHandler
	dockerClient   *dockerclient.Client
	client         *client.ColoniesClient
	executorPrvKey string
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

// Reconcile processes an ExecutorDeployment resource and ensures the desired state
func (r *Reconciler) Reconcile(process *core.Process, resource *core.Resource) error {
	log.WithFields(log.Fields{
		"ResourceName": resource.Metadata.Name,
		"ResourceKind": resource.Kind,
	}).Info("Starting reconciliation")

	// Parse the deployment spec
	var spec DeploymentSpec
	specBytes, err := json.Marshal(resource.Spec)
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
	r.addLog(process, fmt.Sprintf("Reconciling ExecutorDeployment: %s", resource.Metadata.Name))
	r.addLog(process, fmt.Sprintf("Image: %s, Replicas: %d", spec.Image, spec.Replicas))

	// Pull the container image
	if err := r.pullImage(process, spec.Image); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	// Get currently running containers for this deployment
	existingContainers, err := r.listContainersByLabel(resource.Metadata.Name)
	if err != nil {
		r.addLog(process, fmt.Sprintf("Warning: Failed to list existing containers: %v", err))
		existingContainers = []string{} // Continue with empty list
	}

	currentReplicas := len(existingContainers)
	r.addLog(process, fmt.Sprintf("Current replicas: %d, Desired replicas: %d", currentReplicas, spec.Replicas))

	// Scale up or down
	if currentReplicas < spec.Replicas {
		// Scale up - start new containers
		containersToStart := spec.Replicas - currentReplicas
		r.addLog(process, fmt.Sprintf("Scaling up: starting %d new container(s)", containersToStart))

		for i := 0; i < containersToStart; i++ {
			containerName := fmt.Sprintf("%s-%d", resource.Metadata.Name, currentReplicas+i)
			if err := r.startContainer(process, spec, containerName, resource.Metadata.Name, resource.Metadata.Namespace); err != nil {
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

func (r *Reconciler) pullImage(process *core.Process, image string) error {
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

func (r *Reconciler) startContainer(process *core.Process, spec DeploymentSpec, containerName string, deploymentName string, colonyName string) error {
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
	envVars = append(envVars, "COLONIES_DEPLOYMENT="+deploymentName)
	envVars = append(envVars, "COLONIES_CONTAINER_NAME="+containerName)

	// If this is a colony executor deployment, generate a unique executor name
	if spec.ExecutorName != "" {
		uniqueExecutorName, err := r.generateUniqueExecutorName(colonyName, spec.ExecutorName)
		if err != nil {
			return fmt.Errorf("failed to generate unique executor name: %w", err)
		}
		envVars = append(envVars, "COLONIES_EXECUTOR_NAME="+uniqueExecutorName)
		log.WithFields(log.Fields{
			"BaseExecutorName":   spec.ExecutorName,
			"UniqueExecutorName": uniqueExecutorName,
			"ContainerName":      containerName,
		}).Info("Generated unique executor name")
		r.addLog(process, fmt.Sprintf("Generated unique executor name: %s", uniqueExecutorName))
	}

	// Create container config
	config := &container.Config{
		Image: spec.Image,
		Env:   envVars,
		Labels: map[string]string{
			"colonies.deployment": deploymentName,
			"colonies.managed":    "true",
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

	// Create network config to attach to colonies network
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"colonies_default": {},
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
