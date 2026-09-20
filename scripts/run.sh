#!/usr/bin/env bash
# =============================================================================
# go-magic local run script
# =============================================================================
# This used to hardcode BINARY=./build/magic, but `make build` produces
# ./dist/magic, so the script always failed with "No such file or directory".
# It now auto-detects the binary by priority.
# =============================================================================
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

REPO_ROOT="$(gm_repo_root)"
cd "$REPO_ROOT"

PORT="${PORT:-5000}"

usage() {
    cat <<EOF
Usage: $0 [-p <port>] [-b <binary>] [-h]

  -p <port>    listening port (default: ${PORT}, override with the PORT env var)
  -b <path>    binary to run (default: auto-detected)
  -h           show help

Binary detection order:
  1. \$BINARY / the path given with -b
  2. ./dist/magic                              (make build / make build-cli)
  3. ./dist/go-magic-<os>-<arch>               (the CI-named release artifact)
  4. ./build/go-magic-<os>-<arch>              (make build-all / build-cross)
EOF
}

BINARY=""

while getopts "p:b:h" opt; do
    case "$opt" in
        p) PORT="$OPTARG" ;;
        b) BINARY="$OPTARG" ;;
        h) usage; exit 0 ;;
        \?) echo "invalid option: -$OPTARG" >&2; usage; exit 1 ;;
    esac
done

# Validate the port so an invalid value is never passed through to the program
case "$PORT" in
    ''|*[!0-9]*)
        gm_error "port must be a number: $PORT"
        exit 1
        ;;
esac
if [[ "$PORT" -lt 1 || "$PORT" -gt 65535 ]]; then
    gm_error "port out of range (1-65535): $PORT"
    exit 1
fi

# Detect the binary
if [[ -z "$BINARY" ]]; then
    local_goos="$(go env GOOS 2>/dev/null || echo linux)"
    local_goarch="$(go env GOARCH 2>/dev/null || echo amd64)"
    candidates=(
        "./dist/magic"
        "./dist/$(gm_platform_asset "${local_goos}/${local_goarch}")"
        "./build/$(gm_platform_asset "${local_goos}/${local_goarch}")"
    )
    for candidate in "${candidates[@]}"; do
        if [[ -x "$candidate" ]]; then
            BINARY="$candidate"
            break
        fi
    done
fi

if [[ -z "$BINARY" || ! -x "$BINARY" ]]; then
    gm_error "no runnable magic binary found"
    echo "" >&2
    echo "Build it first:" >&2
    echo "  make build                       # current platform -> dist/magic" >&2
    echo "  ./scripts/build.sh go linux/amd64  # specific platform -> dist/go-magic-linux-amd64" >&2
    echo "" >&2
    usage >&2
    exit 1
fi

gm_info "running: $BINARY server --port $PORT"

exec "$BINARY" server --port "$PORT"
