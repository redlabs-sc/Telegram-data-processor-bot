#!/bin/bash

# Emergency Restore Script for Telegram Archive Bot
# This script provides emergency recovery capabilities with safety checks

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BOT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="${BOT_DIR}/backups"
CONFIG_FILE="${BOT_DIR}/.env"
LOG_FILE="${BOT_DIR}/logs/restore.log"
SERVICE_NAME="telegram-archive-bot"

# Command line options
BACKUP_FILE=""
AUTO_CONFIRM=false
SKIP_CURRENT_BACKUP=false
VERIFY_RESTORE=true

# Logging function
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$LOG_FILE"
}

# Error handling
error_exit() {
    log "ERROR: $1"
    exit 1
}

# Show usage information
show_usage() {
    cat << EOF
Emergency Restore Script for Telegram Archive Bot

Usage: $0 [OPTIONS]

OPTIONS:
    -f, --file FILE         Backup file to restore from (required)
    -y, --yes              Auto-confirm all prompts (dangerous!)
    --skip-current-backup  Don't backup current database before restore
    --no-verify           Skip restore verification
    -h, --help            Show this help message

EXAMPLES:
    # Interactive restore with all safety checks
    $0 -f backups/bot_backup_20240125_120000.sql.gz

    # Automated restore (use with caution)
    $0 -f backups/bot_backup_20240125_120000.sql.gz -y

    # Quick restore without current backup (emergency only)
    $0 -f backups/bot_backup_20240125_120000.sql.gz --skip-current-backup

SAFETY FEATURES:
    - Creates backup of current database before restore
    - Verifies backup file integrity before restore
    - Validates restored database integrity
    - Service management with graceful shutdown/startup
    - Comprehensive logging of all operations

EOF
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -f|--file)
                BACKUP_FILE="$2"
                shift 2
                ;;
            -y|--yes)
                AUTO_CONFIRM=true
                shift
                ;;
            --skip-current-backup)
                SKIP_CURRENT_BACKUP=true
                shift
                ;;
            --no-verify)
                VERIFY_RESTORE=false
                shift
                ;;
            -h|--help)
                show_usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done

    if [[ -z "$BACKUP_FILE" ]]; then
        echo "Error: Backup file must be specified with -f option"
        show_usage
        exit 1
    fi
}

# Check if running as root (for systemctl commands)
check_permissions() {
    if [[ $EUID -eq 0 ]]; then
        USE_SUDO=""
    else
        USE_SUDO="sudo"
    fi
}

# Validate backup file
validate_backup_file() {
    log "Validating backup file: $BACKUP_FILE"
    
    if [[ ! -f "$BACKUP_FILE" ]]; then
        error_exit "Backup file does not exist: $BACKUP_FILE"
    fi
    
    # Check file size (should not be empty)
    local file_size
    file_size=$(stat -f%z "$BACKUP_FILE" 2>/dev/null || stat -c%s "$BACKUP_FILE" 2>/dev/null)
    if [[ $file_size -lt 1024 ]]; then
        error_exit "Backup file appears to be too small (${file_size} bytes): $BACKUP_FILE"
    fi
    
    # Test file readability
    if [[ "$BACKUP_FILE" == *.gz ]]; then
        if ! gzip -t "$BACKUP_FILE" 2>/dev/null; then
            error_exit "Backup file appears to be corrupted (gzip test failed): $BACKUP_FILE"
        fi
    fi
    
    log "Backup file validation passed (${file_size} bytes)"
}

# Display backup information
show_backup_info() {
    log "Backup file information:"
    log "  File: $BACKUP_FILE"
    
    local file_size
    file_size=$(stat -f%z "$BACKUP_FILE" 2>/dev/null || stat -c%s "$BACKUP_FILE" 2>/dev/null)
    log "  Size: $(numfmt --to=iec-i --suffix=B $file_size)"
    
    local file_date
    file_date=$(stat -f%Sm "$BACKUP_FILE" 2>/dev/null || stat -c%y "$BACKUP_FILE" 2>/dev/null | cut -d' ' -f1,2)
    log "  Date: $file_date"
    
    local compression="No"
    if [[ "$BACKUP_FILE" == *.gz ]]; then
        compression="Yes (gzip)"
    fi
    log "  Compressed: $compression"
}

# Confirm restore operation
confirm_restore() {
    if [[ "$AUTO_CONFIRM" == true ]]; then
        log "AUTO-CONFIRM enabled - proceeding without user confirmation"
        return 0
    fi
    
    echo ""
    echo "⚠️  WARNING: Database Restore Operation ⚠️"
    echo ""
    echo "This operation will:"
    echo "  1. Stop the bot service"
    if [[ "$SKIP_CURRENT_BACKUP" == false ]]; then
        echo "  2. Create a backup of the current database"
    fi
    echo "  3. Replace the current database with the backup"
    echo "  4. Restart the bot service"
    echo ""
    echo "Backup file: $BACKUP_FILE"
    echo ""
    
    read -p "Are you absolutely sure you want to proceed? (type 'yes' to confirm): " response
    
    if [[ "$response" != "yes" ]]; then
        log "Restore operation cancelled by user"
        exit 0
    fi
    
    log "User confirmed restore operation"
}

# Check service status
check_service() {
    if $USE_SUDO systemctl is-active --quiet "$SERVICE_NAME"; then
        return 0
    else
        return 1
    fi
}

