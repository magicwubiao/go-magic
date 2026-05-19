package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/plugin"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Plugin management for magic",
	Long:  `Discover, install, and manage plugins for Magic Agent.`,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all plugins",
	Run:   runPluginList,
}

var pluginSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for plugins in the marketplace",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginSearch,
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <plugin-id>",
	Short: "Install a plugin",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginInstall,
}

var pluginUninstallCmd = &cobra.Command{
	Use:   "uninstall <plugin-id>",
	Short: "Uninstall a plugin",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginUninstall,
}

var pluginEnableCmd = &cobra.Command{
	Use:   "enable <plugin-id>",
	Short: "Enable a plugin",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginEnable,
}

var pluginDisableCmd = &cobra.Command{
	Use:   "disable <plugin-id>",
	Short: "Disable a plugin",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginDisable,
}

var pluginReloadCmd = &cobra.Command{
	Use:   "reload <plugin-id>",
	Short: "Reload a plugin",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginReload,
}

var pluginUpdateCmd = &cobra.Command{
	Use:   "update [plugin-id]",
	Short: "Update plugin(s) from the marketplace",
	Run:   runPluginUpdate,
}

var pluginInfoCmd = &cobra.Command{
	Use:   "info <plugin-id>",
	Short: "Show detailed information about a plugin",
	Args:  cobra.MinimumNArgs(1),
	Run:   runPluginInfo,
}

var pluginDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover available plugins from the marketplace",
	Run:   runPluginDiscover,
}

var pluginCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for plugin updates",
	Run:   runPluginCheck,
}

var (
	pluginCategory string
	pluginJSON     bool
	pluginAll      bool
)

func init() {
	// List flags
	pluginListCmd.Flags().StringVar(&pluginCategory, "category", "", "Filter by category")
	pluginListCmd.Flags().BoolVar(&pluginJSON, "json", false, "Output as JSON")

	// Update flags
	pluginUpdateCmd.Flags().BoolVar(&pluginAll, "all", false, "Update all plugins")

	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginSearchCmd)
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginUninstallCmd)
	pluginCmd.AddCommand(pluginEnableCmd)
	pluginCmd.AddCommand(pluginDisableCmd)
	pluginCmd.AddCommand(pluginReloadCmd)
	pluginCmd.AddCommand(pluginUpdateCmd)
	pluginCmd.AddCommand(pluginInfoCmd)
	pluginCmd.AddCommand(pluginDiscoverCmd)
	pluginCmd.AddCommand(pluginCheckCmd)
}

func getPluginManager() *plugin.Manager {
	mgr, err := plugin.NewManager(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create plugin manager: %v\n", err)
		os.Exit(1)
	}
	return mgr
}

func runPluginList(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

	if err := mgr.LoadAll(cmd.Context()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load some plugins: %v\n", err)
	}

	var entries []*plugin.PluginEntry
	if pluginCategory != "" {
		entries = mgr.ListByCategory(pluginCategory)
	} else {
		entries = mgr.List()
	}

	if len(entries) == 0 {
		fmt.Println("No plugins found")
		return
	}

	if pluginJSON {
		// Convert to JSON-serializable format
		type PluginInfoJSON struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Version     string   `json:"version"`
			Description string   `json:"description"`
			Category    string   `json:"category"`
			Tags        []string `json:"tags"`
			State       string   `json:"state"`
		}
		var jsonEntries []PluginInfoJSON
		for _, e := range entries {
			jsonEntries = append(jsonEntries, PluginInfoJSON{
				ID:          e.Manifest.ID,
				Name:        e.Manifest.Name,
				Version:     e.Manifest.Version,
				Description: e.Manifest.Description,
				Category:    e.Manifest.Category,
				Tags:        e.Manifest.Tags,
				State:       string(e.State),
			})
		}
		output, _ := json.MarshalIndent(jsonEntries, "", "  ")
		fmt.Println(string(output))
		return
	}

	// Sort by name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Manifest.Name < entries[j].Manifest.Name
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tVERSION\tSTATE\tCATEGORY\tDESCRIPTION\n")

	for _, entry := range entries {
		stateStr := getStateString(entry.State)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			entry.Manifest.Name,
			entry.Manifest.Version,
			stateStr,
			entry.Manifest.Category,
			utils.Truncate(entry.Manifest.Description, 50),
		)
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\n%d plugin(s)\n", len(entries))
}

