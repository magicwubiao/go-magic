#!/usr/bin/env bash
# ============================================================
# 一键安装 go-magic 为 systemd 服务 (开机自启 + 自动重启)
# 用法: sudo ./deploy/local/systemd/install.sh [用户名]
#   不传用户名则用当前登录用户
# ============================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
SERVICE_SRC="$SCRIPT_DIR/magic.service"
SERVICE_DST="/etc/systemd/system/go-magic@.service"   # @ = 模板实例, 允许指定用户

# ---------- 检查 root ----------
if [[ $EUID -ne 0 ]]; then
  echo "[x] 请用 sudo 运行此脚本:  sudo $0 [用户名]" >&2
  exit 1
fi

# ---------- 确定运行用户 ----------
RUN_USER="${1:-$SUDO_USER}"
if [[ -z "$RUN_USER" || "$RUN_USER" == "root" ]]; then
  echo "[x] 必须指定一个非 root 用户作为运行身份" >&2
  echo "    用法: sudo $0 <你的用户名>" >&2
  exit 1
fi
RUN_HOME=$(eval echo "~$RUN_USER")
echo "[i] 运行身份: $RUN_USER (HOME=$RUN_HOME)"

# ---------- 检查 magic 命令 ----------
if ! sudo -u "$RUN_USER" bash -c 'command -v magic >/dev/null 2>&1'; then
  echo "[!] 用户 $RUN_USER 的 PATH 里找不到 magic 命令"
  echo "    请先安装: go install github.com/magicwubiao/go-magic/cmd/magic@latest"
  echo "    或者在项目根目录执行: go build -o /usr/local/bin/magic ./cmd/magic"
  read -r -p "继续吗? (y/N) " ans
  [[ "${ans,,}" != "y" ]] && exit 1
fi

# ---------- 初始化目录 ----------
sudo -u "$RUN_USER" mkdir -p "$RUN_HOME/.magic"
echo "[i] 配置目录: $RUN_HOME/.magic"
if [[ ! -f "$RUN_HOME/.magic/config.json" ]]; then
  echo "[!] 未找到 $RUN_HOME/.magic/config.json, 首次使用请运行:  magic setup"
fi

# ---------- 安装模板 service ----------
install -m 0644 "$SERVICE_SRC" "$SERVICE_DST"
systemctl daemon-reload
echo "[i] 已安装模板服务: $SERVICE_DST"

# ---------- 启用 & 启动实例 ----------
INSTANCE="go-magic@${RUN_USER}"
systemctl enable --now "$INSTANCE"
sleep 2

# ---------- 验证 ----------
if systemctl is-active --quiet "$INSTANCE"; then
  echo "======================================================================"
  echo "[✔] 服务启动成功!  ($INSTANCE)"
  echo
  echo "    本机访问:   http://localhost:5000"
  echo "    局域网访问: http://$(hostname -I | awk '{print $1}'):5000"
  echo
  echo "    常用命令:"
  echo "      查看状态:  systemctl status $INSTANCE"
  echo "      实时日志:  journalctl -u $INSTANCE -f"
  echo "      重启服务:  sudo systemctl restart $INSTANCE"
  echo "      停止开机:  sudo systemctl disable --now $INSTANCE"
  echo "======================================================================"
else
  echo "[x] 服务启动失败, 请查看日志: journalctl -u $INSTANCE -n 50"
  systemctl status "$INSTANCE" --no-pager || true
  exit 1
fi
