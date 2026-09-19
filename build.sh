#!/usr/bin/env bash
# =============================================================================
# go-magic 构建入口（仓库根目录）
# =============================================================================
# 这里曾经是第三份独立的构建实现（与 scripts/build.sh、scripts/build-cross.sh
# 逻辑互相矛盾：把版本号兜底成一个假的固定版本、gzip 之后还在打印已被删除的路径、
# 用 grep -P 在 macOS 上直接失败……）。
#
# 现在它只是一个薄封装，真正实现在 scripts/ 下，保证只有一份构建逻辑：
#   scripts/lib/common.sh     版本/平台/资产名（单一事实源）
#   scripts/build-cross.sh    平台编译
#   scripts/build.sh          web / 平台 / docker / release 编排
#
# 兼容旧命令名:
#   ./build.sh cli     -> scripts/build.sh current   （当前平台）
#   ./build.sh web     -> scripts/build.sh web
#   ./build.sh docker  -> scripts/build.sh docker
#   ./build.sh all     -> scripts/build.sh all
#   ./build.sh release -> scripts/build.sh release
#
# 其它参数（--version/--dir/--compress/--checksum/--clean 等）原样透传。
# 注意: 旧版本的 `all` 会顺带执行 docker build，现在不会（避免无 docker 环境时中断）。
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
go-magic 构建入口（转发到 scripts/build.sh）

用法: ./build.sh [命令] [平台...] [选项]

命令:
  all        构建 Web UI + CI 6 个发布平台（默认）
  cli        只构建当前平台（旧名，等价 scripts/build.sh current）
  web        只构建 Web UI
  docker     构建 Docker 镜像
  release    构建 + 打包 + 创建 GitHub Release
  clean      清理 dist/ 与 build/
  list       列出所有可用平台

选项:
  --version <v>  --dir <path>  --compress  --checksum  --clean
  --no-web      跳过 Web UI 构建
  --publish     release 时直接发布（默认 draft）
  --push        docker 时 buildx 多架构并推送

等价实现: ./scripts/build.sh --help
EOF
        ;;
    *)
        echo "[FAIL] 未知目标: $TARGET" >&2
        echo "用法: ./build.sh [all|cli|web|docker|release|clean|list]" >&2
        exit 1
        ;;
esac
