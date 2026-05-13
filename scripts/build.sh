#!/usr/bin/env bash
# ===========================================
# go-magic 打包脚本
# 打包为多平台可执行文件
# ===========================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }

# Config
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${PROJECT_DIR}/dist"
VERSION=$(git describe --tags 2>/dev/null || echo "1.0.0")

# Clean
clean() {
    info "Cleaning build directory..."
    rm -rf "$BUILD_DIR"
    mkdir -p "$BUILD_DIR"
}

# Build Go binary
build_go() {
    local os=$1
    local arch=$2
    local output="${BUILD_DIR}/go-magic-${os}-${arch}/magic"
    
    info "Building Go binary for ${os}/${arch}..."
    
    mkdir -p "$(dirname "$output")"
    
    GOOS=$os GOARCH=$arch go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o "$output" \
        "${PROJECT_DIR}/cmd/magic"
    
    success "Built: $output"
}

# Build all platforms
build_all() {
    info "Building for all platforms..."
    
    # Linux
    build_go linux amd64
    build_go linux arm64
    
    # macOS
    build_go darwin amd64
    build_go darwin arm64
    
    # Windows
    build_go windows amd64
    
    success "All Go binaries built!"
}

# Package archives
package_archives() {
    info "Creating archives..."
    
    cd "$BUILD_DIR"
    
    for dir in go-magic-*-amd64 go-magic-*-arm64; do
        if [ -d "$dir" ]; then
            tar -czf "${dir}.tar.gz" "$dir"
            success "Created: ${dir}.tar.gz"
        fi
    done
    
    # Windows ZIP
    if [ -d "go-magic-windows-amd64" ]; then
        zip -r "go-magic-windows-amd64.zip" "go-magic-windows-amd64" 2>/dev/null || \
            powershell -Command "Compress-Archive -Path 'go-magic-windows-amd64' -DestinationPath 'go-magic-windows-amd64.zip'"
        success "Created: go-magic-windows-amd64.zip"
    fi
}

# Build web app
build_web() {
    info "Building web app..."
    
    cd "${PROJECT_DIR}/web"
    
    if [ ! -d "node_modules" ]; then
        npm install
    fi
    
    npm run build
    
    success "Web app built!"
}

# Package everything
package_all() {
    info "Packaging for distribution..."
    
    cd "$BUILD_DIR"
    
    # Copy web dist to each binary
    local web_dist="${PROJECT_DIR}/web/dist"
    
    for dir in go-magic-*/; do
        if [ -d "$dir" ]; then
            if [ -d "$web_dist" ]; then
                mkdir -p "${dir}web"
                cp -r "$web_dist/"* "${dir}web/"
            fi
            
            # Copy wrapper script
            cp "${PROJECT_DIR}/scripts/web_wrapper.py" "${dir}" 2>/dev/null || true
            
            # Copy config template
            mkdir -p "${dir}config"
            cat > "${dir}config/config.yaml" << 'EOF'
# go-magic configuration
provider:
  name: openai
  model: gpt-4o

memory:
  enabled: true

server:
  port: 8642
  web_port: 8648
EOF
        fi
    done
    
    # Re-create archives with web included
    package_archives
    
    success "All packages created!"
}

# Build Docker images
build_docker() {
    info "Building Docker images..."
    
    cd "${PROJECT_DIR}"
    
    # Build and push
    docker build -t "ghcr.io/magicwubiao/go-magic:latest" .
    docker build -t "magicwubiao/go-magic:latest" .
    
    # Multi-arch build (requires buildx)
    if command -v docker &> /dev/null && docker buildx version &> /dev/null; then
        docker buildx build \
            --platform linux/amd64,linux/arm64 \
            -t "ghcr.io/magicwubiao/go-magic:latest" \
            --push .
    fi
    
    success "Docker images built!"
}

# Build web wrapper installer
build_wrapper_installer() {
    info "Building web wrapper installer..."
    
    local pyinstaller="${PROJECT_DIR}/scripts/pyinstaller"
    mkdir -p "$pyinstaller"
    
    cat > "${pyinstaller}/spec.magic" << 'PYEOF'
# -*- mode: python ; coding: utf-8 -*-

block_cipher = None

a = Analysis(['../web_wrapper.py'],
             pathex=[],
             binaries=[],
             datas=[
                 ('../web/dist', 'web'),
             ],
             hiddenimports=[],
             hookspath=[],
             hooksconfig={},
             runtime_hooks=[],
             excludes=[],
             win_no_prefer_redirects=False,
             win_private_assemblies=False,
             cipher=block_cipher,
             noarchive=False)

pyz = PYZ(a.pure, a.zipped_data, cipher=block_cipher)

exe = EXE(pyz,
          a.scripts,
          [],
          exclude_binaries=True,
          name='go-magic-web',
          debug=False,
          bootloader_ignore_signals=False,
          strip=False,
          upx=True,
          console=False,
          disable_windowed_traceback=False,
          argv_emulation=False,
          target_arch=None,
          codesign_identity=None,
          entitlements_file=None)

coll = COLLECT(exe,
               a.binaries,
               a.zipfiles,
               a.datas,
               strip=False,
               upx=True,
               upx_exclude=[],
               name='go-magic-web')
PYEOF

    # 构建 Linux 版本
    if command -v pyinstaller &> /dev/null; then
        cd "${pyinstaller}"
        pyinstaller spec.magic
        success "PyInstaller build completed"
    else
        info "PyInstaller not found, skipping..."
    fi
}

# Upload releases
upload_releases() {
    info "Uploading to GitHub Releases..."
    
    if ! command -v gh &> /dev/null; then
        info "GitHub CLI not found. Install from: https://cli.github.com/"
        return
    fi
    
    # Create release
    gh release create "v${VERSION}" \
        --title "go-magic v${VERSION}" \
        --notes "Release v${VERSION}" \
        --draft \
        "${BUILD_DIR}"/*.tar.gz \
        "${BUILD_DIR}"/*.zip || true
    
    success "Release draft created!"
}

# Show summary
show_summary() {
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                   Build Complete!                        ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "Build directory: ${BUILD_DIR}"
    echo "Version: ${VERSION}"
    echo ""
    echo "Contents:"
    ls -la "${BUILD_DIR}"
    echo ""
}

# Main
main() {
    cd "${PROJECT_DIR}"
    
    case "${1:-all}" in
        clean)
            clean
            ;;
        go)
            clean
            build_go linux amd64
            ;;
        web)
            build_web
            ;;
        docker)
            build_docker
            ;;
        all)
            clean
            build_all
            build_web
            package_all
            ;;
        release)
            build_all
            build_web
            package_all
            build_docker
            upload_releases
            ;;
        *)
            echo "Usage: $0 {clean|go|web|docker|all|release}"
            echo ""
            echo "  clean   - Clean build directory"
            echo "  go      - Build Go binaries only"
            echo "  web     - Build web app only"
            echo "  docker  - Build Docker images only"
            echo "  all     - Build everything (default)"
            echo "  release - Build and upload releases"
            ;;
    esac
    
    if [ "$1" = "all" ] || [ -z "$1" ]; then
        show_summary
    fi
}

main "$@"
