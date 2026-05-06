// Package plugin provides a comprehensive plugin system for extending go-magic
// with features including registry, lifecycle management, sandbox isolation,
// version management, and repository support.
package plugin

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// PluginState represents the state of a plugin
type PluginState string

const (
	StateUnloaded PluginState = "unloaded"
	StateLoading  PluginState = "loading"
	StateLoaded   PluginState = "loaded"
	StateEnabled  PluginState = "enabled"
	StateDisabled PluginState = "disabled"
	StateError    PluginState = "error"
)

// PluginManifest represents the complete manifest of a plugin
type PluginManifest struct {
	ID            string            `json:"id"`            // Unique identifier (kebab-case)
	Name          string            `json:"name"`          // Display name
	Version       string            `json:"version"`       // Semantic version
	Description   string            `json:"description"`   // Brief description
	LongDesc      string            `json:"long_desc"`     // Detailed description
	Author        string            `json:"author"`        // Author name
	AuthorEmail   string            `json:"author_email"`  // Author email
	License       string            `json:"license"`       // License type
	Homepage      string            `json:"homepage"`      // Plugin homepage
	Repository    string            `json:"repository"`    // Source repository
	Tags          []string          `json:"tags"`          // Searchable tags
	Category      string            `json:"category"`      // Plugin category
	APIVersion    string            `json:"api_version"`   // go-magic API version
	MinAppVersion string            `json:"min_app_version"` // Minimum go-magic version
	Type          PluginType        `json:"type"`          // Plugin implementation type
	Entrypoint    string            `json:"entrypoint"`     // Main entry file
	Permissions   []string          `json:"permissions"`   // Required permissions
	Dependencies  []Dependency      `json:"dependencies"`  // Plugin dependencies
	ConfigSchema  []ConfigField     `json:"config_schema"` // Configuration schema
	Commands      []CommandSpec     `json:"commands"`      // CLI commands
	Hooks         []string          `json:"hooks"`         // Lifecycle hooks
	Events        []string          `json:"events"`        // Published events
	Resources     []ResourceSpec    `json:"resources"`     // Bundled resources
	CreatedAt     string            `json:"created_at"`    // Creation timestamp
	UpdatedAt     string            `json:"updated_at"`    // Last update timestamp
}

// PluginType represents the type of plugin implementation
type PluginType string

const (
	TypeGo     PluginType = "go"      // Go plugin (.so)
	TypeScript PluginType = "script"  // Script plugin (shell, python, etc.)
	TypeBinary PluginType = "binary"  // Standalone binary
	TypeWasm   PluginType = "wasm"    // WebAssembly plugin
	TypeHTTP   PluginType = "http"    // HTTP-based remote plugin
)

// Dependency represents a plugin dependency
type Dependency struct {
	ID       string `json:"id"`       // Plugin ID
	Version  string `json:"version"`  // Version constraint
	Optional bool   `json:"optional"` // Is this optional
}

// ConfigField represents a configuration field schema
type ConfigField struct {
	Key          string      `json:"key"`           // Field key
	Type         string      `json:"type"`          // Field type (string, int, bool, etc.)
	Default      interface{} `json:"default"`       // Default value
	Description  string      `json:"description"`   // Field description
	Required     bool        `json:"required"`      // Is required
	Options      []string    `json:"options"`       // Allowed values for enum
	Min          *float64    `json:"min,omitempty"` // Minimum for numbers
	Max          *float64    `json:"max,omitempty"` // Maximum for numbers
	Pattern      string      `json:"pattern"`       // Regex pattern for strings
	Sensitive    bool        `json:"sensitive"`      // Is sensitive (password, etc.)
	EnvVar       string      `json:"env_var"`       // Environment variable fallback
}

// CommandSpec defines a command provided by the plugin
type CommandSpec struct {
	Name        string   `json:"name"`        // Command name
	Description string   `json:"description"` // Command description
	Arguments   []string `json:"arguments"`   // Argument names
	Flags       []FlagSpec `json:"flags"`     // Available flags
}

// FlagSpec defines a command flag
type FlagSpec struct {
	Name        string `json:"name"`        // Flag name
	Short       string `json:"short"`       // Short flag
	Description string `json:"description"` // Flag description
	Default     string `json:"default"`     // Default value
	Type        string `json:"type"`        // Flag type
}

// ResourceSpec defines a bundled resource
type ResourceSpec struct {
	Name     string `json:"name"`     // Resource name
	Type     string `json:"type"`     // Resource type (data, template, etc.)
	Path     string `json:"path"`     // Relative path in plugin
	MimeType string `json:"mime_type"` // MIME type
}

