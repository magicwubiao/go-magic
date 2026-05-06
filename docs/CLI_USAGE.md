# CLI Usage Guide

Complete command-line interface reference for go-magic.

## Table of Contents

- [Global Flags](#global-flags)
- [Core Commands](#core-commands)
- [Configuration Commands](#configuration-commands)
- [Skills Management](#skills-management)
- [Plugin Management](#plugin-management)
- [Session Management](#session-management)
- [Tools](#tools)
- [Utilities](#utilities)
- [Shell Completion](#shell-completion)

## Global Flags

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--verbose` | `-v` | Enable verbose logging | `false` |
| `--debug` | | Enable debug mode | `false` |
| `--config` | | Custom config file path | `~/.magic/config.json` |
| `--output` | `-o` | Output format (text, json, yaml, table) | `text` |
| `--no-color` | | Disable colored output | `false` |
| `--magic-home` | | Custom magic home directory | `~/.magic` |
| `--profile` | `-p` | Configuration profile | `default` |

## Core Commands

### magic chat

Start an interactive chat session.

```bash
magic chat [flags]

Flags:
  --session string    Session ID to resume
  --model string      Override model
  --no-history       Disable history saving
```

Examples:
```bash
magic chat
magic chat --session abc123
magic chat --model gpt-4
```

### magic agent

Run agent mode with parallel execution.

```bash
magic agent [flags]

Flags:
  --task string       Task description
  --parallel          Enable parallel tool execution
  --max-turns int     Maximum turns (default: 10)
```

Examples:
```bash
magic agent --task "Write a Python web server"
magic agent --task "Deploy to production" --parallel
```

### magic repl

Start REPL (Read-Eval-Print Loop).

```bash
magic repl [flags]

Flags:
  --prompt string     Custom prompt (default: ">>> ")
  --history int       History size (default: 1000)
```

Examples:
```bash
magic repl
magic repl --prompt "magic> "
```

### magic voice

Voice interaction mode.

```bash
magic voice [flags]

Flags:
  --push-to-talk      Use push-to-talk mode
  --continuous        Continuous voice recognition
  --wake-word string  Wake word (default: "magic")
```

Examples:
```bash
magic voice
magic voice --push-to-talk
```

## Configuration Commands

### magic config list

List all configuration.

```bash
magic config list [flags]

Flags:
  --format string    Output format (text, json, yaml) (default: "text")
  --sensitive        Show sensitive values
```

Examples:
```bash
magic config list
magic config list --format json
magic config list --sensitive
```

### magic config get

Get a configuration value.

```bash
magic config get <key> [flags]
```

Examples:
```bash
magic config get provider
magic config get cortex.enabled
magic config get providers.openai.api_key
```

### magic config set

Set a configuration value.

```bash
magic config set <key> <value> [flags]
```

Examples:
```bash
magic config set provider openai
magic config set model gpt-4
magic config set cortex.enabled true
magic config set cortex.frozen_snapshot true
magic config set "tools.enabled" '["read_file", "write_file"]'
```

### magic config path

Show configuration file path.

```bash
magic config path
```

### magic config validate

Validate configuration file.

```bash
magic config validate
```

### magic config reset

Reset to default configuration.

```bash
magic config reset [flags]

Flags:
  --force    Skip confirmation
```

## Skills Management

### magic skills list

List all skills.

```bash
magic skills list [flags]

Flags:
  --level int      Filter by level (0-2)
  --installed      Show only installed skills
```

Examples:
```bash
magic skills list
magic skills list --level 1
```

### magic skills show

Show skill details.

```bash
magic skills show <name> [flags]

Flags:
  --source    Show skill source code
```

Examples:
```bash
magic skills show coding-assistant
magic skills show debugging --source
```

### magic skills search

Search skills by keyword.

```bash
magic skills search <keyword> [flags]

Flags:
  --limit int    Maximum results (default: 10)
```

Examples:
```bash
magic skills search python
magic skills search "web development" --limit 20
```

### magic skills install

Install a skill.

```bash
magic skills install <name> [flags]

Flags:
  --source string    Install from source
  --force            Overwrite existing
```

Examples:
```bash
magic skills install coding-assistant
magic skills install my-skill --source ./skills/my-skill
```

### magic skills create

Create a new skill.

```bash
magic skills create <name> [flags]

Flags:
  --template string    Use template
  --description string    Skill description
```

Examples:
```bash
magic skills create my-custom-skill --template basic
```

### magic skills delete

Delete a skill.

```bash
magic skills delete <name> [flags]

Flags:
  --force    Skip confirmation
```

### magic skills match

Find skills matching input.

```bash
magic skills match <input>
```

Examples:
```bash
magic skills match "How do I debug Python?"
```

## Plugin Management

### magic plugin list

List loaded plugins.

```bash
magic plugin list [flags]

Flags:
  --verbose    Show detailed info
```

Examples:
```bash
magic plugin list
magic plugin list --verbose
```

### magic plugin discover

Discover available plugins.

```bash
magic plugin discover [flags]

Flags:
  --path string    Custom plugin directory
```

Examples:
```bash
magic plugin discover
magic plugin discover --path ./plugins
```

### magic plugin load

Load a plugin.

```bash
magic plugin load <path> [flags]

Flags:
  --config string    Plugin config file
```

Examples:
```bash
magic plugin load ./my-plugin.so
```

### magic plugin unload

Unload a plugin.

```bash
magic plugin unload <name> [flags]

Flags:
  --force    Force unload
```

Examples:
```bash
magic plugin unload my-plugin
```

## Session Management

### magic session list

List all sessions.

```bash
magic session list [flags]

Flags:
  --limit int    Maximum sessions (default: 20)
  --format string    Output format
```

Examples:
```bash
magic session list
magic session list --limit 50
```

### magic session show

Show session details.

```bash
magic session show <id> [flags]

Flags:
  --messages    Include messages
  --stats       Show statistics
```

Examples:
```bash
magic session show abc123
magic session show abc123 --messages
```

### magic session delete

Delete a session.

```bash
magic session delete <id> [flags]

Flags:
  --force    Skip confirmation
```

### magic session clear

Clear all sessions.

```bash
magic session clear [flags]

Flags:
  --force    Skip confirmation
```

## Tools

### magic tools list

List available tools.

```bash
magic tools list [flags]

Flags:
  --enabled    Show only enabled tools
  --category string    Filter by category
```

Examples:
```bash
magic tools list
magic tools list --enabled
magic tools list --category web
```

### magic tools show

Show tool details.

```bash
magic tools show <name>
```

Examples:
```bash
magic tools show read_file
magic tools show web_search
```

### magic tools exec

Execute a tool.

```bash
magic tools exec <name> [args...]
```

Examples:
```bash
magic tools exec read_file ./README.md
magic tools exec web_search "Go programming"
```

## Utilities

### magic version

Show version information.

```bash
magic version [flags]

Flags:
  --short    Show only version number
  --json     Output as JSON
```

### magic health

Health check.

```bash
magic health [flags]

Flags:
  --watch    Continuous monitoring
  --detail   Show detailed status
```

### magic status

System status.

```bash
magic status [flags]

Flags:
  --detail    Show detailed status
  --json      Output as JSON
```

### magic metrics

Show system metrics.

```bash
magic metrics [flags]

Flags:
  --reset    Reset metrics
  --format string    Output format
```

### magic logs

View logs.

```bash
magic logs [flags]

Flags:
  --lines int     Number of lines (default: 100)
  --level string    Filter by level
  --follow        Follow log output
```

Examples:
```bash
magic logs
magic logs --lines 50 --level error
magic logs --follow
```

### magic doctor

Run diagnostics.

```bash
magic doctor [flags]

Flags:
  --fix     Attempt to fix issues
  --verbose    Verbose output
```

### magic docs

Documentation utilities.

```bash
magic docs [subcommand]

Subcommands:
  magic docs generate    Generate documentation
  magic docs serve        Serve documentation locally
  magic docs man           Generate man pages
```

Examples:
```bash
magic docs generate --output ./docs
magic docs serve --port 8080
magic docs man --output ./man
```

## Shell Completion

### Install Completion

```bash
# Generate and install
magic completion bash > /etc/bash_completion.d/magic
magic completion zsh > "${fpath[1]}/_magic"
magic completion fish > ~/.config/fish/completions/magic.fish

# Or use install subcommand
magic completion install
```

### Using Completion

After installation, shell completion provides:
- Command completion
- Flag completion
- File path completion
- Config key completion

## Configuration Profiles

Create custom profiles:

```bash
# Create profile
magic config --profile work set provider openai

# Use profile
magic chat --profile work

# List profiles
ls ~/.magic/profiles/
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `MAGIC_HOME` | Config directory |
| `MAGIC_CONFIG` | Config file path |
| `MAGIC_LOG_LEVEL` | Log level |
| `MAGIC_NO_COLOR` | Disable colors |
| `MAGIC_PROFILE` | Default profile |
