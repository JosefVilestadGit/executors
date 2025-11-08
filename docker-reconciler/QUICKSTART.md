# Quick Start Guide

Get the deployment controller running in 5 minutes!

## Prerequisites

- Docker installed and running
- Access to a ColonyOS server
- Colony private key

## Option 1: Interactive Setup (Easiest)

```bash
# Run the setup script
./setup.sh

# Start the controller (automatically builds if needed)
docker-compose up -d

# View logs
docker-compose logs -f
```

**Note**: The first start will build the Docker image, which may take a few minutes.

The setup script will:
1. Create `.env` file with your configuration
2. Check Docker installation
3. Create required network
4. Provide next steps

## Option 2: Manual Setup

```bash
# 1. Edit configuration
nano .env
# Set your ColonyOS server details and colony private key

# 2. Create network
docker network create colonies-network

# 3. Start controller
docker-compose up -d

# 4. View logs
docker-compose logs -f
```

## Verify It's Working

Check the logs for:
```
INFO Deployment Controller Executor started
INFO Self-registered
```

## Create Your First Deployment

1. Register the ResourceDefinition (colony owner only):
```bash
colonies service definition add --spec examples/services/executor-deployment-definition.json
```

2. Create a deployment:
```bash
colonies service add --spec examples/nginx-deployment.json
```

3. Check managed containers:
```bash
docker ps --filter "label=colonies.managed=true"
```

You should see your nginx containers running!

## Troubleshooting

### Can't connect to ColonyOS server?

If running ColonyOS in Docker on the same machine:
```bash
# In .env, use:
COLONIES_SERVER_HOST=host.docker.internal  # Mac/Windows
COLONIES_SERVER_HOST=172.17.0.1           # Linux
```

### Permission denied on Docker socket?

Ensure the socket is accessible:
```bash
ls -l /var/run/docker.sock
```

### Container exits immediately?

Check logs:
```bash
docker-compose logs
```

Common issues:
- Missing `COLONIES_COLONY_PRVKEY` in `.env`
- Wrong server host/port
- Network not created

## Next Steps

- Read [README.md](README.md) for full documentation
- See [DOCKER-DEPLOYMENT.md](DOCKER-DEPLOYMENT.md) for advanced Docker configuration
- Check [examples/](examples/) for more deployment examples

## Common Commands

```bash
# Start
docker-compose up -d

# Stop
docker-compose down

# View logs
docker-compose logs -f

# Restart
docker-compose restart

# Rebuild after code changes
docker-compose up -d --build

# List managed containers
docker ps --filter "label=colonies.managed=true"

# Clean up all managed containers
docker rm -f $(docker ps -aq --filter "label=colonies.managed=true")
```
