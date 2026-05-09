# Demo Plugin

A demo plugin for go-magic that showcases all plugin system capabilities.

## Features

- **4 CLI Commands**: `hello`, `time`, `stats`, `echo`
- **3 Lifecycle Hooks**: `on_load`, `on_unload`, `on_session_start`
- **Configuration Schema**: greeting, uppercase, max_length
- **JSON Output**: All commands return structured JSON

## Installation

```bash
# Copy to plugin directory
cp -r plugins/demo/ ~/.magic/plugins/demo/

# Discover
magic plugin discover

# Load
magic plugin load ~/.magic/plugins/demo
```

## Commands

### hello
Say hello with an optional name.
```bash
magic demo hello
magic demo hello --name Alice
```

### time
Show current time in multiple formats (local, UTC, ISO8601, unix timestamp).
```bash
magic demo time
```

### stats
Show system stats (OS, hostname, uptime).
```bash
magic demo stats
```

### echo
Echo text with config transforms (uppercase, truncation).
```bash
magic demo echo "Hello World"
```

## Configuration

In `~/.magic/plugins/demo/config.json`:

```json
{
  "greeting": "Hi",
  "uppercase": false,
  "max_length": 200
}
```

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| greeting | string | "Hello" | Default greeting word |
| uppercase | boolean | false | Convert output to uppercase |
| max_length | integer | 200 | Max output length (10-1000) |

## Lifecycle Hooks

| Hook | When | Output |
|------|------|--------|
| on_load | Plugin loaded | Confirmation message |
| on_unload | Plugin unloaded | Confirmation message |
| on_session_start | Chat session starts | Session ID echo |

## Use as Template

Copy this plugin directory and modify:
1. `manifest.json` — Change id, name, description, commands
2. `run.sh` — Implement your commands
3. `README.md` — Document your plugin
