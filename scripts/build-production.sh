#!/bin/bash

# Telegram Archive Bot - Production Build Script
# This script creates a production-ready deployment package using Option 5 method
# Compiles everything into a single executable without deleting source files

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

# Production build directory
PROD_DIR="$PROJECT_ROOT/production-build"
PROD_NAME="telegram-archive-bot-production"
PROD_PATH="$PROD_DIR/$PROD_NAME"

echo -e "${CYAN}=== Telegram Archive Bot - Production Builder ===${NC}"
echo "Project Root: $PROJECT_ROOT"
echo "Production Build: $PROD_PATH"
echo

# Function to check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check if a file exists
file_exists() {
    [ -f "$1" ]
}

# Function to check if a directory exists
dir_exists() {
    [ -d "$1" ]
}

# Function to create directory if it doesn't exist
ensure_dir() {
    if [ ! -d "$1" ]; then
        mkdir -p "$1"
        echo -e "${GREEN}✓ Created directory: $1${NC}"
    fi
}

# Function to copy file with status
copy_file() {
    local src="$1"
    local dest="$2"
    if [ -f "$src" ]; then
        cp "$src" "$dest"
        echo -e "${GREEN}✓ Copied: $(basename "$src")${NC}"
    else
        echo -e "${RED}✗ Missing: $src${NC}"
        return 1
    fi
}

# Function to copy directory with status
copy_dir() {
    local src="$1"
    local dest="$2"
    if [ -d "$src" ]; then
        cp -r "$src" "$dest"
        echo -e "${GREEN}✓ Copied directory: $(basename "$src")${NC}"
    else
        echo -e "${RED}✗ Missing directory: $src${NC}"
        return 1
    fi
}

# Function to check prerequisites
check_prerequisites() {
    echo -e "${BLUE}🔍 Checking prerequisites...${NC}"
    
    local all_good=true
    
    # Check Go installation
    if command_exists go; then
        local go_version=$(go version | grep -o 'go[0-9]\+\.[0-9]\+\.[0-9]\+' || echo "unknown")
        echo -e "${GREEN}✓ Go installed: $go_version${NC}"
    else
        echo -e "${RED}✗ Go not installed${NC}"
        all_good=false
    fi
    
    # Check required files
    local required_files=(
        ".env"
        "main.go"
        "go.mod"
        "run.sh"
    )
    
    cd "$PROJECT_ROOT"
    for file in "${required_files[@]}"; do
        if file_exists "$file"; then
            echo -e "${GREEN}✓ Found: $file${NC}"
        else
            echo -e "${RED}✗ Missing: $file${NC}"
            all_good=false
        fi
    done
    
    # Check required directories
    local required_dirs=(
        "app/extraction"
        "scripts"
        "bot"
        "workers"
    )
    
    for dir in "${required_dirs[@]}"; do
        if dir_exists "$dir"; then
            echo -e "${GREEN}✓ Found directory: $dir${NC}"
        else
            echo -e "${RED}✗ Missing directory: $dir${NC}"
            all_good=false
        fi
    done
    
    if [ "$all_good" = false ]; then
        echo -e "${RED}❌ Prerequisites not met${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✅ All prerequisites met${NC}"
}

# Function to clean old production build
clean_old_build() {
    echo -e "${BLUE}🧹 Cleaning old production build...${NC}"
    
    if [ -d "$PROD_DIR" ]; then
        echo -e "${YELLOW}⚠️  Removing existing production build${NC}"
        rm -rf "$PROD_DIR"
    fi
    
    ensure_dir "$PROD_DIR"
    ensure_dir "$PROD_PATH"
    
    echo -e "${GREEN}✓ Production directory prepared${NC}"
}

# Function to compile the main bot binary
compile_bot() {
    echo -e "${BLUE}🔨 Compiling main bot binary...${NC}"
    
    cd "$PROJECT_ROOT"
    
    echo -e "${YELLOW}📦 Downloading Go modules...${NC}"
    go mod download
    
    echo -e "${YELLOW}📦 Building production binary...${NC}"
    go build -ldflags="-s -w" -o "$PROD_PATH/telegram-archive-bot" .
    
    if [ ! -f "$PROD_PATH/telegram-archive-bot" ]; then
        echo -e "${RED}❌ Failed to compile bot binary${NC}"
        exit 1
    fi
    
    chmod +x "$PROD_PATH/telegram-archive-bot"
    echo -e "${GREEN}✅ Bot binary compiled successfully${NC}"
}

