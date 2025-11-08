# Deployment Controller Executor

A ColonyOS executor that reconciles container deployments based on ExecutorDeployment services. This executor acts as a Kubernetes-style controller that watches for service changes and ensures containers are deployed and scaled according to the specified configuration.

## 🚀 Quick Start - Complete Copy-Paste Example

```bash
# 1. Start ColonyOS with deployment-controller (already configured in docker-compose.yml)
cd /path/to/colonies
docker-compose up -d

# 2. Wait for services to be ready (about 30 seconds)
docker-compose logs -f deployment-controller

# 3. Register the ExecutorDeployment service definition (one-time setup)
export COLONIES_SERVER_HOST=localhost
export COLONIES_SERVER_PORT=50080
export COLONIES_INSECURE=true
export COLONIES_COLONY_NAME=dev
export COLONIES_PRVKEY=<your-colony-private-key>

colonies service definition add --spec - <<EOF
{
  "metadata": {
    "name": "executordeployments.compute.colonies.io"
  },
  "spec": {
    "group": "compute.colonies.io",
    "version": "v1",
    "names": {
      "kind": "ExecutorDeployment",
      "plural": "executordeployments",
      "singular": "executordeployment"
    },
    "scope": "Namespaced",
    "schema": {
      "type": "object",
      "properties": {
        "image": {
          "type": "string",
          "description": "Container image to deploy"
        },
        "replicas": {
          "type": "number",
          "description": "Number of executor replicas to run",
          "default": 1
        },
        "executorType": {
          "type": "string",
          "description": "Type of executor to deploy"
        },
        "env": {
          "type": "object",
          "description": "Environment variables"
        }
      },
      "required": ["image", "executorType"]
    },
    "handler": {
      "executorType": "deployment-controller",
      "functionName": "reconcile"
    }
  }
}
EOF

# 4. Create your first deployment - nginx with 3 replicas
colonies service add --spec - <<EOF
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
      "ENVIRONMENT": "production",
      "LOG_LEVEL": "info"
    }
  }
}
EOF

# 5. Verify containers are running
docker ps --filter "label=colonies.deployment=nginx-deployment"

# Expected output:
# CONTAINER ID   IMAGE          COMMAND                  STATUS         NAMES
# abc123...      nginx:latest   "/docker-entrypoint.…"   Up 10 seconds  nginx-deployment-0
# def456...      nginx:latest   "/docker-entrypoint.…"   Up 10 seconds  nginx-deployment-1
# ghi789...      nginx:latest   "/docker-entrypoint.…"   Up 10 seconds  nginx-deployment-2

# 6. Scale the deployment (change replicas to 5)
colonies service update --spec - <<EOF
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "nginx-deployment"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 5,
    "executorType": "deployment-controller",
    "env": {
      "ENVIRONMENT": "production"
    }
  }
}
EOF

# 7. Verify scaling worked
docker ps --filter "label=colonies.deployment=nginx-deployment" | wc -l
# Should show 6 (5 containers + 1 header line)

# 8. Clean up - remove the deployment
colonies service remove --name nginx-deployment

# 9. Stop all managed containers
docker rm -f $(docker ps -aq --filter "label=colonies.deployment=nginx-deployment")
```

**What just happened?**
1. ✅ Started deployment-controller with ColonyOS
2. ✅ Registered the ExecutorDeployment service type
3. ✅ Created an nginx deployment with 3 replicas
4. ✅ Controller automatically started 3 nginx containers
5. ✅ Scaled to 5 replicas - controller added 2 more containers
6. ✅ Cleaned up everything

### Alternative: Using Example Files

If you prefer using files instead of heredocs:

```bash
# Navigate to deployment-controller directory
cd executors/deployment-controller

# Register the service definition
colonies service definition add --spec examples/executor-deployment-definition.json

# Create a deployment
colonies service add --spec examples/nginx-deployment.json

# Or use the instance example
colonies service add --spec examples/executor-deployment-instance.json
```

