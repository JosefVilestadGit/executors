#!/bin/bash

# Cleanup Arrowhead Cloud C1 services
# Removes all Arrowhead C1 components in reverse order

set -e

echo "Cleaning up Arrowhead Cloud C1..."
echo ""

echo "Removing Gateway..."
colonies service remove --name c1-gateway || true

echo "Removing Gatekeeper..."
colonies service remove --name c1-gatekeeper || true

echo "Removing Event Handler..."
colonies service remove --name c1-eventhandler || true

echo "Removing Orchestrator..."
colonies service remove --name c1-orchestrator || true

echo "Removing Authorization..."
colonies service remove --name c1-authorization || true

echo "Removing Service Registry..."
colonies service remove --name c1-serviceregistry || true

echo "Removing Database..."
colonies service remove --name c1-database || true

echo ""
echo "Arrowhead Cloud C1 cleanup complete!"
