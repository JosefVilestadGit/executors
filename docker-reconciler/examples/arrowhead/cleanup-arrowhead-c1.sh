#!/bin/bash

# Cleanup Arrowhead Cloud C1 blueprints
# Removes all Arrowhead C1 components in reverse order

set -e

echo "Cleaning up Arrowhead Cloud C1..."
echo ""

echo "Removing Gateway..."
colonies blueprint remove --name c1-gateway || true

echo "Removing Gatekeeper..."
colonies blueprint remove --name c1-gatekeeper || true

echo "Removing Event Handler..."
colonies blueprint remove --name c1-eventhandler || true

echo "Removing Orchestrator..."
colonies blueprint remove --name c1-orchestrator || true

echo "Removing Authorization..."
colonies blueprint remove --name c1-authorization || true

echo "Removing Service Registry..."
colonies blueprint remove --name c1-serviceregistry || true

echo "Removing Database..."
colonies blueprint remove --name c1-database || true

echo ""
echo "Arrowhead Cloud C1 cleanup complete!"
