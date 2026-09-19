#!/usr/bin/env bash
# =============================================================================
# go-magic Homebrew / 直接下载安装器（macOS / Linuxbrew）
# =============================================================================
# 与 CI Release 资产严格一致的命名：go-magic-<os>-<arch>[.exe]
#   go-magic-darwin-amd64  go-magic-darwin-arm64
#   go-magic-linux-amd64   go-magic-linux-arm64
#
# 用法:
#   ./install-homebrew.sh                 # 下载二进制装到 <brew-prefix>/bin/magic
#   ./install-homebrew.sh --tap           # 安装/更新 Homebrew tap（magicwubiao/tap）
#   ./install-homebrew.sh --version v0.5.19
# =============================================================================
set -euo pipefail

REPO="magicwubiao/go-magic"
GITHUB_API="https://api.github.com/repos/${REPO}"
TAP_NAME="magicwubiao/tap"
TAP_REPO="https://github.com/magicwubiao/homebrew-tap"
BINARY_NAME="magic"
VERSION="${VERSION:-}"

BREW_PREFIX="${BREW_PREFIX:-}"
INSTALL_TAP="false"
FORCE="false"

# 颜色
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
    RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; NC=''
fi
log_info()  { printf '%b[INFO]%b %s\n' "$GREEN"  "$NC" "$*"; }
log_warn()  { printf '%b[WARN]%b %s\n' "$YELLOW" "$NC" "$*" >&2; }
log_error() { printf '%b[FAIL]%b %s\n' "$RED"    "$NC" "$*" >&2; exit 1; }

TMP_FILE=""
cleanup() { [[ -n "$TMP_FILE" && -e "$TMP_FILE" ]] && rm -f "$TMP_FILE"; return 0; }
trap cleanup EXIT

show_help() {
    cat <<EOF
go-magic Homebrew 安装器

用法: $0 [选项]

选项:
    --prefix <path>   Homebrew prefix（默认自动检测 brew --prefix）
    --tap             安装/更新 Homebrew tap（${TAP_NAME}）后由 brew 接管
    --version <ver>   指定版本，例如 v0.5.19（默认取最新 Release）
    --force           强制重新安装
    -h, --help        显示帮助

示例:
    $0                       # 安装二进制到 <prefix>/bin/magic
    $0 --tap                 # 安装 tap，然后可执行 brew install ${TAP_NAME}/go-magic
    $0 --version v0.5.19
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)
            [[ -n "${2:-}" ]] || log_error "--prefix 需要参数"
            BREW_PREFIX="$2"; shift
            ;;
        --tap)
            INSTALL_TAP="true"
            ;;
        --version)
            [[ -n "${2:-}" ]] || log_error "--version 需要参数"
            VERSION="$2"; shift
            ;;
        --force)
            FORCE="true"
            ;;
        -h|--help)
            show_help; exit 0
            ;;
        *)
            log_error "未知参数: $1（用 --help 查看用法）"
            ;;
    esac
    shift
done

if [[ -z "$BREW_PREFIX" ]]; then
    if command -v brew >/dev/null 2>&1; then
        BREW_PREFIX="$(brew --prefix)"
    else
        log_error "未找到 brew，请用 --prefix 指定 Homebrew 前缀（如 /opt/homebrew 或 /usr/local）"
    fi
fi

# 只接受 CI 真实发布过的组合
detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$arch" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) log_error "不支持的架构: $(uname -m)
Release 仅提供 amd64 / arm64；其它架构请从源码构建: go install github.com/${REPO}/cmd/magic@latest" ;;
    esac

    case "$os" in
        darwin|linux) printf '%s-%s\n' "$os" "$arch" ;;
        *) log_error "不支持的操作系统: $os（本脚本仅支持 macOS / Linux）" ;;
    esac
}

resolve_version() {
    if [[ -n "$VERSION" ]]; then
        printf '%s\n' "$VERSION"
        return 0
    fi
    command -v curl >/dev/null 2>&1 || log_error "需要 curl 查询最新版本，或用 --version 指定"

    local json tag auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")

    json="$(curl -fsSL "${auth[@]}" "$GITHUB_API/releases/latest" 2>/dev/null)" \
        || log_error "无法获取最新版本（网络或速率限制）。请用 --version 指定，或设置 GITHUB_TOKEN"
    tag="$(printf '%s' "$json" | tr -d '\r\n' \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [[ -n "$tag" ]] || log_error "无法解析版本号，请用 --version 指定"
    printf '%s\n' "$tag"
}

ensure_v_prefix() {
    case "$1" in
        v*) printf '%s\n' "$1" ;;
        *)  printf 'v%s\n' "$1" ;;
    esac
}

download() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        # 必须带 -f：否则 404 的 HTML 会被保存成“二进制”
        curl -fsSL --retry 3 --retry-delay 2 -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$dest" "$url"
    else
        log_error "未找到 curl 或 wget"
    fi
}

install_tap() {
    log_info "安装 Homebrew tap ${TAP_NAME}"

    command -v git >/dev/null 2>&1 || log_error "需要 git 才能安装 tap"

    local tap_dir="${BREW_PREFIX}/Library/Taps/magicwubiao/homebrew-tap"

    if [[ -d "$tap_dir/.git" ]]; then
        log_info "tap 已存在，尝试更新"
        if [[ "$FORCE" == "true" ]]; then
            ( cd "$tap_dir" && git pull --ff-only ) || log_warn "tap 更新失败，继续使用本地副本"
        else
            log_info "跳过更新（加 --force 可强制更新）"
        fi
    else
        mkdir -p "$(dirname "$tap_dir")"
        git clone --depth 1 "$TAP_REPO" "$tap_dir"
    fi

    log_info "tap 已安装: $tap_dir"
    log_info "现在可以执行: brew install ${TAP_NAME}/go-magic"
}

install_binary() {
    local platform tag asset url size bin_dir install_path
    platform="$(detect_platform)"
    tag="$(ensure_v_prefix "$(resolve_version)")"
    asset="go-magic-${platform}"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

    log_info "平台: ${platform}"
    log_info "版本: ${tag}"
    log_info "下载: ${url}"

    bin_dir="${BREW_PREFIX}/bin"
    install_path="${bin_dir}/${BINARY_NAME}"
    mkdir -p "$bin_dir"

    if [[ -e "$install_path" && "$FORCE" != "true" ]]; then
        log_warn "$install_path 已存在，覆盖安装（加 --force 可显式确认）"
    fi

    TMP_FILE="$(mktemp "${TMPDIR:-/tmp}/go-magic.XXXXXX")"
    download "$url" "$TMP_FILE" || log_error "下载失败: $url"

    size="$(wc -c <"$TMP_FILE" | tr -d ' ')"
    if [[ "$size" -lt 1048576 ]]; then
        log_error "下载内容异常（仅 ${size} 字节），该版本可能没有对应平台产物: ${asset}"
    fi

    chmod 0755 "$TMP_FILE"
    mv -f "$TMP_FILE" "$install_path"
    TMP_FILE=""
    log_info "已安装: $install_path"

    if "$install_path" --version >/dev/null 2>&1; then
        log_info "验证通过: $("$install_path" --version)"
    else
        log_warn "验证失败，请手动检查: $install_path --version"
    fi

    case ":${PATH}:" in
        *":${bin_dir}:"*) ;;
        *) log_warn "${bin_dir} 不在 PATH 中" ;;
    esac
}

echo ""
log_info "go-magic Homebrew 安装器"
echo ""

if [[ "$INSTALL_TAP" == "true" ]]; then
    install_tap
else
    install_binary
fi

echo ""
log_info "完成"
