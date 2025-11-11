#!/bin/bash

# Telegram Archive Bot Development Starter
# Optimized for development - no production build components

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

print_color() {
    printf "${1}${2}${NC}\n"
}

print_header() {
    echo
    print_color $CYAN "═══════════════════════════════════════════════════════════════"
    print_color $CYAN "  ${1}"
    print_color $CYAN "═══════════════════════════════════════════════════════════════"
    echo
}

print_step() {
    print_color $BLUE "▶ ${1}"
}

print_success() {
    print_color $GREEN "✓ ${1}"
}

print_warning() {
    print_color $YELLOW "⚠ ${1}"
}

print_error() {
    print_color $RED "✗ ${1}"
}

# Trap to handle cleanup on exit
cleanup() {
    local exit_code=$?
    echo
    print_color $YELLOW "Cleanup initiated..."
    
    # Stop the bot if it's running
    if [ ! -z "$BOT_PID" ] && kill -0 "$BOT_PID" 2>/dev/null; then
        print_step "Stopping Telegram Archive Bot (PID: $BOT_PID)..."
        kill -TERM "$BOT_PID" 2>/dev/null || true
        
        # Wait for graceful shutdown
        for i in {1..10}; do
            if ! kill -0 "$BOT_PID" 2>/dev/null; then
                break
            fi
            sleep 1
        done
        
        # Force kill if still running
        if kill -0 "$BOT_PID" 2>/dev/null; then
            kill -9 "$BOT_PID" 2>/dev/null || true
        fi
        print_success "Bot stopped"
    fi
    
    # Stop Local Bot API Server
    if command -v ./scripts/start-native-api.sh &> /dev/null; then
        print_step "Stopping Local Bot API Server..."
        ./scripts/start-native-api.sh stop 2>/dev/null || true
    fi
    
    print_color $GREEN "Cleanup completed"
    exit $exit_code
}

# Function to check if bot binary exists
check_bot_binary() {
    if [ ! -f "telegram-archive-bot" ]; then
        print_step "Building bot binary..."
        go build -o telegram-archive-bot .
        if [ $? -eq 0 ]; then
            print_success "Bot binary built successfully"
        else
            print_error "Failed to build bot binary"
            exit 1
        fi
    else
        print_success "Bot binary found"
    fi
}

# Function to check if native API binary exists (for development)
check_api_binary() {
    if [ ! -f "app/bin/telegram-bot-api" ]; then
        print_warning "Native Bot API binary not found. Building for development..."
        if [ ! -f "scripts/build-native-api.sh" ]; then
            print_error "Build script not found. Please ensure scripts/build-native-api.sh exists."
            exit 1
        fi
        
        print_step "Building Local Bot API (this may take several minutes)..."
        print_color $CYAN "This is a one-time setup for development"
        ./scripts/build-native-api.sh
        
        if [ -f "app/bin/telegram-bot-api" ]; then
            print_success "Native Bot API binary built successfully"
        else
            print_error "Failed to build Native Bot API binary"
            exit 1
        fi
    else
        print_success "Native Bot API binary found"
    fi
}

# Function to start Local Bot API Server
start_api_server() {
    print_step "Starting Local Bot API Server..."
    
    # Check if already running
    if ./scripts/start-native-api.sh > /dev/null 2>&1; then
        print_warning "Local Bot API Server is already running"
    else
        ./scripts/start-native-api.sh start
        if [ $? -eq 0 ]; then
            print_success "Local Bot API Server started successfully"
        else
            print_error "Failed to start Local Bot API Server"
            exit 1
        fi
    fi
    
    # Wait for server to be ready
    sleep 3
}

# Function to start the bot
start_bot() {
    print_step "Starting Telegram Archive Bot..."
    
    # Start bot in background and capture PID
    ./telegram-archive-bot &
    BOT_PID=$!
    
    # Wait to check if bot started successfully
    sleep 3
    
    if kill -0 "$BOT_PID" 2>/dev/null; then
        print_success "Telegram Archive Bot started successfully (PID: $BOT_PID)"
        return 0
    else
        print_error "Bot failed to start"
        return 1
    fi
}

# Function to monitor processes
monitor_processes() {
    print_header "Development System Status Monitor"
    print_color $CYAN "Press Ctrl+C to stop both services gracefully"
    print_color $YELLOW "Note: Extract and convert tools are used as-is in development mode"
    echo
    
    while true; do
        # Clear previous status
        printf "\033[2K\r"
        
        # Check Local Bot API Server status
        if ./scripts/start-native-api.sh status > /dev/null 2>&1; then
            printf "${GREEN}✓ Local Bot API Server: Running${NC}    "
        else
            printf "${RED}✗ Local Bot API Server: Stopped${NC}    "
        fi
        
        # Check Bot status
        if [ ! -z "$BOT_PID" ] && kill -0 "$BOT_PID" 2>/dev/null; then
            printf "${GREEN}✓ Telegram Bot: Running (PID: $BOT_PID)${NC}"
        else
            printf "${RED}✗ Telegram Bot: Stopped${NC}"
            print_error "Bot process died unexpectedly"
            break
        fi
        
        sleep 5
    done
}

# Set up signal handling
trap cleanup SIGINT SIGTERM

# Main execution
print_header "Starting Telegram Archive Bot Development System"

# Pre-flight checks
print_step "Running development pre-flight checks..."
check_bot_binary
check_api_binary
echo

# Note about development mode
print_color $CYAN "Development Mode Notes:"
print_color $CYAN "• Extract and convert tools are used as standalone Go scripts"
print_color $CYAN "• No production build compilation required"
print_color $CYAN "• Local Bot API enables 4GB file support"
echo

# Start services
start_api_server
start_bot

if [ $? -eq 0 ]; then
    echo
    print_success "Development system started successfully!"
    echo
    print_color $CYAN "System ready for large file processing (up to 4GB)"
    print_color $CYAN "Send files to your bot to begin processing"
    echo
    
    # Start monitoring
    monitor_processes
else
    print_error "Failed to start development system"
    cleanup
    exit 1
fi