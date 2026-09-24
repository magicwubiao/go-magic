# go-magic

**Magic Agent** -- A high-performance, ultra-lightweight AI Agent framework written in Go.

[![Go Version](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](https://go.dev)
[![Version](https://img.shields.io/badge/version-v0.5.9-green)](https://github.com/magicwubiao/go-magic/releases)
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

Telegram, Discord, Slack, WeChat (iLink), WeCom, DingTalk, Feishu, QQ, LINE, Matrix, Microsoft Teams, Google Chat, Email, SMS.

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

Cross-machine peers: DM a bot on another go-magic instance over HTTP(S).

```bash
# on machine B (run the dashboard / gateway so the relay endpoint is up)
magic config set bot_mode.relay_token "shared-secret"   # required for remote peers
# on machine A
magic peer add lab-b http://192.168.1.20:8642 --token shared-secret
magic peer dm lab-b researcher "What's the status of the report?"
# peers live in <magic_home>/peers.json; this machine's id in <magic_home>/instance_id
# without a relay_token, /api/relay/v1/dm only accepts calls from localhost
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

The image runs as a non-root user; `magic server` defaults to port 5000, and the image CMD explicitly switches it to 8642 — so port 8642 must be mapped for the Web UI to be reachable:

```bash
# Quick run (without -p 8642:8642 the Web UI is unreachable from the host)
docker run -it -p 8642:8642 magicwubiao/go-magic

# Docker Compose
docker compose up -d

# Rebuild after frontend changes so a stale dist is not baked into the image
rm -rf internal/server/dist && docker compose build --no-cache
```

Webhook-based gateway platforms (DingTalk / Feishu / Discord, ...) each listen on their **own dedicated port**; only expose the ones you actually use. See [Docker Deployment & Troubleshooting](#docker-deployment--troubleshooting) for the full port table and debugging steps.

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

## Docker Deployment & Troubleshooting

The image is a multi-stage build (Node builds the frontend → Go compiles with `embed` → Alpine runtime) and runs as the non-root user `magic` (uid 1000). The four points below cover most deployment failures, and each was verified against the source.

### 1. Ports must line up (`magic server` defaults to 5000)

| Port | Purpose | Notes |
|------|---------|-------|
| 8642 | API / Web UI | Image CMD hardcodes `server --port 8642` |
| 8643 | Reserved | Nothing listens on it; only registered as a CORS origin |
| 8091 / 8092 | DingTalk / Feishu | webhook callback |
| 8084 / 8085 | Discord / Slack | webhook callback |
| 8087 / 8088 | LINE / Teams | webhook callback |
| 8089 / 8090 | Google Chat / SMS | webhook callback |
| 8080 / 8081 | Gateway loopback ports (API / health) | not exposed outside the container |

These ports are hardcoded constants in the source — no config key or env var overrides them:

- `cmd/magic/server.go` — the `--port` default is `5000`
- `internal/gateway/`: `dingtalk.go`, `feishu.go`, `discord.go`, `slack.go`, `line.go`, `teams.go`, `googlechat.go`, `sms.go` — per-platform `SetCallbackPort(...)`
- `internal/gateway/gateway.go` — `DefaultAPIPort = 8080`, `DefaultHealthPort = 8081`

So when overriding `command:` you must pass `--port 8642` explicitly, otherwise the process listens on 5000 while the mapping points at 8642. Any platform you want callbacks from also needs its port published, or the platform will never reach you.

```bash
docker compose port magic 8642            # host mapping
docker exec go-magic netstat -tlnp        # what actually listens inside
curl -sf http://localhost:8642/api/health
```

### 2. healthcheck and `/api/health`

Both the image and compose use `curl -sf http://localhost:8642/api/health`. The route is registered with a plain `mux.HandleFunc` and the handler never inspects `r.Method`:

```go
// internal/server/server.go
mux.HandleFunc("/api/health", withCORS(s.handleHealth))

// internal/server/health.go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    jsonResponse(w, map[string]string{"status": "healthy"})
}
```

GET and HEAD both return 200, so "`/api/health` does not support HEAD" is not a valid explanation for a false unhealthy. If you do hit one, check startup timing (first-time `~/.magic` initialization) or the probe tool's own behavior instead. `start_period` is 10s in compose and 5s in the image; raise it if cold starts are slow.

```bash
docker inspect --format '{{json .State.Health}}' go-magic | jq
docker logs --tail 100 go-magic
```

### 3. A stale frontend build can get baked into the image

The frontend output directory points straight at Go's embedded directory, and Go packs it in at compile time:

```ts
// web/vite.config.ts
outDir: '../internal/server/dist',
```

```go
// internal/server/server.go
//go:embed dist
```

A leftover local `internal/server/dist` is therefore silently compiled into the binary. The `Dockerfile` avoids this through the ordering of `COPY --from=web-builder`, and `.dockerignore` lists `internal/server/dist/` and `web/dist/` explicitly (a bare `dist/` only matches the context root, not those paths).

If frontend changes don't show up:

```bash
rm -rf internal/server/dist
docker compose build --no-cache
```

### 4. Volume permissions

The container runs as uid 1000 with the `magic-config` volume mounted at `/home/magic/.magic`; if the volume permissions don't match, config writes fail:

```bash
docker exec -u 0 go-magic chown -R 1000:1000 /home/magic/.magic
```

> A `permission denied ... /var/run/docker.sock` error from `docker ps` is a host permission issue: add your user to the `docker` group and log in again — `sudo usermod -aG docker $USER`.

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

The dashboard is protected by a login password you set on first visit. **Credentials travel in request headers only** (`Authorization: Bearer <token>`), never in a URL — for the few requests a browser cannot attach headers to (`<img src>`, `<a href>`, `EventSource`, iframe sub-resources) the frontend first mints a scoped, hard-expiring **signed ticket** via `POST /api/fs/sign` and puts *that* in the URL. The legacy `?token=<login credential>` form has been removed. See [docs/AUTH.en.md](docs/AUTH.en.md) for the credential layers, ticket scopes/TTLs, error codes and the migration table for external scripts.

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
| `GO_MAGIC_CORS_ORIGINS` | Comma-separated CORS allowed origins (for separated web/API deploy) |
| `MAGIC_SKILL_DIR` | Additional skill directory |
| `MAGIC_SESSION_ID` | Bind a CLI process to a specific session ID |

## Building from Source

### Requirements

- Go 1.26+
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
