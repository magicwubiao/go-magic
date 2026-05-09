package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/plugin"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
	Long: `Manage magic plugins.
	
Plugins extend magic with new tools and capabilities.
Plugins are loaded from ~/.magic/plugins/
	
Examples:
  magic plugin list
  magic plugin discover
  magic plugin load <path>
  magic plugin unload <name>
  magic plugin reload <name>`,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List loaded plugins",
	Run:   runPluginList,
}

var pluginDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover available plugins",
	Run:   runPluginDiscover,
}

var pluginLoadCmd = &cobra.Command{
	Use:   "load <path>",
	Short: "Load a plugin from path",
	Args:  cobra.ExactArgs(1),
	Run:   runPluginLoad,
}

var pluginUnloadCmd = &cobra.Command{
	Use:   "unload <name>",
	Short: "Unload a plugin",
	Args:  cobra.ExactArgs(1),
	Run:   runPluginUnload,
}

var pluginReloadCmd = &cobra.Command{
	Use:   "reload <name>",
	Short: "Hot-reload a plugin without restarting",
	Args:  cobra.ExactArgs(1),
	Run:   runPluginReload,
}

func init() {
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginDiscoverCmd)
	pluginCmd.AddCommand(pluginLoadCmd)
	pluginCmd.AddCommand(pluginUnloadCmd)
	pluginCmd.AddCommand(pluginReloadCmd)
	rootCmd.AddCommand(pluginCmd)
}

func newPluginLoader() *plugin.Loader {
	registry := plugin.NewRegistry()
	cfg := plugin.DefaultLoaderConfig()
	return plugin.NewLoader(registry, cfg)
}

func runPluginList(cmd *cobra.Command, args []string) {
	registry := plugin.NewRegistry()
	manifests := registry.List()
	if len(manifests) == 0 {
		fmt.Println("No plugins loaded.")
		fmt.Println("\nUse 'magic plugin discover' to find available plugins.")
		return
	}

	fmt.Println("Loaded Plugins:")
	fmt.Println("==============")
	for _, p := range manifests {
		fmt.Printf("  %s (v%s) - %s\n", p.Name, p.Version, p.Description)
	}
}

func runPluginDiscover(cmd *cobra.Command, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	pluginDir := filepath.Join(home, ".magic", "plugins")

	fmt.Println("Discovering plugins...")
	fmt.Printf("Plugin directory: %s\n\n", pluginDir)

	// Ensure directory exists
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		fmt.Printf("Error: Cannot access plugin directory: %v\n", err)
		return
	}

	// Read directory contents
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		fmt.Printf("Error reading plugin directory: %v\n", err)
		return
	}

	var discovered []PluginDiscovery

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(pluginDir, entry.Name())
		manifestPath := filepath.Join(pluginPath, "manifest.json")

		// Try to load manifest
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			// Try .yaml extension
			manifestPath = filepath.Join(pluginPath, "manifest.yaml")
			data, err = os.ReadFile(manifestPath)
			if err != nil {
				fmt.Printf("  ⚠ %s: No manifest found\n", entry.Name())
				continue
			}
		}

		var manifest plugin.PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			fmt.Printf("  ⚠ %s: Invalid manifest\n", entry.Name())
			continue
		}

		discovered = append(discovered, PluginDiscovery{
			Name:        manifest.Name,
			Version:     manifest.Version,
			Description: manifest.Description,
			Author:      manifest.Author,
			Path:        pluginPath,
			Type:        string(manifest.Type),
		})
	}

	if len(discovered) == 0 {
		fmt.Println("No plugins discovered.")
		fmt.Println("\nTo add plugins:")
		fmt.Println("1. Create a directory in ~/.magic/plugins/")
		fmt.Println("2. Add a manifest.json with plugin metadata")
		fmt.Println("3. Add your plugin code")
		return
	}

	fmt.Printf("Discovered %d plugin(s):\n\n", len(discovered))
	fmt.Println("Available Plugins:")
	fmt.Println("=================")
	for _, p := range discovered {
		fmt.Printf("  📦 %s (v%s)\n", p.Name, p.Version)
		fmt.Printf("     Path: %s\n", p.Path)
		fmt.Printf("     Type: %s\n", p.Type)
		if p.Description != "" {
			fmt.Printf("     Description: %s\n", p.Description)
		}
		if p.Author != "" {
			fmt.Printf("     Author: %s\n", p.Author)
		}
		fmt.Println()
	}

	fmt.Println("To load a plugin: magic plugin load <path>")
}

// PluginDiscovery represents a discovered plugin
type PluginDiscovery struct {
	Name        string
	Version     string
	Description string
	Author      string
	Path        string
	Type        string
}

func runPluginLoad(cmd *cobra.Command, args []string) {
	path := args[0]
	loader := newPluginLoader()

	if err := loader.Load(path); err != nil {
		fmt.Printf("Failed to load plugin: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Plugin loaded from: %s\n", path)
}

func runPluginUnload(cmd *cobra.Command, args []string) {
	name := args[0]
	loader := newPluginLoader()

	if err := loader.Unload(name); err != nil {
		fmt.Printf("Failed to unload plugin: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Plugin '%s' unloaded.\n", name)
}

func runPluginReload(cmd *cobra.Command, args []string) {
	name := args[0]
	loader := newPluginLoader()

	if err := loader.HotReload(name); err != nil {
		fmt.Printf("Failed to reload plugin: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Plugin '%s' reloaded.\n", name)
}
