#!/bin/bash

# Auto File Mover Script
# Monitors Local Bot API Server directory and automatically moves files to extraction directories
# Usage: ./scripts/auto-file-mover.sh

LOCAL_API_DOCS="5773867447:AAFi7VjSS-RwtYm-k9HjkfeOxkk6Oh2TSzY/documents"
TXT_DIR="app/extraction/files/txt"
ALL_DIR="app/extraction/files/all"

# Create destination directories
mkdir -p "$TXT_DIR" "$ALL_DIR"

echo "🔄 Auto File Mover started - monitoring $LOCAL_API_DOCS"
echo "📁 TXT files will be moved to: $TXT_DIR"
echo "📦 Archive files will be moved to: $ALL_DIR"
echo "⏹️  Press Ctrl+C to stop"
echo ""

# Function to move files
move_files() {
    if [ ! -d "$LOCAL_API_DOCS" ]; then
        return
    fi
    
    local moved=0
    for file in "$LOCAL_API_DOCS"/*; do
        if [ -f "$file" ]; then
            filename=$(basename "$file")
            
            # Determine destination based on file extension
            case "${filename##*.}" in
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
            
            dest_file="$dest_dir/$filename"
            
            # Handle conflicts by adding timestamp
            if [ -f "$dest_file" ]; then
                name="${filename%.*}"
                ext="${filename##*.}"
                timestamp=$(date +%s)
                dest_file="$dest_dir/${name}_${timestamp}.${ext}"
            fi
            
            mv "$file" "$dest_file"
            echo "$(date '+%H:%M:%S') ✅ Moved: $filename → $dest_file"
            ((moved++))
        fi
    done
    
    if [ $moved -gt 0 ]; then
        echo "$(date '+%H:%M:%S') 📊 Moved $moved file(s)"
    fi
}

# Monitor loop
while true; do
    move_files
    sleep 2
done