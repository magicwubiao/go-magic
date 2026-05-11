#!/usr/bin/env bash
# ===========================================
# go-magic 一键安装脚本
# Install: curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
# ===========================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Config
REPO="magicwubiao/go-magic"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.go-magic}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
GITHUB_RAW="https://raw.githubusercontent.com/${REPO}"
GITHUB_API="https://api.github.com/repos/${REPO}"

# Functions
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "linux" ;;
        Darwin*)    echo "darwin" ;;
        *)          echo "unsupported" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64)     echo "amd64" ;;
        aarch64)    echo "arm64" ;;
        armv7)      echo "arm" ;;
        *)          echo "amd64" ;;
    esac
}

# Get latest version
get_latest_version() {
    if [ "$VERSION" = "latest" ]; then
        VERSION=$(curl -s "$GITHUB_API/releases/latest" | grep '"tag_name"' | cut -d'"' -f4 | sed 's/v//')
        [ -z "$VERSION" ] && VERSION="1.0.0"
    fi
}

# Download binary
download_binary() {
    local os=$1
    local arch=$2
    local filename="go-magic-${os}-${arch}"
    local url="https://github.com/${REPO}/releases/download/v${VERSION}/${filename}.tar.gz"
    
    info "Downloading go-magic v${VERSION} for ${os}/${arch}..."
    
    mkdir -p "$INSTALL_DIR"
    cd "$INSTALL_DIR"
    
    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "${filename}.tar.gz" || error "Download failed"
    elif command -v wget &> /dev/null; then
        wget -q "$url" -O "${filename}.tar.gz" || error "Download failed"
    else
        error "Neither curl nor wget found"
    fi
    
    info "Extracting..."
    tar -xzf "${filename}.tar.gz" || error "Extraction failed"
    rm "${filename}.tar.gz"
    
    success "Downloaded and extracted to $INSTALL_DIR"
}

# Install binary
install_binary() {
    local magic_bin="$INSTALL_DIR/magic"
    local link_bin="$BIN_DIR/magic"
    
    # Create bin directory
    mkdir -p "$BIN_DIR"
    
    # Create symlink or copy
    if [ -L "$link_bin" ]; then
        rm "$link_bin"
    fi
    
    ln -sf "$magic_bin" "$link_bin"
    chmod +x "$magic_bin"
    
    success "Installed to $link_bin"
}

# Setup config
setup_config() {
    local config_dir="$HOME/.go-magic"
    
    if [ ! -d "$config_dir" ]; then
        mkdir -p "$config_dir"
        
        # Create default config
        cat > "$config_dir/config.yaml" << 'EOF'
# go-magic configuration
provider:
  name: openai
  model: gpt-4

memory:
  enabled: true

tools:
  enabled:
    - web
    - terminal
    - file
EOF
        success "Created config at $config_dir/config.yaml"
    fi
}

# Verify installation
verify_install() {
    local magic_bin="$INSTALL_DIR/magic"
    
    if [ -f "$magic_bin" ]; then
        local version=$("$magic_bin" --version 2>&1 || echo "unknown")
        success "Installation verified! Version: $version"
    else
        error "Binary not found at $magic_bin"
    fi
}

# Docker installation
install_docker() {
    info "Installing via Docker..."
    
    if ! command -v docker &> /dev/null; then
        error "Docker not found. Please install Docker first: https://docs.docker.com/get-docker/"
    fi
    
    # Pull image
    info "Pulling go-magic image..."
    docker pull "ghcr.io/${REPO}:latest" || docker pull "${REPO}:latest" || true
    
    # Create directories
    mkdir -p "$HOME/.go-magic"
    
    # Run container
    info "Starting container..."
    docker run -d \
        --name go-magic \
        --restart unless-stopped \
        -p 8642:8642 \
        -p 8643:8643 \
        -v "$HOME/.go-magic:/home/magic/.go-magic" \
        "ghcr.io/${REPO}:latest" serve
    
    success "Docker container started!"
    info "API available at http://localhost:8642"
    info "Web UI available at http://localhost:8648"
}

# Docker Compose installation
install_compose() {
    info "Installing via Docker Compose..."
    
    if ! command -v docker &> /dev/null; then
        error "Docker not found"
    fi
    
    if ! command -v docker compose &> /dev/null && ! command -v docker-compose &> /dev/null; then
        error "Docker Compose not found"
    fi
    
    local compose_cmd="docker compose"
    $compose_cmd version &> /dev/null || compose_cmd="docker-compose"
    
    info "Pulling images..."
    $compose_cmd pull
    
    info "Starting services..."
    $compose_cmd up -d
    
    success "Docker Compose stack started!"
}

# Main installation
main() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║           go-magic AI Agent Installer                  ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo ""
    
    # Check if Docker mode
    if [ "$1" = "--docker" ]; then
        install_docker
        exit 0
    fi
    
    if [ "$1" = "--compose" ]; then
        install_compose
        exit 0
    fi
    
    # Detect system
    local os=$(detect_os)
    local arch=$(detect_arch)
    
    if [ "$os" = "unsupported" ]; then
        error "Unsupported operating system"
    fi
    
    info "Detected: ${os}/${arch}"
    
    # Get version
    get_latest_version
    info "Installing version: ${VERSION}"
    
    # Download
    download_binary "$os" "$arch"
    
    # Install
    install_binary
    
    # Setup
    setup_config
    
    # Verify
    verify_install
    
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                   Installation Complete!                  ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo "  2. Configure:   magic setup"
    echo "  3. Start:       magic serve"
    echo ""
    echo "Alternative (Docker):"
    echo "  curl -fsSL https://... | bash -s -- --docker"
    echo ""
}

# Run
main "$@"
