# Running Deployment Controller as a Docker Container

This guide covers running the deployment controller executor as a Docker container.

## Prerequisites

- Docker installed and running
- Access to ColonyOS server
- Colony private key (for self-registration) OR executor credentials (if pre-registered)

## Security Warning

⚠️ **Important**: The deployment controller requires access to the Docker socket (`/var/run/docker.sock`). This gives the container **significant privileges** as it can:
- Create and manage containers on the host
- Access all Docker services
- Potentially escalate privileges

**Only run this in trusted environments and ensure:**
- The ColonyOS server is secured
- Only trusted users can create ExecutorDeployment services
- The network is properly isolated

## Quick Start

### Option 1: Using docker-compose (Recommended)

1. **Copy the environment file:**
   ```bash
   cp .env.example .env
   ```

2. **Edit `.env` with your configuration:**
   ```bash
   # Edit these values
   COLONIES_SERVER_HOST=your-colonies-server
   COLONIES_COLONY_NAME=your-colony-name
   COLONIES_COLONY_PRVKEY=your-colony-private-key
   COLONIES_EXECUTOR_NAME=deployment-controller-1
   ```

3. **Create the Docker network (if it doesn't exist):**
   ```bash
   docker network create colonies-network
   ```

4. **Start the controller:**
   ```bash
   docker-compose up -d
   ```

5. **View logs:**
   ```bash
   docker-compose logs -f
   ```

6. **Stop the controller:**
   ```bash
   docker-compose down
   ```

### Option 2: Using docker run

1. **Build the image:**
   ```bash
   make container
   ```

2. **Run with environment variables:**
   ```bash
   docker run -d \
     --name deployment-controller \
     --restart unless-stopped \
     -v /var/run/docker.sock:/var/run/docker.sock:rw \
     -e COLONIES_SERVER_HOST=your-colonies-server \
     -e COLONIES_SERVER_PORT=50080 \
     -e COLONIES_INSECURE=true \
     -e COLONIES_COLONY_NAME=your-colony \
     -e COLONIES_COLONY_PRVKEY=your-key \
     -e COLONIES_EXECUTOR_NAME=deployment-controller-1 \
     colonyos/deployment-controller
   ```

3. **Or use the provided script:**
   ```bash
   # Edit examples/docker-run.sh with your configuration
   ./examples/docker-run.sh
   ```

## Configuration

### Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `COLONIES_SERVER_HOST` | ColonyOS server hostname | Yes | - |
| `COLONIES_SERVER_PORT` | ColonyOS server port | No | 443 |
| `COLONIES_INSECURE` | Use insecure connection | No | false |
| `COLONIES_COLONY_NAME` | Colony name | Yes | - |
| `COLONIES_COLONY_PRVKEY` | Colony private key | Yes* | - |
| `COLONIES_PRVKEY` | Executor private key | Yes* | - |
| `COLONIES_EXECUTOR_NAME` | Executor name | Yes | - |
| `COLONIES_EXECUTOR_TYPE` | Executor type | No | deployment-controller |
| `COLONIES_EXECUTOR_ID` | Executor ID (if pre-registered) | No | - |

\* Either `COLONIES_COLONY_PRVKEY` or `COLONIES_PRVKEY` is required.

### Docker Socket Mount

The controller **must** have access to the Docker socket:
```bash
-v /var/run/docker.sock:/var/run/docker.sock:rw
```

### Networking

#### Standalone Deployment
If running standalone, ensure the container can reach:
- ColonyOS server (outbound)
- Docker daemon (via socket)

#### With ColonyOS Server
If running alongside ColonyOS server, use a shared network:
```bash
docker network create colonies-network

# Both containers should use this network
docker run --network colonies-network ...
```

## Building the Container

### Development Build
```bash
make container
```

This creates: `colonyos/deployment-controller`

### Production Build
```bash
# Build with version tag
make container BUILD_IMAGE=myregistry/deployment-controller:v1.0.0

# Push to registry
make push PUSH_IMAGE=myregistry/deployment-controller:v1.0.0
```

### Multi-stage Build Details

The Dockerfile uses a multi-stage build:
1. **Builder stage**: Compiles Go binary
2. **Runtime stage**: Minimal Alpine image with only the binary

Final image size: ~40MB

## Container Behavior

### What Containers Are Created?

When the controller reconciles an ExecutorDeployment:
1. **Controller container** runs continuously, watching for services
2. **Managed containers** are created on the **host Docker daemon** (not inside the controller)

Example:
```
Host Docker Daemon
├── deployment-controller (this container)
├── nginx-deployment-0 (created by controller)
├── nginx-deployment-1 (created by controller)
└── nginx-deployment-2 (created by controller)
```

### Labels on Managed Containers

All containers created by the controller have these labels:
- `colonies.deployment=<deployment-name>`
- `colonies.managed=true`

You can filter them:
```bash
docker ps --filter "label=colonies.managed=true"
docker ps --filter "label=colonies.deployment=nginx-deployment"
```

## Monitoring and Logs

### View Controller Logs
```bash
# docker-compose
docker-compose logs -f

# docker run
docker logs -f deployment-controller
```

### Check Controller Status
```bash
docker ps | grep deployment-controller
```

### View Managed Containers
```bash
# List all managed containers
docker ps --filter "label=colonies.managed=true"

# List containers for specific deployment
docker ps --filter "label=colonies.deployment=nginx-deployment"
```

### Logs from Managed Containers
```bash
docker logs nginx-deployment-0
```

## Troubleshooting

### Controller Won't Start

**Problem**: Container exits immediately
```bash
docker logs deployment-controller
```

**Common causes:**
- Missing required environment variables
- Cannot connect to ColonyOS server
- Invalid credentials

### Cannot Create Containers

**Problem**: Controller logs show "permission denied" for Docker socket

**Solution**: Ensure Docker socket is mounted with write permissions:
```bash
-v /var/run/docker.sock:/var/run/docker.sock:rw
```

### Controller Can't Reach ColonyOS Server

**Problem**: "connection refused" or timeout errors

**Solutions:**
- Use `host.docker.internal` instead of `localhost` on Mac/Windows
- Use correct hostname/IP for the server
- Ensure both containers are on same network
- Check firewall rules

```bash
# On Mac/Windows
COLONIES_SERVER_HOST=host.docker.internal

# On Linux with ColonyOS in Docker
COLONIES_SERVER_HOST=colonies-server  # Use container name
```

### Managed Containers Keep Restarting

**Problem**: Containers created by controller restart frequently

**Check:**
1. Container logs: `docker logs <container-name>`
2. Reconciliation process logs in ColonyOS
3. Service constraints (CPU/memory)

### Clean Up Orphaned Containers

If the controller crashes, managed containers may be left running:

```bash
# List all managed containers
docker ps -a --filter "label=colonies.managed=true"

# Remove all managed containers
docker rm -f $(docker ps -aq --filter "label=colonies.managed=true")
```

## Security Best Practices

### 1. Use Read-Only Root Filesystem
```yaml
services:
  deployment-controller:
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp
```

### 2. Drop Unnecessary Capabilities
```yaml
services:
  deployment-controller:
    cap_drop:
      - ALL
```

**Note**: The Docker socket access still provides significant power.

### 3. Network Isolation
```yaml
services:
  deployment-controller:
    networks:
      - colonies-internal
```

### 4. Service Limits
```yaml
services:
  deployment-controller:
    deploy:
      services:
        limits:
          cpus: '1.0'
          memory: 512M
```

### 5. Use Secrets for Credentials
```yaml
services:
  deployment-controller:
    secrets:
      - colony_prvkey
    environment:
      COLONIES_COLONY_PRVKEY: /run/secrets/colony_prvkey
```

## Production Deployment

### Recommended Setup

```yaml
version: '3.8'

services:
  deployment-controller:
    image: myregistry/deployment-controller:v1.0.0
    container_name: deployment-controller
    restart: unless-stopped

    # Security
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL

    # Services
    deploy:
      services:
        limits:
          cpus: '1.0'
          memory: 512M
        reservations:
          cpus: '0.25'
          memory: 128M

    # Volumes
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:rw

    # Environment
    env_file: .env

    # Networking
    networks:
      - colonies-network

    # Health check
    healthcheck:
      test: ["CMD", "pgrep", "-f", "deployment-controller"]
      interval: 30s
      timeout: 10s
      retries: 3

networks:
  colonies-network:
    driver: bridge
```

## Upgrading

### Zero-Downtime Upgrade

1. Build new image with new tag:
   ```bash
   docker build -t colonyos/deployment-controller:v1.1.0 .
   ```

2. Start new instance with different name:
   ```bash
   docker run -d --name deployment-controller-v1.1.0 ...
   ```

3. Wait for registration and verify:
   ```bash
   docker logs deployment-controller-v1.1.0
   ```

4. Stop old instance:
   ```bash
   docker stop deployment-controller
   ```

5. Remove old instance and rename new:
   ```bash
   docker rm deployment-controller
   docker rename deployment-controller-v1.1.0 deployment-controller
   ```

### With docker-compose

```bash
# Pull new image
docker-compose pull

# Recreate containers
docker-compose up -d
```

## Backup and Recovery

### Backup Configuration
```bash
# Backup .env file
cp .env .env.backup

# Backup docker-compose.yml
cp docker-compose.yml docker-compose.yml.backup
```

### Recovery
The controller is stateless. Managed containers will continue running even if the controller stops.

On restart, the controller will:
1. Re-register with ColonyOS
2. Resume processing reconciliation requests
3. Manage existing containers (identified by labels)

## Integration with CI/CD

### Example GitHub Actions Workflow

```yaml
name: Deploy Controller

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2

      - name: Build image
        run: |
          cd executors/deployment-controller
          docker build -t myregistry/deployment-controller:${{ github.sha }} .

      - name: Push image
        run: docker push myregistry/deployment-controller:${{ github.sha }}

      - name: Deploy to server
        run: |
          ssh user@server 'cd /opt/deployment-controller && \
            docker-compose pull && \
            docker-compose up -d'
```

## Frequently Asked Questions

**Q: Can I run multiple deployment controllers?**
A: Yes, each should have a unique `COLONIES_EXECUTOR_NAME`.

**Q: What happens if the controller crashes?**
A: Managed containers keep running. On restart, the controller will reconcile them.

**Q: Can the controller manage containers on remote Docker hosts?**
A: Not directly. It manages containers on the Docker daemon it's connected to (via socket).

**Q: How do I update a deployment?**
A: Update the ExecutorDeployment service in ColonyOS. The controller will reconcile the changes.

**Q: Can I use Podman instead of Docker?**
A: Potentially, if Podman's socket is Docker-compatible. This hasn't been tested.

## See Also

- [README.md](README.md) - General documentation
- [examples/](examples/) - Example configurations
- [ColonyOS Documentation](https://docs.colonyos.io)
