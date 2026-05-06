# Plugin Development Guide

This guide walks you through creating a plugin for go-magic.

## Prerequisites

- Basic understanding of Go or shell scripting
- A text editor
- Access to the go-magic installation

## Plugin Structure

Every plugin needs:

```
my-plugin/
├── manifest.json    # Plugin metadata (required)
└── run.sh          # Entry script (required for script plugins)
```

Optional files:
- `data/` - Plugin data directory (created at runtime)
- `cache/` - Plugin cache directory (created at runtime)
- `config/` - Plugin-specific config directory (created at runtime)
- Any resource files your plugin needs

## Step 1: Create the Plugin Directory

```bash
mkdir -p ~/.magic/plugins/my-plugin
cd ~/.magic/plugins/my-plugin
```

## Step 2: Create the Manifest

Create `manifest.json` with your plugin's metadata:

```json
{
    "id": "my-plugin",
    "name": "My Awesome Plugin",
    "version": "1.0.0",
    "description": "Does something awesome",
    "long_desc": "This plugin does something really awesome that helps with your daily tasks.",
    "author": "Your Name",
    "author_email": "you@example.com",
    "license": "MIT",
    "api_version": "1.0",
    "type": "script",
    "entrypoint": "run.sh",
    "category": "utilities",
    "tags": ["awesome", "utility", "helper"],
    "permissions": ["filesystem"],
    "dependencies": [],
    "commands": [
        {
            "name": "do-something",
            "description": "Does something awesome",
            "arguments": ["arg1", "arg2"]
        }
    ],
    "hooks": ["on_load"],
    "events": []
}
```

### Manifest Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Unique identifier (lowercase, hyphenated) |
| `name` | Yes | Display name |
| `version` | Yes | Semantic version (e.g., 1.0.0) |
| `type` | Yes | `script`, `go`, or `binary` |
| `description` | No | Brief description |
| `author` | No | Author name |
| `license` | No | License type |
| `category` | No | Plugin category |
| `tags` | No | Searchable tags |
| `permissions` | No | Required permissions |
| `dependencies` | No | Plugin dependencies |
| `commands` | No | CLI commands provided |
| `hooks` | No | Lifecycle hooks to register |

## Step 3: Create the Entry Script

For script plugins, create the entry script:

```bash
#!/bin/bash
set -e

COMMAND=$1
shift

case "$COMMAND" in
    "")
        # No command - output plugin info
        echo "My Awesome Plugin v1.0.0"
        echo ""
        echo "Available commands:"
        echo "  do-something - Does something awesome"
        ;;
    "do-something")
        ARG1=$1
        ARG2=$2
        echo "Doing something awesome with $ARG1 and $ARG2!"
        ;;
    *)
        echo "Unknown command: $COMMAND"
        exit 1
        ;;
esac
```

Make it executable:
```bash
chmod +x run.sh
```

## Step 4: Test Your Plugin

Run the plugin manually to verify it works:

```bash
./run.sh
./run.sh do-something hello world
```

## Step 5: Load the Plugin

Plugins are auto-loaded on startup. To manually load:

```go
import "github.com/magicwubiao/go-magic/internal/plugin"

registry := plugin.NewRegistry()
loader := plugin.NewLoader(registry, nil)
loader.LoadAll()
```

## Script Plugin Examples

### Example 1: Code Formatter

```json
{
    "id": "code-formatter",
    "name": "Code Formatter",
    "version": "1.0.0",
    "type": "script",
    "entrypoint": "format.sh",
    "category": "development",
    "tags": ["code", "formatter", "development"],
    "permissions": ["filesystem"],
    "commands": [
        {
            "name": "format",
            "description": "Format code files",
            "arguments": ["path"]
        }
    ]
}
```

```bash
#!/bin/bash
set -e

COMMAND=$1
shift

case "$COMMAND" in
    "format")
        PATH=$1
        echo "Formatting $PATH..."
        # Add formatting logic here
        ;;
    *)
        echo "Usage: format <path>"
        exit 1
        ;;
esac
```

### Example 2: Web Scraper

```json
{
    "id": "web-scraper",
    "name": "Web Scraper",
    "version": "1.0.0",
    "type": "script",
    "entrypoint": "scrape.sh",
    "category": "research",
    "tags": ["web", "scraper", "research"],
    "permissions": ["network", "filesystem"],
    "commands": [
        {
            "name": "scrape",
            "description": "Scrape content from URL",
            "arguments": ["url"]
        }
    ]
}
```

```bash
#!/bin/bash
set -e

COMMAND=$1
shift

case "$COMMAND" in
    "scrape")
        URL=$1
        echo "Scraping $URL..."
        curl -s "$URL" | grep -o '<title>[^<]*</title>'
        ;;
    *)
        echo "Usage: scrape <url>"
        exit 1
        ;;
esac
```

### Example 3: Data Transformer

