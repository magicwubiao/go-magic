package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// Agent Plugins 的命令行入口。
//
// 与 Web 的 /api/agent-plugins 面向同一份扫描目录与同一份配置：
// 插件以目录形式落在 <magicHome>/agent-plugins/<dir>，启停状态记在
// config.json 的 agent_plugins.disabled。这里不做另一套存储。
//
// 装插件=把目录放进扫描目录，卸插件=把目录删掉；不像"把连接器写进 config"
// 那样需要回滚记录——插件是自包含的目录，删了就干净了。

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage Agent Plugins (skills + mcp servers + cli connectors)",
}

var (
	pluginInstallName string

	pluginListCmd = &cobra.Command{
		Use:   "list",
		Short: "List installed Agent Plugins",
		RunE:  func(*cobra.Command, []string) error { return runPluginList() },
	}

	pluginInstallCmd = &cobra.Command{
		Use:   "install <dir|zip>",
		Short: "Install a plugin from a local directory or zip archive",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return runPluginInstall(args[0]) },
	}

	pluginRemoveCmd = &cobra.Command{
		Use:   "remove <name>",
		Short: "Uninstall a plugin (removes its directory and data dir)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return runPluginRemove(args[0]) },
	}

	pluginEnableCmd = &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a plugin (clear it from agent_plugins.disabled)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return setPluginEnabled(args[0], true) },
	}

	pluginDisableCmd = &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a plugin without removing it",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return setPluginEnabled(args[0], false) },
	}
)

func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginListCmd, pluginInstallCmd, pluginRemoveCmd, pluginEnableCmd, pluginDisableCmd)
	pluginInstallCmd.Flags().StringVar(&pluginInstallName, "name", "", "directory name under the plugin scan dir (default: manifest name)")
}

// loadAgentPluginsFromDisk 扫描默认目录，返回按名单索引的已加载插件。
func loadAgentPluginsFromDisk() map[string]*agentplugin.ManagedPlugin {
	_ = agentplugin.EnsureDefaultScanDir()
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		fmt.Fprintf(os.Stderr, "warning: failed to load config (disabled list ignored): %v\n", err)
	}
	disabled := map[string]bool{}
	if cfg != nil {
		for _, name := range cfg.AgentPlugins.Disabled {
			disabled[name] = true
		}
	}
	return agentplugin.NewManager([]string{agentplugin.DefaultScanDir()}, disabled).LoadAll()
}

func runPluginList() error {
	plugins := loadAgentPluginsFromDisk()
	if len(plugins) == 0 {
		fmt.Printf("No plugins installed under %s\n", agentplugin.DefaultScanDir())
		fmt.Println("Install one with: magic plugin install ./my-plugin")
		return nil
	}

	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tSTATUS\tSKILLS\tMCP\tCLI")
	for _, name := range names {
		mp := plugins[name]
		p := mp.Plugin
		status := "ok"
		switch {
		case p.IsRejected():
			status = "rejected"
		case mp.Disabled:
			status = "disabled"
		}
		version := "-"
		if p.Manifest != nil && strings.TrimSpace(p.Manifest.Version) != "" {
			version = p.Manifest.Version
		}
		skills := 0
		for _, ref := range p.Skills {
			if ref.Skill != nil {
				skills++
			}
		}
		cliTools := 0
		for _, cs := range p.CLIConnectors {
			if cs.Error == "" {
				cliTools += len(cs.Connector.Tools)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\n", name, version, status, skills, len(p.MCPServers), cliTools)
	}
	w.Flush()

	// 失败原因不塞进表格：一个坏插件的报错往往很长，会把表格挤变形。
	for _, name := range names {
		p := plugins[name].Plugin
		if p.FatalError != "" {
			fmt.Printf("\n%s: rejected: %s\n", name, p.FatalError)
		}
		for _, warning := range p.Warnings {
			fmt.Printf("\n%s: warning: %s\n", name, warning)
		}
		for _, cs := range p.CLIConnectors {
			if cs.Error != "" {
				fmt.Printf("\n%s: cli %q: %s\n", name, cs.Name, cs.Error)
			}
		}
	}
	return nil
}

func runPluginInstall(target string) error {
	target = strings.TrimSpace(target)
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("install source not found: %w", err)
	}

	// 目录名先按清单名推断，装完再把真实目录名报出来：
	// 规范允许目录名与 manifest.name 不同，但默认保持一致最不容易搞混。
	name := pluginInstallName
	if name == "" {
		manifest, err := agentplugin.InspectManifest(target)
		if err != nil {
			return fmt.Errorf("invalid plugin: %w", err)
		}
		name = manifest.Name
	}

	var dest string
	var skipped []string
	if info.IsDir() {
		dest, skipped, err = agentplugin.InstallFromDir(target, name)
	} else {
		dest, err = agentplugin.InstallFromZip(target, name)
	}
	if err != nil {
		return err
	}

	// 装完立刻自检：解压成功不等于插件可用（manifest 可能被拒、条目可能校验失败）。
	plugin := agentplugin.Load(dest, filepath.Join(config.GetMagicHome(), "agent-plugins-data", name))
	fmt.Printf("Installed into %s\n", dest)
	if plugin.IsRejected() {
		fmt.Fprintf(os.Stderr, "warning: plugin is rejected and will not load: %s\n", plugin.FatalError)
		return nil
	}
	skills := 0
	for _, ref := range plugin.Skills {
		if ref.Skill != nil {
			skills++
		}
	}
	cliTools := 0
	for _, cs := range plugin.CLIConnectors {
		if cs.Error != "" {
			fmt.Fprintf(os.Stderr, "warning: cli connector %q: %s\n", cs.Name, cs.Error)
			continue
		}
		cliTools += len(cs.Connector.Tools)
	}
	fmt.Printf("  skills: %d, mcp servers: %d, cli tools: %d\n", skills, len(plugin.MCPServers), cliTools)
	for _, warning := range plugin.Warnings {
		fmt.Printf("  warning: %s\n", warning)
	}
	if len(skipped) > 0 {
		fmt.Printf("  skipped (symlink/special): %s\n", strings.Join(skipped, ", "))
	}
	if plugin.CLIDisabled || plugin.MCPDisabled {
		fmt.Println("  note: a component was disabled; run 'magic plugin list' for details")
	}
	fmt.Println("Restart any running session to pick up the new plugin.")
	return nil
}

func runPluginRemove(name string) error {
	plugins := loadAgentPluginsFromDisk()
	mp, ok := plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q is not installed", name)
	}
	// Uninstall 按目录名删；目录名与 manifest.name 可能不同，从 Root 反推。
	dirName := filepath.Base(mp.Plugin.Root)
	removed, err := agentplugin.Uninstall(dirName)
	if err != nil {
		return err
	}
	fmt.Printf("Removed %s\n", removed)
	fmt.Printf("  data dir removed: %s\n", mp.Plugin.DataDir)
	return nil
}

func setPluginEnabled(name string, enabled bool) error {
	plugins := loadAgentPluginsFromDisk()
	if _, ok := plugins[name]; !ok {
		return fmt.Errorf("plugin %q is not installed", name)
	}
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		return fmt.Errorf("failed to load config: %w", err)
	}
	disabled := make([]string, 0, len(cfg.AgentPlugins.Disabled)+1)
	for _, n := range cfg.AgentPlugins.Disabled {
		if n != name {
			disabled = append(disabled, n)
		}
	}
	if !enabled {
		disabled = append(disabled, name)
	}
	cfg.AgentPlugins.Disabled = disabled
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("%s is now %s\n", name, state)
	return nil
}
