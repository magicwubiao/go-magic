#!/usr/bin/env bash
# =============================================================================
# go-magic 本地构建入口
# =============================================================================
# 与 CI (.github/workflows/release.yml) 对齐：
#   - 版本号来自 git tag（唯一来源），不硬编码任何版本号
#   - 先构建 Web UI（internal/server/dist 是 //go:embed 目标且被 .gitignore 忽略）
#   - 产物名与 Release 资产一致：go-magic-<os>-<arch>[.exe]
#   - 平台编译逻辑只有一份，在 build-cross.sh
#
# 用法:
#   scripts/build.sh                    # web + CI 6 平台
#   scripts/build.sh web                # 只构建 Web UI（强制重建）
#   scripts/build.sh current            # 只构建当前平台
#   scripts/build.sh go linux/amd64     # 构建指定平台
#   scripts/build.sh docker             # 构建 Docker 镜像
#   scripts/build.sh release            # 构建 + 打包 + 创建 GitHub Release(draft)
#   scripts/build.sh clean
#   scripts/build.sh list
#
# 选项:
#   --version <v>  --dir <path>  --compress  --checksum  --clean
#   --no-web       跳过 Web UI 构建（要求 dist 已存在）
#   --publish      release 时直接发布（默认 draft）
#   --push         docker 时用 buildx 构建多架构并推送
#   -h, --help
#
# 兼容性: Bash 3.2+（macOS 自带 /bin/bash）
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
go-magic 本地构建

用法: $0 [命令] [平台...] [选项]

命令:
  all            构建 Web UI + CI 6 个发布平台（默认）
  web            只构建 Web UI（强制重建）
  current        只构建当前平台
  go             构建指定平台（需跟平台键）
  docker         构建 Docker 镜像（${DOCKER_REPO}:\$VERSION 与 :latest）
  release        构建 + 打包 + 创建 GitHub Release
  clean          清理 dist/ 与 build/
  list           列出所有可用平台

选项:
  --version <v>  版本号（默认: git tag）
  --dir <path>   输出目录（默认: ./dist）
  --compress     额外生成 .tar.gz / .zip
  --checksum     生成 checksums.txt
  --clean        构建前清空输出目录
  --no-web       跳过 Web UI 构建
  --publish      release 时直接发布（默认 draft）
  --push         docker 时 buildx 构建多架构并推送
  -h, --help     显示帮助

示例:
  $0
  $0 go linux/amd64 windows/arm64
  $0 all --compress --checksum
  $0 release --publish
EOF
}

# 当前平台的平台键；arm 主机映射到 linux/armv7
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

# Web UI 前置：默认按需构建；--no-web 时要求已存在（否则 go build 必然失败）
await_web() {
    if [[ "$NO_WEB" == "true" ]]; then
        if gm_web_dist_ready; then
            gm_ok "跳过 Web UI 构建（--no-web）"
            return 0
        fi
        gm_error "--no-web 但 $(gm_web_dist_dir)/index.html 不存在，go:embed dist 会失败"
        return 1
    fi
    gm_ensure_web_dist
}

# 平台编译统一委托给 build-cross.sh（单一实现）
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
    [[ -f "$REPO_ROOT/Dockerfile" ]] || { gm_error "找不到 Dockerfile"; exit 1; }

    await_web || exit 1

    if [[ "$PUSH" == "true" ]]; then
        docker buildx build --platform linux/amd64,linux/arm64 \
            -t "$DOCKER_REPO:$VERSION" -t "$DOCKER_REPO:latest" --push "$REPO_ROOT"
        gm_ok "已推送多架构镜像 $DOCKER_REPO:$VERSION"
    else
        # 只构建本地镜像，不再像旧脚本那样隐式 --push
        docker build -t "$DOCKER_REPO:$VERSION" -t "$DOCKER_REPO:latest" "$REPO_ROOT"
        gm_ok "镜像已构建: $DOCKER_REPO:$VERSION, $DOCKER_REPO:latest"
    fi
}

