#!/bin/bash

# Filename Mapper Script
# Maps Local Bot API Server files (file_0.txt, file_1.rar) to original filenames
# Usage: ./scripts/filename-mapper.sh "original_filename.txt" "file_0.txt"

if [ $# -ne 2 ]; then
    echo "Usage: $0 <original_filename> <local_api_filename>"
    echo "Example: $0 'document.txt' 'file_0.txt'"
    exit 1
fi

ORIGINAL_NAME="$1"
LOCAL_API_NAME="$2"

LOCAL_API_DOCS="5773867447:AAGVPLvZ65Dm4KxkCyFaaAngFPPEX5PzpWA/documents"
TXT_DIR="app/extraction/files/txt"
ALL_DIR="app/extraction/files/all"

# Create destination directories
mkdir -p "$TXT_DIR" "$ALL_DIR"

# Source file path
SOURCE_FILE="$LOCAL_API_DOCS/$LOCAL_API_NAME"

if [ ! -f "$SOURCE_FILE" ]; then
    echo "❌ Source file not found: $SOURCE_FILE"
    exit 1
fi

# Determine destination based on original file extension
case "${ORIGINAL_NAME##*.}" in
    txt)
        dest_dir="$TXT_DIR"
        ;;
    rar|zip)
        dest_dir="$ALL_DIR"
        ;;
    *)
        dest_dir="$ALL_DIR"
        echo "⚠️ Unknown file type for $ORIGINAL_NAME, moving to all directory"
        ;;
esac

# Destination file path with original name
DEST_FILE="$dest_dir/$ORIGINAL_NAME"

# Handle filename conflicts
counter=1
while [ -f "$DEST_FILE" ]; do
    name="${ORIGINAL_NAME%.*}"
    ext="${ORIGINAL_NAME##*.}"
    DEST_FILE="$dest_dir/${name}_${counter}.${ext}"
    ((counter++))
done

# Move and rename file
if mv "$SOURCE_FILE" "$DEST_FILE"; then
    echo "✅ Successfully moved and renamed:"
    echo "   $LOCAL_API_NAME → $DEST_FILE"
    echo "   📁 Directory: $dest_dir"
    echo "   📄 Original name preserved: $ORIGINAL_NAME"
else
    echo "❌ Failed to move file"
    exit 1
fi