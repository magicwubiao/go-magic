# go-magic 使用文档

> Magic Agent —— 用 Go 编写的高性能 AI Agent 框架，自带 TUI、Web Dashboard、Bot 模式与消息网关。
>
> 对应版本：v0.5.9 ｜ 文档基于实际编译产物的命令树整理（共 34 个一级命令、164 个子命令）。

---

## 目录

- [1. 它能做什么](#1-它能做什么)
- [2. 安装](#2-安装)
- [3. 快速开始（5 分钟跑通）](#3-快速开始5-分钟跑通)
- [4. 目录结构与数据存储](#4-目录结构与数据存储)
- [5. 配置详解](#5-配置详解)
- [6. 命令行（CLI）总览](#6-命令行cli总览)
- [7. 核心命令详解](#7-核心命令详解)
- [8. TUI 斜杠命令](#8-tui-斜杠命令)
- [9. Web Dashboard](#9-web-dashboard)
- [10. Bot 模式（具名 Agent）](#10-bot-模式具名-agent)
- [11. 群聊与 Rooms](#11-群聊与-rooms)
- [12. 消息网关（接外部聊天平台）](#12-消息网关接外部聊天平台)
- [13. 技能系统](#13-技能系统)
- [14. 工具与工具集](#14-工具与工具集)
- [15. MCP 与 ACP](#15-mcp-与-acp)
- [16. 定时任务（Cron）](#16-定时任务cron)
- [17. 看板（Kanban）](#17-看板kanban)
- [18. Token 用量与预算](#18-token-用量与预算)
- [19. 隐私脱敏](#19-隐私脱敏)
- [20. 运维：诊断、备份、更新](#20-运维诊断备份更新)
- [21. 常见问题 FAQ](#21-常见问题-faq)
- [22. 已知问题（实测确认）](#22-已知问题实测确认)

---

## 1. 它能做什么

| 能力 | 说明 |
|------|------|
| **多 Provider** | 21 个内置 Provider：openai、anthropic、deepseek、minimax、ollama、dashscope、vllm、zhipu、openrouter、gemini、groq、together、mistral、cohere、perplexity、huoshan、wenxin、moonshot、mimo、hunyuan、longcat；任意 OpenAI 兼容端点用 `custom` 接入 |
| **单 Provider 多模型** | 每个 Provider 可配多个模型，**数组第一项为当前活跃模型**，支持热切换，无需重启 |
| **TUI 聊天** | 基于 BubbleTea，流式输出、Markdown 渲染、多行输入、斜杠命令 |
| **Web Dashboard** | Vue 3 + TypeScript，16 个功能页面，支持流式对话、配置热加载、文件管理、用量面板等 |
| **Bot 模式** | 具名 Agent 档案：独立人设、模型绑定、持久化会话、定时例程，Bot 之间可互发消息 |
| **群聊 / Rooms** | 多个 Bot 在同一房间里多轮发言、互相 @ 拉人 |
| **跨机器 Peer** | 一台机器上的 Bot 可以直接给另一台机器上的 Bot 发消息 |
| **消息网关** | 12 个平台：telegram、discord、slack、whatsapp、whatsapp_business、wechat_ilink、wecom、dingtalk、feishu、qq、line、matrix |
| **工具系统** | 49 个内置工具，12 个工具集（web / file / terminal / browser / code_execution / memory / skills / session / cron / delegation / utility / homeassistant） |
| **技能系统** | 渐进式加载（L0/L1/L2），三级来源（内置 / 全局 / 工作区），支持自动生成的技能审核流 |
| **Cortex 认知架构** | 记忆（SOUL.md / USER.md / 快照）、感知、规划、执行、技能进化 |
| **用量追踪** | 按模型 / 会话 / 天统计 Token 与成本，支持月度预算告警 |
| **隐私脱敏** | 日志与输出中的手机号、邮箱、身份证、银行卡、IP、地址自动打码 |

---

## 2. 安装

### 2.1 下载预编译二进制（推荐）

```bash
# Linux / macOS
curl -L https://github.com/magicwubiao/go-magic/releases/latest/download/magic-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz | tar xz
chmod +x magic-*
sudo mv magic-* /usr/local/bin/magic
```

Windows 用户直接从 [Releases](https://github.com/magicwubiao/go-magic/releases) 下载 `magic-windows-amd64.exe`。

### 2.2 Go Install

```bash
go install github.com/magicwubiao/go-magic/cmd/magic@latest
```

### 2.3 一键脚本（Linux / macOS）

```bash
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
```

另有 Homebrew / Scoop 安装脚本，见 `scripts/` 目录。

### 2.4 Docker

```bash
docker run -it magicwubiao/go-magic

# 或用 compose（含可选 Redis）
docker compose up -d
```

> ⚠️ **注意（实测）**：`docker-compose.yml` 映射的是 `8642:8642`，但 `magic server` 的默认端口是 **5000**。容器里默认执行的是 `magic server`，因此需要显式指定端口才能对上映射：
> ```bash
> docker run -p 8642:8642 magicwubiao/go-magic server --port 8642
> ```
> 详见 [22. 已知问题](#22-已知问题实测确认)。

### 2.5 从源码构建

要求：**Go 1.26+**、**Node.js 20+**（Web Dashboard 用）。

```bash
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic

make build        # 构建前端 + CLI
make build-all    # 常见平台 (Linux / macOS / Windows)
make build-cross  # 全部支持平台

# 指定平台
make build-linux
make build-macos
make build-windows
```

常用 Make 目标：

| 目标 | 作用 |
|------|------|
| `make build` | build-web + build-cli |
| `make build-web` | 仅构建前端（vite 产物直接输出到 `internal/server/dist`，由 Go embed 打包） |
| `make build-cli` | 仅构建 Go 二进制 |
| `make test` / `make test-short` / `make test-coverage` | 测试 |
| `make lint` | 代码检查 |
| `make run` / `make run-server` | 运行 TUI / Dashboard |
| `make docker` / `make docker-run` / `make docker-stop` | 镜像相关 |
| `make clean` | 清理产物 |

---

## 3. 快速开始（5 分钟跑通）

```bash
# 1. 交互式初始化：选 Provider、填 API Key、选模型、配工具
magic setup

# 2. 开聊（TUI）
magic chat

# 3. 或者启动 Web Dashboard
magic server
# 浏览器打开 http://localhost:5000
```

跳过向导中某些环节：

```bash
magic setup --skip-model      # 跳过模型选择
magic setup --skip-tools      # 跳过工具配置
magic setup --skip-gateway    # 跳过网关配置
```

也可以完全用命令行配置，跳过向导：

```bash
magic config set providers.deepseek.api_key sk-xxxxxxxx
magic config set provider deepseek
magic model deepseek:deepseek-chat
magic doctor                # 自检
```

---

## 4. 目录结构与数据存储

所有数据默认存放在 **magic home**，即 `~/.magic`（Windows：`%USERPROFILE%\.magic`）。

```
~/.magic/
├── config.json          主配置文件
├── sessions.db          会话数据库（SQLite，含 FTS5 全文索引）
├── bots.db              Bot 模式库
├── groupchat.db         群聊库（gc_rooms / gc_room_agents / gc_messages）
├── kanban.db            看板库
├── instance_id          本机身份标识（跨机器 Peer 用，惰性生成）
├── peers.json           已知 Peer 列表（权限 0600）
├── .auth_token          Dashboard 登录令牌
├── bots/                每个 Bot 一个 <name>.json，私有变量在 <name>/.env
├── skills/              全局技能
├── builtin_skills/      内置技能副本
├── plugins/             插件
├── logs/                日志，命名 magic_YYYY-MM-DD_HH-MM-SS.log
├── usage/               用量统计
├── cron/                定时任务数据
├── cortex/              Cortex 认知数据
├── memories/            记忆数据
└── workspace/           工作区文件
```

切换 magic home：

```bash
magic --magic-home /data/magic config list
export GO_MAGIC_HOME=/data/magic     # 环境变量方式（代码实际读取的是这个）
```

---

## 5. 配置详解

### 5.1 配置文件位置

```bash
magic config path      # 打印当前配置文件路径
magic config list      # 列出全部配置
magic config get <key> # 读单个值
magic config set <key> <value>
magic config validate  # 校验
magic config reset     # 恢复默认
```

### 5.2 完整配置示例

```jsonc
{
  "profile": "default",                 // 配置档名
  "magic_home": "~/.magic",
  "provider": "deepseek",               // 当前 Provider
  "model": "deepseek-chat",             // 已废弃，建议用 providers.<x>.models[0]
  "chat_mode": "chat",                  // magic chat 的默认模式：chat | coding

  "providers": {
    "deepseek": {
      "api_key": "sk-xxxx",
      "base_url": "https://api.deepseek.com/v1",   // 可省略
      "models": ["deepseek-chat", "deepseek-reasoner"]
    },
    "openai":    { "api_key": "sk-xxxx", "models": ["gpt-5.6", "gpt-5.6-terra"] },
    "anthropic": { "api_key": "sk-ant-xxxx", "models": ["claude-sonnet-5"] },
    "ollama":    { "base_url": "http://localhost:11434", "models": ["llama3.3", "qwen3"] }
  },

  "tools":  { "enabled": ["all"], "disabled": [] },
  "skills": { "enabled": [], "disabled": [], "user_dir": "" },

  "agent": {
    "goal_max_turns": 20,      // 目标驱动模式的最大回合
    "max_turns": 60,           // 单轮对话的工具循环上限，0 = 内置默认
    "max_iterations": 80,      // 转向控制上限
    "max_token_budget": 0      // 令牌预算，0 = 不限
  },

  "bot_mode": {
    "enabled": false,
    "inject_bot_protocol": true,   // nil 时运行时默认开启
    "history_window": 200,         // 每个 Bot 保留的历史消息数，0 = 200
    "turn_timeout_minutes": 5,     // 单回合超时，0 = 5 分钟
    "relay_token": ""              // 跨机器 relay 共享密钥，空 = 匿名（仅可信网络）
  },

  "gateway": {
    "enabled": false,
    "rate_limit_per_user": 20,     // 每人每分钟消息数，0 = 默认 20，负数 = 不限
    "rate_limit_window_sec": 60,
    "blocked_users": [],
    "sensitive_words": [],
    "platforms": {
      "telegram": {
        "enabled": true,
        "token": "123456:ABC-DEF",
        "dm_policy": "open",            // open | allowlist | disabled
        "group_policy": "open",
        "allowed_channels": [],
        "blocked_channels": []
      }
    }
  },

  "memory": { "enabled": true },
  "cortex": { "enabled": true, "skill_min_pattern_freq": 3 },

  "mcp": { "servers": {} },
  "subagent": { "max_concurrent": 4, "max_depth": 2, "timeout": 120 },

  "approval": {
    "strategy": "smart",           // smart | manual | auto
    "trust_threshold": 3,
    "enable_learning": true,
    "enable_cli_confirm": false,
    "approval_timeout": 60,
    "timeout_strategy": "deny"     // deny | allow_low_medium | allow_all
  },

  "privacy": {
    "enabled": true,
    "redact_phone": true,
    "redact_email": true,
    "redact_id_card": true,
    "redact_bank_card": true,
    "redact_ip": true,
    "redact_address": true,
    "custom_patterns": {}
  },

  "display": { "skin": "default", "no_color": false, "show_banner": true },
  "server":  { "file_strategy": "auto", "upload_url_prefix": "" }
}
```

### 5.3 环境变量

| 变量 | 说明 | 备注 |
|------|------|------|
| `GO_MAGIC_HOME` | 指定 magic home 目录（默认 `~/.magic`） | ✅ 代码中实际读取 |
| `GO_MAGIC_CORS_ORIGINS` | Dashboard 额外允许的 CORS 源，逗号分隔 | ✅ |
| `MAGIC_SKILL_DIR` | 覆盖技能目录 | ✅ |
| `MAGIC_SESSION_ID` | 指定会话 ID | ✅ |
| `OPENAI_API_KEY` / `DEEPSEEK_API_KEY` / `ANTHROPIC_API_KEY` / `GOOGLE_API_KEY` / `GROQ_API_KEY` / `OPENROUTER_API_KEY` / `TOGETHER_API_KEY` | 各家 API Key | ✅ |
| `TELEGRAM_BOT_TOKEN` / `DISCORD_BOT_TOKEN` | 网关 Token | ✅ |

> Profile 切换请用 `--profile/-p` 参数或配置文件中的 `profile` 字段，没有对应的环境变量。

### 5.4 全局参数

所有子命令都可用：

| 参数 | 说明 |
|------|------|
| `-v, --verbose` | 详细输出 |
| `--debug` | 调试模式（隐含 verbose） |
| `--config <path>` | 指定配置文件路径 |
| `-o, --output <fmt>` | 输出格式：`text` / `json` / `yaml` / `table` |
| `--no-color` | 关闭彩色输出 |
| `--magic-home <dir>` | 指定 magic home |
| `-p, --profile <name>` | 指定配置档 |

> `-p` 是全局保留的简写。注意它与部分子命令的本地标志冲突，见 [22. 已知问题](#22-已知问题实测确认)。

---

## 6. 命令行（CLI）总览

```
magic
├── chat                 TUI 交互式聊天
├── server               Web Dashboard
├── setup                交互式初始化向导
├── model                选择/查看 Provider 与模型
├── config               配置管理
│   ├── get / list / path / reset / set / validate
├── sessions             会话管理  list / show / rename / delete
├── skills               技能管理  list / show / search / match / create / install /
│   │                              uninstall / import / delete / enable / disable
│   ├── auto             自动生成技能审核  list / pending / approve / reject /
│   │                                    archive / restore / delete / status / stats
│   ├── hub              远程技能库  list / audit
│   └── config           技能配置  list / disabled
├── tools                工具  list
│   └── toolsets         工具集  list / show / enable / disable
├── mcp                  MCP 服务器  list / add / connect / disconnect / health
├── acp                  ACP 智能体互联  serve / connect / disconnect / list / ping / skills / call
├── plugin               插件  list / search / discover / install / uninstall /
│                               enable / disable / reload / update / info / check
├── agent                子智能体  spawn / list / kill / stats
├── cron                 定时任务  list / add / edit / remove / toggle / test
├── kanban               看板  create / list / show / start / claim / complete / block /
│                              unblock / archive / comment / link / unlink / triage /
│                              board / stats / delete
├── bots                 Bot 模式  create / list / show / edit / clone / chat /
│   │                             message / hide / unhide / remove
│   └── routine          Bot 例程  add / list / enable / disable / remove
├── rooms                群聊房间  create / list / show / send / messages / remove
├── peer                 跨机器节点  add / list / dm / remove
├── gateway              消息网关  start / stop / restart / status / setup
├── privacy              PII 脱敏  detect / redact / audit / stats
├── skin                 主题  list / show / preview / create / set / export / delete
├── voice                语音  listen / speak / test
├── vision               图像理解  analyze / compare
├── coding               编码助手  init / analyze / lint / test / build / debug
├── docs                 文档生成  generate | serve | man
├── usage                Token 用量（默认 1 天，-d 指定天数）
├── logs                 日志  list / show / latest
├── status / health / metrics / stats / doctor   系统状态与诊断
├── backup               全量备份
├── update               自更新
├── migrate              从 OpenClaw 迁移配置
├── completion           生成 shell 自动补全脚本
└── validate / version
```

---

## 7. 核心命令详解

### 7.1 `magic chat` —— TUI 聊天

```bash
magic chat                 # 默认流式输出
magic chat --no-stream     # 关闭流式
magic chat -v              # 带详细日志
```

进入后直接打字即可，输入 `/help` 查看全部斜杠命令。

**Chat 模式 vs Coding 模式**

| 项目 | Chat（默认） | Coding |
|------|-------------|--------|
| 文件写入权限 | 受限 | 放宽 |
| 命令执行超时 | 30 秒 | 300 秒 |
| Python / Node 代码执行 | 禁用 | 启用 |
| Shell 访问 | 有限 | 完全 |
| 工具自动批准 | 否 | 是 |

TUI 内用 `/mode coding` 切换，`/mode chat` 切回。持久化默认值写配置：

```bash
magic config set chat_mode coding
```

### 7.2 `magic server` —— Web Dashboard

```bash
magic server                 # 默认 http://localhost:5000
magic server --port 8642     # 换端口
magic server --open          # 启动后打开浏览器
```

启动时会打印数据库路径与监听地址，`Ctrl+C` 优雅退出。

### 7.3 `magic model` —— 切换 Provider / 模型

```bash
magic model                              # 查看当前
magic model gpt-5.6                      # 只换模型（当前 Provider）
magic model deepseek:deepseek-chat       # 同时换 Provider 和模型
magic model --list openai                # 列出某 Provider 的可用模型
magic model huoshan:ep-20250105-xxxxx    # 火山引擎接入点 ID 也支持
```

### 7.4 `magic sessions` —— 会话管理

```bash
magic sessions list              # 按最近更新时间倒序
magic sessions show <id>
magic sessions rename <id> <新名字>
magic sessions delete <id>       # 不可恢复，建议先 magic backup
```

### 7.5 `magic agent` —— 子智能体并行执行

```bash
magic agent spawn "代码审查" "审查 ./internal/server 下的并发安全" \
    --tools read_file,search_in_files --timeout 300 --max-depth 2
magic agent list
magic agent stats
magic agent kill <agent-id>
```

### 7.6 `magic coding` —— 编码助手

```bash
magic coding init --type go            # 识别/初始化项目（go/python/node/rust/java/cpp）
magic coding analyze ./main.go --ai    # AI 深度分析，-f 给出修复建议
magic coding lint ./src --fix --format json
magic coding build --target linux/amd64 --release -o myapp
magic coding debug --chat              # 带编码上下文的交互调试
```

---

## 8. TUI 斜杠命令

在 `magic chat` 中使用：

| 命令 | 别名 | 说明 |
|------|------|------|
| `/help [command]` | `/?` | 帮助 |
| `/commands [category]` | `/cmds` | 列出全部命令 |
| `/new` | | 开始新对话 |
| `/clear` | | 清空聊天历史 |
| `/compress` | | 压缩上下文窗口 |
| `/retry` | | 重试上一次响应 |
| `/undo` | | 撤销上一次操作 |
| `/export [format]` | `/save` | 导出对话 |
| `/handoff [model]` | | 把会话转交另一个模型 |
| `/model [provider:model]` | `/m` | 切换模型 |
| `/mode [chat\|coding]` | | 切换模式 |
| `/personality [name]` | `/persona`, `/tone` | 设置人格 |
| `/tools [category]` | | 列出可用工具 |
| `/skills [name]` | `/skill` | 列出可用技能 |
| `/context [add\|remove\|list]` | `/ctx` | 管理上下文文件 |
| `/clarify [question]` | | 向用户追问澄清 |
| `/interrupt [reason]` | | 中断执行 |
| `/subgoal <text>` | | 给当前目标添加子目标 |
| `/goals` | | 目标管理 |
| `/status` | | 系统状态 |
| `/version` | `/ver` | 版本 |
| `/usage [--days N]` | | Token 用量 |
| `/insights [-d N]` | | 用量洞察（默认 7 天） |
| `/sessions [list\|search]` | `/session` | 会话列表 |
| `/sethome [session_id]` | | 设置网关的主会话 |
| `/stop` | `/cancel` | 停止当前操作 |

---

## 9. Web Dashboard

启动：`magic server`（默认 `http://localhost:5000`）。首次访问会引导设置登录密码（**至少 8 位**，用 bcrypt 存储）。设置完成后除 `/api/health`、`/api/status`、`/metrics`、`/api/fs/shared/*`、`/api/relay/v1/dm` 外，其余接口都需登录态。

### 页面清单

| 路由 | 页面 | 说明 |
|------|------|------|
| `/chat` | 聊天 | 流式对话，会话管理 |
| `/kanban` | 看板 | 多 Agent 任务协作 |
| `/config` | 配置 | 在线编辑配置，热加载 |
| `/models-providers` | 模型与 Provider | 多模型管理、即时切换 |
| `/tools` | 工具 | 工具 / 工具集开关 |
| `/skills` | 技能 | 浏览、安装、启停、审核自动生成技能 |
| `/cron` | 定时任务 | Cron 任务管理 |
| `/gateway` | 网关 | 平台启停、扫码登录 |
| `/groupchat` | 群聊 | 多 Agent 群聊（唯一入口，`/rooms` 已重定向到 `/bots`） |
| `/bots` | Bots | Bot 档案管理 |
| `/logs` | 日志 | 实时日志 |
| `/system` | 系统 | 系统信息与健康检查 |
| `/profiles` | 配置档 | 多 Profile 切换 |
| `/goals` | 目标 | 目标分解与追踪 |
| `/approval` | 审批 | 高危工具调用审批 |
| `/files` | 文件 | 在线文件浏览 / 上传 / 下载 / 打包 |
| `/usage` | 用量 | Token 与成本统计、预算进度 |
| `/mcp` | MCP | MCP 服务器管理 |
| `/plugins` | 插件 | Agent 插件管理 |
| `/login` | 登录 | 公开路由 |

### 主要 API（前端同源调用）

```
/api/auth/*          登录与初始化
/api/chat            非流式对话
/api/chat/stream     流式对话（SSE）
/api/sessions/*      会话 CRUD 与搜索
/api/models/*        模型查询与切换（热加载）
/api/providers/*     Provider 管理
/api/config/*        配置读写
/api/skills/*        技能
/api/tools/*         工具与工具集
/api/mcp/servers/*   MCP
/api/cron/jobs/*     定时任务
/api/kanban/*        看板
/api/groupchat/*     群聊
/api/bots/*          Bot
/api/fs/*            文件系统
/api/usage/*         用量与预算
/api/gateway/*       网关控制
/api/logs/*          日志
/api/relay/v1/dm     跨机器 relay（不套登录校验，令牌在请求体内独立校验）
/metrics             Prometheus 指标
```

---

## 10. Bot 模式（具名 Agent）

Bot 是一份**具名配置档案**：角色标签、人设提示词、Provider/模型绑定、工具与技能白名单、私有环境变量、独立持久化会话，以及定时例程。

### 10.1 开启

```bash
magic config set bot_mode.enabled true
```

### 10.2 创建与管理

```bash
magic bots create researcher \
  --title "研究助手" \
  --desc "负责检索和总结论文" \
  --prompt "你负责查找和总结论文，输出要带引用链接。" \
  --provider deepseek --model deepseek-chat \
  --tools web_search,web_fetch,read_file \
  --skills "paper-review" \
  --env "SERPAPI_API_KEY=xxxx" \
  --avatar "🔬" \
  --memory "用户偏好：中文输出，结论先行"

magic bots list           # 列出所有 Bot 及状态
magic bots show researcher
magic bots edit researcher --model deepseek-reasoner
magic bots clone researcher analyst     # 复制配置，会话历史清空
magic bots hide researcher / unhide researcher   # 在 Dashboard 列表中显示/隐藏
magic bots remove researcher
```

`--env` 写入 `~/.magic/bots/<name>/.env`，实现**每个 Bot 独立凭据**。

### 10.3 对话

```bash
magic bots chat researcher "找一下关于 agent 记忆的最新论文"
magic bots message researcher coder "把这份总结整理成 Markdown 表格"   # Bot → Bot，发后不管
```

### 10.4 定时例程

```bash
magic bots routine add researcher daily-digest \
  --schedule "0 9 * * *" \
  --prompt "总结昨天的发现。"

magic bots routine list researcher
magic bots routine enable  researcher daily-digest
magic bots routine disable researcher daily-digest
magic bots routine remove  researcher daily-digest
```

### 10.5 在聊天平台里用 Bot

任意已接入的网关平台上：

```
/bot researcher 找一下关于 agent 记忆的最新论文
@researcher 找一下关于 agent 记忆的最新论文
```

### 10.6 跨机器 Peer

一台机器上的 Bot 可以直接给另一台机器上的 Bot 发消息（HTTP relay）。

```bash
# 机器 B（被呼叫方）：确保 dashboard 或 gateway 在跑，relay 端点随服务端口
magic config set bot_mode.relay_token "shared-secret"    # 强烈建议设置

# 机器 A（发起方）
magic peer add lab-b http://192.168.1.20:5000 --token shared-secret
magic peer list
magic peer dm lab-b researcher "报告进展如何？"
magic peer remove lab-b
```

- Peer 表：`<magicHome>/peers.json`（权限 0600）
- 本机身份：`<magicHome>/instance_id`
- relay 端点：`POST http://<host>:<port>/api/relay/v1/dm`，消息带 `[relay from@instance]` 前缀
- **DM 是阻塞的**：会一直等到远端 Bot 跑完整个回合，所以远端慢 = 命令慢（客户端超时 6 分钟，响应上限 4MB）
- `relay_token` 为空时 relay 接受匿名请求，仅限可信网络

---

## 11. 群聊与 Rooms

一个 Room 容纳 **2–6 个 Bot**，人类发一条消息后，成员轮流发言（最多 3 轮），互相用 `@名字` 拉人。Bot 以 `@user` 开头即可中断本轮、把问题抛回人类。

```bash
magic rooms create design-review --members researcher,coder --topic "架构评审"
magic rooms send <room-id> "Review the plan" --target researcher
magic rooms messages <room-id>
magic rooms show <room-id>
magic rooms list
magic rooms remove <room-id>
```

Web 端在 `/groupchat` 页面操作，可以：

- 创建房间时**从现有 Bot 拉入**作为智能体
- 头部切换**回复模式**：`mention`（只有被 @ 的回复）/ `all`（全员回复）
- 用**邀请码加入房间**
- 在房间信息面板管理成员
- 前端会处理 `round` / `start` / `pass` 等 SSE 事件，展示轮次分隔与跳过提示

> 历史遗留：旧版 `/api/rooms` 与 `bots/rooms/*.json` 已废弃，启动时**一次性幂等迁移**到 `groupchat.db`（旧目录会被改名为 `rooms.migrated-<时间戳>` 备份）。老数据时间戳是秒、新库是毫秒，迁移代码会自动兼容。

---

## 12. 消息网关（接外部聊天平台）

网关把 Agent 接到外部聊天软件上，健康检查端点在 `http://localhost:8081/health`。

```bash
magic gateway setup      # 交互式配置平台
magic gateway start                       # 启动所有已启用平台
magic gateway start -P telegram           # 只启动单个平台
magic gateway status
magic gateway stop
magic gateway restart
```

### 12.1 支持的平台

| 平台 ID | 名称 | 关键配置项 |
|---------|------|-----------|
| `telegram` | Telegram | `token` |
| `discord` | Discord | `token` |
| `slack` | Slack | `bot_token`（或 `token`）、`signing_secret`（或 `app_secret`） |
| `whatsapp` | WhatsApp 个人号（扫码） | `data_dir`，`mode: personal` |
| `whatsapp_business` | WhatsApp Business API | `phone_number_id`（或 `app_id`）、`access_token`（或 `token`）、`app_secret`、`verify_token` |
| `wechat_ilink` | 微信 iLink | `token`、`base_url`、`data_dir`、`auto_login` |
| `wecom` | 企业微信 | `corp_id`、`agent_id`、`secret`、`mode: qr \| app` |
| `dingtalk` | 钉钉 | `app_key`、`app_secret` |
| `feishu` | 飞书 / Lark | `app_id`、`app_secret` |
| `qq` | QQ | `number`（或 `app_id`）、`password`（或 `app_secret`） |
| `line` | LINE | `token`、`secret` |
| `matrix` | Matrix | `token`、`base_url` |

### 12.2 配置示例

```jsonc
"gateway": {
  "enabled": true,
  "rate_limit_per_user": 20,
  "blocked_users": ["spam-user-id"],
  "sensitive_words": [],
  "platforms": {
    "telegram": {
      "enabled": true,
      "token": "123456:ABC-DEF",
      "dm_policy": "allowlist",              // open | allowlist | disabled
      "dm_allowlist": ["111222333"],
      "group_policy": "open",
      "allowed_channels": ["-1001234567890"],// 白名单；留空 = 全部
      "blocked_channels": [],
      "mention_patterns": []                 // 额外视为 @ 的正则
    },
    "wecom": {
      "enabled": true,
      "mode": "app",
      "corp_id": "wwxxxx",
      "agent_id": "1000002",
      "secret": "xxxx",
      "aes_key": "xxxx"
    }
  }
}
```

### 12.3 在平台上与 Bot 对话

```
/bot <名称> <消息>
@<tag> <消息>
```

用 `/sethome <session_id>` 可以把某个会话设为网关的「主会话」。

---

## 13. 技能系统

技能从三个层级加载，同名时后者优先：

1. **内置技能** —— 随程序打包
2. **全局技能** —— `~/.magic/skills/`
3. **工作区技能** —— `./skills/` 或 `.magic/skills/`

支持格式：`SKILL.md`（带 YAML frontmatter，推荐）、`.json`、`.md`/`.markdown`、`.txt`、带 `manifest.json` 的目录。

```bash
magic skills list                        # 表格列出
magic skills list --format json          # table | list | json
magic skills list --source global        # builtin | global | local | imported
magic skills list --filter api --show-tools
magic skills show <name>
magic skills search <keyword>
magic skills match "帮我 review 这段代码"   # 看哪些技能会被触发
magic skills create <name> [--force]
magic skills install <name> --from ./path-or-url
magic skills uninstall <name>
magic skills enable <name> / disable <name>
magic skills delete <name>
```

### 13.1 导入既有技能

```bash
magic skills import ./path/to/skill
magic skills import ./skills --recursive --force
magic skills import ./skills --list       # 只看不导入
```

支持 OpenClaw（`trigger_conditions`）、Hermes（`hermes` 元数据）、通用 SKILL.md 三种来源。

### 13.2 自动生成技能的审核流

Cortex 会从你的使用模式里归纳出新技能，但**默认不会自动投入使用**，需要人工审核：

```bash
magic skills auto list       # 全部及状态
magic skills auto pending    # 待审核
magic skills auto approve <name>    # 通过 → 之后会注入 Agent 提示词
magic skills auto reject <name>
magic skills auto archive <name>    # 归档（文件保留，不再激活）
magic skills auto restore <name>
magic skills auto delete <name>
magic skills auto status <name>
magic skills auto stats      # 各状态计数
```

状态流转：`pending` → `approved` ⇄ `archived`，`rejected` 等待删除。

### 13.3 技能库

```bash
magic skills hub list
magic skills hub audit       # 安装审计日志
magic skills config list
magic skills config disabled
```

---

## 14. 工具与工具集

```bash
magic tools list             # 列出全部工具（当前 49 个）
magic tools list --json
magic tools toolsets list    # 12 个工具集
magic tools toolsets show <name>
magic tools toolsets enable <name> / disable <name>
```

### 工具集与工具对照

| 工具集 | 工具 |
|--------|------|
| **web** | `web_search`、`web_fetch` |
| **file** | `read_file`、`write_file`、`file_edit`、`list_files`、`search_in_files` |
| **terminal** | `execute_command`、`terminal`、`process` |
| **browser** | `browser_navigate`、`browser_snapshot`、`browser_click`、`browser_type`、`browser_scroll`、`browser_back`、`browser_get_images`、`browser_console` |
| **code_execution** | `execute_code`（Python / Node.js） |
| **memory** | `memory_store`、`memory_recall` |
| **skills** | `skill_list`、`skill_view`、`skill_manage`、`skill_create`、`skill_delete` |
| **session** | `session_search` |
| **delegation** | `delegate_task`、`poll_task`、`list_tasks`、`cancel_task` |
| **cron** | `cronjob` |
| **homeassistant** | `ha_list_entities`、`ha_get_state`、`ha_list_services`、`ha_call_service`、`ha_events`、`ha_config` |
| **utility** | `json`、`yaml`、`string`、`hash`、`uuid`、`random`、`time`、`math`、`csv`、`env`、`system_info` |

配置文件中控制开关：

```jsonc
"tools": { "enabled": ["all"], "disabled": ["browser_*"] }
```

---

## 15. MCP 与 ACP

### 15.1 MCP（Model Context Protocol）

连接外部 MCP 服务器，把它们的工具挂进 Agent（工具名前缀 `mcp_*`）。支持 stdio 与 SSE 两种传输。

```bash
magic mcp add <server-name>
magic mcp connect <server-name> <command> [args...]     # stdio 方式
magic mcp list            # 列出已连接服务器及其工具
magic mcp health [server-name]
magic mcp disconnect <server-name>
```

配置写在 `config.json` 的 `mcp.servers` 下：

```jsonc
"mcp": {
  "servers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"],
      "transport": "stdio"
    }
  }
}
```

### 15.2 ACP（Agent Communication Protocol）

让多个 Agent 互相发现并调用彼此的技能。

```bash
magic acp serve --transport stdio                    # 默认 stdio
magic acp serve --transport http --address ":8080" --agent-id my-agent

magic acp connect helper --transport http --address http://localhost:8080
magic acp connect local-helper --transport stdio --command /path/to/agent --args "--mode","acp"
magic acp list
magic acp ping [agent]
magic acp skills [agent]
magic acp call helper summarize --params '{"text":"..."}'
magic acp disconnect helper
```

---

## 16. 定时任务（Cron）

```bash
# 让 Agent 在指定时间执行一段提示词
magic cron add morning-brief \
  --schedule "0 9 * * *" \
  --prompt "总结昨天未完成的任务" \
  --enabled

# 或者跳过 Agent，直接跑脚本
magic cron add nightly-backup \
  --schedule "0 2 * * *" \
  --no-agent \
  --script "/usr/local/bin/backup.sh"

magic cron list
magic cron edit <name>
magic cron toggle <name>
magic cron test <name>       # 立即跑一次
magic cron remove <name> [--force]
```

`--schedule` 用标准 5 段 cron 表达式，默认 `0 9 * * *`。

---

## 17. 看板（Kanban）

面向多 Agent 协作的任务板，任务状态流转：

```
triage/todo → ready → running → done → archived
                   ↘ blocked → ready
```

```bash
magic kanban create "实现登录页" --body "含手机号与邮箱两种方式" \
    --priority 2 --assignee coder --tenant default -w "dir:/data/proj"
magic kanban list --status running --assignee coder --tenant default
magic kanban show <task_id>
magic kanban start <task_id>          # triage/todo → ready
magic kanban claim <task_id>          # ready → running
magic kanban complete <task_id> -s "已上线"   # running → done
magic kanban block <task_id> -r "等待设计稿"
magic kanban unblock <task_id>
magic kanban comment <task_id> "补充：需要兼容深色模式"
magic kanban link <parent_id> <child_id>     # 父子依赖
magic kanban unlink <parent_id> <child_id>
magic kanban triage <task_id>                # 用 LLM 细化任务
magic kanban board
magic kanban stats
magic kanban archive <task_id>
magic kanban delete <task_id>
```

`--priority`：`0=低`、`1=中`、`2=高`、`3=紧急`。
`--workspace`：支持 `scratch`、`dir:/path`、`worktree` 三种形态。

> ℹ️ `magic kanban create` 的 `-p` 简写已移除（改为 `--priority`），详见 [22. 已知问题](#22-已知问题实测确认)。

---

## 18. Token 用量与预算

```bash
magic usage            # 今天（默认 1 天）
magic usage -d 7       # 最近 7 天
magic usage -d 30      # 最近 30 天
```

TUI 内用 `/usage` 与 `/insights`。Web 端 `/usage` 页面提供：今日统计、每日趋势图、月度明细、预算进度条与告警、Top 模型/会话。

相关 API：`/api/usage/today|daily|weekly|monthly|insights|budget|sessions|top-sessions|providers|hourly`。

---

## 19. 隐私脱敏

自动识别并打码：手机号、邮箱、身份证、银行卡、IP、地址，支持自定义正则。

```bash
magic privacy detect "联系我 13800138000 或 a@b.com"     # 只检测，不打码
magic privacy detect "..." --json
magic privacy redact "联系我 13800138000"
magic privacy redact --disable        # 关闭脱敏
magic privacy redact --enable         # 开启脱敏
magic privacy audit                   # 脱敏审计日志
magic privacy stats                   # 统计
```

配置项：`privacy.enabled` 及各 `redact_*` 开关、`custom_patterns`。

---

## 20. 运维：诊断、备份、更新

### 20.1 诊断

```bash
magic doctor                       # 默认跑一遍
magic doctor -c config             # 只查配置
magic doctor -c provider           # 只测 Provider 连通性
magic doctor -c tools              # 只查工具可用性
magic doctor -c gateway            # 只查网关
magic doctor -c skills             # 只查技能目录
magic doctor all

magic status                       # 配置 / 技能 / 插件 / 工具 / 资源 / 健康
magic health
magic metrics
magic stats                        # 配置概要 + 技能数 + 会话库 + 日志数
magic validate                     # 校验配置文件
```

### 20.2 日志

```bash
magic logs list
magic logs show magic_2026-09-01_14-42-28.log
magic logs latest
```

### 20.3 备份

```bash
magic backup
```

把整个 magic home 复制到 `~/.magic_backup_YYYYMMDD_HHMMSS`，包含 config.json、skills/、sessions.db、logs/、SOUL.md、MEMORY.md、USER.md。

### 20.4 更新

```bash
magic update --check                      # 只检查
magic update                              # 更新（默认先备份）
magic update --channel beta               # stable | beta | nightly
magic update --force                      # 同版本也重装
magic update --backup=false               # 不备份
```

### 20.5 迁移

```bash
magic migrate        # 从 OpenClaw 的配置与数据迁移到 go-magic
```

### 20.6 Shell 补全

```bash
magic completion bash
magic completion zsh
magic completion fish
magic completion powershell
```

### 20.7 自动生成文档

```bash
magic docs generate --format markdown
magic docs serve --port 8080
magic docs man
```

---

## 21. 常见问题 FAQ

**Q：改了配置需要重启吗？**
Provider / 模型切换是热加载的，Web Dashboard 上切换即时生效。其他项（如 `bot_mode.enabled`、网关平台）建议重启相关服务。

**Q：怎么在一个 Provider 下配多个模型？**
`providers.<name>.models` 数组里列多个，**第一个是当前活跃模型**。TUI 用 `/model provider:model` 切，CLI 用 `magic model provider:model`。

**Q：接入一个不在列表里的 OpenAI 兼容服务？**
用 `custom` Provider，或者给任意 Provider 配上 `base_url`。

**Q：Bot 之间怎么通信？**
Bot 内置 `message_agent` 工具；CLI 上是 `magic bots message <from> <to> <msg>`（发后不管，回复以后台通知形式送达发送方）。前提是 `bot_mode.enabled = true`。

**Q：Dashboard 的 CORS 报错怎么办？**
默认只允许 localhost / 127.0.0.1 的 8642、8643 端口。用 `GO_MAGIC_CORS_ORIGINS` 追加允许的源，逗号分隔。

**Q：想让两个 go-magic 实例上的 Bot 互相通信？**
用 `magic peer`，见 [10.6 跨机器 Peer](#106-跨机器-peer)。记得在接收方设置 `bot_mode.relay_token`。

**Q：Docker 起来后访问不到页面？**
端口不一致导致，见下一节。

---

## 22. 已知问题（实测确认）

以下问题在 **v0.5.9 源码编译产物**上实测复现。标「✅ 已修复」的已在本次会话中修复。

### 22.1 两个子命令会因标志冲突 panic ✅ 已修复

全局持久化标志 `-p`（`--profile`）与两个子命令的本地标志 `-p` 冲突，会导致命令一执行就 panic：

```bash
$ magic coding test
panic: unable to redefine 'p' shorthand in "test" flagset: it's already used for "pattern" flag

$ magic kanban create
panic: unable to redefine 'p' shorthand in "create" flagset: it's already used for "priority" flag
```

- `coding.go` 中 `test` 定义了 `-p/--pattern`
- `kanban.go` 中 `create` 定义了 `-p/--priority`

**修复**：已移除这两个子命令的 `-p` 简写（`coding test` 改用 `--pattern`、`kanban create` 改用 `--priority`），保留全局 `-p/--profile`。现已可直接使用。

### 22.2 Docker 端口与默认端口不一致 ✅ 已修复

- `magic server` 的默认端口是 **5000**
- `Dockerfile` / `docker-compose.yml` / `Makefile` 暴露的是 **8642（API）与 8643（Webhook）**
- 容器默认命令是 `magic server`，所以按 compose 原样启动会在容器内监听 5000，与 `8642:8642` 映射对不上

**修复**：`Dockerfile` 的 `CMD` 已改为 `["server", "--port", "8642"]`，`docker-compose.yml` 端口映射改为 `8642:8642`，`Makefile docker-run` 同步去掉了无效的 `GO_MAGIC_PROFILE` 并修正端口。`peers.go` 帮助文本中的 `:8642` 示例也已更正。

### 22.3 文档与实际环境变量不符 ✅ 已修复

README 与自动生成的文档里曾提到 `GO_MAGIC_PROFILE`、`MAGIC_HOME`、`MAGIC_PROFILE`、`MAGIC_VERBOSE`、`MAGIC_NO_COLOR`，这些在代码中**没有实际读取逻辑**（仅出现在 `cmd/magic/docs.go`、`internal/docs/llm_generator.go` 的文档文本里）。

**修复**：`README.md`、`README.zh-CN.md`、`cmd/magic/docs.go`、`internal/docs/llm_generator.go` 中已全部替换为实际生效的 `GO_MAGIC_HOME`、`GO_MAGIC_CORS_ORIGINS`、`MAGIC_SKILL_DIR`、`MAGIC_SESSION_ID`。切换 Profile 仍用 `--profile/-p` 参数或配置文件 `profile` 字段。

### 22.4 已废弃的 rooms 路由

旧版 `/api/rooms` 与前端 `RoomsView.vue` 已废弃，前端 `/rooms` 路由重定向到 `/bots`，群聊统一走 `/groupchat` 与 `/api/groupchat/*`。旧数据在首次启动时自动迁移到 `groupchat.db`。

---

## 附录：一分钟速查

```bash
# 首次
magic setup                       # 向导
magic doctor                      # 自检

# 日常
magic chat                        # TUI 聊天
magic server                      # Web Dashboard :5000
magic model deepseek:deepseek-chat
magic usage -d 7

# Bot
magic config set bot_mode.enabled true
magic bots create researcher --title "研究助手" --prompt "查找并总结论文"
magic bots chat researcher "找一下关于 agent 记忆的最新论文"
magic bots routine add researcher daily-digest --schedule "0 9 * * *" --prompt "总结昨天"

# 群聊
magic rooms create review --members researcher,coder --topic "评审"

# 跨机
magic peer add lab-b http://192.168.1.20:5000 --token secret
magic peer dm lab-b researcher "进展如何？"

# 网关
magic gateway setup
magic gateway start -P telegram

# 运维
magic backup
magic logs latest
magic update --check
```
