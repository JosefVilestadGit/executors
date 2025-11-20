package reconciler

import (
	"fmt"
	"os"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/common/pkg/docker"
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

// CreateReconciler creates a new Reconciler instance
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
