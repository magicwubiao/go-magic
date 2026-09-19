#!/usr/bin/env bash
# =============================================================================
# go-magic 共享 Shell 库
# =============================================================================
# 仅供「仓库内」脚本 source（build.sh / build-cross.sh / run.sh 以及根 build.sh）。
#
# 为什么 install.sh / install-homebrew.sh 不 source 本文件？
#   它们的使用方式是 `curl -fsSL .../install.sh | bash`，$0 是 bash、没有同级目录，
#   无法 source 兄弟文件。因此这两个脚本必须自包含，但**必须遵守同样的命名规则**。
#
# 单一事实源（Single Source of Truth）：
#   1. 版本号    → git tag（与 .github/workflows/release.yml 的 get-version 一致）
#   2. 平台矩阵  → GM_PLATFORMS_CI（与 release.yml 构建矩阵一致）
#   3. 产物命名  → gm_asset_name()（与 CI 实际上传的 Release 资产名一致）
#
# 兼容性：Bash 3.2+（macOS 自带 /bin/bash 是 3.2，不使用关联数组 / ${var,,} / mapfile）
# =============================================================================

# 不要在这里 set -e / set -u，由调用方决定

GM_REPO="magicwubiao/go-magic"
GM_BINARY="magic"
GM_TAG_PREFIX="v"

# -----------------------------------------------------------------------------
# CI 权威平台矩阵 —— 与 Release 资产严格一致（v0.5.19 实测资产名）：
#   go-magic-linux-amd64  go-magic-linux-arm64
#   go-magic-darwin-amd64 go-magic-darwin-arm64
#   go-magic-windows-amd64.exe  go-magic-windows-arm64.exe
# 注意：CI **不** 发布 386 / armv6 / riscv64 / BSD 产物。
# -----------------------------------------------------------------------------
GM_PLATFORMS_CI=(
    linux/amd64
    linux/arm64
    darwin/amd64
    darwin/arm64
    windows/amd64
    windows/arm64
)

# 额外平台：仅用于本地交叉验证，不参与发布，命名带自身后缀（如 go-magic-linux-armv7）
GM_PLATFORMS_EXTRA=(
    linux/386
    linux/armv7
    linux/riscv64
    linux/ppc64le
    linux/s390x
    freebsd/amd64
)

# -----------------------------------------------------------------------------
# 颜色与日志（非 TTY 或 NO_COLOR 时自动降级为纯文本）
# -----------------------------------------------------------------------------
if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-dumb}" != "dumb" ]]; then
    GM_C_RED=$'\033[0;31m'
    GM_C_GREEN=$'\033[0;32m'
    GM_C_YELLOW=$'\033[1;33m'
    GM_C_BLUE=$'\033[0;34m'
    GM_C_NC=$'\033[0m'
else
    GM_C_RED=''
    GM_C_GREEN=''
    GM_C_YELLOW=''
    GM_C_BLUE=''
    GM_C_NC=''
fi

gm_info()  { printf '%s[INFO]%s %s\n'  "$GM_C_BLUE"   "$GM_C_NC" "$*"; }
gm_ok()    { printf '%s[ OK ]%s %s\n'  "$GM_C_GREEN"  "$GM_C_NC" "$*"; }
gm_warn()  { printf '%s[WARN]%s %s\n'  "$GM_C_YELLOW" "$GM_C_NC" "$*" >&2; }
gm_error() { printf '%s[FAIL]%s %s\n'  "$GM_C_RED"    "$GM_C_NC" "$*" >&2; }
gm_step()  { printf '\n%s==>%s %s\n'   "$GM_C_BLUE"   "$GM_C_NC" "$*"; }

# -----------------------------------------------------------------------------
# 路径与依赖
# -----------------------------------------------------------------------------
gm_repo_root() {
    local here
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    (cd "$here/../.." && pwd)
}

gm_web_dist_dir() {
    printf '%s/internal/server/dist\n' "$(gm_repo_root)"
}

gm_require_cmd() {
    local cmd="$1" hint="${2:-}"
    if ! command -v "$cmd" >/dev/null 2>&1; then
        gm_error "缺少必需命令: $cmd${hint:+（$hint）}"
        return 1
    fi
    return 0
}

# -----------------------------------------------------------------------------
# 版本解析 —— 唯一来源是 git tag（与 CI 相同），绝不硬编码版本号
#   优先级: 环境变量 VERSION > 精确 tag > git describe --tags --always > dev
# -----------------------------------------------------------------------------
gm_resolve_version() {
    local v root
    if [[ -n "${VERSION:-}" ]]; then
        printf '%s\n' "$VERSION"
        return 0
    fi
    root="$(gm_repo_root)"
    if v="$(git -C "$root" describe --tags --exact-match 2>/dev/null)" && [[ -n "$v" ]]; then
        printf '%s\n' "$v"
        return 0
    fi
    if v="$(git -C "$root" describe --tags --always 2>/dev/null)" && [[ -n "$v" ]]; then
        printf '%s\n' "$v"
        return 0
    fi
    printf 'dev\n'
}