# Stop bot service
stop_service() {
    log "Stopping $SERVICE_NAME service..."
    
    if check_service; then
        if ! $USE_SUDO systemctl stop "$SERVICE_NAME"; then
            error_exit "Failed to stop $SERVICE_NAME service"
        fi
        
        # Wait for graceful shutdown
        local attempts=0
        while check_service && [[ $attempts -lt 30 ]]; do
            sleep 1
            ((attempts++))
        done
        
        if check_service; then
            log "WARNING: Service did not stop gracefully, attempting force stop"
            $USE_SUDO systemctl kill "$SERVICE_NAME" || true
            sleep 5
        fi
        
        log "Service stopped successfully"
    else
        log "Service was not running"
    fi
}

# Start bot service
start_service() {
    log "Starting $SERVICE_NAME service..."
    
    if ! $USE_SUDO systemctl start "$SERVICE_NAME"; then
        error_exit "Failed to start $SERVICE_NAME service"
    fi
    
    # Wait for service to be ready
    local attempts=0
    while ! check_service && [[ $attempts -lt 30 ]]; do
        sleep 1
        ((attempts++))
    done
    
    if ! check_service; then
        error_exit "Service failed to start properly"
    fi
    
    log "Service started successfully"
}

# Create backup of current database
backup_current_database() {
    if [[ "$SKIP_CURRENT_BACKUP" == true ]]; then
        log "Skipping current database backup (--skip-current-backup specified)"
        return 0
    fi
    
    log "Creating backup of current database before restore..."
    
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    local current_backup="${BACKUP_DIR}/current_db_backup_${timestamp}.sql.gz"
    
    cd "$BOT_DIR"
    
    if ! go run cmd/backup/main.go \
        -action=backup \
        -dir="$BACKUP_DIR" \
        -compress=true \
        -verify=false; then
        error_exit "Failed to create backup of current database"
    fi
    
    log "Current database backed up successfully"
}

# Perform database restore
restore_database() {
    log "Performing database restore from: $BACKUP_FILE"
    
    cd "$BOT_DIR"
    
    local restore_opts="-action=restore -file=$BACKUP_FILE"
    
    if [[ "$SKIP_CURRENT_BACKUP" == true ]]; then
        restore_opts="$restore_opts -backup-current=false"
    else
        restore_opts="$restore_opts -backup-current=true"
    fi
    
    if [[ "$VERIFY_RESTORE" == true ]]; then
        restore_opts="$restore_opts -verify=true"
    else
        restore_opts="$restore_opts -verify=false"
    fi
    
    # Force non-interactive mode
    restore_opts="$restore_opts -force=true"
    
    if ! go run cmd/backup/main.go $restore_opts; then
        error_exit "Database restore failed"
    fi
    
    log "Database restore completed successfully"
}

# Verify system health after restore
verify_system_health() {
    log "Verifying system health after restore..."
    
    # Check database accessibility
    local db_path
    db_path=$(grep DATABASE_PATH "$CONFIG_FILE" | cut -d= -f2 | tr -d '"' | tr -d "'")
    
    if ! timeout 10 sqlite3 "$db_path" "SELECT COUNT(*) FROM tasks;" >/dev/null 2>&1; then
        error_exit "Database health check failed after restore"
    fi
    
    # Check database integrity
    local integrity_result
    integrity_result=$(sqlite3 "$db_path" "PRAGMA integrity_check;")
    if [[ "$integrity_result" != "ok" ]]; then
        error_exit "Database integrity check failed: $integrity_result"
    fi
    
    # Check if service is responding (basic test)
    sleep 10  # Give service time to initialize
    
    if ! check_service; then
        error_exit "Service is not running after restore"
    fi
    
    log "System health verification completed successfully"
}

# Show restore summary
show_restore_summary() {
    log "Restore operation completed successfully!"
    log "Summary:"
    log "  Restored from: $BACKUP_FILE"
    log "  Service status: $(systemctl is-active "$SERVICE_NAME")"
    log "  Database verified: $VERIFY_RESTORE"
    
    if [[ "$SKIP_CURRENT_BACKUP" == false ]]; then
        log "  Previous DB backed up: Yes"
    else
        log "  Previous DB backed up: No (skipped)"
    fi
    
    echo ""
    echo "✅ Emergency restore completed successfully!"
    echo ""
    echo "Next steps:"
    echo "  1. Test bot functionality by sending commands"
    echo "  2. Monitor logs for any issues: tail -f logs/bot.log"
    echo "  3. Verify that all expected data is present"
    echo ""
}

# Cleanup function for error handling
cleanup_on_error() {
    log "Cleanup due to error..."
    
    # Always try to start the service if it was stopped
    if ! check_service; then
        log "Attempting to restart service after error..."
        start_service || log "WARNING: Failed to restart service"
    fi
}

# Main restore procedure
main() {
    local start_time
    start_time=$(date +%s)
    
    log "Starting emergency restore procedure..."
    
    # Setup error handling
    trap cleanup_on_error ERR
    
    # Validation and preparation
    check_permissions
    mkdir -p "$(dirname "$LOG_FILE")"
    validate_backup_file
    show_backup_info
    confirm_restore
    
    # Record initial service state
    local service_was_running=false
    if check_service; then
        service_was_running=true
    fi
    
    # Perform restore procedure
    stop_service
    backup_current_database
    restore_database
    
    # Always try to restart service
    if [[ "$service_was_running" == true ]]; then
        start_service
    fi
    
    # Verification
    verify_system_health
    
    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log "Emergency restore completed in ${duration} seconds"
    show_restore_summary
}

# Script entry point
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    # Parse command line arguments
    parse_args "$@"
    
    # Check if bot directory exists
    if [[ ! -d "$BOT_DIR" ]] || [[ ! -f "${BOT_DIR}/.env" ]]; then
        echo "Error: Bot directory or configuration not found"
        echo "Expected directory: $BOT_DIR"
        exit 1
    fi
    
    # Ensure we're in the bot directory
    cd "$BOT_DIR"
    
    # Run main restore procedure
    main
fi