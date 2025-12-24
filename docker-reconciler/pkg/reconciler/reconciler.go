package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
	"github.com/colonyos/executors/docker-reconciler/pkg/constants"
	"github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

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
	if host := os.Getenv("COLONIES_SERVER_HOST"); host != "" {
		defaults["COLONIES_SERVER_HOST"] = host
	}
	if port := os.Getenv("COLONIES_SERVER_PORT"); port != "" {
		defaults["COLONIES_SERVER_PORT"] = port
	}
	// Also inject the new HTTP client configuration for newer executors
	if host := os.Getenv("COLONIES_CLIENT_HTTP_HOST"); host != "" {
		defaults["COLONIES_CLIENT_HTTP_HOST"] = host
	} else if host := os.Getenv("COLONIES_SERVER_HOST"); host != "" {
		defaults["COLONIES_CLIENT_HTTP_HOST"] = host
	}
	if port := os.Getenv("COLONIES_CLIENT_HTTP_PORT"); port != "" {
		defaults["COLONIES_CLIENT_HTTP_PORT"] = port
	} else if port := os.Getenv("COLONIES_SERVER_PORT"); port != "" {
		defaults["COLONIES_CLIENT_HTTP_PORT"] = port
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

	// Hardware defaults required by docker-executor (strconv.Atoi requires non-empty values)
	// These can be overridden by blueprint env vars for deployments with specific hardware
	defaults["EXECUTOR_HW_NODES"] = "1"
	defaults["EXECUTOR_HW_GPU_COUNT"] = "0"
	defaults["EXECUTOR_HW_GPU_NODES_COUNT"] = "0"
	defaults["EXECUTOR_LOCATION_LONG"] = "0"
	defaults["EXECUTOR_LOCATION_LAT"] = "0"

	// Software version defaults (will be overridden with actual image)
	defaults["EXECUTOR_SW_TYPE"] = "docker"

	return defaults
}

// CreateReconciler creates a new Reconciler instance
func CreateReconciler(client *client.ColoniesClient, executorPrvKey, colonyOwnerKey, colonyName, executorName, location string) (*Reconciler, error) {
	dockerHandler, err := docker.CreateDockerHandler()
	if err != nil {
		return nil, err
	}

	dockerCli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	// Get docker network from environment, default to "colonies_default"
	dockerNetwork := os.Getenv("DOCKER_NETWORK")
	if dockerNetwork == "" {
		dockerNetwork = "colonies_default"
	}

	// Ensure the docker network exists
	if err := ensureNetworkExists(dockerCli, dockerNetwork); err != nil {
		return nil, fmt.Errorf("failed to ensure docker network exists: %w", err)
	}

	return &Reconciler{
		dockerHandler:  dockerHandler,
		dockerClient:   dockerCli,
		client:         client,
		executorPrvKey: executorPrvKey,
		colonyOwnerKey: colonyOwnerKey,
		colonyName:     colonyName,
		executorName:   executorName,
		location:       location,
		dockerNetwork:  dockerNetwork,
	}, nil
}

// ensureNetworkExists creates the docker network if it doesn't exist
func ensureNetworkExists(dockerCli *dockerclient.Client, networkName string) error {
	ctx := context.Background()

	log.WithField("Network", networkName).Info("Checking if docker network exists")

	// Check if network exists
	networks, err := dockerCli.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	for _, n := range networks {
		if n.Name == networkName {
			log.WithField("Network", networkName).Info("Docker network already exists")
			return nil
		}
	}

	// Create the network
	log.WithField("Network", networkName).Info("Creating docker network")
	_, err = dockerCli.NetworkCreate(ctx, networkName, types.NetworkCreate{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	log.WithField("Network", networkName).Info("Created docker network")
	return nil
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

// ForceReconcile forces recreation of all containers for a blueprint
// This is useful when you want to restart containers with a new image (same tag)
func (r *Reconciler) ForceReconcile(process *core.Process, blueprint *core.Blueprint) error {
	log.WithFields(log.Fields{
		"ResourceName": blueprint.Metadata.Name,
		"ResourceKind": blueprint.Kind,
	}).Info("Starting FORCE reconciliation - will recreate all containers")

	r.addLog(process, fmt.Sprintf("Force reconciling %s - will pull fresh image and recreate all containers", blueprint.Metadata.Name))

	// Extract image(s) from blueprint and force pull them first
	// Use shorter timeout for force reconcile to fail fast and preserve service availability
	images, err := r.extractImagesFromBlueprint(blueprint)
	if err != nil {
		r.addLog(process, fmt.Sprintf("ERROR: Failed to extract images from blueprint: %v", err))
		return fmt.Errorf("failed to extract images from blueprint: %w", err)
	}

	// Pull all images BEFORE removing any containers
	// This ensures we don't remove containers if images cannot be pulled
	r.addLog(process, fmt.Sprintf("Pulling %d image(s) with %s timeout...", len(images), constants.ForceReconcileImagePullTimeout))
	for _, image := range images {
		if err := r.forcePullImageWithTimeout(process, image, constants.ForceReconcileImagePullTimeout); err != nil {
			r.addLog(process, fmt.Sprintf("ERROR: Failed to pull image %s: %v - aborting force reconcile to preserve service availability", image, err))
			return fmt.Errorf("failed to pull image %s (aborting to preserve running containers): %w", image, err)
		}
		r.addLog(process, fmt.Sprintf("Successfully pulled image: %s", image))
	}

	r.addLog(process, "All images pulled successfully, proceeding to recreate containers...")

	// Get all existing containers for this blueprint
	existingContainers, err := r.listContainersByLabel(blueprint.Metadata.Name)
	if err != nil {
		r.addLog(process, fmt.Sprintf("Warning: Failed to list existing containers: %v", err))
		existingContainers = []string{}
	}

	r.addLog(process, fmt.Sprintf("Found %d existing container(s) to recreate", len(existingContainers)))

	// Stop and remove all existing containers (deregistering executors if needed)
	for _, containerID := range existingContainers {
		if err := r.forceRemoveContainer(process, containerID, blueprint); err != nil {
			r.addLog(process, fmt.Sprintf("Warning: Failed to remove container %s: %v", truncateID(containerID, 12), err))
		}
	}

	// Now do a normal reconciliation to bring containers back up
	r.addLog(process, "Starting containers with fresh image...")
	return r.Reconcile(process, blueprint)
}

// extractImagesFromBlueprint extracts container image names from a blueprint spec
func (r *Reconciler) extractImagesFromBlueprint(blueprint *core.Blueprint) ([]string, error) {
	var images []string

	specBytes, err := json.Marshal(blueprint.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	switch blueprint.Kind {
	case "ExecutorDeployment":
		var spec DeploymentSpec
		if err := json.Unmarshal(specBytes, &spec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ExecutorDeployment spec: %w", err)
		}
		if spec.Image != "" {
			images = append(images, spec.Image)
		}

	case "DockerDeployment":
		var spec DockerDeploymentSpec
		if err := json.Unmarshal(specBytes, &spec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal DockerDeployment spec: %w", err)
		}
		for _, instance := range spec.Instances {
			if instance.Image != "" {
				images = append(images, instance.Image)
			}
		}

	default:
		return nil, fmt.Errorf("unsupported blueprint kind: %s", blueprint.Kind)
	}

	return images, nil
}
