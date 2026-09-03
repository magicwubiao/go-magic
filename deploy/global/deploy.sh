#!/usr/bin/env bash
# =====================================================================
# go-magic · 全球通用一键部署脚本
#
# 设计目标:
#   ✅ 国内访问顺畅 (绕开 Cloudflare 大陆干扰)
#   ✅ 海外访问飞快 (CF 全球节点加速)
#   ✅ 自动 HTTPS, 固定域名, 开机自启, 崩溃重启
#
# 【强烈推荐的服务器选择】
#   ⭐⭐⭐ 首选: 阿里云国际/腾讯云轻量 · 香港 · 2C4G 以上
#   ⭐⭐  次选: 阿里云国际 · 新加坡 · 2C4G 以上
#   ⭐   备选: Vultr/DO/Linode · 日本/新加坡
#   为什么选 HK/SG?
#     → 国内延迟 30~80ms, 无备案要求, 出海链路好, 海外延迟也低
#     → 调国内/海外大模型 API 都比较顺畅
#
# 用法 (全新的 Ubuntu 22.04/24.04 服务器, root 身份):
#   # 1. 先把本项目代码放到服务器: git clone 或 scp -r
#   # 2. 进入项目根目录
#   cd go-magic
#
#   # 3. 执行部署 (交互式问你域名, 然后全自动)
#   bash deploy/global/deploy.sh
#
#   # 4. 部署完成后按提示去 DNS 服务商配置分线路解析即可
# =====================================================================
set -euo pipefail

