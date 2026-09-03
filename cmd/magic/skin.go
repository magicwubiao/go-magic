package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skin"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var (
	skinName    string
	skinListAll bool
	skinExport  bool
	skinPreview bool
	skinInfo    string
	skinCreate  string
	skinDelete  string
)

func init() {
	// skin list command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all available skins",
		Run:   runSkinList,
	}
	listCmd.Flags().BoolVar(&skinListAll, "all", false, "Show all skins including details")

	// skin show command
	showCmd := &cobra.Command{
		Use:   "show [name]",
		Short: "Show skin details",
		Args:  cobra.MaximumNArgs(1),
		Run:   runSkinShow,
	}

	// skin preview command
	previewCmd := &cobra.Command{
		Use:   "preview [name]",
		Short: "Preview a skin with sample output",
		Args:  cobra.MaximumNArgs(1),
		Run:   runSkinPreview,
	}

	// skin create command
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new skin from another skin",
		Args:  cobra.ExactArgs(1),
		Run:   runSkinCreate,
	}
	createCmd.Flags().StringVarP(&skinCreate, "from", "f", "", "Source skin to copy from")

	// skin delete command
	deleteCmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete a user skin",
		Args:  cobra.ExactArgs(1),
		Run:   runSkinDelete,
	}

	// skin export command
	exportCmd := &cobra.Command{
		Use:   "export [name]",
		Short: "Export a skin as JSON",
		Args:  cobra.MaximumNArgs(1),
		Run:   runSkinExport,
	}

	// skin set command
	setCmd := &cobra.Command{
		Use:   "set [name]",
		Short: "Set the active skin",
		Args:  cobra.ExactArgs(1),
		Run:   runSkinSet,
	}

	// Main skin command
	skinCmd := &cobra.Command{
		Use:   "skin",
		Short: "Skin/Theme management for CLI appearance",
		Long: `Manage CLI skins to customize the visual appearance.

Built-in skins:
  - default: Classic gold/kawaii style
  - mono: Clean grayscale monochrome
  - slate: Cool blue developer-focused theme
  - cyber: Neon cyberpunk terminal theme

User skins are stored under your magic home directory in /skins/`,
	}

	// Add subcommands
	skinCmd.AddCommand(listCmd)
	skinCmd.AddCommand(showCmd)
	skinCmd.AddCommand(previewCmd)
	skinCmd.AddCommand(createCmd)
	skinCmd.AddCommand(deleteCmd)
	skinCmd.AddCommand(exportCmd)
	skinCmd.AddCommand(setCmd)

	rootCmd.AddCommand(skinCmd)
}

func getSkinManager() *skin.Manager {
	// Get the config directory (magic home)
	cfgDir := config.GetMagicHome()
	_ = cfgDir

	skinDir := filepath.Join(cfgDir, "skins")
	return skin.NewManager(skinDir)
}

func runSkinList(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()

	// Load current active skin from config
	activeSkin := "default"
	if cfg, err := config.Load(); err == nil {
		if cfg.Display.Skin != "" {
			activeSkin = cfg.Display.Skin
		}
	}

	if skinListAll {
		// Show detailed list
		fmt.Println("\n╔══════════════════════════════════════════════════════════╗")
		fmt.Println("║                    Available Skins                       ║")
		fmt.Println("╠══════════════════════════════════════════════════════════╣")

		// Built-in skins
		fmt.Printf("║ %sBuilt-in Skins:%s\n", "\033[38;5;220m", "\033[0m")
		for _, name := range mgr.ListBuiltin() {
			skin, _ := mgr.GetSkin(name)
			marker := "  "
			if name == activeSkin {
				marker = "✓ "
			}
			fmt.Printf("║   %s%s%s - %s\n", marker, name, reset(), skin.Description)
		}

		// User skins
		userSkins := mgr.ListUser()
		if len(userSkins) > 0 {
			fmt.Printf("║ %sUser Skins:%s\n", "\033[38;5;75m", "\033[0m")
			for _, name := range userSkins {
				marker := "  "
				if name == activeSkin {
					marker = "✓ "
				}
				fmt.Printf("║   %s%s%s\n", marker, name, reset())
			}
		}

		fmt.Println("╚══════════════════════════════════════════════════════════╝")
		fmt.Println()
		fmt.Printf("Active skin: %s%s%s\n", "\033[38;5;214m", activeSkin, reset())
	} else {
		// Simple list
		fmt.Println("Available skins:")
		fmt.Println("  Built-in:")
		for _, name := range mgr.ListBuiltin() {
			marker := ""
			if name == activeSkin {
				marker = " (active)"
			}
			fmt.Printf("    %s%s%s\n", "\033[38;5;220m", name, reset()+marker)
		}

		userSkins := mgr.ListUser()
		if len(userSkins) > 0 {
			fmt.Println("  User:")
			for _, name := range userSkins {
				marker := ""
				if name == activeSkin {
					marker = " (active)"
				}
				fmt.Printf("    %s%s%s\n", "\033[38;5;75m", name, reset()+marker)
			}
		}
	}
}

