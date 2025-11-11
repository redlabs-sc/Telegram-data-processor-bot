#!/bin/bash

# Local Bot API Server Setup Script
# This script sets up a local Telegram Bot API server for handling large files (up to 2GB)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_color() {
    printf "${1}${2}${NC}\n"
}

print_color $BLUE "=========================================="
print_color $BLUE "  Local Bot API Server Setup"
print_color $BLUE "=========================================="
echo

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    print_color $RED "Error: Docker is not installed!"
    print_color $YELLOW "Please install Docker first: https://docs.docker.com/get-docker/"
    exit 1
fi

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null; then
    print_color $RED "Error: Docker Compose is not installed!"
    print_color $YELLOW "Please install Docker Compose: https://docs.docker.com/compose/install/"
    exit 1
fi

# Load environment variables
if [ -f .env ]; then
    source .env
    print_color $GREEN "✓ Loaded configuration from .env file"
else
    print_color $RED "Error: .env file not found!"
    print_color $YELLOW "Please create .env file with API_ID and API_HASH"
    exit 1
fi

# Check required environment variables
if [ -z "$API_ID" ] || [ -z "$API_HASH" ]; then
    print_color $RED "Error: Missing required environment variables!"
    print_color $YELLOW "Please set API_ID and API_HASH in your .env file"
    print_color $YELLOW "You can get these from https://my.telegram.org/apps"
    exit 1
fi

print_color $BLUE "Configuration:"
echo "  API_ID: $API_ID"
echo "  API_HASH: ${API_HASH:0:8}..."
echo "  Local API URL: ${LOCAL_BOT_API_URL:-http://localhost:8081}"
echo

# Check if the service is already running
if docker-compose -f docker-compose.local-api.yml ps | grep -q "telegram-bot-api-server.*Up"; then
    print_color $YELLOW "⚠ Local Bot API Server is already running"
    
    read -p "Do you want to restart it? (y/n): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        print_color $BLUE "Restarting Local Bot API Server..."
        docker-compose -f docker-compose.local-api.yml down
        docker-compose -f docker-compose.local-api.yml up -d
    else
        print_color $GREEN "✓ Using existing Local Bot API Server"
        exit 0
    fi
else
    print_color $BLUE "Starting Local Bot API Server..."
    
    # Start the local Bot API server
    docker-compose -f docker-compose.local-api.yml up -d
fi

# Wait for the server to be ready
print_color $BLUE "Waiting for Local Bot API Server to start..."
for i in {1..30}; do
    if curl -s http://localhost:8081/health > /dev/null 2>&1; then
        break
    fi
    if [ $i -eq 30 ]; then
        print_color $RED "Error: Local Bot API Server failed to start within 30 seconds"
        print_color $YELLOW "Check logs with: docker-compose -f docker-compose.local-api.yml logs"
        exit 1
    fi
    printf "."
    sleep 1
done
echo

print_color $GREEN "✓ Local Bot API Server is running!"
echo

# Verify the setup
print_color $BLUE "Verifying setup..."

# Test the health endpoint
if curl -s http://localhost:8081/health > /dev/null; then
    print_color $GREEN "✓ Health check passed"
else
    print_color $RED "✗ Health check failed"
fi

# Show container status
print_color $BLUE "Container status:"
docker-compose -f docker-compose.local-api.yml ps

echo
print_color $GREEN "=========================================="
print_color $GREEN "  Setup Complete!"
print_color $GREEN "=========================================="
echo

print_color $GREEN "✓ Local Bot API Server is running on port 8081"
print_color $GREEN "✓ Your bot can now handle files up to 2GB"
print_color $GREEN "✓ Configuration is already set in .env file"
echo

print_color $BLUE "Next steps:"
echo "  1. Build and start your bot: go run ."
echo "  2. The bot will automatically use the Local Bot API Server"
echo "  3. Test with a large file (>20MB) to verify it works"
echo

print_color $BLUE "Useful commands:"
echo "  View logs:    docker-compose -f docker-compose.local-api.yml logs -f"
echo "  Stop server:  docker-compose -f docker-compose.local-api.yml down"
echo "  Restart:      docker-compose -f docker-compose.local-api.yml restart"
echo

print_color $YELLOW "Note: The first large file download may take longer as the server initializes"