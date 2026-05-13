#!/bin/bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

PORT=5000

usage() {
  echo "Usage: $0 -p <port>"
  echo "  -p  Port to listen on (default: 5000)"
}

while getopts "p:h" opt; do
  case "$opt" in
    p)
      PORT="$OPTARG"
      ;;
    h)
      usage
      exit 0
      ;;
    \?)
      echo "Invalid option: -$OPTARG"
      usage
      exit 1
      ;;
  esac
done

export PORT

# Find the built binary
GO_OS=$(go env GOOS)
GO_ARCH=$(go env GOARCH)
BINARY="./dist/magic-${GO_OS}-${GO_ARCH}"

# Fallback to bin/magic for development mode
if [ ! -f "$BINARY" ]; then
    BINARY="./bin/magic"
fi

# Final fallback: build if needed
if [ ! -f "$BINARY" ]; then
    ./build.sh cli
    BINARY="./dist/magic-${GO_OS}-${GO_ARCH}"
fi

# Start server with the built binary
exec "$BINARY" server --port 5000
