# go-magic

**Magic Agent** -- A high-performance, ultra-lightweight AI Agent framework written in Go.

[![Go Version](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](https://go.dev)
[![Version](https://img.shields.io/badge/version-v0.4.14-green)](https://github.com/magicwubiao/go-magic/releases)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## Overview

go-magic is a full-featured AI Agent framework that combines a powerful Go backend with a modern Vue 3/TypeScript web dashboard. It supports 22+ AI providers, ships a built-in TUI (BubbleTea), and offers extensive tooling for file operations, code execution, web browsing, and more.

## Features

### Multi-Provider Support (22+)

DeepSeek, OpenAI, Anthropic, Gemini, Ollama, vLLM, Groq, 硅基流动, 智谱GLM, 通义千问, 文心一言, MiniMax, MiMo, 腾讯混元, 豆包(火山引擎), Moonshot (Kimi), OpenRouter, Together AI, Mistral AI, Cohere, Perplexity, and any OpenAI-compatible endpoint.

### Multi-Model per Provider

Each provider can configure multiple models. The first model in the array is the current active model. Switch models instantly without restart (hot-reload).

### TUI Interface

Built with [BubbleTea](https://github.com/charmbracelet/bubbletea), featuring multi-line input, Markdown rendering, streaming output, and slash commands.

### Web Dashboard

Vue 3/TypeScript frontend with:
- Real-time chat with streaming responses
- Session management (create, search, resume)
- Provider and model configuration with hot-reload
- Skills management
- Cron job scheduling
- Kanban board
- Token usage dashboard with budget alerts
- Group chat for multi-agent conversations

### Coding Mode

A dedicated mode with relaxed permissions, longer timeouts, and support for Python/Node.js code execution -- designed for development workflows.

### Tool System

15+ built-in tools organized into toolsets:

| Toolset | Tools |
|---------|-------|
| **Web** | web_search, web_fetch |
| **File** | read_file, write_file, file_edit, list_files, search_in_files |
| **Terminal** | execute_command, terminal, process |
| **Browser** | browser_navigate, browser_snapshot, browser_click, browser_type |
| **Code Execution** | execute_code (Python, Node.js) |
| **Memory** | memory_store, memory_recall |
| **Skills** | skill (list, invoke, info) |
| **MCP** | mcp_* (from connected servers) |

### Skills System

Auto-creation and progressive loading (L0/L1/L2). Skills are learned from usage patterns and can be installed from Skills Hub.

### Cortex (Cognitive Architecture)

A complete agent cognitive system:
- **Memory System**: SOUL.md (personality), USER.md (user profile), snapshot memory, FTS search
- **Perception**: Input analysis, intent recognition, complexity assessment
- **Cognition**: Planning, decision making, LLM-based task decomposition
- **Execution**: Tool invocation and result processing
- **Skill Evolution (GEPA)**: Automatic skill creation from historical patterns

### Messaging Gateway

Connect your agent to external platforms:

Telegram, Discord, Slack, WhatsApp, WeChat, WeCom, DingTalk, Feishu, QQ, LINE, Matrix.

### Bot Mode

Named agent profiles with persistent chats (inspired by Hermes Agent's Bot Mode):

- Each bot is an isolated profile: role title, persona prompt, model/provider pins, a dedicated persistent chat session, and cron routines.
- Bots can message each other via the `message_agent` tool; replies arrive later as background notifications to the sender.
- On any gateway platform, address a specific bot with `/bot <name> <message>` or `@<tag>`.

```bash
magic config set bot_mode.enabled true
magic bots create researcher --title "Research Assistant" --prompt "You find and summarize papers."
magic bots chat researcher "Find recent papers on agent memory"
magic bots routine add researcher daily-digest --schedule "0 9 * * *" --prompt "Summarize yesterday's findings."
```

### MCP Protocol

Connect to external MCP (Model Context Protocol) servers to extend agent capabilities.

### Group Chat

Create group conversations with multiple AI agents. Each agent can have different providers and models.

### Session Management

SQLite-based persistence with FTS5 full-text search across all sessions.

### Token Usage Statistics

Track API consumption with detailed statistics:
- Real-time usage dashboard in Web Dashboard
- CLI command: `magic usage`
- Monthly budget with alert thresholds
- Per-model and per-session breakdown

### Sensitive Data Redaction

Automatic redaction of API keys, tokens, passwords, and other sensitive information in logs and outputs.

## Quick Start

### Download Release

Download the latest binary from [GitHub Releases](https://github.com/magicwubiao/go-magic/releases):

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
# Quick run
docker run -it magicwubiao/go-magic

# Docker Compose (includes optional Redis and PostgreSQL)
docker compose up -d
```

### One-Line Install (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
```

### First Run

```bash
# Interactive setup wizard
magic setup

# Start chatting
magic chat

# Start web dashboard
magic server
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `magic chat` | Start interactive TUI chat |
| `magic server` | Start web dashboard server |
| `magic usage [-d N]` | Show token usage statistics |
| `magic stats` | Show system statistics |
| `magic config` | Manage configuration |
| `magic skills` | Manage skills |
| `magic cron` | Manage scheduled tasks |
| `magic kanban` | Kanban board management |
| `magic gateway` | Start messaging gateway |
| `magic mcp` | Manage MCP servers |
| `magic logs` | View logs |
| `magic doctor` | Diagnose issues |
| `magic backup` | Backup data |

## TUI Slash Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `/help [command]` | `/?` | Show help |
| `/commands [category]` | `/cmds` | List all commands |
| `/new` | `/reset` | Start a new conversation |
| `/clear` | | Clear chat history |
| `/compress` | | Compress context window |
| `/retry` | | Retry last response |
| `/undo` | | Undo last action |
| `/export [format]` | `/save` | Export conversation |
| `/model [provider:model]` | `/m` | Change the AI model |
| `/mode [chat coding]` | | Switch agent mode |
| `/personality [name]` | `/persona`, `/tone` | Set agent personality |
| `/tools [category]` | | List available tools |
| `/skills [name]` | `/skill` | List available skills |
| `/status` | | Show system status |
| `/version` | `/ver` | Show version |
| `/usage` | | Show token usage |
| `/sessions [list search]` | `/session` | List sessions |
| `/sethome [session_id]` | | Set home session for messaging |
| `/stop` | `/cancel` | Stop current operation |

## Coding Mode

Switch to coding mode for development tasks:

```
/mode coding
```

| Feature | Chat Mode | Coding Mode |
|---------|-----------|-------------|
| File write permissions | Restricted | Relaxed |
| Command execution timeout | 30s | 300s |
| Code execution (Python/Node) | Disabled | Enabled |
| Shell access | Limited | Full |
| Auto-approve tools | No | Yes |

Switch back with `/mode chat`.

### Default Mode Configuration

Set the default startup mode in your config file:

```json
{
  "chat_mode": "coding"
}
```

Available values: `"chat"` (default) or `"coding"`.

## Web Dashboard

Start the web server:

```bash
magic server
```

Then open `http://localhost:5000` in your browser.

Features:
- Real-time chat with streaming responses
- Session management (create, search, resume)
- Provider and model configuration with hot-reload
- Skills management
- Cron job scheduling
- Kanban board
- Token usage dashboard with budget tracking
- Group chat for multi-agent conversations

## Token Usage Tracking

Track your API consumption:

```bash
# Show today's usage
magic usage

# Show last 7 days
magic usage -d 7

# Show last 30 days
magic usage -d 30
```

In the Web Dashboard, navigate to the Usage page to see:
- Today's statistics
- Daily trend chart
- Monthly breakdown
- Budget progress bar with alerts
- Top models and sessions

## Configuration

Create or edit `~/.magic/config.json`:

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

### Multi-Model Configuration

Each provider supports multiple models:

```json
"providers": {
  "deepseek": {
    "api_key": "your-api-key",
    "models": ["deepseek-chat", "deepseek-coder", "deepseek-reasoner"]
  }
}
```

- The first model in the array is the current active model
- Switch models instantly from Web Dashboard (no restart needed)
- Use `/model provider:model` in TUI to switch

### Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENAI_API_KEY` | OpenAI API key |
| `DEEPSEEK_API_KEY` | DeepSeek API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `GOOGLE_API_KEY` | Google/Gemini API key |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `GO_MAGIC_HOME` | Config directory (default: `~/.magic`) |
| `GO_MAGIC_PROFILE` | Profile name (default: `default`) |

## Building from Source

### Requirements

- Go 1.25+
- Node.js 20+ (for web dashboard)

### Build for Current Platform

```bash
make build
```

### Cross-Platform Build

```bash
# All common platforms (Linux, macOS, Windows)
make build-all

# All supported platforms
make build-cross

# Specific platform
make build-linux
make build-macos
make build-windows
```

### Supported Platforms

| OS | Architectures |
|----|---------------|
| Linux | amd64, arm64, armv6, riscv64, ppc64le, s390x |
| macOS | amd64, arm64 |
| Windows | amd64, arm64, 386 |
| BSD | freebsd, openbsd, netbsd |

## Download

Get the latest version from [GitHub Releases](https://github.com/magicwubiao/go-magic/releases).

## License

MIT License -- see [LICENSE](LICENSE) for details.
