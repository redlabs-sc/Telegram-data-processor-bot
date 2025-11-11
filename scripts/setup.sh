#!/bin/bash

# Telegram Archive Bot - Setup Script
# This script installs dependencies, sets up the system, and compiles everything

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Get script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo -e "${CYAN}=== Telegram Archive Bot Setup ===${NC}"
echo "Project Root: $PROJECT_ROOT"
echo

# Function to check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        echo "$ID"
    elif command_exists lsb_release; then
        lsb_release -si | tr '[:upper:]' '[:lower:]'
    else
        echo "unknown"
    fi
}

# Function to install system dependencies
install_system_dependencies() {
    echo -e "${BLUE}🔧 Installing system dependencies...${NC}"
    
    local os=$(detect_os)
    echo "Detected OS: $os"
    
    case "$os" in
        "ubuntu"|"debian")
            echo -e "${YELLOW}📦 Updating package lists...${NC}"
            sudo apt-get update
            
            echo -e "${YELLOW}📦 Installing dependencies...${NC}"
            sudo apt-get install -y \
                git \
                curl \
                wget \
                build-essential \
                cmake \
                sqlite3 \
                libsqlite3-dev \
                openssl \
                libssl-dev \
                pkg-config \
                gperf \
                php-cli \
                unzip \
                p7zip-full \
                zip
            ;;
        "centos"|"rhel"|"fedora")
            echo -e "${YELLOW}📦 Installing dependencies...${NC}"
            if command_exists dnf; then
                sudo dnf install -y \
                    git \
                    curl \
                    wget \
                    gcc \
                    gcc-c++ \
                    make \
                    cmake \
                    sqlite \
                    sqlite-devel \
                    openssl \
                    openssl-devel \
                    pkgconfig \
                    gperf \
                    php-cli \
                    unzip \
                    p7zip \
                    zip
            else
                sudo yum install -y \
                    git \
                    curl \
                    wget \
                    gcc \
                    gcc-c++ \
                    make \
                    cmake \
                    sqlite \
                    sqlite-devel \
                    openssl \
                    openssl-devel \
                    pkgconfig \
                    gperf \
                    php-cli \
                    unzip \
                    p7zip \
                    zip
            fi
            ;;
        "arch")
            echo -e "${YELLOW}📦 Installing dependencies...${NC}"
            sudo pacman -S --noconfirm \
                git \
                curl \
                wget \
                base-devel \
                cmake \
                sqlite \
                openssl \
                pkgconf \
                gperf \
                php \
                unzip \
                p7zip \
                zip
            ;;
        *)
            echo -e "${YELLOW}⚠️  Unknown OS: $os${NC}"
            echo -e "${YELLOW}Please install the following packages manually:${NC}"
            echo "  - git, curl, wget"
            echo "  - build-essential (gcc, g++, make)"
            echo "  - cmake"
            echo "  - sqlite3, libsqlite3-dev"
            echo "  - openssl, libssl-dev"
            echo "  - pkg-config, gperf"
            echo "  - php-cli"
            echo "  - unzip, p7zip-full, zip"
            ;;
    esac
    
    echo -e "${GREEN}✅ System dependencies installed${NC}"
}

# Function to install Go
install_go() {
    echo -e "${BLUE}🐹 Installing Go...${NC}"
    
    if command_exists go; then
        local go_version=$(go version | grep -o 'go[0-9]\+\.[0-9]\+\.[0-9]\+' || echo "unknown")
        echo -e "${GREEN}✓ Go already installed: $go_version${NC}"
        return 0
    fi
    
    local go_version="1.23.0"
    local go_arch="amd64"
    
    # Detect architecture
    local arch=$(uname -m)
    case "$arch" in
        "x86_64") go_arch="amd64" ;;
        "aarch64"|"arm64") go_arch="arm64" ;;
        "armv6l") go_arch="armv6l" ;;
        "armv7l") go_arch="armv7l" ;;
        *) echo -e "${RED}Unsupported architecture: $arch${NC}"; exit 1 ;;
    esac
    
    local go_package="go${go_version}.linux-${go_arch}.tar.gz"
    local go_url="https://golang.org/dl/${go_package}"
    
    echo -e "${YELLOW}📥 Downloading Go ${go_version}...${NC}"
    cd /tmp
    wget -q "$go_url" || curl -L -o "$go_package" "$go_url"
    
    echo -e "${YELLOW}📦 Installing Go...${NC}"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "$go_package"
    
    # Add Go to PATH
    if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    fi
    
    export PATH=$PATH:/usr/local/go/bin
    
    echo -e "${GREEN}✅ Go installed: $(go version)${NC}"
}

