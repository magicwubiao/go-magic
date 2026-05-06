package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
)

// Loader handles plugin loading from various sources
type Loader struct {
	registry    *Registry
	config     *LoaderConfig
	mu         sync.Mutex
	loading    map[string]bool
}

// LoaderConfig holds loader configuration
type LoaderConfig struct {
	PluginDir     string   // Directory to load plugins from
	AllowedDirs   []string // Additional allowed directories
	AutoEnable    bool     // Auto-enable loaded plugins
	ValidateDeps  bool     // Validate dependencies on load
	LoadBuiltins  bool     // Load built-in plugins
	BuiltinDir    string   // Built-in plugins directory
	PreloadHooks  []string // Hooks to preload
}

// DefaultLoaderConfig returns default loader configuration
func DefaultLoaderConfig() *LoaderConfig {
	home, _ := os.UserHomeDir()
	return &LoaderConfig{
		PluginDir:    filepath.Join(home, ".magic", "plugins"),
		AutoEnable:   true,
		ValidateDeps: true,
		LoadBuiltins: true,
		BuiltinDir:   "internal/plugin/builtin",
	}
}

// NewLoader creates a new plugin loader
func NewLoader(registry *Registry, config *LoaderConfig) *Loader {
	if config == nil {
		config = DefaultLoaderConfig()
	}

	loader := &Loader{
		registry: registry,
		config:   config,
		loading:  make(map[string]bool),
	}

	// Ensure plugin directory exists
	os.MkdirAll(config.PluginDir, 0755)

	return loader
}

// LoadFromDirectory loads all plugins from a directory
func (l *Loader) LoadFromDirectory(dir string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var loadErrs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(dir, entry.Name())
		if err := l.loadPlugin(pluginPath); err != nil {
			loadErrs = append(loadErrs, fmt.Errorf("failed to load %s: %w", entry.Name(), err))
		}
	}

	if len(loadErrs) > 0 {
		return fmt.Errorf("load errors: %v", loadErrs)
	}
	return nil
}

// Load loads a single plugin from a path
func (l *Loader) Load(pluginPath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loadPlugin(pluginPath)
}

