# go-magic

A high-performance, ultra-lightweight Go implementation of the magic AI Agent, inspired by Nous Research's magic-agent.

## Features

- **Ultra-Lightweight**: Minimal dependencies, designed for efficiency
- **Multi-Provider Support**: OpenAI, DeepSeek, Huoshan, Anthropic, Zhipu, Kimi, MiniMax, DashScope, OpenRouter, Ollama, vLLM
- **Tool System**: Extensible tool framework with 15+ built-in tools
- **Parallel Tool Execution**: Concurrent execution of independent tools for reduced latency
- **Context Compression**: Intelligent history summarization to prevent context overflow
- **Secure Execution**: Command whitelist, injection detection, dangerous pattern blocking
- **Session Management**: Persistent chat sessions with SQLite storage
- **Gateway**: Multi-platform messaging gateway (Telegram, Discord, WeCom, QQ, DingTalk, Feishu, WeChat)
- **Skills System**: Extensible skill framework with auto-creation capabilities
- **MCP Protocol**: Connect to external MCP servers for extended tool capabilities
- **Subagents**: Parallel execution of multiple subagents
- **PII Redaction**: Automatic detection and redaction of sensitive information
- **Voice Mode**: Voice interaction with push-to-talk functionality
- **Health Check**: HTTP health endpoint with detailed status
- **CLI REPL**: Interactive shell with slash commands and colored output

## Cortex Agent Architecture

go-magic implements a complete three-layer cognitive architecture with six self-evolution systems:

```
┌─────────────────────────────────────────────────────────────────────┐
│   Layer 1: Perception → Layer 2: Cognition → Layer 3: Execution     │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐     │
│   │  Intent     │    │  Task       │    │  Checkpoint &       │     │
│   │  Classification    │  Planning  │    │  Resume Support     │     │
│   │  Complexity │    │  DAG       │    │  Result Validation   │     │
│   │  Assessment │    │  Management │    │  Progress Tracking  │     │
│   └─────────────┘    └─────────────┘    └─────────────────────┘     │
└─────────────────────────────────────────────────────────────────────┘

Six Self-Evolution Systems:
┌────────────┐ ┌────────┐ ┌─────────────┐ ┌──────────────┐
│  Message   │ │ Nudge  │ │ Background  │ │  Frozen      │
│  Trigger   │ │ System │ │ Review      │ │  Snapshot    │
└────────────┘ └────────┘ └─────────────┘ └──────────────┘
┌────────────┐ ┌────────────────┐
│  FTS       │ │  Skill Auto-   │
│  Memory    │ │  Evolution     │
└────────────┘ └────────────────┘
```

### Three-Layer Architecture

| Layer | Purpose | Key Features |
|-------|---------|--------------|
| **Perception** | Understand user input | 7 intent types, 3 complexity levels, entity extraction, noise detection |
| **Cognition** | Plan execution | Task decomposition, DAG dependency, adaptive max turns, sub-agent decisions |
| **Execution** | Execute tasks | Checkpoint/resume, result validation, progress tracking |

### Six Self-Evolution Systems

1. **Message Trigger**: Detects conversation turns, triggers Nudge signals
2. **Nudge System**: Async background review without blocking user
3. **Background Review**: Analyzes patterns, generates skill drafts
4. **Frozen Snapshot**: Protects prefix cache, reduces API costs by 90%
5. **FTS Memory**: Full-text search across sessions with BM25 ranking
6. **Skill Auto-Evolution**: Progressive disclosure (Level 0-2), learns from usage

### Cost Optimization

Using frozen snapshots achieves **90% API cost savings**:

```
Without Frozen Snapshot: ~$0.20/turn × 100 turns = $20.00
With Frozen Snapshot:    ~$0.02/turn × 100 turns = $2.00
```

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

### Configuration

```bash
# Set provider
magic config set provider openai

# Set API key
magic config set providers.openai.api_key YOUR_KEY

# Enable Cortex mode (recommended)
magic config set cortex.enabled true

# View configuration
magic config list
```

### Basic Usage

```bash
# Start interactive chat
magic chat

# Run agent mode
magic agent

# Start REPL
magic repl

# Voice interaction
magic voice
```

## Documentation

