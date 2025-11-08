#!/bin/sh
# Entrypoint wrapper for docker executor to generate unique names
# This script uses the COLONIES_CONTAINER_NAME environment variable
# (automatically injected by deployment-controller) to create unique executor names

# If COLONIES_EXECUTOR_NAME is not set, derive it from COLONIES_CONTAINER_NAME
if [ -z "$COLONIES_EXECUTOR_NAME" ]; then
    export COLONIES_EXECUTOR_NAME="${COLONIES_CONTAINER_NAME}"
    echo "Using executor name: $COLONIES_EXECUTOR_NAME"
fi

# Wait for Colonies server to be ready
until colonies colony check --name "${COLONIES_COLONY_NAME}"; do
    echo "Waiting for Colonies server..."
    sleep 3
done

# Try to remove existing executor with same name (in case of restart)
colonies executor remove --name "${COLONIES_EXECUTOR_NAME}" 2>/dev/null || true

# Start the docker executor
exec docker_executor start -v
