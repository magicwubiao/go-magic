# Plugin API Reference

Complete API reference for the go-magic plugin system.

## Table of Contents

- [Core Interfaces](#core-interfaces)
- [Registry](#registry)
- [Loader](#loader)
- [ConfigManager](#configmanager)
- [Sandbox](#sandbox)
- [Repository](#repository)
- [VersionManager](#versionmanager)
- [Context](#context)
- [Lifecycle Events](#lifecycle-events)

---

## Core Interfaces

### Plugin

The main interface that all plugins must implement.

```go
type Plugin interface {
    Manifest() *PluginManifest
    Initialize(ctx *Context) error
    Execute(cmd string, args []string) (interface{}, error)
    Shutdown() error
}
```

**Methods:**

| Method | Description |
|--------|-------------|
| `Manifest()` | Returns the plugin manifest |
| `Initialize(ctx)` | Initializes the plugin with context |
| `Execute(cmd, args)` | Executes a command with arguments |
| `Shutdown()` | Gracefully shuts down the plugin |

**Example:**

```go
type MyPlugin struct {
    manifest *PluginManifest
}

func (p *MyPlugin) Manifest() *PluginManifest {
    return p.manifest
}

func (p *MyPlugin) Initialize(ctx *plugin.Context) error {
    // Setup plugin resources
    return nil
}

func (p *MyPlugin) Execute(cmd string, args []string) (interface{}, error) {
    switch cmd {
    case "run":
        return "executed", nil
    default:
        return nil, fmt.Errorf("unknown command: %s", cmd)
    }
}

func (p *MyPlugin) Shutdown() error {
    // Cleanup resources
    return nil
}
```

---

### LifecyclePlugin

Extended plugin interface with lifecycle support.

```go
type LifecyclePlugin interface {
    Plugin
    OnLifecycle(ctx *Context, event LifecycleEvent, data interface{}) error
    RegisterLifecycleHooks() []LifecycleHookRegistration
}
```

---

## Registry

Plugin registry for registration and discovery.

### Functions

#### NewRegistry

Creates a new plugin registry.

```go
func NewRegistry() *Registry
```

**Returns:** A new Registry instance.

---

### Methods

#### Register

Registers a plugin with the registry.

```go
func (r *Registry) Register(plugin Plugin) error
```

**Parameters:**
- `plugin` - The plugin instance to register

**Returns:** Error if registration fails.

**Example:**

```go
myPlugin := &MyPlugin{manifest: manifest}
if err := registry.Register(myPlugin); err != nil {
    log.Fatal(err)
}
```

---

#### Unregister

Unregisters a plugin.

```go
func (r *Registry) Unregister(id string) error
```

**Parameters:**
- `id` - Plugin ID to unregister

**Returns:** Error if unregistration fails.

---

#### Get

Retrieves a plugin by ID.

```go
func (r *Registry) Get(id string) (Plugin, bool)
```

**Parameters:**
- `id` - Plugin ID

**Returns:** Plugin instance and whether it was found.

---

#### GetInfo

Retrieves plugin info by ID.

```go
func (r *Registry) GetInfo(id string) (*PluginInfo, bool)
```

**Parameters:**
- `id` - Plugin ID

**Returns:** PluginInfo and whether found.

---

#### List

Lists all registered plugin manifests.

```go
func (r *Registry) List() []*PluginManifest
```

**Returns:** Slice of all plugin manifests.

---

#### ListInfos

Lists all plugin info entries.

```go
func (r *Registry) ListInfos() []*PluginInfo
```

**Returns:** Slice of all plugin info.

---

#### ListByCategory

Lists plugins in a specific category.

```go
func (r *Registry) ListByCategory(category string) []*PluginManifest
```

**Parameters:**
- `category` - Category to filter by

**Returns:** Plugins in the category.

---

#### ListByTag

Lists plugins with a specific tag.

```go
func (r *Registry) ListByTag(tag string) []*PluginManifest
```

**Parameters:**
- `tag` - Tag to filter by

**Returns:** Plugins with the tag.

---

#### Search

Searches plugins by name, description, or tags.

```go
func (r *Registry) Search(query string) []*PluginManifest
```

**Parameters:**
- `query` - Search query

**Returns:** Matching plugins.

---

#### Enable

Enables a plugin.

```go
func (r *Registry) Enable(id string) error
```

---

#### Disable

Disables a plugin.

```go
func (r *Registry) Disable(id string) error
```

---

#### ResolveDependencies

Resolves plugin dependencies.

```go
func (r *Registry) ResolveDependencies(id string) error
```

**Returns:** Error if dependencies are missing or incompatible.

---

#### Count

Returns the number of registered plugins.

```go
func (r *Registry) Count() int
```

---

#### CountByState

Returns plugin count by state.

```go
func (r *Registry) CountByState() map[PluginState]int
```

**Returns:** Map of state to count.

---

#### Categories

Returns all unique categories.

```go
func (r *Registry) Categories() []string
```

---

#### Tags

Returns all unique tags.

```go
func (r *Registry) Tags() []string
```

---

## Loader

Plugin loader for loading from various sources.

### Functions

#### NewLoader

Creates a new plugin loader.

```go
func NewLoader(registry *Registry, config *LoaderConfig) *Loader
```

**Parameters:**
- `registry` - Plugin registry
- `config` - Loader configuration (nil for defaults)

---

### Methods

#### Load

Loads a single plugin from a path.

```go
func (l *Loader) Load(pluginPath string) error
```

---

#### LoadFromDirectory

Loads all plugins from a directory.

```go
func (l *Loader) LoadFromDirectory(dir string) error
```

---

#### LoadAll

Loads all plugins from configured directories.

```go
func (l *Loader) LoadAll() error
```

---

#### Unload

Unloads a plugin.

```go
func (l *Loader) Unload(id string) error
```

---

#### HotReload

Reloads a plugin without restarting.

```go
func (l *Loader) HotReload(id string) error
```

---

#### Validate

Validates a plugin without loading it.

```go
func (l *Loader) Validate(pluginPath string) error
```

---

#### FindPlugins

Discovers all potential plugins in a directory.

```go
func (l *Loader) FindPlugins(dir string) ([]string, error)
```

**Returns:** Paths to discovered plugins.

---

### LoaderConfig

```go
type LoaderConfig struct {
    PluginDir     string   // Directory to load plugins from
    AllowedDirs   []string // Additional allowed directories
    AutoEnable    bool     // Auto-enable loaded plugins
    ValidateDeps  bool     // Validate dependencies on load
    LoadBuiltins  bool     // Load built-in plugins
    BuiltinDir    string   // Built-in plugins directory
    PreloadHooks  []string // Hooks to preload
}
```

---

## ConfigManager

Configuration management for plugins.

### Functions

#### NewConfigManager

Creates a new configuration manager.

```go
func NewConfigManager(configDir string) (*ConfigManager, error)
```

---

### Methods

#### RegisterSchema

Registers a configuration schema for a plugin.

```go
func (cm *ConfigManager) RegisterSchema(pluginID string, fields []ConfigField)
```

---

#### SetConfig

Sets configuration for a plugin.

```go
func (cm *ConfigManager) SetConfig(pluginID string, config map[string]interface{}) error
```

---

#### GetConfig

Returns configuration for a plugin (with defaults applied).

```go
func (cm *ConfigManager) GetConfig(pluginID string) map[string]interface{}
```

---

#### GetRawConfig

Returns raw configuration without defaults.

```go
func (cm *ConfigManager) GetRawConfig(pluginID string) map[string]interface{}
```

---

#### SetDefault

Sets a default value for a configuration key.

```go
func (cm *ConfigManager) SetDefault(pluginID, key string, value interface{})
```

---

#### GetDefault

Returns the default value for a key.

```go
func (cm *ConfigManager) GetDefault(pluginID, key string) (interface{}, bool)
```

---

#### DeleteConfig

Removes a configuration key.

```go
func (cm *ConfigManager) DeleteConfig(pluginID, key string) error
```

---

#### ResetConfig

Resets plugin config to defaults.

```go
func (cm *ConfigManager) ResetConfig(pluginID string) error
```

---

#### ExportConfig

Exports configuration for all plugins.

```go
func (cm *ConfigManager) ExportConfig() ([]byte, error)
```

---

#### ImportConfig

Imports configuration from JSON.

```go
func (cm *ConfigManager) ImportConfig(data []byte) error
```

---

### ConfigField

```go
type ConfigField struct {
    Key         string      // Field key
    Type        string      // Field type (string, int, bool, etc.)
    Default     interface{} // Default value
    Description string      // Field description
    Required    bool        // Is required
    Options     []string    // Allowed values for enum
    Min         *float64    // Minimum for numbers
    Max         *float64    // Maximum for numbers
    Pattern     string      // Regex pattern for strings
    Sensitive   bool        // Is sensitive (password, etc.)
    EnvVar      string      // Environment variable fallback
}
```

---

## Sandbox

Security sandbox for plugin execution.

### Functions

#### NewSandbox

Creates a new sandbox for a plugin.

```go
func NewSandbox(plugin Plugin, config *SandboxConfig) *Sandbox
```

---

#### NewSandboxManager

Creates a sandbox manager.

```go
func NewSandboxManager(config *SandboxConfig) *SandboxManager
```

---

### SandboxConfig

```go
type SandboxConfig struct {
    Timeout        time.Duration // Execution timeout
    MemLimit       int64         // Memory limit in bytes
    CPUQuota       int           // CPU quota percentage
    AllowNetwork   bool          // Allow network access
    FilesystemRead  []string      // Allowed read paths
    FilesystemWrite []string      // Allowed write paths
    EnvWhitelist    []string       // Allowed environment variables
}
```

---

### Methods

#### RunWithSandbox

Executes the plugin with sandbox restrictions.

```go
func (s *Sandbox) RunWithSandbox(ctx context.Context, input interface{}) (interface{}, error)
```

---

## Repository

Remote plugin repository client.

### Functions

#### NewRepository

Creates a new repository client.

```go
func NewRepository(baseURL string) (*Repository, error)
```

---

### Methods

#### Search

Searches for plugins by query.

```go
func (r *Repository) Search(query string) ([]PluginManifest, error)
```

---

#### ListAvailable

Returns all available plugins from the repository.

```go
func (r *Repository) ListAvailable() ([]PluginManifest, error)
```

---

#### Install

Downloads and installs a plugin from the repository.

```go
func (r *Repository) Install(pluginID string, version string, targetDir string) error
```

---

#### Uninstall

Removes a plugin from the local directory.

```go
func (r *Repository) Uninstall(pluginID string, pluginDir string) error
```

---

#### Update

Checks for and installs updates.

```go
func (r *Repository) Update(pluginID string, currentVersion string, targetDir string) (bool, string, error)
```

**Returns:** Whether update occurred, new version, error.

---

#### GetPluginInfo

Returns plugin information from the repository.

```go
func (r *Repository) GetPluginInfo(pluginID string) (*PluginManifest, error)
```

---

## VersionManager

Version management for plugins.

### Functions

#### NewVersionManager

Creates a new version manager.

```go
func NewVersionManager() *VersionManager
```

---

### Methods

#### AddVersion

Adds a version to a plugin.

```go
func (vm *VersionManager) AddVersion(pluginID, version string)
```

---

#### RemoveVersion

Removes a version from a plugin.

```go
func (vm *VersionManager) RemoveVersion(pluginID, version string)
```

---

#### GetVersions

Returns all versions for a plugin.

```go
func (vm *VersionManager) GetVersions(pluginID string) []string
```

---

#### GetLatest

Returns the latest version for a plugin.

```go
func (vm *VersionManager) GetLatest(pluginID string) (string, bool)
```

---

#### GetCompatible

Returns the best compatible version for a constraint.

```go
func (vm *VersionManager) GetCompatible(pluginID, constraint string) (string, bool)
```

---

#### CheckUpgrade

Checks if there's an upgrade available.

```go
func (vm *VersionManager) CheckUpgrade(pluginID, currentVersion string) (bool, string)
```

**Returns:** Whether upgrade available, latest version.

---

### Version Functions

#### CompareVersions

Compares two versions.

```go
func CompareVersions(a, b string) int
```

**Returns:** -1 if a < b, 0 if equal, 1 if a > b.

---

#### CheckVersion

Checks if a version satisfies a constraint.

```go
func CheckVersion(version, constraint string) bool
```

---

#### CheckUpgrade

Checks if there's an upgrade available.

```go
func CheckUpgrade(current, latest string) bool
```

---

## Context

Plugin execution context.

### Context

```go
type Context struct {
    PluginID    string                 // Plugin ID
    HomeDir     string                 // User home directory
    WorkingDir  string                 // Current working directory
    Environment []string               // Environment variables
    DataDir     string                 // Plugin data directory
    CacheDir    string                 // Plugin cache directory
    ConfigDir   string                 // Plugin config directory
    Config      map[string]interface{}  // Plugin configuration
    SessionID   string                 // Current session ID
    UserID      string                 // Current user ID
    Logger      Logger                 // Plugin logger
    Metadata    map[string]interface{}  // Additional metadata
}
```

---

### Logger

Plugin logging interface.

```go
type Logger interface {
    Debug(msg string, args ...interface{})
    Info(msg string, args ...interface{})
    Warn(msg string, args ...interface{})
    Error(msg string, args ...interface{})
}
```

---

## Lifecycle Events

```go
type LifecycleEvent string

const (
    EventOnLoad         LifecycleEvent = "on_load"
    EventOnUnload       LifecycleEvent = "on_unload"
    EventOnEnable       LifecycleEvent = "on_enable"
    EventOnDisable      LifecycleEvent = "on_disable"
    EventOnSessionStart LifecycleEvent = "on_session_start"
    EventOnSessionEnd   LifecycleEvent = "on_session_end"
    EventOnLLMCall      LifecycleEvent = "on_llm_call"
    EventOnToolCall     LifecycleEvent = "on_tool_call"
    EventOnError        LifecycleEvent = "on_error"
    EventOnConfigChange LifecycleEvent = "on_config_change"
)
```

---

## Types

### PluginState

```go
type PluginState string

const (
    StateUnloaded PluginState = "unloaded"
    StateLoading  PluginState = "loading"
    StateLoaded   PluginState = "loaded"
    StateEnabled  PluginState = "enabled"
    StateDisabled PluginState = "disabled"
    StateError    PluginState = "error"
)
```

---

### PluginType

```go
type PluginType string

const (
    TypeGo     PluginType = "go"
    TypeScript PluginType = "script"
    TypeBinary PluginType = "binary"
    TypeWasm   PluginType = "wasm"
    TypeHTTP   PluginType = "http"
)
```

---

### PluginManifest

See [Plugin System Design](PLUGIN_SYSTEM.md#plugin-manifest) for full definition.

---

### Dependency

```go
type Dependency struct {
    ID       string // Plugin ID
    Version  string // Version constraint
    Optional bool   // Is this optional
}
```

---

### CommandSpec

```go
type CommandSpec struct {
    Name        string   // Command name
    Description string   // Command description
    Arguments   []string // Argument names
    Flags       []FlagSpec // Available flags
}
```

---

### Validation Functions

#### ValidateManifest

Validates a plugin manifest.

```go
func ValidateManifest(m *PluginManifest) error
```

---

#### IsValidVersion

Checks if a version string is valid semver.

```go
func IsValidVersion(version string) bool
```

---

#### IsValidVersionConstraint

Checks if a version constraint is valid.

```go
func IsValidVersionConstraint(constraint string) bool
```
