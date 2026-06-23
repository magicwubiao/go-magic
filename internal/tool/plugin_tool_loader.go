package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// PluginToolLoader discovers plugins and creates PluginTool instances
// from their manifest commands.
type PluginToolLoader struct {
	pluginDir string
}

// newPluginToolLoader creates a plugin tool loader
func newPluginToolLoader() *PluginToolLoader {
	dir := filepath.Join(config.GetMagicHome(), "plugins")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return &PluginToolLoader{pluginDir: dir}
}

// pluginManifest is a minimal manifest for tool discovery
type pluginManifest struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Entrypoint  string            `json:"entrypoint"`
	Entrypoints map[string]string `json:"entrypoints"`
	Commands    []pluginCommand   `json:"commands"`
}

type pluginCommand struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Arguments   []string `json:"arguments"`
}

// DiscoverTools scans the plugin directory and creates PluginTool instances
// for every command declared in every plugin manifest.
func (l *PluginToolLoader) DiscoverTools() []Tool {
	var tools []Tool

	entries, err := os.ReadDir(l.pluginDir)
	if err != nil {
		return tools
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(l.pluginDir, entry.Name())
		manifestPath := filepath.Join(pluginPath, "manifest.json")

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest pluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		// Resolve entrypoint for current platform
		entrypoint := l.resolveEntrypoint(manifest, pluginPath)
		if entrypoint == "" {
			continue
		}

		// Create a tool for each command
		for _, cmd := range manifest.Commands {
			tool := NewPluginTool(
				manifest.ID,
				entrypoint,
				cmd.Name,
				cmd.Description,
				cmd.Arguments,
			)
			tools = append(tools, tool)
		}
	}

	return tools
}

// resolveEntrypoint resolves the correct entrypoint for the current platform
func (l *PluginToolLoader) resolveEntrypoint(manifest pluginManifest, pluginPath string) string {
	// 1. Check entrypoints map
	if manifest.Entrypoints != nil {
		if ep, ok := manifest.Entrypoints[runtime.GOOS]; ok {
			if abs, err := filepath.Abs(filepath.Join(pluginPath, ep)); err == nil {
				return abs
			}
			return filepath.Join(pluginPath, ep)
		}
		archKey := runtime.GOOS + "/" + runtime.GOARCH
		if ep, ok := manifest.Entrypoints[archKey]; ok {
			return filepath.Join(pluginPath, ep)
		}
	}

	// 2. Default entrypoint
	if manifest.Entrypoint != "" {
		return filepath.Join(pluginPath, manifest.Entrypoint)
	}

	return ""
}
