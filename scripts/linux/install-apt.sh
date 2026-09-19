#!/usr/bin/env bash
# =============================================================================
# go-magic Debian/Ubuntu 安装脚本
# =============================================================================
# 默认（也是最可靠的）方式：直接从 GitHub Release 安装 CI 产出的 .deb
#   https://github.com/magicwubiao/go-magic/releases/download/v0.5.19/go-magic_amd64.deb
#   CI 目前只构建 amd64 的 .deb（见 .github/workflows/release.yml）
#
# 备选方式：配置自建 APT 仓库（需要显式给出 --repo-url，CI 并不托管 APT 仓库）
#
# 用法:
#   ./install-apt.sh                       # 从 Release 下载 .deb 并安装
#   ./install-apt.sh --version v0.5.19
#   ./install-apt.sh --repo-url https://packages.example.com --install
#   ./install-apt.sh --remove              # 移除仓库配置与 GPG key
# =============================================================================
set -euo pipefail

REPO="magicwubiao/go-magic"
KEYRING_FILE="/etc/apt/keyrings/go-magic.gpg"
REPO_FILE="/etc/apt/sources.list.d/go-magic.list"
VERSION="${VERSION:-}"
REPO_URL=""
DISTRIBUTION=""
DO_REMOVE="false"

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
go-magic Debian/Ubuntu 安装脚本

用法: $0 [选项]

选项:
    --version <ver>       指定版本，例如 v0.5.19（默认取最新 Release）
    --repo-url <url>      使用自建 APT 仓库（不指定则直接用 GitHub Release 的 .deb）
    --distribution <cod>  仓库模式下的发行版代号（默认 lsb_release -cs）
    --remove              移除 APT 仓库配置与 GPG key
    -h, --help            显示帮助

示例:
    $0                                  # 最简：安装 Release 里的 .deb
    $0 --version v0.5.19
    $0 --repo-url https://packages.example.com
    $0 --remove
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)
            [[ -n "${2:-}" ]] || log_error "--version 需要参数"
            VERSION="$2"; shift
            ;;
        --repo-url)
            [[ -n "${2:-}" ]] || log_error "--repo-url 需要参数"
            REPO_URL="$2"; shift
            ;;
        --distribution)
            [[ -n "${2:-}" ]] || log_error "--distribution 需要参数"
            DISTRIBUTION="$2"; shift
            ;;
        --remove)
            DO_REMOVE="true"
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

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || log_error "缺少必需命令: $1${2:+（$2）}"
}

ensure_v_prefix() {
    case "$1" in
        v*) printf '%s\n' "$1" ;;
        *)  printf 'v%s\n' "$1" ;;
    esac
}

resolve_version() {
    if [[ -n "$VERSION" ]]; then
        printf '%s\n' "$VERSION"
        return 0
    fi
    require_cmd curl
    local json tag auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
    json="$(curl -fsSL "${auth[@]}" "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null)" \
        || log_error "无法获取最新版本，请用 --version 指定（或设置 GITHUB_TOKEN）"
    tag="$(printf '%s' "$json" | tr -d '\r\n' \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [[ -n "$tag" ]] || log_error "无法解析版本号，请用 --version 指定"
    printf '%s\n' "$tag"
}

is_debian_like() {
    command -v apt >/dev/null 2>&1 && command -v dpkg >/dev/null 2>&1
}

# -----------------------------------------------------------------------------
# 移除仓库配置
# -----------------------------------------------------------------------------
remove_repo() {
    log_info "移除 APT 仓库配置"
    sudo rm -f "$REPO_FILE" "$KEYRING_FILE"
    sudo apt update >/dev/null 2>&1 || true
    log_info "已移除: $REPO_FILE, $KEYRING_FILE"
    log_info "如不再需要程序: sudo apt remove -y go-magic"
}

