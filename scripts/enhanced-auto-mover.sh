#!/bin/bash

# Enhanced Auto File Mover with Original Filename Preservation
# Monitors bot logs to get original filenames and maps them to Local Bot API files
# Usage: ./scripts/enhanced-auto-mover.sh

LOCAL_API_DOCS="5773867447:AAFi7VjSS-RwtYm-k9HjkfeOxkk6Oh2TSzY/documents"
TXT_DIR="app/extraction/files/txt"
ALL_DIR="app/extraction/files/all"
LOG_FILE="logs/bot-session.log"
MAPPING_FILE="/tmp/filename_mapping.txt"

# Create destination directories
mkdir -p "$TXT_DIR" "$ALL_DIR"

echo "🔄 Enhanced Auto File Mover started"
echo "📁 TXT files → $TXT_DIR"
echo "📦 Archive files → $ALL_DIR"
echo "📝 Monitoring bot logs for original filenames"
echo "⏹️  Press Ctrl+C to stop"
echo ""

# Function to extract filename mappings from bot logs
update_mappings() {
    if [ ! -f "$LOG_FILE" ]; then
        return
    fi
    
    # Look for recent file uploads in logs and create mapping
    # Extract lines like: "Received file from admin file_name=original.txt"
    tail -50 "$LOG_FILE" | grep "Received file from admin" | while read -r line; do
        if [[ $line =~ file_name=([^[:space:]]+) ]]; then
            original_name="${BASH_REMATCH[1]}"
            timestamp=$(echo "$line" | cut -d'[' -f2 | cut -d']' -f1)
            echo "$timestamp:$original_name" >> "$MAPPING_FILE.tmp"
        fi
    done
    
    # Keep only recent entries (last 10)
    if [ -f "$MAPPING_FILE.tmp" ]; then
        tail -10 "$MAPPING_FILE.tmp" > "$MAPPING_FILE"
        rm -f "$MAPPING_FILE.tmp"
    fi
}

# Function to get original filename for a Local Bot API file
get_original_filename() {
    local local_api_file="$1"
    local file_number
    
    # Extract number from file_X.ext
    if [[ $local_api_file =~ file_([0-9]+)\. ]]; then
        file_number="${BASH_REMATCH[1]}"
    else
        echo "$local_api_file"  # Return as-is if no pattern match
        return
    fi
    
    # Try to map to original filename
    if [ -f "$MAPPING_FILE" ]; then
        # Get the Nth most recent file (where N is the file number)
        local line_number=$((file_number + 1))
        local mapping_line=$(tail -"$line_number" "$MAPPING_FILE" | head -1)
        
        if [ -n "$mapping_line" ]; then
            echo "${mapping_line#*:}"  # Extract filename after timestamp
            return
        fi
    fi
    
    # Fallback: use Local Bot API filename
    echo "$local_api_file"
}

# Function to move files with original names
move_files() {
    if [ ! -d "$LOCAL_API_DOCS" ]; then
        return
    fi
    
    local moved=0
    for file in "$LOCAL_API_DOCS"/*; do
        if [ -f "$file" ]; then
            local api_filename=$(basename "$file")
            local original_filename=$(get_original_filename "$api_filename")
            
            # Determine destination based on original file extension
            case "${original_filename##*.}" in
                txt)
                    dest_dir="$TXT_DIR"
                    ;;
                rar|zip)
                    dest_dir="$ALL_DIR"
                    ;;
                *)
                    dest_dir="$ALL_DIR"
                    ;;
            esac
            
            dest_file="$dest_dir/$original_filename"
            
            # Handle filename conflicts by adding timestamp
            if [ -f "$dest_file" ]; then
                name="${original_filename%.*}"
                ext="${original_filename##*.}"
                timestamp=$(date +%s)
                dest_file="$dest_dir/${name}_${timestamp}.${ext}"
            fi
            
            if mv "$file" "$dest_file"; then
                echo "$(date '+%H:%M:%S') ✅ Moved: $api_filename → $(basename "$dest_file")"
                echo "                      📄 Original name: $original_filename"
                echo "                      📁 Directory: $dest_dir"
                ((moved++))
            fi
        fi
    done
    
    if [ $moved -gt 0 ]; then
        echo "$(date '+%H:%M:%S') 📊 Moved $moved file(s) with original names preserved"
        echo ""
    fi
}

# Main monitoring loop
while true; do
    update_mappings
    move_files
    sleep 3
done