func runSkinShow(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()

	// Default to current skin
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		name = "default"
	}

	// Load active skin from config
	if name == "" {
		if cfg, err := config.Load(); err == nil {
			name = cfg.Display.Skin
			if name == "" {
				name = "default"
			}
		}
	}

	skinCfg, err := mgr.GetSkin(name)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	// Show skin details
	fmt.Printf("\n%s═══════════════════════════════════════════%s\n", "\033[38;5;220m", "\033[0m")
	fmt.Printf("  Skin: %s%s%s\n", "\033[38;5;214m", skinCfg.Name, "\033[0m")
	fmt.Printf("  Description: %s\n", skinCfg.Description)

	builtin := mgr.IsBuiltin(name)
	builtinStr := "User"
	if builtin {
		builtinStr = "Built-in"
	}
	fmt.Printf("  Type: %s\n", builtinStr)

	fmt.Printf("\n%sColors:%s\n", "\033[38;5;220m", "\033[0m")
	fmt.Printf("  Banner:     %s██%s %s██%s %s██%s\n",
		skinCfg.Colors.BannerBorder, "\033[0m",
		skinCfg.Colors.BannerTitle, "\033[0m",
		skinCfg.Colors.BannerAccent, "\033[0m")
	fmt.Printf("  Success:    %s██%s\n", skinCfg.Colors.Success, "\033[0m")
	fmt.Printf("  Error:      %s██%s\n", skinCfg.Colors.Error, "\033[0m")
	fmt.Printf("  Warning:    %s██%s\n", skinCfg.Colors.Warning, "\033[0m")

	fmt.Printf("\n%sSpinner:%s\n", "\033[38;5;220m", "\033[0m")
	frame := skinCfg.Spinner.Frames[0]
	fmt.Printf("  Frame: %s\n", frame)
	if len(skinCfg.Spinner.ThinkingVerbs) > 0 {
		fmt.Printf("  Verbs: %s\n", joinStrings(skinCfg.Spinner.ThinkingVerbs[:min(3, len(skinCfg.Spinner.ThinkingVerbs))]))
	}

	fmt.Printf("\n%sBranding:%s\n", "\033[38;5;220m", "\033[0m")
	fmt.Printf("  Agent Name:      %s\n", skinCfg.Branding.AgentName)
	fmt.Printf("  Prompt Symbol:   %s\n", skinCfg.Branding.PromptSymbol)
	fmt.Printf("  Tool Prefix:     %s\n", skinCfg.ToolPrefix)

	// Show tool emojis
	if len(skinCfg.ToolEmojis) > 0 {
		fmt.Printf("\n%sTool Emojis (first 5):%s\n", "\033[38;5;220m", "\033[0m")
		count := 0
		for tool, emoji := range skinCfg.ToolEmojis {
			if count >= 5 {
				break
			}
			fmt.Printf("  %s%s: %s\n", emoji, tool, reset())
			count++
		}
	}

	fmt.Printf("%s═══════════════════════════════════════════%s\n", "\033[38;5;220m", "\033[0m")
}

