#!/bin/bash
set -e

# =============================================================================
# go-magic Cross-Platform Build Script
# =============================================================================
# Supports: Linux (386, amd64, arm, arm64), macOS (amd64, arm64),
#           Windows (386, amd64), FreeBSD, OpenBSD, NetBSD
# =============================================================================

set -e

# Configuration
VERSION=${VERSION:-"dev"}
BUILD_DIR=${BUILD_DIR:-"./dist"}
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
PROJECT_NAME="go-magic"
BINARY_NAME="magic"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "${BLUE}[STEP]${NC} $1"; }

# =============================================================================
# Platform Definitions
# =============================================================================

# All supported platforms
declare -A PLATFORMS=(
    # Linux
    ["linux-386"]="linux/386"
    ["linux-amd64"]="linux/amd64"
    ["linux-armv6"]="linux/armv6"
    ["linux-arm64"]="linux/arm64"
    ["linux-riscv64"]="linux/riscv64"
    ["linux-ppc64le"]="linux/ppc64le"
    ["linux-s390x"]="linux/s390x"

    # macOS
    ["darwin-amd64"]="darwin/amd64"
    ["darwin-arm64"]="darwin/arm64"

    # Windows
    ["windows-386"]="windows/386"
    ["windows-amd64"]="windows/amd64"
    ["windows-armv6"]="windows/armv6"
    ["windows-arm64"]="windows/arm64"

    # BSD
    ["freebsd-386"]="freebsd/386"
    ["freebsd-amd64"]="freebsd/amd64"
    ["freebsd-arm"]="freebsd/arm"
    ["openbsd-386"]="openbsd/386"
    ["openbsd-amd64"]="openbsd/amd64"
    ["openbsd-arm"]="openbsd/arm"
    ["netbsd-386"]="netbsd/386"
    ["netbsd-amd64"]="netbsd/amd64"
    ["netbsd-arm"]="netbsd/arm"
)

# Common platforms (most used)
COMMON_PLATFORMS=(
    "linux-amd64"
    "linux-arm64"
    "darwin-amd64"
    "darwin-arm64"
    "windows-amd64"
)

# =============================================================================
# Functions
# =============================================================================

show_help() {
    cat << EOF
${PROJECT_NAME} Cross-Platform Build Script

Usage: $0 [COMMAND] [OPTIONS]

Commands:
    all         Build all platforms (default)
    common      Build common platforms (Linux/macOS/Windows amd64+arm64)
    list        List all supported platforms
    install     Install built binaries to system

Platforms:
    linux-386, linux-amd64, linux-arm, linux-arm64
    linux-riscv64, linux-ppc64le, linux-s390x
    darwin-amd64, darwin-arm64
    windows-386, windows-amd64, windows-arm, windows-arm64
    freebsd-*, openbsd-*, netbsd-*

Options:
    --version <ver>    Set version (default: dev)
    --dir <path>       Set output directory (default: ./dist)
    --compress         Create compressed archives (.tar.gz/.zip)
    --checksum         Generate checksums
    --clean            Clean build directory first
    -h, --help         Show this help

Examples:
    $0                           # Build common platforms
    $0 all                       # Build all platforms
    $0 linux-amd64 darwin-arm64  # Build specific platforms
    $0 --compress --checksum     # Build with compression and checksums
    $0 --version v1.0.0          # Build with version v1.0.0
    $0 install                   # Install to ~/.local/bin

EOF
}

list_platforms() {
    echo "Supported platforms:"
    echo ""
    echo "Linux:"
    for key in linux-*; do
        echo "  - $key"
    done
    echo ""
    echo "macOS:"
    for key in darwin-*; do
        echo "  - $key"
    done
    echo ""
    echo "Windows:"
    for key in windows-*; do
        echo "  - $key"
    done
    echo ""
    echo "BSD:"
    for key in freebsd-* openbsd-* netbsd-*; do
        echo "  - $key"
    done
}

get_ldflags() {
    local commit=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    local ldflags="-s -w"
    ldflags="${ldflags} -X main.Version=${VERSION}"
    ldflags="${ldflags} -X main.BuildDate=${TIMESTAMP}"
    ldflags="${ldflags} -X main.Commit=${commit}"
    echo "$ldflags"
}

