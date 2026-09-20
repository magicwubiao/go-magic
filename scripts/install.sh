#!/usr/bin/env bash
# =============================================================================
# go-magic one-line install script
# =============================================================================
# Install: curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
#
# Important: this script supports the `curl | bash` invocation, where there is no
#      sibling directory and other files cannot be sourced, so it must be
#      self-contained. Its asset naming rules match CI
#      (.github/workflows/release.yml) exactly:
#        go-magic-<os>-<arch>        Linux/macOS
#        go-magic-<os>-<arch>.exe    Windows
#        go-magic_amd64.deb          Debian/Ubuntu
#      CI only releases amd64/arm64 (Linux/macOS/Windows); no 386/armv6/BSD.
#
# The install directory and the config directory are separate:
#   --dir     binary directory  default ~/.local/share/go-magic
#   --bin-dir command symlinks  default ~/.local/bin
#   The config directory is always magic home (default ~/.magic), created by the
#   program itself
# =============================================================================

set -euo pipefail

REPO="magicwubiao/go-magic"
GITHUB_API="https://api.github.com/repos/${REPO}"
VERSION="${VERSION:-}"
# $HOME is not expanded at the top level: when HOME is unset (stripped-down
# docker run environments, env -i, some sudo/CI setups) a top-level expansion
# makes even --help unrunnable, so it is resolved lazily instead
INSTALL_DIR="${INSTALL_DIR:-}"
BIN_DIR="${BIN_DIR:-}"
INSTALL_METHOD="${INSTALL_METHOD:-binary}"

resolve_install_dirs() {
    # HOME is only required when the default directories must be inferred;
    # passing --dir/--bin-dir explicitly needs no HOME
    if [[ -z "$INSTALL_DIR" || -z "$BIN_DIR" ]]; then
        if [[ -z "${HOME:-}" ]]; then
            error "the HOME environment variable is not set, so the default install directories cannot be inferred.
Specify them explicitly: --dir <binary-dir> --bin-dir <bin-dir>"
        fi
    fi
    INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/share/go-magic}"
    BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
}

# Colors
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
    RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; BLUE=''; NC=''
fi

info()    { printf '%b[INFO]%b %s\n' "$BLUE"   "$NC" "$*"; }
success() { printf '%b[ OK ]%b %s\n' "$GREEN"  "$NC" "$*"; }
warn()    { printf '%b[WARN]%b %s\n' "$YELLOW" "$NC" "$*" >&2; }
error()   { printf '%b[FAIL]%b %s\n' "$RED"    "$NC" "$*" >&2; exit 1; }

# Temp files use a global variable plus a single EXIT trap: if tmp were declared
# local, the trap would fail under set -u once the variable goes out of scope
TMP_FILE=""
cleanup() { [[ -n "$TMP_FILE" && -e "$TMP_FILE" ]] && rm -f "$TMP_FILE"; return 0; }
trap cleanup EXIT

new_tmp() {
    TMP_FILE="$(mktemp "${TMPDIR:-/tmp}/${1:-magic-install}.XXXXXX")"
    printf '%s\n' "$TMP_FILE"
}

show_help() {
    cat <<EOF
go-magic one-line install script

Usage: curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash -s -- [options]

Options:
    --method <m>     install method: binary | homebrew | docker | apt | scoop (default: binary)
    --version <ver>  version, e.g. v0.5.19 (default: the latest GitHub Release)
    --dir <path>     binary install directory (default: ~/.local/share/go-magic)
    --bin-dir <path> command symlink directory (default: ~/.local/bin)
    -h, --help       show help

Install methods:
    binary    download a prebuilt binary (recommended; Linux/macOS/Windows)
    homebrew  use Homebrew (macOS/Linux) -- requires the tap magicwubiao/tap to be published
    docker    pull the Docker image      -- CI only builds the image, never pushes, so usually unavailable
    apt       Debian/Ubuntu: install the .deb from GitHub Release (amd64 only)
    scoop     Windows: install via the Scoop bucket -- requires the bucket to be published

Note: homebrew / docker / scoop depend on an external repository or image being
      published first; if they fail the script prints a clear message and an
      alternative -- use the default binary method in that case.

Supported platforms (matching the Release assets):
    Linux:   amd64, arm64
    macOS:   amd64, arm64
    Windows: amd64, arm64

The config directory is magic home (default ~/.magic), independent of the
binary install directory.
EOF
}

