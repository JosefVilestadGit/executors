# Docker Reconciler Examples

This directory contains example blueprint specifications for deploying containers using the docker-reconciler.

## Example Files

### 1. executor-deployment-definition.json
The BlueprintDefinition that registers the ExecutorDeployment kind with ColonyOS. This must be added once (by colony owner) before you can create ExecutorDeployment blueprints.

**Usage:**
```bash
colonies blueprint definition add --spec executor-deployment-definition.json
```

### 2. docker-executor-edge.json
Deploys a docker executor specifically on the **edge node** (docker-reconciler-edge).

**Key Settings:**
- `executorType`: `docker-reconciler` - Requires a docker-reconciler type executor
- `executorName`: `docker-reconciler-edge` - Targets the specific edge node reconciler
- `replicas`: 1 - Single executor instance

**Use Case:** When you need a docker executor running specifically in the edge datacenter.

**Usage:**
```bash
colonies blueprint add --spec docker-executor-edge.json
```

### 3. docker-executor-any-node.json
Deploys docker executors on **any available docker-reconciler node** with automatic load balancing.

**Key Settings:**
- `executorType`: `docker-reconciler` - Requires a docker-reconciler type executor
- `executorName`: *not specified* - ANY reconciler can handle it
- `replicas`: 3 - Creates 3 executor instances

**Use Case:** When you want high availability and don't care which node runs the executors. The reconciliation process will be picked up by whichever reconciler claims it first.

**Usage:**
```bash
colonies blueprint add --spec docker-executor-any-node.json
```

## Executor Targeting Strategies

### Strategy 1: Target Specific Executor (Pinned Deployment)
```json
{
  "executorType": "docker-reconciler",
  "executorName": "docker-reconciler-edge"
}
```
- ✅ Guarantees deployment on specific node
- ✅ Good for location-specific requirements
- ⚠️ Fails if that executor is down

### Strategy 2: Any Executor (Load Balanced)
```json
{
  "executorType": "docker-reconciler"
  // No executorName specified
}
```
- ✅ High availability - any reconciler can handle it
- ✅ Automatic load distribution
- ✅ Survives individual node failures
- ⚠️ You don't control which node it runs on

### Strategy 3: Multiple Specific Executors (Failover)
```json
{
  "executorType": "docker-reconciler",
  "executorNames": ["docker-reconciler-node1", "docker-reconciler-edge"]
}
```
- ✅ Failover between specific nodes
- ✅ More control than "any executor"
- ✅ Higher availability than single executor

## Common Configuration

All examples include:

### Environment Variables
- **ColonyOS Connection**: Server host, port, TLS settings
- **Colony Credentials**: Colony name and private key
- **S3/MinIO**: For file storage integration
- **Executor Metadata**: Type, capabilities, location

### Volumes
- `/var/run/docker.sock` - Required for Docker API access
- `/tmp/colonies` - Shared file storage directory

### Security
- `privileged: true` - Required for Docker-in-Docker operations

## Testing the Examples

### 1. Register the Blueprint Definition (one-time setup)
```bash
export COLONIES_PRVKEY=${COLONIES_COLONY_PRVKEY}
colonies blueprint definition add --spec executor-deployment-definition.json
```

### 2. Deploy to Edge Node
```bash
colonies blueprint add --spec docker-executor-edge.json

# Check status
colonies blueprint get --name docker-executor-edge

# Watch reconciliation
colonies process ps
```

### 3. Deploy to Any Node (Load Balanced)
```bash
colonies blueprint add --spec docker-executor-any-node.json

# Check which node picked it up
colonies executor ls
```

## Updating Deployments

### Scale Up/Down
```bash
# Scale edge deployment to 3 replicas
colonies blueprint set --name docker-executor-edge --key spec.replicas --value 3

# Scale down to 1
colonies blueprint set --name docker-executor-edge --key spec.replicas --value 1
```

### Change Image Version
```bash
colonies blueprint set --name docker-executor-edge \
  --key spec.image --value colonyos/dockerexecutor:v1.0.8
```

### Update Environment Variables
```bash
colonies blueprint set --name docker-executor-edge \
  --key spec.env.EXECUTOR_GPU --value 1
```

## Monitoring

### Check Blueprint Status
```bash
# List all blueprints
colonies blueprint ls

# Get specific blueprint
colonies blueprint get --name docker-executor-edge

# View history
colonies blueprint history --name docker-executor-edge
```

### Check Running Containers
```bash
# Via ColonyOS
colonies executor ls

# Via Docker (on the node)
docker ps --filter label=colonies.blueprint=docker-executor-edge
```

### Check Nodes
```bash
# List all registered nodes
colonies node ls

# Get specific node details
colonies node get --name edge
```

## Troubleshooting

### Blueprint Stuck in "Reconciling"
```bash
# Check reconciler logs
docker logs docker-reconciler-edge -f

# Check process status
colonies process ps

# Look for errors
colonies blueprint get --name docker-executor-edge
```

### Containers Not Starting
```bash
# On the reconciler node, check Docker
docker ps -a --filter label=colonies.managed=true

# Check container logs
docker logs <container-id>
```

### Wrong Node Picked Up Blueprint
If you specified `executorName` but the wrong node picked it up:
1. Check the executor name matches exactly
2. Verify that executor is registered: `colonies executor ls`
3. Check blueprint spec: `colonies blueprint get --name <name>`

## Advanced Examples

See the `arrowhead/` directory for examples of deploying complex multi-service applications using DockerDeployment kind.

## See Also

- [../../README.md](../../README.md) - Docker Reconciler documentation
- [../../../../colonies/docs/Blueprints.md](../../../../colonies/docs/Blueprints.md) - Complete blueprint guide
- [../../../../colonies/docs/Reconciliation.md](../../../../colonies/docs/Reconciliation.md) - How reconciliation works
