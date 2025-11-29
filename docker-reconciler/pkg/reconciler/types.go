package reconciler

import (
	"sync"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/executors/common/pkg/docker"
)

// DeploymentSpec defines the specification for an executor deployment
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
	Command      []string               `json:"command,omitempty"`    // Override container command
	Args         []string               `json:"args,omitempty"`       // Override container args
	Entrypoint   []string               `json:"entrypoint,omitempty"` // Override container entrypoint
}

// PortSpec defines a port mapping
type PortSpec struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// VolumeSpec defines a volume mount
type VolumeSpec struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	ReadOnly  bool   `json:"readOnly"`
}

// DockerDeploymentSpec defines the specification for a Docker deployment
type DockerDeploymentSpec struct {
	Replicas  int                 `json:"replicas,omitempty"` // Number of replicas (optional, defaults to number of instances)
	Instances []ContainerInstance `json:"instances"`
	Network   *NetworkSpec        `json:"network,omitempty"`
}

// ContainerInstance defines a single container instance in a Docker deployment
type ContainerInstance struct {
	Name          string                `json:"name"`
	Type          string                `json:"type"`
	Image         string                `json:"image"`
	Command       []string              `json:"command,omitempty"`
	Args          []string              `json:"args,omitempty"`
	Environment   map[string]string     `json:"environment,omitempty"`
	EnvFile       string                `json:"envFile,omitempty"`
	Ports         []DockerPortSpec      `json:"ports,omitempty"`
	Volumes       []DockerVolumeSpec    `json:"volumes,omitempty"`
	DependsOn     []string              `json:"dependsOn,omitempty"`
	RestartPolicy string                `json:"restartPolicy,omitempty"`
	Healthcheck   *HealthcheckSpec      `json:"healthcheck,omitempty"`
	Resources     *ResourcesSpec        `json:"resources,omitempty"`
	Labels        map[string]string     `json:"labels,omitempty"`
	Privileged    bool                  `json:"privileged"`
	User          string                `json:"user,omitempty"`
	WorkingDir    string                `json:"workingDir,omitempty"`
	Hostname      string                `json:"hostname,omitempty"`
}

// DockerPortSpec defines a Docker port mapping
type DockerPortSpec struct {
	Container int    `json:"container"`
	Host      int    `json:"host,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

// DockerVolumeSpec defines a Docker volume mount
type DockerVolumeSpec struct {
	Type      string `json:"type"` // bind, named, tmpfs
	HostPath  string `json:"hostPath,omitempty"`
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly"`
}

// NetworkSpec defines a Docker network configuration
type NetworkSpec struct {
	Name   string `json:"name"`
	Create bool   `json:"create"`
	Driver string `json:"driver,omitempty"`
}

// HealthcheckSpec defines health check configuration
type HealthcheckSpec struct {
	Test        string `json:"test"`
	Interval    string `json:"interval"`
	Timeout     string `json:"timeout"`
	Retries     int    `json:"retries"`
	StartPeriod string `json:"startPeriod"`
}

// ResourcesSpec defines resource constraints
type ResourcesSpec struct {
	CPUs   string `json:"cpus"`
	Memory string `json:"memory"`
}

// Reconciler is the main reconciler struct that handles blueprint reconciliation
type Reconciler struct {
	dockerHandler  *docker.DockerHandler
	dockerClient   DockerClient // Now uses interface for testability
	client         *client.ColoniesClient
	executorPrvKey string
	colonyOwnerKey string
	colonyName     string
	location       string
	dockerNetwork  string     // Docker network name for deployed containers
	logMu          sync.Mutex // Mutex for synchronizing log writes
}
