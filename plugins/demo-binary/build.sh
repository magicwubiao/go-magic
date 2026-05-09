#!/bin/bash
# Build demo-binary plugin for multiple platforms
# Usage: ./build.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT_DIR="$SCRIPT_DIR/bin"

mkdir -p "$OUTPUT_DIR"

echo "Building demo-binary plugin..."

platforms=(
    "windows/amd64:demo-binary.exe"
    "linux/amd64:demo-binary-linux"
    "linux/arm64:demo-binary-linux-arm64"
    "darwin/amd64:demo-binary-mac"
    "darwin/arm64:demo-binary-arm64"
)

for item in "${platforms[@]}"; do
    IFS=':' read -r platform output <<< "$item"
    GOOS="${platform%%/*}"
    GOARCH="${platform##*/}"
    
    echo "  Building $GOOS/$GOARCH -> $output..."
    GOOS=$GOOS GOARCH=$GOARCH go build -ldflags "-s -w" -o "$OUTPUT_DIR/$output" "$SCRIPT_DIR/main.go"
    echo "  OK"
done

echo ""
echo "Done! Binaries in: $OUTPUT_DIR"
ls -lh "$OUTPUT_DIR"
