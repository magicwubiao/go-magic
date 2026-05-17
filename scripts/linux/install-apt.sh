#!/bin/bash
# =============================================================================
# go-magic APT Repository Installer (Debian/Ubuntu)
# =============================================================================

set -e

REPO_NAME="magicwubiao"
REPO_URL="https://packages.magicwubiao.com"
DISTRIBUTION=$(lsb_release -cs 2>/dev/null || echo "stable")
ARCH=$(dpkg --print-architecture)

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
go-magic APT Repository Installer

Usage: $0 [OPTIONS]

This script installs the go-magic APT repository and package.

Options:
    --repo-url <url>    Repository URL (default: $REPO_URL)
    --distribution     Distribution name (default: auto-detect)
    --install          Install the package after adding repo
    -h, --help         Show this help

Examples:
    $0                          # Add repository only
    $0 --install                # Add repository and install package
    $0 --distribution focal     # Use specific distribution

Requires: curl, apt, lsb-release (optional)

EOF
}

# Parse arguments
INSTALL_PACKAGE="false"

while [[ $# -gt 0 ]]; do
    case $1 in
        --repo-url)
            REPO_URL="$2"
            shift
            ;;
        --distribution)
            DISTRIBUTION="$2"
            shift
            ;;
        --install)
            INSTALL_PACKAGE="true"
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            log_error "Unknown argument: $1"
            show_help
            exit 1
            ;;
    esac
    shift
done

echo ""
log_info "go-magic APT Repository Installer"
echo ""

# Check for required tools
if ! command -v curl &> /dev/null; then
    log_error "curl is required but not installed"
    log_info "Run: sudo apt install curl"
    exit 1
fi

if ! command -v apt &> /dev/null; then
    log_error "apt is required but not installed"
    exit 1
fi

# Detect distribution if not specified and lsb-release available
if [[ "$DISTRIBUTION" == "stable" ]] && command -v lsb_release &> /dev/null; then
    DISTRIBUTION=$(lsb_release -cs)
fi

log_info "Distribution: $DISTRIBUTION"
log_info "Architecture: $ARCH"

# Add repository
log_info "Adding repository..."

# Create apt directory
sudo mkdir -p "/etc/apt/trusted.gpg.d"
sudo mkdir -p "/etc/apt/sources.list.d"

# Download and install GPG key
KEYRING_FILE="/etc/apt/trusted.gpg.d/${REPO_NAME}.gpg"
curl -fsSL "${REPO_URL}/keys/public.gpg" | sudo gpg --dearmor -o "$KEYRING_FILE"

# Add repository
REPO_FILE="/etc/apt/sources.list.d/${REPO_NAME}.list"
echo "deb [signed-by=${KEYRING_FILE}] ${REPO_URL} ${DISTRIBUTION} main" | sudo tee "$REPO_FILE"

# Update apt
log_info "Updating package lists..."
sudo apt update

# Install package
if [[ "$INSTALL_PACKAGE" == "true" ]]; then
    log_info "Installing go-magic..."
    sudo apt install -y go-magic
fi

echo ""
log_info "Done!"
echo ""
log_info "Repository added: $REPO_FILE"
log_info "Package: go-magic"
echo ""
