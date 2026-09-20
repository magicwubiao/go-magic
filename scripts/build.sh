#!/usr/bin/env bash
# =============================================================================
# go-magic local build entry point
# =============================================================================
# Aligned with CI (.github/workflows/release.yml):
#   - Version comes from the git tag (single source of truth), never hardcoded
#   - Builds the Web UI first (internal/server/dist is the //go:embed target and
#     is ignored by .gitignore)
#   - Artifact names match the Release assets: go-magic-<os>-<arch>[.exe]
#   - There is exactly one implementation of the per-platform compile logic,
#     in build-cross.sh
#
# Usage:
#   scripts/build.sh                    # web + the 6 CI platforms
#   scripts/build.sh web                # build the Web UI only (force rebuild)
#   scripts/build.sh current            # build the current platform only
#   scripts/build.sh go linux/amd64     # build the given platform
#   scripts/build.sh docker             # build the Docker image
#   scripts/build.sh release            # build + package + create a GitHub Release (draft)
#   scripts/build.sh clean
#   scripts/build.sh list
#
# Options:
#   --version <v>  --dir <path>  --compress  --checksum  --clean
#   --no-web       skip the Web UI build (requires dist to exist)
#   --publish      publish on release (default: draft)
#   --push         build multi-arch with buildx and push (docker)
#   -h, --help
#
# Compatibility: Bash 3.2+ (the /bin/bash shipped with macOS)
# =============================================================================
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

REPO_ROOT="$(gm_repo_root)"
cd "$REPO_ROOT"

BUILD_DIR="${BUILD_DIR:-$REPO_ROOT/dist}"
DOCKER_REPO="${DOCKER_REPO:-$GM_REPO}"
VERSION="$(gm_resolve_version)"

COMMAND=""
PLATFORMS=""
COMPRESS="false"
CHECKSUM="false"
CLEAN="false"
NO_WEB="false"
PUBLISH="false"
PUSH="false"

usage() {
    cat <<EOF
go-magic local build

Usage: $0 [command] [platform...] [options]

Commands:
  all            build the Web UI + the 6 CI release platforms (default)
  web            build the Web UI only (force rebuild)
  current        build the current platform only
  go             build the given platform(s) (requires a platform key)
  docker         build the Docker image (${DOCKER_REPO}:\$VERSION and :latest)
  release        build + package + create a GitHub Release
  clean          remove dist/ and build/
  list           list all available platforms

Options:
  --version <v>  version (default: git tag)
  --dir <path>   output directory (default: ./dist)
  --compress     also produce .tar.gz / .zip
  --checksum     generate checksums.txt
  --clean        empty the output directory before building
  --no-web       skip the Web UI build
  --publish      publish on release (default: draft)
  --push         build multi-arch with buildx and push (docker)
  -h, --help     show help

Examples:
  $0
  $0 go linux/amd64 windows/arm64
  $0 all --compress --checksum
  $0 release --publish
EOF
}

# Platform key of the current machine; arm hosts map to linux/armv7
current_platform_key() {
    local goos goarch key
    goos="$(go env GOOS)"
    goarch="$(go env GOARCH)"

    key="$goos/$goarch"
    if gm_platform_env "$key" >/dev/null 2>&1; then
        printf '%s\n' "$key"
        return 0
    fi
    if [[ "$goarch" == "arm" ]]; then
        key="$goos/armv7"
        gm_platform_env "$key" >/dev/null 2>&1 && { printf '%s\n' "$key"; return 0; }
    fi
    return 1
}

# Web UI prerequisite: built on demand by default; with --no-web it must already
# exist (otherwise go build is guaranteed to fail)
await_web() {
    if [[ "$NO_WEB" == "true" ]]; then
        if gm_web_dist_ready; then
            gm_ok "skipping the Web UI build (--no-web)"
            return 0
        fi
        gm_error "--no-web given but $(gm_web_dist_dir)/index.html is missing; go:embed dist would fail"
        return 1
    fi
    gm_ensure_web_dist
}

# Per-platform compilation is delegated to build-cross.sh (single implementation)
delegate_build() {
    local platforms="$1"
    local args=()
    local p

    for p in $platforms; do
        args+=("$p")
    done
    args+=("--dir" "$BUILD_DIR" "--version" "$VERSION")
    [[ "$COMPRESS" == "true" ]] && args+=("--compress")
    [[ "$CHECKSUM" == "true" ]] && args+=("--checksum")
    [[ "$CLEAN" == "true" ]] && args+=("--clean")

    "$SCRIPT_DIR/build-cross.sh" "${args[@]}"
}

cmd_docker() {
    gm_require_cmd docker "https://docs.docker.com/get-docker/" || exit 1
    [[ -f "$REPO_ROOT/Dockerfile" ]] || { gm_error "Dockerfile not found"; exit 1; }

    await_web || exit 1

    if [[ "$PUSH" == "true" ]]; then
        docker buildx build --platform linux/amd64,linux/arm64 \
            -t "$DOCKER_REPO:$VERSION" -t "$DOCKER_REPO:latest" --push "$REPO_ROOT"
        gm_ok "pushed multi-arch image $DOCKER_REPO:$VERSION"
    else
        # Build the local image only; no implicit --push like the old script did
        docker build -t "$DOCKER_REPO:$VERSION" -t "$DOCKER_REPO:latest" "$REPO_ROOT"
        gm_ok "image built: $DOCKER_REPO:$VERSION, $DOCKER_REPO:latest"
    fi
}

