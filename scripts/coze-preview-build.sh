#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

echo "Building web UI..."
cd web
pnpm install --frozen-lockfile 2>/dev/null || pnpm install
pnpm build

echo "Copying dist to server directory..."
mkdir -p ../internal/server/dist
cp -r dist/* ../internal/server/dist/

echo "Building Go binary..."
cd ..
/usr/local/go/bin/go build -o build/magic ./cmd/magic

echo "Preview build complete!"
