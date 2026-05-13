# Go Magic

A high-performance, ultra-lightweight AI Agent framework written in Go, inspired by [Nous Research's hermes-agent](https://github.com/NousResearch/hermes-agent).

## Features

| Feature | Description |
|---------|-------------|
| **Multi-Provider Support** | OpenAI, DeepSeek, Huoshan, Anthropic, Zhipu, Kimi, MiniMax, DashScope, OpenRouter, Ollama, vLLM |
| **Cortex Architecture** | Three-layer cognitive system with six self-evolution mechanisms |
| **Tool System** | 15+ built-in tools with extensible plugin framework |
| **Gateway** | Multi-platform messaging (Telegram, Discord, WhatsApp, Signal, Slack, etc.) |
| **Skills System** | Auto-creation, progressive loading (L0/L1/L2), Skills Hub integration |
| **MCP Protocol** | Connect to external MCP servers for extended capabilities |
| **Session Management** | SQLite-based persistence with FTS5 full-text search |
| **Voice Mode** | TTS/STT with multiple provider support |
| **Vision** | Image understanding with multi-model support |
| **Multi-Platform** | Linux, macOS, Windows, FreeBSD, Docker |

## Quick Start

### Installation

```bash
# Install via Go
go install github.com/magicwubiao/go-magic/cmd/magic@latest

# Or clone and build
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
make build
```

### One-Click Install (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
```

### Docker

```bash
docker run -it magicwubiao/go-magic
```

## Usage

```bash
# Interactive chat
magic chat

# Agent mode with parallel execution
magic agent

# Voice interaction
magic voice listen
magic voice speak "Hello world"

# Vision image analysis
magic vision analyze image.png

# REPL mode
magic repl
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `magic chat` | Interactive chat session |
| `magic agent` | Agent mode with task planning |
| `magic repl` | REPL shell |
| `magic voice` | Voice interaction (listen/speak/test) |
| `magic vision` | Image understanding (analyze/compare) |
| `magic config` | Configuration management |
| `magic skills` | Skills management |
| `magic plugin` | Plugin system |
| `magic session` | Session management |
| `magic gateway` | Messaging gateway |
| `magic mcp` | MCP server management |
| `magic doctor` | Diagnostics |
| `magic update` | Auto-update |

## Configuration

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

## Tool System

| Toolset | Tools |
|---------|-------|
| **web** | web_search, web_extract |
| **file** | read_file, write_file, file_edit, list_files, search_in_files |
| **terminal** | execute_command, terminal, process |
| **browser** | browser_navigate, browser_snapshot, browser_click, browser_type |
| **memory** | memory_store, memory_recall |
| **skills** | skill_list, skill_view, skill_manage |
| **code_execution** | execute_code |
| **delegation** | delegate_task, poll_task |
| **homeassistant** | ha_list_entities, ha_get_state, ha_call_service |
| **mcp** | mcp_* (from connected servers) |

## Architecture

### Three-Layer Cognitive System

```
┌──────────────────────────────────────────────────────┐
│  Layer 1: Perception                                 │
│  Intent Classification → Complexity Assessment       │
├──────────────────────────────────────────────────────┤
│  Layer 2: Cognition                                  │
│  Task Planning → DAG Management → Sub-agent Decisions│
├──────────────────────────────────────────────────────┤
│  Layer 3: Execution                                  │
│  Checkpoint/Resume → Result Validation               │
└──────────────────────────────────────────────────────┘
```

### Six Self-Evolution Systems

| System | Description |
|--------|-------------|
| **Message Trigger** | Detects conversation turns, triggers Nudge signals |
| **Nudge System** | Async background review without blocking |
| **Background Review** | Analyzes patterns, generates skill drafts |
| **Frozen Snapshot** | 90% API cost reduction via prefix caching |
| **FTS Memory** | Full-text search across sessions |
| **Skill Evolution** | Progressive disclosure, learns from usage |

## Plugin System

```bash
# Discover plugins
magic plugin discover

# Search plugins
magic plugin search <query>

# Install plugin
magic plugin install <plugin-id>

# Enable/disable/reload
magic plugin enable <id>
magic plugin disable <id>
magic plugin reload <id>

# Check updates
magic plugin update
magic plugin check
```

## Skills System

```bash
# List skills
magic skills list

# Show skill details
magic skills show <name>

# Create new skill
magic skills create <name>

# Install from Skills Hub
magic skills hub install <name>

# Progressive loading
magic skills view <name> --level 0  # List only
magic skills view <name> --level 1  # Full content
magic skills view <name> --level 2  # With references
```

## Messaging Gateway

```bash
# Setup gateway
magic gateway setup

# Start gateway
magic gateway start

# Configure platforms
magic gateway config telegram --token <token>
magic gateway config discord --bot-token <token>
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENAI_API_KEY` | OpenAI API key |
| `DEEPSEEK_API_KEY` | DeepSeek API key |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `GO_MAGIC_HOME` | Config directory (default: ~/.magic) |
| `GO_MAGIC_PROFILE` | Profile name (default: default) |

## Building

### Requirements
- Go 1.21+
- Node.js 18+ (for web dashboard)

### Build for Current Platform

```bash
# Linux/macOS
./scripts/build.sh

# Windows
.\scripts\windows\build.ps1
```

### Development Build

```bash
# Build web assets first
cd web && npm install && npm run build && cd ..

# Copy web assets for embedding
cp -r web/dist internal/server/dist

# Build Go binary
go build -o magic ./cmd/magic
```

### Cross-Platform Build

```bash
# Build for all platforms (includes web embedding)
./scripts/build.sh all

# Build specific targets
./scripts/build.sh go      # Build Go binaries only
./scripts/build.sh web     # Build web app only
./scripts/build.sh docker  # Build Docker images
```

### Build Output

The build script will automatically:
1. Build the React web dashboard
2. Copy web assets to `internal/server/dist` for embedding
3. Compile Go binary with embedded frontend
4. Create platform-specific packages

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

Built with inspiration from [hermes-agent](https://github.com/NousResearch/hermes-agent) by [Nous Research](https://nousresearch.com/).