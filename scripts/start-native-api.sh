#!/bin/bash

# Native Telegram Bot API Server Management Script
# Starts, stops, and manages the native telegram-bot-api binary

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

# Load environment variables
if [ -f .env ]; then
    source .env
    print_color $GREEN "✓ Loaded configuration from .env file"
else
    print_color $RED "Error: .env file not found!"
    exit 1
fi

# Check required environment variables
if [ -z "$API_ID" ] || [ -z "$API_HASH" ]; then
    print_color $RED "Error: Missing required environment variables!"
    print_color $YELLOW "Please set API_ID and API_HASH in your .env file"
    exit 1
fi

# Configuration
BINARY_PATH="app/bin/telegram-bot-api"
PID_FILE="app/bin/telegram-bot-api.pid"
LOG_FILE="logs/telegram-bot-api.log"
API_PORT="8081"

# Function to check if binary exists
check_binary() {
    if [ ! -f "$BINARY_PATH" ]; then
        print_color $RED "Error: telegram-bot-api binary not found!"
        print_color $YELLOW "Run: ./scripts/build-native-api.sh to build it"
        exit 1
    fi
}

# Function to check if API server is running
is_running() {
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            return 0
        else
            rm -f "$PID_FILE"
            return 1
        fi
    fi
    return 1
}

# Function to start the API server
start_server() {
    if is_running; then
        print_color $YELLOW "⚠ Telegram Bot API Server is already running (PID: $(cat $PID_FILE))"
        return 0
    fi
    
    print_color $BLUE "Starting Native Telegram Bot API Server..."
    
    # Create logs directory
    mkdir -p logs
    
    # Start the server in background (Native Local Bot API Server supports large files by default)
    nohup "$BINARY_PATH" \
        --api-id="$API_ID" \
        --api-hash="$API_HASH" \
        --http-port="$API_PORT" \
        --local \
        --verbosity=1 \
        > "$LOG_FILE" 2>&1 &
    
    # Save PID
    echo $! > "$PID_FILE"
    local pid=$(cat "$PID_FILE")
    
    print_color $BLUE "Waiting for server to start..."
    
    # Wait for server to be ready
    for i in {1..30}; do
        if curl -s "http://localhost:$API_PORT/health" > /dev/null 2>&1; then
            break
        fi
        if [ $i -eq 30 ]; then
            print_color $RED "Error: Server failed to start within 30 seconds"
            stop_server
            exit 1
        fi
        printf "."
        sleep 1
    done
    echo
    
    print_color $GREEN "✓ Native Telegram Bot API Server started successfully!"
    print_color $BLUE "  PID: $pid"
    print_color $BLUE "  Port: $API_PORT"
    print_color $BLUE "  Logs: $LOG_FILE"
}

# Function to stop the API server
stop_server() {
    if ! is_running; then
        print_color $YELLOW "⚠ Telegram Bot API Server is not running"
        return 0
    fi
    
    local pid=$(cat "$PID_FILE")
    print_color $BLUE "Stopping Telegram Bot API Server (PID: $pid)..."
    
    # Send TERM signal
    kill "$pid" 2>/dev/null || true
    
    # Wait for graceful shutdown
    for i in {1..10}; do
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
        sleep 1
    done
    
    # Force kill if still running
    if kill -0 "$pid" 2>/dev/null; then
        print_color $YELLOW "Forcing shutdown..."
        kill -9 "$pid" 2>/dev/null || true
    fi
    
    rm -f "$PID_FILE"
    print_color $GREEN "✓ Telegram Bot API Server stopped"
}

# Function to show status
show_status() {
    if is_running; then
        local pid=$(cat "$PID_FILE")
        print_color $GREEN "✓ Telegram Bot API Server is running"
        print_color $BLUE "  PID: $pid"
        print_color $BLUE "  Port: $API_PORT"
        print_color $BLUE "  Health: $(curl -s http://localhost:$API_PORT/health 2>/dev/null || echo 'NOT_RESPONDING')"
    else
        print_color $RED "✗ Telegram Bot API Server is not running"
    fi
}

# Function to show logs
show_logs() {
    if [ -f "$LOG_FILE" ]; then
        tail -f "$LOG_FILE"
    else
        print_color $YELLOW "No log file found"
    fi
}

# Main script logic
case "${1:-start}" in
    "start")
        check_binary
        start_server
        ;;
    "stop")
        stop_server
        ;;
    "restart")
        check_binary
        stop_server
        start_server
        ;;
    "status")
        show_status
        ;;
    "logs")
        show_logs
        ;;
    "health")
        curl -s "http://localhost:$API_PORT/health" || print_color $RED "Server not responding"
        ;;
    *)
        print_color $BLUE "Usage: $0 {start|stop|restart|status|logs|health}"
        print_color $BLUE "  start   - Start the API server"
        print_color $BLUE "  stop    - Stop the API server"
        print_color $BLUE "  restart - Restart the API server"
        print_color $BLUE "  status  - Show server status"
        print_color $BLUE "  logs    - Show server logs (tail -f)"
        print_color $BLUE "  health  - Check server health"
        exit 1
        ;;
esac