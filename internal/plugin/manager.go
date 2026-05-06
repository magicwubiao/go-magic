// Package plugin provides a plugin system for extending go-magic
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
)

// Plugin represents a loaded plugin
type Plugin interface {
	Manifest() *Manifest
	Initialize(ctx *Context) error
	Execute(args []string) (interface{}, error)
	Shutdown() error
}

// Manifest describes a plugin
type Manifest struct {
	Name         string        `json:"name"`
	Version      string        `json:"version"`
	Description  string        `json:"description"`
	Author       string        `json:"author"`
	License      string        `json:"license"`
	APIVersion   string        `json:"api_version"`
	Dependencies []string      `json:"dependencies,omitempty"`
	Permissions  []string      `json:"permissions,omitempty"`
	Commands     []CommandSpec `json:"commands,omitempty"`
	Events       []string      `json:"events,omitempty"`
	EntryPoint   string        `json:"entry_point,omitempty"`
	ScriptPath   string        `json:"script_path,omitempty"`
	Type         string        `json:"type"` // "go", "script", "binary"
}

// CommandSpec defines a command provided by the plugin
type CommandSpec struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Arguments   []string `json:"arguments,omitempty"`
}

// Context provides context to plugins
type Context struct {
	HomeDir     string
	WorkingDir  string
	Environment []string
	DataDir     string
	CacheDir    string
	Config      map[string]interface{}
	SessionID   string
	UserID      string
}

// Manager manages plugin lifecycle
type Manager struct {
	plugins   map[string]Plugin
	manifests map[string]*Manifest
	config    *ManagerConfig
	mu        sync.RWMutex
	pluginDir string
	registry  *Registry
	hooks     map[string][]HookFunc
}

// ManagerConfig holds manager configuration
type ManagerConfig struct {
	PluginDir   string
	AutoLoad    bool
	AutoEnable  bool
	Sandboxed   bool
	AllowedCmds []string
}

// DefaultManagerConfig returns default configuration
func DefaultManagerConfig() *ManagerConfig {
	home, _ := os.UserHomeDir()
	return &ManagerConfig{
		PluginDir:  filepath.Join(home, ".magic", "plugins"),
		AutoLoad:   true,
		AutoEnable: true,
		Sandboxed:  true,
	}
}

// NewManager creates a new plugin manager
func NewManager(config *ManagerConfig) (*Manager, error) {
	if config == nil {
		config = DefaultManagerConfig()
	}

	m := &Manager{
		plugins:   make(map[string]Plugin),
		manifests: make(map[string]*Manifest),
		config:    config,
		pluginDir: config.PluginDir,
		registry:  NewRegistry(),
		hooks:     make(map[string][]HookFunc),
	}

	os.MkdirAll(config.PluginDir, 0755)

	if config.AutoLoad {
		if err := m.LoadAll(); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// LoadAll loads all plugins from the plugin directory
func (m *Manager) LoadAll() error {
	entries, err := os.ReadDir(m.pluginDir)
	if err != nil {
		return fmt.Errorf("failed to read plugin directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(m.pluginDir, entry.Name())
		if err := m.Load(pluginPath); err != nil {
			fmt.Printf("Failed to load plugin %s: %v\n", entry.Name(), err)
		}
	}

	return nil
}

// Load loads a plugin from the given path
func (m *Manager) Load(pluginPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	manifestPath := filepath.Join(pluginPath, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	if _, exists := m.plugins[manifest.Name]; exists {
		return nil
	}

	var p Plugin

	switch manifest.Type {
	case "go":
		p, err = m.loadGoPlugin(pluginPath, &manifest)
	case "script":
		p, err = m.loadScriptPlugin(pluginPath, &manifest)
	case "binary":
		p, err = m.loadBinaryPlugin(pluginPath, &manifest)
	default:
		return fmt.Errorf("unknown plugin type: %s", manifest.Type)
	}

	if err != nil {
		return err
	}

	ctx := m.createContext()
	if err := p.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	for _, cmd := range manifest.Commands {
		m.registry.Register(Tool{
			Name:        fmt.Sprintf("%s.%s", manifest.Name, cmd.Name),
			Description: cmd.Description,
			Plugin:      manifest.Name,
		})
	}

	m.plugins[manifest.Name] = p
	m.manifests[manifest.Name] = &manifest

	return nil
}

func (m *Manager) loadGoPlugin(pluginPath string, manifest *Manifest) (Plugin, error) {
	soPath := filepath.Join(pluginPath, pluginName(manifest.Name)+".so")
	if manifest.EntryPoint != "" {
		soPath = filepath.Join(pluginPath, manifest.EntryPoint)
	}

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
		return nil, fmt.Errorf("invalid plugin type")
	}

	return pl, nil
}

func (m *Manager) loadScriptPlugin(pluginPath string, manifest *Manifest) (Plugin, error) {
	scriptPath := filepath.Join(pluginPath, manifest.ScriptPath)
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("script not found: %s", scriptPath)
	}

	return &ScriptPlugin{
		manifest:   manifest,
		scriptPath: scriptPath,
		manager:    m,
	}, nil
}

func (m *Manager) loadBinaryPlugin(pluginPath string, manifest *Manifest) (Plugin, error) {
	binaryPath := filepath.Join(pluginPath, manifest.EntryPoint)
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("binary not found: %s", binaryPath)
	}

	return &BinaryPlugin{
		manifest:   manifest,
		binaryPath: binaryPath,
		manager:    m,
	}, nil
}

// Unload unloads a plugin
func (m *Manager) Unload(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("plugin not found: %s", name)
	}

	if err := p.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown plugin: %w", err)
	}

	delete(m.plugins, name)
	delete(m.manifests, name)
	m.registry.Unregister(name)

	return nil
}