## Overview

The Deployment Controller Executor:
- Watches for `ExecutorDeployment` services in ColonyOS
- Reconciles the desired state (replicas, image, env vars) with the actual running containers
- Manages container lifecycle (start, stop, scale up/down)
- Uses Docker labels to track and manage deployed containers
- Provides automated scaling based on replica count

## Architecture

```
┌─────────────────────┐
│   ColonyOS Server   │
│                     │
│  ┌──────────────┐  │
│  │  Services   │  │
│  │  (CRDs)      │  │
│  └──────────────┘  │
└──────────┬──────────┘
           │
           │ Reconcile Process
           ▼
┌─────────────────────┐
│ Deployment          │
│ Controller          │
│ Executor            │
└──────────┬──────────┘
           │
           │ Docker API
           ▼
┌─────────────────────┐
│   Docker Engine     │
│                     │
│  ┌──────────────┐  │
│  │  Containers  │  │
│  └──────────────┘  │
└─────────────────────┘
```

## Features

- **Declarative Management**: Define desired container state via ExecutorDeployment services
- **Automatic Reconciliation**: Continuously ensures actual state matches desired state
- **Scaling**: Automatically scales containers up or down based on replica count
- **Label-based Tracking**: Uses Docker labels to identify and manage containers
- **Environment Variables**: Supports passing environment variables to containers
- **Port Configuration**: Configure exposed ports for containers
- **Service Specification**: Define CPU and memory requirements

## Building

```bash
make build
```

This will create a binary at `./bin/deployment-controller`.

## Configuration

The executor is configured via environment variables:

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `COLONIES_SERVER_HOST` | Colonies server hostname | Yes | - |
| `COLONIES_SERVER_PORT` | Colonies server port | No | 443 |
| `COLONIES_INSECURE` | Use insecure connection (true/false) | No | false |
| `COLONIES_COLONY_NAME` | Name of the colony | Yes | - |
| `COLONIES_COLONY_PRVKEY` | Colony private key (for self-registration) | Yes* | - |
| `COLONIES_PRVKEY` | Executor private key (if pre-registered) | Yes* | - |
| `COLONIES_EXECUTOR_NAME` | Name of this executor | Yes | - |
| `COLONIES_EXECUTOR_TYPE` | Type of executor | No | deployment-controller |

\* Either `COLONIES_COLONY_PRVKEY` (for self-registration) or `COLONIES_PRVKEY` (for pre-registered executor) is required.

## Running

> **Running as Docker Container**: See [DOCKER-DEPLOYMENT.md](DOCKER-DEPLOYMENT.md) for detailed instructions on running the controller as a Docker container (recommended for production).

### Running Directly on Host

#### Self-Registration Mode

If you have the colony private key, the executor can self-register:

```bash
export COLONIES_SERVER_HOST="localhost"
export COLONIES_SERVER_PORT="50080"
export COLONIES_INSECURE="true"
export COLONIES_COLONY_NAME="dev"
export COLONIES_COLONY_PRVKEY="your-colony-private-key"
export COLONIES_EXECUTOR_NAME="deployment-controller-1"

./bin/deployment-controller start --verbose
```

#### Pre-Registered Mode

If the executor is already registered:

```bash
export COLONIES_SERVER_HOST="localhost"
export COLONIES_SERVER_PORT="50080"
export COLONIES_INSECURE="true"
export COLONIES_COLONY_NAME="dev"
export COLONIES_PRVKEY="your-executor-private-key"
export COLONIES_EXECUTOR_NAME="deployment-controller-1"
export COLONIES_EXECUTOR_ID="your-executor-id"

./bin/deployment-controller start --verbose
```

## Usage

### 1. Register the ResourceDefinition

First, register the ExecutorDeployment service definition (requires colony owner privileges):

