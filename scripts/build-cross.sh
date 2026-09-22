#!/usr/bin/env bash
# =============================================================================
# go-magic cross-platform build script (the single platform build implementation)
# =============================================================================
# Artifact names match the CI Release assets exactly: go-magic-<os>-<arch>[.exe]
#   go-magic-linux-amd64   go-magic-linux-arm64
#   go-magic-darwin-amd64  go-magic-darwin-arm64
#   go-magic-windows-amd64.exe   go-magic-windows-arm64.exe
#
# Usage:
#   scripts/build-cross.sh                          # the 6 CI platforms
#   scripts/build-cross.sh all                      # the 6 CI platforms + extras
#   scripts/build-cross.sh linux/amd64 darwin/arm64 # specific platforms (linux-amd64 works too)
#   scripts/build-cross.sh list                     # list platforms
#
# Options:
#   --version <v>   version (default: git tag, same source as CI)
#   --dir <path>    output directory (default: ./dist)
#   --compress      also produce .tar.gz / .zip
#   --checksum      generate checksums.txt
#   --clean         empty the output directory first
#   -h, --help      help
#
# Compatibility: Bash 3.2+ (the /bin/bash shipped with macOS); no associative
# arrays and no empty-array expansion
# =============================================================================
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

REPO_ROOT="$(gm_repo_root)"
cd "$REPO_ROOT"

VERSION="$(gm_resolve_version)"
OUT_DIR="${BUILD_DIR:-$REPO_ROOT/dist}"
COMPRESS="false"
CHECKSUM="false"
CLEAN="false"
COMMAND=""

# The platform list is kept in a newline-separated string: expanding an empty
# array under bash 3.2 + set -u raises "unbound variable"
PLATFORMS=""
PLATFORM_COUNT=0
FAILED=""
FAILED_COUNT=0

usage() {
    cat <<EOF
go-magic cross-platform build

Usage: $0 [command] [platform...] [options]

Commands:
  common         build the 6 CI release platforms (default)
  all            build the 6 CI platforms + the extra local ones
  list           list all available platforms

Options:
  --version <v>  version (default: the git tag from git describe)
  --dir <path>   output directory (default: ./dist)
  --compress     also produce .tar.gz / .zip
  --checksum     generate checksums.txt
  --clean        remove the output directory first
  -h, --help     show help

Examples:
  $0
  $0 all --compress --checksum
  $0 linux/amd64 darwin/arm64 --dir ./build
EOF
}

list_platforms() {
    local key
    printf 'CI release platforms (same names as the Release assets):\n'
    for key in "${GM_PLATFORMS_CI[@]}"; do
        printf '  %-16s -> %s\n' "$key" "$(gm_platform_asset "$key")"
    done
    printf '\nExtra local platforms (not released):\n'
    for key in "${GM_PLATFORMS_EXTRA[@]}"; do
        printf '  %-16s -> %s\n' "$key" "$(gm_platform_asset "$key")"
    done
}

add_platform() {
    # Deduplicate (exact match on the newline-separated list, so the same
    # platform is never compiled twice)
    if [[ $'\n'"$PLATFORMS"$'\n' == *$'\n'"$1"$'\n'* ]]; then
        return 0
    fi
    if [[ -z "$PLATFORMS" ]]; then
        PLATFORMS="$1"
    else
        PLATFORMS="$PLATFORMS
$1"
    fi
    PLATFORM_COUNT=$((PLATFORM_COUNT + 1))
}

# linux-amd64 -> linux/amd64 (already-slash form is accepted as well)
normalize_platform() {
    gm_normalize_platform "$1"
}

run_go_build() {
    local goos="$1" goarch="$2" goarm="$3" out="$4"
    # `go` is a **native** binary: under Git Bash a POSIX `-o /d/...` path is read
    # as "drive-relative", the compile silently lands in `D:\d\...` and go exits 0.
    # Hand the toolchain a Windows path when that is what it expects.
    local out_native
    out_native="$(gm_native_path "$out")"
    if [[ -n "$goarm" ]]; then
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
            go build -ldflags "$(gm_ldflags "$VERSION")" -o "$out_native" ./cmd/magic
    else
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
            go build -ldflags "$(gm_ldflags "$VERSION")" -o "$out_native" ./cmd/magic
    fi
}

build_one() {
    local key="$1"
    local env_str goos goarch goarm asset out

    if ! env_str="$(gm_platform_env "$key")"; then
        gm_error "unsupported platform: $key (use $0 list to see the available platforms)"
        return 1
    fi

    goos="${env_str%% *}"
    goarch="${env_str#* }"
    goarm=""
    case "$goarch" in
        *\ *) goarm="${goarch#* }"; goarch="${goarch%% *}" ;;
    esac

    asset="$(gm_platform_asset "$key")"
    out="$OUT_DIR/$asset"

    gm_info "building $key -> $asset"

    # CGO_ENABLED=0 matches CI (SQLite uses the pure-Go modernc.org/sqlite)
    if ! run_go_build "$goos" "$goarch" "$goarm" "$out"; then
        gm_error "build failed: $key"
        return 1
    fi

    # go build can exit 0 without producing anything (most notoriously when `-o`
    # gets a path the native toolchain cannot interpret, e.g. a Git-Bash
    # `/d/...` -- see gm_native_path). Never report success on a phantom file.
    if [[ ! -f "$out" ]]; then
        gm_error "go build exited 0 but $out does not exist"
        gm_error "  (native toolchain could not write the -o path; is it a POSIX path on Windows?)"
        return 1
    fi

    gm_ok "created $out ($(gm_file_size "$out"))"

    if [[ "$COMPRESS" == "true" ]]; then
        create_archive "$out" "$goos"
    fi
    return 0
}

