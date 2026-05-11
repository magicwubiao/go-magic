#!/usr/bin/env bash
# =============================================================================
# go-magic 一键安装脚本
# =============================================================================
# Install: curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
#
# Supported platforms:
#   Linux:   amd64, arm64, armv6, 386, ppc64le, s390x, riscv64
#   macOS:   amd64, arm64
#   Windows: amd64, 386, arm64
#   FreeBSD, OpenBSD, NetBSD
#
# Installation methods:
#   - Binary download (default)
#   - Homebrew (macOS/Linux)
#   - Docker
#   - APT (Debian/Ubuntu)
#   - Scoop (Windows)
# =============================================================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Config
REPO="magicwubiao/go-magic"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.go-magic}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
GITHUB_API="https://api.github.com/repos/${REPO}"
INSTALL_METHOD="${INSTALL_METHOD:-binary}"

# Functions
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

show_help() {
    cat << EOF
${GREEN}go-magic 一键安装脚本${NC}

用法: curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash [OPTIONS]

选项:
    --method <method>      安装方式: binary, homebrew, docker, apt (default: binary)
    --version <ver>        指定版本 (default: latest)
    --dir <path>           安装目录 (default: ~/.go-magic)
    --bin-dir <path>       bin 目录 (default: ~/.local/bin)
    --help                 显示此帮助

安装方式:
    binary      下载预编译二进制 (默认)
    homebrew    使用 Homebrew (macOS/Linux)
    docker      使用 Docker
    apt         使用 APT 仓库 (Debian/Ubuntu)

示例:
    curl -fsSL ... | bash                    # 默认安装
    curl -fsSL ... | bash --method docker    # Docker 安装
    curl -fsSL ... | bash --version v1.0.0   # 指定版本
    curl -fsSL ... | bash --method apt       # APT 安装

支持的平台:
    Linux:   amd64, arm64, armv6, 386, ppc64le, s390x, riscv64
    macOS:   amd64, arm64
    Windows: amd64, 386, arm64
EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --method)
            INSTALL_METHOD="$2"
            shift
            ;;
        --version)
            VERSION="$2"
            shift
            ;;
        --dir)
            INSTALL_DIR="$2"
            shift
            ;;
        --bin-dir)
            BIN_DIR="$2"
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            error "Unknown argument: $1"
            ;;
    esac
    shift
done

# Detect OS
detect_os() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux*)     echo "linux" ;;
        darwin*)    echo "darwin" ;;
        freebsd*)   echo "freebsd" ;;
        openbsd*)   echo "openbsd" ;;
        netbsd*)    echo "netbsd" ;;
        mingw*|cygwin*|msys*)
            echo "windows" ;;
        *)          echo "unsupported" ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch=$(uname -m)
    case "$arch" in
        x86_64)     echo "amd64" ;;
        aarch64)    echo "arm64" ;;
        armv7l)     echo "armv6" ;;
        armv6l)     echo "armv6" ;;
        i386|i686)  echo "386" ;;
        ppc64le)    echo "ppc64le" ;;
        s390x)      echo "s390x" ;;
        riscv64)    echo "riscv64" ;;
        arm64)      echo "arm64" ;;
        *)          echo "amd64" ;;
    esac
}

# Get latest version
get_latest_version() {
    if [ "$VERSION" = "latest" ]; then
        info "获取最新版本..."
        VERSION=$(curl -s "$GITHUB_API/releases/latest" | grep '"tag_name"' | cut -d'"' -f4 | sed 's/v//')
        [ -z "$VERSION" ] && VERSION="1.0.0"
    fi
}

