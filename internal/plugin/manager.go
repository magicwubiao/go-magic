package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// Manager provides high-level plugin management
type Manager struct {
	registry  *Registry
	loader    *Loader
	repo      *Repository
	sandboxes map[string]*Sandbox
	config    *ManagerConfig
	mu        sync.RWMutex
}

// ManagerConfig holds manager configuration
type ManagerConfig struct {
	PluginDir     string
	CacheDir      string
	AllowNetwork  bool
	EnableSandbox bool
}

// DefaultManagerConfig returns default manager configuration
func DefaultManagerConfig() *ManagerConfig {
	home := config.GetMagicHome()
	return &ManagerConfig{
		PluginDir:     filepath.Join(home, "plugins"),
		CacheDir:      filepath.Join(home, "plugins", "cache"),
		AllowNetwork:  true,
		EnableSandbox: true,
	}
}

// NewManager creates a new plugin manager
func NewManager(config *ManagerConfig) (*Manager, error) {
	if config == nil {
		config = DefaultManagerConfig()
	}

	// Ensure directories exist
	os.MkdirAll(config.PluginDir, 0755)
	os.MkdirAll(config.CacheDir, 0755)

	// Create registry
	registry := NewRegistry()

	// Create loader
	loaderConfig := DefaultLoaderConfig()
	loaderConfig.PluginDir = config.PluginDir
	loader := NewLoader(registry, loaderConfig)

	// Create repository
	repo, err := NewRepository("https://plugins.magicwubiao.com")
	if err != nil {
		// Repository is optional, continue without it
		repo = nil
	}

	m := &Manager{
		registry:  registry,
		loader:    loader,
		repo:      repo,
		sandboxes: make(map[string]*Sandbox),
		config:    config,
	}

	return m, nil
}

// LoadAll loads all plugins from the plugin directory
func (m *Manager) LoadAll(ctx context.Context) error {
	return m.loader.LoadFromDirectory(m.config.PluginDir)
}

// Load loads a single plugin
func (m *Manager) Load(ctx context.Context, pluginID string) error {
	pluginPath := filepath.Join(m.config.PluginDir, pluginID)
	return m.loader.Load(pluginPath)
}

// Unload unloads a plugin
func (m *Manager) Unload(pluginID string) error {
	return m.registry.Unregister(pluginID)
}

// Enable enables a plugin
func (m *Manager) Enable(pluginID string) error {
	return m.registry.Enable(pluginID)
}

// Disable disables a plugin
func (m *Manager) Disable(pluginID string) error {
	return m.registry.Disable(pluginID)
}

// Get returns a plugin by ID
func (m *Manager) Get(pluginID string) (Plugin, bool) {
	return m.registry.Get(pluginID)
}

// GetEntry returns a plugin entry by ID
func (m *Manager) GetEntry(pluginID string) (*PluginEntry, bool) {
	return m.registry.GetEntry(pluginID)
}

// List returns all plugins
func (m *Manager) List() []*PluginEntry {
	return m.registry.ListEntries()
}

// ListByCategory returns plugins by category
func (m *Manager) ListByCategory(category string) []*PluginEntry {
	var result []*PluginEntry
	for _, entry := range m.registry.ListEntries() {
		if entry.Manifest.Category == category {
			result = append(result, entry)
		}
	}
	return result
}

// ListByTag returns plugins by tag
func (m *Manager) ListByTag(tag string) []*PluginEntry {
	var result []*PluginEntry
	for _, entry := range m.registry.ListEntries() {
		for _, t := range entry.Manifest.Tags {
			if t == tag {
				result = append(result, entry)
				break
			}
		}
	}
	return result
}

// Search searches for plugins in the repository
func (m *Manager) Search(ctx context.Context, query string) ([]PluginManifest, error) {
	if m.repo == nil {
		return nil, fmt.Errorf("plugin repository not configured")
	}
	return m.repo.Search(query)
}

