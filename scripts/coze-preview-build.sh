#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

export PATH="$PATH:/usr/local/go/bin"
export GOTOOLCHAIN=local

echo "Building web UI..."
cd web
pnpm install --frozen-lockfile 2>/dev/null || pnpm install
pnpm build

echo "Copying dist to server directory..."
mkdir -p ../internal/server/dist
if [ -d "dist" ]; then
  cp -r dist/* ../internal/server/dist/
else
  echo "dist/ not found, assuming vite already built to ../internal/server/dist"
fi

echo "Building Go binary..."
cd ..
go build -o build/magic ./cmd/magic

echo "Preview build complete!"