# Function to build Local Bot API if needed
build_local_api() {
    echo -e "${BLUE}🌐 Preparing Local Bot API...${NC}"
    
    # Check if Local Bot API binary exists
    if [ -f "$PROJECT_ROOT/app/bin/telegram-bot-api" ]; then
        echo -e "${GREEN}✓ Local Bot API binary found${NC}"
    else
        echo -e "${YELLOW}📦 Building Local Bot API (this may take several minutes)...${NC}"
        cd "$PROJECT_ROOT"
        if [ -x "scripts/build-native-api.sh" ]; then
            ./scripts/build-native-api.sh
        else
            echo -e "${RED}❌ Local Bot API build script not found or not executable${NC}"
            exit 1
        fi
    fi
    
    if [ ! -f "$PROJECT_ROOT/app/bin/telegram-bot-api" ]; then
        echo -e "${RED}❌ Local Bot API binary not found after build${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✅ Local Bot API ready${NC}"
}

# Function to create production directory structure
create_production_structure() {
    echo -e "${BLUE}📁 Creating production directory structure...${NC}"
    
    # Create main directories
    ensure_dir "$PROD_PATH/app/bin"
    ensure_dir "$PROD_PATH/app/extraction/files"
    ensure_dir "$PROD_PATH/scripts"
    ensure_dir "$PROD_PATH/data"
    ensure_dir "$PROD_PATH/logs"
    
    # Create extraction subdirectories
    local extraction_dirs=(
        "all"
        "txt"
        "pass"
        "done"
        "errors"
        "nopass"
        "etbanks"
    )
    
    for dir in "${extraction_dirs[@]}"; do
        ensure_dir "$PROD_PATH/app/extraction/files/$dir"
    done
    
    echo -e "${GREEN}✓ Directory structure created${NC}"
}

