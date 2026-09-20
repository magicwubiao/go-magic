#!/usr/bin/env bash
# =============================================================================
# go-magic Debian/Ubuntu install script
# =============================================================================
# Default (and most reliable) method: install the .deb CI produces straight from
# the GitHub Release
#   https://github.com/magicwubiao/go-magic/releases/download/v0.5.19/go-magic_amd64.deb
#   CI currently only builds the amd64 .deb (see .github/workflows/release.yml)
#
# Alternative: configure your own APT repository (requires an explicit --repo-url;
# CI does not host an APT repository)
#
# Usage:
#   ./install-apt.sh                       # download the .deb from the Release and install it
#   ./install-apt.sh --version v0.5.19
#   ./install-apt.sh --repo-url https://packages.example.com --install
#   ./install-apt.sh --remove              # remove the repository config and GPG key
# =============================================================================
set -euo pipefail

REPO="magicwubiao/go-magic"
KEYRING_FILE="/etc/apt/keyrings/go-magic.gpg"
REPO_FILE="/etc/apt/sources.list.d/go-magic.list"
VERSION="${VERSION:-}"
REPO_URL=""
DISTRIBUTION=""
DO_REMOVE="false"

# Colors
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
go-magic Debian/Ubuntu install script

Usage: $0 [options]

Options:
    --version <ver>       version, e.g. v0.5.19 (default: the latest Release)
    --repo-url <url>      use your own APT repository (otherwise the .deb from the GitHub Release is used directly)
    --distribution <cod>  distribution codename for repository mode (default: lsb_release -cs)
    --remove              remove the APT repository config and the GPG key
    -h, --help            show help

Examples:
    $0                                  # simplest: install the .deb from the Release
    $0 --version v0.5.19
    $0 --repo-url https://packages.example.com
    $0 --remove
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)
            [[ -n "${2:-}" ]] || log_error "--version requires a value"
            VERSION="$2"; shift
            ;;
        --repo-url)
            [[ -n "${2:-}" ]] || log_error "--repo-url requires a value"
            REPO_URL="$2"; shift
            ;;
        --distribution)
            [[ -n "${2:-}" ]] || log_error "--distribution requires a value"
            DISTRIBUTION="$2"; shift
            ;;
        --remove)
            DO_REMOVE="true"
            ;;
        -h|--help)
            show_help; exit 0
            ;;
        *)
            log_error "unknown argument: $1 (use --help for usage)"
            ;;
    esac
    shift
done

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || log_error "missing required command: $1${2:+ ($2)}"
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
        || log_error "could not fetch the latest version; pass --version (or set GITHUB_TOKEN)"
    tag="$(printf '%s' "$json" | tr -d '\r\n' \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [[ -n "$tag" ]] || log_error "could not parse the version; pass --version explicitly"
    printf '%s\n' "$tag"
}

is_debian_like() {
    command -v apt >/dev/null 2>&1 && command -v dpkg >/dev/null 2>&1
}

# -----------------------------------------------------------------------------
# Remove the repository config
# -----------------------------------------------------------------------------
remove_repo() {
    log_info "removing the APT repository config"
    sudo rm -f "$REPO_FILE" "$KEYRING_FILE"
    sudo apt update >/dev/null 2>&1 || true
    log_info "removed: $REPO_FILE, $KEYRING_FILE"
    log_info "if the program is no longer needed: sudo apt remove -y go-magic"
}

# -----------------------------------------------------------------------------
# Method 1: install the .deb from the GitHub Release (default)
# -----------------------------------------------------------------------------
install_from_release() {
    is_debian_like || log_error "this method requires apt and dpkg"

    local arch tag deb url size
    arch="$(dpkg --print-architecture)"
    if [[ "$arch" != "amd64" ]]; then
        log_error "CI currently only builds the .deb for amd64 (current arch: ${arch}).
Use install.sh --method binary instead, or build from source."
    fi

    tag="$(ensure_v_prefix "$(resolve_version)")"
    deb="go-magic_${arch}.deb"
    url="https://github.com/${REPO}/releases/download/${tag}/${deb}"

    log_info "version:  ${tag}"
    log_info "download: ${url}"

    require_cmd curl
    TMP_FILE="$(mktemp "${TMPDIR:-/tmp}/go-magic.XXXXXX.deb")"
    # -f is essential, otherwise a 404 error page is saved as the .deb
    curl -fsSL --retry 3 --retry-delay 2 -o "$TMP_FILE" "$url" || log_error "download failed: $url"

    size="$(wc -c <"$TMP_FILE" | tr -d ' ')"
    if [[ "$size" -lt 1048576 ]]; then
        log_error "unexpected download content (only ${size} bytes): ${url}"
    fi

    log_info "installing the .deb"
    sudo apt install -y "$TMP_FILE"

    if command -v magic >/dev/null 2>&1; then
        log_info "verification passed: $(magic --version)"
    else
        log_warn "installed but magic is not in PATH; check /usr/bin/magic"
    fi
}

# -----------------------------------------------------------------------------
# Method 2: configure your own APT repository
# -----------------------------------------------------------------------------
install_from_repo() {
    local codename
    is_debian_like || log_error "this method requires apt and dpkg"
    require_cmd curl
    require_cmd gpg "sudo apt install gnupg"

    if [[ -z "$DISTRIBUTION" ]]; then
        if command -v lsb_release >/dev/null 2>&1; then
            DISTRIBUTION="$(lsb_release -cs)"
        else
            DISTRIBUTION="stable"
            log_warn "could not detect the distribution codename (lsb-release is missing); falling back to: ${DISTRIBUTION}"
        fi
    fi
    codename="$DISTRIBUTION"

    log_info "repository: ${REPO_URL}  distribution: ${codename}  arch: $(dpkg --print-architecture)"

    # Modern approach: the keyring goes into /etc/apt/keyrings (not the deprecated
    # trusted.gpg.d)
    sudo mkdir -p /etc/apt/keyrings /etc/apt/sources.list.d

    # Write a temp file first and replace atomically: avoids gpg complaining that
    # the file exists, or corrupting an existing keyring
    local key_tmp
    key_tmp="$(mktemp)"
    curl -fsSL "${REPO_URL}/keys/public.gpg" -o "$key_tmp" || { rm -f "$key_tmp"; log_error "downloading the GPG key failed"; }
    sudo gpg --dearmor --yes -o "$KEYRING_FILE" "$key_tmp"
    rm -f "$key_tmp"
    sudo chmod 0644 "$KEYRING_FILE"
    log_info "GPG key: $KEYRING_FILE"

    printf 'deb [signed-by=%s] %s %s main\n' "$KEYRING_FILE" "$REPO_URL" "$codename" \
        | sudo tee "$REPO_FILE" >/dev/null
    log_info "repository config: $REPO_FILE"

    log_info "updating the package index"
    sudo apt update

    log_info "installing go-magic"
    sudo apt install -y go-magic

    if command -v magic >/dev/null 2>&1; then
        log_info "verification passed: $(magic --version)"
    fi
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
echo ""
log_info "go-magic Debian/Ubuntu install script"

if [[ "$DO_REMOVE" == "true" ]]; then
    is_debian_like || log_error "this script only supports Debian/Ubuntu"
    remove_repo
    echo ""
    log_info "done"
    exit 0
fi

if [[ -n "$REPO_URL" ]]; then
    install_from_repo
else
    install_from_release
fi

echo ""
log_info "done"
echo ""
log_info "usage: magic --help"