// loadPlugin internal load method
func (l *Loader) loadPlugin(pluginPath string) error {
	// Get plugin ID from directory name
	pluginID := filepath.Base(pluginPath)

	// Check if already loading
	if l.loading[pluginID] {
		return fmt.Errorf("plugin already loading: %s", pluginID)
	}
	l.loading[pluginID] = true
	defer delete(l.loading, pluginID)

	// Check if already registered
	if _, exists := l.registry.Get(pluginID); exists {
		return nil // Already loaded
	}

	// Load manifest
	manifest, err := l.loadManifest(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Override ID from directory if needed
	if manifest.ID == "" {
		manifest.ID = pluginID
	}

	// Validate manifest
	if err := ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	// Validate dependencies
	if l.config.ValidateDeps {
		for _, dep := range manifest.Dependencies {
			if _, exists := l.registry.Get(dep.ID); !exists && !dep.Optional {
				return fmt.Errorf("missing required dependency: %s", dep.ID)
			}
		}
	}

	// Create plugin instance based on type
	var pl Plugin
	switch manifest.Type {
	case TypeGo:
		pl, err = l.loadGoPlugin(pluginPath, manifest)
	case TypeScript:
		pl, err = l.loadScriptPlugin(pluginPath, manifest)
	case TypeBinary:
		pl, err = l.loadBinaryPlugin(pluginPath, manifest)
	default:
		return fmt.Errorf("unsupported plugin type: %s", manifest.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create plugin instance: %w", err)
	}

	// Create context
	ctx := l.createContext(manifest)

	// Initialize plugin
	if err := pl.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	// Register plugin
	if err := l.registry.Register(pl); err != nil {
		pl.Shutdown()
		return fmt.Errorf("failed to register plugin: %w", err)
	}

	// Enable plugin if configured
	if l.config.AutoEnable {
		if err := l.registry.Enable(manifest.ID); err != nil {
			// Log but don't fail
			fmt.Printf("Warning: failed to enable plugin %s: %v\n", manifest.ID, err)
		}
	}

	return nil
}

// Unload unloads a plugin
func (l *Loader) Unload(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if plugin exists
	entry, exists := l.registry.GetInfo(id)
	if !exists {
		return fmt.Errorf("plugin not found: %s", id)
	}

	// Check dependents
	if len(entry.Dependents) > 0 {
		return fmt.Errorf("plugin has active dependents: %v", entry.Dependents)
	}

	// Unregister
	if err := l.registry.Unregister(id); err != nil {
		return fmt.Errorf("failed to unregister: %w", err)
	}

	return nil
}

// HotReload reloads a plugin without restarting the application
func (l *Loader) HotReload(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Get current plugin info
	entry, exists := l.registry.GetInfo(id)
	if !exists {
		return fmt.Errorf("plugin not found: %s", id)
	}

	// Find plugin directory
	pluginDir := filepath.Join(l.config.PluginDir, id)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		pluginDir = filepath.Join(l.config.BuiltinDir, id)
	}

	// Unregister current plugin
	if err := l.registry.Unregister(id); err != nil {
		return fmt.Errorf("failed to unregister: %w", err)
	}

	// Reload plugin
	if err := l.loadPlugin(pluginDir); err != nil {
		return fmt.Errorf("failed to reload plugin: %w", err)
	}

	// Restore state if was enabled
	if entry.State == StateEnabled {
		l.registry.Enable(id)
	}

	return nil
}

// LoadAll loads all plugins from configured directories
func (l *Loader) LoadAll() error {
	// Load from plugin directory
	if err := l.LoadFromDirectory(l.config.PluginDir); err != nil {
		fmt.Printf("Warning: error loading plugins from %s: %v\n", l.config.PluginDir, err)
	}

	// Load from additional directories
	for _, dir := range l.config.AllowedDirs {
		if err := l.LoadFromDirectory(dir); err != nil {
			fmt.Printf("Warning: error loading plugins from %s: %v\n", dir, err)
		}
	}

	// Load built-in plugins
	if l.config.LoadBuiltins && l.config.BuiltinDir != "" {
		if err := l.LoadFromDirectory(l.config.BuiltinDir); err != nil {
			fmt.Printf("Warning: error loading built-in plugins: %v\n", err)
		}
	}

	return nil
}

// Validate checks if a plugin is valid without loading it
func (l *Loader) Validate(pluginPath string) error {
	manifest, err := l.loadManifest(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	if err := ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	// Validate entrypoint exists
	switch manifest.Type {
	case TypeGo:
		entrypoint := manifest.Entrypoint
		if entrypoint == "" {
			entrypoint = manifest.ID + ".so"
		}
		if _, err := os.Stat(filepath.Join(pluginPath, entrypoint)); err != nil {
			return fmt.Errorf("plugin binary not found: %s", entrypoint)
		}
	case TypeScript:
		entrypoint := manifest.Entrypoint
		if entrypoint == "" {
			entrypoint = "run." + getScriptExt(pluginPath)
		}
		if _, err := os.Stat(filepath.Join(pluginPath, entrypoint)); err != nil {
			return fmt.Errorf("script not found: %s", entrypoint)
		}
	case TypeBinary:
		if _, err := os.Stat(filepath.Join(pluginPath, manifest.Entrypoint)); err != nil {
			return fmt.Errorf("binary not found: %s", manifest.Entrypoint)
		}
	}

	return nil
}

// FindPlugins discovers all potential plugins in a directory
func (l *Loader) FindPlugins(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var plugins []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(pluginPath, "manifest.json")

		if _, err := os.Stat(manifestPath); err == nil {
			plugins = append(plugins, pluginPath)
		}
	}

	return plugins, nil
}

// loadManifest loads a plugin manifest
func (l *Loader) loadManifest(pluginPath string) (*PluginManifest, error) {
	manifestPath := filepath.Join(pluginPath, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Auto-detect type if not specified
	if manifest.Type == "" {
		manifest.Type = detectPluginType(pluginPath)
	}

	return &manifest, nil
}

// loadGoPlugin loads a Go plugin
func (l *Loader) loadGoPlugin(pluginPath string, manifest *PluginManifest) (Plugin, error) {
	entrypoint := manifest.Entrypoint
	if entrypoint == "" {
		entrypoint = manifest.ID + ".so"
	}

	soPath := filepath.Join(pluginPath, entrypoint)

	p, err := plugin.Open(soPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	symbol, err := p.Lookup("Plugin")
	if err != nil {
		return nil, fmt.Errorf("plugin symbol not found: %w", err)
	}

	pl, ok := symbol.(Plugin)
	if !ok {
		return nil, fmt.Errorf("invalid plugin type: expected Plugin interface")
	}

	return pl, nil
}

// loadScriptPlugin loads a script plugin
func (l *Loader) loadScriptPlugin(pluginPath string, manifest *PluginManifest) (Plugin, error) {
	entrypoint := manifest.Entrypoint
	if entrypoint == "" {
		ext := getScriptExt(pluginPath)
		entrypoint = "run." + ext
	}

	scriptPath := filepath.Join(pluginPath, entrypoint)
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("script not found: %s", scriptPath)
	}

	return &ScriptPlugin{
		manifest:   manifest,
		scriptPath: scriptPath,
		loader:     l,
	}, nil
}

// loadBinaryPlugin loads a binary plugin
func (l *Loader) loadBinaryPlugin(pluginPath string, manifest *PluginManifest) (Plugin, error) {
	if manifest.Entrypoint == "" {
		return nil, fmt.Errorf("binary plugin requires entrypoint")
	}

	binaryPath := filepath.Join(pluginPath, manifest.Entrypoint)
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("binary not found: %s", binaryPath)
	}

	return &BinaryPlugin{
		manifest:   manifest,
		binaryPath: binaryPath,
		loader:     l,
	}, nil
}

// createContext creates a plugin context
func (l *Loader) createContext(manifest *PluginManifest) *Context {
	home, _ := os.UserHomeDir()
	pluginDir := filepath.Join(l.config.PluginDir, manifest.ID)

	return &Context{
		PluginID:   manifest.ID,
		HomeDir:    home,
		WorkingDir: pluginDir,
		Environment: os.Environ(),
		DataDir:    filepath.Join(pluginDir, "data"),
		CacheDir:   filepath.Join(pluginDir, "cache"),
		ConfigDir:  filepath.Join(pluginDir, "config"),
		Config:     make(map[string]interface{}),
		Logger:     NewSimpleLogger(manifest.ID),
		Metadata:   make(map[string]interface{}),
	}
}

// detectPluginType detects plugin type from directory contents
func detectPluginType(dir string) PluginType {
	entries, _ := os.ReadDir(dir)

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".so") {
			return TypeGo
		}
	}

	// Check for script files
	for _, entry := range entries {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		switch ext {
		case ".sh", ".bash", ".py", ".js", ".rb":
			return TypeScript
		}
	}

	// Check for binary files (executables)
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".md") {
			path := filepath.Join(dir, entry.Name())
			if info, err := os.Stat(path); err == nil && info.Mode()&0111 != 0 {
				return TypeBinary
			}
		}
	}

	return TypeScript // Default to script
}

