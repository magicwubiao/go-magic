package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/plugin"
)

// PluginManager manages plugin integration with the Agent
type PluginManager struct {
	registry    *plugin.Registry
	loader      *plugin.Loader
	repo        *plugin.Repository
	configMgr   *plugin.ConfigManager
	sandboxMgr  *plugin.SandboxManager
	versionMgr  *plugin.VersionManager
	agent       *Agent
	mu          sync.RWMutex
	enabled     map[string]bool // pluginID -> enabled
	recommended map[string][]string // task -> recommended plugins
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(agent *Agent) (*PluginManager, error) {
	home, _ := os.UserHomeDir()
	pluginDir := filepath.Join(home, ".magic", "plugins")
	os.MkdirAll(pluginDir, 0755)

	// Create components
	registry := plugin.NewRegistry()
	config := &plugin.LoaderConfig{
		PluginDir:   pluginDir,
		AutoEnable:  true,
		ValidateDeps: true,
	}
	loader := plugin.NewLoader(registry, config)
	configMgr, err := plugin.NewConfigManager("")
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}
	sandboxMgr := plugin.NewSandboxManager(nil)
	versionMgr := plugin.NewVersionManager()

	pm := &PluginManager{
		registry:    registry,
		loader:      loader,
		configMgr:   configMgr,
		sandboxMgr:  sandboxMgr,
		versionMgr:  versionMgr,
		agent:       agent,
		enabled:     make(map[string]bool),
		recommended: make(map[string][]string),
	}

	// Register built-in hooks
	pm.registerHooks()

	return pm, nil
}

// Initialize initializes the plugin manager
func (pm *PluginManager) Initialize(ctx context.Context) error {
	// Load all plugins
	if err := pm.loader.LoadAll(); err != nil {
		fmt.Printf("Warning: failed to load some plugins: %v\n", err)
	}

	// Register built-in skills as plugins
	if err := RegisterBuiltinSkills(pm.registry); err != nil {
		fmt.Printf("Warning: failed to register built-in skills: %v\n", err)
	}

	// Build recommended plugins map
	pm.buildRecommendations()

	// Enable default plugins
	pm.enableDefaults()

	return nil
}

// registerHooks registers plugin lifecycle hooks with the agent
func (pm *PluginManager) registerHooks() {
	// This would register hooks with the agent's hook manager
	// The actual implementation depends on the agent's hook system
}

// buildRecommendations builds the recommended plugins map
func (pm *PluginManager) buildRecommendations() {
	// Task -> recommended plugin IDs
	pm.recommended = map[string][]string{
		"code_review":    {"code-review", "static-analysis"},
		"write_report":   {"daily-report", "summarization"},
		"translate":      {"translation", "localization"},
		"search":         {"web-search", "information-retrieval"},
		"organize_files": {"file-organizer", "cleanup"},
		"analyze_data":   {"data-analysis", "visualization"},
	}
}

// enableDefaults enables plugins marked as default
func (pm *PluginManager) enableDefaults() {
	infos := pm.registry.ListInfos()
	for _, info := range infos {
		// Enable all loaded plugins by default
		pm.enabled[info.ID] = true
	}
}

// GetRecommendedPlugins returns plugins recommended for a task
func (pm *PluginManager) GetRecommendedPlugins(task string) []*plugin.PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Find matching task patterns
	var pluginIDs []string
	task = strings.ToLower(task)

	for pattern, ids := range pm.recommended {
		if strings.Contains(task, pattern) {
			pluginIDs = append(pluginIDs, ids...)
		}
	}

	// Get plugin info for matched IDs
	var infos []*plugin.PluginInfo
	seen := make(map[string]bool)

	for _, id := range pluginIDs {
		if seen[id] {
			continue
		}
		seen[id] = true

		if info, exists := pm.registry.GetInfo(id); exists {
			if info.State == plugin.StateEnabled {
				infos = append(infos, info)
			}
		}
	}

	return infos
}

