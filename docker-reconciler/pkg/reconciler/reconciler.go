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
	"github.com/colonyos/colonies/pkg/security/crypto"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
)

// truncateID safely truncates a container ID to the specified length
// If the ID is shorter than the length, returns the full ID
func truncateID(id string, length int) string {
	if len(id) <= length {
		return id
	}
	return id[:length]
}

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
	dockerClient   DockerClient // Now uses interface for testability
	client         *client.ColoniesClient
	executorPrvKey string
	colonyOwnerKey string
	colonyName     string
	location       string
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

// getDefaultEnvVars returns default environment variables that are auto-injected into containers
// User-provided env vars in the blueprint will override these defaults
func (r *Reconciler) getDefaultEnvVars(executorType string) map[string]string {
	defaults := make(map[string]string)

	// Colonies connection settings (inherit from reconciler)
	if backends := os.Getenv("COLONIES_CLIENT_BACKENDS"); backends != "" {
		defaults["COLONIES_CLIENT_BACKENDS"] = backends
	}
	if host := os.Getenv("COLONIES_CLIENT_HTTP_HOST"); host != "" {
		defaults["COLONIES_CLIENT_HTTP_HOST"] = host
	}
	if port := os.Getenv("COLONIES_CLIENT_HTTP_PORT"); port != "" {
		defaults["COLONIES_CLIENT_HTTP_PORT"] = port
	}
	if insecure := os.Getenv("COLONIES_CLIENT_HTTP_INSECURE"); insecure != "" {
		defaults["COLONIES_CLIENT_HTTP_INSECURE"] = insecure
	}
	if host := os.Getenv("COLONIES_SERVER_HOST"); host != "" {
		defaults["COLONIES_SERVER_HOST"] = host
	}
	if port := os.Getenv("COLONIES_SERVER_PORT"); port != "" {
		defaults["COLONIES_SERVER_PORT"] = port
	}
	if tls := os.Getenv("COLONIES_SERVER_TLS"); tls != "" {
		defaults["COLONIES_SERVER_TLS"] = tls
	}
	if tls := os.Getenv("COLONIES_TLS"); tls != "" {
		defaults["COLONIES_TLS"] = tls
	}
	defaults["COLONIES_COLONY_NAME"] = r.colonyName

	// S3/MinIO configuration (if available in reconciler environment)
	if tls := os.Getenv("AWS_S3_TLS"); tls != "" {
		defaults["AWS_S3_TLS"] = tls
	}
	if skip := os.Getenv("AWS_S3_SKIPVERIFY"); skip != "" {
		defaults["AWS_S3_SKIPVERIFY"] = skip
	}
	if endpoint := os.Getenv("AWS_S3_ENDPOINT"); endpoint != "" {
		defaults["AWS_S3_ENDPOINT"] = endpoint
	}
	if accessKey := os.Getenv("AWS_S3_ACCESSKEY"); accessKey != "" {
		defaults["AWS_S3_ACCESSKEY"] = accessKey
	}
	if secretKey := os.Getenv("AWS_S3_SECRETKEY"); secretKey != "" {
		defaults["AWS_S3_SECRETKEY"] = secretKey
	}
	if region := os.Getenv("AWS_S3_REGION_KEY"); region != "" {
		defaults["AWS_S3_REGION_KEY"] = region
	}
	if bucket := os.Getenv("AWS_S3_BUCKET"); bucket != "" {
		defaults["AWS_S3_BUCKET"] = bucket
	}

	// Locale/Timezone defaults
	defaults["LANG"] = "en_US.UTF-8"
	defaults["LANGUAGE"] = "en_US.UTF-8"
	defaults["LC_ALL"] = "en_US.UTF-8"
	defaults["LC_CTYPE"] = "UTF-8"
	if tz := os.Getenv("TZ"); tz != "" {
		defaults["TZ"] = tz
	} else {
		defaults["TZ"] = "Europe/Stockholm"
	}

	// Executor type and metadata defaults
	defaults["EXECUTOR_TYPE"] = executorType
	defaults["EXECUTOR_ADD_DEBUG_LOGS"] = "false"
	defaults["EXECUTOR_FS_DIR"] = "/tmp/colonies"

	// Hardware defaults
	defaults["EXECUTOR_GPU"] = "0"
	defaults["EXECUTOR_HW_MODEL"] = "n/a"
	defaults["EXECUTOR_HW_NODES"] = "1"
	defaults["EXECUTOR_HW_CPU"] = ""
	defaults["EXECUTOR_HW_MEM"] = ""
	defaults["EXECUTOR_HW_STORAGE"] = ""
	defaults["EXECUTOR_HW_GPU_COUNT"] = "0"
	defaults["EXECUTOR_HW_GPU_MEM"] = ""
	defaults["EXECUTOR_HW_GPU_NODES_COUNT"] = "0"
	defaults["EXECUTOR_HW_GPU_NAME"] = ""

	// Location defaults
	defaults["EXECUTOR_LOCATION_LONG"] = ""
	defaults["EXECUTOR_LOCATION_LAT"] = ""
	defaults["EXECUTOR_LOCATION_DESC"] = "n/a"

	// Software version defaults (will be overridden with actual image)
	defaults["EXECUTOR_SW_TYPE"] = "docker"

	return defaults
}

