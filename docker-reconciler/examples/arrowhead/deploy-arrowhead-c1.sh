#!/bin/bash

# Deploy Arrowhead Cloud C1 as ColonyOS blueprints
# This script deploys all components in the correct order

set -e

EXAMPLES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Deploying Arrowhead Cloud C1..."
echo ""

# Check if password is set
#if ! grep -q "PASSWORD" /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env 2>/dev/null; then
#    echo "Warning: .env file not found or PASSWORD not set"
#    echo "Please set PASSWORD in /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env"
#    exit 1
#fi

#PASSWORD=$(grep "^PASSWORD=" /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env | cut -d'=' -f2)

PASSWORD=123456
if [ -z "$PASSWORD" ]; then
    echo "Error: PASSWORD is empty in .env file"
    exit 1
fi

# Register DockerDeployment definition if not already registered
echo "Checking if DockerDeployment definition is registered..."
if ! colonies blueprint definition get --name docker-deployment 2>/dev/null; then
    echo "Registering DockerDeployment blueprint definition..."
    echo "NOTE: This requires colony owner privileges. Please run with COLONIES_PRVKEY set to colony private key:"
    echo "  export COLONIES_PRVKEY=\${COLONIES_COLONY_PRVKEY}"
    echo "  $0"
    exit 1
else
    echo "DockerDeployment definition already registered"
fi
echo ""

# Update password in all blueprint definitions
echo "Updating password in blueprint definitions..."
for file in arrowhead-c1-*.json; do
    if [ "$file" != "arrowhead-c1-database.json" ]; then
        sed -i "s/\"PASSWORD\": \".*\"/\"PASSWORD\": \"$PASSWORD\"/" "$file"
    fi
done

echo ""
echo "Step 1: Deploying database..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-database.json"
echo "Waiting for database to be ready..."
sleep 5

echo ""
echo "Step 2: Deploying Service Registry..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-serviceregistry.json"

echo ""
echo "Step 3: Deploying Authorization..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-authorization.json"

echo ""
echo "Step 4: Deploying Orchestrator..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-orchestrator.json"

echo ""
echo "Step 5: Deploying Event Handler..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-eventhandler.json"

echo ""
echo "Step 6: Deploying Gatekeeper..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-gatekeeper.json"

echo ""
echo "Step 7: Deploying Gateway..."
colonies blueprint add --spec "$EXAMPLES_DIR/arrowhead-c1-gateway.json"

echo ""
echo "==============================================="
echo "Arrowhead Cloud C1 deployment initiated!"
echo "==============================================="
echo ""
echo "Check blueprint status with:"
echo "  colonies blueprint ls"
echo ""
echo "View individual blueprints:"
echo "  colonies blueprint get --name c1-database"
echo "  colonies blueprint get --name c1-serviceregistry"
echo "  colonies blueprint get --name c1-authorization"
echo "  colonies blueprint get --name c1-orchestrator"
echo "  colonies blueprint get --name c1-eventhandler"
echo "  colonies blueprint get --name c1-gatekeeper"
echo "  colonies blueprint get --name c1-gateway"
echo ""
echo "View reconciliation processes:"
echo "  colonies process ps"