// Install installs a plugin from the repository
func (m *Manager) Install(ctx context.Context, pluginID string) error {
	if m.repo == nil {
		return fmt.Errorf("plugin repository not configured")
	}

	// Download and install plugin
	err := m.repo.Install(pluginID, "", m.config.PluginDir)
	if err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	// Load the plugin
	pluginPath := filepath.Join(m.config.PluginDir, pluginID)
	return m.loader.Load(pluginPath)
}

// InstallFromURL installs a plugin directly from a URL
func (m *Manager) InstallFromURL(ctx context.Context, url string) error {
	if m.repo == nil {
		return fmt.Errorf("plugin repository not configured")
	}

	err := m.repo.InstallFromURL(url, m.config.PluginDir)
	if err != nil {
		return fmt.Errorf("failed to install plugin from URL: %w", err)
	}

	return nil
}

// Uninstall uninstalls a plugin
func (m *Manager) Uninstall(pluginID string) error {
	// Unload first
	if err := m.registry.Unregister(pluginID); err != nil {
		return fmt.Errorf("failed to unregister plugin: %w", err)
	}

	// Remove files
	pluginPath := filepath.Join(m.config.PluginDir, pluginID)
	return os.RemoveAll(pluginPath)
}

// Update updates a plugin from the repository
func (m *Manager) Update(ctx context.Context, pluginID string) error {
	if m.repo == nil {
		return fmt.Errorf("plugin repository not configured")
	}

	// Get current version from manifest
	entry, ok := m.registry.GetEntry(pluginID)
	if !ok {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}
	currentVersion := entry.Manifest.Version

	// Get latest version from repo
	manifest, err := m.repo.GetPluginInfo(pluginID)
	if err != nil {
		return fmt.Errorf("failed to get plugin info: %w", err)
	}

	// Check if update needed
	if manifest.Version == currentVersion {
		return fmt.Errorf("plugin %s is already up to date (version %s)", pluginID, currentVersion)
	}

	// Uninstall old version
	if err := m.Uninstall(pluginID); err != nil {
		return fmt.Errorf("failed to uninstall old version: %w", err)
	}

	// Install new version
	return m.repo.Install(pluginID, "", m.config.PluginDir)
}

// CheckUpdates checks for plugin updates
func (m *Manager) CheckUpdates() ([]UpdateInfo, error) {
	var updates []UpdateInfo

	if m.repo == nil {
		return updates, nil
	}

	for _, entry := range m.registry.ListEntries() {
		manifest, err := m.repo.GetPluginInfo(entry.Manifest.ID)
		if err != nil {
			continue
		}

		// Compare versions
		if compareVersions(manifest.Version, entry.Manifest.Version) > 0 {
			updates = append(updates, UpdateInfo{
				PluginID:   entry.Manifest.ID,
				CurrentVer: entry.Manifest.Version,
				NewVersion: manifest.Version,
			})
		}
	}

	return updates, nil
}

// compareVersions compares two semantic versions
// Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	// Simple version comparison - in production use semver library
	if v1 == v2 {
		return 0
	}
	// For now, just return 1 if different (newer version available)
	return 1
}

// Reload reloads a plugin
func (m *Manager) Reload(ctx context.Context, pluginID string) error {
	// Get plugin path
	pluginPath := filepath.Join(m.config.PluginDir, pluginID)

	// Unload
	if err := m.registry.Unregister(pluginID); err != nil {
		return fmt.Errorf("failed to unregister plugin: %w", err)
	}

	// Load again
	return m.loader.Load(pluginPath)
}

// RunWithSandbox executes a plugin with sandbox restrictions
func (m *Manager) RunWithSandbox(ctx context.Context, pluginID string, input interface{}) (interface{}, error) {
	entry, ok := m.registry.GetEntry(pluginID)
	if !ok {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	sandbox := NewSandbox(entry.Plugin, nil)
	return sandbox.RunWithSandbox(ctx, input)
}

// UpdateInfo contains update information for a plugin
type UpdateInfo struct {
	PluginID   string
	CurrentVer string
	NewVersion string
}
