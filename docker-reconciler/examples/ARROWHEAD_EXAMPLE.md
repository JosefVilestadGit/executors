# Arrowhead Cloud as ColonyOS Service

This example demonstrates how to deploy an Eclipse Arrowhead Framework cloud as a ColonyOS managed service using the docker-reconciler.

## Overview

The Arrowhead Framework is an IoT platform that provides service-oriented architecture for industrial automation. Each Arrowhead cloud consists of:

- **Database**: TimescaleDB/MySQL database for storing service registry and authorization data
- **Core Systems**:
  - Service Registry: Service discovery and registration
  - Authorization: Access control and authorization rules
  - Orchestrator: Service orchestration and matchmaking
  - Event Handler: Event-based communication
  - Gatekeeper: Inter-cloud communication gateway
  - Gateway: Tunnel management for secure connections

## Service Definition

The `arrowhead-cloud-definition.json` defines a new service kind called `ArrowheadCloud` with JSON schema validation for:

- Cloud naming and identification
- Database configuration (image, ports, volumes)
- Core systems configuration (enabling/disabling individual systems)
- Network configuration
- Certificate password

## Example Service Instance

The `arrowhead-cloud-c1.json` creates a service instance named "c1-cloud" with:

- Database on port 3306
- All 6 core systems enabled with standard ports
- Configuration path pointing to your local arrowhead-core-docker setup
- Custom Docker network for container communication

## How the Docker Reconciler Handles This

When you create or update this service, the docker-reconciler will:

1. **Parse the spec** and validate it against the ArrowheadCloud schema
2. **Create a Docker network** (arrowhead-c1) for inter-container communication
3. **Deploy containers in dependency order**:
   - First: c1-database (database must be ready before core systems)
   - Then: All 6 core systems in parallel (they all depend on database)

4. **For each container**, the reconciler will:
   - Set container name: `{cloudName}-{systemName}` (e.g., c1-serviceregistry)
   - Mount volumes:
     - Certificate directories from `{configPath}/{cloudName}/{systemName}/certificates`
     - Application properties from `{configPath}/{cloudName}-props/{systemName}/application.properties`
     - Config files from `{configPath}/config/{systemName}/`
   - Set environment variables (PASSWORD, SYSTEM_NAME, etc.)
   - Configure port mappings
   - Connect to the custom network

5. **Monitor container status** and report back in the service status field

6. **Handle updates**: If you change the spec (e.g., disable eventhandler), the reconciler will:
   - Detect the difference
   - Remove the eventhandler container
   - Keep other containers running
   - Update the service status

## Prerequisites

Before deploying this service, you need to:

1. **Generate certificates** for all core systems using the scripts in arrowhead-core-docker
2. **Create .env file** with PASSWORD set
3. **Configure /etc/hosts** with DNS entries (c1-serviceregistry, c1-authorization, etc.)
4. **Ensure paths exist**:
   - `{configPath}/c1/serviceregistry/certificates/`
   - `{configPath}/c1/authorization/certificates/`
   - etc. for all systems
   - `{configPath}/c1-props/serviceregistry/application.properties`
   - etc.

## Deployment Steps

1. **Add the ServiceDefinition** (colony owner only):
```bash
colonies service definition add --spec arrowhead-cloud-definition.json
```

2. **Create the service instance**:
```bash
colonies service add --spec arrowhead-cloud-c1.json
```

3. **Check service status**:
```bash
colonies service get --name c1-cloud
```

4. **View deployment details**:
```bash
colonies service get --name c1-cloud
```

The status will show:
- Which containers are running
- Container IDs and health status
- Any errors during deployment

## Updating the Service

To scale or reconfigure:

```bash
# Edit the JSON file (e.g., disable eventhandler)
# Then update:
colonies service update --spec arrowhead-cloud-c1.json
```

Or use the set command to change a single field:

```bash
colonies service set --name c1-cloud --key systems.eventhandler.enabled --value false
```

## Service History

Track all changes to your Arrowhead cloud:

```bash
colonies service history --name c1-cloud
```

This shows who made changes (user or reconciler) and when, with generation numbers incrementing for each spec change.

## Implementation Notes

### Container Type Mapping

The docker-reconciler would need to be enhanced to handle this multi-container service pattern. Currently it expects a single `instance` array with `type: "container"`. For Arrowhead, you could either:

1. **Option A**: Keep current structure, list all 7 containers explicitly in the spec:
```json
{
  "instances": [
    {
      "name": "c1-database",
      "type": "container",
      "image": "aitiaiiot/arrowhead-database:4.6.1",
      ...
    },
    {
      "name": "c1-serviceregistry",
      "type": "container",
      "image": "aitiaiiot/arrowhead-system:4.6.1",
      ...
    },
    ...
  ]
}
```

2. **Option B**: Enhance reconciler to understand "cloud" type and expand it into containers:
```json
{
  "instances": [
    {
      "name": "c1",
      "type": "cloud",
      "cloudSpec": { ... }
    }
  ]
}
```

**Option A is simpler and works with current reconciler immediately.** Let me create that version...

### Dependency Management

The reconciler should:
- Start database first, wait for healthy status
- Only then start core systems
- Use Docker healthchecks or wait-for-it scripts
- Handle cascade deletion (remove all containers when service is deleted)

### Volume Management

Named volumes vs bind mounts:
- Database: Use named Docker volume for persistence
- Certificates: Use bind mounts to local filesystem (already exist)
- Config files: Use bind mounts to local filesystem (already exist)

### Network Isolation

Create a dedicated Docker network per service instance to isolate different Arrowhead clouds from each other while allowing inter-container communication within a cloud.
