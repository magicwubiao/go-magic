#!/bin/bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

mkdir -p bin
go build -ldflags="-X github.com/magicwubiao/go-magic/cmd/magic.Version=coze-deploy" -o bin/magic ./cmd/magic
