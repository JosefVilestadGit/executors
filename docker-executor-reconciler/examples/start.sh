#!/bin/bash

# Example script to start the deployment controller executor

export COLONIES_SERVER_HOST="localhost"
export COLONIES_SERVER_PORT="50080"
export COLONIES_INSECURE="true"
export COLONIES_COLONY_NAME="dev"
export COLONIES_COLONY_PRVKEY="your-colony-private-key-here"
export COLONIES_EXECUTOR_NAME="deployment-controller-1"
export COLONIES_EXECUTOR_TYPE="deployment-controller"

# Run the executor
../bin/deployment-controller start --verbose