# -----------------------------------------------------------------------------
# Argument parsing
# -----------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --method)
            [[ -n "${2:-}" ]] || error "--method requires a value"
            INSTALL_METHOD="$2"; shift
            ;;
        --version)
            [[ -n "${2:-}" ]] || error "--version requires a value"
            VERSION="$2"; shift
            ;;
        --dir)
            [[ -n "${2:-}" ]] || error "--dir requires a value"
            INSTALL_DIR="$2"; shift
            ;;
        --bin-dir)
            [[ -n "${2:-}" ]] || error "--bin-dir requires a value"
            BIN_DIR="$2"; shift
            ;;
        -h|--help)
            show_help; exit 0
            ;;
        *)
            error "unknown argument: $1 (use --help for usage)"
            ;;
    esac
    shift
done

# -----------------------------------------------------------------------------
# Platform detection -- only combinations CI has actually released are accepted
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
    error "unsupported platform: $os/$arch
Release only provides: Linux(amd64,arm64) / macOS(amd64,arm64) / Windows(amd64,arm64)
For other platforms build from source: go install github.com/${REPO}/cmd/magic@latest"
}

# -----------------------------------------------------------------------------
# Version resolution -- only the git tag (GitHub Release) is trusted; no
# hardcoded fallback version
# -----------------------------------------------------------------------------
resolve_version() {
    if [[ -n "$VERSION" ]]; then
        printf '%s\n' "$VERSION"
        return 0
    fi

    command -v curl >/dev/null 2>&1 || error "curl is required to query the latest version, or pass --version"

    local json tag auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")

    if ! json="$(curl -fsSL "${auth[@]}" "$GITHUB_API/releases/latest" 2>/dev/null)"; then
        error "could not fetch the latest version (GitHub API failed; network or rate limit?).
Specify the version explicitly, e.g. --version v0.5.19
or set GITHUB_TOKEN to raise the rate limit."
    fi

    tag="$(printf '%s' "$json" | tr -d '\r\n' \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [[ -n "$tag" ]] || error "could not parse the latest version; pass --version explicitly"

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
        # -f must stay: otherwise a 404 error page is written into the target as if it were the binary
        curl -fsSL --retry 3 --retry-delay 2 -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$dest" "$url"
    else
        error "neither curl nor wget was found"
    fi
}

# -----------------------------------------------------------------------------
# binary install
# -----------------------------------------------------------------------------
install_binary() {
    local os arch tag asset url ext tmp

    os="$(detect_os)"
    arch="$(detect_arch)"
    [[ "$os" == "unsupported" ]] && error "unsupported operating system: $(uname -s)"
    assert_supported_platform "$os" "$arch"
    resolve_install_dirs

    tag="$(ensure_v_prefix "$(resolve_version)")"
    ext=""
    [[ "$os" == "windows" ]] && ext=".exe"
    asset="go-magic-${os}-${arch}${ext}"
    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

    info "installing go-magic ${tag} (${os}/${arch})"
    info "download: ${url}"

    mkdir -p "$INSTALL_DIR" "$BIN_DIR"
    tmp="$(new_tmp magic-install)"

    download "$url" "$tmp" || error "download failed: $url"

    # Size sanity check: a real binary is about 35MB; anything much smaller means
    # an error page was fetched instead of a binary
    local size
    size="$(wc -c <"$tmp" | tr -d ' ')"
    if [[ "$size" -lt 1048576 ]]; then
        error "unexpected download content (only ${size} bytes); this version may have no artifact for that platform: $asset"
    fi

    install_binary_file "$tmp" "${INSTALL_DIR}/${asset}"
    success "installed: ${INSTALL_DIR}/${asset}"

    # Symlinks are often unavailable on Windows (msys/git-bash), so copy instead
    if [[ "$os" == "windows" ]]; then
        cp -f "${INSTALL_DIR}/${asset}" "${BIN_DIR}/magic${ext}"
    else
        ln -sf "${INSTALL_DIR}/${asset}" "${BIN_DIR}/magic"
    fi
    success "command entry point: ${BIN_DIR}/magic${ext}"

    if [[ ":${PATH}:" != *":${BIN_DIR}:"* ]]; then
        warn "${BIN_DIR} is not in PATH; add it to your shell config (~/.bashrc or ~/.zshrc):"
        printf '\n  export PATH="%s:$PATH"\n\n' "$BIN_DIR"
    fi

    verify_install "${BIN_DIR}/magic${ext}"
}