# Binary installation
install_binary() {
    local os=$(detect_os)
    local arch=$(detect_arch)

    [ "$os" = "unsupported" ] && error "不支持的操作系统"

    get_latest_version

    info "正在下载 go-magic v${VERSION} (${os}/${arch})..."

    local filename="magic-${os}-${arch}"
    local extension="tar.gz"
    [ "$os" = "windows" ] && extension="zip" && filename="${filename}.exe"

    local url="https://github.com/${REPO}/releases/download/v${VERSION}/${filename}.${extension}"

    mkdir -p "$INSTALL_DIR"
    cd "$INSTALL_DIR"

    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "magic-install.${extension}" || error "下载失败"
    elif command -v wget &> /dev/null; then
        wget -q "$url" -O "magic-install.${extension}" || error "下载失败"
    else
        error "未找到 curl 或 wget"
    fi

    info "正在解压..."
    if [ "$extension" = "tar.gz" ]; then
        tar -xzf "magic-install.${extension}" || error "解压失败"
        rm -f "magic-install.${extension}"
    else
        unzip -q "magic-install.${extension}" || error "解压失败"
        rm -f "magic-install.${extension}"
    fi

    success "下载并解压到 $INSTALL_DIR"

    # Create symlink
    mkdir -p "$BIN_DIR"
    local link="$BIN_DIR/magic"
    [ "$os" = "windows" ] && link="${link}.exe"

    if [ -L "$link" ]; then
        rm "$link"
    fi
    ln -sf "${INSTALL_DIR}/magic" "$link"
    chmod +x "${INSTALL_DIR}/magic"

    success "已创建符号链接: $link"

    # PATH 提示
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        warn "${BIN_DIR} 不在 PATH 中"
        info "请添加到 shell 配置文件 (~/.bashrc 或 ~/.zshrc):"
        echo ""
        echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\""
        echo ""
    fi

    # Verify
    if "${INSTALL_DIR}/magic" --version &> /dev/null; then
        success "安装验证成功!"
    else
        warn "安装验证失败"
    fi
}

# Homebrew installation
install_homebrew() {
    local os=$(detect_os)
    [ "$os" = "windows" ] && error "Windows 不支持 Homebrew，请使用 --method scoop"

    info "正在安装 Homebrew..."

    # Check if Homebrew installed
    if ! command -v brew &> /dev/null; then
        /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" || error "Homebrew 安装失败"
    fi

    # Add tap
    info "正在添加 Homebrew tap..."
    brew tap "$REPO" || true

    # Install
    info "正在安装 go-magic..."
    brew install go-magic

    success "Homebrew 安装完成!"
}

# Docker installation
install_docker() {
    info "正在安装 Docker 镜像..."

    if ! command -v docker &> /dev/null; then
        error "未找到 Docker，请先安装: https://docs.docker.com/get-docker/"
    fi

    get_latest_version

    info "正在拉取镜像..."
    docker pull "${REPO}:v${VERSION}"

    success "Docker 镜像拉取成功!"
    info "运行: docker run -it ${REPO}:v${VERSION}"
}

# APT installation
install_apt() {
    local os=$(detect_os)
    [ "$os" != "linux" ] && error "APT 安装仅支持 Linux"

    info "正在配置 APT 仓库..."

    # Add GPG key
    curl -fsSL "https://packages.magicwubiao.com/keys/public.gpg" | sudo gpg --dearmor -o /etc/apt/trusted.gpg.d/magicwubiao.gpg

    # Add repository
    local codename=$(lsb_release -cs 2>/dev/null || echo "stable")
    echo "deb [signed-by=/etc/apt/trusted.gpg.d/magicwubiao.gpg] https://packages.magicwubiao.com ${codename} main" | sudo tee /etc/apt/sources.list.d/magicwubiao.list

    # Update and install
    info "正在更新并安装..."
    sudo apt update && sudo apt install -y go-magic

    success "APT 安装完成!"
}

# Scoop installation (Windows)
install_scoop() {
    info "正在安装 Scoop..."

    if ! command -v scoop &> /dev/null; then
        powershell -ExecutionPolicy Bypass -c "irm get.scoop.sh | iex" || error "Scoop 安装失败"
    fi

    # Add bucket
    info "正在添加 Scoop bucket..."
    scoop bucket add magic https://github.com/magicwubiao/scoop-bucket || true

    # Install
    info "正在安装 go-magic..."
    scoop install magic

    success "Scoop 安装完成!"
}

# =============================================================================
# Main
# =============================================================================

echo ""
echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║      go-magic 一键安装脚本             ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
echo ""
info "安装方式: $INSTALL_METHOD"
echo ""

case "$INSTALL_METHOD" in
    binary)
        install_binary
        ;;
    homebrew)
        install_homebrew
        ;;
    docker)
        install_docker
        ;;
    apt)
        install_apt
        ;;
    scoop)
        install_scoop
        ;;
    *)
        error "未知安装方式: $INSTALL_METHOD"
        ;;
esac

echo ""
success "安装完成!"
echo ""
info "使用: magic --help"
echo ""
