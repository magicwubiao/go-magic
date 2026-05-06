# Frequently Asked Questions

Common questions and troubleshooting tips for go-magic.

## Table of Contents

- [General](#general)
- [Configuration](#configuration)
- [Tools](#tools)
- [Plugins](#plugins)
- [Skills](#skills)
- [Cortex Architecture](#cortex-architecture)
- [Performance](#performance)
- [Debugging](#debugging)

## General

### What is go-magic?

go-magic is a high-performance, ultra-lightweight Go implementation of the magic AI Agent, inspired by Nous Research's magic-agent. It features a complete three-layer cognitive architecture (Perception, Cognition, Execution) with six self-evolution systems.

### What is the difference between Tool, Plugin, and Skill?

| Component | Purpose | Loading | Example |
|-----------|---------|---------|---------|
| **Tool** | Atomic executable functions | Built-in/static | `read_file`, `web_search` |
| **Plugin** | Dynamic extension modules | Runtime/dynamic | Custom LLM adapters |
| **Skill** | High-level task templates | Lazy/progressive | Code generation, debugging |

See [GLOSSARY.md](GLOSSARY.md) for detailed definitions.

### Which LLM providers are supported?

- OpenAI (GPT-4, GPT-3.5)
- DeepSeek
- Huoshan (ByteDance)
- Anthropic (Claude)
- Zhipu AI
- Kimi (Moonshot)
- MiniMax
- DashScope (Alibaba)
- OpenRouter
- Ollama (local)
- vLLM

## Configuration

### How do I configure the LLM provider?

```bash
# Set provider
magic config set provider openai

# Set API key
magic config set providers.openai.api_key YOUR_KEY

# Set model (optional)
magic config set model gpt-4

# Or use a different provider
magic config set provider anthropic
magic config set providers.anthropic.api_key YOUR_ANTHROPIC_KEY
```

### Where is the configuration file?

- Default: `~/.magic/config.json`
- Custom: Set via `--config` flag or `MAGIC_CONFIG` environment variable

### How do I enable Cortex mode?

```bash
magic config set cortex.enabled true
magic config set cortex.frozen_snapshot true
```

### How do I reset configuration to defaults?

```bash
magic config reset
```

## Tools

### How do I enable/disable specific tools?

```bash
# Enable specific tools
magic config set tools.enabled '["read_file", "write_file", "web_search"]'

# Disable all tools except defaults
magic config set tools.enabled '["read_file"]'
```

### How do I create a custom tool?

See [TOOL_DEVELOPMENT.md](TOOL_DEVELOPMENT.md) for detailed instructions.

### Why are tools not executing in parallel?

Tools execute in parallel only when they have no dependencies. The DAG (Directed Acyclic Graph) in the Cognition layer determines execution order based on task dependencies.

## Plugins

### How do I load a plugin?

```bash
# Discover available plugins
magic plugin discover

# Load a plugin
magic plugin load /path/to/plugin.so

# List loaded plugins
magic plugin list
```

### How do I create a plugin?

See [PLUGIN_DEVELOPMENT.md](PLUGIN_DEVELOPMENT.md) for plugin development guide.

### Can I unload a plugin at runtime?

Yes:

```bash
magic plugin unload my-plugin
```

## Skills

### What is the Skill Auto-Evolution system?

Skills start at Level 0 (minimal visibility) and progressively disclose more information based on usage patterns. This reduces API costs while providing contextual assistance.

### How do I install a new skill?

```bash
# List available skills
magic skills list

# Install a skill
magic skills install coding-assistant

# Search for skills
magic skills search python
```

### How do I create a custom skill?

See the Skills framework documentation for skill creation guide.

## Cortex Architecture

### What is the Perception layer?

The Perception layer analyzes user input to understand:
- Intent (7 types: task, question, chat, system, etc.)
- Complexity (simple, moderate, advanced)
- Entities and key information
- Noise/non-essential content

### What is the Cognition layer?

The Cognition layer plans task execution:
- Task decomposition into subtasks
- DAG (Directed Acyclic Graph) dependency management
- Adaptive max turns calculation
- Sub-agent decisions for parallel execution

### What is the Execution layer?

The Execution layer handles task completion:
- Checkpoint/resume support for long-running tasks
- Result validation
- Progress tracking
- Error recovery

### What are the Six Self-Evolution Systems?

1. **Message Trigger**: Detects conversation turns, triggers Nudge signals
2. **Nudge System**: Async background review without blocking user
3. **Background Review**: Analyzes patterns, generates skill drafts
4. **Frozen Snapshot**: Protects prefix cache, reduces API costs by 90%
5. **FTS Memory**: Full-text search across sessions with BM25 ranking
6. **Skill Auto-Evolution**: Progressive disclosure (Level 0-2)

### How does Frozen Snapshot reduce costs?

When enabled, the system caches the conversation prefix. Subsequent turns only send the new user message plus the cached prefix, dramatically reducing token usage.

```
Without Frozen Snapshot: ~$0.20/turn × 100 turns = $20.00
With Frozen Snapshot:    ~$0.02/turn × 100 turns = $2.00
```

## Performance

### How do I improve response times?

1. **Enable parallel tool execution**: Ensure tools have no dependencies
2. **Use cached responses**: Enable `cortex.frozen_snapshot`
3. **Reduce context**: Enable compression with `agent.compression_enabled`
4. **Use faster models**: Switch to lower-latency models for simple tasks

### How do I reduce API costs?

1. Enable Frozen Snapshot
2. Use context compression
3. Adjust max turns based on task complexity
4. Enable Skill Auto-Evolution

### How much memory does it use?

Typical memory usage:
- Light usage: 100-200 MB
- Heavy usage: 500-800 MB
- With large context: 1-2 GB

## Debugging

### How do I enable debug logging?

```bash
# Via CLI flag
magic chat --debug

# Or via config
magic config set log_level debug
```

### How do I check system health?

```bash
magic health
```

### How do I run diagnostics?

```bash
magic doctor
```

### Why is my command not executing?

1. Check if the command is in the whitelist
2. Verify command permissions
3. Check for dangerous patterns (blocked by security)
4. Review logs with `--debug`

### Why are tools not working?

1. Verify tool is enabled in config
2. Check tool permissions
3. Review tool-specific requirements
4. Use `magic tools show <name>` for tool details

## Troubleshooting

### "Command not found" after installation

Ensure the binary is in your PATH:
```bash
export PATH=$PATH:$(go env GOPATH)/bin
# Or
export PATH=$PATH:/usr/local/bin
```

### "Permission denied" when running

Make the binary executable:
```bash
chmod +x /path/to/magic
```

### Configuration not loading

1. Check config file syntax: `magic config validate`
2. Verify file permissions: `chmod 600 ~/.magic/config.json`
3. Check environment variables

### Build errors

```bash
# Clean and rebuild
go clean -cache
go mod tidy
make build
```
