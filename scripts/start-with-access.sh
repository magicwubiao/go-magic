#!/usr/bin/env bash
# ============================================================
# Magic Agent - 一键启动 + 手机访问脚本
# 功能：启动 Dashboard 服务，并可选开启内网穿透
# 使用：
#   ./scripts/start-with-access.sh              # 仅局域网访问（同 WiFi 下手机直接用）
#   ./scripts/start-with-access.sh --tunnel lt  # 加 localtunnel 公网穿透
#   ./scripts/start-with-access.sh --tunnel cf  # 加 Cloudflare 公网穿透
# ============================================================

set -euo pipefail

PORT=5000
TUNNEL_MODE="none"  # none | lt | cf
MY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$MY_DIR/.." && pwd)"

# --- 参数解析 ---
while [[ $# -gt 0 ]]; do
  case "$1" in
    --port)    PORT="$2";     shift 2 ;;
    --tunnel)
      case "$2" in
        lt|cf) TUNNEL_MODE="$2" ;;
        *) echo "[x] --tunnel 参数只能是 lt(localtunnel) 或 cf(cloudflare)"; exit 1 ;;
      esac
      shift 2 ;;
    -h|--help)
      cat <<'EOF'
用法: ./scripts/start-with-access.sh [选项]

选项:
  --port <端口>       指定服务端口，默认 5000
  --tunnel lt|cf      开启公网穿透:
                        lt = localtunnel (需 Node.js, 一条命令免注册)
                        cf = Cloudflare Tunnel (更快更稳, 首次自动下载)
  -h, --help          显示帮助

示例:
  ./scripts/start-with-access.sh              # 仅局域网访问
  ./scripts/start-with-access.sh --tunnel lt  # 局域网 + localtunnel 公网
EOF
      exit 0 ;;
    *) echo "[x] 未知参数: $1"; exit 1 ;;
  esac
done

# --- 辅助函数 ---
info()    { echo -e "\033[36m[i]\033[0m $*"; }
success() { echo -e "\033[32m[✔]\033[0m $*"; }
warn()    { echo -e "\033[33m[!]\033[0m $*"; }
error()   { echo -e "\033[31m[x]\033[0m $*"; }

get_lan_ip() {
  # 尝试多平台拿第一个非 127 的内网 IP
  local ip=""
  if command -v ip >/dev/null 2>&1; then
    ip=$(ip -4 addr show scope global 2>/dev/null | grep -oP 'inet \K[0-9.]+' | head -1)
  fi
  if [[ -z "$ip" ]] && command -v ifconfig >/dev/null 2>&1; then
    ip=$(ifconfig 2>/dev/null | grep -oE 'inet (192\.168|10\.|172\.(1[6-9]|2[0-9]|3[01]))\.[0-9.]+' | awk '{print $2}' | head -1)
  fi
  if [[ -z "$ip" ]] && command -v hostname >/dev/null 2>&1; then
    ip=$(hostname -I 2>/dev/null | awk '{print $1}')
  fi
  echo "${ip:-[未获取到, 请手动查看]}"
}

# --- 0. 端口占用检查 ---
if command -v lsof >/dev/null 2>&1; then
  if lsof -i :"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
    warn "端口 $PORT 已被占用, 正在尝试关闭旧进程..."
    lsof -i :"$PORT" -sTCP:LISTEN -t | xargs -r kill 2>/dev/null || true
    sleep 1
  fi
fi

# --- 1. 启动 Magic Server ---
info "正在启动 Magic Agent Dashboard (端口: $PORT) ..."

# 优先用系统里的 magic 命令; 没有就尝试在项目里 go run
if command -v magic >/dev/null 2>&1; then
  SERVER_CMD="magic server --port $PORT"
elif command -v go >/dev/null 2>&1 && [[ -d "$ROOT_DIR/cmd/magic" ]]; then
  SERVER_CMD="cd $ROOT_DIR && go run ./cmd/magic server --port $PORT"
else
  error "找不到 magic 命令, 也没找到 Go 编译环境."
  error "请先执行: go install github.com/magicwubiao/go-magic/cmd/magic@latest"
  exit 1
fi

# 后台启动 server, 写 PID 和日志
LOG_FILE="$ROOT_DIR/.magic-server.log"
rm -f "$LOG_FILE"
# shellcheck disable=SC2086
bash -c "$SERVER_CMD" > "$LOG_FILE" 2>&1 &
SERVER_PID=$!
echo "$SERVER_PID" > "$ROOT_DIR/.magic-server.pid"

# 等待服务就绪
info "等待服务启动..."
READY=0
for _ in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:$PORT/" >/dev/null 2>&1 || \
     curl -sf "http://127.0.0.1:$PORT/api/system/health" >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 1
done

if [[ $READY -ne 1 ]]; then
  error "服务启动超时, 请查看日志: $LOG_FILE"
  kill "$SERVER_PID" 2>/dev/null || true
  exit 1
fi
success "服务已启动!  PID=$SERVER_PID"

# --- 2. 打印局域网访问地址 ---
LAN_IP=$(get_lan_ip)
echo
echo "┌───────────────────────────────────────────────────────┐"
echo "│  📱 【同一 WiFi 下手机直接访问】                        │"
echo "│                                                       │"
echo "│     电脑端 (本机):   http://localhost:$PORT            │"
echo "│     手机端 (局域网):  http://$LAN_IP:$PORT             │"
echo "└───────────────────────────────────────────────────────┘"
echo

