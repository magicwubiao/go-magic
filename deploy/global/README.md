# go-magic · 国内海外都能用的全球部署方案

> 一句话架构：**服务器放香港/新加坡 + DNS 分线路解析 + Caddy 自动 HTTPS 反代**
>
> - 国内 → 直接连 HK/SG 服务器（延迟 30~80ms，无备案，绕开 Cloudflare 大陆干扰）
> - 海外 → Cloudflare 全球 300+ 节点加速
> - 服务器任何时候只暴露 80/443，Magic 服务不直接对公网

## 0. 一分钟选型（买哪个服务器）

**⭐⭐⭐⭐⭐ 最推荐：腾讯云轻量应用服务器 · 香港 · 2C4G · 约 ¥50/月**
- 国内电信/联通走 CN2 GIA 线路，移动走直连，全国低延迟
- 无需备案，开通即用
- 到海外（美西/欧洲）延迟也不错

**⭐⭐⭐⭐ 备选：阿里云国际 · 新加坡 · 2C4G**
- 比香港稍慢一点，但更稳定
- 适合海外用户比重大的场景

**⚠️ 避坑**：
- ❌ 不要买「阿里云国内/腾讯云国内」——需要 ICP 备案，且海外访问慢
- ❌ 不要买美西服务器——国内延迟 180ms+，聊天打字都卡

## 1. 一键部署

```bash
# 把项目代码传到服务器 (任选其一)
git clone <你的 fork 地址> go-magic
# 或 scp -r ./go-magic root@<服务器IP>:/root/

cd go-magic

# 以 root 执行, 交互式问 3 个问题, 后面全自动
bash deploy/global/deploy.sh
```

部署完成后，脚本会打印下一步 DNS 配置指引到屏幕，同时保存为：

```
deploy/global/DNS-SETUP.md           # DNS 分线路配置步骤
deploy/global/MODEL-RECOMMEND.md     # 全球可用的模型供应商 + Base URL 建议
```

## 2. 部署后的目录结构

```
go-magic/
├── data/                            # 🌟 所有真实数据都在这, 备份就打这个包
│   ├── magic-config/                #   ← config.json, sessions.db (映射到容器 ~/.magic)
│   └── caddy/                       #   ← Caddy 证书, 配置缓存
├── docker-compose.yml               # magic + Caddy, 两个容器
├── deploy/cloud/Caddyfile           # Caddy 反代配置 (HTTPS, 流式, 安全头)
└── deploy/global/
    ├── deploy.sh
    ├── DNS-SETUP.md
    └── MODEL-RECOMMEND.md
```

**备份一条命令：**
```bash
tar czf /root/magic-$(date +%F).tar.gz -C /root/go-magic data/
```

## 3. 常见问题

### Q: 手机浏览器打开 HTTPS 显示证书错误？
A: Let's Encrypt 签发需要 1-3 分钟，且 **80 端口必须对公网开放（用于 HTTP-01 验证）**。检查：
1. 云服务器安全组放开 TCP 80/443、UDP 443
2. `docker compose logs caddy | tail -50` 看签发出错信息

### Q: 国内访问还是卡？
A: 检查国内 DNS 解析是否**真的指向服务器 IP**，而不是 Cloudflare 的橙色云 IP：
```bash
# 国内手机上用 HTTP Custom / 网络调试类 App 测：
curl -v https://你的域名/api/system/health
# 看看 Connected to <IP> 是服务器 HK IP 还是 CF IP
# 如果是 CF IP → DNS 分线路没生效，重新按 DNS-SETUP.md 配置
```

### Q: Agent 流式输出卡住？
A: Caddy 已在配置中为 `/api/*/stream` 等路径放宽 WebSocket 超时到 10 分钟。如果还卡：
1. 调大模型 `timeout`
2. 检查服务器和模型 API 的连通性（DeepSeek/OpenAI 从 HK 连都是 OK 的）

### Q: 如何升级最新代码？
```bash
cd go-magic
git pull
docker compose up -d --build
```
构建完成会自动滚动重启，**不丢数据**（都在 data/ 目录）。

## 4. 架构图

```
          全球用户
             │
     ┌───────┴───────┐
     ▼               ▼
  国内用户         海外用户
  (电信/移动/联通)  (美/欧/东南亚)
     │               │
     │ DNS 境内线    │ DNS 默认线(CF橙色云)
     ▼               ▼
   HK/SG 服务器    Cloudflare 全球 300+ 节点
  （直连，低延迟）    （边缘加速 + WAF + DDoS 防护）
     │               │
     └───────┬───────┘
             │ 反代
             ▼
       Caddy (容器, :80/:443)
             │
             ▼
   go-magic magic:5000 (仅内网, 不公网暴露)
```
