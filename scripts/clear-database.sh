#!/bin/bash

# Clear Database Script
# This script clears all data from bot.db while preserving table structure

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DB_PATH="$PROJECT_ROOT/data/bot.db"

echo -e "${BLUE}=== Database Clear Script ===${NC}"
echo "Project Root: $PROJECT_ROOT"
echo "Database Path: $DB_PATH"
echo

# Check if database exists
if [ ! -f "$DB_PATH" ]; then
    echo -e "${RED}Error: Database file not found at $DB_PATH${NC}"
    exit 1
fi

# Create backup
BACKUP_PATH="$PROJECT_ROOT/data/bot.db.backup.$(date +%Y%m%d_%H%M%S)"
echo -e "${YELLOW}Creating backup at: $BACKUP_PATH${NC}"
cp "$DB_PATH" "$BACKUP_PATH"

# Stop bot if running
echo -e "${YELLOW}Stopping bot processes...${NC}"
pkill -f telegram-archive-bot || true
sleep 2

# Get list of tables first
echo -e "${BLUE}Getting database structure...${NC}"
TABLES=$(sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%';")

if [ -z "$TABLES" ]; then
    echo -e "${RED}No tables found in database${NC}"
    exit 1
fi

echo "Found tables:"
for table in $TABLES; do
    echo "  - $table"
done
echo

# Clear all data from each table
echo -e "${BLUE}Clearing data from all tables...${NC}"
for table in $TABLES; do
    echo -e "${YELLOW}Clearing table: $table${NC}"
    
    # Get row count before
    COUNT_BEFORE=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM $table;")
    
    # Clear the table
    sqlite3 "$DB_PATH" "DELETE FROM $table;"
    
    # Get row count after
    COUNT_AFTER=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM $table;")
    
    echo "  Rows removed: $COUNT_BEFORE"
done

# Reset auto-increment sequences if any
echo -e "${BLUE}Resetting auto-increment sequences...${NC}"
sqlite3 "$DB_PATH" "DELETE FROM sqlite_sequence;" 2>/dev/null || true

# Vacuum database to reclaim space
echo -e "${BLUE}Vacuuming database...${NC}"
sqlite3 "$DB_PATH" "VACUUM;"

# Verify database structure is intact
echo -e "${BLUE}Verifying database structure...${NC}"
SCHEMA_CHECK=$(sqlite3 "$DB_PATH" ".schema" | wc -l)
if [ "$SCHEMA_CHECK" -gt 0 ]; then
    echo -e "${GREEN}✓ Database structure preserved${NC}"
else
    echo -e "${RED}✗ Database structure may be corrupted${NC}"
    exit 1
fi

# Show final table status
echo -e "${BLUE}Final table status:${NC}"
for table in $TABLES; do
    COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM $table;")
    echo "  $table: $COUNT rows"
done

echo
echo -e "${GREEN}=== Database cleared successfully ===${NC}"
echo -e "${GREEN}✓ Backup created: $BACKUP_PATH${NC}"
echo -e "${GREEN}✓ All table data cleared${NC}"
echo -e "${GREEN}✓ Database structure preserved${NC}"
echo -e "${GREEN}✓ Database vacuumed${NC}"
echo
echo -e "${YELLOW}Note: Bot was stopped during this operation${NC}"
echo -e "${YELLOW}Run './telegram-archive-bot' to restart the bot${NC}"