build_platform() {
    local platform=$1
    local output_dir=$2
    local compress=$3
    local os="${platform%%-*}"
    local arch="${platform##*-}"

    # Skip unsupported platforms (macOS arm64 requires go 1.16+)
    if [[ "$os" == "darwin" && "$arch" == "arm64" ]]; then
        local go_minor=$(go version | grep -oP 'go\d+\.\d+' | grep -oP '\d+\.\d+')
        if [[ "$(echo "$go_minor < 1.16" | bc)" == "1" ]]; then
            log_warn "Skipping $platform (requires Go 1.16+)"
            return 0
        fi
    fi

    local output_name="${BINARY_NAME}-${platform}"
    [[ "$os" == "windows" ]] && output_name="${output_name}.exe"

    local output_path="${output_dir}/${output_name}"

    log_info "Building ${platform}..."

    # Set CGO flags
    local cgo_enabled=0
    if [[ "$os" == "darwin" && "$arch" == "amd64" ]]; then
        cgo_enabled=0
    fi

    # Build
    CGO_ENABLED=$cgo_enabled GOOS=$os GOARCH=$arch go build \
        -ldflags="$(get_ldflags)" \
        -o "$output_path" \
        ./cmd/magic 2>/dev/null

    if [[ -f "$output_path" ]]; then
        log_info "Built: ${output_path} ($(du -h "$output_path" | cut -f1))"

        # Compress
        if [[ "$compress" == "true" ]]; then
            case "$os" in
                windows)
                    if command -v zip &> /dev/null; then
                        cd "$output_dir"
                        zip -q "${output_name}.zip" "${output_name}.exe"
                        rm -f "${output_name}.exe"
                        log_info "Created: ${output_name}.zip"
                        cd - > /dev/null
                    fi
                    ;;
                darwin|linux|freebsd|openbsd|netbsd)
                    if command -v gzip &> /dev/null; then
                        cd "$output_dir"
                        tar -czf "${output_name}.tar.gz" "${output_name}"
                        rm -f "${output_name}"
                        log_info "Created: ${output_name}.tar.gz"
                        cd - > /dev/null
                    fi
                    ;;
            esac
        fi
    else
        log_error "Failed to build ${platform}"
        return 1
    fi
}

generate_checksums() {
    local build_dir=$1
    log_info "Generating checksums..."

    cd "$build_dir"

    # Generate SHA256 checksums
    if command -v sha256sum &> /dev/null; then
        sha256sum * > checksums.txt
    elif command -v shasum &> /dev/null; then
        shasum -a 256 * > checksums.txt
    elif command -v sha256 &> /dev/null; then
        sha256 * > checksums.txt
    fi

    log_info "Checksums saved to checksums.txt"
    cat checksums.txt

    cd - > /dev/null
}

install_binaries() {
    local build_dir=$1
    local install_dir="${HOME}/.local/bin"

    log_info "Installing binaries to ${install_dir}..."

    mkdir -p "$install_dir"

    for binary in "${build_dir}"/*; do
        if [[ -f "$binary" && ! "$binary" =~ \.(tar\.gz|zip|checksums?\.txt)$ ]]; then
            local filename=$(basename "$binary")
            cp "$binary" "${install_dir}/${filename}"
            chmod +x "${install_dir}/${filename}"
            log_info "Installed: ${install_dir}/${filename}"
        fi
    done

    # Add to PATH if needed
    if [[ ":$PATH:" != *":${install_dir}:"* ]]; then
        log_warn "${install_dir} is not in your PATH"
        log_info "Add this to your shell profile:"
        echo ""
        echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\""
        echo ""
    fi
}

# =============================================================================
# Main
# =============================================================================

# Parse arguments
COMMAND=${1:-"common"}
shift || true

COMPRESS="false"
CHECKSUM="false"
CLEAN="false"
PLATFORMS_TO_BUILD=()

while [[ $# -gt 0 ]]; do
    case $1 in
        all)
            COMMAND="all"
            ;;
        common)
            COMMAND="common"
            ;;
        list)
            list_platforms
            exit 0
            ;;
        install)
            COMMAND="install"
            ;;
        --version)
            VERSION="$2"
            shift
            ;;
        --dir)
            BUILD_DIR="$2"
            shift
            ;;
        --compress)
            COMPRESS="true"
            ;;
        --checksum)
            CHECKSUM="true"
            ;;
        --clean)
            CLEAN="true"
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        linux-*|darwin-*|windows-*|freebsd-*|openbsd-*|netbsd-*)
            PLATFORMS_TO_BUILD+=("$1")
            ;;
        *)
            log_error "Unknown argument: $1"
            show_help
            exit 1
            ;;
    esac
    shift
done

# Clean build directory
if [[ "$CLEAN" == "true" ]]; then
    log_info "Cleaning build directory..."
    rm -rf "$BUILD_DIR"
fi

# Create build directory
mkdir -p "$BUILD_DIR"

# Determine platforms to build
case "$COMMAND" in
    all)
        PLATFORMS_TO_BUILD=("${!PLATFORMS[@]}")
        ;;
    common)
        PLATFORMS_TO_BUILD=("${COMMON_PLATFORMS[@]}")
        ;;
    install)
        install_binaries "$BUILD_DIR"
        exit 0
        ;;
esac

# Add specific platforms from command line
if [[ ${#PLATFORMS_TO_BUILD[@]} -eq 0 ]]; then
    PLATFORMS_TO_BUILD=("${COMMON_PLATFORMS[@]}")
fi

log_step "Building ${PROJECT_NAME} v${VERSION}"
echo ""

# Build each platform
for platform in "${PLATFORMS_TO_BUILD[@]}"; do
    build_platform "$platform" "$BUILD_DIR" "$COMPRESS"
done

echo ""

# Generate checksums
if [[ "$CHECKSUM" == "true" ]]; then
    generate_checksums "$BUILD_DIR"
fi

# Summary
echo ""
log_step "Build Summary"
echo ""
log_info "Version: ${VERSION}"
log_info "Build dir: ${BUILD_DIR}"
log_info "Platforms built: ${#PLATFORMS_TO_BUILD[@]}"
echo ""

ls -lh "$BUILD_DIR"/*.{tar.gz,zip,exe} 2>/dev/null || ls -lh "$BUILD_DIR"/* 2>/dev/null || true

echo ""
log_info "Build complete!"
