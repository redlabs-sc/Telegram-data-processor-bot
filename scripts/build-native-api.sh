#!/bin/bash

# Native Telegram Bot API Server Build Script
# Builds telegram-bot-api from source and places it in the project

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

print_color $BLUE "==========================================="
print_color $BLUE "  Native Telegram Bot API Server Build"
print_color $BLUE "==========================================="
echo

# Check if already built
if [ -f "app/bin/telegram-bot-api" ]; then
    print_color $YELLOW "⚠ Native telegram-bot-api already exists"
    read -p "Do you want to rebuild it? (y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_color $GREEN "✓ Using existing binary"
        exit 0
    fi
fi

print_color $BLUE "Installing build dependencies..."

# Install required packages
if command -v apt &> /dev/null; then
    echo "0911" | sudo -S apt update
    echo "0911" | sudo -S apt install -y cmake g++ libssl-dev zlib1g-dev git
elif command -v yum &> /dev/null; then
    sudo yum install -y cmake gcc-c++ openssl-devel zlib-devel git
elif command -v pacman &> /dev/null; then
    sudo pacman -S --needed cmake gcc openssl zlib git
else
    print_color $RED "Unsupported package manager. Please install: cmake, g++, libssl-dev, zlib1g-dev manually"
    exit 1
fi

print_color $GREEN "✓ Dependencies installed"

# Create directories
mkdir -p app/bin
mkdir -p app/telegram-bot-api-src

print_color $BLUE "Cloning telegram-bot-api source..."

# Clone or update source
if [ -d "app/telegram-bot-api-src/.git" ]; then
    cd app/telegram-bot-api-src
    git pull
    cd ../..
else
    rm -rf app/telegram-bot-api-src
    git clone --recursive https://github.com/tdlib/telegram-bot-api.git app/telegram-bot-api-src
fi

print_color $GREEN "✓ Source code ready"

print_color $BLUE "Building telegram-bot-api..."

# Build the binary
cd app/telegram-bot-api-src
mkdir -p build
cd build

cmake -DCMAKE_BUILD_TYPE=Release ..
cmake --build . --target telegram-bot-api -j$(nproc)

# Copy binary to project bin directory
cp telegram-bot-api ../../bin/

cd ../../..

# Verify binary
if [ -f "app/bin/telegram-bot-api" ]; then
    print_color $GREEN "✓ telegram-bot-api built successfully"
    
    # Make executable
    chmod +x app/bin/telegram-bot-api
    
    # Show binary info
    ls -la app/bin/telegram-bot-api
    print_color $BLUE "Binary size: $(du -h app/bin/telegram-bot-api | cut -f1)"
else
    print_color $RED "✗ Build failed - binary not found"
    exit 1
fi

print_color $GREEN "==========================================="
print_color $GREEN "  Build Complete!"
print_color $GREEN "==========================================="
echo

print_color $GREEN "✓ Native telegram-bot-api is ready at app/bin/telegram-bot-api"
print_color $GREEN "✓ No Docker dependency required"
print_color $GREEN "✓ Ready for large file processing (up to 2GB)"
echo

print_color $BLUE "Next steps:"
echo "  1. Run: ./scripts/start-native-api.sh"
echo "  2. Build and start your bot: go run ."
echo "  3. Test with large files to verify functionality"
echo

print_color $YELLOW "Note: The native binary provides better performance than Docker for large files"