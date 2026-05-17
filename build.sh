#!/bin/bash
set -e

# go-magic Build Script
# Usage: ./build.sh [target]
# Targets: cli, web, all (default: all)

VERSION=${VERSION:-"dev"}
BUILD_DIR=${BUILD_DIR:-"./dist"}
TIMESTAMP=$(date +%Y%m%d%H%M%S)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Parse arguments
TARGET=${1:-"all"}

# Ensure build directory
mkdir -p "$BUILD_DIR"

# Copy web dist to internal/server/dist for embedding
if [ -d "web/dist" ]; then
    mkdir -p internal/server/dist
    cp -r web/dist/* internal/server/dist/
    echo "Copied web/dist to internal/server/dist"
fi

# Get Go info
GO_VERSION=$(go version | grep -oP 'go\d+\.\d+')
GO_ARCH=$(go env GOARCH)
GO_OS=$(go env GOOS)

log_info "Building go-magic v${VERSION}"
log_info "Go: ${GO_VERSION}, Arch: ${GO_ARCH}, OS: ${GO_OS}"

# Build CLI
build_cli() {
    log_info "Building CLI binary..."

    local OUTPUT="${BUILD_DIR}/magic-${GO_OS}-${GO_ARCH}"

    case "$GO_OS" in
        windows)
            OUTPUT="${OUTPUT}.exe"
            ;;
    esac

    CGO_ENABLED=0 go build \
        -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${TIMESTAMP}" \
        -o "$OUTPUT" \
        ./cmd/magic

    # Create compressed archive
    cd "$BUILD_DIR"
    if command -v gzip &> /dev/null; then
        gzip -f "magic-${GO_OS}-${GO_ARCH}" 2>/dev/null || true
    fi

    log_info "CLI built: $OUTPUT"
    cd - > /dev/null
}

# Build Web UI
build_web() {
    log_info "Building Web UI..."

    if [ ! -d "web" ]; then
        log_warn "Web directory not found, skipping web build"
        return
    fi

    cd web

    if [ ! -f "package.json" ]; then
        log_warn "package.json not found, skipping web build"
        cd - > /dev/null
        return
    fi

    # Install dependencies if needed
    if [ ! -d "node_modules" ]; then
        log_info "Installing web dependencies..."
        npm install --legacy-peer-deps 2>/dev/null || pnpm install 2>/dev/null || yarn install
    fi

    # Build
    npm run build 2>/dev/null || pnpm build 2>/dev/null || yarn build

    cd - > /dev/null

    log_info "Web UI built"
}

# Build Docker image
build_docker() {
    log_info "Building Docker image..."

    if [ ! -f "Dockerfile" ]; then
        log_warn "Dockerfile not found, skipping docker build"
        return
    fi

    local IMAGE_NAME="go-magic:${VERSION}"
    docker build -t "$IMAGE_NAME" .

    log_info "Docker image built: $IMAGE_NAME"
}

# Build all
case "$TARGET" in
    cli)
        # Ensure web is built and copied for embedding
        build_web
        build_cli
        ;;
    web)
        build_web
        ;;
    docker)
        build_docker
        ;;
    all)
        # Build web first, then CLI (which embeds web assets), then Docker
        build_web
        build_cli
        build_docker
        ;;
    *)
        log_error "Unknown target: $TARGET"
        echo "Usage: $0 [cli|web|docker|all]"
        exit 1
        ;;
esac

log_info "Build complete!"