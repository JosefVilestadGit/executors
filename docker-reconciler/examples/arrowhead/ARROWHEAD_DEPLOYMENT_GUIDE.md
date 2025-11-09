# Deploying Arrowhead Cloud with ColonyOS

This guide shows how to deploy an Eclipse Arrowhead Framework cloud as ColonyOS managed blueprints.

## Overview

The Arrowhead Framework is an IoT platform that provides service-oriented architecture for industrial automation. Each cloud consists of:

- **Database** (MySQL/TimescaleDB)
- **6 Core Systems**:
  - Service Registry
  - Authorization
  - Orchestrator
  - Event Handler
  - Gatekeeper
  - Gateway

## Deployment Architecture

Each Arrowhead component is deployed as a separate ColonyOS blueprint of kind `ExecutorDeployment`. This approach:

- Works with the docker-reconciler immediately
- Allows independent scaling and management of each component
- Provides individual history tracking for each blueprint
- Enables fine-grained control over the Arrowhead cloud

### Networking

The docker-reconciler automatically configures:

- **Network Aliases**: Each container gets a network alias matching its container name (e.g., `c1-serviceregistry`), allowing inter-container communication without /etc/hosts modifications
- **Port Bindings**: All ports defined in the blueprint spec are automatically exposed on the host machine (e.g., port 8443 for service registry), enabling access from the Arrowhead Go CLI and other host-based tools
- **Docker Network**: All containers join the `colonies-network` bridge network for seamless communication

## Prerequisites

Before deploying, ensure you have:

1. **Certificates generated** for all core systems
   - Location: `/home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/c1/{system}/certificates`

2. **.env file** with PASSWORD set
   - Location: `/home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env`
   - Content: `PASSWORD=your-certificate-password`

3. **Configuration files** in place
   - Properties: `c1-props/{system}/application.properties`
   - Configs: `config/{system}/log4j2.xml` and `run.sh`

4. **ColonyOS blueprints running**
   - Colonies server
   - Docker-reconciler executor

## Quick Deployment

Use the provided deployment script:

```bash
cd /home/johan/dev/github/colonyos/executors/docker-reconciler/examples
./deploy-arrowhead-c1.sh
```

This script will:
1. Read PASSWORD from .env file
2. Update all blueprint definitions with the password
3. Deploy components in the correct order (database first, then core systems)
4. Provide status commands to check deployment

## Manual Deployment

If you prefer to deploy manually:

```bash
# 1. Deploy database first
colonies blueprint add --spec arrowhead-c1-database.json

# Wait for database to be ready
sleep 5

# 2. Deploy core systems (can be done in parallel)
colonies blueprint add --spec arrowhead-c1-serviceregistry.json
colonies blueprint add --spec arrowhead-c1-authorization.json
colonies blueprint add --spec arrowhead-c1-orchestrator.json
colonies blueprint add --spec arrowhead-c1-eventhandler.json
colonies blueprint add --spec arrowhead-c1-gatekeeper.json
colonies blueprint add --spec arrowhead-c1-gateway.json
```

## Checking Status

View all blueprints:
```bash
colonies blueprint ls
```

Check individual blueprint status:
```bash
colonies blueprint get --name c1-database
colonies blueprint get --name c1-serviceregistry
# ... etc
```

View blueprint history:
```bash
colonies blueprint history --name c1-serviceregistry
```

## Blueprint Files

Each component has its own JSON blueprint definition:

- `arrowhead-c1-database.json` - MySQL database
- `arrowhead-c1-serviceregistry.json` - Service Registry core system
- `arrowhead-c1-authorization.json` - Authorization core system
- `arrowhead-c1-orchestrator.json` - Orchestrator core system
- `arrowhead-c1-eventhandler.json` - Event Handler core system
- `arrowhead-c1-gatekeeper.json` - Gatekeeper core system
- `arrowhead-c1-gateway.json` - Gateway core system

