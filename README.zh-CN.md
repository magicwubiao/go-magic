# Go Magic

高性能、超轻量级的 Go 语言 AI Agent 框架，灵感来源于 [Nous Research 的 hermes-agent](https://github.com/NousResearch/hermes-agent)。

## 特性

| 特性 | 描述 |
|------|------|
| **多 Provider 支持** | OpenAI, DeepSeek, Huoshan, Anthropic, Zhipu, Kimi, MiniMax, DashScope, OpenRouter, Ollama, vLLM |
| **Cortex 架构** | 三层认知系统，六大自进化机制 |
| **工具系统** | 15+ 内置工具，可扩展插件框架 |
| **消息网关** | 多平台消息（Telegram, Discord, WhatsApp, Signal, Slack 等） |
| **技能系统** | 自动创建，渐进式加载（L0/L1/L2），Skills Hub 集成 |
| **MCP 协议** | 连接外部 MCP 服务器扩展能力 |
| **会话管理** | SQLite 持久化，FTS5 全文搜索 |
| **语音模式** | TTS/STT 多 Provider 支持 |
| **视觉理解** | 多模型支持的图像理解 |
| **多平台支持** | Linux, macOS, Windows, FreeBSD, Docker |

## 快速开始

### 安装

```bash
# 通过 Go 安装
go install github.com/magicwubiao/go-magic/cmd/magic@latest

# 或克隆并构建
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
make build
```

### 一键安装 (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
```

### Docker

```bash
docker run -it magicwubiao/go-magic
```

## 使用方法

```bash
# 交互式聊天
magic chat

# Agent 模式（并行执行）
magic agent

# 语音交互
magic voice listen
magic voice speak "你好世界"

# 图像分析
magic vision analyze image.png

# REPL 模式
magic repl
```

## CLI 命令

| 命令 | 描述 |
|------|------|
| `magic chat` | 交互式聊天会话 |
| `magic agent` | Agent 模式（任务规划） |
| `magic repl` | REPL shell |
| `magic voice` | 语音交互 (listen/speak/test) |
| `magic vision` | 图像理解 (analyze/compare) |
| `magic config` | 配置管理 |
| `magic skills` | 技能管理 |
| `magic plugin` | 插件系统 |
| `magic session` | 会话管理 |
| `magic gateway` | 消息网关 |
| `magic mcp` | MCP 服务器管理 |
| `magic doctor` | 诊断工具 |
| `magic update` | 自动更新 |

## 配置

```yaml
# ~/.magic/config.yaml
provider:
  name: deepseek
  api_key: ${DEEPSEEK_API_KEY}

cortex:
  enabled: true
  max_turns: 25

tools:
  enabled: ["all"]

gateway:
  enabled: true
  platforms:
    telegram:
      token: ${TELEGRAM_BOT_TOKEN}
    discord:
      bot_token: ${DISCORD_BOT_TOKEN}
```

## 工具系统

| 工具集 | 工具 |
|--------|------|
| **web** | web_search, web_extract |
| **file** | read_file, write_file, file_edit, list_files, search_in_files |
| **terminal** | execute_command, terminal, process |
| **browser** | browser_navigate, browser_snapshot, browser_click, browser_type |
| **memory** | memory_store, memory_recall |
| **skills** | skill_list, skill_view, skill_manage |
| **code_execution** | execute_code |
| **delegation** | delegate_task, poll_task |
| **homeassistant** | ha_list_entities, ha_get_state, ha_call_service |
| **mcp** | mcp_* (来自已连接的服务器) |

## 架构

### 三层认知系统

```
┌──────────────────────────────────────────────────────┐
│  Layer 1: Perception (感知层)                          │
│  Intent Classification → Complexity Assessment        │
├──────────────────────────────────────────────────────┤
│  Layer 2: Cognition (认知层)                          │
│  Task Planning → DAG Management → Sub-agent Decisions │
├──────────────────────────────────────────────────────┤
│  Layer 3: Execution (执行层)                          │
│  Checkpoint/Resume → Result Validation                │
└──────────────────────────────────────────────────────┘
```

### 六大自进化系统

| 系统 | 描述 |
|------|------|
| **Message Trigger** | 检测对话轮次，触发 Nudge 信号 |
| **Nudge System** | 异步后台审查，不阻塞用户 |
| **Background Review** | 分析模式，生成技能草稿 |
| **Frozen Snapshot** | 通过前缀缓存降低 90% API 成本 |
| **FTS Memory** | 跨会话全文搜索 |
| **Skill Evolution** | 渐进式披露，从使用中学习 |

## 插件系统

```bash
# 发现插件
magic plugin discover

# 搜索插件
magic plugin search <query>

# 安装插件
magic plugin install <plugin-id>

# 启用/禁用/重载
magic plugin enable <id>
magic plugin disable <id>
magic plugin reload <id>

# 检查更新
magic plugin update
magic plugin check
```

## 技能系统

```bash
# 列出技能
magic skills list

# 查看技能详情
magic skills show <name>

# 创建新技能
magic skills create <name>

# 从 Skills Hub 安装
magic skills hub install <name>

# 渐进式加载
magic skills view <name> --level 0  # 仅列表
magic skills view <name> --level 1  # 完整内容
magic skills view <name> --level 2  # 含引用
```

## 消息网关

```bash
# 设置网关
magic gateway setup

# 启动网关
magic gateway start

# 配置平台
magic gateway config telegram --token <token>
magic gateway config discord --bot-token <token>
```

## 环境变量

| 变量 | 描述 |
|------|------|
| `OPENAI_API_KEY` | OpenAI API 密钥 |
| `DEEPSEEK_API_KEY` | DeepSeek API 密钥 |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token |
| `DISCORD_BOT_TOKEN` | Discord Bot Token |
| `GO_MAGIC_HOME` | 配置目录 (默认: ~/.magic) |
| `GO_MAGIC_PROFILE` | Profile 名称 (默认: default) |

## 构建

```bash
# 构建当前平台
make build

# 跨平台构建
make build-all

# 构建指定平台
./scripts/build-cross.sh linux-amd64 darwin-arm64 windows-amd64
```

## 贡献

欢迎贡献！请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 了解指南。

## 许可证

MIT License - 详见 [LICENSE](LICENSE)。

---

灵感来源于 [Nous Research](https://nousresearch.com/) 的 [hermes-agent](https://github.com/NousResearch/hermes-agent)。
