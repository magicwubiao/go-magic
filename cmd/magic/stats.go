package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show magic usage statistics",
	Long: `Display usage statistics for magic Agent.
	
Shows information about:
  - Configuration (profile, provider, model)
  - Number of skills loaded
  - Session database status
  - Log files count
  - Tools status (enabled/disabled)
	
This is useful for getting an overview of your magic setup.`,
	Run: runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== magic Statistics ===")
	fmt.Println()

	// Config info
	fmt.Println("Configuration:")
	fmt.Printf("  Profile:  %s\n", cfg.Profile)
	fmt.Printf("  Provider: %s\n", cfg.Provider)
	fmt.Printf("  Model:    %s\n", cfg.Model)
	fmt.Println()

	// Skills count
	mgr, err := skills.NewManager()
	skillCount := 0
	if err == nil {
		skillCount = len(mgr.List())
	}
	fmt.Printf("Skills:   %d loaded\n", skillCount)

	// Sessions count
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".magic", "sessions.db")
	if _, err := os.Stat(dbPath); err == nil {
		// Simple file size as proxy
		info, _ := os.Stat(dbPath)
		fmt.Printf("Sessions: Database exists (%.1f KB)\n", float64(info.Size())/1024)
	} else {
		fmt.Println("Sessions: No sessions yet")
	}

	// Logs
	logsDir := filepath.Join(home, ".magic", "logs")
	entries, err := os.ReadDir(logsDir)
	if err == nil {
		fmt.Printf("Logs:     %d log files\n", len(entries))
	} else {
		fmt.Println("Logs:     No logs found")
	}

	fmt.Println()

	// Tools status
	fmt.Println("Tools:")
	registry := tool.NewRegistry()
	registry.Register(&tool.ReadFileTool{})
	registry.Register(&tool.WriteFileTool{})
	registry.Register(&tool.WebSearchTool{})
	registry.Register(&tool.WebExtractTool{})
	registry.Register(&tool.ExecuteCommandTool{})

	for _, t := range registry.List() {
		enabled := "enabled"
		for _, d := range cfg.Tools.Disabled {
			if d == t {
				enabled = "disabled"
				break
			}
		}
		fmt.Printf("  [%s] %s\n", enabled, t)
	}
}