All blueprints use:
- **Kind**: `ExecutorDeployment`
- **Labels**: `cloud: c1`, `arrowhead: true`, `component: {name}`
- **Replicas**: 1 (can be scaled later)
- **Port Bindings**: Ports are automatically exposed on the host machine
- **Network Aliases**: Containers can communicate using container names (e.g., `c1-serviceregistry`)

## Managing Blueprints

### Scaling

To scale a component (e.g., run 2 service registry instances):
```bash
colonies blueprint set --name c1-serviceregistry --key replicas --value 2
```

### Updating Configuration

If you change application.properties or other config files on the host, restart the blueprint:
```bash
colonies blueprint set --name c1-serviceregistry --key replicas --value 0
sleep 2
colonies blueprint set --name c1-serviceregistry --key replicas --value 1
```

Or update the blueprint definition and reapply:
```bash
# Edit arrowhead-c1-serviceregistry.json
colonies blueprint update --spec arrowhead-c1-serviceregistry.json
```

### Viewing Logs

Check Docker container logs:
```bash
# Find container name
docker ps | grep c1-serviceregistry

# View logs
docker logs c1-serviceregistry-{suffix}
```

## Cleanup

Remove all Arrowhead blueprints:
```bash
./cleanup-arrowhead-c1.sh
```

Or manually:
```bash
colonies blueprint remove --name c1-gateway
colonies blueprint remove --name c1-gatekeeper
colonies blueprint remove --name c1-eventhandler
colonies blueprint remove --name c1-orchestrator
colonies blueprint remove --name c1-authorization
colonies blueprint remove --name c1-serviceregistry
colonies blueprint remove --name c1-database
```

## Troubleshooting

### Service not starting

1. Check reconciler logs:
   ```bash
   docker logs docker-reconciler
   ```

2. Check if process failed:
   ```bash
   colonies process psf --count 10
   ```

3. Verify paths exist:
   ```bash
   ls -la /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/c1/serviceregistry/certificates
   ```

### Port conflicts

If ports are already in use, update the blueprint definition and change the port number:
```bash
# Edit the JSON file to change the port
vi arrowhead-c1-serviceregistry.json

# Update the blueprint
colonies blueprint update --spec arrowhead-c1-serviceregistry.json
```

### Password issues

Ensure PASSWORD is set correctly in .env file and matches the certificate password:
```bash
cat /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env
```

### Connectivity issues

Verify ports are exposed and accessible:
```bash
# Check port bindings
docker ps --filter "name=c1-" --format "table {{.Names}}\t{{.Ports}}"

# Test connectivity
curl -k https://localhost:8443/serviceregistry/echo

# Verify network aliases
docker exec c1-orchestrator ping -c 2 c1-serviceregistry
```

## Future Enhancements

For a more integrated experience, you could:

1. **Create an arrowhead-reconciler** that understands the full cloud topology
2. **Use ArrowheadCloud kind** to deploy all components as a single unit
3. **Add health checks** to verify component connectivity
4. **Implement dependency handling** so core systems wait for database
5. **Add auto-discovery** so components can find each other via ColonyOS

## Blueprint Definition Structure

Each blueprint follows this pattern:

```json
{
  "metadata": {
    "name": "c1-{component}",
    "labels": {
      "cloud": "c1",
      "component": "{component}",
      "arrowhead": "true"
    }
  },
  "kind": "ExecutorDeployment",
  "spec": {
    "image": "aitiaiiot/arrowhead-system:4.6.1",
    "replicas": 1,
    "env": {
      "SYSTEM_NAME": "{component}",
      "PASSWORD": "from-.env-file"
    },
    "ports": [...],
    "volumes": [...]
  }
}
```

The docker-reconciler will:
1. Receive the blueprint spec
2. Start the specified number of containers
3. Monitor container health
4. Update blueprint status
5. Handle scaling when replicas change