// GetPluginContent returns the content/metadata for enabled plugins
func (pm *PluginManager) GetPluginContent() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var parts []string
	parts = append(parts, "## Available Plugins\n")

	infos := pm.registry.ListInfos()
	for _, info := range infos {
		if info.State == plugin.StateEnabled {
			parts = append(parts, fmt.Sprintf(
				"- **%s** (%s): %s",
				info.Name, info.Version, info.Description,
			))
		}
	}

	return strings.Join(parts, "\n")
}

// ExecutePlugin executes a plugin command
func (pm *PluginManager) ExecutePlugin(pluginID, command string, args []string) (interface{}, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.enabled[pluginID] {
		return nil, fmt.Errorf("plugin not enabled: %s", pluginID)
	}

	p, exists := pm.registry.Get(pluginID)
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	// Get sandbox if configured
	if sandbox, exists := pm.sandboxMgr.GetSandbox(pluginID); exists {
		ctx := context.Background()
		return sandbox.RunWithSandbox(ctx, map[string]interface{}{
			"command": command,
			"args":    args,
		})
	}

	return p.Execute(command, args)
}

// EnablePlugin enables a plugin
func (pm *PluginManager) EnablePlugin(pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := pm.registry.Enable(pluginID); err != nil {
		return err
	}

	pm.enabled[pluginID] = true
	return nil
}

// DisablePlugin disables a plugin
func (pm *PluginManager) DisablePlugin(pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := pm.registry.Disable(pluginID); err != nil {
		return err
	}

	pm.enabled[pluginID] = false
	return nil
}

// IsPluginEnabled returns whether a plugin is enabled
func (pm *PluginManager) IsPluginEnabled(pluginID string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.enabled[pluginID]
}

// ListPlugins returns all plugin information
func (pm *PluginManager) ListPlugins() []*plugin.PluginInfo {
	return pm.registry.ListInfos()
}

// ListEnabledPlugins returns only enabled plugins
func (pm *PluginManager) ListEnabledPlugins() []*plugin.PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var enabled []*plugin.PluginInfo
	for _, info := range pm.registry.ListInfos() {
		if pm.enabled[info.ID] {
			enabled = append(enabled, info)
		}
	}
	return enabled
}

// InstallPlugin installs a plugin from repository
func (pm *PluginManager) InstallPlugin(pluginID string, version string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	home, _ := os.UserHomeDir()
	pluginDir := filepath.Join(home, ".magic", "plugins")

	// Check if we have a repository
	if pm.repo == nil {
		return fmt.Errorf("no repository configured")
	}

	return pm.repo.Install(pluginID, version, pluginDir)
}

// UninstallPlugin uninstalls a plugin
func (pm *PluginManager) UninstallPlugin(pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if plugin is enabled
	if pm.enabled[pluginID] {
		return fmt.Errorf("cannot uninstall enabled plugin: %s", pluginID)
	}

	home, _ := os.UserHomeDir()
	pluginDir := filepath.Join(home, ".magic", "plugins")

	return pm.repo.Uninstall(pluginID, pluginDir)
}

// UpdatePlugin updates a plugin to the latest version
func (pm *PluginManager) UpdatePlugin(pluginID string) (bool, string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.registry.GetInfo(pluginID)
	if !exists {
		return false, "", fmt.Errorf("plugin not found: %s", pluginID)
	}

	home, _ := os.UserHomeDir()
	pluginDir := filepath.Join(home, ".magic", "plugins")

	return pm.repo.Update(pluginID, info.Version, pluginDir)
}

// SearchPlugins searches for plugins
func (pm *PluginManager) SearchPlugins(query string) []*plugin.PluginManifest {
	return pm.registry.Search(query)
}

// SetRepositoryURL sets the plugin repository URL
func (pm *PluginManager) SetRepositoryURL(url string) error {
	repo, err := plugin.NewRepository(url)
	if err != nil {
		return err
	}
	pm.repo = repo
	return nil
}

// GetPluginConfig returns plugin configuration
func (pm *PluginManager) GetPluginConfig(pluginID string) map[string]interface{} {
	return pm.configMgr.GetConfig(pluginID)
}

// SetPluginConfig sets plugin configuration
func (pm *PluginManager) SetPluginConfig(pluginID string, config map[string]interface{}) error {
	return pm.configMgr.SetConfig(pluginID, config)
}