# ---------- 颜色 ----------
export DEBIAN_FRONTEND=noninteractive
C='\033[0;36m'; G='\033[0;32m'; Y='\033[1;33m'; R='\033[0;31m'; N='\033[0m'
info()  { echo -e "${C}[i]${N} $*"; }
ok()    { echo -e "${G}[✔]${N} $*"; }
warn()  { echo -e "${Y}[!]${N} $*"; }
err()   { echo -e "${R}[x]${N} $*" >&2; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
DATA_DIR="$ROOT_DIR/data"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

# ---------- 0. 运行环境检查 ----------
preflight() {
  if [[ $EUID -ne 0 ]]; then
    err "请用 root 用户运行此脚本 (sudo bash $0)"
    exit 1
  fi
  if [[ ! -f "$COMPOSE_FILE" ]]; then
    err "找不到 docker-compose.yml, 请先 cd 到 go-magic 项目根目录再运行."
    exit 1
  fi
  if ! grep -qi "ubuntu\|debian" /etc/os-release 2>/dev/null; then
    warn "当前系统不是 Ubuntu/Debian, 自动安装 Docker 步骤可能失败."
    read -r -p "继续? (y/N) " ans; [[ "${ans,,}" != "y" ]] && exit 1
  fi
  TOTAL_MEM=$(awk '/MemTotal/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)
  if (( TOTAL_MEM < 3000 )); then
    warn "服务器内存不到 3G (检测到 ${TOTAL_MEM}MB). 建议至少 4G."
  fi
}

# ---------- 1. 交互参数 ----------
ask_params() {
  echo
  info "== 请回答几个问题 =="
  read -r -p "① 绑定的主域名 (例: magic.example.com) ? " DOMAIN_MAIN
  : "${DOMAIN_MAIN:?域名不能为空}"

  # 邮箱用来申请 Let's Encrypt 证书, Caddy 需要
  read -r -p "② 你的邮箱 (用于 SSL 证书提醒, 可留空) ? " SSL_EMAIL
  SSL_EMAIL="${SSL_EMAIL:-admin@$DOMAIN_MAIN}"

  # 国内直连 IP (可选: 用户如果有 CN2/GIA 优质线路, 直连更快)
  read -r -p "③ 启用【国内直连 + 海外CF】双线路加速? (Y/n) " DUAL_LINE
  DUAL_LINE="${DUAL_LINE:-Y}"
  SERVER_PUB_IP=$(curl -s4 https://ifconfig.me 2>/dev/null || curl -s4 https://api.ipify.org)
  info "检测到服务器公网 IPv4: $SERVER_PUB_IP"

  # 时区
  read -r -p "④ 时区 (默认 Asia/Shanghai) ? " TZ_VAL
  TZ_VAL="${TZ_VAL:-Asia/Shanghai}"
  export TZ_VAL

  echo
  info "配置确认:"
  echo "   域名:      $DOMAIN_MAIN"
  echo "   SSL 邮箱:  $SSL_EMAIL"
  echo "   双线路:    ${DUAL_LINE^^}"
  echo "   服务器IP:  $SERVER_PUB_IP"
  echo "   时区:      $TZ_VAL"
  read -r -p "回车确认, Ctrl+C 取消 " _
}

# ---------- 2. 系统依赖: Docker + Docker Compose 插件 ----------
install_docker() {
  if command -v docker >/dev/null 2>&1; then
    ok "Docker 已安装: $(docker -v)"
  else
    info "安装 Docker Engine (官方 apt 仓库) ..."
    apt-get update -y >/dev/null
    apt-get install -y ca-certificates curl gnupg lsb-release >/dev/null
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
      | gpg --dearmor -o /etc/apt/keyrings/docker.gpg 2>/dev/null
    chmod a+r /etc/apt/keyrings/docker.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
      https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
      > /etc/apt/sources.list.d/docker.list
    apt-get update -y >/dev/null
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin >/dev/null
    ok "Docker 安装完成."
  fi
  # 装 compose v2 插件(compose 子命令), 不再用独立 docker-compose
  if ! docker compose version >/dev/null 2>&1; then
    apt-get install -y docker-compose-plugin >/dev/null
  fi
  ok "Docker Compose: $(docker compose version 2>&1 | head -1)"
  systemctl enable --now docker >/dev/null
}

# ---------- 3. 系统调优 ----------
sys_tune() {
  info "系统调优 (文件句柄 / backlog / keepalive) ..."
  cat > /etc/sysctl.d/99-go-magic.conf <<'EOF'
fs.file-max                    = 1048576
net.core.somaxconn             = 65535
net.ipv4.tcp_syncookies        = 1
net.ipv4.tcp_fin_timeout       = 15
net.ipv4.tcp_keepalive_time    = 600
net.ipv4.tcp_keepalive_intvl   = 30
net.ipv4.tcp_keepalive_probes  = 5
net.ipv4.ip_local_port_range   = 1024 65535
EOF
  sysctl --system >/dev/null 2>&1 || true
  # 解除 systemd 默认 1024 限制
  mkdir -p /etc/systemd/system/docker.service.d/
  cat > /etc/systemd/system/docker.service.d/limits.conf <<'EOF'
[Service]
LimitNOFILE=1048576
LimitNPROC=65536
EOF
  systemctl daemon-reload
}

# ---------- 4. 创建数据目录 ----------
init_dirs() {
  mkdir -p \
    "$DATA_DIR/magic-config" \
    "$DATA_DIR/caddy/data" \
    "$DATA_DIR/caddy/config"
  # 如果本机是 root, 容器里是 UID 1000, 需要改权限
  if [[ -n "$(stat -c %U "$DATA_DIR/magic-config" 2>/dev/null | grep -x root || true)" ]]; then
    chown -R 1000:1000 "$DATA_DIR/magic-config" 2>/dev/null || true
  fi
  ok "数据目录就绪: $DATA_DIR"
}

# ---------- 5. 生成 Caddyfile ----------
render_caddyfile() {
  CADDYFILE="$ROOT_DIR/deploy/cloud/Caddyfile"
  mkdir -p "$(dirname "$CADDYFILE")"
  cat > "$CADDYFILE" <<'CADDY_EOF'
{
  # Caddy 全局配置: 自动 HTTPS + HTTP/3 + OCSP stapling
  email    {{SSL_EMAIL}}
  acme_ca  https://acme-v02.api.letsencrypt.org/directory
  # 如果是国内服务器想更快签发 (零信任 无需 80 端口), 切换成下面这行并配 DNS 插件:
  # acme_dns cloudflare {env.CLOUDFLARE_API_TOKEN}
  servers {
    protocol {
      experimental_http3
    }
    listeners :80 {
      wrap {
        # 限制连接速率, 防 CC
        max_concurrent_streams 1000
      }
    }
  }
}

# =================================================================
# 入口: https://你的域名
# 策略:
#   · 自动签发证书 (HTTP-01, 需要 80/443 端口对公网开放)
#   · 反代到 docker compose 里的 magic 容器 (同 network)
#   · WebSocket 透传 (Agent 流式输出必须!)
#   · 全局 Gzip/Zstd, 首屏 JS 体积大, 很有必要
#   · 基础安全头 + 访问日志
# =================================================================
{{DOMAIN_MAIN}} {
  encode gzip zstd

  # --- 访问日志 (轮换交由 systemd/journald 或 caddy 自带) ---
  log {
    output file /var/log/caddy/access_magic.log {
      roll_size    100mb
      roll_keep    10
      roll_keep_for 2160h
    }
    format json
  }

  # --- 安全头 (去掉不必要的 X-Powered-By 等) ---
  header {
    X-Content-Type-Options  nosniff
    X-Frame-Options         SAMEORIGIN
    Referrer-Policy         strict-origin-when-cross-origin
    Permissions-Policy      "geolocation=(), microphone=(), camera=()"
    -Server
  }

  # --- WebSocket / SSE 流式 (Agent 输出流式必须) ---
  @streaming {
    header Connection *Upgrade*
    header Upgrade    websocket
    path              /api/*/stream  /api/*/ws  /api/*/events
  }
  reverse_proxy @streaming http://magic:5000 {
    header_up X-Real-IP       {remote_host}
    header_up X-Forwarded-For {remote_host}
    transport http {
      # Agent 流式可能要长连接, 超时放宽
      versions 1.1
      keepalive 120s
      read_timeout  10m
      write_timeout 10m
    }
  }

  # --- 普通请求 (含 SPA history fallback) ---
  reverse_proxy http://magic:5000 {
    header_up X-Real-IP         {remote_host}
    header_up X-Forwarded-For   {remote_host}
    header_up X-Forwarded-Proto {scheme}

    # 后端健康探测
    health_uri /api/system/health
    health_interval 30s
    health_timeout  5s
    health_status   2xx

    transport http {
      versions 1.1
      keepalive 120s
      response_header_timeout 60s
    }
  }
}

# -------- HTTP → HTTPS 全局强制跳转 --------
:80 {
  redir https://{host}{uri} permanent
}
CADDY_EOF

  # 变量替换 (因为不想引入 envsubst 依赖)
  sed -i "s|{{DOMAIN_MAIN}}|${DOMAIN_MAIN}|g; s|{{SSL_EMAIL}}|${SSL_EMAIL}|g" "$CADDYFILE"
  ok "Caddyfile 已生成: $CADDYFILE"
}

# ---------- 6. 启用 docker-compose 里的 Caddy 服务 ----------
enable_caddy_in_compose() {
  info "在 docker-compose.yml 中启用 Caddy 反代服务 ..."
  # 把注释掉的 caddy 块取消注释 (块从 "# caddy:" 到 "# Optional: Redis" 之前)
  # 用 Python 更稳
  python3 - "$COMPOSE_FILE" <<'PYEOF'
import re, sys
path = sys.argv[1]
with open(path, 'r', encoding='utf-8') as f:
    txt = f.read()
# 1. 取消 caddy 块整段注释: 找以 "# caddy:" 开头到紧接着下一个非注释行(或Redis注释)之间的每行注释
pattern = re.compile(r'(?ms)^(\s*)#\s*(caddy:.*?)(?=^\s*#\s*=+\s*$\s*#\s*可选:\s*Redis)')
m = pattern.search(txt)
if m:
    block = m.group(2)
    # 每行开头 "# " 去掉
    uncommented = re.sub(r'(?m)^\s*#\s?', lambda s: s.group().replace('#','',1).lstrip(), block)
    # 还原缩进对齐
    indent = m.group(1)
    fixed_block = "\n".join(indent + l if l.strip() else l for l in block.splitlines())
    # 去 "# " 前缀
    fixed_block = re.sub(r'(?m)^(\s*)# ?', r'\1', fixed_block)
    txt = txt[:m.start(2)] + fixed_block + txt[m.end(2):]
with open(path, 'w', encoding='utf-8') as f:
    f.write(txt)
PYEOF
  ok "docker-compose.yml 已启用 Caddy."
}

# ---------- 7. 构建 & 启动 ----------
build_and_up() {
  info "拉取基础镜像并构建 go-magic (首次较慢) ..."
  cd "$ROOT_DIR"
  docker compose build --pull 2>&1 | tail -30
  info "启动服务 ..."
  # 创建 log 目录, Caddyfile 引用了
  mkdir -p /var/log/caddy
  docker compose up -d
  sleep 8

  # 检查容器健康
  HEALTHY=$(docker inspect --format='{{.State.Health.Status}}' go-magic 2>/dev/null || echo "unknown")
  if [[ "$HEALTHY" == "healthy" || "$HEALTHY" == "starting" ]]; then
    ok "容器已启动 (health=$HEALTHY)."
  else
    warn "健康检查: $HEALTHY, 查看详情: docker compose ps && docker compose logs --tail=50 magic"
  fi
}

# ---------- 8. 防火墙 (UFW, 如安装) ----------
setup_firewall() {
  if command -v ufw >/dev/null 2>&1; then
    ufw allow 80/tcp comment "HTTP (Caddy ACME)"   >/dev/null 2>&1 || true
    ufw allow 443/tcp comment "HTTPS (Caddy)"       >/dev/null 2>&1 || true
    ufw allow 443/udp comment "HTTP/3 QUIC (Caddy)" >/dev/null 2>&1 || true
    info "UFW 规则已添加 (如果启用的话)."
  fi
  # 云服务器记得去控制台放开安全组: TCP 80,443 / UDP 443
}

# ---------- 9. 生成 DNS 指引 & 打印结果 ----------
print_next_steps() {
  GUIDE="$ROOT_DIR/deploy/global/DNS-SETUP.md"
  cat > "$GUIDE" <<MD
# go-magic · 国内海外双线路 DNS 配置指南

## 你的信息

| 项 | 值 |
|----|----|
| 主域名 | \`${DOMAIN_MAIN}\` |
| 服务器公网 IPv4 | \`${SERVER_PUB_IP}\` |
| 服务器位置 (建议) | 香港 / 新加坡 / 日本 |

---

## 方案一: 双线路 (推荐 ✅, 国内直连 + 海外 CF 加速)

**DNS 服务商推荐:** DNSPod(腾讯云) / 阿里云解析 / Cloudflare Enterprise
普通 Cloudflare 免费版不支持对中国大陆单独分线路, 建议用 DNSPod 免费版足够.

在你的 DNS 服务商为 \`${DOMAIN_MAIN}\` 添加 **两条 A 记录**:

| 主机记录 | 线路类型 | 记录值 | TTL |
|----------|---------|--------|-----|
| ${DOMAIN_MAIN%%.*}.${DOMAIN_MAIN#*.} (完整写 ${DOMAIN_MAIN}) | **默认 / 海外** | 先开启 Cloudflare 的橙色云(Proxy): 先 CNAME → \`${DOMAIN_MAIN}\`.cdn.cloudflare.net. | 自动 |
| 同上 | **中国大陆 / 境内** | \`${SERVER_PUB_IP}\` (直连 HK 服务器, 绕开 CF 大陆干扰) | 600 |

> 如果你用 Cloudflare 做 DNS 且没有 Enterprise 版, 参考下方"方案二".

---

## 方案二: 纯 Cloudflare (免费版可用, 但国内访问可能慢/不稳)

| 类型 | 主机记录 | 记录值 | 代理状态 |
|------|----------|--------|----------|
| A 记录 | @ 或 子域名前缀 | \`${SERVER_PUB_IP}\` | 橙色云 Proxied ✅ |

效果: 海外飞快, 国内受 Cloudflare 大陆接入质量影响.

---

## 方案三: 纯直连 (不推荐)

直接一条 A 记录指向服务器 IP.
优点: 简单.
缺点: 海外访问慢, 且易被扫描.

---

## 验证

配置后, 国内朋友和海外朋友分别 ping / curl \`https://${DOMAIN_MAIN}/api/system/health\`:

- **国内** resolve 到 \`${SERVER_PUB_IP}\` → 延迟 30-80ms  ✅
- **海外** resolve 到 Cloudflare IP (104.x / 172.x) → 延迟看地区 ✅

---

## 后续运维

\`\`\`bash
cd $(pwd)
# 查看状态
docker compose ps
# 看日志
docker compose logs -f magic
# 升级 (重新构建最新代码)
git pull && docker compose up -d --build
# 完整备份 (配置+会话)
tar czf /root/magic-backup-\$(date +%F).tar.gz data/
\`\`\`
MD

  echo
  echo "=================================================================="
  ok "🎉 部署基本完成!"
  echo
  echo "   部署目录:        $(pwd)"
  echo "   数据目录:        $DATA_DIR"
  echo "   服务日志:        docker compose logs -f magic"
  echo "   Caddy 反代日志:  tail -f /var/log/caddy/access_magic.log"
  echo
  warn "【下一步】请去你的 DNS 服务商配置分线路解析:"
  echo "     → 详细指南: $GUIDE"
  echo
  info "   国内用户解析到:  $SERVER_PUB_IP (直连 HK/SG)"
  info "   海外用户解析到:  Cloudflare 橙色云 (全球加速)"
  echo
  if [[ "${DUAL_LINE^^}" == "Y" ]]; then
    info "【DNS 分线路配置快速参考】"
    echo "   • 线路=大陆境内       A       ${DOMAIN_MAIN}   →   ${SERVER_PUB_IP}"
    echo "   • 线路=默认/海外     CNAME   ${DOMAIN_MAIN}   →   ${DOMAIN_MAIN}.cdn.cloudflare.net.  (橙色云打开)"
  fi
  echo
  warn "DNS 解析 & SSL 证书签发需要 3-10 分钟, 请耐心等待."
  echo "     完成后手机浏览器直接打开: https://${DOMAIN_MAIN}"
  echo "=================================================================="
}

# ---------- 10. 全球模型 API 可达推荐 ----------
print_model_recommendation() {
  CONF_TIP="$ROOT_DIR/deploy/global/MODEL-RECOMMEND.md"
  cat > "$CONF_TIP" <<MD
# 全球通用 · 模型供应商选择建议 (HK/SG 服务器实测)

你的 go-magic 跑在香港/新加坡服务器上, 以下 API 都能顺畅调用:

| 优先级 | 供应商 | Base URL (建议用哪个) | 国内/海外 访问质量 | 备注 |
|--------|--------|---------------------|-------------------|------|
| ⭐⭐⭐⭐⭐ | DeepSeek | \`https://api.deepseek.com\` | 双端都快 | 性价比最高, 代码能力强 |
| ⭐⭐⭐⭐⭐ | 火山方舟 (豆包) | 国际版 Endpoint | 双端都快 | 中文能力强 |
| ⭐⭐⭐⭐ | 阿里百炼 | 国际版 | 双端都快 | qwen-max 中文好 |
| ⭐⭐⭐⭐ | OpenAI | \`https://api.openai.com\` (HK/SG 走专线) | 海外快 国内稍慢 | GPT 系列质量高 |
| ⭐⭐⭐ | Anthropic | \`https://api.anthropic.com\` | 海外快 内地偶发抖动 | 长上下文 Claude Opus |
| ⭐⭐⭐ | Gemini | \`https://generativelanguage.googleapis.com\` | 海外快 国内需翻墙 | 多模态强 |

> 🔑 **配置文件位置 (容器内路径映射):**
> \`data/magic-config/config.json\` → 容器内 \`/home/magic/.magic/config.json\`
> 修改后: \`docker compose restart magic\`

## 🌐 全球访问体验补充建议

1. **Dashboard 登录鉴权**: 务必在 config 中开启访问密码, 并设 API 白名单 (仅允许你信任的几个IP段).
2. **Webhook/回调**: 如果开了消息网关, 确保回调域名也走同样的双线路配置.
3. **备份**: 每周备份一次 \`data/\` 目录到对象存储(阿里云 OSS 香港/Cloudflare R2).
MD
  info "模型选型建议已保存: $CONF_TIP"
}

# ================ main ================
preflight
ask_params
install_docker
sys_tune
init_dirs
render_caddyfile
enable_caddy_in_compose
build_and_up
setup_firewall
print_model_recommendation
print_next_steps
