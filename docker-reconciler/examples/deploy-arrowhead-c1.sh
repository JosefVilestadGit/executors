#!/bin/bash

# Deploy Arrowhead Cloud C1 as ColonyOS services
# This script deploys all components in the correct order

set -e

EXAMPLES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Deploying Arrowhead Cloud C1..."
echo ""

# Check if password is set
if ! grep -q "PASSWORD" /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env 2>/dev/null; then
    echo "Warning: .env file not found or PASSWORD not set"
    echo "Please set PASSWORD in /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env"
    exit 1
fi

PASSWORD=$(grep "^PASSWORD=" /home/johan/dev/github/colonyos/colonies/arrowhead/arrowhead-core-docker/.env | cut -d'=' -f2)

if [ -z "$PASSWORD" ]; then
    echo "Error: PASSWORD is empty in .env file"
    exit 1
fi

# Update password in all service definitions
echo "Updating password in service definitions..."
for file in arrowhead-c1-*.json; do
    if [ "$file" != "arrowhead-c1-database.json" ]; then
        sed -i "s/\"PASSWORD\": \".*\"/\"PASSWORD\": \"$PASSWORD\"/" "$file"
    fi
done

echo ""
echo "Step 1: Deploying database..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-database.json"
echo "Waiting for database to be ready..."
sleep 5

echo ""
echo "Step 2: Deploying Service Registry..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-serviceregistry.json"

echo ""
echo "Step 3: Deploying Authorization..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-authorization.json"

echo ""
echo "Step 4: Deploying Orchestrator..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-orchestrator.json"

echo ""
echo "Step 5: Deploying Event Handler..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-eventhandler.json"

echo ""
echo "Step 6: Deploying Gatekeeper..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-gatekeeper.json"

echo ""
echo "Step 7: Deploying Gateway..."
colonies service add --spec "$EXAMPLES_DIR/arrowhead-c1-gateway.json"

echo ""
echo "==============================================="
echo "Arrowhead Cloud C1 deployment initiated!"
echo "==============================================="
echo ""
echo "Check service status with:"
echo "  colonies service ls --kind ExecutorDeployment"
echo ""
echo "View individual services:"
echo "  colonies service get --name c1-database"
echo "  colonies service get --name c1-serviceregistry"
echo "  colonies service get --name c1-authorization"
echo "  colonies service get --name c1-orchestrator"
echo "  colonies service get --name c1-eventhandler"
echo "  colonies service get --name c1-gatekeeper"
echo "  colonies service get --name c1-gateway"
echo ""
echo "View all Arrowhead services (filtering not yet implemented):"
echo "  colonies service ls"