// RegisterPluginSchema registers a configuration schema for a plugin
func (pm *PluginManager) RegisterPluginSchema(pluginID string, schema []plugin.ConfigField) {
	pm.configMgr.RegisterSchema(pluginID, schema)
}

// HotReload reloads a plugin without restarting
func (pm *PluginManager) HotReload(pluginID string) error {
	return pm.loader.HotReload(pluginID)
}

// GetPluginStats returns plugin usage statistics
func (pm *PluginManager) GetPluginStats() map[string]interface{} {
	infos := pm.registry.ListInfos()

	stats := map[string]interface{}{
		"total":      len(infos),
		"enabled":    0,
		"disabled":   0,
		"by_category": make(map[string]int),
	}

	stateCounts := pm.registry.CountByState()
	stats["enabled"] = stateCounts[plugin.StateEnabled]
	stats["disabled"] = stateCounts[plugin.StateDisabled]

	for _, info := range infos {
		stats["by_category"] = map[string]int{}
	}

	return stats
}

// TriggerLifecycleEvent triggers a lifecycle event for all enabled plugins
func (pm *PluginManager) TriggerLifecycleEvent(event plugin.LifecycleEvent, data interface{}) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for id, enabled := range pm.enabled {
		if !enabled {
			continue
		}

		p, exists := pm.registry.Get(id)
		if !exists {
			continue
		}

		// Check if plugin supports lifecycle
		if lp, ok := p.(plugin.LifecyclePlugin); ok {
			ctx := &plugin.Context{
				PluginID: id,
				Metadata: map[string]interface{}{
					"event": event,
					"data":  data,
				},
			}

			go lp.OnLifecycle(ctx, event, data)
		}
	}
}

// AutoInstallMissing installs plugins that are missing but recommended
func (pm *PluginManager) AutoInstallMissing(recommended []string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var installErrors []error

	for _, pluginID := range recommended {
		// Check if already installed
		if _, exists := pm.registry.Get(pluginID); exists {
			continue
		}

		// Try to install
		if err := pm.repo.Install(pluginID, "", pm.loader.(*plugin.Loader).LoadFromDirectory); err != nil {
			installErrors = append(installErrors, fmt.Errorf("failed to install %s: %w", pluginID, err))
		}
	}

	if len(installErrors) > 0 {
		return fmt.Errorf("install errors: %v", installErrors)
	}

	return nil
}

// ExportPlugins exports plugin configurations
func (pm *PluginManager) ExportPlugins() ([]byte, error) {
	return pm.configMgr.ExportConfig()
}

// ImportPlugins imports plugin configurations
func (pm *PluginManager) ImportPlugins(data []byte) error {
	return pm.configMgr.ImportConfig(data)
}

// Close shuts down the plugin manager
func (pm *PluginManager) Close() error {
	// Trigger unload for all plugins
	infos := pm.registry.ListInfos()
	for _, info := range infos {
		if err := pm.registry.Unregister(info.ID); err != nil {
			fmt.Printf("Warning: failed to unregister plugin %s: %v\n", info.ID, err)
		}
	}

	return nil
}

// PluginInvokeTool provides skill/plugin invocation functionality
type PluginInvokeTool struct {
	manager *PluginManager
}

// NewPluginInvokeTool creates a new plugin invoke tool
func NewPluginInvokeTool(manager *PluginManager) *PluginInvokeTool {
	return &PluginInvokeTool{manager: manager}
}

// Invoke invokes a plugin by name
func (t *PluginInvokeTool) Invoke(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	return t.manager.ExecutePlugin(name, "use", nil)
}

// ListAvailable returns all available plugins
func (t *PluginInvokeTool) ListAvailable() []string {
	infos := t.manager.ListEnabledPlugins()
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name
	}
	return names
}

// GetDescription returns the description for a plugin
func (t *PluginInvokeTool) GetDescription(name string) (string, error) {
	infos := t.manager.ListPlugins()
	for _, info := range infos {
		if info.Name == name {
			return info.Description, nil
		}
	}
	return "", fmt.Errorf("plugin not found: %s", name)
}
