#!/bin/bash

# Script to move existing files from Local Bot API directory to proper extraction directories
# Usage: ./scripts/move-existing-files.sh

set -e

echo "🔄 Moving existing files from Local Bot API directory to proper extraction directories..."

# Define source and destination directories
LOCAL_API_DIR="5773867447:AAGVPLvZ65Dm4KxkCyFaaAngFPPEX5PzpWA/documents"
TXT_DIR="app/extraction/files/txt"
ALL_DIR="app/extraction/files/all"

# Create destination directories if they don't exist
echo "📁 Creating destination directories..."
mkdir -p "$TXT_DIR"
mkdir -p "$ALL_DIR"

# Check if Local Bot API directory exists
if [ ! -d "$LOCAL_API_DIR" ]; then
    echo "❌ Local Bot API directory not found: $LOCAL_API_DIR"
    exit 1
fi

# Count files to be moved
total_files=$(find "$LOCAL_API_DIR" -type f -name "*.txt" -o -name "*.zip" -o -name "*.rar" | wc -l)

if [ "$total_files" -eq 0 ]; then
    echo "✅ No files to move"
    exit 0
fi

echo "📊 Found $total_files files to move"

# Move .txt files to files/txt/
txt_count=0
if find "$LOCAL_API_DIR" -name "*.txt" -type f | head -1 > /dev/null 2>&1; then
    echo "📄 Moving TXT files to $TXT_DIR..."
    for file in "$LOCAL_API_DIR"/*.txt; do
        if [ -f "$file" ]; then
            filename=$(basename "$file")
            dest_path="$TXT_DIR/$filename"
            
            # Handle filename conflicts
            counter=1
            while [ -f "$dest_path" ]; do
                name_part="${filename%.*}"
                ext_part="${filename##*.}"
                dest_path="$TXT_DIR/${name_part}_${counter}.${ext_part}"
                ((counter++))
            done
            
            mv "$file" "$dest_path"
            echo "  ✅ $filename → $dest_path"
            ((txt_count++))
        fi
    done
fi

# Move .zip and .rar files to files/all/
archive_count=0
for ext in zip rar; do
    if find "$LOCAL_API_DIR" -name "*.$ext" -type f | head -1 > /dev/null 2>&1; then
        echo "📦 Moving $ext files to $ALL_DIR..."
        for file in "$LOCAL_API_DIR"/*.$ext; do
            if [ -f "$file" ]; then
                filename=$(basename "$file")
                dest_path="$ALL_DIR/$filename"
                
                # Handle filename conflicts
                counter=1
                while [ -f "$dest_path" ]; do
                    name_part="${filename%.*}"
                    ext_part="${filename##*.}"
                    dest_path="$ALL_DIR/${name_part}_${counter}.${ext_part}"
                    ((counter++))
                done
                
                mv "$file" "$dest_path"
                echo "  ✅ $filename → $dest_path"
                ((archive_count++))
            fi
        done
    fi
done

echo ""
echo "📈 File movement summary:"
echo "  • TXT files moved: $txt_count"
echo "  • Archive files moved: $archive_count" 
echo "  • Total files moved: $((txt_count + archive_count))"

# Check if Local Bot API documents directory is now empty
remaining_files=$(find "$LOCAL_API_DIR" -type f | wc -l)
if [ "$remaining_files" -eq 0 ]; then
    echo "✅ Local Bot API documents directory is now empty"
    echo "🧹 Removing empty directory: $LOCAL_API_DIR"
    rmdir "$LOCAL_API_DIR" 2>/dev/null || echo "⚠️  Could not remove directory (may contain other files)"
else
    echo "⚠️  $remaining_files files remain in $LOCAL_API_DIR (non-standard file types)"
fi

echo ""
echo "✅ File movement completed successfully!"
echo "🔄 Files are now properly organized:"
echo "  • TXT files: $TXT_DIR/"
echo "  • ZIP/RAR files: $ALL_DIR/"
echo ""
echo "💡 Next steps:"
echo "  1. Start the bot: ./telegram-archive-bot"
echo "  2. Use /extract command to process archive files"
echo "  3. Use /convert command to process extracted content"