# --- 3. 可选: 公网穿透 ---
cleanup() {
  echo
  info "正在停止服务..."
  [[ -n "${TUNNEL_PID:-}" ]] && kill "$TUNNEL_PID" 2>/dev/null || true
  [[ -n "$SERVER_PID"     ]] && kill "$SERVER_PID"  2>/dev/null || true
  success "已停止所有服务."
  exit 0
}
trap cleanup INT TERM

if [[ "$TUNNEL_MODE" != "none" ]]; then
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  info "开启公网穿透模式: $TUNNEL_MODE (手机流量/任意WiFi均可访问)"
  echo

  if [[ "$TUNNEL_MODE" == "lt" ]]; then
    # localtunnel: 依赖 npx
    if ! command -v npx >/dev/null 2>&1; then
      error "未检测到 Node.js/npx, 无法使用 localtunnel."
      error "请先装 Node.js: https://nodejs.org  或改用 --tunnel cf"
      kill "$SERVER_PID" 2>/dev/null
      exit 1
    fi
    info "正在启动 localtunnel (首次会自动下载, 请稍候)..."
    TUNNEL_LOG="$ROOT_DIR/.localtunnel.log"
    rm -f "$TUNNEL_LOG"
    (
      # 用子进程跑, 输出日志, 同时监听 URL 并打印给用户
      npx --yes localtunnel --port "$PORT" > "$TUNNEL_LOG" 2>&1
    ) &
    TUNNEL_PID=$!

    # 等 URL 出现
    TUNNEL_URL=""
    for _ in $(seq 1 30); do
      TUNNEL_URL=$(grep -oE 'https://[a-zA-Z0-9-]+\.loca\.lt' "$TUNNEL_LOG" 2>/dev/null | head -1 || true)
      [[ -n "$TUNNEL_URL" ]] && break
      sleep 1
    done

    if [[ -n "$TUNNEL_URL" ]]; then
      success "公网穿透已建立!"
      echo
      echo "┌───────────────────────────────────────────────────────┐"
      echo "│  🌐 【任何网络手机浏览器打开】                          │"
      echo "│                                                       │"
      echo "│     $TUNNEL_URL"
      echo "│                                                       │"
      echo "│  ⚠️  首次打开页面请点击「Click to Continue」继续        │"
      echo "└───────────────────────────────────────────────────────┘"
    else
      warn "未能解析 localtunnel URL, 可手动查看日志: $TUNNEL_LOG"
    fi

  elif [[ "$TUNNEL_MODE" == "cf" ]]; then
    # Cloudflare Quick Tunnel
    CF_BIN="$ROOT_DIR/.bin/cloudflared"
    mkdir -p "$(dirname "$CF_BIN")"
    if [[ ! -x "$CF_BIN" ]]; then
      info "首次使用 Cloudflare Tunnel, 正在下载 cloudflared..."
      OS=$(uname -s | tr '[:upper:]' '[:lower:]')
      ARCH=$(uname -m)
      case "$ARCH" in
        x86_64)  CF_ARCH="amd64" ;;
        aarch64|arm64) CF_ARCH="arm64" ;;
        *) CF_ARCH="amd64" ;;
      esac
      URL="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-${OS}-${CF_ARCH}"
      if curl -fsSL "$URL" -o "$CF_BIN" 2>/dev/null; then
        chmod +x "$CF_BIN"
      else
        error "下载 cloudflared 失败, 请检查网络或手动下载放到: $CF_BIN"
        kill "$SERVER_PID" 2>/dev/null
        exit 1
      fi
    fi

    TUNNEL_LOG="$ROOT_DIR/.cloudflared.log"
    rm -f "$TUNNEL_LOG"
    "$CF_BIN" tunnel --url "http://127.0.0.1:$PORT" --no-autoupdate > "$TUNNEL_LOG" 2>&1 &
    TUNNEL_PID=$!

    TUNNEL_URL=""
    for _ in $(seq 1 40); do
      TUNNEL_URL=$(grep -oE 'https://[a-zA-Z0-9-]+\.trycloudflare\.com' "$TUNNEL_LOG" 2>/dev/null | head -1 || true)
      [[ -n "$TUNNEL_URL" ]] && break
      sleep 1
    done

    if [[ -n "$TUNNEL_URL" ]]; then
      success "公网穿透已建立!"
      echo
      echo "┌───────────────────────────────────────────────────────┐"
      echo "│  🌐 【任何网络手机浏览器打开】                          │"
      echo "│                                                       │"
      echo "│     $TUNNEL_URL"
      echo "│                                                       │"
      echo "│  ✨ 速度快, 无额外验证页, 即开即用                     │"
      echo "└───────────────────────────────────────────────────────┘"
    else
      warn "未能解析 Cloudflare URL, 可手动查看日志: $TUNNEL_LOG"
      cat "$TUNNEL_LOG" 2>/dev/null | tail -20
    fi
  fi

  echo
  info "按 Ctrl+C 停止所有服务 (服务端 + 穿透)"
  # 挂住进程等 Ctrl+C
  wait "$TUNNEL_PID" 2>/dev/null || wait
else
  echo
  info "仅局域网模式已就绪. 按 Ctrl+C 停止服务."
  wait "$SERVER_PID" 2>/dev/null || wait
fi
