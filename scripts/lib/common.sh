#!/usr/bin/env bash
# =============================================================================
# go-magic shared shell library
# =============================================================================
# Only meant to be sourced by scripts "inside the repository" (build.sh /
# build-cross.sh / run.sh and the root build.sh).
#
# Why don't install.sh / install-homebrew.sh source this file?
#   They are used as `curl -fsSL .../install.sh | bash`, so $0 is bash and there
#   is no sibling directory to source from. Those two scripts must therefore be
#   self-contained, but **must follow the same naming rules**.
#
# Single source of truth:
#   1. Version    -> git tag (same as get-version in .github/workflows/release.yml)
#   2. Platform matrix -> GM_PLATFORMS_CI (same as the release.yml build matrix)
#   3. Artifact naming -> gm_asset_name() (same as the Release assets CI uploads)
#
# Compatibility: Bash 3.2+ (the /bin/bash shipped with macOS is 3.2; no
# associative arrays, no ${var,,}, no mapfile)
# =============================================================================

# Do not set -e / set -u here; that is up to the caller

GM_REPO="magicwubiao/go-magic"
GM_BINARY="magic"
GM_TAG_PREFIX="v"

# -----------------------------------------------------------------------------
# Authoritative CI platform matrix -- exactly matching the Release assets
# (asset names verified on v0.5.19):
#   go-magic-linux-amd64  go-magic-linux-arm64
#   go-magic-darwin-amd64 go-magic-darwin-arm64
#   go-magic-windows-amd64.exe  go-magic-windows-arm64.exe
# Note: CI does **not** publish 386 / armv6 / riscv64 / BSD artifacts.
# -----------------------------------------------------------------------------
GM_PLATFORMS_CI=(
    linux/amd64
    linux/arm64
    darwin/amd64
    darwin/arm64
    windows/amd64
    windows/arm64
)

# Extra platforms: for local cross-compilation checks only, never released, and
# named with their own suffix (e.g. go-magic-linux-armv7)
GM_PLATFORMS_EXTRA=(
    linux/386
    linux/armv7
    linux/riscv64
    linux/ppc64le
    linux/s390x
    freebsd/amd64
)

# -----------------------------------------------------------------------------
# Colors and logging (falls back to plain text when not a TTY or NO_COLOR is set)
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
# Paths and dependencies
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
        gm_error "missing required command: $cmd${hint:+ ($hint)}"
        return 1
    fi
    return 0
}

# -----------------------------------------------------------------------------
# Version resolution -- the only source is the git tag (same as CI); the version
# is never hardcoded
#   priority: env var VERSION > exact tag > git describe --tags --always > dev
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

# Make sure the tag carries the v prefix (Release download URLs look like
# /releases/download/v0.5.19/...)
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

# Aligned with the CI -ldflags="-s -w -X main.Version=<tag>" (plus Commit/BuildDate)
# Note the variable name must be main.Version (capitalized); main.version is
# silently ignored by go
gm_ldflags() {
    local version="${1:-$(gm_resolve_version)}"
    printf -- '-s -w -X main.Version=%s -X main.Commit=%s -X main.BuildDate=%s' \
        "$version" "$(gm_git_commit)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}

# -----------------------------------------------------------------------------
# Platforms
# -----------------------------------------------------------------------------
# Platform key (os/arch) -> "GOOS GOARCH [GOARM]"
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

# Platform name normalization: linux-amd64 -> linux/amd64; linux/amd64 is returned as-is
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

# Release download URL (one-to-one with the assets CI uploads)
gm_asset_url() {
    local version="$1" key="$2"
    printf 'https://github.com/%s/releases/download/%s/%s\n' \
        "$GM_REPO" "$(gm_tag_name "$version")" "$(gm_platform_asset "$key")"
}

# -----------------------------------------------------------------------------
# Web UI bootstrap build
#   internal/server/dist is the //go:embed dist target and is ignored by
#   .gitignore, so on a clean clone the Web UI **must be built first** or
#   go build fails outright with "pattern dist: no matching files found"
# -----------------------------------------------------------------------------
gm_web_dist_ready() {
    [[ -f "$(gm_web_dist_dir)/index.html" ]]
}

gm_ensure_web_dist() {
    local root dist
    root="$(gm_repo_root)"
    dist="$(gm_web_dist_dir)"

    if [[ -f "$dist/index.html" ]]; then
        gm_ok "Web UI already present ($dist)"
        return 0
    fi

    gm_step "building the Web UI (required by go:embed dist)"
    gm_require_cmd npm "please install Node.js 22+" || return 1

    if [[ ! -f "$root/web/package.json" ]]; then
        gm_error "web/package.json not found; cannot build the Web UI"
        return 1
    fi

    # npm ci, same as CI (release.yml); fall back to --legacy-peer-deps on peer
    # dependency conflicts (same as the Dockerfile)
    (
        cd "$root/web" || exit 1
        if [[ -f package-lock.json ]]; then
            npm ci || npm ci --legacy-peer-deps
        else
            npm install --legacy-peer-deps
        fi
        npm run build
    ) || { gm_error "Web UI build failed"; return 1; }

    if [[ ! -f "$dist/index.html" ]]; then
        gm_error "the build finished but $dist/index.html still does not exist (was the vite outDir changed?)"
        return 1
    fi
    gm_ok "Web UI build complete"
}

# -----------------------------------------------------------------------------
# Helpers
# -----------------------------------------------------------------------------
gm_file_size() {
    local f="$1"
    if command -v du >/dev/null 2>&1; then
        du -h "$f" 2>/dev/null | cut -f1
    else
        wc -c <"$f" | awk '{printf "%.1f MB", $1/1048576}'
    fi
}
