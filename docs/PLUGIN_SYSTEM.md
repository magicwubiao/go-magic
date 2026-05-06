# Plugin System Design

## Overview

The go-magic plugin system provides a comprehensive mechanism for extending the application's functionality through modular, independently deployable plugins. This document outlines the architecture, components, and design decisions of the plugin system.

## Architecture

The plugin system consists of several core components:

```
┌─────────────────────────────────────────────────────────────────┐
│                        Plugin Manager                            │
│  (Coordinates plugin lifecycle, integration with Agent)         │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
┌─────────────┐     ┌─────────────────┐    ┌─────────────────┐
│  Registry   │     │     Loader      │    │   Repository    │
│ (Discovery) │     │   (Loading)      │    │ (Installation)  │
└─────────────┘     └─────────────────┘    └─────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
┌───────────────────────────────────────────────────────────────┐
│                      Plugin Interface                          │
│  Manifest | Initialize | Execute | Shutdown | LifecycleHooks   │
└───────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────┐     ┌─────────────────┐    ┌─────────────────┐
│   Sandbox   │     │     Config      │    │    Version      │
│ (Security)  │     │   Management    │    │    Management   │
└─────────────┘     └─────────────────┘    └─────────────────┘
```

## Core Components

### 1. Plugin Registry (`registry.go`)

The registry provides centralized plugin discovery and management:

- **Registration**: Plugins register themselves with unique IDs and manifests
- **State Management**: Track plugin states (loaded, enabled, disabled, error)
- **Search & Discovery**: Find plugins by name, category, tags, or full-text search
- **Dependency Tracking**: Manage plugin dependencies and references
- **Event Broadcasting**: Notify listeners of plugin lifecycle changes

**Key Types:**
- `Registry`: Main registry implementation with thread-safe operations
- `PluginEntry`: Wrapper containing plugin instance, manifest, and state
- `PluginIndex`: Fast lookup index by name, category, and tags

### 2. Plugin Loader (`loader.go`)

The loader handles plugin discovery and instantiation:

- **Directory Loading**: Load all plugins from a directory
- **Type Detection**: Auto-detect plugin type from directory contents
- **Hot Reload**: Reload plugins without restarting the application
- **Validation**: Validate plugin manifests and entry points before loading
- **Multiple Types**: Support Go (.so), script (.sh, .py, .js), and binary plugins

**Supported Plugin Types:**
- `TypeGo`: Go plugins compiled as shared objects (.so)
- `TypeScript`: Shell scripts, Python, JavaScript
- `TypeBinary`: Standalone executables
- `TypeWasm`: WebAssembly plugins (future)
- `TypeHTTP`: HTTP-based remote plugins (future)

### 3. Plugin Interface

All plugins must implement the `Plugin` interface:

```go
type Plugin interface {
    Manifest() *PluginManifest
    Initialize(ctx *Context) error
    Execute(cmd string, args []string) (interface{}, error)
    Shutdown() error
}
```

Optional lifecycle support via `LifecyclePlugin`:
```go
type LifecyclePlugin interface {
    Plugin
    OnLifecycle(ctx *Context, event LifecycleEvent, data interface{}) error
    RegisterLifecycleHooks() []LifecycleHookRegistration
}
```

### 4. Plugin Manifest

Each plugin requires a `manifest.json` with metadata:

```json
{
    "id": "my-plugin",
    "name": "My Plugin",
    "version": "1.0.0",
    "description": "A sample plugin",
    "author": "Author Name",
    "license": "MIT",
    "api_version": "1.0",
    "type": "script",
    "entrypoint": "run.sh",
    "category": "utilities",
    "tags": ["utility", "helper"],
    "permissions": ["filesystem", "network"],
    "dependencies": [],
    "config_schema": [],
    "commands": [],
    "hooks": ["on_load", "on_unload"],
    "events": []
}
```

### 5. Configuration Management (`config.go`)

Plugin-specific configuration handling:

- **Schema Validation**: Define and validate configuration schemas
- **Default Values**: Set and override defaults
- **Persistence**: Save/load configurations to disk
- **Type Checking**: Validate types (string, number, boolean, array, object)
- **Pattern Matching**: Regex validation for string fields
- **Enum Validation**: Restrict values to allowed options