// Plugin represents a loaded plugin instance
type Plugin interface {
	// Manifest returns the plugin manifest
	Manifest() *PluginManifest

	// Initialize initializes the plugin with configuration
	Initialize(ctx *Context) error

	// Execute runs a command with arguments
	Execute(cmd string, args []string) (interface{}, error)

	// Shutdown gracefully shuts down the plugin
	Shutdown() error
}

// LifecyclePlugin extends Plugin with lifecycle support
type LifecyclePlugin interface {
	Plugin

	// OnLifecycle is called for lifecycle events
	OnLifecycle(ctx *Context, event LifecycleEvent, data interface{}) error

	// RegisterLifecycleHooks returns hooks this plugin provides
	RegisterLifecycleHooks() []LifecycleHookRegistration
}

// LifecycleEvent represents plugin lifecycle events
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

// LifecycleHookRegistration describes a lifecycle hook
type LifecycleHookRegistration struct {
	Event    LifecycleEvent
	Priority int // Higher priority runs first
	Handler  LifecycleHook
}

// LifecycleHook defines a lifecycle hook function
type LifecycleHook func(ctx *Context, event LifecycleEvent, data interface{}) error

// Context provides context to plugins
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

// Logger provides logging interface for plugins
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// SimpleLogger implements Logger using fmt
type SimpleLogger struct {
	prefix string
}

// NewSimpleLogger creates a new simple logger
func NewSimpleLogger(prefix string) *SimpleLogger {
	return &SimpleLogger{prefix: prefix}
}

func (l *SimpleLogger) Debug(msg string, args ...interface{}) {
	fmt.Printf("[DEBUG "+l.prefix+"] "+msg+"\n", args...)
}

func (l *SimpleLogger) Info(msg string, args ...interface{}) {
	fmt.Printf("[INFO "+l.prefix+"] "+msg+"\n", args...)
}

func (l *SimpleLogger) Warn(msg string, args ...interface{}) {
	fmt.Printf("[WARN "+l.prefix+"] "+msg+"\n", args...)
}

func (l *SimpleLogger) Error(msg string, args ...interface{}) {
	fmt.Printf("[ERROR "+l.prefix+"] "+msg+"\n", args...)
}

// PluginInfo represents detailed runtime information about a plugin
type PluginInfo struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	State       PluginState            `json:"state"`
	Author      string                 `json:"author"`
	Category    string                 `json:"category"`
	Tags        []string               `json:"tags"`
	StateInfo   string                 `json:"state_info"`   // Additional state info
	EnabledAt   *time.Time             `json:"enabled_at"`   // When enabled
	DisabledAt  *time.Time             `json:"disabled_at"`  // When disabled
	LoadedAt    *time.Time             `json:"loaded_at"`    // When loaded
	UnloadedAt  *time.Time             `json:"unloaded_at"`  // When unloaded
	ErrorMsg    string                 `json:"error_msg"`    // Error message if state is error
	Permissions []string               `json:"permissions"`   // Permissions granted
	Commands    []string               `json:"commands"`     // Available commands
	Hooks       []string               `json:"hooks"`       // Registered hooks
	Dependents  []string               `json:"dependents"`   // Plugins depending on this
	Dependencies []string              `json:"dependencies"` // Plugins this depends on
	Config      map[string]interface{} `json:"config"`       // Current config
}

// SortPluginInfos sorts plugin info slice by name
func SortPluginInfos(infos []*PluginInfo) {
	sort.Slice(infos, func(i, j int) bool {
		// Sort by category first, then by name
		if infos[i].Category != infos[j].Category {
			return infos[i].Category < infos[j].Category
		}
		return infos[i].Name < infos[j].Name
	})
}

// ValidateManifest validates a plugin manifest
func ValidateManifest(m *PluginManifest) error {
	if m.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}
	if !isValidID(m.ID) {
		return fmt.Errorf("plugin ID must be lowercase alphanumeric with hyphens")
	}
	if m.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("plugin version is required")
	}
	if !IsValidVersion(m.Version) {
		return fmt.Errorf("invalid semantic version: %s", m.Version)
	}
	if m.Type == "" {
		return fmt.Errorf("plugin type is required")
	}
	validTypes := map[PluginType]bool{
		TypeGo: true, TypeScript: true, TypeBinary: true, TypeWasm: true, TypeHTTP: true,
	}
	if !validTypes[m.Type] {
		return fmt.Errorf("invalid plugin type: %s", m.Type)
	}

	// Validate dependencies
	for _, dep := range m.Dependencies {
		if !isValidID(dep.ID) {
			return fmt.Errorf("invalid dependency ID: %s", dep.ID)
		}
		if !IsValidVersionConstraint(dep.Version) {
			return fmt.Errorf("invalid version constraint for %s: %s", dep.ID, dep.Version)
		}
	}

	return nil
}

// isValidID checks if an ID is valid (lowercase alphanumeric with hyphens)
func isValidID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// FormatVersion formats a version string for display
func FormatVersion(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
