#!/bin/bash

# Telegram Archive Bot Unified Starter
# Automatically starts Local Bot API Server and the main bot application

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
        ./scripts/start-native-api.sh stop
    fi
    
    print_color $GREEN "Cleanup completed"
    exit $exit_code
}

# Function to check if bot binary exists
check_bot_binary() {
    if [ ! -f "telegram-archive-bot" ]; then
        print_warning "Bot binary not found. Building..."
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

# Function to check if native API binary exists
check_api_binary() {
    if [ ! -f "app/bin/telegram-bot-api" ]; then
        print_warning "Native Bot API binary not found. Building..."
        if [ ! -f "scripts/build-native-api.sh" ]; then
            print_error "Build script not found. Please ensure scripts/build-native-api.sh exists."
            exit 1
        fi
        
        print_step "This will take several minutes to compile..."
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
    if ./scripts/start-native-api.sh status > /dev/null 2>&1; then
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
    
    # Wait a moment for server to be fully ready
    sleep 2
}

# Function to start the bot
start_bot() {
    print_step "Starting Telegram Archive Bot..."
    
    # Start bot in background and capture PID
    ./telegram-archive-bot &
    BOT_PID=$!
    
    # Wait a moment to check if bot started successfully
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
    print_header "System Status Monitor"
    print_color $CYAN "Press Ctrl+C to stop both services gracefully"
    echo
    
    while true; do
        # Clear previous status (move cursor up and clear lines)
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

# Function to show status
show_status() {
    print_header "Service Status"
    
    # Local Bot API Server status
    print_step "Local Bot API Server:"
    ./scripts/start-native-api.sh status
    echo
    
    # Bot status
    print_step "Telegram Archive Bot:"
    if [ ! -z "$BOT_PID" ] && kill -0 "$BOT_PID" 2>/dev/null; then
        print_success "Running (PID: $BOT_PID)"
        
        # Show recent logs if available
        if [ -f "logs/bot.log" ]; then
            echo
            print_step "Recent bot logs (last 5 lines):"
            tail -n 5 logs/bot.log 2>/dev/null || echo "No recent logs available"
        fi
    else
        print_error "Not running"
    fi
    echo
}

# Function to show help
show_help() {
    print_header "Telegram Archive Bot Unified Starter"
    
    print_color $BOLD "Usage:"
    echo "  $0 [COMMAND]"
    echo
    
    print_color $BOLD "Commands:"
    echo "  start     Start both Local Bot API Server and Telegram Bot (default)"
    echo "  stop      Stop both services"
    echo "  restart   Restart both services"
    echo "  status    Show status of both services"
    echo "  logs      Show live logs from the bot"
    echo "  api-logs  Show live logs from Local Bot API Server"
    echo "  help      Show this help message"
    echo
    
    print_color $BOLD "Examples:"
    echo "  $0                # Start both services"
    echo "  $0 start          # Start both services"
    echo "  $0 status         # Check service status"
    echo "  $0 logs           # Monitor bot logs"
    echo "  $0 stop           # Stop both services"
    echo
    
    print_color $BOLD "Features:"
    echo "  • Automatic dependency checking and building"
    echo "  • Graceful shutdown on Ctrl+C"
    echo "  • Process monitoring and status display"
    echo "  • Large file support (1GB-4GB) via native Local Bot API"
    echo "  • Zero Docker dependency"
    echo
}

# Set up signal handling
trap cleanup SIGINT SIGTERM

# Main script logic
case "${1:-start}" in
    "start")
        print_header "Starting Telegram Archive Bot System"
        
        # Pre-flight checks
        print_step "Running pre-flight checks..."
        check_api_binary
        check_bot_binary
        echo
        
        # Start services
        start_api_server
        start_bot
        
        if [ $? -eq 0 ]; then
            echo
            print_success "All services started successfully!"
            echo
            print_color $CYAN "System ready for large file processing (1GB-4GB)"
            print_color $CYAN "Send files to your bot to begin processing"
            echo
            
            # Start monitoring
            monitor_processes
        else
            print_error "Failed to start all services"
            cleanup
            exit 1
        fi
        ;;
        
    "stop")
        print_header "Stopping Telegram Archive Bot System"
        cleanup
        ;;
        
    "restart")
        print_header "Restarting Telegram Archive Bot System"
        print_step "Stopping services..."
        cleanup
        sleep 2
        echo
        print_step "Starting services..."
        exec "$0" start
        ;;
        
    "status")
        show_status
        ;;
        
    "logs")
        print_header "Telegram Bot Logs (Live)"
        print_color $CYAN "Press Ctrl+C to exit log viewer"
        echo
        if [ -f "logs/bot.log" ]; then
            tail -f logs/bot.log
        else
            print_warning "No log file found. Start the bot first."
        fi
        ;;
        
    "api-logs")
        print_header "Local Bot API Server Logs (Live)"
        print_color $CYAN "Press Ctrl+C to exit log viewer"
        echo
        if [ -f "logs/telegram-bot-api.log" ]; then
            tail -f logs/telegram-bot-api.log
        else
            print_warning "No API log file found. Start the API server first."
        fi
        ;;
        
    "help"|"-h"|"--help")
        show_help
        ;;
        
    *)
        print_error "Unknown command: $1"
        echo
        show_help
        exit 1
        ;;
esac