cmd_release() {
    gm_require_cmd gh "https://cli.github.com/" || exit 1
    await_web || exit 1

    local tag="$1"
    case "$VERSION" in
        *-[0-9]*-g*) gm_warn "版本 $VERSION 不是精确 tag，正式发布请基于 tag 运行" ;;
    esac

    PLATFORMS="$(printf '%s\n' "${GM_PLATFORMS_CI[@]}" | tr '\n' ' ')"
    COMPRESS="true"
    CHECKSUM="true"
    delegate_build "$PLATFORMS"

    # 收集产物（bash 3.2 下空数组展开不安全，这里用字符串 + 计数）
    local assets="" count=0
    for f in "$BUILD_DIR"/go-magic-*; do
        if [[ -f "$f" ]]; then
            assets="$assets $f"
            count=$((count + 1))
        fi
    done
    if [[ "$count" -eq 0 ]]; then
        gm_error "没有可上传的产物"
        exit 1
    fi

    local gh_args=(release create "$tag" --title "$tag" --generate-notes)
    [[ "$PUBLISH" != "true" ]] && gh_args+=(--draft)

    gm_step "创建 Release $tag（$( [[ "$PUBLISH" == "true" ]] && echo 发布 || echo draft)）"
    # shellcheck disable=SC2086
    gh "${gh_args[@]}" $assets
    gm_ok "Release 地址: https://github.com/$GM_REPO/releases/tag/$tag"
}

cmd_clean() {
    rm -rf "$BUILD_DIR" "$REPO_ROOT/build"
    gm_ok "已清理 $BUILD_DIR 与 $REPO_ROOT/build"
}

show_summary() {
    gm_step "构建摘要"
    gm_info "版本: $VERSION"
    gm_info "输出: $BUILD_DIR"
    if [[ -d "$BUILD_DIR" ]]; then
        ls -1 "$BUILD_DIR" 2>/dev/null | sed 's/^/  /'
    fi
    echo ""
    gm_info "运行: ./scripts/run.sh -p 5000"
}

# =============================================================================
# 参数解析
# =============================================================================
while [[ $# -gt 0 ]]; do
    case "$1" in
        all|web|current|docker|release|clean|list|go)
            COMMAND="$1"
            ;;
        --version)
            [[ -n "${2:-}" ]] || { gm_error "--version 需要参数"; exit 1; }
            VERSION="$2"; shift
            ;;
        --dir)
            [[ -n "${2:-}" ]] || { gm_error "--dir 需要参数"; exit 1; }
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
            gm_error "未知选项: $1"; usage; exit 1
            ;;
        *)
            key="$(gm_normalize_platform "$1")"
            if gm_platform_env "$key" >/dev/null 2>&1; then
                if [[ -z "$PLATFORMS" ]]; then PLATFORMS="$key"; else PLATFORMS="$PLATFORMS $key"; fi
                [[ -z "$COMMAND" ]] && COMMAND="go"
            else
                gm_error "未知平台: $1（用 $0 list 查看可用平台）"
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
        key="$(current_platform_key)" || { gm_error "无法识别当前平台（$(go env GOOS)/$(go env GOARCH)）"; exit 1; }
        gm_step "构建当前平台 $key"
        await_web || exit 1
        delegate_build "$key"
        show_summary
        ;;
    go)
        if [[ -z "$PLATFORMS" ]]; then
            gm_error "go 命令需要指定平台，例如: $0 go linux/amd64"
            exit 1
        fi
        gm_step "构建指定平台: $PLATFORMS"
        await_web || exit 1
        delegate_build "$PLATFORMS"
        show_summary
        ;;
    all)
        gm_step "构建 Web UI + CI 发布平台"
        await_web || exit 1
        delegate_build "$(printf '%s\n' "${GM_PLATFORMS_CI[@]}" | tr '\n' ' ')"
        show_summary
        ;;
    *)
        gm_error "未知命令: $COMMAND"
        usage
        exit 1
        ;;
esac
