# go-magic

**Magic Agent** -- 高性能、超轻量级的 AI Agent 框架，使用 Go 语言编写。

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8)](https://go.dev)
[![Version](https://img.shields.io/badge/version-v0.5.9-green)](https://github.com/magicwubiao/go-magic/releases)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## 简介

go-magic 是一个功能完整的 AI Agent 框架，后端使用 Go 编写，前端使用 Vue 3 / TypeScript 构建 Web Dashboard。支持 22+ AI Provider，内置 TUI 界面，提供文件操作、代码执行、Web 搜索、浏览器自动化等丰富的工具集。

## 功能特性

### 多 Provider 支持 (22+)

DeepSeek、OpenAI、Anthropic、Gemini、Ollama、vLLM、Groq、硅基流动、智谱GLM、通义千问、文心一言、MiniMax、MiMo、腾讯混元、豆包(火山引擎)、Moonshot (Kimi)、OpenRouter、Together AI、Mistral AI、Cohere、Perplexity，以及任何兼容 OpenAI 的端点。

### 单 Provider 多模型

每个 Provider 可配置多个模型，数组第一个模型为当前活跃模型。切换模型即时生效，无需重启（热加载）。

### TUI 界面

基于 [BubbleTea](https://github.com/charmbracelet/bubbletea) 构建，支持多行输入、Markdown 渲染、流式输出、斜杠命令。

### Web Dashboard

Vue 3 / TypeScript 前端，功能包括：
- 实时聊天对话，支持流式响应
- 会话管理（创建、搜索、恢复）
- Provider 和模型配置，支持热加载
- 技能管理
- Cron 定时任务管理
- 看板（Kanban）
- Token 使用统计，支持预算告警
- 群聊，支持多 Agent 对话

### Coding 模式

专为开发任务设计的模式，放宽权限、更长超时、支持 Python/Node.js 代码执行。

### 工具系统

15+ 内置工具，按工具集组织：

| 工具集 | 工具 |
|--------|------|
| **Web** | web_search, web_fetch |
| **文件** | read_file, write_file, file_edit, list_files, search_in_files |
| **终端** | execute_command, terminal, process |
| **浏览器** | browser_navigate, browser_snapshot, browser_click, browser_type |
| **代码执行** | execute_code (Python, Node.js) |
| **记忆** | memory_store, memory_recall |
| **技能** | skill_list, skill_view, skill_manage |
| **MCP** | mcp_* (来自已连接的 MCP 服务器) |

### 技能系统

自动创建、渐进式加载（L0/L1/L2）。技能从使用模式中学习，也可从 Skills Hub 安装。

### Cortex（认知架构）

完整的 Agent 认知系统：
- **记忆系统**：SOUL.md（人格）、USER.md（用户画像）、快照记忆、FTS 搜索
- **感知层**：输入分析、意图识别、复杂度评估
- **认知层**：规划、决策、基于 LLM 的任务分解
- **执行层**：工具调用和结果处理
- **技能进化 (GEPA)**：从历史模式自动创建新技能

### 消息网关

将 Agent 接入外部平台：

Telegram、Discord、Slack、WeChat（iLink）、WeCom、钉钉、飞书、QQ、LINE、Matrix、Microsoft Teams、Google Chat、Email、SMS。

### Bot 模式

具名 Agent 配置档案，支持持久化会话（设计参考 Hermes Agent 的 Bot Mode）：

- 每个 Bot 是一个独立配置：角色标签、人设提示词、模型/Provider 绑定、专属持久化会话，以及定时例程（routines）。
- Bot 之间可通过 `message_agent` 工具互发消息；回复稍后会以后台通知的形式送达发送方。
- 在任意网关平台上，使用 `/bot <名称> <消息>` 或 `@<tag>` 即可与指定 Bot 对话。

```bash
magic config set bot_mode.enabled true
magic bots create researcher --title "研究助手" --prompt "你负责查找和总结论文。"
magic bots chat researcher "找一下关于 agent 记忆的最新论文"
magic bots routine add researcher daily-digest --schedule "0 9 * * *" --prompt "总结昨天的发现。"
```

跨机器 Peer：通过 HTTP(S) 直接给另一台 go-magic 实例上的 Bot 发消息。

```bash
# 在机器 B（需要已启动 dashboard / gateway，relay 端点在线）
magic config set bot_mode.relay_token "shared-secret"   # 可选，但推荐设置
# 在机器 A
magic peer add lab-b http://192.168.1.20:8642 --token shared-secret
magic peer dm lab-b researcher "报告进展如何？"
# peer 表保存在 <magic_home>/peers.json；本机身份标识在 <magic_home>/instance_id
```

### MCP 协议

连接外部 MCP (Model Context Protocol) 服务器，扩展 Agent 能力。

### 群聊

创建多人 AI Agent 对话群组，每个 Agent 可使用不同的 Provider 和模型。

### 会话管理

基于 SQLite 的持久化存储，支持 FTS5 全文搜索所有会话。

### Token 使用统计

详细追踪 API 消耗：
- Web Dashboard 实时用量面板
- CLI 命令：`magic usage`
- 月度预算与告警阈值
- 按模型、按会话的用量细分

### 敏感信息脱敏

自动对日志和输出中的 API Key、Token、密码等敏感信息进行脱敏处理。

## 快速开始

### 下载 Release

从 [GitHub Releases](https://github.com/magicwubiao/go-magic/releases) 下载最新二进制文件：

```bash
# Linux / macOS
curl -L https://github.com/magicwubiao/go-magic/releases/latest/download/magic-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz | tar xz
chmod +x magic-*
sudo mv magic-* /usr/local/bin/magic
```

### Go Install

```bash
go install github.com/magicwubiao/go-magic/cmd/magic@latest
```

### Docker

镜像以非 root 用户运行；`magic server` 默认端口是 5000，镜像 CMD 已显式改为 8642，因此必须映射 8642 才能从宿主机访问 Web UI：

```bash
# 快速运行（不映射 8642 则宿主机访问不到 Web UI）
docker run -it -p 8642:8642 magicwubiao/go-magic

# Docker Compose
docker compose up -d

# 改了前端后重建，避免旧 dist 被编进镜像
rm -rf internal/server/dist && docker compose build --no-cache
```

网关平台（钉钉 / 飞书 / Discord 等）的 webhook 回调使用**各自独立端口**，只用到的平台才需要在 compose 里放开。完整端口表与排障步骤见 [Docker 部署与排障](#docker-部署与排障)。

### 一键安装 (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
```

### 首次运行

```bash
# 交互式设置向导
magic setup

# 开始聊天
magic chat

# 启动 Web Dashboard
magic server
```

## Docker 部署与排障

镜像为多阶段构建（Node 构建前端 → Go 编译并 `embed` → Alpine 运行时），容器以非 root 用户 `magic`（uid 1000）运行。下面四点覆盖绝大多数部署故障，均已回到源码确认。

### 1. 端口必须对齐（`magic server` 默认 5000）

| 端口 | 用途 | 说明 |
|------|------|------|
| 8642 | API / Web UI | 镜像 CMD 已写死 `server --port 8642` |
| 8643 | 预留 | 当前无服务监听，仅登记在 CORS 允许源里 |
| 8091 / 8092 | 钉钉 / 飞书 | webhook 回调 |
| 8084 / 8085 | Discord / Slack | webhook 回调 |
| 8087 / 8088 | LINE / Teams | webhook 回调 |
| 8089 / 8090 | Google Chat / SMS | webhook 回调 |
| 8080 / 8081 | 网关自身回环端口（API / 健康检查） | 不对容器外暴露 |

这些端口在源码中是硬编码常量，没有配置项或环境变量可以覆盖：

- `cmd/magic/server.go` — `--port` 默认值 `5000`
- `internal/gateway/` 下 `dingtalk.go`、`feishu.go`、`discord.go`、`slack.go`、`line.go`、`teams.go`、`googlechat.go`、`sms.go` — 各平台 `SetCallbackPort(...)`
- `internal/gateway/gateway.go` — `DefaultAPIPort = 8080`、`DefaultHealthPort = 8081`

因此：覆盖 `command:` 时必须显式带上 `--port 8642`，否则容器内监听 5000、映射 8642 直接失效；需要哪个平台的回调，就必须映射对应端口，否则平台永远收不到通知。

```bash
docker compose port magic 8642            # 宿主机映射情况
docker exec go-magic netstat -tlnp        # 容器内真实监听端口
curl -sf http://localhost:8642/api/health
```

### 2. healthcheck 与 `/api/health`

镜像与 compose 均使用 `curl -sf http://localhost:8642/api/health`。该路由由普通 `mux.HandleFunc` 注册，handler 完全不检查 `r.Method`：

```go
// internal/server/server.go
mux.HandleFunc("/api/health", withCORS(s.handleHealth))

// internal/server/health.go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    jsonResponse(w, map[string]string{"status": "healthy"})
}
```

GET / HEAD 都会返回 200，所以「`/api/health` 不支持 HEAD」不能作为误报 unhealthy 的原因。真遇到 unhealthy 时，请从启动时序（首次初始化 `~/.magic`）或探测工具自身差异入手。compose 的 `start_period` 为 10s、镜像内为 5s，冷启动慢可调大。

```bash
docker inspect --format '{{json .State.Health}}' go-magic | jq
docker logs --tail 100 go-magic
```

### 3. 旧的前端产物会被编进镜像

前端输出目录直接指向 Go 的嵌入目录，Go 侧在编译期打包：

```ts
// web/vite.config.ts
outDir: '../internal/server/dist',
```

```go
// internal/server/server.go
//go:embed dist
```

因此本地残留的旧 `internal/server/dist` 会被静默编入二进制。`Dockerfile` 通过 `COPY --from=web-builder` 的先后顺序规避这一点，`.dockerignore` 也显式列出了 `internal/server/dist/` 与 `web/dist/`（裸 `dist/` 只匹配上下文根目录，匹配不到这两处）。

改了前端却仍看到旧页面时：

```bash
rm -rf internal/server/dist
docker compose build --no-cache
```

### 4. 数据卷权限

容器以 uid 1000 运行，配置卷 `magic-config` 挂载到 `/home/magic/.magic`；卷权限不匹配时配置写不进去：

```bash
docker exec -u 0 go-magic chown -R 1000:1000 /home/magic/.magic
```

> `docker ps` 报 `permission denied ... /var/run/docker.sock` 属于宿主机权限问题：把当前用户加入 `docker` 组后重新登录即可 —— `sudo usermod -aG docker $USER`。

## CLI 命令

| 命令 | 描述 |
|------|------|
| `magic chat` | 启动交互式 TUI 聊天 |
| `magic server` | 启动 Web Dashboard 服务器 |
| `magic usage [-d N]` | 显示 Token 使用统计 |
| `magic stats` | 显示系统统计信息 |
| `magic config` | 管理配置 |
| `magic skills` | 管理技能 |
| `magic cron` | 管理定时任务 |
| `magic kanban` | 看板管理 |
| `magic gateway` | 启动消息网关 |
| `magic mcp` | 管理 MCP 服务器 |
| `magic logs` | 查看日志 |
| `magic doctor` | 诊断问题 |
| `magic backup` | 备份数据 |

## TUI 斜杠命令

| 命令 | 别名 | 描述 |
|------|------|------|
| `/help [command]` | `/?` | 显示帮助 |
| `/commands [category]` | `/cmds` | 列出所有命令 |
| `/new` | `/reset` | 开始新对话 |
| `/clear` | | 清除聊天历史 |
| `/compress` | | 压缩上下文窗口 |
| `/retry` | | 重试上一次响应 |
| `/undo` | | 撤销上一次操作 |
| `/export [format]` | `/save` | 导出对话 |
| `/model [provider:model]` | `/m` | 切换 AI 模型 |
| `/mode [chat coding]` | | 切换 Agent 模式 |
| `/personality [name]` | `/persona`, `/tone` | 设置 Agent 人格 |
| `/tools [category]` | | 列出可用工具 |
| `/skills [name]` | `/skill` | 列出可用技能 |
| `/status` | | 显示系统状态 |
| `/version` | `/ver` | 显示版本 |
| `/usage` | | 显示 Token 使用量 |
| `/sessions [list search]` | `/session` | 列出会话 |
| `/sethome [session_id]` | | 设置消息网关的主会话 |
| `/context [add removel ist]` | `/ctx` | 管理上下文文件 |
| `/stop` | `/cancel` | 停止当前操作 |

## Coding 模式

切换到 Coding 模式进行开发任务：

```
/mode coding
```

| 功能 | Chat 模式 | Coding 模式 |
|------|-----------|-------------|
| 文件写入权限 | 受限 | 放宽 |
| 命令执行超时 | 30秒 | 300秒 |
| 代码执行 (Python/Node) | 禁用 | 启用 |
| Shell 访问 | 有限 | 完全 |
| 工具自动批准 | 否 | 是 |

使用 `/mode chat` 切换回来。

### 全局配置默认模式

在配置文件中设置默认启动模式：

```json
{
  "chat_mode": "coding"
}
```

可选值：`"chat"`（默认）或 `"coding"`。

## Web Dashboard

启动 Web 服务器：

```bash
magic server
```

然后在浏览器中打开 `http://localhost:5000`。

Dashboard 由首次访问时设置的登录密码保护。**登录凭据只走请求头**（`Authorization: Bearer <token>`），永不进 URL——浏览器发不出请求头的那几类请求（`<img src>`、`<a href>`、`EventSource`、iframe 子资源）改为先用 `POST /api/fs/sign` 换一张**范围受限、带硬性过期时间的签名票据**，把票据放进 URL。旧的 `?token=<登录凭据>` 写法已删除。凭据分层、票据作用域与有效期、错误码，以及外部脚本的迁移对照表见 [docs/AUTH.zh-CN.md](docs/AUTH.zh-CN.md)。

功能：
- 实时聊天对话，支持流式响应
- 会话管理（创建、搜索、恢复）
- Provider 和模型配置，支持热加载
- 技能管理
- Cron 定时任务管理
- 看板（Kanban）
- Token 使用统计，含预算追踪
- 群聊，支持多 Agent 对话

## Token 使用追踪

追踪 API 消耗：

```bash
# 显示今日用量
magic usage

# 显示最近7天
magic usage -d 7

# 显示最近30天
magic usage -d 30
```

在 Web Dashboard 中，访问 Usage 页面可查看：
- 今日统计
- 每日趋势图表
- 月度明细
- 预算进度条与告警
- Top 模型和会话

## 配置

创建或编辑 `~/.magic/config.json`：

```json
{
  "profile": "default",
  "provider": "deepseek",
  "model": "deepseek-chat",
  "providers": {
    "deepseek": {
      "api_key": "your-deepseek-api-key",
      "models": ["deepseek-chat", "deepseek-coder"]
    },
    "openai": {
      "api_key": "your-openai-api-key",
      "base_url": "https://api.openai.com/v1",
      "models": ["gpt-5.6", "gpt-5.6-luna"]
    },
    "anthropic": {
      "api_key": "your-anthropic-api-key",
      "models": ["claude-sonnet-5", "claude-haiku-4-5"]
    },
    "ollama": {
      "base_url": "http://localhost:11434",
      "models": ["llama3", "codellama"]
    }
  },
  "tools": {
    "enabled": ["all"],
    "disabled": []
  },
  "gateway": {
    "enabled": false,
    "platforms": {
      "telegram": {
        "token": "your-telegram-bot-token",
        "enabled": false
      },
      "discord": {
        "token": "your-discord-bot-token",
        "enabled": false
      }
    }
  }
}
```

### 多模型配置

每个 Provider 支持多个模型：

```json
"providers": {
  "deepseek": {
    "api_key": "your-api-key",
    "models": ["deepseek-chat", "deepseek-coder", "deepseek-reasoner"]
  }
}
```

- 数组第一个模型为当前活跃模型
- 从 Web Dashboard 可即时切换模型（无需重启）
- TUI 中使用 `/model provider:model` 切换

### 环境变量

| 变量 | 描述 |
|------|------|
| `OPENAI_API_KEY` | OpenAI API 密钥 |
| `DEEPSEEK_API_KEY` | DeepSeek API 密钥 |
| `ANTHROPIC_API_KEY` | Anthropic API 密钥 |
| `GOOGLE_API_KEY` | Google/Gemini API 密钥 |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token |
| `DISCORD_BOT_TOKEN` | Discord Bot Token |
| `GO_MAGIC_HOME` | 配置目录（默认：`~/.magic`） |
| `GO_MAGIC_CORS_ORIGINS` | 逗号分隔的 CORS 允许源（前后端分离部署时使用） |
| `MAGIC_SKILL_DIR` | 额外的技能目录 |
| `MAGIC_SESSION_ID` | 将 CLI 进程绑定到指定会话 ID |

## 从源码构建

### 要求

- Go 1.26+
- Node.js 20+（用于 Web Dashboard）

### 构建当前平台

```bash
make build
```

### 跨平台构建

```bash
# 所有常见平台 (Linux, macOS, Windows)
make build-all

# 所有支持的平台
make build-cross

# 指定平台
make build-linux
make build-macos
make build-windows
```

### 支持的平台

| 操作系统 | 架构 |
|---------|------|
| Linux | amd64, arm64, armv6, riscv64, ppc64le, s390x |
| macOS | amd64, arm64 |
| Windows | amd64, arm64, 386 |
| BSD | freebsd, openbsd, netbsd |

## 下载

从 [GitHub Releases](https://github.com/magicwubiao/go-magic/releases) 下载最新版本。

## 许可证

MIT License - 详见 [LICENSE](LICENSE)。
