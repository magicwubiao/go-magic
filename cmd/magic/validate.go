package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/pkg/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	Run:   runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) {
	fmt.Println("Validating configuration...")
	fmt.Println()

	// Check if config file exists
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error: Cannot get home directory: %v\n", err)
		os.Exit(1)
	}

	configPath := filepath.Join(home, ".magic", "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("Config file not found: %s\n", configPath)
		fmt.Println("Run 'magic setup' to create configuration.")
		os.Exit(1)
	}

	fmt.Printf("Found config: %s\n", configPath)

	// Try to load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Configuration is valid!")
	fmt.Println()

	// Show summary
	fmt.Println("Configuration Summary:")
	fmt.Printf("  Profile: %s\n", cfg.Profile)
	fmt.Printf("  Provider: %s\n", cfg.Provider)
	fmt.Printf("  Model: %s\n", cfg.Model)

	if _, ok := cfg.Providers[cfg.Provider]; ok {
		fmt.Println("  Provider config: ✓")
	} else {
		fmt.Println("  Provider config: ✗ (not configured)")
	}

	fmt.Printf("  Gateway: %v\n", cfg.Gateway.Enabled)
	fmt.Printf("  Tools enabled: %v\n", cfg.Tools.Enabled)

	fmt.Println()
	fmt.Println("Validation complete.")
}