# Function to create required directories
create_directories() {
    echo -e "${BLUE}📁 Creating required directories...${NC}"
    
    local dirs=(
        "data"
        "logs" 
        "temp"
        "app/extraction/files/all"
        "app/extraction/files/txt"
        "app/extraction/files/pass"
        "app/extraction/files/done"
        "app/extraction/files/errors"
        "app/extraction/files/nopass"
        "app/extraction/files/etbanks"
        "app/bin"
        "scripts"
    )
    
    cd "$PROJECT_ROOT"
    for dir in "${dirs[@]}"; do
        if [ ! -d "$dir" ]; then
            mkdir -p "$dir"
            echo -e "${GREEN}✓ Created: $dir${NC}"
        else
            echo -e "${YELLOW}✓ Exists: $dir${NC}"
        fi
    done
}

# Function to setup .env file
setup_env_file() {
    echo -e "${BLUE}⚙️  Setting up .env configuration...${NC}"
    
    if [ -f "$PROJECT_ROOT/.env" ]; then
        echo -e "${YELLOW}⚠️  .env file already exists${NC}"
        echo -e "${YELLOW}Please ensure it contains all required variables${NC}"
        return 0
    fi
    
    cat > "$PROJECT_ROOT/.env" << 'EOF'
# Telegram Bot configuration
API_ID=YOUR_API_ID
API_HASH="YOUR_API_HASH"
BOT_TOKEN="YOUR_BOT_TOKEN"
TELEGRAM_BOT_TOKEN="YOUR_BOT_TOKEN"

# Local Bot API Server configuration (for large files >20MB)
USE_LOCAL_BOT_API=true
LOCAL_BOT_API_URL=http://localhost:8081
LOCAL_BOT_API_ENABLED=true

# Multiple admin IDs (comma-separated) - use this for multiple admins
ADMIN_IDS="YOUR_ADMIN_ID"
# Legacy single admin ID (kept for backward compatibility)
ADMIN_USER_ID=YOUR_ADMIN_ID
CHAT_ID=YOUR_ADMIN_ID

# Converter configuration
CONVERT_INPUT_DIR=app/extraction/files/pass
CONVERT_OUTPUT_FILE=app/extraction/
CONVERT_OUTPUT_PREFIX=redscorpion_

# Database Configuration
DATABASE_PATH=data/bot.db

# Logging Configuration
LOG_LEVEL=INFO
LOG_FILE=logs/bot.log
LOG_ROTATION=true

# --- Pipeline Queue and Worker Settings ---
MAX_QUEUE_SIZE=1000
MAX_WORKERS=5
WORKER_TIMEOUT_MINUTES=30

# File Processing Configuration
MAX_FILE_SIZE_MB=4096
TEMP_DIR=temp
EXTRACTION_DIR=app/extraction
DOWNLOAD_TIMEOUT=30m
EXTRACTION_TIMEOUT=60m
CONVERSION_TIMEOUT=30m

# Worker Configuration
MAX_CONCURRENT_DOWNLOADS=3
MAX_RETRY_ATTEMPTS=3
RETRY_DELAY=5s

# Security Configuration
ALLOWED_FILE_TYPES=zip,rar,txt
SCAN_UPLOADS=true

# Rate Limiting Configuration
TELEGRAM_RATE_LIMIT=1s
FLOOD_WAIT_BUFFER=5s

# Cleanup Configuration
AUTO_CLEANUP=true
CLEANUP_INTERVAL=1h
RETENTION_PERIOD=7d
TEMP_FILE_LIFETIME=24h

# Database settings
DB_MAX_CONNECTIONS=10
DB_CONNECTION_TIMEOUT_SECONDS=30

# Security settings
ENABLE_FILE_VALIDATION=true
ENABLE_VIRUS_SCAN=false
MAX_ADMIN_SESSIONS=5

# Monitoring and alerts
ENABLE_METRICS=true
METRICS_PORT=8080
ALERT_ON_QUEUE_FULL=true
ALERT_ON_WORKER_FAILURE=true
EOF
    
    echo -e "${GREEN}✅ .env template created${NC}"
    echo -e "${YELLOW}⚠️  Please edit .env and fill in your actual values:${NC}"
    echo "  - API_ID and API_HASH from https://my.telegram.org/apps"
    echo "  - BOT_TOKEN from @BotFather"
    echo "  - ADMIN_IDS with your Telegram user ID"
}

