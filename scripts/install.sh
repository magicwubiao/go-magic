#!/usr/bin/env bash
# =============================================================================
# go-magic 一键安装脚本
# =============================================================================
# 安装: curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
#
# 重要：本脚本支持 `curl | bash` 方式执行，此时没有同级目录、无法 source 其它文件，
#      因此必须自包含。其资产命名规则与 CI (.github/workflows/release.yml) 严格一致：
#        go-magic-<os>-<arch>        Linux/macOS
#        go-magic-<os>-<arch>.exe    Windows
#        go-magic_amd64.deb          Debian/Ubuntu
#      CI 只发布 amd64/arm64（Linux/macOS/Windows），不发布 386/armv6/BSD。
#
# 安装目录与配置目录是分开的：
#   --dir     二进制目录   默认 ~/.local/share/go-magic
#   --bin-dir 命令软链接   默认 ~/.local/bin
#   配置目录始终是 magic home（默认 ~/.magic），由程序自己创建
# =============================================================================

set -euo pipefail

REPO="magicwubiao/go-magic"
GITHUB_API="https://api.github.com/repos/${REPO}"
VERSION="${VERSION:-}"
# 不在顶层展开 $HOME：HOME 未设置时（docker run 精简环境、env -i、部分 sudo/CI）
# 顶层展开会让脚本连 --help 都跑不起来，因此改为按需惰性解析
INSTALL_DIR="${INSTALL_DIR:-}"
BIN_DIR="${BIN_DIR:-}"
INSTALL_METHOD="${INSTALL_METHOD:-binary}"

resolve_install_dirs() {
    # 只有在需要推断默认目录时才要求 HOME；显式传了 --dir/--bin-dir 就无需 HOME
    if [[ -z "$INSTALL_DIR" || -z "$BIN_DIR" ]]; then
        if [[ -z "${HOME:-}" ]]; then
            error "环境变量 HOME 未设置，无法推断默认安装目录。
请显式指定: --dir <binary-dir> --bin-dir <bin-dir>"
        fi
    fi
    INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/share/go-magic}"
    BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
}

# 颜色
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
    RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; BLUE=''; NC=''
fi

info()    { printf '%b[INFO]%b %s\n' "$BLUE"   "$NC" "$*"; }
success() { printf '%b[ OK ]%b %s\n' "$GREEN"  "$NC" "$*"; }
warn()    { printf '%b[WARN]%b %s\n' "$YELLOW" "$NC" "$*" >&2; }
error()   { printf '%b[FAIL]%b %s\n' "$RED"    "$NC" "$*" >&2; exit 1; }

# 临时文件用全局变量 + 单一 EXIT trap：
# 若把 tmp 声明为 local，函数返回后 trap 在 set -u 下会因变量已消失而报错
TMP_FILE=""
cleanup() { [[ -n "$TMP_FILE" && -e "$TMP_FILE" ]] && rm -f "$TMP_FILE"; return 0; }
trap cleanup EXIT

new_tmp() {
    TMP_FILE="$(mktemp "${TMPDIR:-/tmp}/${1:-magic-install}.XXXXXX")"
    printf '%s\n' "$TMP_FILE"
}

show_help() {
    cat <<EOF
go-magic 一键安装脚本

用法: curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash -s -- [选项]

选项:
    --method <m>     安装方式: binary | homebrew | docker | apt | scoop (默认: binary)
    --version <ver>  指定版本，例如 v0.5.19 (默认: 取 GitHub 最新 Release)
    --dir <path>     二进制安装目录 (默认: ~/.local/share/go-magic)
    --bin-dir <path> 命令软链接目录 (默认: ~/.local/bin)
    -h, --help       显示帮助

安装方式说明:
    binary    下载预编译二进制（推荐，Linux/macOS/Windows）
    homebrew  使用 Homebrew (macOS/Linux) —— 需要 tap magicwubiao/tap 已发布
    docker    拉取 Docker 镜像        —— CI 只构建镜像不推送，故通常不可用
    apt       Debian/Ubuntu：从 GitHub Release 安装 .deb（仅 amd64）
    scoop     Windows：通过 Scoop bucket 安装 —— 需要 bucket 已发布

注意: homebrew / docker / scoop 依赖外部仓库或镜像先行发布；若失败脚本会给出
      明确提示与替代方案，此时请使用默认的 binary 方式。

支持的平台（与 Release 资产一致）:
    Linux:   amd64, arm64
    macOS:   amd64, arm64
    Windows: amd64, arm64

配置目录是 magic home（默认 ~/.magic），与二进制安装目录相互独立。
EOF
}

