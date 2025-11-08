# Docker Reconciler Examples

This directory contains example service definitions for the ColonyOS docker-reconciler.

## Available Examples

### 1. Docker Executor Deployment

Deploy ColonyOS docker executors as managed services:

- **File**: `docker-executor.json`
- **Definition**: `docker-executor-definition.json`
- **Kind**: `ExecutorDeployment`

This example shows how to deploy executor containers that can run processes in your colony.

### 2. Arrowhead Framework Cloud Deployment

Deploy an Eclipse Arrowhead Framework cloud as ColonyOS services:

**Quick Start**:
```bash
./deploy-arrowhead-c1.sh
```

**Service Files**:
- `arrowhead-c1-database.json` - MySQL database
- `arrowhead-c1-serviceregistry.json` - Service Registry
- `arrowhead-c1-authorization.json` - Authorization
- `arrowhead-c1-orchestrator.json` - Orchestrator
- `arrowhead-c1-eventhandler.json` - Event Handler
- `arrowhead-c1-gatekeeper.json` - Gatekeeper
- `arrowhead-c1-gateway.json` - Gateway

**Documentation**: See `ARROWHEAD_DEPLOYMENT_GUIDE.md` for detailed instructions.

**Scripts**:
- `deploy-arrowhead-c1.sh` - Deploy all Arrowhead components
- `cleanup-arrowhead-c1.sh` - Remove all Arrowhead services

### 3. Service Definitions

The docker-reconciler requires service definitions (CRDs) to be registered:

- `docker-deployment-definition.json` - Defines DockerDeployment kind
- `docker-executor-definition.json` - Defines ExecutorDeployment kind (already exists)
- `arrowhead-cloud-definition.json` - Defines ArrowheadCloud kind (for future use)

## Service Definition Concepts

ColonyOS uses Kubernetes-inspired service definitions:

1. **ServiceDefinition** (CRD): Defines a new service kind with JSON schema validation
2. **Service** (CR): An instance of a service kind

Example flow:
```bash
# 1. Register the service definition (colony owner only)
colonies service definition add --spec docker-deployment-definition.json

# 2. Create service instances
colonies service add --spec arrowhead-c1-database.json

# 3. Manage services
colonies service get --name c1-database
colonies service set --name c1-database --key replicas --value 2
colonies service history --name c1-database
```

## Architecture

### How It Works

1. **User creates/updates a service** → Server validates against ServiceDefinition schema
2. **Server triggers reconciliation** → Creates a process for the configured reconciler
3. **Reconciler receives process** → Reads service spec and reconciles actual state
4. **Reconciler reports status** → Updates service status with instance information
5. **Periodic reconciliation** → Ensures desired state matches actual state

### Multi-Container Deployments

For complex deployments like Arrowhead with multiple containers:

**Current Approach** (works now):
- Deploy each component as a separate service
- Use labels to group related services
- Scripts to deploy/manage multiple services together

**Future Enhancement** (requires custom reconciler):
- Create ArrowheadCloud kind with all components in one spec
- Build arrowhead-reconciler that understands cloud topology
- Deploy entire cloud as a single service

## Example: Deploying Arrowhead Cloud

```bash
# Ensure prerequisites are met
ls /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env

# Deploy all components
cd /home/johan/dev/github/colonyos/executors/docker-reconciler/examples
./deploy-arrowhead-c1.sh

# Check status
colonies service ls
colonies service get --name c1-database
colonies service get --name c1-serviceregistry

# View history
colonies service history --name c1-serviceregistry

# Scale a component
colonies service set --name c1-serviceregistry --key replicas --value 2

# Cleanup
./cleanup-arrowhead-c1.sh
```

## Files in This Directory

```
├── README.md                           # This file
├── ARROWHEAD_DEPLOYMENT_GUIDE.md       # Detailed Arrowhead deployment guide
├── ARROWHEAD_EXAMPLE.md                # Conceptual multi-container example
│
├── docker-deployment-definition.json   # DockerDeployment CRD
├── docker-executor-definition.json     # ExecutorDeployment CRD
├── arrowhead-cloud-definition.json     # ArrowheadCloud CRD (future use)
│
├── docker-executor.json                # Executor deployment example
│
├── arrowhead-c1-database.json          # Arrowhead database service
├── arrowhead-c1-serviceregistry.json   # Arrowhead service registry
├── arrowhead-c1-authorization.json     # Arrowhead authorization
├── arrowhead-c1-orchestrator.json      # Arrowhead orchestrator
├── arrowhead-c1-eventhandler.json      # Arrowhead event handler
├── arrowhead-c1-gatekeeper.json        # Arrowhead gatekeeper
├── arrowhead-c1-gateway.json           # Arrowhead gateway
│
├── arrowhead-cloud-c1.json             # Conceptual single-service approach
├── arrowhead-cloud-c1-practical.json   # Multi-container approach (not used)
│
├── deploy-arrowhead-c1.sh              # Deployment script
└── cleanup-arrowhead-c1.sh             # Cleanup script
```

## Testing

After deploying services, verify they work:

```bash
# Check all services
colonies service ls

# Check individual service
colonies service get --name c1-database

# Check Docker containers
docker ps | grep c1-

# Check reconciler logs
docker logs docker-reconciler

# Check processes
colonies process ps
colonies process pss
colonies process psf
```

## Common Operations

### Deploy a Service
```bash
colonies service add --spec service-definition.json
```

### Check Service Status
```bash
colonies service get --name service-name
```

### Update a Service Field
```bash
colonies service set --name service-name --key replicas --value 2
```

### Update Entire Service
```bash
# Edit JSON file
colonies service update --spec service-definition.json
```

### Remove a Service
```bash
colonies service remove --name service-name
```

### View Service History
```bash
colonies service history --name service-name
```

## Troubleshooting

### Service not starting

1. Check reconciler logs:
   ```bash
   docker logs docker-reconciler
   ```

2. Check failed processes:
   ```bash
   colonies process psf --count 10
   ```

3. Verify service definition exists:
   ```bash
   colonies service definition ls
   ```

### Container not running

1. Check service status:
   ```bash
   colonies service get --name service-name
   ```

2. Check Docker:
   ```bash
   docker ps -a | grep service-name
   docker logs container-name
   ```

### Permission issues

Ensure volumes and paths are accessible:
```bash
ls -la /path/to/volume
```

## Next Steps

- Explore creating custom reconcilers for specialized workloads
- Implement health checks and auto-healing
- Add support for Docker Compose file imports
- Create visualization tools for service topology