```bash
colonies service definition add --spec examples/services/executor-deployment-definition.json
```

### 2. Create a Deployment

Create an ExecutorDeployment service:

```bash
colonies service add --spec examples/nginx-deployment.json
```

Example deployment (`nginx-deployment.json`):

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
    "cpu": "500m",
    "memory": "512Mi",
    "env": {
      "ENVIRONMENT": "production",
      "LOG_LEVEL": "info"
    },
    "ports": [
      {
        "name": "http",
        "port": 80,
        "protocol": "TCP"
      }
    ]
  }
}
```

### 3. The Executor Reconciles

The deployment controller executor will:
1. Pull the specified container image
2. Start the requested number of replicas
3. Set environment variables
4. Add labels for tracking (`colonies.deployment=<name>`, `colonies.managed=true`)

### 4. Scale the Deployment

Update the service to change the replica count:

```bash
# Edit the JSON file to change replicas
colonies service update --spec examples/nginx-deployment.json
```

The controller will automatically scale up or down to match the desired state.

### 5. Verify Containers

Check that containers are running:

```bash
docker ps --filter "label=colonies.deployment=nginx-deployment"
```

## How It Works

### Reconciliation Loop

1. **Watch for Processes**: The executor watches for `reconcile` function calls
2. **Fetch Service**: Retrieves the ExecutorDeployment service specification
3. **Compare State**: Lists running containers with the deployment label
4. **Reconcile**:
   - If current < desired replicas: Start new containers
   - If current > desired replicas: Stop excess containers
   - If current == desired replicas: No action needed
5. **Report Status**: Logs reconciliation status to the process

### Container Naming

Containers are named using the pattern: `<deployment-name>-<index>`

Example: `nginx-deployment-0`, `nginx-deployment-1`, `nginx-deployment-2`

### Labels

Each managed container has these labels:
- `colonies.deployment=<deployment-name>`: Identifies which deployment manages this container
- `colonies.managed=true`: Indicates the container is managed by ColonyOS

### Environment Variables

The executor automatically adds these environment variables to containers:
- `COLONIES_DEPLOYMENT=<deployment-name>`
- `COLONIES_CONTAINER_NAME=<container-name>`
- Plus any user-specified env vars from the service spec

## Troubleshooting

### Executor not receiving processes

- Check that the executor type matches the handler in the ResourceDefinition
- Verify the executor is registered and approved
- Check executor logs for connection errors

### Containers not starting

- Verify Docker is running and accessible
- Check image name is correct and accessible
- Review executor logs for detailed error messages
- Ensure sufficient services (CPU, memory) are available

### Scaling not working

- Verify the service was updated successfully
- Check that a reconciliation process was triggered
- Review process logs in ColonyOS

## Development

### Project Structure

```
deployment-controller/
├── cmd/
│   └── main.go              # Entry point
├── internal/
│   └── cli/                 # CLI commands
│       ├── root.go
│       └── start.go
├── pkg/
│   ├── build/              # Build info
│   ├── executor/           # Main executor logic
│   └── reconciler/         # Reconciliation logic
├── examples/               # Example configurations
├── Makefile
├── go.mod
└── README.md
```

### Testing Locally

1. Start a local ColonyOS server (see main repo docker-compose.yml)
2. Build the executor: `make build`
3. Register the ExecutorDeployment ResourceDefinition
4. Run the executor with the example script
5. Create test deployments

## Docker Container

Build a Docker container:

```bash
make container
```

Push to registry:

```bash
make push
```

## Future Enhancements

- [ ] Support for volume mounts
- [ ] Health checks and automatic restart
- [ ] Service limits enforcement (CPU, memory)
- [ ] Network configuration
- [ ] Support for multiple container runtimes (Podman, containerd)
- [ ] Status reporting back to service
- [ ] Event streaming
- [ ] Deployment strategies (rolling updates, blue-green)

## License

See main ColonyOS repository for license information.
