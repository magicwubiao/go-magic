#!/usr/bin/env bash
# =============================================================================
# go-magic 本地运行脚本
# =============================================================================
# 以前这里硬编码 BINARY=./build/magic，但 `make build` 产出的是 ./dist/magic，
# 所以脚本必然报“No such file or directory”。现在按优先级自动探测。
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
用法: $0 [-p <port>] [-b <binary>] [-h]

  -p <port>    监听端口（默认: ${PORT}，可用环境变量 PORT 覆盖）
  -b <path>    指定要运行的二进制（默认自动探测）
  -h           显示帮助

二进制探测顺序:
  1. \$BINARY / -b 指定的路径
  2. ./dist/magic                              （make build / make build-cli）
  3. ./dist/go-magic-<os>-<arch>               （CI 同名的发布产物）
  4. ./build/go-magic-<os>-<arch>              （make build-all / build-cross）
EOF
}

BINARY=""

while getopts "p:b:h" opt; do
    case "$opt" in
        p) PORT="$OPTARG" ;;
        b) BINARY="$OPTARG" ;;
        h) usage; exit 0 ;;
        \?) echo "无效选项: -$OPTARG" >&2; usage; exit 1 ;;
    esac
done

# 端口校验，避免把非法值透传给程序
case "$PORT" in
    ''|*[!0-9]*)
        gm_error "端口必须是数字: $PORT"
        exit 1
        ;;
esac
if [[ "$PORT" -lt 1 || "$PORT" -gt 65535 ]]; then
    gm_error "端口超出范围 (1-65535): $PORT"
    exit 1
fi

# 探测二进制
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
    gm_error "找不到可执行的 magic 二进制"
    echo "" >&2
    echo "请先构建：" >&2
    echo "  make build                       # 当前平台 -> dist/magic" >&2
    echo "  ./scripts/build.sh go linux/amd64  # 指定平台 -> dist/go-magic-linux-amd64" >&2
    echo "" >&2
    usage >&2
    exit 1
fi

gm_info "运行: $BINARY server --port $PORT"

exec "$BINARY" server --port "$PORT"