**Configuration Schema Example:**
```json
{
    "key": "debug",
    "type": "boolean",
    "default": false,
    "description": "Enable debug mode",
    "required": false
}
```

### 6. Sandbox Isolation (`sandbox.go`)

Security boundaries for plugin execution:

- **Timeout Enforcement**: Limit execution time
- **Memory Limits**: Restrict memory usage
- **Network Control**: Allow/deny network access
- **Filesystem Restrictions**: Limit read/write paths
- **Environment Filtering**: Whitelist environment variables

### 7. Version Management (`version.go`)

Semantic versioning support:

- **Version Parsing**: Support semver (1.2.3), v-prefixed (v1.2.3)
- **Constraint Matching**: >=, <=, ^, ~ operators
- **Upgrade Detection**: Check for newer versions
- **Compatibility**: Determine compatible versions

**Supported Constraints:**
- `*` - Any version
- `latest` - Latest available
- `^1.0.0` - Compatible with 1.x.x
- `~1.2.0` - Compatible with 1.2.x
- `>=1.0.0` - At least 1.0.0

### 8. Repository (`repository.go`)

Remote plugin discovery and installation:

- **Index Search**: Search available plugins
- **Download & Install**: Fetch plugins from remote repositories
- **Update Management**: Check and apply updates
- **Caching**: Cache plugin index locally

## Lifecycle Events

Plugins can hook into lifecycle events:

| Event | Description |
|-------|-------------|
| `on_load` | Plugin loaded into memory |
| `on_unload` | Plugin unloaded from memory |
| `on_enable` | Plugin enabled |
| `on_disable` | Plugin disabled |
| `on_session_start` | User session started |
| `on_session_end` | User session ended |
| `on_llm_call` | Before/after LLM call |
| `on_tool_call` | Before/after tool execution |
| `on_error` | Error occurred |
| `on_config_change` | Configuration changed |

## Skill Integration

The plugin system integrates with the skills system through adapters:

- **PluginAdapter**: Wraps a Skill as a Plugin
- **BuiltinSkillPlugin**: Registers built-in skills as plugins
- **Skill → Plugin Mapping**: Skills are exposed as script-type plugins

## Usage Examples

### Creating a Plugin

1. Create plugin directory:
```bash
mkdir -p ~/.magic/plugins/my-plugin
```

2. Create manifest.json:
```json
{
    "id": "my-plugin",
    "name": "My Plugin",
    "version": "1.0.0",
    "type": "script",
    "entrypoint": "run.sh",
    "description": "A sample plugin"
}
```

3. Create entry script (run.sh):
```bash
#!/bin/bash
echo "Hello from my plugin!"
```

### Loading Plugins Programmatically

```go
// Create registry
registry := plugin.NewRegistry()

// Create loader
loader := plugin.NewLoader(registry, nil)

// Load all plugins
loader.LoadAll()

// Enable a plugin
registry.Enable("my-plugin")
```

### Searching Plugins

```go
// Search by name/description
results := registry.Search("code review")

// Filter by category
utils := registry.ListByCategory("utilities")

// Filter by tag
searchPlugins := registry.ListByTag("search")
```

## Directory Structure

```
~/.magic/
└── plugins/
    ├── config/           # Plugin configurations
    ├── cache/
    │   └── repo/         # Repository index cache
    ├── my-plugin/
    │   ├── manifest.json
    │   ├── run.sh
    │   ├── data/         # Plugin data directory
    │   ├── cache/        # Plugin cache directory
    │   └── config/       # Plugin-specific config
    └── another-plugin/
        └── ...
```

## Security Considerations

1. **Plugin Verification**: Always validate manifests before loading
2. **Sandbox Enforcement**: Run untrusted plugins in sandbox mode
3. **Permission Model**: Implement least-privilege permissions
4. **Update Signing**: Verify plugin integrity on update
5. **Dependency Isolation**: Prevent dependency conflicts

## Future Enhancements

- [ ] WebAssembly plugin support
- [ ] HTTP-based remote plugins
- [ ] Plugin marketplace integration
- [ ] Plugin signing and verification
- [ ] Dependency resolution optimization
- [ ] Plugin profiling and monitoring
