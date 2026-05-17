package skin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Manager handles skin loading, caching, and application
type Manager struct {
	mu           sync.RWMutex
	activeSkin   *Config
	skinDir      string
	userSkins    map[string]*Config
	builtinSkins map[string]*Config
	onChange     []func(*Config) // callbacks when skin changes
}

// NewManager creates a new skin manager
func NewManager(skinDir string) *Manager {
	m := &Manager{
		skinDir:      skinDir,
		userSkins:    make(map[string]*Config),
		builtinSkins: BuiltinSkins(),
		activeSkin:   DefaultSkin,
	}

	// Load user skins
	if skinDir != "" {
		m.loadUserSkins()
	}

	return m
}

// SetSkinDir sets the user skin directory
func (m *Manager) SetSkinDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.skinDir = dir
	m.loadUserSkins()
}

// loadUserSkins loads skins from the user directory
func (m *Manager) loadUserSkins() {
	if m.skinDir == "" {
		return
	}

	entries, err := os.ReadDir(m.skinDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		skinPath := filepath.Join(m.skinDir, entry.Name())
		skin, err := LoadSkinFile(skinPath)
		if err != nil {
			continue
		}

		// Use filename (without extension) as skin name
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		m.userSkins[name] = skin
	}
}

// GetSkin returns a skin by name (checks user skins first, then builtins)
func (m *Manager) GetSkin(name string) (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check user skins first
	if skin, ok := m.userSkins[name]; ok {
		return skin, nil
	}

	// Check built-in skins
	if skin, ok := m.builtinSkins[name]; ok {
		return skin, nil
	}

	// Try to load from file
	if m.skinDir != "" {
		skinPath := filepath.Join(m.skinDir, name+".yaml")
		return LoadSkinFile(skinPath)
	}

	return nil, fmt.Errorf("skin not found: %s", name)
}

// List returns all available skin names
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skins := make([]string, 0, len(m.builtinSkins)+len(m.userSkins))

	// Built-in skins first
	for name := range m.builtinSkins {
		skins = append(skins, name)
	}

	// User skins
	for name := range m.userSkins {
		skins = append(skins, name)
	}

	return skins
}

// ListBuiltin returns only built-in skin names
func (m *Manager) ListBuiltin() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.builtinSkins))
	for name := range m.builtinSkins {
		names = append(names, name)
	}
	return names
}

// ListUser returns only user skin names
func (m *Manager) ListUser() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.userSkins))
	for name := range m.userSkins {
		names = append(names, name)
	}
	return names
}

// SetActive sets the active skin
func (m *Manager) SetActive(name string) error {
	skin, err := m.GetSkin(name)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.activeSkin = skin
	m.mu.Unlock()

	// Notify callbacks
	m.mu.RLock()
	callbacks := m.onChange
	m.mu.RUnlock()

	for _, cb := range callbacks {
		go cb(skin)
	}

	return nil
}

// GetActive returns the currently active skin
func (m *Manager) GetActive() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeSkin
}

// GetActiveName returns the name of the active skin
func (m *Manager) GetActiveName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.activeSkin == nil {
		return "default"
	}
	return m.activeSkin.Name
}

// OnChange registers a callback for skin changes
func (m *Manager) OnChange(cb func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = append(m.onChange, cb)
}

// SaveUserSkin saves a skin to the user directory
func (m *Manager) SaveUserSkin(name string, skin *Config) error {
	if m.skinDir == "" {
		return fmt.Errorf("skin directory not set")
	}

	// Validate
	if err := skin.Validate(); err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(m.skinDir, 0755); err != nil {
		return err
	}

	// Save to file
	skinPath := filepath.Join(m.skinDir, name+".yaml")
	data, err := yaml.Marshal(skin)
	if err != nil {
		return err
	}

	if err := os.WriteFile(skinPath, data, 0644); err != nil {
		return err
	}

	// Update cache
	m.mu.Lock()
	m.userSkins[name] = skin
	m.mu.Unlock()

	return nil
}

// DeleteUserSkin removes a user skin
func (m *Manager) DeleteUserSkin(name string) error {
	if m.skinDir == "" {
		return fmt.Errorf("skin directory not set")
	}

	// Cannot delete built-in skins
	m.mu.RLock()
	if _, ok := m.builtinSkins[name]; ok {
		m.mu.RUnlock()
		return fmt.Errorf("cannot delete built-in skin: %s", name)
	}
	m.mu.RUnlock()

	// Check if exists
	skin, ok := m.userSkins[name]
	if !ok {
		skinPath := filepath.Join(m.skinDir, name+".yaml")
		if _, err := os.Stat(skinPath); os.IsNotExist(err) {
			return fmt.Errorf("skin not found: %s", name)
		}
	}

	// If it's the active skin, switch to default
	if skin != nil && m.GetActiveName() == name {
		if err := m.SetActive("default"); err != nil {
			return err
		}
	}

	// Delete file
	skinPath := filepath.Join(m.skinDir, name+".yaml")
	if err := os.Remove(skinPath); err != nil {
		return err
	}

	// Update cache
	m.mu.Lock()
	delete(m.userSkins, name)
	m.mu.Unlock()

	return nil
}

// GetSkinInfo returns info about a skin without loading full content
func (m *Manager) GetSkinInfo(name string) (map[string]interface{}, error) {
	skin, err := m.GetSkin(name)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"name":        skin.Name,
		"description": skin.Description,
		"builtin":     m.IsBuiltin(name),
		"type":        "builtin",
	}, nil
}

// IsBuiltin checks if a skin is built-in
func (m *Manager) IsBuiltin(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.builtinSkins[name]
	return ok
}

// ExportJSON exports a skin as JSON
func (m *Manager) ExportJSON(name string) (string, error) {
	skin, err := m.GetSkin(name)
	if err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(skin, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Reset resets to the default skin
func (m *Manager) Reset() error {
	return m.SetActive("default")
}

// LoadSkinFile loads a skin from a YAML file
func LoadSkinFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var skin Config
	if err := yaml.Unmarshal(data, &skin); err != nil {
		return nil, err
	}

	if err := skin.Validate(); err != nil {
		return nil, err
	}

	return &skin, nil
}
