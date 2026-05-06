# Configuration Guide

Complete configuration reference for go-magic.

## Table of Contents

- [Configuration File](#configuration-file)
- [Provider Configuration](#provider-configuration)
- [Cortex Configuration](#cortex-configuration)
- [Tools Configuration](#tools-configuration)
- [Agent Configuration](#agent-configuration)
- [Gateway Configuration](#gateway-configuration)
- [Environment Variables](#environment-variables)

## Configuration File

Default location: `~/.magic/config.json`

```bash
# View current config
magic config list

# Set a value
magic config set <key> <value>

# Reset to defaults
magic config reset
```

## Provider Configuration

### Basic Provider Setup

```json
{
  "provider": "openai",
  "model": "gpt-4",
  "providers": {
    "openai": {
      "api_key": "sk-...",
      "base_url": "https://api.openai.com/v1"
    }
  }
}
```

### Supported Providers

| Provider | Config Key | API Key Env Var |
|----------|------------|-----------------|
| OpenAI | `providers.openai` | `OPENAI_API_KEY` |
| DeepSeek | `providers.deepseek` | `DEEPSEEK_API_KEY` |
| Anthropic | `providers.anthropic` | `ANTHROPIC_API_KEY` |
| Zhipu | `providers.zhipu` | `ZHIPU_API_KEY` |
| Kimi | `providers.moonshot` | `MOONSHOT_API_KEY` |
| MiniMax | `providers.minimax` | `MINIMAX_API_KEY` |
| DashScope | `providers.dashscope` | `DASHSCOPE_API_KEY` |
| Ollama | `providers.ollama` | - |
| vLLM | `providers.vllm` | - |

### Provider Examples

#### OpenAI

```json
{
  "provider": "openai",
  "model": "gpt-4",
  "providers": {
    "openai": {
      "api_key": "sk-...",
      "base_url": "https://api.openai.com/v1"
    }
  }
}
```

#### Anthropic

```json
{
  "provider": "anthropic",
  "model": "claude-3-opus-20240229",
  "providers": {
    "anthropic": {
      "api_key": "sk-ant-...",
      "base_url": "https://api.anthropic.com"
    }
  }
}
```

#### Ollama (Local)

```json
{
  "provider": "ollama",
  "model": "llama2",
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434"
    }
  }
}
```

## Cortex Configuration

### Enable Cortex

```json
{
  "cortex": {
    "enabled": true,
    "frozen_snapshot": true,
    "nudge_threshold": 15,
    "memory": {
      "enabled": true,
      "fts_enabled": true
    },
    "skill_evolution": {
      "enabled": true,
      "auto_level_up": true
    }
  }
}
```

### Configuration Options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `cortex.enabled` | bool | `true` | Enable Cortex architecture |
| `cortex.frozen_snapshot` | bool | `true` | Enable cost optimization |
| `cortex.nudge_threshold` | int | `15` | Turns before nudge |
| `cortex.memory.enabled` | bool | `true` | Enable memory system |
| `cortex.memory.fts_enabled` | bool | `true` | Enable full-text search |
| `cortex.skill_evolution.enabled` | bool | `true` | Enable skill auto-evolution |
| `cortex.skill_evolution.auto_level_up` | bool | `true` | Auto level up skills |

## Tools Configuration

### Basic Tools Setup

```json
{
  "tools": {
    "enabled": ["read_file", "write_file", "web_search", "execute_command"],
    "parallel": true,
    "timeout_seconds": 30
  }
}
```

### Tool Categories

| Category | Tools |
|----------|-------|
| File | `read_file`, `write_file`, `list_files`, `search_in_files` |
| Web | `web_search`, `web_extract`, `browser_*` |
| Execution | `execute_command`, `python_execute`, `node_execute` |
| Memory | `memory_store`, `memory_recall` |

### Command Whitelist

```json
{
  "tools": {
    "command_whitelist": ["git", "ls", "cat", "grep", "find", "curl"],
    "dangerous_patterns_blocked": true,
    "injection_detection": true
  }
}
```

### Tools Configuration Options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `tools.enabled` | []string | all | Enabled tool names |
| `tools.parallel` | bool | `true` | Enable parallel execution |
| `tools.timeout_seconds` | int | `30` | Default timeout |
| `tools.command_whitelist` | []string | all | Allowed commands |
| `tools.dangerous_patterns_blocked` | bool | `true` | Block dangerous patterns |
| `tools.injection_detection` | bool | `true` | Detect injection attacks |

## Agent Configuration

### Basic Agent Setup

```json
{
  "agent": {
    "max_turns": 10,
    "max_context_length": 200000,
    "compression_enabled": true,
    "compression_ratio": 0.7,
    "temperature": 0.7,
    "top_p": 0.9
  }
}
```

### Agent Configuration Options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `agent.max_turns` | int | `10` | Maximum conversation turns |
| `agent.max_context_length` | int | `200000` | Max context tokens |
| `agent.compression_enabled` | bool | `true` | Enable history compression |
| `agent.compression_ratio` | float | `0.7` | Compression ratio |
| `agent.temperature` | float | `0.7` | LLM temperature |
| `agent.top_p` | float | `0.9` | LLM top_p |
| `agent.approval_required` | bool | `false` | Require approval for actions |
| `agent.auto_continue` | bool | `true` | Auto continue tasks |

## Gateway Configuration

### Enable Gateway

```json
{
  "gateway": {
    "enabled": true,
    "port": 8080,
    "platforms": {
      "telegram": {
        "enabled": true,
        "bot_token": "..."
      },
      "discord": {
        "enabled": true,
        "bot_token": "...",
        "guild_id": "..."
      }
    }
  }
}
```

### Supported Platforms

| Platform | Config Key | Required |
|----------|------------|----------|
| Telegram | `gateway.platforms.telegram` | Bot token |
| Discord | `gateway.platforms.discord` | Bot token |
| WeChat | `gateway.platforms.wechat` | AppID, AppSecret |
| QQ | `gateway.platforms.qq` | Bot token |
| DingTalk | `gateway.platforms.dingtalk` | AppKey, AppSecret |
| Feishu | `gateway.platforms.feishu` | AppID, AppSecret |
| WeCom | `gateway.platforms.wecom` | CorpID, AgentID |

## Security Configuration

### PII Redaction

```json
{
  "security": {
    "pii_redaction": {
      "enabled": true,
      "patterns": ["email", "phone", "ssn", "credit_card"]
    }
  }
}
```

### API Security

```json
{
  "security": {
    "rate_limit": {
      "enabled": true,
      "requests_per_minute": 60
    },
    "api_key_required": false
  }
}
```

## Session Configuration

### Session Storage

```json
{
  "session": {
    "storage": {
      "type": "sqlite",
      "path": "~/.magic/sessions.db"
    },
    "auto_save": true,
    "max_sessions": 100
  }
}
```

## Logging Configuration

```json
{
  "logging": {
    "level": "info",
    "format": "json",
    "output": "stdout",
    "file": "~/.magic/logs/magic.log",
    "rotation": {
      "enabled": true,
      "max_size": "100MB",
      "max_age": 7,
      "max_backups": 3
    }
  }
}
```

## Environment Variables

### Configuration via Environment

| Variable | Description | Config Equivalent |
|----------|-------------|-------------------|
| `MAGIC_HOME` | Config directory | - |
| `MAGIC_CONFIG` | Config file path | - |
| `MAGIC_LOG_LEVEL` | Log level | `logging.level` |
| `MAGIC_NO_COLOR` | Disable colors | - |
| `MAGIC_PROFILE` | Profile name | - |
| `OPENAI_API_KEY` | OpenAI API key | `providers.openai.api_key` |
| `ANTHROPIC_API_KEY` | Anthropic API key | `providers.anthropic.api_key` |

### Priority Order

1. Command-line flags (highest)
2. Environment variables
3. Configuration file
4. Default values (lowest)

## Complete Example

```json
{
  "provider": "openai",
  "model": "gpt-4",
  "providers": {
    "openai": {
      "api_key": "sk-...",
      "base_url": "https://api.openai.com/v1"
    }
  },
  "cortex": {
    "enabled": true,
    "frozen_snapshot": true,
    "nudge_threshold": 15,
    "memory": {
      "enabled": true,
      "fts_enabled": true
    },
    "skill_evolution": {
      "enabled": true,
      "auto_level_up": true
    }
  },
  "tools": {
    "enabled": ["read_file", "write_file", "web_search", "execute_command"],
    "parallel": true,
    "timeout_seconds": 30,
    "command_whitelist": ["git", "ls", "cat", "grep", "find", "curl"]
  },
  "agent": {
    "max_turns": 10,
    "max_context_length": 200000,
    "compression_enabled": true,
    "compression_ratio": 0.7,
    "temperature": 0.7
  },
  "gateway": {
    "enabled": false
  },
  "logging": {
    "level": "info",
    "format": "json"
  }
}
```