func runPluginSearch(cmd *cobra.Command, args []string) {
	query := strings.Join(args, " ")
	mgr := getPluginManager()

	results, err := mgr.Search(cmd.Context(), query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Search failed: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Printf("No plugins found matching '%s'\n", query)
		return
	}

	fmt.Printf("Found %d plugin(s):\n\n", len(results))

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tVERSION\tCATEGORY\tTAGS\n")

	for _, p := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			p.Name,
			p.Version,
			p.Category,
			strings.Join(p.Tags, ", "),
		)
	}
	w.Flush()

	fmt.Fprintf(os.Stderr, "\nInstall with: magic plugin install <plugin-id>\n")
}

func runPluginInstall(cmd *cobra.Command, args []string) {
	pluginID := args[0]
	mgr := getPluginManager()

	fmt.Printf("Installing plugin: %s\n", pluginID)

	if err := mgr.Install(cmd.Context(), pluginID); err != nil {
		fmt.Fprintf(os.Stderr, "Installation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Plugin installed successfully")
}

func runPluginUninstall(cmd *cobra.Command, args []string) {
	pluginID := args[0]
	mgr := getPluginManager()

	if !confirm(fmt.Sprintf("Uninstall plugin '%s'?", pluginID)) {
		return
	}

	if err := mgr.Uninstall(pluginID); err != nil {
		fmt.Fprintf(os.Stderr, "Uninstallation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Plugin uninstalled successfully")
}

func runPluginEnable(cmd *cobra.Command, args []string) {
	pluginID := args[0]
	mgr := getPluginManager()

	if err := mgr.Enable(pluginID); err != nil {
		fmt.Fprintf(os.Stderr, "Enable failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Plugin enabled")
}

func runPluginDisable(cmd *cobra.Command, args []string) {
	pluginID := args[0]
	mgr := getPluginManager()

	if err := mgr.Disable(pluginID); err != nil {
		fmt.Fprintf(os.Stderr, "Disable failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Plugin disabled")
}

func runPluginReload(cmd *cobra.Command, args []string) {
	pluginID := args[0]
	mgr := getPluginManager()

	fmt.Printf("Reloading plugin: %s\n", pluginID)

	if err := mgr.Reload(cmd.Context(), pluginID); err != nil {
		fmt.Fprintf(os.Stderr, "Reload failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Plugin reloaded successfully")
}

func runPluginUpdate(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

	var updates []plugin.UpdateInfo
	var err error

	if pluginAll || len(args) == 0 {
		updates, err = mgr.CheckUpdates()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Check updates failed: %v\n", err)
			os.Exit(1)
		}

		if len(updates) == 0 {
			fmt.Println("All plugins are up to date")
			return
		}

		fmt.Printf("Found %d update(s):\n\n", len(updates))
		for _, u := range updates {
			fmt.Printf("  %s: %s -> %s\n", u.PluginID, u.CurrentVer, u.NewVersion)
		}
		fmt.Println()

		if !confirm("Update all plugins?") {
			return
		}

		for _, u := range updates {
			fmt.Printf("Updating %s...\n", u.PluginID)
			if err := mgr.Update(cmd.Context(), u.PluginID); err != nil {
				fmt.Printf("  Failed: %v\n", err)
			} else {
				fmt.Printf("  Updated to %s\n", u.NewVersion)
			}
		}
	} else {
		pluginID := args[0]
		fmt.Printf("Updating plugin: %s\n", pluginID)

		if err := mgr.Update(cmd.Context(), pluginID); err != nil {
			fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Plugin updated successfully")
	}
}

func runPluginInfo(cmd *cobra.Command, args []string) {
	pluginID := args[0]
	mgr := getPluginManager()

	entry, ok := mgr.GetEntry(pluginID)
	if !ok {
		fmt.Fprintf(os.Stderr, "Plugin not found: %s\n", pluginID)
		os.Exit(1)
	}

	m := entry.Manifest

	fmt.Println()
	fmt.Println("===", m.Name, "===")
	fmt.Printf("ID:         %s\n", m.ID)
	fmt.Printf("Version:    %s\n", m.Version)
	fmt.Printf("Author:     %s\n", m.Author)
	fmt.Printf("License:    %s\n", m.License)
	fmt.Printf("Category:   %s\n", m.Category)
	fmt.Printf("State:      %s\n", entry.State)
	fmt.Printf("Type:       %s\n", m.Type)
	fmt.Println()
	fmt.Printf("Description:\n%s\n", m.Description)
	if m.LongDesc != "" {
		fmt.Printf("\nLong Description:\n%s\n", m.LongDesc)
	}
	fmt.Printf("\nTags:       %s\n", strings.Join(m.Tags, ", "))

	if len(m.Commands) > 0 {
		fmt.Println("\nCommands:")
		for _, c := range m.Commands {
			fmt.Printf("  /%s - %s\n", c.Name, c.Description)
		}
	}

	if len(m.Hooks) > 0 {
		fmt.Printf("\nHooks:      %s\n", strings.Join(m.Hooks, ", "))
	}

	if len(m.Permissions) > 0 {
		fmt.Printf("\nPermissions: %s\n", strings.Join(m.Permissions, ", "))
	}

	fmt.Println()
}

func runPluginDiscover(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

	fmt.Println("Discovering plugins...")

	// Show categories
	categories := []string{
		"utilities",
		"productivity",
		"development",
		"ai",
		"automation",
		"integration",
		"data",
		"security",
	}

	fmt.Println("\nAvailable Categories:")
	for _, cat := range categories {
		fmt.Printf("  - %s\n", cat)
	}

	fmt.Println("\nPopular Plugins:")

	// Search for popular plugins
	popular := []string{"code", "web", "data", "api"}
	for _, q := range popular {
		results, err := mgr.Search(cmd.Context(), q)
		if err != nil {
			continue
		}
		if len(results) > 0 {
			count := len(results)
			if count > 3 {
				count = 3
			}
			fmt.Printf("\n  [%s]:\n", q)
			for i := 0; i < count; i++ {
				p := results[i]
				fmt.Printf("    %s - %s\n", p.Name, utils.Truncate(p.Description, 40))
			}
		}
	}

	fmt.Println()
	fmt.Println("Search for specific plugins:")
	fmt.Println("  magic plugin search <query>")
	fmt.Println()
	fmt.Println("Install a plugin:")
	fmt.Println("  magic plugin install <plugin-id>")
	fmt.Println()
}

func runPluginCheck(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

	fmt.Println("Checking for updates...")

	updates, err := mgr.CheckUpdates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Check failed: %v\n", err)
		os.Exit(1)
	}

	if len(updates) == 0 {
		fmt.Println("All plugins are up to date")
		return
	}

	fmt.Printf("Found %d update(s):\n\n", len(updates))
	for _, u := range updates {
		fmt.Printf("  %s: %s -> %s\n", u.PluginID, u.CurrentVer, u.NewVersion)
	}
	fmt.Println()
	fmt.Println("Update with: magic plugin update --all")
}

// Helper functions

func getStateString(state plugin.PluginState) string {
	switch state {
	case plugin.StateEnabled:
		return "enabled"
	case plugin.StateDisabled:
		return "disabled"
	case plugin.StateError:
		return "error"
	default:
		return string(state)
	}
}



func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	var answer string
	fmt.Scanln(&answer)
	return strings.ToLower(answer) == "y"
}