```json
{
    "id": "data-transformer",
    "name": "Data Transformer",
    "version": "1.0.0",
    "type": "script",
    "entrypoint": "transform.py",
    "category": "utilities",
    "tags": ["data", "transform", "json"],
    "permissions": ["filesystem"]
}
```

```python
#!/usr/bin/env python3
import sys
import json

def main():
    data = json.load(sys.stdin)
    # Transform data
    result = {
        "transformed": True,
        "items": len(data.get("items", [])),
        "data": data
    }
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    main()
```

## Adding Configuration

### Define Schema in Manifest

```json
{
    "config_schema": [
        {
            "key": "debug",
            "type": "boolean",
            "default": false,
            "description": "Enable debug mode"
        },
        {
            "key": "api_key",
            "type": "string",
            "default": "",
            "description": "API key for external service",
            "sensitive": true
        },
        {
            "key": "timeout",
            "type": "integer",
            "default": 30,
            "min": 1,
            "max": 300,
            "description": "Request timeout in seconds"
        },
        {
            "key": "mode",
            "type": "string",
            "default": "normal",
            "options": ["normal", "strict", "relaxed"],
            "description": "Processing mode"
        }
    ]
}
```

### Access Configuration in Script

Configuration is passed via environment variables:

```bash
#!/bin/bash

DEBUG=${MAGIC_PLUGIN_DEBUG:-false}
API_KEY=${MAGIC_PLUGIN_API_KEY:-}
TIMEOUT=${MAGIC_PLUGIN_TIMEOUT:-30}
MODE=${MAGIC_PLUGIN_MODE:-normal}

if [ "$DEBUG" = "true" ]; then
    echo "Debug mode enabled"
fi
```

## Lifecycle Hooks

### Registering Hooks

In your manifest, specify which hooks you want:

```json
{
    "hooks": ["on_load", "on_unload", "on_error"]
}
```

### Hook Implementations

#### on_load

Called when the plugin is loaded:

```bash
#!/bin/bash
case "$1" in
    "on_load")
        echo "Plugin loaded!"
        # Initialize resources, check dependencies, etc.
        ;;
    "on_unload")
        echo "Plugin unloading..."
        # Cleanup resources
        ;;
esac
```

#### on_error

Called when an error occurs:

```bash
case "$1" in
    "on_error")
        ERROR_MSG=$2
        echo "Error occurred: $ERROR_MSG" >&2
        # Log error, send notification, etc.
        ;;
esac
```

## Best Practices

### 1. Error Handling

Always handle errors gracefully:

```bash
#!/bin/bash
set -e  # Exit on error

# Or handle errors explicitly:
command_that_might_fail || {
    echo "Command failed!" >&2
    exit 1
}
```

### 2. Logging

Use stderr for errors and logs:

```bash
echo "Info message"
echo "Warning: something might be wrong" >&2
echo "Error: something failed" >&2
```

### 3. Performance

- Keep scripts lightweight
- Cache results when possible
- Use efficient tools (e.g., `grep` over `awk` for simple cases)

### 4. Portability

Write portable scripts:
- Use `#!/bin/bash` for bash-specific features
- Use `#!/bin/sh` for POSIX compatibility
- Test on multiple shells

### 5. Security

- Validate all inputs
- Sanitize file paths
- Don't expose sensitive data in logs
- Use quotes around variables

## Testing Your Plugin

### Manual Testing

```bash
# Test entry point
./run.sh

# Test specific command
./run.sh my-command arg1 arg2
```

### Automated Testing

Create a test script:

```bash
#!/bin/bash
set -e

echo "Running tests..."

# Test 1: Basic invocation
output=$(./run.sh)
if [[ ! "$output" =~ "My Plugin" ]]; then
    echo "FAIL: Basic invocation"
    exit 1
fi

# Test 2: Command execution
output=$(./run.sh do-something test)
if [[ ! "$output" =~ "test" ]]; then
    echo "FAIL: Command execution"
    exit 1
fi

echo "All tests passed!"
```

## Debugging

### Enable Debug Mode

Set environment variable:
```bash
export MAGIC_PLUGIN_DEBUG=true
./run.sh
```

### Common Issues

1. **Plugin not loading**: Check manifest.json syntax
2. **Permission denied**: Make script executable (`chmod +x`)
3. **Command not found**: Check shebang line
4. **Missing dependency**: Ensure required tools are installed

## Publishing Your Plugin

### Local Installation

```bash
cp -r my-plugin ~/.magic/plugins/
```

### Sharing

1. Create a ZIP of your plugin:
```bash
zip -r my-plugin.zip my-plugin/
```

2. Share the ZIP file or repository URL

## Summary

To create a plugin:

1. Create directory structure
2. Write manifest.json
3. Implement entry script
4. Add optional features (config, hooks)
5. Test thoroughly
6. Share or install locally

For more information, see the [Plugin System Design](../PLUGIN_SYSTEM.md) document.