# -----------------------------------------------------------------------------
# 参数解析
# -----------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --method)
            [[ -n "${2:-}" ]] || error "--method 需要参数"
            INSTALL_METHOD="$2"; shift
            ;;
        --version)
            [[ -n "${2:-}" ]] || error "--version 需要参数"
            VERSION="$2"; shift
            ;;
        --dir)
            [[ -n "${2:-}" ]] || error "--dir 需要参数"
            INSTALL_DIR="$2"; shift
            ;;
        --bin-dir)
            [[ -n "${2:-}" ]] || error "--bin-dir 需要参数"
            BIN_DIR="$2"; shift
            ;;
        -h|--help)
            show_help; exit 0
            ;;
        *)
            error "未知参数: $1（用 --help 查看用法）"
            ;;
    esac
    shift
done

# -----------------------------------------------------------------------------
# 平台检测 —— 只接受 CI 真实发布过的组合
# -----------------------------------------------------------------------------
detect_os() {
    case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
        linux*)                 printf 'linux\n' ;;
        darwin*)                printf 'darwin\n' ;;
        mingw*|msys*|cygwin*)   printf 'windows\n' ;;
        *)                      printf 'unsupported\n' ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  printf 'amd64\n' ;;
        arm64|aarch64) printf 'arm64\n' ;;
        *)             printf 'unsupported\n' ;;
    esac
}

assert_supported_platform() {
    local os="$1" arch="$2"
    case "$os/$arch" in
        linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64|windows/arm64)
            return 0
            ;;
    esac
    error "不支持的平台: $os/$arch
Release 只提供: Linux(amd64,arm64) / macOS(amd64,arm64) / Windows(amd64,arm64)
其它平台请从源码构建: go install github.com/${REPO}/cmd/magic@latest"
}

# -----------------------------------------------------------------------------
# 版本解析 —— 只信任 git tag（GitHub Release），不硬编码任何兜底版本号
# -----------------------------------------------------------------------------
resolve_version() {
    if [[ -n "$VERSION" ]]; then
        printf '%s\n' "$VERSION"
        return 0
    fi

    command -v curl >/dev/null 2>&1 || error "需要 curl 来查询最新版本，或使用 --version 指定版本"

    local json tag auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")

    if ! json="$(curl -fsSL "${auth[@]}" "$GITHUB_API/releases/latest" 2>/dev/null)"; then
        error "无法获取最新版本（GitHub API 失败，可能是网络或速率限制）。
请显式指定版本，例如: --version v0.5.19
或设置 GITHUB_TOKEN 提高速率限制上限。"
    fi

    tag="$(printf '%s' "$json" | tr -d '\r\n' \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [[ -n "$tag" ]] || error "无法解析最新版本号，请用 --version 显式指定"

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
        # -f 必须保留：否则 404 的错误页会被当成二进制写进目标文件
        curl -fsSL --retry 3 --retry-delay 2 -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$dest" "$url"
    else
        error "未找到 curl 或 wget"
    fi
}

# -----------------------------------------------------------------------------
# binary 安装
# -----------------------------------------------------------------------------
install_binary() {
    local os arch tag asset url ext tmp

    os="$(detect_os)"
    arch="$(detect_arch)"
    [[ "$os" == "unsupported" ]] && error "不支持的操作系统: $(uname -s)"
    assert_supported_platform "$os" "$arch"
    resolve_install_dirs

    tag="$(ensure_v_prefix "$(resolve_version)")"
    ext=""
    [[ "$os" == "windows" ]] && ext=".exe"
    asset="go-magic-${os}-${arch}${ext}"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

    info "安装 go-magic ${tag} (${os}/${arch})"
    info "下载: ${url}"

    mkdir -p "$INSTALL_DIR" "$BIN_DIR"
    tmp="$(new_tmp magic-install)"

    download "$url" "$tmp" || error "下载失败: $url"

    # 体积自检：真实二进制约 35MB；过小说明拿到的是错误页而非二进制
    local size
    size="$(wc -c <"$tmp" | tr -d ' ')"
    if [[ "$size" -lt 1048576 ]]; then
        error "下载内容异常（仅 ${size} 字节），可能该版本没有对应平台的产物: $asset"
    fi

    install_binary_file "$tmp" "${INSTALL_DIR}/${asset}"
    success "已安装: ${INSTALL_DIR}/${asset}"

    # Windows(msys/git-bash) 下软链接常不可用，直接复制
    if [[ "$os" == "windows" ]]; then
        cp -f "${INSTALL_DIR}/${asset}" "${BIN_DIR}/magic${ext}"
    else
        ln -sf "${INSTALL_DIR}/${asset}" "${BIN_DIR}/magic"
    fi
    success "命令入口: ${BIN_DIR}/magic${ext}"

    if [[ ":${PATH}:" != *":${BIN_DIR}:"* ]]; then
        warn "${BIN_DIR} 不在 PATH 中，请加入 shell 配置（~/.bashrc 或 ~/.zshrc）:"
        printf '\n  export PATH="%s:$PATH"\n\n' "$BIN_DIR"
    fi

    verify_install "${BIN_DIR}/magic${ext}"
}

# 安装二进制：优先 install(1)，缺失时退化为 cp + chmod（Windows msys 等环境没有 install）
install_binary_file() {
    local src="$1" dst="$2"
    if command -v install >/dev/null 2>&1; then
        install -m 0755 "$src" "$dst"
    else
        cp -f "$src" "$dst"
        chmod 0755 "$dst"
    fi
}