# -----------------------------------------------------------------------------
# 方式一：从 GitHub Release 安装 .deb（默认）
# -----------------------------------------------------------------------------
install_from_release() {
    is_debian_like || log_error "本方式需要 apt 与 dpkg"

    local arch tag deb url size
    arch="$(dpkg --print-architecture)"
    if [[ "$arch" != "amd64" ]]; then
        log_error "CI 目前只为 amd64 构建 .deb（当前架构: ${arch}）。
请改用 install.sh --method binary，或从源码构建。"
    fi

    tag="$(ensure_v_prefix "$(resolve_version)")"
    deb="go-magic_${arch}.deb"
    url="https://github.com/${REPO}/releases/download/${tag}/${deb}"

    log_info "版本: ${tag}"
    log_info "下载: ${url}"

    require_cmd curl
    TMP_FILE="$(mktemp "${TMPDIR:-/tmp}/go-magic.XXXXXX.deb")"
    # -f 必不可少，否则 404 的错误页会被当成 .deb 保存下来
    curl -fsSL --retry 3 --retry-delay 2 -o "$TMP_FILE" "$url" || log_error "下载失败: $url"

    size="$(wc -c <"$TMP_FILE" | tr -d ' ')"
    if [[ "$size" -lt 1048576 ]]; then
        log_error "下载内容异常（仅 ${size} 字节）: ${url}"
    fi

    log_info "安装 .deb"
    sudo apt install -y "$TMP_FILE"

    if command -v magic >/dev/null 2>&1; then
        log_info "验证通过: $(magic --version)"
    else
        log_warn "已安装但 magic 不在 PATH 中，请检查 /usr/bin/magic"
    fi
}

# -----------------------------------------------------------------------------
# 方式二：配置自建 APT 仓库
# -----------------------------------------------------------------------------
install_from_repo() {
    local codename
    is_debian_like || log_error "本方式需要 apt 与 dpkg"
    require_cmd curl
    require_cmd gpg "sudo apt install gnupg"

    if [[ -z "$DISTRIBUTION" ]]; then
        if command -v lsb_release >/dev/null 2>&1; then
            DISTRIBUTION="$(lsb_release -cs)"
        else
            DISTRIBUTION="stable"
            log_warn "无法检测发行版代号（缺少 lsb-release），回退为: ${DISTRIBUTION}"
        fi
    fi
    codename="$DISTRIBUTION"

    log_info "仓库: ${REPO_URL}  发行版: ${codename}  架构: $(dpkg --print-architecture)"

    # 现代做法：keyring 放 /etc/apt/keyrings（而不是已废弃的 trusted.gpg.d）
    sudo mkdir -p /etc/apt/keyrings /etc/apt/sources.list.d

    # 先写临时文件再原子替换：避免 gpg 报 “文件已存在” 或写坏已有 keyring
    local key_tmp
    key_tmp="$(mktemp)"
    curl -fsSL "${REPO_URL}/keys/public.gpg" -o "$key_tmp" || { rm -f "$key_tmp"; log_error "下载 GPG key 失败"; }
    sudo gpg --dearmor --yes -o "$KEYRING_FILE" "$key_tmp"
    rm -f "$key_tmp"
    sudo chmod 0644 "$KEYRING_FILE"
    log_info "GPG key: $KEYRING_FILE"

    printf 'deb [signed-by=%s] %s %s main\n' "$KEYRING_FILE" "$REPO_URL" "$codename" \
        | sudo tee "$REPO_FILE" >/dev/null
    log_info "仓库配置: $REPO_FILE"

    log_info "更新软件源"
    sudo apt update

    log_info "安装 go-magic"
    sudo apt install -y go-magic

    if command -v magic >/dev/null 2>&1; then
        log_info "验证通过: $(magic --version)"
    fi
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
echo ""
log_info "go-magic Debian/Ubuntu 安装脚本"

if [[ "$DO_REMOVE" == "true" ]]; then
    is_debian_like || log_error "本脚本仅支持 Debian/Ubuntu 系"
    remove_repo
    echo ""
    log_info "完成"
    exit 0
fi

if [[ -n "$REPO_URL" ]]; then
    install_from_repo
else
    install_from_release
fi

echo ""
log_info "完成"
echo ""
log_info "使用: magic --help"
