#!/bin/bash
set -e

# =============================================================================
# go-magic Homebrew Installer (Linuxbrew/macOS)
# =============================================================================

set -e

BREW_PREFIX="${BREW_PREFIX:-$(brew --prefix 2>/dev/null || echo '/usr/local')}"
TAP_NAME="magicwubiao/tap"
INSTALL_DIR="${BREW_PREFIX}/Library/Taps/magicwubiao/homebrew-tap"
BINARY_NAME="magic"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

show_help() {
    cat << EOF
go-magic Homebrew Installer

Usage: $0 [OPTIONS]

Options:
    --prefix <path>    Set Homebrew prefix (default: auto-detect)
    --tap              Also install the Homebrew tap
    --force            Force reinstall
    -h, --help         Show this help

Examples:
    $0                          # Install binary
    $0 --tap                    # Install with Homebrew tap
    $0 --prefix /opt/homebrew  # Custom prefix

EOF
}

# Parse arguments
INSTALL_TAP="false"
FORCE="false"

while [[ $# -gt 0 ]]; do
    case $1 in
        --tap)
            INSTALL_TAP="true"
            ;;
        --prefix)
            BREW_PREFIX="$2"
            shift
            ;;
        --force)
            FORCE="true"
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
    esac
    shift
done

# Detect OS and architecture
detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)

    case "$os" in
        darwin)
            case "$arch" in
                x86_64) echo "darwin-amd64" ;;
                arm64|aarch64) echo "darwin-arm64" ;;
                *) log_error "Unsupported macOS architecture: $arch"; exit 1 ;;
            esac
            ;;
        linux)
            case "$arch" in
                x86_64) echo "linux-amd64" ;;
                aarch64|arm64) echo "linux-arm64" ;;
                armv7l|armv6l) echo "linux-armv6" ;;
                *) log_error "Unsupported Linux architecture: $arch"; exit 1 ;;
            esac
            ;;
        *)
            log_error "Unsupported OS: $os"; exit 1 ;;
    esac
}

# Install Homebrew tap
install_tap() {
    log_info "Installing Homebrew tap..."

    # Create tap directory
    mkdir -p "${BREW_PREFIX}/Library/Taps/magicwubiao"

    # Clone or update tap
    if [[ -d "$INSTALL_DIR" ]]; then
        if [[ "$FORCE" == "true" ]]; then
            cd "$INSTALL_DIR"
            git pull origin master
            cd - > /dev/null
        fi
    else
        git clone https://github.com/magicwubiao/homebrew-tap "$INSTALL_DIR"
    fi

    log_info "Tap installed: $INSTALL_DIR"
}

# Install binary
install_binary() {
    local platform=$(detect_platform)
    local version="${VERSION:-"latest"}"
    local download_url

    log_info "Detected platform: $platform"

    # Get download URL based on version
    if [[ "$version" == "latest" ]]; then
        download_url="https://github.com/magicwubiao/go-magic/releases/latest/download/magic-${platform}"
    else
        download_url="https://github.com/magicwubiao/go-magic/releases/download/${version}/magic-${platform}"
    fi

    # For Windows, add .exe extension
    [[ "$platform" == windows-* ]] && download_url="${download_url}.exe"

    local bin_dir="${BREW_PREFIX}/bin"
    local install_path="${bin_dir}/${BINARY_NAME}"

    mkdir -p "$bin_dir"

    log_info "Downloading from $download_url..."

    if command -v curl &> /dev/null; then
        curl -sSL "$download_url" -o "$install_path"
    elif command -v wget &> /dev/null; then
        wget -q "$download_url" -O "$install_path"
    else
        log_error "Neither curl nor wget found"
        exit 1
    fi

    chmod +x "$install_path"

    log_info "Installed to $install_path"

    # Verify installation
    if "$install_path" --version &> /dev/null; then
        log_info "Installation verified!"
    else
        log_warn "Binary may not be correctly installed"
    fi
}

# Main
echo ""
log_info "go-magic Homebrew Installer"
echo ""

if [[ "$INSTALL_TAP" == "true" ]]; then
    install_tap
    echo ""
    log_info "Now you can install with: brew install go-magic"
    echo ""
else
    install_binary
fi

echo ""
log_info "Done!"
