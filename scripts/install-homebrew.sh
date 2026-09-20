#!/usr/bin/env bash
# =============================================================================
# go-magic Homebrew / direct download installer (macOS / Linuxbrew)
# =============================================================================
# Naming matches the CI Release assets exactly: go-magic-<os>-<arch>[.exe]
#   go-magic-darwin-amd64  go-magic-darwin-arm64
#   go-magic-linux-amd64   go-magic-linux-arm64
#
# Usage:
#   ./install-homebrew.sh                 # download the binary into <brew-prefix>/bin/magic
#   ./install-homebrew.sh --tap           # install/update the Homebrew tap (magicwubiao/tap)
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
go-magic Homebrew installer

Usage: $0 [options]

Options:
    --prefix <path>   Homebrew prefix (default: auto-detected via brew --prefix)
    --tap             install/update the Homebrew tap (${TAP_NAME}), then let brew take over
    --version <ver>   version, e.g. v0.5.19 (default: the latest Release)
    --force           force reinstall
    -h, --help        show help

Examples:
    $0                       # install the binary to <prefix>/bin/magic
    $0 --tap                 # install the tap, then run brew install ${TAP_NAME}/go-magic
    $0 --version v0.5.19
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)
            [[ -n "${2:-}" ]] || log_error "--prefix requires a value"
            BREW_PREFIX="$2"; shift
            ;;
        --tap)
            INSTALL_TAP="true"
            ;;
        --version)
            [[ -n "${2:-}" ]] || log_error "--version requires a value"
            VERSION="$2"; shift
            ;;
        --force)
            FORCE="true"
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

if [[ -z "$BREW_PREFIX" ]]; then
    if command -v brew >/dev/null 2>&1; then
        BREW_PREFIX="$(brew --prefix)"
    else
        log_error "brew not found; pass --prefix to set the Homebrew prefix (e.g. /opt/homebrew or /usr/local)"
    fi
fi

# Only combinations CI has actually released are accepted
detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$arch" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) log_error "unsupported architecture: $(uname -m)
Release only provides amd64 / arm64; for other architectures build from source: go install github.com/${REPO}/cmd/magic@latest" ;;
    esac

    case "$os" in
        darwin|linux) printf '%s-%s\n' "$os" "$arch" ;;
        *) log_error "unsupported operating system: $os (this script only supports macOS / Linux)" ;;
    esac
}

resolve_version() {
    if [[ -n "$VERSION" ]]; then
        printf '%s\n' "$VERSION"
        return 0
    fi
    command -v curl >/dev/null 2>&1 || log_error "curl is required to query the latest version, or pass --version"

    local json tag auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")

    json="$(curl -fsSL "${auth[@]}" "$GITHUB_API/releases/latest" 2>/dev/null)" \
        || log_error "could not fetch the latest version (network or rate limit). Pass --version, or set GITHUB_TOKEN"
    tag="$(printf '%s' "$json" | tr -d '\r\n' \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [[ -n "$tag" ]] || log_error "could not parse the version; pass --version explicitly"
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
        # -f is required: otherwise a 404 HTML page is saved as the "binary"
        curl -fsSL --retry 3 --retry-delay 2 -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$dest" "$url"
    else
        log_error "neither curl nor wget was found"
    fi
}

install_tap() {
    log_info "installing the Homebrew tap ${TAP_NAME}"

    command -v git >/dev/null 2>&1 || log_error "git is required to install the tap"

    local tap_dir="${BREW_PREFIX}/Library/Taps/magicwubiao/homebrew-tap"

    if [[ -d "$tap_dir/.git" ]]; then
        log_info "the tap already exists; updating"
        if [[ "$FORCE" == "true" ]]; then
            ( cd "$tap_dir" && git pull --ff-only ) || log_warn "tap update failed; keeping the local copy"
        else
            log_info "skipping the update (pass --force to update)"
        fi
    else
        mkdir -p "$(dirname "$tap_dir")"
        git clone --depth 1 "$TAP_REPO" "$tap_dir"
    fi

    log_info "tap installed: $tap_dir"
    log_info "you can now run: brew install ${TAP_NAME}/go-magic"
}

install_binary() {
    local platform tag asset url size bin_dir install_path
    platform="$(detect_platform)"
    tag="$(ensure_v_prefix "$(resolve_version)")"
    asset="go-magic-${platform}"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

    log_info "platform: ${platform}"
    log_info "version:  ${tag}"
    log_info "download: ${url}"

    bin_dir="${BREW_PREFIX}/bin"
    install_path="${bin_dir}/${BINARY_NAME}"
    mkdir -p "$bin_dir"

    if [[ -e "$install_path" && "$FORCE" != "true" ]]; then
        log_warn "$install_path already exists; overwriting (pass --force to acknowledge)"
    fi

    TMP_FILE="$(mktemp "${TMPDIR:-/tmp}/go-magic.XXXXXX")"
    download "$url" "$TMP_FILE" || log_error "download failed: $url"

    size="$(wc -c <"$TMP_FILE" | tr -d ' ')"
    if [[ "$size" -lt 1048576 ]]; then
        log_error "unexpected download content (only ${size} bytes); this version may have no artifact for that platform: ${asset}"
    fi

    chmod 0755 "$TMP_FILE"
    mv -f "$TMP_FILE" "$install_path"
    TMP_FILE=""
    log_info "installed: $install_path"

    if "$install_path" --version >/dev/null 2>&1; then
        log_info "verification passed: $("$install_path" --version)"
    else
        log_warn "verification failed; check manually: $install_path --version"
    fi

    case ":${PATH}:" in
        *":${bin_dir}:"*) ;;
        *) log_warn "${bin_dir} is not in PATH" ;;
    esac
}

echo ""
log_info "go-magic Homebrew installer"
echo ""

if [[ "$INSTALL_TAP" == "true" ]]; then
    install_tap
else
    install_binary
fi

echo ""
log_info "done"
