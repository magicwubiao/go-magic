package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
)

// LifecycleEvent represents a plugin lifecycle event
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
)

// LifecycleHook defines a lifecycle hook function
type LifecycleHook func(ctx context.Context, event LifecycleEvent, data interface{}) error

// LifecyclePlugin extends Plugin with lifecycle support
type LifecyclePlugin interface {
	Plugin
	// OnLifecycle is called for lifecycle events
	OnLifecycle(ctx context.Context, event LifecycleEvent, data interface{}) error
	// RegisterLifecycleHooks registers the hooks this plugin provides
	RegisterLifecycleHooks() []LifecycleHookRegistration
}

// LifecycleHookRegistration describes a lifecycle hook
type LifecycleHookRegistration struct {
	Event    LifecycleEvent
	Priority int // Higher priority runs first
	Handler  LifecycleHook
}

// LifecycleManager manages plugin lifecycles
type LifecycleManager struct {
	plugins  map[string]LifecyclePlugin
	managers map[string]*Manager
	hooks    map[LifecycleEvent][]lifecycleHookEntry
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// lifecycleHookEntry holds a hook with its priority
type lifecycleHookEntry struct {
	pluginName string
	priority   int
	handler    LifecycleHook
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager() *LifecycleManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &LifecycleManager{
		plugins:  make(map[string]LifecyclePlugin),
		managers: make(map[string]*Manager),
		hooks:    make(map[LifecycleEvent][]lifecycleHookEntry),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Close closes the lifecycle manager
func (lm *LifecycleManager) Close() {
	lm.cancel()

	// Trigger unload for all plugins
	for name, plugin := range lm.plugins {
		plugin.OnLifecycle(lm.ctx, EventOnUnload, map[string]string{"name": name})
	}
}

// RegisterPlugin registers a plugin with lifecycle support
func (lm *LifecycleManager) RegisterPlugin(name string, plugin LifecyclePlugin) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, exists := lm.plugins[name]; exists {
		return fmt.Errorf("plugin already registered: %s", name)
	}

	lm.plugins[name] = plugin

	// Register lifecycle hooks
	lifecycleHooks := plugin.RegisterLifecycleHooks()
	for _, h := range lifecycleHooks {
		entry := lifecycleHookEntry{
			pluginName: name,
			priority:   h.Priority,
			handler:    h.Handler,
		}
		lm.hooks[h.Event] = append(lm.hooks[h.Event], entry)
	}

	// Sort hooks by priority (descending)
	lm.sortHooks()

	return nil
}

// UnregisterPlugin unregisters a plugin
func (lm *LifecycleManager) UnregisterPlugin(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	plugin, exists := lm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin not found: %s", name)
	}

	// Trigger unload
	plugin.OnLifecycle(lm.ctx, EventOnUnload, map[string]string{"name": name})

	// Remove hooks
	for event := range lm.hooks {
		var remaining []lifecycleHookEntry
		for _, entry := range lm.hooks[event] {
			if entry.pluginName != name {
				remaining = append(remaining, entry)
			}
		}
		lm.hooks[event] = remaining
	}

	delete(lm.plugins, name)
	return nil
}

// Trigger triggers a lifecycle event
func (lm *LifecycleManager) Trigger(event LifecycleEvent, data interface{}) {
	lm.mu.RLock()
	hookEntries, exists := lm.hooks[event]
	lm.mu.RUnlock()

	if !exists {
		return
	}

	for _, entry := range hookEntries {
		go func(e lifecycleHookEntry) {
			if err := e.handler(lm.ctx, event, data); err != nil {
				// Log error but don't fail
				fmt.Printf("Lifecycle hook error from %s: %v\n", e.pluginName, err)
			}
		}(entry)
	}
}

// TriggerSync triggers a lifecycle event synchronously
func (lm *LifecycleManager) TriggerSync(event LifecycleEvent, data interface{}) error {
	lm.mu.RLock()
	hookEntries, exists := lm.hooks[event]
	lm.mu.RUnlock()

	if !exists {
		return nil
	}

	var lastErr error
	for _, entry := range hookEntries {
		if err := entry.handler(lm.ctx, event, data); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// OnSessionStart triggers session start event
func (lm *LifecycleManager) OnSessionStart(sessionID string) {
	lm.Trigger(EventOnSessionStart, map[string]string{"session_id": sessionID})
}

// OnSessionEnd triggers session end event
func (lm *LifecycleManager) OnSessionEnd(sessionID string) {
	lm.Trigger(EventOnSessionEnd, map[string]string{"session_id": sessionID})
}

// OnLLMCall triggers LLM call event
func (lm *LifecycleManager) OnLLMCall(req *hooks.LLMHookRequest) {
	lm.Trigger(EventOnLLMCall, req)
}

// OnToolCall triggers tool call event
func (lm *LifecycleManager) OnToolCall(req *hooks.ToolCallHookRequest) {
	lm.Trigger(EventOnToolCall, req)
}

// sortHooks sorts hooks by priority (descending)
func (lm *LifecycleManager) sortHooks() {
	for event := range lm.hooks {
		hooks := lm.hooks[event]
		for i := 0; i < len(hooks)-1; i++ {
			for j := i + 1; j < len(hooks); j++ {
				if hooks[j].priority > hooks[i].priority {
					hooks[i], hooks[j] = hooks[j], hooks[i]
				}
			}
		}
		lm.hooks[event] = hooks
	}
}

// PluginManifest represents a plugin manifest with lifecycle support
type PluginManifest struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Author      string        `json:"author"`
	License     string        `json:"license"`
	APIVersion  string        `json:"api_version"`
	Permissions []string      `json:"permissions,omitempty"`
	Commands    []CommandSpec `json:"commands,omitempty"`
	Events      []string      `json:"events,omitempty"` // Lifecycle events this plugin handles
	Hooks       []string      `json:"hooks,omitempty"`  // Agent hooks this plugin provides
	Type        string        `json:"type"`             // "go", "script", "binary"
}

// LoadPluginFromDir loads a plugin from a directory
func LoadPluginFromDir(dir string) (*PluginManifest, string, error) {
	manifestPath := filepath.Join(dir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, "", fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Determine plugin type from directory structure
	pluginType := detectPluginType(dir)
	manifest.Type = pluginType

	return &manifest, dir, nil
}

// detectPluginType detects the plugin type from directory contents
func detectPluginType(dir string) string {
	// Check for .so file (Go plugin)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".so") {
			return "go"
		}
	}

	// Check for script files
	scriptExts := []string{".sh", ".py", ".js"}
	for _, entry := range entries {
		for _, ext := range scriptExts {
			if strings.HasSuffix(entry.Name(), ext) {
				return "script"
			}
		}
	}

	// Check for binary files
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".md") {
			return "binary"
		}
	}

	return "unknown"
}

