#!/bin/bash

OUTPUT_DIR="_misc"
OUTPUT="$OUTPUT_DIR/filelist.md"

mkdir -p "$OUTPUT_DIR"

echo "# Project Structure" > "$OUTPUT"
echo "" >> "$OUTPUT"

if command -v tree &> /dev/null; then
    tree -a -I ".git|$OUTPUT_DIR" | sed 's/^/    /' >> "$OUTPUT"
else
    find . \
        -path './.git' -prune -o \
        -path "./$OUTPUT_DIR" -prune -o \
        -print | sed 's/^/    /' >> "$OUTPUT"
fi

echo "File list updated in $OUTPUT"