# Install the binary: prefer install(1), fall back to cp + chmod when it is
# missing (environments such as Windows msys have no install)
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
        warn "executable $bin not found; skipping verification"
        return 0
    fi
    local out
    if out="$("$bin" --version 2>/dev/null)"; then
        success "verification passed: ${out}"
    else
        warn "install verification failed; try running it manually: $bin --version"
    fi
}


# -----------------------------------------------------------------------------
# homebrew / docker / apt / scoop
# -----------------------------------------------------------------------------
install_homebrew() {
    local os
    os="$(detect_os)"
    [[ "$os" == "windows" ]] && error "Homebrew is not supported on Windows; use --method scoop"

    command -v brew >/dev/null 2>&1 || error "Homebrew is not installed; install it first: https://brew.sh"

    info "adding the tap magicwubiao/tap"
    brew tap magicwubiao/tap

    info "installing go-magic"
    brew install magicwubiao/tap/go-magic
    success "Homebrew install complete"
    command -v magic >/dev/null 2>&1 && verify_install "$(command -v magic)"
}

install_docker() {
    command -v docker >/dev/null 2>&1 || error "Docker not found: https://docs.docker.com/get-docker/"

    local tag
    tag="$(ensure_v_prefix "$(resolve_version)")"
    local image="${REPO}:${tag}"

    info "pulling image ${image}"
    docker pull "$image" || error "pull failed: the image may not be published yet (docker images are published by make docker-push / docker-buildx)"

    success "image ready: ${image}"
    info "run: docker run -it --rm -p 8642:8642 -v ~/.magic:/home/magic/.magic ${image}"
}

install_apt() {
    local os arch arch_deb tag deb url tmp
    os="$(detect_os)"
    arch="$(detect_arch)"
    [[ "$os" == "linux" ]] || error "the apt method only supports Linux"

    # CI only builds the amd64 .deb
    [[ "$arch" == "amd64" ]] || error "the apt method currently only provides an amd64 .deb (current arch: ${arch}).
Use --method binary instead."

    command -v apt >/dev/null 2>&1 || error "apt not found"
    command -v sudo >/dev/null 2>&1 || warn "sudo not found; the following commands may need root privileges"

    tag="$(ensure_v_prefix "$(resolve_version)")"
    arch_deb="$(dpkg --print-architecture 2>/dev/null || printf 'amd64')"
    deb="go-magic_${arch_deb}.deb"
    url="https://github.com/${REPO}/releases/download/${tag}/${deb}"

    info "downloading ${deb}"
    tmp="$(new_tmp go-magic-deb)"
    download "$url" "$tmp" || error "download failed: $url"

    info "installing the .deb"
    sudo apt install -y "$tmp"
    success "APT install complete"
    verify_install "/usr/bin/magic"
}

install_scoop() {
    local os
    os="$(detect_os)"
    [[ "$os" == "windows" ]] || error "Scoop only supports Windows (current: ${os})"

    command -v scoop >/dev/null 2>&1 || error "Scoop is not installed; install it first: https://scoop.sh"

    info "adding the Scoop bucket"
    scoop bucket add magic "https://github.com/magicwubiao/scoop-bucket" \
        || error "could not add the bucket (magicwubiao/scoop-bucket may not be published yet).
Use --method binary instead (or build from source on Windows: scripts\\windows\\install.bat)"

    info "installing go-magic"
    scoop install magic
    success "Scoop install complete"
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
printf '\n%b╔════════════════════════════════════════╗%b\n' "$GREEN" "$NC"
printf '%b║      go-magic Installer                ║%b\n' "$GREEN" "$NC"
printf '%b╚════════════════════════════════════════╝%b\n\n' "$GREEN" "$NC"
info "install method: ${INSTALL_METHOD}"

case "$INSTALL_METHOD" in
    binary)   install_binary ;;
    homebrew) install_homebrew ;;
    brew)     install_homebrew ;;
    docker)   install_docker ;;
    apt)      install_apt ;;
    scoop)    install_scoop ;;
    *)        error "unknown install method: ${INSTALL_METHOD} (available: binary|homebrew|docker|apt|scoop)" ;;
esac

printf '\n'
success "install complete"
printf '\n'
info "next: magic setup   # initialize the configuration (config dir ~/.magic)"
info "      magic chat    # start a conversation"
info "      magic server  # start the Web console"
printf '\n'
if [[ "$INSTALL_METHOD" == "binary" ]]; then
    info "uninstall: delete ${INSTALL_DIR} and ${BIN_DIR}/magic (the config in ~/.magic is yours to keep)"
    printf '\n'
fi