func CreateReconciler(client *client.ColoniesClient, executorPrvKey, colonyOwnerKey, colonyName, location string) (*Reconciler, error) {
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
		colonyOwnerKey: colonyOwnerKey,
		colonyName:     colonyName,
		location:       location,
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

			// With generation-based naming, the old executor needs to be deregistered
			// because the new container will have a different executor name (with new generation number)

			// Extract the old executor name from the container's environment
			containerInfo, err := r.dockerClient.ContainerInspect(context.Background(), containerID)
			if err == nil {
				oldExecutorName := ""
				for _, env := range containerInfo.Config.Env {
					if strings.HasPrefix(env, "COLONIES_EXECUTOR_NAME=") {
						oldExecutorName = strings.TrimPrefix(env, "COLONIES_EXECUTOR_NAME=")
						break
					}
				}

				if oldExecutorName != "" {
					r.addLog(process, fmt.Sprintf("Deregistering old executor: %s", oldExecutorName))
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
				// Executor name includes generation suffix (e.g., "docker-executor-abc-5")
				executorName := fmt.Sprintf("%s-%d", containerName, blueprint.Metadata.Generation)

				log.WithFields(log.Fields{
					"ExecutorName": executorName,
					"ContainerID":  truncateID(containerID, 12),
				}).Info("Deregistering executor before stopping container")

				if err := r.client.RemoveExecutor(r.colonyName, executorName, r.colonyOwnerKey); err != nil {
					log.WithFields(log.Fields{
						"Error":        err,
						"ExecutorName": executorName,
					}).Warn("Failed to deregister executor")
					r.addLog(process, fmt.Sprintf("Warning: Failed to deregister executor %s: %v", executorName, err))
					// Continue anyway to stop the container
				} else {
					r.addLog(process, fmt.Sprintf("Deregistered executor: %s", executorName))
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
	if err := r.CleanupOldGenerationContainers(blueprint); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup old generation containers")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup old generation containers: %v", err))
	}

	// Cleanup stopped containers and stale executor registrations
	r.addLog(process, "Running cleanup of stopped containers and stale executors...")
	if err := r.CleanupStoppedContainers(); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup stopped containers")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup stopped containers: %v", err))
	}

	if err := r.CleanupStaleExecutors(blueprint.Metadata.Name, spec.ExecutorType); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup stale executors")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup stale executors: %v", err))
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

	// Cleanup stopped containers (don't cleanup stale executors for DockerDeployment as they might not be executors)
	r.addLog(process, "Running cleanup of stopped containers...")
	if err := r.CleanupStoppedContainers(); err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to cleanup stopped containers")
		r.addLog(process, fmt.Sprintf("Warning: Failed to cleanup stopped containers: %v", err))
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
			"id":        truncateID(inspect.ID, 12), // Short ID
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
		executor := core.CreateExecutor(executorID, executorType, executorName, r.colonyName, time.Now(), time.Now())

		// Set location from reconciler (inherits from parent)
		executor.Location = core.Location{Description: r.location}

		addedExecutor, err := r.client.AddExecutor(executor, r.colonyOwnerKey)
		if err != nil {
			return fmt.Errorf("failed to register executor %s: %w", containerName, err)
		}

		// Auto-approve the executor (reconciler has colony owner privileges)
		err = r.client.ApproveExecutor(r.colonyName, executorName, r.colonyOwnerKey)
		if err != nil {
			return fmt.Errorf("failed to approve executor %s: %w", containerName, err)
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
			"BlueprintName":   blueprint.Metadata.Name,
			"ExecutorName":    executorName,
			"ContainerName":   containerName,
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

// CleanupStoppedContainers removes all stopped/exited containers managed by the reconciler
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
				"ContainerID":    truncateID(cont.ID, 12),
				"GenerationStr":  generationStr,
				"Error":          err,
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