// getScriptExt detects script extension from directory
func getScriptExt(dir string) string {
	entries, _ := os.ReadDir(dir)

	extPriority := []string{".sh", ".bash", ".py", ".js", ".rb"}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		for _, p := range extPriority {
			if ext == p {
				return p[1:] // Remove dot
			}
		}
	}

	return "sh" // Default to shell
}

// ScriptPlugin implements a script-based plugin
type ScriptPlugin struct {
	manifest   *PluginManifest
	scriptPath string
	loader     *Loader
}

// Manifest returns the plugin manifest
func (p *ScriptPlugin) Manifest() *PluginManifest {
	return p.manifest
}

// Initialize initializes the plugin
func (p *ScriptPlugin) Initialize(ctx *Context) error {
	pluginDir := filepath.Join(ctx.DataDir)
	os.MkdirAll(pluginDir, 0755)
	return nil
}

// Execute runs a script command
func (p *ScriptPlugin) Execute(cmd string, args []string) (interface{}, error) {
	return nil, fmt.Errorf("script plugins do not support direct execution")
}

// Shutdown shuts down the plugin
func (p *ScriptPlugin) Shutdown() error {
	return nil
}

// BinaryPlugin implements a binary plugin
type BinaryPlugin struct {
	manifest   *PluginManifest
	binaryPath string
	loader     *Loader
}

// Manifest returns the plugin manifest
func (p *BinaryPlugin) Manifest() *PluginManifest {
	return p.manifest
}

// Initialize initializes the plugin
func (p *BinaryPlugin) Initialize(ctx *Context) error {
	return nil
}

// Execute runs the binary with arguments
func (p *BinaryPlugin) Execute(cmd string, args []string) (interface{}, error) {
	return nil, fmt.Errorf("binary plugins do not support direct execution")
}

// Shutdown shuts down the plugin
func (p *BinaryPlugin) Shutdown() error {
	return nil
}

// CreateSamplePlugin creates a sample plugin for demonstration
func CreateSamplePlugin(dir string) error {
	manifest := PluginManifest{
		ID:            "example",
		Name:          "Example Plugin",
		Version:       "1.0.0",
		Description:   "Example plugin demonstrating the plugin system",
		LongDesc:      "This is a more detailed description of the example plugin.",
		Author:        "go-magic",
		License:       "MIT",
		APIVersion:    "1.0",
		Type:          TypeScript,
		Entrypoint:    "run.sh",
		Category:      "utilities",
		Tags:          []string{"example", "demo", "sample"},
		Permissions:   []string{"filesystem", "network"},
		ConfigSchema: []ConfigField{
			{
				Key:         "debug",
				Type:        "boolean",
				Default:     false,
				Description: "Enable debug mode",
			},
		},
		Commands: []CommandSpec{
			{Name: "hello", Description: "Say hello"},
			{Name: "greet", Description: "Greet someone", Arguments: []string{"name"}},
		},
		Hooks:  []string{"on_load", "on_unload"},
		Events: []string{"example_event"},
	}

	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
		return err
	}

	script := `#!/bin/bash
case "$1" in
    hello)
        echo "Hello from go-magic plugin!"
        ;;
    greet)
        shift
        echo "Hello, ${*:-World}!"
        ;;
    *)
        echo "Unknown command: $1"
        exit 1
        ;;
esac
`
	return os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0755)
}
