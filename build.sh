#!/bin/bash
set -e

VERSION="dev"
COMMIT="unknown"
DATE="unknown"
LDFLAGS="-X github.com/magicwubiao/go-magic/cmd/magic.Version=${VERSION} -X github.com/magicwubiao/go-magic/cmd/magic.Commit=${COMMIT} -X github.com/magicwubiao/go-magic/cmd/magic.BuildDate=${DATE}"

mkdir -p build

echo "==> Building linux/amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o build/magic-linux-amd64 ./cmd/magic
echo "    Done: build/magic-linux-amd64"

echo "==> Building darwin/amd64..."
GOOS=darwin GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o build/magic-darwin-amd64 ./cmd/magic
echo "    Done: build/magic-darwin-amd64"

echo "==> Building darwin/arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o build/magic-darwin-arm64 ./cmd/magic
echo "    Done: build/magic-darwin-arm64"

echo "==> Building windows/amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o build/magic-windows-amd64.exe ./cmd/magic
echo "    Done: build/magic-windows-amd64.exe"

echo ""
echo "==> All builds completed!"
ls -lh build/
