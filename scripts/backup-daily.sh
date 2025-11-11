#!/bin/bash

# Daily Backup Script for Telegram Archive Bot
# This script should be run daily via cron to create automated backups

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BOT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="${BOT_DIR}/backups"
CONFIG_FILE="${BOT_DIR}/.env"
LOG_FILE="${BOT_DIR}/logs/backup.log"
RETENTION_DAYS=30
SERVICE_NAME="telegram-archive-bot"

# Logging function
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$LOG_FILE"
}

# Error handling
error_exit() {
    log "ERROR: $1"
    exit 1
}

# Check if running as root (for systemctl commands)
check_permissions() {
    if [[ $EUID -eq 0 ]]; then
        USE_SUDO=""
    else
        USE_SUDO="sudo"
    fi
}

# Create necessary directories
setup_directories() {
    mkdir -p "$BACKUP_DIR"
    mkdir -p "$(dirname "$LOG_FILE")"
}

# Check if bot service is running
check_service() {
    if $USE_SUDO systemctl is-active --quiet "$SERVICE_NAME"; then
        log "Service $SERVICE_NAME is running"
        return 0
    else
        log "WARNING: Service $SERVICE_NAME is not running"
        return 1
    fi
}

# Stop bot service safely
stop_service() {
    log "Stopping $SERVICE_NAME service for consistent backup..."
    if ! $USE_SUDO systemctl stop "$SERVICE_NAME"; then
        error_exit "Failed to stop $SERVICE_NAME service"
    fi
    
    # Wait a moment for graceful shutdown
    sleep 5
}

# Start bot service
start_service() {
    log "Starting $SERVICE_NAME service..."
    if ! $USE_SUDO systemctl start "$SERVICE_NAME"; then
        error_exit "Failed to start $SERVICE_NAME service"
    fi
    
    # Wait for service to be ready
    sleep 10
    
    if ! $USE_SUDO systemctl is-active --quiet "$SERVICE_NAME"; then
        error_exit "Service $SERVICE_NAME failed to start properly"
    fi
    
    log "Service $SERVICE_NAME started successfully"
}

# Create database backup
create_backup() {
    log "Creating database backup..."
    
    cd "$BOT_DIR"
    
    # Use the backup tool to create backup
    if ! go run cmd/backup/main.go \
        -action=backup \
        -dir="$BACKUP_DIR" \
        -retention="$RETENTION_DAYS" \
        -compress=true \
        -verify=true; then
        error_exit "Failed to create database backup"
    fi
    
    log "Database backup completed successfully"
}

# Backup configuration files
backup_config() {
    log "Backing up configuration files..."
    
    local date_stamp
    date_stamp=$(date +%Y%m%d_%H%M%S)
    
    # Backup .env file (sensitive - restrict permissions)
    if [[ -f "$CONFIG_FILE" ]]; then
        cp "$CONFIG_FILE" "${BACKUP_DIR}/config_${date_stamp}.env"
        chmod 600 "${BACKUP_DIR}/config_${date_stamp}.env"
        log "Configuration backed up to config_${date_stamp}.env"
    fi
    
    # Backup external dependencies
    if [[ -d "${BOT_DIR}/app/extraction" ]]; then
        tar -czf "${BACKUP_DIR}/extraction_${date_stamp}.tar.gz" \
            -C "${BOT_DIR}/app" extraction/
        log "Extraction tools backed up to extraction_${date_stamp}.tar.gz"
    fi
}

# Cleanup old backups
cleanup_old_backups() {
    log "Cleaning up old backups (retention: $RETENTION_DAYS days)..."
    
    cd "$BOT_DIR"
    
    # Use the backup tool for cleanup
    if ! go run cmd/backup/main.go \
        -action=cleanup \
        -dir="$BACKUP_DIR" \
        -retention="$RETENTION_DAYS" \
        -force=true; then
        log "WARNING: Backup cleanup had errors, but continuing..."
    fi
    
    # Clean up old config backups
    find "$BACKUP_DIR" -name "config_*.env" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true
    find "$BACKUP_DIR" -name "extraction_*.tar.gz" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true
    
    log "Cleanup completed"
}

# Verify backup integrity
verify_backup() {
    log "Verifying backup integrity..."
    
    cd "$BOT_DIR"
    
    # Get stats to verify backups exist and are recent
    if ! go run cmd/backup/main.go \
        -action=stats \
        -dir="$BACKUP_DIR"; then
        error_exit "Failed to verify backup integrity"
    fi
    
    log "Backup verification completed"
}

# Health check after backup
health_check() {
    log "Performing post-backup health check..."
    
    # Check service status
    if ! check_service; then
        error_exit "Service health check failed"
    fi
    
    # Check database accessibility
    cd "$BOT_DIR"
    if ! timeout 10 sqlite3 "$(grep DATABASE_PATH .env | cut -d= -f2)" "SELECT COUNT(*) FROM tasks;" >/dev/null 2>&1; then
        error_exit "Database health check failed"
    fi
    
    # Check disk space
    local disk_usage
    disk_usage=$(df "$BACKUP_DIR" | awk 'NR==2 {print $5}' | sed 's/%//')
    if [[ $disk_usage -gt 90 ]]; then
        log "WARNING: Disk usage is ${disk_usage}% - consider cleanup"
    fi
    
    log "Health check completed successfully"
}

# Send notification (implement your notification method)
send_notification() {
    local status=$1
    local message=$2
    
    # Example: send to a log monitoring system, email, or Slack
    # You can implement your preferred notification method here
    
    log "NOTIFICATION [$status]: $message"
    
    # Example Slack webhook (uncomment and configure if needed)
    # if [[ -n "${SLACK_WEBHOOK_URL:-}" ]]; then
    #     curl -X POST -H 'Content-type: application/json' \
    #         --data "{\"text\":\"Backup [$status]: $message\"}" \
    #         "$SLACK_WEBHOOK_URL" || true
    # fi
}

# Main backup procedure
main() {
    local start_time
    start_time=$(date +%s)
    
    log "Starting daily backup procedure..."
    
    # Setup
    check_permissions
    setup_directories
    
    # Record initial service state
    local service_was_running=false
    if check_service; then
        service_was_running=true
    fi
    
    # Perform backup with error handling
    local backup_success=false
    
    if [[ "$service_was_running" == true ]]; then
        stop_service
    fi
    
    # Always try to restart service, even if backup fails
    trap 'if [[ "$service_was_running" == true ]]; then start_service; fi' EXIT
    
    # Create backups
    if create_backup && backup_config; then
        backup_success=true
    fi
    
    # Restart service if it was running
    if [[ "$service_was_running" == true ]]; then
        start_service
    fi
    
    # Continue with post-backup tasks
    if [[ "$backup_success" == true ]]; then
        cleanup_old_backups
        verify_backup
        health_check
        
        local end_time
        end_time=$(date +%s)
        local duration=$((end_time - start_time))
        
        log "Daily backup completed successfully in ${duration} seconds"
        send_notification "SUCCESS" "Daily backup completed in ${duration}s"
    else
        error_exit "Backup procedure failed"
    fi
}

# Script entry point
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    # Check if bot directory exists
    if [[ ! -d "$BOT_DIR" ]] || [[ ! -f "${BOT_DIR}/.env" ]]; then
        echo "Error: Bot directory or configuration not found"
        echo "Expected directory: $BOT_DIR"
        exit 1
    fi
    
    # Ensure we're in the bot directory
    cd "$BOT_DIR"
    
    # Run main backup procedure
    main "$@"
fi