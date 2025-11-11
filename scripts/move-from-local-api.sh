#!/bin/bash

# Script to move files from Local Bot API Server directory to proper extraction directories
# Usage: ./scripts/move-from-local-api.sh

set -e

echo "🔄 Moving files from Local Bot API Server directory to extraction directories..."

# Define source directory
LOCAL_API_DOCS="5773867447:AAGVPLvZ65Dm4KxkCyFaaAngFPPEX5PzpWA/documents"

# Define destination directories  
TXT_DIR="app/extraction/files/txt"
ALL_DIR="app/extraction/files/all"

# Create destination directories
mkdir -p "$TXT_DIR" "$ALL_DIR"

# Check if source directory exists
if [ ! -d "$LOCAL_API_DOCS" ]; then
    echo "❌ Local Bot API documents directory not found: $LOCAL_API_DOCS"
    exit 1
fi

# Move files
moved_count=0

echo "📄 Moving files..."
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
                echo "  ⚠️ Unknown file type: $filename, moving to all directory"
                ;;
        esac
        
        # Generate proper filename (the original filenames are unknown, so we use generic names)
        dest_file="$dest_dir/$filename"
        
        # Handle conflicts
        counter=1
        while [ -f "$dest_file" ]; do
            name="${filename%.*}"
            ext="${filename##*.}"
            dest_file="$dest_dir/${name}_${counter}.${ext}"
            ((counter++))
        done
        
        mv "$file" "$dest_file"
        echo "  ✅ $filename → $dest_file"
        ((moved_count++))
    fi
done

echo ""
echo "📊 Summary:"
echo "  • Files moved: $moved_count"
echo "  • TXT files location: $TXT_DIR/"
echo "  • Archive files location: $ALL_DIR/"

# Clean up empty directory
if [ -d "$LOCAL_API_DOCS" ] && [ -z "$(ls -A "$LOCAL_API_DOCS")" ]; then
    rmdir "$LOCAL_API_DOCS"
    echo "  • Removed empty documents directory"
fi

echo ""
echo "✅ File movement completed!"