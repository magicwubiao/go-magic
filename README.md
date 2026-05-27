# go-magic

**Magic Agent** -- A high-performance, ultra-lightweight AI Agent framework written in Go.

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8)](https://go.dev)
[![Version](https://img.shields.io/badge/version-v0.3.1-green)](https://github.com/magicwubiao/go-magic/releases)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## Overview

go-magic is a full-featured AI Agent framework that combines a powerful Go backend with a modern React/TypeScript web dashboard. It supports 20+ AI providers, ships a built-in TUI (BubbleTea), and offers extensive tooling for file operations, code execution, web browsing, and more.

## Features

### Multi-Provider Support (20+)

DeepSeek, OpenAI, Anthropic, Gemini, Ollama, OpenRouter, Groq, Mistral, Cohere, Perplexity, Together, DashScope, Kimi, MiniMax, Zhipu, Doubao (Volcano), Wenxin, Moonshot, Hunyuan, Mimo, and any OpenAI-compatible endpoint.

### TUI Interface

Built with [BubbleTea](https://github.com/charmbracelet/bubbletea), featuring multi-line input, Markdown rendering, streaming output, and slash commands.

### Coding Mode

A dedicated mode with relaxed permissions, longer timeouts, and support for Python/Node.js code execution -- designed for development workflows.

### Web Dashboard

React/TypeScript frontend with real-time chat, session management, and configuration management. Embedded into the binary for single-file deployment.

### Tool System

15+ built-in tools organized into toolsets:

| Toolset | Tools |
|---------|-------|
| **Web** | web_search, web_extract |
| **File** | read_file, write_file, file_edit, list_files, search_in_files |
| **Terminal** | execute_command, terminal, process |
| **Browser** | browser_navigate, browser_snapshot, browser_click, browser_type |
| **Code Execution** | execute_code (Python, Node.js) |
| **Memory** | memory_store, memory_recall |
| **Skills** | skill_list, skill_view, skill_manage |
| **MCP** | mcp_* (from connected servers) |

### Skills System

Auto-creation and progressive loading (L0/L1/L2). Skills are learned from usage patterns and can be shared via Skills Hub.

### Messaging Gateway

Connect your agent to external platforms:

Telegram, Discord, Slack, WhatsApp, WeChat, WeCom, DingTalk, Feishu, QQ, LINE, Matrix.

### MCP Protocol

Connect to external MCP (Model Context Protocol) servers to extend agent capabilities.

### Session Management

SQLite-based persistence with FTS5 full-text search across all sessions.

### CI/CD

GitHub Actions workflow for automatic multi-platform compilation and release.

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

# With Docker Compose (includes optional Redis and PostgreSQL)
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
```

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
| `/mode [chat\|coding]` | | Switch agent mode |
| `/personality [name]` | `/persona`, `/tone` | Set agent personality |
| `/tools [category]` | | List available tools |
| `/skills [name]` | `/skill` | List available skills |
| `/status` | | Show system status |
| `/version` | `/ver` | Show version |
| `/usage [--days N]` | | Show token usage |
| `/insights [--days N]` | `-d` | Get usage insights |
| `/sessions [list\|search]` | `/session` | List sessions |
| `/sethome [session_id]` | | Set home session for messaging |
| `/context [add\|remove\|list]` | `/ctx` | Manage context files |
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

Set the default startup mode in your config file so you don't need to switch manually every time:

```json
{
  "chat_mode": "coding"
}
```

Available values: `"chat"` (default) or `"coding"`.

You can also configure this via Web Dashboard → Config → General → Chat Mode.

After startup, you can still switch temporarily with `/mode chat` or `/mode coding`.

## Web Dashboard

Start the web server:

```bash
magic server
```

Then open `http://localhost:8642` in your browser.

Features:
- Real-time chat with streaming responses
- Session management (create, search, resume)
- Provider and model configuration
- Tool and skill management
- Token usage dashboard

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
      "model": "deepseek-chat"
    },
    "openai": {
      "api_key": "your-openai-api-key",
      "base_url": "https://api.openai.com/v1",
      "model": "gpt-4"
    },
    "anthropic": {
      "api_key": "your-anthropic-api-key",
      "model": "claude-3-opus-20240229"
    },
    "ollama": {
      "base_url": "http://localhost:11434",
      "model": "llama3"
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

### GitHub Actions

Releases are built automatically via GitHub Actions when a version tag is pushed:

```bash
git tag v0.3.1
git push origin v0.3.1
```

The workflow builds binaries for Linux (amd64/arm64), macOS (amd64/arm64), and Windows (amd64), creates archives, and publishes a GitHub Release.

## Download

Get the latest version from [GitHub Releases](https://github.com/magicwubiao/go-magic/releases).

## License

MIT License -- see [LICENSE](LICENSE) for details.
