package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadManifest loads a plugin manifest from file
func LoadManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &manifest, nil
}

// SaveManifest saves a plugin manifest to file
func SaveManifest(path string, manifest *PluginManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// FindPlugins finds all potential plugins in a directory
func FindPlugins(pluginsDir string) ([]string, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, fmt.Errorf("read plugins dir: %w", err)
	}

	var pluginPaths []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(pluginsDir, entry.Name(), "manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			pluginPaths = append(pluginPaths, filepath.Join(pluginsDir, entry.Name()))
		}
	}

	return pluginPaths, nil
}

// GetPluginDir returns the default plugin directory
func GetPluginDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".magic", "plugins")
}

// EnsurePluginDir ensures the plugin directory exists
func EnsurePluginDir() error {
	dir := GetPluginDir()
	return os.MkdirAll(dir, 0755)
}
