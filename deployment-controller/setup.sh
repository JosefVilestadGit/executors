#!/bin/bash

# Setup script for deployment controller
# This script helps you get started quickly

set -e

echo "=========================================="
echo "Deployment Controller Setup"
echo "=========================================="
echo ""

# Check if .env exists
if [ -f .env ]; then
    echo "✓ .env file already exists"
    read -p "Do you want to reconfigure? (y/N): " reconfigure
    if [[ ! "$reconfigure" =~ ^[Yy]$ ]]; then
        echo "Using existing .env file"
        USE_EXISTING=true
    fi
fi

if [ "$USE_EXISTING" != "true" ]; then
    echo "Creating .env file from template..."
    cp .env.example .env
    echo "✓ Created .env file"
    echo ""

    # Prompt for configuration
    echo "Please provide your ColonyOS configuration:"
    echo ""

    read -p "ColonyOS Server Host [localhost]: " server_host
    server_host=${server_host:-localhost}
    sed -i "s/COLONIES_SERVER_HOST=.*/COLONIES_SERVER_HOST=$server_host/" .env

    read -p "ColonyOS Server Port [50080]: " server_port
    server_port=${server_port:-50080}
    sed -i "s/COLONIES_SERVER_PORT=.*/COLONIES_SERVER_PORT=$server_port/" .env

    read -p "Use insecure connection? (true/false) [true]: " insecure
    insecure=${insecure:-true}
    sed -i "s/COLONIES_INSECURE=.*/COLONIES_INSECURE=$insecure/" .env

    read -p "Colony Name [dev]: " colony_name
    colony_name=${colony_name:-dev}
    sed -i "s/COLONIES_COLONY_NAME=.*/COLONIES_COLONY_NAME=$colony_name/" .env

    read -p "Colony Private Key: " colony_prvkey
    if [ -n "$colony_prvkey" ]; then
        sed -i "s/COLONIES_COLONY_PRVKEY=.*/COLONIES_COLONY_PRVKEY=$colony_prvkey/" .env
    fi

    read -p "Executor Name [deployment-controller-1]: " executor_name
    executor_name=${executor_name:-deployment-controller-1}
    sed -i "s/COLONIES_EXECUTOR_NAME=.*/COLONIES_EXECUTOR_NAME=$executor_name/" .env

    echo ""
    echo "✓ Configuration saved to .env"
fi

echo ""
echo "Checking Docker..."
if ! command -v docker &> /dev/null; then
    echo "✗ Docker is not installed"
    echo "Please install Docker: https://docs.docker.com/get-docker/"
    exit 1
fi
echo "✓ Docker is installed"

echo ""
echo "Checking Docker daemon..."
if ! docker info &> /dev/null; then
    echo "✗ Docker daemon is not running"
    echo "Please start Docker"
    exit 1
fi
echo "✓ Docker daemon is running"

echo ""
echo "Checking docker-compose..."
if ! command -v docker-compose &> /dev/null; then
    echo "✗ docker-compose is not installed"
    echo "Please install docker-compose: https://docs.docker.com/compose/install/"
    exit 1
fi
echo "✓ docker-compose is installed"

echo ""
echo "Creating Docker network (if needed)..."
if docker network inspect colonies-network &> /dev/null; then
    echo "✓ Network 'colonies-network' already exists"
else
    docker network create colonies-network
    echo "✓ Created network 'colonies-network'"
fi

echo ""
echo "=========================================="
echo "Setup Complete!"
echo "=========================================="
echo ""
echo "Next steps:"
echo ""
echo "1. Review your configuration:"
echo "   cat .env"
echo ""
echo "2. Build and start the controller:"
echo "   docker-compose up -d"
echo ""
echo "3. View logs:"
echo "   docker-compose logs -f"
echo ""
echo "4. Check status:"
echo "   docker-compose ps"
echo ""
echo "For more information, see:"
echo "  - README.md (general documentation)"
echo "  - DOCKER-DEPLOYMENT.md (Docker-specific guide)"
echo ""