create_archive() {
    local bin="$1" goos="$2"
    local base dir
    dir="$(dirname "$bin")"
    base="$(basename "$bin")"

    case "$goos" in
        windows)
            if command -v zip >/dev/null 2>&1; then
                ( cd "$dir" && rm -f "${base%.exe}.zip" && zip -q "${base%.exe}.zip" "$base" )
                rm -f "$bin"
                gm_ok "compressed ${base%.exe}.zip"
            else
                gm_warn "zip is not installed; skipping the archive for $base"
            fi
            ;;
        *)
            ( cd "$dir" && tar -czf "${base}.tar.gz" "$base" )
            rm -f "$bin"
            gm_ok "compressed ${base}.tar.gz"
            ;;
    esac
}

generate_checksums() {
    local tool=""
    if command -v sha256sum >/dev/null 2>&1; then
        tool="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        tool="shasum -a 256"
    elif command -v sha256 >/dev/null 2>&1; then
        tool="sha256"
    else
        gm_warn "no sha256 tool found; skipping checksums"
        return 1
    fi

    # checksums.txt itself must be excluded first, otherwise a re-run folds the
    # previous result into the new one (bug in the original script)
    (
        cd "$OUT_DIR" || exit 1
        rm -f checksums.txt
        # shellcheck disable=SC2086
        find . -maxdepth 1 -type f ! -name checksums.txt -exec $tool {} + \
            | sed 's# \./# #' | sort -k2 > checksums.txt
    )
    gm_ok "created $OUT_DIR/checksums.txt"
}

# =============================================================================
# Argument parsing
# =============================================================================
while [[ $# -gt 0 ]]; do
    case "$1" in
        common|all|list)
            COMMAND="$1"
            ;;
        --version)
            [[ -n "${2:-}" ]] || { gm_error "--version requires a value"; exit 1; }
            VERSION="$2"; shift
            ;;
        --dir)
            [[ -n "${2:-}" ]] || { gm_error "--dir requires a value"; exit 1; }
            OUT_DIR="$2"; shift
            ;;
        --compress) COMPRESS="true" ;;
        --checksum) CHECKSUM="true" ;;
        --clean)    CLEAN="true" ;;
        -h|--help)  usage; exit 0 ;;
        -*)
            gm_error "unknown option: $1"
            usage
            exit 1
            ;;
        *)
            # Fixes a bug in the original script that swallowed the first
            # positional argument as COMMAND (./build-cross.sh linux-amd64 used
            # to silently degrade to building "common")
            key="$(normalize_platform "$1")"
            if gm_platform_env "$key" >/dev/null 2>&1; then
                add_platform "$key"
            else
                gm_error "unknown platform: $1 (use $0 list to see the available platforms)"
                exit 1
            fi
            ;;
    esac
    shift
done

if [[ "$COMMAND" == "list" ]]; then
    list_platforms
    exit 0
fi

gm_require_cmd go "please install Go 1.26+" || exit 1

# Platform selection: explicit platforms > command > default "common"
if [[ "$PLATFORM_COUNT" -eq 0 ]]; then
    case "${COMMAND:-common}" in
        all)
            for key in "${GM_PLATFORMS_CI[@]}" "${GM_PLATFORMS_EXTRA[@]}"; do
                add_platform "$key"
            done
            ;;
        *)
            for key in "${GM_PLATFORMS_CI[@]}"; do
                add_platform "$key"
            done
            ;;
    esac
fi

# Safety check: an empty --dir or one pointing at the root would make rm -rf a disaster
if [[ -z "$OUT_DIR" || "$OUT_DIR" == "/" || ${#OUT_DIR} -lt 3 ]]; then
    gm_error "invalid output directory: '$OUT_DIR'"
    exit 1
fi

if [[ "$CLEAN" == "true" ]]; then
    gm_info "emptying the output directory $OUT_DIR"
    rm -rf "$OUT_DIR"
fi
mkdir -p "$OUT_DIR"

# =============================================================================
# Build
# =============================================================================
gm_step "go-magic ${VERSION} -- ${PLATFORM_COUNT} platform(s) -> $OUT_DIR"

# On a clean clone internal/server/dist is missing and go build fails outright,
# so build it here as a fallback
gm_ensure_web_dist || exit 1

while IFS= read -r key; do
    [[ -z "$key" ]] && continue
    if ! build_one "$key"; then
        if [[ -z "$FAILED" ]]; then FAILED="$key"; else FAILED="$FAILED $key"; fi
        FAILED_COUNT=$((FAILED_COUNT + 1))
    fi
done <<EOF
$PLATFORMS
EOF

echo ""
if [[ "$CHECKSUM" == "true" ]]; then
    generate_checksums || true
fi

gm_step "build result"
gm_info "version:  $VERSION"
gm_info "output:   $OUT_DIR"
gm_info "succeeded: $((PLATFORM_COUNT - FAILED_COUNT))/${PLATFORM_COUNT}"

if [[ "$FAILED_COUNT" -gt 0 ]]; then
    gm_error "failed platforms: $FAILED"
    exit 1
fi

ls -1 "$OUT_DIR" 2>/dev/null | sed 's/^/  /'
gm_ok "all done"