// GetPlugin returns a loaded plugin
func (m *Manager) GetPlugin(name string) (Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, exists := m.plugins[name]
	return p, exists
}

// ListPlugins returns all loaded plugins
func (m *Manager) ListPlugins() []*Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var manifests []*Manifest
	for _, manifest := range m.manifests {
		manifests = append(manifests, manifest)
	}
	return manifests
}

// Execute runs a plugin command
func (m *Manager) Execute(pluginName, command string, args []string) (interface{}, error) {
	m.mu.RLock()
	p, exists := m.plugins[pluginName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", pluginName)
	}

	if m.config.Sandboxed && !m.isCommandAllowed(command) {
		return nil, fmt.Errorf("command not allowed in sandbox mode: %s", command)
	}

	return p.Execute(args)
}

// RegisterHook registers a hook function
func (m *Manager) RegisterHook(event string, fn HookFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hooks[event] = append(m.hooks[event], fn)
}

// TriggerHooks triggers all hooks for an event
func (m *Manager) TriggerHooks(event string, data interface{}) {
	m.mu.RLock()
	hooks := m.hooks[event]
	m.mu.RUnlock()

	for _, hook := range hooks {
		go hook(data)
	}
}

func (m *Manager) createContext() *Context {
	home, _ := os.UserHomeDir()
	return &Context{
		HomeDir:     home,
		WorkingDir:  "/",
		Environment: os.Environ(),
		DataDir:     filepath.Join(home, ".magic", "data"),
		CacheDir:    filepath.Join(home, ".magic", "cache"),
		Config:      make(map[string]interface{}),
	}
}

func (m *Manager) isCommandAllowed(cmd string) bool {
	if len(m.config.AllowedCmds) == 0 {
		return true
	}

	for _, allowed := range m.config.AllowedCmds {
		if allowed == cmd || allowed == "*" {
			return true
		}
	}
	return false
}

func pluginName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

// ScriptPlugin implements a script-based plugin
type ScriptPlugin struct {
	manifest   *Manifest
	scriptPath string
	manager    *Manager
}

func (p *ScriptPlugin) Manifest() *Manifest {
	return p.manifest
}

func (p *ScriptPlugin) Initialize(ctx *Context) error {
	pluginDir := filepath.Join(ctx.DataDir, "plugins", p.manifest.Name)
	os.MkdirAll(pluginDir, 0755)
	return nil
}

func (p *ScriptPlugin) Execute(args []string) (interface{}, error) {
	cmd := exec.Command(p.scriptPath, args...)
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("script execution failed: %w - %s", err, string(output))
	}

	return string(output), nil
}

func (p *ScriptPlugin) Shutdown() error {
	return nil
}

// BinaryPlugin implements a binary plugin
type BinaryPlugin struct {
	manifest   *Manifest
	binaryPath string
	manager    *Manager
}

func (p *BinaryPlugin) Manifest() *Manifest {
	return p.manifest
}

func (p *BinaryPlugin) Initialize(ctx *Context) error {
	return nil
}

func (p *BinaryPlugin) Execute(args []string) (interface{}, error) {
	cmd := exec.Command(p.binaryPath, args...)
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("binary execution failed: %w - %s", err, string(output))
	}

	return string(output), nil
}

func (p *BinaryPlugin) Shutdown() error {
	return nil
}

// HookFunc is a hook callback function
type HookFunc func(data interface{})

// Registry manages registered tools
type Registry struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

// Tool represents a registered tool
type Tool struct {
	Name        string
	Description string
	Plugin      string
	Schema      map[string]interface{}
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register registers a tool
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
}

// Unregister unregisters all tools from a plugin
func (r *Registry) Unregister(pluginName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, tool := range r.tools {
		if tool.Plugin == pluginName {
			delete(r.tools, name)
		}
	}
}

// Get returns a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, exists := r.tools[name]
	return t, exists
}

// List returns all tools
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []Tool
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// ToolDefinition returns an OpenAI-style tool definition
func (t *Tool) ToolDefinition() map[string]interface{} {
	schema := t.Schema
	if schema == nil {
		schema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"args": map[string]interface{}{
					"type":        "array",
					"description": "Arguments to pass to the tool",
				},
			},
		}
	}

	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  schema,
		},
	}
}

// CreateSamplePlugin creates a sample plugin for demonstration
func CreateSamplePlugin(dir string) error {
	manifest := Manifest{
		Name:        "example",
		Version:     "1.0.0",
		Description: "Example plugin demonstrating the plugin system",
		Author:      "go-magic",
		License:     "MIT",
		APIVersion:  "1.0",
		Type:        "script",
		ScriptPath:  "run.sh",
		Commands: []CommandSpec{
			{Name: "hello", Description: "Say hello"},
			{Name: "greet", Description: "Greet someone by name", Arguments: []string{"name"}},
		},
		Events: []string{"on_load", "on_unload"},
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
        echo "Hello, ${2:-World}!"
        ;;
    *)
        echo "Unknown command: $1"
        exit 1
        ;;
esac
`
	return os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0755)
}