cmd_release() {
    gm_require_cmd gh "https://cli.github.com/" || exit 1
    await_web || exit 1

    local tag="$1"
    case "$VERSION" in
        *-[0-9]*-g*) gm_warn "version $VERSION is not an exact tag; run from a tag for a real release" ;;
    esac

    PLATFORMS="$(printf '%s\n' "${GM_PLATFORMS_CI[@]}" | tr '\n' ' ')"
    COMPRESS="true"
    CHECKSUM="true"
    delegate_build "$PLATFORMS"

    # Collect artifacts (expanding empty arrays is unsafe on bash 3.2, so use a
    # string plus a counter here)
    local assets="" count=0
    for f in "$BUILD_DIR"/go-magic-*; do
        if [[ -f "$f" ]]; then
            assets="$assets $f"
            count=$((count + 1))
        fi
    done
    if [[ "$count" -eq 0 ]]; then
        gm_error "no artifacts to upload"
        exit 1
    fi

    local gh_args=(release create "$tag" --title "$tag" --generate-notes)
    [[ "$PUBLISH" != "true" ]] && gh_args+=(--draft)

    gm_step "creating Release $tag ($( [[ "$PUBLISH" == "true" ]] && echo published || echo draft))"
    # shellcheck disable=SC2086
    gh "${gh_args[@]}" $assets
    gm_ok "Release URL: https://github.com/$GM_REPO/releases/tag/$tag"
}

cmd_clean() {
    rm -rf "$BUILD_DIR" "$REPO_ROOT/build"
    gm_ok "removed $BUILD_DIR and $REPO_ROOT/build"
}

show_summary() {
    gm_step "build summary"
    gm_info "version: $VERSION"
    gm_info "output:  $BUILD_DIR"
    if [[ -d "$BUILD_DIR" ]]; then
        ls -1 "$BUILD_DIR" 2>/dev/null | sed 's/^/  /'
    fi
    echo ""
    gm_info "run: ./scripts/run.sh -p 5000"
}

# =============================================================================
# Argument parsing
# =============================================================================
while [[ $# -gt 0 ]]; do
    case "$1" in
        all|web|current|docker|release|clean|list|go)
            COMMAND="$1"
            ;;
        --version)
            [[ -n "${2:-}" ]] || { gm_error "--version requires a value"; exit 1; }
            VERSION="$2"; shift
            ;;
        --dir)
            [[ -n "${2:-}" ]] || { gm_error "--dir requires a value"; exit 1; }
            BUILD_DIR="$2"; shift
            ;;
        --compress) COMPRESS="true" ;;
        --checksum) CHECKSUM="true" ;;
        --clean)    CLEAN="true" ;;
        --no-web)   NO_WEB="true" ;;
        --publish)  PUBLISH="true" ;;
        --push)     PUSH="true" ;;
        -h|--help)  usage; exit 0 ;;
        -*)
            gm_error "unknown option: $1"; usage; exit 1
            ;;
        *)
            key="$(gm_normalize_platform "$1")"
            if gm_platform_env "$key" >/dev/null 2>&1; then
                if [[ -z "$PLATFORMS" ]]; then PLATFORMS="$key"; else PLATFORMS="$PLATFORMS $key"; fi
                [[ -z "$COMMAND" ]] && COMMAND="go"
            else
                gm_error "unknown platform: $1 (use $0 list to see the available platforms)"
                exit 1
            fi
            ;;
    esac
    shift
done

case "${COMMAND:-all}" in
    list)
        exec "$SCRIPT_DIR/build-cross.sh" list
        ;;
    clean)
        cmd_clean
        ;;
    web)
        rm -rf "$(gm_web_dist_dir)"
        gm_ensure_web_dist
        ;;
    docker)
        cmd_docker
        ;;
    release)
        cmd_release "$(gm_tag_name "$VERSION")"
        ;;
    current)
        key="$(current_platform_key)" || { gm_error "cannot detect the current platform ($(go env GOOS)/$(go env GOARCH))"; exit 1; }
        gm_step "building the current platform $key"
        await_web || exit 1
        delegate_build "$key"
        show_summary
        ;;
    go)
        if [[ -z "$PLATFORMS" ]]; then
            gm_error "the go command requires a platform, e.g. $0 go linux/amd64"
            exit 1
        fi
        gm_step "building platform(s): $PLATFORMS"
        await_web || exit 1
        delegate_build "$PLATFORMS"
        show_summary
        ;;
    all)
        gm_step "building the Web UI + the CI release platforms"
        await_web || exit 1
        delegate_build "$(printf '%s\n' "${GM_PLATFORMS_CI[@]}" | tr '\n' ' ')"
        show_summary
        ;;
    *)
        gm_error "unknown command: $COMMAND"
        usage
        exit 1
        ;;
esac