verify_install() {
    local bin="$1"
    if [[ ! -x "$bin" ]]; then
        warn "找不到可执行文件 $bin，跳过验证"
        return 0
    fi
    local out
    if out="$("$bin" --version 2>/dev/null)"; then
        success "验证通过: ${out}"
    else
        warn "安装验证失败，可尝试手动运行: $bin --version"
    fi
}


# -----------------------------------------------------------------------------
# homebrew / docker / apt / scoop
# -----------------------------------------------------------------------------
install_homebrew() {
    local os
    os="$(detect_os)"
    [[ "$os" == "windows" ]] && error "Windows 不支持 Homebrew，请使用 --method scoop"

    command -v brew >/dev/null 2>&1 || error "未安装 Homebrew，请先安装: https://brew.sh"

    info "添加 Tap magicwubiao/tap"
    brew tap magicwubiao/tap

    info "安装 go-magic"
    brew install magicwubiao/tap/go-magic
    success "Homebrew 安装完成"
    command -v magic >/dev/null 2>&1 && verify_install "$(command -v magic)"
}

install_docker() {
    command -v docker >/dev/null 2>&1 || error "未找到 Docker: https://docs.docker.com/get-docker/"

    local tag
    tag="$(ensure_v_prefix "$(resolve_version)")"
    local image="${REPO}:${tag}"

    info "拉取镜像 ${image}"
    docker pull "$image" || error "拉取失败：镜像可能尚未发布（docker 镜像由 make docker-push / docker-buildx 发布）"

    success "镜像已就绪: ${image}"
    info "运行: docker run -it --rm -p 8642:8642 -v ~/.magic:/home/magic/.magic ${image}"
}

install_apt() {
    local os arch arch_deb tag deb url tmp
    os="$(detect_os)"
    arch="$(detect_arch)"
    [[ "$os" == "linux" ]] || error "apt 安装仅支持 Linux"

    # CI 只构建 amd64 的 .deb
    [[ "$arch" == "amd64" ]] || error "apt 方式目前仅提供 amd64 的 .deb（当前架构: ${arch}）。
请改用 --method binary。"

    command -v apt >/dev/null 2>&1 || error "未找到 apt"
    command -v sudo >/dev/null 2>&1 || warn "未找到 sudo，后续命令可能需要 root 权限"

    tag="$(ensure_v_prefix "$(resolve_version)")"
    arch_deb="$(dpkg --print-architecture 2>/dev/null || printf 'amd64')"
    deb="go-magic_${arch_deb}.deb"
    url="https://github.com/${REPO}/releases/download/${tag}/${deb}"

    info "下载 ${deb}"
    tmp="$(new_tmp go-magic-deb)"
    download "$url" "$tmp" || error "下载失败: $url"

    info "安装 .deb"
    sudo apt install -y "$tmp"
    success "APT 安装完成"
    verify_install "/usr/bin/magic"
}

install_scoop() {
    local os
    os="$(detect_os)"
    [[ "$os" == "windows" ]] || error "Scoop 仅支持 Windows（当前: ${os}）"

    command -v scoop >/dev/null 2>&1 || error "未安装 Scoop，请先安装: https://scoop.sh"

    info "添加 Scoop bucket"
    scoop bucket add magic "https://github.com/magicwubiao/scoop-bucket" \
        || error "无法添加 bucket（magicwubiao/scoop-bucket 可能尚未发布）。
请改用: --method binary（或在 Windows 上从源码构建 scripts\\windows\\install.bat）"

    info "安装 go-magic"
    scoop install magic
    success "Scoop 安装完成"
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
printf '\n%b╔════════════════════════════════════════╗%b\n' "$GREEN" "$NC"
printf '%b║      go-magic 一键安装脚本             ║%b\n' "$GREEN" "$NC"
printf '%b╚════════════════════════════════════════╝%b\n\n' "$GREEN" "$NC"
info "安装方式: ${INSTALL_METHOD}"

case "$INSTALL_METHOD" in
    binary)   install_binary ;;
    homebrew) install_homebrew ;;
    brew)     install_homebrew ;;
    docker)   install_docker ;;
    apt)      install_apt ;;
    scoop)    install_scoop ;;
    *)        error "未知安装方式: ${INSTALL_METHOD}（可选: binary|homebrew|docker|apt|scoop）" ;;
esac

printf '\n'
success "安装完成"
printf '\n'
info "下一步: magic setup   # 初始化配置（配置目录 ~/.magic）"
info "        magic chat    # 开始对话"
info "        magic server  # 启动 Web 控制台"
printf '\n'
if [[ "$INSTALL_METHOD" == "binary" ]]; then
    info "卸载: 删除 ${INSTALL_DIR} 与 ${BIN_DIR}/magic 即可（配置在 ~/.magic，可按需保留）"
    printf '\n'
fi

