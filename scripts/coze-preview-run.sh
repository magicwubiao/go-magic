#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

export PATH="$PATH:/usr/local/go/bin"
export PORT=5000

# 清理 5000 端口残留进程
fuser -k 5000/tcp 2>/dev/null || true
sleep 1

echo "Starting go-magic server on port 5000..."
exec ./build/magic server --port 5000
