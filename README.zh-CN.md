# go-magic

**Magic Agent** -- 高性能、超轻量级的 AI Agent 框架，使用 Go 语言编写。

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8)](https://go.dev)
[![Version](https://img.shields.io/badge/version-v0.4.14-green)](https://github.com/magicwubiao/go-magic/releases)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## 简介

go-magic 是一个功能完整的 AI Agent 框架，后端使用 Go 编写，前端使用 React/TypeScript 构建 Web Dashboard。支持 22+ AI Provider，内置 TUI 界面，提供文件操作、代码执行、Web 搜索、浏览器自动化等丰富的工具集。

## 功能特性

### 多 Provider 支持 (22+)

DeepSeek、OpenAI、Anthropic、Gemini、Ollama、vLLM、Groq、硅基流动、Kimi、智谱GLM、通义千问、文心一言、MiniMax、MiMo、腾讯混元、豆包、Moonshot、OpenRouter、Together AI、Mistral AI、Cohere、Perplexity，以及任何兼容 OpenAI 的端点。

### 单 Provider 多模型

每个 Provider 可配置多个模型，数组第一个模型为当前活跃模型。切换模型即时生效，无需重启（热加载）。

### TUI 界面

基于 [BubbleTea](https://github.com/charmbracelet/bubbletea) 构建，支持多行输入、Markdown 渲染、流式输出、斜杠命令。

### Web Dashboard

React/TypeScript 前端，功能包括：
- 实时聊天对话，支持流式响应
- 会话管理（创建、搜索、恢复）
- Provider 和模型配置，支持热加载
- 技能管理
- 插件配置
- Token 使用统计

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

Telegram、Discord、Slack、WhatsApp、WeChat、WeCom、钉钉、飞书、QQ、LINE、Matrix。

### MCP 协议

连接外部 MCP (Model Context Protocol) 服务器，扩展 Agent 能力。

### 群聊

创建多人 AI Agent 对话群组，每个 Agent 可使用不同的 Provider 和模型。

### 会话管理

基于 SQLite 的持久化存储，支持 FTS5 全文搜索所有会话。

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

```bash
# 快速运行
docker run -it magicwubiao/go-magic

# Docker Compose（包含可选的 Redis 和 PostgreSQL）
docker compose up -d
```

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
```

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
| `/mode [chat|coding]` | | 切换 Agent 模式 |
| `/personality [name]` | `/persona`, `/tone` | 设置 Agent 人格 |
| `/tools [category]` | | 列出可用工具 |
| `/skills [name]` | `/skill` | 列出可用技能 |
| `/status` | | 显示系统状态 |
| `/version` | `/ver` | 显示版本 |
| `/usage [--days N]` | | 显示 Token 使用量 |
| `/insights [--days N]` | `-d` | 获取使用洞察 |
| `/sessions [list|search]` | `/session` | 列出会话 |
| `/sethome [session_id]` | | 设置消息网关的主会话 |
| `/context [add|remove|list]` | `/ctx` | 管理上下文文件 |
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

功能：
- 实时聊天对话，支持流式响应
- 会话管理（创建、搜索、恢复）
- Provider 和模型配置，支持热加载
- 技能管理
- 插件配置
- Token 使用统计

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
      "models": ["gpt-4", "gpt-3.5-turbo"]
    },
    "anthropic": {
      "api_key": "your-anthropic-api-key",
      "models": ["claude-3-opus-20240229", "claude-3-sonnet-20240229"]
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
| `GO_MAGIC_PROFILE` | Profile 名称（默认：`default`） |

## 从源码构建

### 要求

- Go 1.25+
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
