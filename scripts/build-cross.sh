#!/usr/bin/env bash
# =============================================================================
# go-magic 跨平台编译脚本（唯一的平台编译实现）
# =============================================================================
# 产物命名与 CI Release 资产严格一致：go-magic-<os>-<arch>[.exe]
#   go-magic-linux-amd64   go-magic-linux-arm64
#   go-magic-darwin-amd64  go-magic-darwin-arm64
#   go-magic-windows-amd64.exe   go-magic-windows-arm64.exe
#
# 用法:
#   scripts/build-cross.sh                          # CI 6 平台
#   scripts/build-cross.sh all                      # CI 6 平台 + 额外平台
#   scripts/build-cross.sh linux/amd64 darwin/arm64 # 指定平台（也接受 linux-amd64）
#   scripts/build-cross.sh list                     # 列出平台
#
# 选项:
#   --version <v>   指定版本（默认取 git tag，与 CI 同源）
#   --dir <path>    输出目录（默认 ./dist）
#   --compress      额外生成 .tar.gz / .zip
#   --checksum      生成 checksums.txt
#   --clean         先清空输出目录
#   -h, --help      帮助
#
# 兼容性: Bash 3.2+（macOS 自带 /bin/bash），不使用关联数组 / 空数组展开
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

# 平台列表用换行分隔字符串保存：空数组展开在 bash 3.2 + set -u 下会报 unbound variable
PLATFORMS=""
PLATFORM_COUNT=0
FAILED=""
FAILED_COUNT=0

usage() {
    cat <<EOF
go-magic 跨平台编译

用法: $0 [命令] [平台...] [选项]

命令:
  common         构建 CI 的 6 个发布平台（默认）
  all            构建 CI 6 平台 + 额外本地平台
  list           列出所有可用平台

选项:
  --version <v>  版本号（默认: git describe 得到的 git tag）
  --dir <path>   输出目录（默认: ./dist）
  --compress     额外生成 .tar.gz / .zip
  --checksum     生成 checksums.txt
  --clean        先删除输出目录
  -h, --help     显示帮助

示例:
  $0
  $0 all --compress --checksum
  $0 linux/amd64 darwin/arm64 --dir ./build
EOF
}

list_platforms() {
    local key
    printf 'CI 发布平台（与 Release 资产同名）:\n'
    for key in "${GM_PLATFORMS_CI[@]}"; do
        printf '  %-16s -> %s\n' "$key" "$(gm_platform_asset "$key")"
    done
    printf '\n额外本地平台（不发布）:\n'
    for key in "${GM_PLATFORMS_EXTRA[@]}"; do
        printf '  %-16s -> %s\n' "$key" "$(gm_platform_asset "$key")"
    done
}

add_platform() {
    # 去重（用换行分隔的精确匹配，避免重复编译同一平台）
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

# linux-amd64 -> linux/amd64（同时兼容已经是 linux/amd64 的写法）
normalize_platform() {
    gm_normalize_platform "$1"
}

run_go_build() {
    local goos="$1" goarch="$2" goarm="$3" out="$4"
    if [[ -n "$goarm" ]]; then
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
            go build -ldflags "$(gm_ldflags "$VERSION")" -o "$out" ./cmd/magic
    else
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
            go build -ldflags "$(gm_ldflags "$VERSION")" -o "$out" ./cmd/magic
    fi
}

build_one() {
    local key="$1"
    local env_str goos goarch goarm asset out

    if ! env_str="$(gm_platform_env "$key")"; then
        gm_error "不支持的平台: $key（用 $0 list 查看可用平台）"
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

    gm_info "编译 $key -> $asset"

    # CGO_ENABLED=0 与 CI 一致（SQLite 使用纯 Go 的 modernc.org/sqlite）
    if ! run_go_build "$goos" "$goarch" "$goarm" "$out"; then
        gm_error "编译失败: $key"
        return 1
    fi

    gm_ok "已生成 $out ($(gm_file_size "$out"))"

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
                gm_ok "已压缩 ${base%.exe}.zip"
            else
                gm_warn "未安装 zip，跳过 $base 的压缩"
            fi
            ;;
        *)
            ( cd "$dir" && tar -czf "${base}.tar.gz" "$base" )
            rm -f "$bin"
            gm_ok "已压缩 ${base}.tar.gz"
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
        gm_warn "找不到 sha256 工具，跳过 checksums"
        return 1
    fi

    # 必须先排除 checksums.txt 自身，否则重跑会把上一版结果算进去（原脚本的 bug）
    (
        cd "$OUT_DIR" || exit 1
        rm -f checksums.txt
        # shellcheck disable=SC2086
        find . -maxdepth 1 -type f ! -name checksums.txt -exec $tool {} + \
            | sed 's# \./# #' | sort -k2 > checksums.txt
    )
    gm_ok "已生成 $OUT_DIR/checksums.txt"
}

# =============================================================================
# 参数解析
# =============================================================================
while [[ $# -gt 0 ]]; do
    case "$1" in
        common|all|list)
            COMMAND="$1"
            ;;
        --version)
            [[ -n "${2:-}" ]] || { gm_error "--version 需要参数"; exit 1; }
            VERSION="$2"; shift
            ;;
        --dir)
            [[ -n "${2:-}" ]] || { gm_error "--dir 需要参数"; exit 1; }
            OUT_DIR="$2"; shift
            ;;
        --compress) COMPRESS="true" ;;
        --checksum) CHECKSUM="true" ;;
        --clean)    CLEAN="true" ;;
        -h|--help)  usage; exit 0 ;;
        -*)
            gm_error "未知选项: $1"
            usage
            exit 1
            ;;
        *)
            # 修正原脚本把第一个位置参数当成 COMMAND 吞掉的 bug
            # （./build-cross.sh linux-amd64 过去会静默退化成构建 common）
            key="$(normalize_platform "$1")"
            if gm_platform_env "$key" >/dev/null 2>&1; then
                add_platform "$key"
            else
                gm_error "未知平台: $1（用 $0 list 查看可用平台）"
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

gm_require_cmd go "请安装 Go 1.26+" || exit 1

# 平台选择：显式平台 > 命令 > 默认 common
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

# 安全检查：避免 --dir 为空或指向根目录导致 rm -rf 灾难
if [[ -z "$OUT_DIR" || "$OUT_DIR" == "/" || ${#OUT_DIR} -lt 3 ]]; then
    gm_error "输出目录不合法: '$OUT_DIR'"
    exit 1
fi

if [[ "$CLEAN" == "true" ]]; then
    gm_info "清空输出目录 $OUT_DIR"
    rm -rf "$OUT_DIR"
fi
mkdir -p "$OUT_DIR"

# =============================================================================
# 开始构建
# =============================================================================
gm_step "go-magic ${VERSION} —— 共 ${PLATFORM_COUNT} 个平台 -> $OUT_DIR"

# 干净克隆上没有 internal/server/dist 时 go build 会直接失败，这里先兜底构建
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

gm_step "构建结果"
gm_info "版本:   $VERSION"
gm_info "输出:   $OUT_DIR"
gm_info "成功:   $((PLATFORM_COUNT - FAILED_COUNT))/${PLATFORM_COUNT}"

if [[ "$FAILED_COUNT" -gt 0 ]]; then
    gm_error "失败平台: $FAILED"
    exit 1
fi

ls -1 "$OUT_DIR" 2>/dev/null | sed 's/^/  /'
gm_ok "全部完成"