| Document | Description |
|----------|-------------|
| [Quick Start](docs/QUICKSTART.md) | Get started quickly |
| [Architecture](docs/ARCHITECTURE.md) | Complete architecture overview |
| [API Reference](docs/API.md) | API documentation |
| [Cortex Architecture](docs/CORTEX_ARCHITECTURE_OVERVIEW.md) | Three-layer six-system architecture |
| [Tools](docs/TOOLS.md) | Built-in tools |
| [Plugin System](docs/PLUGIN_SYSTEM.md) | Plugin development |
| [CLI Usage](docs/CLI_USAGE.md) | CLI commands |
| [Configuration](docs/CONFIGURATION.md) | Configuration guide |
| [Development](docs/DEVELOPMENT.md) | Development guide |
| [Deployment](docs/DEPLOYMENT.md) | Deployment guide |
| [FAQ](docs/FAQ.md) | Common questions |
| [Glossary](docs/GLOSSARY.md) | Terminology |

## CLI Commands

### Core Commands

| Command | Description |
|---------|-------------|
| `magic chat` | Start interactive chat session |
| `magic agent` | Run agent mode with parallel execution |
| `magic repl` | Start REPL (Read-Eval-Print Loop) |
| `magic voice` | Voice interaction mode |

### Configuration

| Command | Description |
|---------|-------------|
| `magic config list` | List all configuration |
| `magic config get <key>` | Get config value |
| `magic config set <key> <value>` | Set config value |
| `magic config path` | Show config file path |
| `magic config validate` | Validate configuration |
| `magic config reset` | Reset to defaults |

### Skills Management

| Command | Description |
|---------|-------------|
| `magic skills list` | List all skills |
| `magic skills show <name>` | Show skill details |
| `magic skills search <keyword>` | Search skills |
| `magic skills install <name>` | Install a skill |
| `magic skills create <name>` | Create new skill |
| `magic skills delete <name>` | Delete a skill |
| `magic skills match <input>` | Find matching skills |

### Plugin Management

| Command | Description |
|---------|-------------|
| `magic plugin list` | List loaded plugins |
| `magic plugin discover` | Discover available plugins |
| `magic plugin load <path>` | Load a plugin |
| `magic plugin unload <name>` | Unload a plugin |

### Session Management

| Command | Description |
|---------|-------------|
| `magic session list` | List all sessions |
| `magic session show <id>` | Show session details |
| `magic session delete <id>` | Delete a session |
| `magic session clear` | Clear all sessions |

### Utilities

| Command | Description |
|---------|-------------|
| `magic version` | Show version information |
| `magic health` | Health check |
| `magic status` | System status |
| `magic metrics` | Show system metrics |
| `magic logs` | View logs |
| `magic doctor` | Run diagnostics |

## Configuration

Config file: `~/.magic/config.json`

```json
{
  "provider": "openai",
  "model": "gpt-4",
  "providers": {
    "openai": {
      "api_key": "...",
      "base_url": "https://api.openai.com/v1"
    }
  },
  "cortex": {
    "enabled": true,
    "frozen_snapshot": true,
    "nudge_threshold": 15
  },
  "tools": {
    "enabled": ["web_search", "execute_command"]
  },
  "agent": {
    "max_turns": 10,
    "max_context_length": 200000,
    "compression_enabled": true,
    "compression_ratio": 0.7
  },
  "gateway": {
    "enabled": false,
    "platforms": {}
  }
}
```

## Built-in Tools

### File Tools
| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `write_file` | Write file contents |
| `list_files` | List directory contents |
| `search_in_files` | Search content in files |

### Web Tools
| Tool | Description |
|------|-------------|
| `web_search` | Web search with structured results |
| `web_extract` | Extract readable content from URL |
| `browser_navigate` | Navigate to URL with full browser automation |
| `browser_click` | Click elements on page |
| `browser_type` | Type text into input fields |
| `browser_screenshot` | Take page screenshots |

### Execution Tools
| Tool | Description |
|------|-------------|
| `execute_command` | Secure shell command execution |
| `python_execute` | Execute Python code |
| `node_execute` | Execute Node.js code |

### Memory Tools
| Tool | Description |
|------|-------------|
| `memory_store` | Store information in session memory |
| `memory_recall` | Recall stored information |

## Cortex API Example

```go
import "github.com/magicwubiao/go-magic/internal/cortex"

// Create manager
mgr := cortex.NewManager("/data/cortex")
mgr.Start()

// Process message
mgr.OnUserMessage("Write a Python ETL pipeline")

// Get results
fmt.Println("Intent:", mgr.GetIntent())                    // "task"
fmt.Println("Complexity:", mgr.GetTaskComplexity())         // "advanced"
fmt.Println("Max Turns:", mgr.GetRecommendedMaxTurns())    // 25

// Get execution plan
plan := mgr.GetExecutionPlan()
for _, step := range plan.Steps {
    fmt.Printf("[%d] %s\n", step.ID, step.Description)
}
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) for details.
