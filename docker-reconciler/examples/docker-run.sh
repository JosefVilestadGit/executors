#!/bin/bash

# Example script to run the deployment controller as a Docker container
# This demonstrates running the controller without docker-compose

# Configuration
IMAGE_NAME="colonyos/deployment-controller"
CONTAINER_NAME="deployment-controller"

# ColonyOS configuration (set these to your values)
COLONIES_SERVER_HOST="${COLONIES_SERVER_HOST:-localhost}"
COLONIES_SERVER_PORT="${COLONIES_SERVER_PORT:-50080}"
COLONIES_INSECURE="${COLONIES_INSECURE:-true}"
COLONIES_COLONY_NAME="${COLONIES_COLONY_NAME:-dev}"
COLONIES_COLONY_PRVKEY="${COLONIES_COLONY_PRVKEY:-your-colony-private-key-here}"
COLONIES_EXECUTOR_NAME="${COLONIES_EXECUTOR_NAME:-deployment-controller-1}"
COLONIES_EXECUTOR_TYPE="${COLONIES_EXECUTOR_TYPE:-deployment-controller}"

# Check if container already exists
if [ "$(docker ps -aq -f name=${CONTAINER_NAME})" ]; then
    echo "Stopping and removing existing container..."
    docker stop ${CONTAINER_NAME} 2>/dev/null
    docker rm ${CONTAINER_NAME} 2>/dev/null
fi

# Run the container
echo "Starting deployment controller container..."
docker run -d \
  --name ${CONTAINER_NAME} \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock:rw \
  -e COLONIES_SERVER_HOST="${COLONIES_SERVER_HOST}" \
  -e COLONIES_SERVER_PORT="${COLONIES_SERVER_PORT}" \
  -e COLONIES_INSECURE="${COLONIES_INSECURE}" \
  -e COLONIES_COLONY_NAME="${COLONIES_COLONY_NAME}" \
  -e COLONIES_COLONY_PRVKEY="${COLONIES_COLONY_PRVKEY}" \
  -e COLONIES_EXECUTOR_NAME="${COLONIES_EXECUTOR_NAME}" \
  -e COLONIES_EXECUTOR_TYPE="${COLONIES_EXECUTOR_TYPE}" \
  ${IMAGE_NAME}

# Check if container started successfully
if [ $? -eq 0 ]; then
    echo "✓ Deployment controller started successfully!"
    echo ""
    echo "Container name: ${CONTAINER_NAME}"
    echo "View logs with: docker logs -f ${CONTAINER_NAME}"
    echo "Stop with: docker stop ${CONTAINER_NAME}"
else
    echo "✗ Failed to start deployment controller"
    exit 1
fi
