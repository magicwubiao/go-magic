package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/plugins"
)

// 插件（plugin.json）的命令行入口。
//
// 与 Web 的 /api/plugins 等价，只是不经 HTTP：CLI 直接读 config.json 写回，
// 和 `magic mcp add` 同一条路径。两边都靠"配置是唯一真值"来保持一致。

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugin manifests (skills + CLI connectors + MCP servers)",
}

var (
	pluginInstallForce bool

	pluginListCmd = &cobra.Command{
		Use:   "list",
		Short: "List installed and discoverable plugins",
		Run:   runPluginList,
	}

	pluginInstallCmd = &cobra.Command{
		Use:   "install <path-to-plugin.json|plugin-name>",
		Short: "Install a plugin into config.json",
		Args:  cobra.ExactArgs(1),
		Run:   runPluginInstall,
	}

	pluginRemoveCmd = &cobra.Command{
		Use:   "remove <name>",
		Short: "Uninstall a plugin (keeps connectors you have edited)",
		Args:  cobra.ExactArgs(1),
		Run:   runPluginRemove,
	}
)

func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginListCmd, pluginInstallCmd, pluginRemoveCmd)
	pluginInstallCmd.Flags().BoolVarP(&pluginInstallForce, "force", "f", false, "overwrite connectors that already exist")
}

func pluginDir() string { return filepath.Join(config.GetMagicHome(), "plugins") }

func pluginStore() *plugins.Store {
	return plugins.NewStore(filepath.Join(pluginDir(), "installed.json"))
}

func runPluginList(cmd *cobra.Command, args []string) {
	store := pluginStore()
	records, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read installed plugins: %v\n", err)
		os.Exit(1)
	}
	discovered, errs := plugins.Discover(pluginDir())
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warning: %v\n", e)
	}

	installed := make(map[string]bool, len(records))
	for _, r := range records {
		installed[r.Name] = true
	}

	if len(discovered) == 0 && len(records) == 0 {
		fmt.Printf("No plugins found under %s\n", pluginDir())
		fmt.Println("Drop a <name>/plugin.json there, or install one from a path:")
		fmt.Println("  magic plugin install ./my-plugin/plugin.json")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tINSTALLED\tCONNECTORS\tSKILLS")
	for _, m := range discovered {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n", m.Name, dash(m.Version), yesNo(installed[m.Name]),
			len(m.Connectors)+len(m.MCPServers), len(m.Skills))
	}
	for _, r := range records {
		if hasManifest(discovered, r.Name) {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\tinstalled (manifest missing)\t%d\t%d\n",
			r.Name, dash(r.Version), len(r.Connectors)+len(r.MCPServers), len(r.Skills))
	}
	w.Flush()
}

func runPluginInstall(cmd *cobra.Command, args []string) {
	target := strings.TrimSpace(args[0])

	manifest, err := resolvePluginCLI(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if missing := manifest.MissingEnv(); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "environment variable(s) not set: %s\n", strings.Join(missing, ", "))
		fmt.Fprintf(os.Stderr, "export them first, then reference them as ${%s} in the config\n", missing[0])
		os.Exit(1)
	}

	// ErrNoConfig 表示配置文件还不存在（首次使用）：继续用默认配置写出一份，
	// 而不是让"装第一个插件"卡在一条与插件无关的报错上。
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	if conflicts := manifest.Conflicts(connectorMap(cfg), mcpMap(cfg)); len(conflicts) > 0 {
		if !pluginInstallForce {
			fmt.Fprintf(os.Stderr, "connector(s) already exist: %s (use --force to replace)\n", strings.Join(conflicts, ", "))
			os.Exit(1)
		}
	}

	if len(manifest.Connectors) > 0 {
		if cfg.Cli == nil {
			cfg.Cli = &cliconn.Config{}
		}
		if cfg.Cli.Connectors == nil {
			cfg.Cli.Connectors = map[string]cliconn.Connector{}
		}
		for name, c := range manifest.Connectors {
			cfg.Cli.Connectors[name] = c
		}
	}
	if len(manifest.MCPServers) > 0 {
		if cfg.MCP == nil {
			cfg.MCP = &config.MCPConfig{}
		}
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = map[string]mcp.ServerConfig{}
		}
		for name, s := range manifest.MCPServers {
			cfg.MCP.Servers[name] = s
		}
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
		os.Exit(1)
	}

	if err := pluginStore().Put(plugins.Record{Manifest: manifest, Source: target}); err != nil {
		fmt.Fprintf(os.Stderr, "plugin applied but could not be recorded: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Installed plugin %s\n", manifest.Name)
	if len(manifest.Connectors) > 0 || len(manifest.MCPServers) > 0 {
		fmt.Println("Run 'magic cli list' / 'magic mcp list' to confirm the tools.")
	}
	if len(manifest.Skills) > 0 {
		fmt.Printf("Skills declared: %s\n", strings.Join(manifest.Skills, ", "))
		for _, name := range manifest.Skills {
			if !skillExists(name) {
				fmt.Printf("  warning: skill %q not found under %s/skills\n", name, config.GetMagicHome())
			}
		}
	}
}