# 保证 tag 带 v 前缀（Release 下载路径是 /releases/download/v0.5.19/...）
gm_tag_name() {
    local v="${1:-$(gm_resolve_version)}"
    case "$v" in
        v*) printf '%s\n' "$v" ;;
        *)  printf '%s%s\n' "$GM_TAG_PREFIX" "$v" ;;
    esac
}

gm_git_commit() {
    git -C "$(gm_repo_root)" rev-parse --short HEAD 2>/dev/null || printf 'unknown\n'
}

# 与 CI 的 -ldflags="-s -w -X main.Version=<tag>" 对齐（额外注入 Commit/BuildDate）
# 注意变量名必须是 main.Version（大写），写成 main.version 会被 go 静默忽略
gm_ldflags() {
    local version="${1:-$(gm_resolve_version)}"
    printf -- '-s -w -X main.Version=%s -X main.Commit=%s -X main.BuildDate=%s' \
        "$version" "$(gm_git_commit)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}

# -----------------------------------------------------------------------------
# 平台
# -----------------------------------------------------------------------------
# 平台键 (os/arch) -> "GOOS GOARCH [GOARM]"
gm_platform_env() {
    case "${1:-}" in
        linux/amd64)   printf 'linux amd64\n'   ;;
        linux/arm64)   printf 'linux arm64\n'   ;;
        linux/386)     printf 'linux 386\n'     ;;
        linux/armv7)   printf 'linux arm 7\n'   ;;
        linux/riscv64) printf 'linux riscv64\n' ;;
        linux/ppc64le) printf 'linux ppc64le\n' ;;
        linux/s390x)   printf 'linux s390x\n'   ;;
        darwin/amd64)  printf 'darwin amd64\n'  ;;
        darwin/arm64)  printf 'darwin arm64\n'  ;;
        windows/amd64) printf 'windows amd64\n' ;;
        windows/arm64) printf 'windows arm64\n' ;;
        freebsd/amd64) printf 'freebsd amd64\n' ;;
        *) return 1 ;;
    esac
}

gm_platform_asset() {
    local key="${1:-}"
    local name="${key//\//-}"           # linux/amd64 -> linux-amd64
    case "$key" in
        windows/*) printf 'go-magic-%s.exe\n' "$name" ;;
        *)         printf 'go-magic-%s\n'     "$name" ;;
    esac
}

# 平台名归一化: linux-amd64 -> linux/amd64；已是 linux/amd64 则原样返回
gm_normalize_platform() {
    local p="${1:-}"
    case "$p" in
        */*) printf '%s\n' "$p" ;;
        *-*) printf '%s/%s\n' "${p%%-*}" "${p#*-}" ;;
        *)   printf '%s\n' "$p" ;;
    esac
}

gm_platform_goos() {
    local env_str
    env_str="$(gm_platform_env "$1")" || return 1
    printf '%s\n' "${env_str%% *}"
}

# Release 下载地址（与 CI 上传的资产一一对应）
gm_asset_url() {
    local version="$1" key="$2"
    printf 'https://github.com/%s/releases/download/%s/%s\n' \
        "$GM_REPO" "$(gm_tag_name "$version")" "$(gm_platform_asset "$key")"
}

# -----------------------------------------------------------------------------
# Web UI 引导构建
#   internal/server/dist 是 //go:embed dist 的目标且被 .gitignore 忽略，
#   因此在干净克隆上 **必须先构建 Web UI**，否则 go build 直接失败：
#   "pattern dist: no matching files found"
# -----------------------------------------------------------------------------
gm_web_dist_ready() {
    [[ -f "$(gm_web_dist_dir)/index.html" ]]
}

gm_ensure_web_dist() {
    local root dist
    root="$(gm_repo_root)"
    dist="$(gm_web_dist_dir)"

    if [[ -f "$dist/index.html" ]]; then
        gm_ok "Web UI 已就绪（$dist）"
        return 0
    fi

    gm_step "构建 Web UI（go:embed dist 依赖）"
    gm_require_cmd npm "请先安装 Node.js 22+" || return 1

    if [[ ! -f "$root/web/package.json" ]]; then
        gm_error "找不到 web/package.json，无法构建 Web UI"
        return 1
    fi

    # 与 CI (release.yml) 一致使用 npm ci；peer 依赖冲突时回退 --legacy-peer-deps（与 Dockerfile 一致）
    (
        cd "$root/web" || exit 1
        if [[ -f package-lock.json ]]; then
            npm ci || npm ci --legacy-peer-deps
        else
            npm install --legacy-peer-deps
        fi
        npm run build
    ) || { gm_error "Web UI 构建失败"; return 1; }

    if [[ ! -f "$dist/index.html" ]]; then
        gm_error "构建结束但 $dist/index.html 仍不存在（vite outDir 配置被改动？）"
        return 1
    fi
    gm_ok "Web UI 构建完成"
}

# -----------------------------------------------------------------------------
# 小工具
# -----------------------------------------------------------------------------
gm_file_size() {
    local f="$1"
    if command -v du >/dev/null 2>&1; then
        du -h "$f" 2>/dev/null | cut -f1
    else
        wc -c <"$f" | awk '{printf "%.1f MB", $1/1048576}'
    fi
}