# Function to compile extraction tools
compile_extraction_tools() {
    echo -e "${BLUE}🔨 Compiling extraction tools...${NC}"
    
    cd "$PROJECT_ROOT/app/extraction"
    
    # Make extract.go executable
    if [ -f "extract.go" ]; then
        echo -e "${YELLOW}📦 Compiling extract.go...${NC}"
        go build -o extract extract.go
        chmod +x extract
        mv extract extract.go
        echo -e "${GREEN}✓ extract.go compiled${NC}"
    else
        echo -e "${RED}✗ extract.go not found${NC}"
    fi
    
    # Make convert.go executable
    if [ -f "convert.go" ]; then
        echo -e "${YELLOW}📦 Compiling convert.go...${NC}"
        go build -o convert convert.go
        chmod +x convert
        mv convert convert.go
        echo -e "${GREEN}✓ convert.go compiled${NC}"
    else
        echo -e "${RED}✗ convert.go not found${NC}"
    fi
    
    # Create pass.txt if it doesn't exist
    if [ ! -f "pass.txt" ]; then
        touch pass.txt
        echo -e "${GREEN}✓ Created pass.txt${NC}"
    fi
}

# Function to compile bot
compile_bot() {
    echo -e "${BLUE}🤖 Compiling bot...${NC}"
    
    cd "$PROJECT_ROOT"
    
    echo -e "${YELLOW}📦 Downloading Go modules...${NC}"
    go mod download
    
    echo -e "${YELLOW}📦 Building bot binary...${NC}"
    go build -o telegram-archive-bot .
    chmod +x telegram-archive-bot
    
    echo -e "${GREEN}✅ Bot compiled successfully${NC}"
}

# Function to setup Local Bot API
setup_local_bot_api() {
    echo -e "${BLUE}🌐 Setting up Local Bot API...${NC}"
    
    cd "$PROJECT_ROOT"
    
    # Make scripts executable
    chmod +x scripts/*.sh
    
    echo -e "${YELLOW}📦 Building Local Bot API (this may take several minutes)...${NC}"
    ./scripts/build-native-api.sh
    
    echo -e "${GREEN}✅ Local Bot API setup complete${NC}"
}

# Function to run tests
run_tests() {
    echo -e "${BLUE}🧪 Running basic tests...${NC}"
    
    cd "$PROJECT_ROOT"
    
    # Test binary execution
    if [ -x "telegram-archive-bot" ]; then
        echo -e "${GREEN}✓ Bot binary is executable${NC}"
    else
        echo -e "${RED}✗ Bot binary is not executable${NC}"
        return 1
    fi
    
    # Test extraction tools
    cd app/extraction
    if [ -x "extract.go" ] && [ -x "convert.go" ]; then
        echo -e "${GREEN}✓ Extraction tools are executable${NC}"
    else
        echo -e "${RED}✗ Extraction tools are not executable${NC}"
        return 1
    fi
    
    # Test Local Bot API
    cd "$PROJECT_ROOT"
    if [ -x "app/bin/telegram-bot-api" ]; then
        echo -e "${GREEN}✓ Local Bot API binary is executable${NC}"
    else
        echo -e "${RED}✗ Local Bot API binary is not executable${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✅ All tests passed${NC}"
}

# Main setup function
main() {
    echo -e "${BLUE}🚀 Starting setup process...${NC}"
    
    # Install system dependencies
    install_system_dependencies
    
    # Install Go
    install_go
    
    # Create directories
    create_directories
    
    # Setup .env file
    setup_env_file
    
    # Compile extraction tools
    compile_extraction_tools
    
    # Compile bot
    compile_bot
    
    # Setup Local Bot API
    setup_local_bot_api
    
    # Run tests
    run_tests
    
    echo -e "\n${GREEN}🎉 Setup completed successfully!${NC}"
    echo -e "${CYAN}📝 Next steps:${NC}"
    echo "  1. Edit .env file with your actual configuration values"
    echo "  2. Run './run.sh' to start the bot system"
    echo "  3. Check 'tail -f logs/bot.log' for runtime logs"
    echo
    echo -e "${YELLOW}⚠️  Don't forget to configure .env before running!${NC}"
}

# Run main function
main "$@"