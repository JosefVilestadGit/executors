# Docker Reconciler

A ColonyOS executor that acts as a Kubernetes-style controller for managing Docker containers declaratively. It watches for blueprint changes (ExecutorDeployments and DockerDeployments) and reconciles the desired state with actual running containers.

## Table of Contents

- [Architecture](#architecture)
- [How It Works](#how-it-works)
- [Blueprint Types](#blueprint-types)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Usage Examples](#usage-examples)
- [Container Management](#container-management)
- [Node Registration](#node-registration)
- [Development](#development)
- [Troubleshooting](#troubleshooting)

## Architecture

### High-Level Overview

```mermaid
graph TB
    subgraph "ColonyOS Server"
        BS[Blueprint Store]
        PS[Process Scheduler]
    end

    subgraph "Docker Host"
        DR[Docker Reconciler<br/>Executor]
        DD[Docker Daemon]

        subgraph "Managed Containers"
            C1[Container 1]
            C2[Container 2]
            C3[Container N]
        end
    end

    User[User/CLI] -->|1. Create/Update<br/>Blueprint| BS
    BS -->|2. Trigger<br/>Reconciliation| PS
    PS -->|3. Assign Process| DR
    DR -->|4. Pull Images| DD
    DR -->|5. Reconcile State| DD
    DD -->|6. Manage| C1
    DD -->|6. Manage| C2
    DD -->|6. Manage| C3
    DR -->|7. Report Status| PS
    PS -->|8. Update Blueprint<br/>Status| BS

    style DR fill:#4CAF50
    style DD fill:#2196F3
    style BS fill:#FF9800
    style C1 fill:#9E9E9E
    style C2 fill:#9E9E9E
    style C3 fill:#9E9E9E
```

### Components

1. **ColonyOS Server**: Stores blueprints and triggers reconciliation processes
2. **Docker Reconciler**: Executor that watches for reconciliation processes and manages containers
3. **Docker Daemon**: Runs the actual containers
4. **Managed Containers**: Containers deployed and tracked by the reconciler

## How It Works

### Reconciliation Loop

```mermaid
sequenceDiagram
    participant User
    participant Server as ColonyOS Server
    participant Reconciler as Docker Reconciler
    participant Docker as Docker Daemon

    User->>Server: Create/Update Blueprint
    Note over Server: Blueprint stored with<br/>generation counter
    Server->>Server: Trigger reconciliation<br/>process
    Server->>Reconciler: Assign process with<br/>blueprint data

    Reconciler->>Docker: List existing containers<br/>(by label)
    Docker-->>Reconciler: Container list

    Reconciler->>Reconciler: Compare actual vs<br/>desired state

    alt Containers need update (generation mismatch)
        Reconciler->>Docker: Stop dirty containers
        Reconciler->>Docker: Start new containers<br/>with updated config
    end

    alt Scale up needed
        Reconciler->>Docker: Pull image (if needed)
        Reconciler->>Docker: Start new containers
    end

    alt Scale down needed
        Reconciler->>Docker: Stop excess containers
    end

    Reconciler->>Docker: Collect container status
    Docker-->>Reconciler: Container states
    Reconciler->>Server: Report status
    Server->>Server: Update blueprint status
```

### Generation-Based Updates

The reconciler uses a **generation counter** to track blueprint changes:

```mermaid
graph LR
    subgraph "Blueprint Update Flow"
        V1[Blueprint v1<br/>Generation: 1] -->|User updates| V2[Blueprint v2<br/>Generation: 2]
        V2 -->|User updates| V3[Blueprint v3<br/>Generation: 3]
    end

    subgraph "Container Labels"
        L1[Container A<br/>generation: 1<br/> Dirty]
        L2[Container B<br/>generation: 2<br/> Dirty]
        L3[Container C<br/>generation: 3<br/> Up to date]
    end

    V3 -.->|Reconcile| L1
    V3 -.->|Reconcile| L2
    V3 -.->|Reconcile| L3

    style L1 fill:#f44336
    style L2 fill:#f44336
    style L3 fill:#4CAF50
```

When a blueprint is updated:
1. Server increments the generation counter
2. Reconciler compares container labels with blueprint generation
3. Containers with outdated generation are **recreated** with new config
4. Containers with current generation are left running

## Blueprint Types

The reconciler supports two types of blueprints:

### 1. ExecutorDeployment

Simple container deployments with replica scaling.

```mermaid
graph TB
    subgraph "ExecutorDeployment Spec"
        ED[ExecutorDeployment]
        ED --> Image[image: nginx:latest]
        ED --> Replicas[replicas: 3]
        ED --> Env[env: KEY=VALUE]
        ED --> Type[executorType:<br/>deployment-controller]
    end

    subgraph "Resulting Containers"
        C1[nginx-deploy-0]
        C2[nginx-deploy-1]
        C3[nginx-deploy-2]
    end

    ED -.->|Creates| C1
    ED -.->|Creates| C2
    ED -.->|Creates| C3

    style ED fill:#4CAF50
    style C1 fill:#2196F3
    style C2 fill:#2196F3
    style C3 fill:#2196F3
```

**Use Case**: Simple services that need scaling (web servers, workers, etc.)

**Example**:
```json
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "nginx-deployment"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 3,
    "executorType": "deployment-controller",
    "env": {
      "ENVIRONMENT": "production"
    }
  }
}
```

### 2. DockerDeployment

Multi-container deployments with advanced configuration (like Docker Compose).

```mermaid
graph TB
    subgraph "DockerDeployment Spec"
        DD[DockerDeployment]
        DD --> I1[Instance 1:<br/>Database]
        DD --> I2[Instance 2:<br/>API Server]
        DD --> I3[Instance 3:<br/>Web Frontend]

        I1 --> V1[Volumes]
        I1 --> P1[Ports]
        I2 --> D2[DependsOn: db]
        I2 --> P2[Ports]
        I3 --> D3[DependsOn: api]
        I3 --> P3[Ports]
    end

    subgraph "Resulting Containers"
        C1[myapp-database]
        C2[myapp-api]
        C3[myapp-web]
    end

    I1 -.->|Creates| C1
    I2 -.->|Creates| C2
    I3 -.->|Creates| C3

    C1 --> C2
    C2 --> C3

    style DD fill:#FF9800
    style C1 fill:#673AB7
    style C2 fill:#673AB7
    style C3 fill:#673AB7
```

**Use Case**: Complex applications with multiple interdependent services

**Features**:
- Named container instances
- Volume mounts (bind, named, tmpfs)
- Port mappings
- Dependency ordering
- Health checks
- Resource limits
- Custom commands and entrypoints

**Example**:
```json
{
  "kind": "DockerDeployment",
  "metadata": {
    "name": "myapp"
  },
  "spec": {
    "instances": [
      {
        "name": "database",
        "type": "container",
        "image": "postgres:15",
        "environment": {
          "POSTGRES_PASSWORD": "secret"
        },
        "volumes": [
          {
            "type": "named",
            "name": "postgres-data",
            "mountPath": "/var/lib/postgresql/data"
          }
        ]
      },
      {
        "name": "api",
        "type": "container",
        "image": "myapp/api:latest",
        "dependsOn": ["database"],
        "ports": [
          {
            "container": 8080,
            "host": 8080
          }
        ]
      }
    ]
  }
}
```

## Quick Start

### Prerequisites

- Docker installed and running
- ColonyOS server running
- Colony private key

### Run with Docker Compose

```bash
# 1. Configure environment
export COLONIES_COLONY_PRVKEY="your-colony-private-key"

# 2. Start the reconciler
docker-compose up -d

# 3. Check logs
docker-compose logs -f
```

### Run Locally

```bash
# 1. Build
make build

# 2. Configure
export COLONIES_SERVER_HOST="localhost"
export COLONIES_SERVER_PORT="50080"
export COLONIES_INSECURE="true"
export COLONIES_COLONY_NAME="dev"
export COLONIES_COLONY_PRVKEY="your-colony-private-key"
export COLONIES_EXECUTOR_NAME="docker-reconciler-1"

# 3. Start
./bin/docker-reconciler start --verbose
```

## Configuration

### Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `COLONIES_SERVER_HOST` | ColonyOS server hostname | Yes | - |
| `COLONIES_SERVER_PORT` | ColonyOS server port | No | 443 |
| `COLONIES_INSECURE` | Use insecure HTTP (true/false) | No | false |
| `COLONIES_COLONY_NAME` | Colony name | Yes | - |
| `COLONIES_COLONY_PRVKEY` | Colony private key (for self-registration) | Yes* | - |
| `COLONIES_PRVKEY` | Executor private key (if pre-registered) | Yes* | - |
| `COLONIES_EXECUTOR_NAME` | Executor name | Yes | - |
| `COLONIES_EXECUTOR_TYPE` | Executor type | No | deployment-controller |
| `COLONIES_NODE_NAME` | Node name for registration | No | hostname |
| `COLONIES_NODE_LOCATION` | Node location | No | default |

\* Either `COLONIES_COLONY_PRVKEY` or `COLONIES_PRVKEY` is required

### Node Metadata

The reconciler automatically detects and registers node metadata:

```mermaid
graph LR
    subgraph "Auto-Detection"
        H[Hostname] --> NM[Node Metadata]
        OS[OS/Arch] --> NM
        CPU[CPU Cores] --> NM
        MEM[Memory] --> NM
        GPU[GPU Detection] --> NM
    end

    subgraph "Registration"
        NM --> ER[Executor<br/>Registration]
        ER --> NS[Node in<br/>Colonies Server]
    end

    style NM fill:#4CAF50
    style NS fill:#2196F3
```

Detected information:
- Platform (linux, darwin, windows)
- Architecture (amd64, arm64)
- CPU cores
- Total memory (MB)
- GPU count and details (NVIDIA via nvidia-smi)
- Capabilities (docker)

## Usage Examples

### Example 1: Simple Web Server

```bash
# Create blueprint definition (one-time)
colonies blueprint definition add --spec - <<EOF
{
  "metadata": {
    "name": "executordeployments.compute.colonies.io"
  },
  "spec": {
    "group": "compute.colonies.io",
    "version": "v1",
    "names": {
      "kind": "ExecutorDeployment",
      "plural": "executordeployments"
    },
    "handler": {
      "executorType": "deployment-controller",
      "functionName": "reconcile"
    }
  }
}
EOF

# Deploy nginx with 3 replicas
colonies blueprint add --spec - <<EOF
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "nginx"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 3
  }
}
EOF

# Verify containers
docker ps --filter "label=colonies.deployment=nginx"
```

### Example 2: Scale Deployment

```bash
# Update replicas from 3 to 5
colonies blueprint update --spec - <<EOF
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "nginx"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 5
  }
}
EOF

# Reconciler automatically adds 2 more containers
```

### Example 3: Complex Multi-Container App

```bash
# Deploy a full stack application
colonies blueprint add --spec - <<EOF
{
  "kind": "DockerDeployment",
  "metadata": {
    "name": "myapp"
  },
  "spec": {
    "instances": [
      {
        "name": "database",
        "image": "postgres:15",
        "environment": {
          "POSTGRES_PASSWORD": "secret",
          "POSTGRES_DB": "myapp"
        },
        "volumes": [
          {
            "type": "named",
            "name": "postgres-data",
            "mountPath": "/var/lib/postgresql/data"
          }
        ],
        "ports": [
          {
            "container": 5432,
            "host": 5432
          }
        ]
      },
      {
        "name": "backend",
        "image": "myapp/backend:latest",
        "dependsOn": ["database"],
        "environment": {
          "DATABASE_URL": "postgresql://postgres:secret@database:5432/myapp"
        },
        "ports": [
          {
            "container": 8080,
            "host": 8080
          }
        ]
      },
      {
        "name": "frontend",
        "image": "myapp/frontend:latest",
        "dependsOn": ["backend"],
        "environment": {
          "API_URL": "http://backend:8080"
        },
        "ports": [
          {
            "container": 80,
            "host": 8000
          }
        ]
      }
    ]
  }
}
EOF
```

## Container Management

### Container Labels

Every managed container is labeled for tracking:

| Label | Description | Example |
|-------|-------------|---------|
| `colonies.deployment` | Blueprint name | `nginx` |
| `colonies.managed` | Managed by ColonyOS | `true` |
| `colonies.generation` | Blueprint version | `3` |

### Container Naming

**ExecutorDeployment**: `<blueprint-name>-<index>`
- Example: `nginx-0`, `nginx-1`, `nginx-2`

**DockerDeployment**: `<instance-name>` (from spec)
- Example: `database`, `backend`, `frontend`

**ExecutorDeployment with executorName**: `<executor-name>-<unique-hash>`
- Example: `docker-executor-a3f2e`
- Each container auto-registers as a ColonyOS executor

### Listing Managed Containers

```bash
# All managed containers
docker ps --filter "label=colonies.managed=true"

# Specific deployment
docker ps --filter "label=colonies.deployment=nginx"

# Containers by generation
docker ps --filter "label=colonies.generation=3"
```

### Manual Cleanup

```bash
# Remove all managed containers
docker rm -f $(docker ps -aq --filter "label=colonies.managed=true")

# Remove specific deployment
docker rm -f $(docker ps -aq --filter "label=colonies.deployment=nginx")
```

## Node Registration

### Executor Deployment with Node Registration

The reconciler supports deploying ColonyOS executors that auto-register with node metadata:

```mermaid
sequenceDiagram
    participant User
    participant Reconciler
    participant Docker
    participant Container as Docker Executor<br/>Container
    participant Server as ColonyOS Server

    User->>Reconciler: Create ExecutorDeployment<br/>with executorName
    Reconciler->>Reconciler: Generate unique<br/>executor name
    Reconciler->>Docker: Create container with<br/>COLONIES_EXECUTOR_NAME<br/>COLONIES_NODE_NAME<br/>COLONIES_NODE_LOCATION
    Docker->>Container: Start container
    Container->>Server: Self-register as executor<br/>with node metadata
    Server->>Server: Create/update node<br/>Add executor to node
    Container->>Server: Start pulling processes
```

**Example**:
```json
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "docker-executors"
  },
  "spec": {
    "image": "colonyos/dockerexecutor:latest",
    "replicas": 5,
    "executorName": "docker-executor",
    "env": {
      "COLONIES_COLONY_PRVKEY": "colony-private-key"
    }
  }
}
```

This creates 5 executors:
- `docker-executor-a3f2e`
- `docker-executor-b7k9m`
- `docker-executor-c2p1x`
- etc.

Each automatically registers with the node where the reconciler is running.

### Network Configuration

Containers are attached to the `colonies_default` network with aliases:

```mermaid
graph TB
    subgraph "colonies_default Network"
        C1[Container:<br/>nginx-0]
        C2[Container:<br/>nginx-1]
        C3[Container:<br/>nginx-2]

        A1[Alias: nginx]
    end

    A1 -.->|Load balanced| C1
    A1 -.->|Load balanced| C2
    A1 -.->|Load balanced| C3

    External[External Client] -->|nginx:80| A1

    style A1 fill:#FF9800
    style C1 fill:#2196F3
    style C2 fill:#2196F3
    style C3 fill:#2196F3
```

All containers from the same deployment share a network alias (blueprint name), enabling service discovery.

## Development

### Project Structure

```
docker-reconciler/
├── cmd/
│   └── main.go              # Entry point
├── pkg/
│   ├── executor/            # Executor logic & process handling
│   │   └── executor.go      # - Process assignment
│   │                        # - Reconciliation orchestration
│   │                        # - Node metadata detection
│   ├── reconciler/          # Core reconciliation engine
│   │   └── reconciler.go    # - State comparison
│   │                        # - Container lifecycle
│   │                        # - Docker API interaction
│   └── build/               # Build info
├── internal/
│   └── cli/                 # CLI commands
│       ├── root.go
│       └── start.go
├── examples/                # Example blueprints
├── docker-compose.yml       # Docker Compose setup
├── Dockerfile               # Container build
├── Makefile                 # Build automation
└── README.md                # This file
```

### Building

```bash
# Build binary
make build

# Build Docker image
make container

# Run tests
make test

# Install to /usr/local/bin
sudo make install
```

### Reconciliation Flow in Code

```mermaid
flowchart TD
    A[Executor.ServeForEver] --> B[Assign Process]
    B --> C{Function Name?}
    C -->|reconcile| D[handleReconcile]
    C -->|other| E[Fail Process]

    D --> F{Action Type?}
    F -->|create/update| G[reconciler.Reconcile]
    F -->|delete| G

    G --> H{Blueprint Kind?}
    H -->|ExecutorDeployment| I[reconcileExecutorDeployment]
    H -->|DockerDeployment| J[reconcileDockerDeployment]

    I --> K[List Containers]
    J --> K

    K --> L[Find Dirty Containers<br/>generation mismatch]
    L --> M{Dirty?}
    M -->|Yes| N[Recreate Containers]
    M -->|No| O[Check Replica Count]

    N --> O
    O --> P{Scale?}
    P -->|Up| Q[Start New Containers]
    P -->|Down| R[Stop Excess Containers]
    P -->|Match| S[No Action]

    Q --> T[Collect Status]
    R --> T
    S --> T

    T --> U[Report to Server]
    U --> V[Close Process]

    style G fill:#4CAF50
    style I fill:#2196F3
    style J fill:#FF9800
    style T fill:#9C27B0
```

## Troubleshooting

### Reconciler Not Starting

**Check logs**:
```bash
docker-compose logs
```

**Common Issues**:
- Missing `COLONIES_COLONY_PRVKEY` environment variable
- Cannot reach ColonyOS server
- Docker daemon not accessible

**Solution**:
```bash
# Verify Docker socket
ls -l /var/run/docker.sock

# Test connection
docker ps

# Check environment
docker-compose config
```

### Containers Not Being Created

**Check reconciler logs**:
```bash
docker-compose logs -f | grep -i error
```

**Common Issues**:
- Image not found or pull failed
- Insufficient resources
- Docker socket permissions

**Solution**:
```bash
# Pull image manually
docker pull nginx:latest

# Check disk space
df -h

# Check Docker socket permissions
ls -l /var/run/docker.sock
```

### Containers Not Updating

**Verify generation**:
```bash
# Check container generation
docker inspect <container-name> | grep colonies.generation

# Check blueprint generation
colonies blueprint get --name <blueprint-name>
```

**Common Issues**:
- Generation label missing (old containers)
- Reconciliation process failed

**Solution**:
```bash
# Force recreation by removing containers
docker rm -f $(docker ps -aq --filter "label=colonies.deployment=<name>")

# Trigger reconciliation
colonies blueprint update --spec <spec-file>
```

### Connection Issues on Mac/Windows

**Problem**: Cannot connect to ColonyOS on localhost

**Solution**: Use `host.docker.internal`
```bash
# In .env or docker-compose.yml
COLONIES_SERVER_HOST=host.docker.internal
```

### GPU Detection Not Working

**Check nvidia-smi**:
```bash
# Inside container
docker exec <reconciler-container> nvidia-smi
```

**Solution**:
```yaml
# In docker-compose.yml, add GPU support
services:
  deployment-controller:
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
```

## Advanced Topics

### Custom Network Configuration

Override default network in docker-compose.yml:

```yaml
networks:
  colonies_default:
    external: true
    name: my-custom-network
```

### Resource Limits

For DockerDeployment, specify resource limits:

```json
{
  "name": "heavy-worker",
  "image": "myapp/worker:latest",
  "resources": {
    "cpus": "2.0",
    "memory": "4096"
  }
}
```

### Health Checks

Add health checks to DockerDeployment instances:

```json
{
  "name": "api",
  "image": "myapp/api:latest",
  "healthcheck": {
    "test": "curl -f http://localhost:8080/health || exit 1",
    "interval": "30s",
    "timeout": "10s",
    "retries": 3,
    "startPeriod": "40s"
  }
}
```

### Privileged Containers

For containers requiring elevated privileges:

```json
{
  "kind": "ExecutorDeployment",
  "spec": {
    "image": "docker:dind",
    "privileged": true
  }
}
```

## Security Considerations

**Important Security Notes**:

1. **Docker Socket Access**: The reconciler requires access to `/var/run/docker.sock`, which grants significant privileges
2. **Privileged Containers**: Use `privileged: true` only when absolutely necessary
3. **Network Isolation**: Consider using custom networks for isolation
4. **Secrets Management**: Never store secrets in blueprint specs; use environment variables or secret management systems
5. **TLS**: Always use TLS in production (`COLONIES_INSECURE=false`)

## License

See main ColonyOS repository for license information.

## Contributing

This executor is part of the ColonyOS ecosystem. Contributions welcome!

1. Follow ColonyOS contribution guidelines
2. Test locally before submitting
3. Update documentation as needed
4. Add tests for new features

## Support

- **Documentation**: This README
- **Issues**: [ColonyOS GitHub Issues](https://github.com/colonyos/colonies/issues)
- **Community**: ColonyOS community channels