// AutoDiscovery discovers plugins in a directory
func AutoDiscovery(pluginDir string) ([]*PluginManifest, error) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin directory: %w", err)
	}

	var manifests []*PluginManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(pluginDir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // Skip plugins without manifest
		}

		var manifest PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		manifests = append(manifests, &manifest)
	}

	return manifests, nil
}

// ValidatePlugin validates a plugin manifest
func ValidatePlugin(manifest *PluginManifest) error {
	if manifest.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if manifest.Version == "" {
		return fmt.Errorf("plugin version is required")
	}
	if manifest.Type == "" {
		return fmt.Errorf("plugin type is required")
	}

	validTypes := map[string]bool{"go": true, "script": true, "binary": true}
	if !validTypes[manifest.Type] {
		return fmt.Errorf("invalid plugin type: %s", manifest.Type)
	}

	return nil
}

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

// PluginInfo represents detailed plugin information
type PluginInfo struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	State       PluginState `json:"state"`
	Author      string      `json:"author"`
	EnabledAt   *time.Time  `json:"enabled_at,omitempty"`
	LoadedAt    *time.Time  `json:"loaded_at,omitempty"`
	Permissions []string    `json:"permissions"`
	Commands    []string    `json:"commands"`
	Hooks       []string    `json:"hooks"`
}

// GetPluginInfo returns detailed plugin information
func (lm *LifecycleManager) GetPluginInfo(name string) *PluginInfo {
	lm.mu.RLock()
	plugin, exists := lm.plugins[name]
	lm.mu.RUnlock()

	if !exists {
		return nil
	}

	info := &PluginInfo{
		Name:  name,
		State: StateLoaded,
	}

	if manifest := plugin.Manifest(); manifest != nil {
		info.Version = manifest.Version
		info.Description = manifest.Description
		info.Author = manifest.Author
		info.Permissions = manifest.Permissions
		for _, cmd := range manifest.Commands {
			info.Commands = append(info.Commands, cmd.Name)
		}
		info.Hooks = manifest.Events
	}

	now := time.Now()
	info.LoadedAt = &now

	return info
}

// ListPluginInfos returns information about all plugins
func (lm *LifecycleManager) ListPluginInfos() []*PluginInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var infos []*PluginInfo
	for name := range lm.plugins {
		plugin := lm.plugins[name]
		info := &PluginInfo{
			Name:  name,
			State: StateLoaded,
		}
		if manifest := plugin.Manifest(); manifest != nil {
			info.Version = manifest.Version
			info.Description = manifest.Description
			info.Author = manifest.Author
		}
		infos = append(infos, info)
	}

	return infos
}

// ExportPlugins exports plugin configurations
func ExportPlugins(lm *LifecycleManager) ([]byte, error) {
	infos := lm.ListPluginInfos()
	return json.MarshalIndent(infos, "", "  ")
}