// skillExists 用文件存在性判断技能是否可用（与 Web 侧 missingSkills 同一判据）。
func skillExists(name string) bool {
	base := filepath.Join(config.GetMagicHome(), "skills")
	if _, err := os.Stat(filepath.Join(base, name)); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(base, name+".md")); err == nil {
		return true
	}
	return false
}

func runPluginRemove(cmd *cobra.Command, args []string) {
	name := strings.TrimSpace(args[0])
	store := pluginStore()
	rec, ok, err := store.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read installed plugins: %v\n", err)
		os.Exit(1)
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "plugin %q is not installed\n", name)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	var removed, kept []string
	if cfg.Cli != nil {
		for cname, wanted := range rec.Connectors {
			if current, exists := cfg.Cli.Connectors[cname]; exists {
				if reflect.DeepEqual(current, wanted) {
					delete(cfg.Cli.Connectors, cname)
					removed = append(removed, "cli:"+cname)
				} else {
					kept = append(kept, "cli:"+cname)
				}
			}
		}
	}
	if cfg.MCP != nil {
		for sname, wanted := range rec.MCPServers {
			if current, exists := cfg.MCP.Servers[sname]; exists {
				if reflect.DeepEqual(current, wanted) {
					delete(cfg.MCP.Servers, sname)
					removed = append(removed, "mcp:"+sname)
				} else {
					kept = append(kept, "mcp:"+sname)
				}
			}
		}
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save config: %v\n", err)
		os.Exit(1)
	}
	if _, _, err := store.Delete(name); err != nil {
		fmt.Fprintf(os.Stderr, "failed to update install records: %v\n", err)
		os.Exit(1)
	}

	sort.Strings(removed)
	sort.Strings(kept)
	fmt.Printf("Removed plugin %s\n", name)
	if len(removed) > 0 {
		fmt.Printf("  removed: %s\n", strings.Join(removed, ", "))
	}
	if len(kept) > 0 {
		// 用户改过的配置保留：盲目按名字删会把他的修改一起带走。
		fmt.Printf("  kept (modified since install): %s\n", strings.Join(kept, ", "))
	}
}

func resolvePluginCLI(target string) (plugins.Manifest, error) {
	if fi, err := os.Stat(target); err == nil {
		if fi.IsDir() {
			return plugins.LoadFile(filepath.Join(target, plugins.ManifestFile))
		}
		return plugins.LoadFile(target)
	}
	if err := plugins.ValidateName(target); err != nil {
		return plugins.Manifest{}, err
	}
	return plugins.LoadFile(filepath.Join(pluginDir(), target, plugins.ManifestFile))
}

// connectorMap / mcpMap 复制一份当前配置，供冲突检测使用（插件包的
// Conflicts 只读入参，不持有配置对象）。
func connectorMap(cfg *config.Config) map[string]cliconn.Connector {
	out := make(map[string]cliconn.Connector)
	if cfg != nil && cfg.Cli != nil {
		for k, v := range cfg.Cli.Connectors {
			out[k] = v
		}
	}
	return out
}

func mcpMap(cfg *config.Config) map[string]mcp.ServerConfig {
	out := make(map[string]mcp.ServerConfig)
	if cfg != nil && cfg.MCP != nil {
		for k, v := range cfg.MCP.Servers {
			out[k] = v
		}
	}
	return out
}

func hasManifest(list []plugins.Manifest, name string) bool {
	for _, m := range list {
		if m.Name == name {
			return true
		}
	}
	return false
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
