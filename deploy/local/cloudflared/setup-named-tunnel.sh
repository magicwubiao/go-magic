#!/usr/bin/env bash
# ============================================================
# Cloudflare 命名隧道一键搭建 (地址永久固定 + 自己的域名)
#
# 解决「之前 quick tunnel 每次启动地址都变」的痛点:
#   - 创建一次, 永久有效
#   - 绑定自己的域名 (例如 magic.yourdomain.com)
#   - 生成 systemd 服务, 开机自动建隧道
#
# 前置条件:
#   1. 有一个 Cloudflare 账号, 且域名的 NS 已经接入 Cloudflare
#   2. 本机能访问互联网
#
# 用法: sudo ./deploy/local/cloudflared/setup-named-tunnel.sh magic.yourdomain.com
# ============================================================
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "用法: sudo $0 <你的子域名, 例如 magic.example.com>" >&2
  echo "提示: 根域名 example.com 必须已经托管在 Cloudflare." >&2
  exit 1
fi
DOMAIN="$1"
TUNNEL_NAME="go-magic-local"
LOCAL_PORT=5000

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CF_BIN=""
CLOUDFLARED_SERVICE_NAME="cloudflared"

# --------- 颜色 ---------
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()    { echo -e "${GREEN}[i]${NC} $*"; }
warn()    { echo -e "${YELLOW}[!]${NC} $*"; }
err()     { echo -e "${RED}[x]${NC} $*" >&2; }

# --------- 1. 安装 cloudflared ---------
install_cloudflared() {
  info "安装 cloudflared ..."
  if command -v cloudflared >/dev/null 2>&1; then
    CF_BIN=$(command -v cloudflared)
    info "已安装: $($CF_BIN -v 2>&1 | head -1)"
    return
  fi
  if [[ -x /usr/local/bin/cloudflared ]]; then
    CF_BIN=/usr/local/bin/cloudflared
    return
  fi
  # 官方 apt/yum/brew 仓库优先, 否则直下二进制
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)   CF_ARCH=amd64 ;;
    aarch64)  CF_ARCH=arm64 ;;
    armv7l)   CF_ARCH=arm ;;
    *) err "不支持的架构: $ARCH"; exit 1 ;;
  esac
  TMP=$(mktemp -d)
  URL="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-${OS}-${CF_ARCH}"
  info "下载: $URL"
  curl -fsSL "$URL" -o "$TMP/cloudflared"
  install -m 0755 "$TMP/cloudflared" /usr/local/bin/cloudflared
  rm -rf "$TMP"
  CF_BIN=/usr/local/bin/cloudflared
  info "安装完成: $($CF_BIN -v 2>&1 | head -1)"
}

# --------- 2. 登录 Cloudflare ---------
cf_login() {
  # 已经有 cert.pem 就跳过
  if [[ -f /root/.cloudflared/cert.pem ]]; then
    info "已登录 Cloudflare (cert.pem 已存在)."
    return
  fi
  warn "即将打开浏览器, 请登录并选择要授权的 Cloudflare 账号/Zone."
  warn "如果本机无图形界面, 会输出一个 URL, 复制到其他设备浏览器打开也行."
  read -r -p "回车继续... " _
  "$CF_BIN" tunnel login
  if [[ ! -f /root/.cloudflared/cert.pem ]]; then
    err "登录失败, 未找到 cert.pem. 请重试."
    exit 1
  fi
}

# --------- 3. 创建/复用命名隧道 ---------
create_tunnel() {
  info "创建命名隧道: $TUNNEL_NAME"
  # 已存在则复用
  if "$CF_BIN" tunnel list --output json 2>/dev/null \
       | grep -q "\"Name\":\"${TUNNEL_NAME}\""; then
    info "隧道 $TUNNEL_NAME 已存在, 复用."
    return
  fi
  "$CF_BIN" tunnel create "$TUNNEL_NAME"
  info "隧道创建成功."
}

# --------- 4. 绑定域名 DNS ---------
bind_dns() {
  info "绑定域名: $DOMAIN → 隧道 $TUNNEL_NAME"
  # --overwrite-dns 允许覆盖已有记录
  "$CF_BIN" tunnel route dns --overwrite-dns "$TUNNEL_NAME" "$DOMAIN"
  info "DNS 记录已创建 (CNAME -> <UUID>.cfargotunnel.com)."
}

# --------- 5. 写 config.yml ---------
write_config() {
  mkdir -p /root/.cloudflared
  CFG=/root/.cloudflared/config.yml
  TUNNEL_ID=$(
    "$CF_BIN" tunnel list --output json 2>/dev/null \
      | python3 -c "import json,sys; data=json.load(sys.stdin);
          print([t['ID'] for t in data if t['Name']=='$TUNNEL_NAME'][0])" 2>/dev/null
  )
  if [[ -z "${TUNNEL_ID:-}" ]]; then
    err "无法获取隧道 ID, 请检查: cloudflared tunnel list"
    exit 1
  fi
  cat > "$CFG" <<EOF
# ---------- go-magic 专用 Cloudflare 命名隧道 ----------
tunnel: ${TUNNEL_ID}
credentials-file: /root/.cloudflared/${TUNNEL_ID}.json

ingress:
  # 你的 Magic Agent Dashboard
  - hostname: ${DOMAIN}
    service: http://localhost:${LOCAL_PORT}
    originRequest:
      connectTimeout: 30s
      noTLSVerify: false

  # 兜底 (防止 502 Bad Gateway 误报)
  - service: http_status:404
EOF
  info "配置文件已写入: $CFG"
}

# --------- 6. 安装为 systemd 服务 ---------
install_systemd() {
  info "安装 cloudflared 系统服务 (开机自启隧道)."
  # cloudflared 自带 service 子命令, 直接用官方生成器
  "$CF_BIN" service install --config /root/.cloudflared/config.yml 2>/dev/null || true
  systemctl daemon-reload
  systemctl enable --now "$CLOUDFLARED_SERVICE_NAME"
  sleep 2
  if systemctl is-active --quiet "$CLOUDFLARED_SERVICE_NAME"; then
    info "cloudflared 服务运行中."
  else
    warn "cloudflared 服务状态异常, 手动查看: journalctl -u $CLOUDFLARED_SERVICE_NAME -n 50"
    systemctl status "$CLOUDFLARED_SERVICE_NAME" --no-pager || true
  fi
}

# --------- 7. 验证 ---------
verify() {
  echo
  echo "======================================================================"
  info "🎉 搭建完成!"
  echo
  echo "   本地服务:      http://localhost:${LOCAL_PORT}"
  echo "   手机/外网固定: 👉 https://${DOMAIN} 👈 (永久不变)"
  echo
  echo "   常用管理:"
  echo "     查看隧道:   cloudflared tunnel list"
  echo "     查看配置:   cat /root/.cloudflared/config.yml"
  echo "     服务日志:   journalctl -u cloudflared -f"
  echo "     重启隧道:   systemctl restart cloudflared"
  echo "     卸载服务:   cloudflared service uninstall"
  echo
  warn "注意: 如果 1-2 分钟内手机还打不开, 是 DNS 全球同步延迟, 稍等即可."
  echo "======================================================================"
}

# --------- main ---------
install_cloudflared
cf_login
create_tunnel
bind_dns
write_config
install_systemd
verify
