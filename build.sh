#!/usr/bin/env bash
# =============================================================================
# go-magic build entry point (repository root)
# =============================================================================
# This used to be a third, independent build implementation that contradicted
# scripts/build.sh and scripts/build-cross.sh (it fell back to a fake fixed
# version, printed a path that had already been deleted after gzipping, and used
# grep -P, which fails outright on macOS ...).
#
# It is now a thin wrapper; the real implementation lives under scripts/, so
# there is only one build logic:
#   scripts/lib/common.sh     version/platform/asset naming (single source of truth)
#   scripts/build-cross.sh    per-platform compilation
#   scripts/build.sh          web / platform / docker / release orchestration
#
# Legacy command names are still accepted:
#   ./build.sh cli     -> scripts/build.sh current   (current platform)
#   ./build.sh web     -> scripts/build.sh web
#   ./build.sh docker  -> scripts/build.sh docker
#   ./build.sh all     -> scripts/build.sh all
#   ./build.sh release -> scripts/build.sh release
#
# All other arguments (--version/--dir/--compress/--checksum/--clean etc.) are
# passed through unchanged.
# Note: the old `all` also ran docker build; it no longer does (so a machine
# without docker does not abort the build).
# =============================================================================
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TARGET="${1:-all}"
[[ $# -gt 0 ]] && shift || true

case "$TARGET" in
    cli|current)
        exec "$SCRIPT_DIR/scripts/build.sh" current "$@"
        ;;
    web)
        exec "$SCRIPT_DIR/scripts/build.sh" web "$@"
        ;;
    docker)
        exec "$SCRIPT_DIR/scripts/build.sh" docker "$@"
        ;;
    all|"")
        exec "$SCRIPT_DIR/scripts/build.sh" all "$@"
        ;;
    release)
        exec "$SCRIPT_DIR/scripts/build.sh" release "$@"
        ;;
    clean)
        exec "$SCRIPT_DIR/scripts/build.sh" clean "$@"
        ;;
    list)
        exec "$SCRIPT_DIR/scripts/build.sh" list "$@"
        ;;
    go)
        exec "$SCRIPT_DIR/scripts/build.sh" go "$@"
        ;;
    -h|--help|help)
        cat <<EOF
go-magic build entry point (forwards to scripts/build.sh)

Usage: ./build.sh [command] [platform...] [options]

Commands:
  all        build the Web UI + the 6 CI release platforms (default)
  cli        build the current platform only (legacy name; same as scripts/build.sh current)
  web        build the Web UI only
  docker     build the Docker image
  release    build + package + create a GitHub Release
  clean      remove dist/ and build/
  list       list all available platforms

Options:
  --version <v>  --dir <path>  --compress  --checksum  --clean
  --no-web      skip the Web UI build
  --publish     publish on release (default: draft)
  --push        build multi-arch with buildx and push (docker)

Equivalent: ./scripts/build.sh --help
EOF
        ;;
    *)
        echo "[FAIL] unknown target: $TARGET" >&2
        echo "usage: ./build.sh [all|cli|web|docker|release|clean|list]" >&2
        exit 1
        ;;
esac