func runSkinPreview(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()

	name := "default"
	if len(args) > 0 {
		name = args[0]
	}

	skinCfg, err := mgr.GetSkin(name)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	renderer := skin.NewRenderer(skinCfg)

	// Show preview
	fmt.Println()
	fmt.Println(renderer.Banner("Skin Preview", name+" skin"))
	fmt.Println()
	fmt.Println(renderer.SectionHeader("Success/Error"))
	fmt.Println("  " + renderer.Success("Operation completed successfully"))
	fmt.Println("  " + renderer.Error("Failed to connect to server"))
	fmt.Println("  " + renderer.Warning("Configuration may be outdated"))
	fmt.Println()
	fmt.Println(renderer.SectionHeader("Tool Output"))
	fmt.Println("  " + renderer.ToolName("web_search") + " Searching...")
	fmt.Println(renderer.ToolPrefix() + "  " + renderer.ToolOutput("Found 42 results"))
	fmt.Println()
	fmt.Println(renderer.SectionHeader("Prompt"))
	fmt.Println("  " + renderer.Prompt() + renderer.AgentName() + " ")
	fmt.Println()
	fmt.Println(renderer.Welcome())
	fmt.Println()
}

func runSkinCreate(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()
	name := args[0]

	// Check if already exists
	if _, err := mgr.GetSkin(name); err == nil {
		fmt.Printf("Error: skin '%s' already exists\n", name)
		return
	}

	// Get source skin
	source := skinCreate
	if source == "" {
		source = "default"
	}

	sourceSkin, err := mgr.GetSkin(source)
	if err != nil {
		fmt.Printf("Error: source skin '%s' not found: %s\n", source, err)
		return
	}

	// Create new skin based on source
	newSkin := &skin.Config{
		Name:        name,
		Description: "Custom skin based on " + source,
		Colors:      sourceSkin.Colors,
		Spinner:     sourceSkin.Spinner,
		Branding:    sourceSkin.Branding,
		ToolPrefix:  sourceSkin.ToolPrefix,
		ToolEmojis:  make(skin.ToolEmojis),
	}

	// Copy tool emojis
	for k, v := range sourceSkin.ToolEmojis {
		newSkin.ToolEmojis[k] = v
	}

	if err := mgr.SaveUserSkin(name, newSkin); err != nil {
		fmt.Printf("Error: failed to save skin: %s\n", err)
		return
	}

	fmt.Printf("✓ Created skin '%s' based on '%s'\n", name, source)
	fmt.Printf("  Edit %s/skins/%s.yaml to customize\n", config.GetMagicHome(), name)
}

func runSkinDelete(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()
	name := args[0]

	// Check if it's a built-in skin
	if mgr.IsBuiltin(name) {
		fmt.Printf("Error: cannot delete built-in skin '%s'\n", name)
		return
	}

	if err := mgr.DeleteUserSkin(name); err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	fmt.Printf("✓ Deleted skin '%s'\n", name)
}

func runSkinExport(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()

	name := "default"
	if len(args) > 0 {
		name = args[0]
	}

	jsonStr, err := mgr.ExportJSON(name)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	// Pretty print
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err == nil {
		prettyJSON, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(prettyJSON))
	} else {
		fmt.Println(jsonStr)
	}
}

func runSkinSet(cmd *cobra.Command, args []string) {
	mgr := getSkinManager()
	name := args[0]

	// Verify skin exists
	if _, err := mgr.GetSkin(name); err != nil {
		fmt.Printf("Error: skin '%s' not found\n", name)
		return
	}

	// Save to config
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: failed to load config: %s\n", err)
		return
	}

	cfg.Display.Skin = name
	if err := cfg.Save(); err != nil {
		fmt.Printf("Error: failed to save config: %s\n", err)
		return
	}

	fmt.Printf("✓ Active skin set to '%s'\n", name)
}

// Helper functions

func reset() string {
	return "\033[0m"
}

func joinStrings(strs []string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure skins directory exists
func ensureSkinsDir() {
	skinDir := filepath.Join(config.GetMagicHome(), "skins")
	os.MkdirAll(skinDir, 0755)
}
