# Scaling Docker Executors with Deployment Controller

## Overview

The deployment-controller can be used to scale docker executors, but there are some considerations regarding unique executor names and key management.

## The Challenge

Docker executors need:
1. **Unique executor names** - Each instance must have a different `COLONIES_EXECUTOR_NAME`
2. **Colony private key** - For self-registration with ColonyOS
3. **Access to Docker socket** - Mounted as `/var/run/docker.sock`
4. **Privileged mode** - To manage Docker containers

## Current Solution

The deployment-controller now supports:
- ✅ Volume mounts (for Docker socket)
- ✅ Privileged mode
- ✅ Environment variables
- ❌ Dynamic executor names (each replica needs unique name)

## Workaround: Use Index-Based Names

The deployment-controller creates containers with names like:
- `docker-executor-0`
- `docker-executor-1`
- `docker-executor-2`

We can use the `COLONIES_CONTAINER_NAME` environment variable (automatically injected) to derive unique executor names.

### Modified Docker Executor

The docker executor would need to be modified to:
```bash
# If COLONIES_EXECUTOR_NAME is not set, use COLONIES_CONTAINER_NAME
export COLONIES_EXECUTOR_NAME=${COLONIES_EXECUTOR_NAME:-$COLONIES_CONTAINER_NAME}
docker_executor start -v
```

## Alternative Approach: Manual Executor Names

Instead of using the deployment-controller for docker executors, you can:

1. **Use docker-compose for fixed executors**:
```yaml
services:
  docker-executor-1:
    image: colonyos/dockerexecutor:v1.0.5
    environment:
      COLONIES_EXECUTOR_NAME: "docker-executor-1"
      ...

  docker-executor-2:
    image: colonyos/dockerexecutor:v1.0.5
    environment:
      COLONIES_EXECUTOR_NAME: "docker-executor-2"
      ...
```

2. **Use deployment-controller for stateless workloads** (like nginx, processing tasks, etc.)

## Key Management

### Current Approach (Development)
Keys are embedded in the service spec:
```json
{
  "spec": {
    "env": {
      "COLONIES_COLONY_PRVKEY": "ba949fa134981372d6da62b6a56f336ab4d843b22c02a4257dcf7d0d73097514"
    }
  }
}
```

**Pros**: Simple, no additional infrastructure
**Cons**: Keys visible in service specs, not secure for production

### Recommended Approach (Production)

Implement a **Secret** service type in ColonyOS:

```json
{
  "kind": "Secret",
  "metadata": {
    "name": "colony-keys",
    "namespace": "dev"
  },
  "data": {
    "COLONIES_COLONY_PRVKEY": "<base64-encoded-key>"
  }
}
```

Then reference secrets in deployments:
```json
{
  "kind": "ExecutorDeployment",
  "spec": {
    "envFrom": [
      {
        "secretRef": {
          "name": "colony-keys"
        }
      }
    ]
  }
}
```

### Alternative: External Secret Management

Use external secret managers:
- **HashiCorp Vault** - Store keys in Vault, inject at runtime
- **Kubernetes Secrets** - If running in Kubernetes
- **Docker Secrets** - If using Docker Swarm
- **Environment variables** - Injected by deployment tool

## Example: Scaling Stateless Executors

The deployment-controller works great for stateless executors that don't need unique names:

```json
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "python-worker"
  },
  "spec": {
    "image": "colonyos/python-executor:latest",
    "replicas": 5,
    "executorType": "deployment-controller",
    "env": {
      "COLONIES_COLONY_NAME": "dev",
      "COLONIES_COLONY_PRVKEY": "..."
    }
  }
}
```

This will create 5 Python executor instances that can process tasks in parallel.

## Testing Scaling

### Scale Up
```bash
# Scale to 5 replicas
cat > /tmp/deployment-scaled.json <<EOF
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "nginx-deployment"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 5,
    "executorType": "deployment-controller"
  }
}
EOF

colonies service update --spec /tmp/deployment-scaled.json
```

### Scale Down
```bash
# Scale to 2 replicas
cat > /tmp/deployment-scaled.json <<EOF
{
  "kind": "ExecutorDeployment",
  "metadata": {
    "name": "nginx-deployment"
  },
  "spec": {
    "image": "nginx:latest",
    "replicas": 2,
    "executorType": "deployment-controller"
  }
}
EOF

colonies service update --spec /tmp/deployment-scaled.json
```

### Verify
```bash
docker ps --filter "label=colonies.deployment=nginx-deployment"
```

## Conclusion

The deployment-controller successfully implements:
- ✅ Container lifecycle management
- ✅ Scaling up and down
- ✅ Volume mounts and privileged mode
- ✅ Label-based container tracking
- ✅ Reconciliation pattern

For docker executors specifically, additional work is needed for:
- Dynamic executor name generation
- Secure key management (Secrets implementation)
- Executor state management (if executors have state)

**Recommendation**: Use the deployment-controller for stateless workloads and keep docker executors in docker-compose until unique naming is implemented.