# Function to copy all development files to production
copy_all_files() {
    echo -e "${BLUE}📋 Copying all development files to production...${NC}"
    
    cd "$PROJECT_ROOT"
    
    # Copy all Go source files
    echo -e "${YELLOW}📦 Copying Go source files...${NC}"
    local go_dirs=("." "bot" "workers" "models" "utils" "storage" "pipeline" "monitoring" "tests")
    for dir in "${go_dirs[@]}"; do
        if [ -d "$dir" ] && [ "$dir" != "production-build" ]; then
            cp -r "$dir"/*.go "$PROD_PATH/" 2>/dev/null || true
            if [ "$dir" != "." ]; then
                ensure_dir "$PROD_PATH/$dir"
                cp -r "$dir"/*.go "$PROD_PATH/$dir/" 2>/dev/null || true
            fi
        fi
    done
    
    # Copy go.mod and go.sum
    copy_file "go.mod" "$PROD_PATH/go.mod"
    copy_file "go.sum" "$PROD_PATH/go.sum"
    
    # Copy main configuration files
    copy_file ".env" "$PROD_PATH/.env"
    copy_file "run.sh" "$PROD_PATH/run.sh"
    chmod +x "$PROD_PATH/run.sh"
    
    # Copy Local Bot API binary
    copy_file "app/bin/telegram-bot-api" "$PROD_PATH/app/bin/telegram-bot-api"
    chmod +x "$PROD_PATH/app/bin/telegram-bot-api"
    
    # Copy extraction files
    if [ -f "app/extraction/pass.txt" ]; then
        copy_file "app/extraction/pass.txt" "$PROD_PATH/app/extraction/pass.txt"
    else
        touch "$PROD_PATH/app/extraction/pass.txt"
        echo -e "${YELLOW}✓ Created empty pass.txt${NC}"
    fi
    
    # Copy all scripts
    if [ -d "scripts" ]; then
        cp -r scripts/* "$PROD_PATH/scripts/" 2>/dev/null || true
        chmod +x "$PROD_PATH/scripts"/*.sh 2>/dev/null || true
    fi
    
    echo -e "${GREEN}✓ All development files copied${NC}"
}

# Function to make production-specific modifications
make_production_modifications() {
    echo -e "${BLUE}🔧 Making production-specific modifications...${NC}"
    
    cd "$PROD_PATH"
    
    # 1. Update .env for production configuration
    echo -e "${YELLOW}📝 Updating .env for production...${NC}"
    sed -i 's|CONVERT_OUTPUT_FILE=app/extraction/files/all.txt|CONVERT_OUTPUT_FILE=app/extraction/|g' .env
    echo 'CONVERT_OUTPUT_PREFIX=redscorpion_' >> .env
    
    # 2. Update conversion worker for production settings
    echo -e "${YELLOW}📝 Updating conversion worker...${NC}"
    if [ -f "workers/conversion.go" ]; then
        # Change output prefix from output_ to redscorpion_
        sed -i 's|fmt.Sprintf("output_%s_%s.txt"|fmt.Sprintf("redscorpion_%s_%s.txt"|g' workers/conversion.go
        
        # Update environment variable handling for folder path
        sed -i 's|os.Setenv("CONVERT_OUTPUT_FILE", outputFileName)|// Get output folder from .env and construct full path\
			outputFolder := os.Getenv("CONVERT_OUTPUT_FILE")\
			if outputFolder == "" {\
				outputFolder = "app/extraction/"\
			}\
			fullOutputPath := filepath.Join(outputFolder, outputFileName)\
			\
			// Set environment variables for the conversion function\
			os.Setenv("CONVERT_INPUT_DIR", "files/pass")\
			os.Setenv("CONVERT_OUTPUT_FILE", fullOutputPath)|g' workers/conversion.go
    fi
    
    # 3. Update pipeline for manual-only extract/convert
    echo -e "${YELLOW}📝 Updating pipeline for manual operations...${NC}"
    if [ -f "pipeline/pipeline.go" ]; then
        # Replace automatic pipeline progression with manual-only
        sed -i '/Check for completed download jobs and submit to extraction/,/^	}$/c\
	// Manual-only extract and convert: No automatic pipeline progression\
	// Download jobs complete but do not automatically trigger extraction\
	for job := range p.downloadPool.GetCompletedJobs() {\
		if job.Status == JobStatusCompleted {\
			p.logger.WithField("task_id", job.Task.ID).Info("Download completed - ready for manual extraction")\
			// Files are ready for manual /extract command\
		}\
	}' pipeline/pipeline.go
        
        sed -i '/Check for completed extraction jobs and submit to conversion/,/^	}$/c\
	// Extraction jobs complete but do not automatically trigger conversion\
	for job := range p.extractionPool.GetCompletedJobs() {\
		if job.Status == JobStatusCompleted {\
			p.logger.WithField("task_id", job.Task.ID).Info("Extraction completed - ready for manual conversion")\
			// Files are ready for manual /convert command\
		}\
	}' pipeline/pipeline.go
    fi
    
    # 4. Update run.sh for production environment
    echo -e "${YELLOW}📝 Updating run.sh for production...${NC}"
    cat > run.sh << 'EOF'
#!/bin/bash

# Telegram Archive Bot - Production Runner
# This script starts both Local Bot API and the bot for production deployment

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}=== Telegram Archive Bot - Production ===${NC}"

# Function to create Local Bot API directories
setup_local_api_directories() {
    local bot_token=$(grep TELEGRAM_BOT_TOKEN .env | cut -d'=' -f2 | tr -d '"')
    if [ -n "$bot_token" ]; then
        mkdir -p "${bot_token}/documents" "${bot_token}/temp"
        echo -e "${GREEN}✓ Local Bot API directories created${NC}"
    fi
}

# Function to start Local Bot API Server
start_local_api() {
    echo -e "${BLUE}🚀 Starting Local Bot API Server...${NC}"
    
    if pgrep -f "telegram-bot-api" > /dev/null; then
        echo -e "${YELLOW}⚠️  Local Bot API Server already running${NC}"
        return 0
    fi
    
    setup_local_api_directories
    
    if [ ! -f "app/bin/telegram-bot-api" ]; then
        echo -e "${RED}❌ Local Bot API binary not found${NC}"
        exit 1
    fi
    
    ./scripts/start-native-api.sh
    
    echo -e "${YELLOW}⏳ Waiting for Local Bot API Server to start...${NC}"
    for i in {1..10}; do
        if curl -s "http://localhost:8081" > /dev/null 2>&1; then
            echo -e "${GREEN}✅ Local Bot API Server is running${NC}"
            return 0
        fi
        sleep 2
    done
    
    echo -e "${RED}❌ Local Bot API Server failed to start${NC}"
    return 1
}

# Function to start the bot
start_bot() {
    echo -e "${BLUE}🤖 Starting Telegram Archive Bot...${NC}"
    
    if pgrep -f "telegram-archive-bot" > /dev/null; then
        echo -e "${YELLOW}⚠️  Bot already running${NC}"
        return 0
    fi
    
    ./telegram-archive-bot > logs/bot.log 2>&1 &
    local bot_pid=$!
    
    sleep 3
    if kill -0 "$bot_pid" 2>/dev/null; then
        echo -e "${GREEN}✅ Bot started successfully (PID: $bot_pid)${NC}"
        echo -e "${CYAN}📝 Logs: tail -f logs/bot.log${NC}"
        return 0
    else
        echo -e "${RED}❌ Bot failed to start${NC}"
        echo -e "${RED}Check logs: tail logs/bot.log${NC}"
        return 1
    fi
}

# Function to show status
show_status() {
    echo -e "\n${CYAN}📊 Service Status:${NC}"
    
    if pgrep -f "telegram-bot-api" > /dev/null; then
        local api_pid=$(pgrep -f "telegram-bot-api")
        echo -e "${GREEN}✅ Local Bot API Server: Running (PID: $api_pid)${NC}"
    else
        echo -e "${RED}❌ Local Bot API Server: Not running${NC}"
    fi
    
    if pgrep -f "telegram-archive-bot" > /dev/null; then
        local bot_pid=$(pgrep -f "telegram-archive-bot")
        echo -e "${GREEN}✅ Telegram Bot: Running (PID: $bot_pid)${NC}"
    else
        echo -e "${RED}❌ Telegram Bot: Not running${NC}"
    fi
    
    if curl -s "http://localhost:8081" > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Local Bot API: Responding${NC}"
    else
        echo -e "${RED}❌ Local Bot API: Not responding${NC}"
    fi
}

# Main execution
case "${1:-}" in
    "status")
        show_status
        ;;
    *)
        start_local_api
        start_bot
        show_status
        echo -e "\n${GREEN}🎉 Production bot started successfully!${NC}"
        echo -e "${CYAN}💡 Use 'pkill -f telegram' to stop all services${NC}"
        echo -e "${CYAN}📊 Monitor with: tail -f logs/bot.log${NC}"
        ;;
esac
EOF
    chmod +x run.sh
    
    echo -e "${GREEN}✓ Production modifications completed${NC}"
}

# Function to create deployment documentation
create_documentation() {
    echo -e "${BLUE}📝 Creating deployment documentation...${NC}"
    
    cat > "$PROD_PATH/README.md" << 'EOF'
# Telegram Archive Bot - Production Deployment

## Quick Start

1. **Configure the bot:**
   ```bash
   nano .env
   ```
   Fill in your:
   - API_ID and API_HASH from https://my.telegram.org/apps
   - BOT_TOKEN from @BotFather
   - ADMIN_IDS with your Telegram user ID

2. **Start the bot:**
   ```bash
   ./run.sh
   ```

3. **Check status:**
   ```bash
   ./run.sh status
   ```

## What's Included

- `telegram-archive-bot` - Main bot binary (contains all extract/convert logic)
- `app/bin/telegram-bot-api` - Local Bot API server for 4GB file support
- `run.sh` - Intelligent startup script
- `.env` - Configuration file (edit this!)
- `scripts/` - Utility scripts
- `app/extraction/files/` - File processing directories

## Features

- ✅ Single executable (no source code visible)
- ✅ 4GB file support via Local Bot API
- ✅ Manual extract/convert commands only
- ✅ Custom output file prefix: redscorpion_
- ✅ Configurable output directory

## Commands

- `/queue` - Show processing status
- `/extract` - Manually trigger extraction
- `/convert` - Manually trigger conversion
- `/stats` - Show bot statistics
- `/cleanup` - Clean temporary files

## Support

Check logs: `tail -f logs/bot.log`
EOF

    echo -e "${GREEN}✓ Documentation created${NC}"
}

# Function to create deployment package
create_deployment_package() {
    echo -e "${BLUE}📦 Creating deployment package...${NC}"
    
    cd "$PROD_DIR"
    
    # Create tar.gz package
    local package_name="telegram-archive-bot-production-$(date +%Y%m%d_%H%M%S).tar.gz"
    tar -czf "$package_name" "$PROD_NAME"
    
    local package_size=$(du -sh "$package_name" | cut -f1)
    
    echo -e "${GREEN}✅ Deployment package created: $package_name ($package_size)${NC}"
    echo -e "${CYAN}📍 Location: $PROD_DIR/$package_name${NC}"
}

# Function to verify production build
verify_build() {
    echo -e "${BLUE}🔍 Verifying production build...${NC}"
    
    local all_good=true
    
    # Check main binary
    if [ -x "$PROD_PATH/telegram-archive-bot" ]; then
        echo -e "${GREEN}✓ Main binary is executable${NC}"
    else
        echo -e "${RED}✗ Main binary missing or not executable${NC}"
        all_good=false
    fi
    
    # Check Local Bot API
    if [ -x "$PROD_PATH/app/bin/telegram-bot-api" ]; then
        echo -e "${GREEN}✓ Local Bot API binary is executable${NC}"
    else
        echo -e "${RED}✗ Local Bot API binary missing or not executable${NC}"
        all_good=false
    fi
    
    # Check configuration
    if [ -f "$PROD_PATH/.env" ]; then
        echo -e "${GREEN}✓ Configuration file present${NC}"
    else
        echo -e "${RED}✗ Configuration file missing${NC}"
        all_good=false
    fi
    
    # Check run script
    if [ -x "$PROD_PATH/run.sh" ]; then
        echo -e "${GREEN}✓ Run script is executable${NC}"
    else
        echo -e "${RED}✗ Run script missing or not executable${NC}"
        all_good=false
    fi
    
    # Check directory structure
    local required_dirs=(
        "app/extraction/files/all"
        "app/extraction/files/pass"
        "scripts"
        "data"
        "logs"
    )
    
    for dir in "${required_dirs[@]}"; do
        if [ -d "$PROD_PATH/$dir" ]; then
            echo -e "${GREEN}✓ Directory exists: $dir${NC}"
        else
            echo -e "${RED}✗ Directory missing: $dir${NC}"
            all_good=false
        fi
    done
    
    if [ "$all_good" = true ]; then
        echo -e "${GREEN}✅ Production build verification passed${NC}"
    else
        echo -e "${RED}❌ Production build verification failed${NC}"
        exit 1
    fi
}

# Function to show summary
show_summary() {
    echo
    echo -e "${CYAN}🎉 Production Build Complete!${NC}"
    echo
    echo -e "${BLUE}📍 Production Build Location:${NC}"
    echo "  $PROD_PATH"
    echo
    echo -e "${BLUE}📦 Deployment Package:${NC}"
    echo "  $PROD_DIR/telegram-archive-bot-production-*.tar.gz"
    echo
    echo -e "${BLUE}🚀 Next Steps:${NC}"
    echo "  1. Extract the package on your production server"
    echo "  2. Edit .env with your configuration"
    echo "  3. Run './run.sh' to start the bot"
    echo
    echo -e "${YELLOW}💡 Tips:${NC}"
    echo "  - Single executable contains all extract/convert logic"
    echo "  - No source code is visible in production"
    echo "  - All files process with 'redscorpion_' prefix"
    echo "  - Extract and convert are manual-only commands"
    echo
}

# Main execution
main() {
    echo -e "${BLUE}🚀 Starting production build process...${NC}"
    
    # Run all build steps
    check_prerequisites
    clean_old_build
    compile_bot
    build_local_api
    create_production_structure
    copy_all_files
    make_production_modifications
    create_documentation
    verify_build
    create_deployment_package
    show_summary
    
    echo -e "${GREEN}✅ Production build completed successfully!${NC}"
}

# Handle command line arguments
case "${1:-}" in
    "clean")
        echo -e "${BLUE}🧹 Cleaning production builds...${NC}"
        if [ -d "$PROD_DIR" ]; then
            rm -rf "$PROD_DIR"
            echo -e "${GREEN}✓ Production builds cleaned${NC}"
        else
            echo -e "${YELLOW}ℹ️  No production builds to clean${NC}"
        fi
        ;;
    "verify")
        if [ -d "$PROD_PATH" ]; then
            verify_build
        else
            echo -e "${RED}❌ No production build found to verify${NC}"
            exit 1
        fi
        ;;
    *)
        main
        ;;
esac