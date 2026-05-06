package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/pkg/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set individual config values",
	Args:  cobra.ExactArgs(2),
	Run:   runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get config value",
	Args:  cobra.ExactArgs(1),
	Run:   runConfigGet,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration",
	Run:   runConfigList,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show config file path",
	Run:   runConfigPath,
}

var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset config to defaults",
	Run:   runConfigReset,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate current configuration",
	Run:   runConfigValidate,
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configValidateCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigSet(cmd *cobra.Command, args []string) {
	key := args[0]
	value := args[1]

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(key) {
	case "profile":
		cfg.Profile = value
	case "provider":
		cfg.Provider = value
	case "model":
		cfg.Model = value
	default:
		if strings.HasPrefix(key, "providers.") {
			parts := strings.Split(key, ".")
			if len(parts) == 3 {
				provider := parts[1]
				field := parts[2]

				provCfg, ok := cfg.Providers[provider]
				if !ok {
					provCfg = config.ProviderConfig{}
				}

				switch field {
				case "api_key":
					provCfg.APIKey = value
				case "base_url":
					provCfg.BaseURL = value
				case "model":
					provCfg.Model = value
				}

				cfg.Providers[provider] = provCfg
			}
		} else {
			fmt.Printf("Unknown config key: %s\n", key)
			fmt.Println("Available keys: profile, provider, model, providers.<name>.api_key, providers.<name>.base_url")
			os.Exit(1)
		}
	}

	err = cfg.Save()
	if err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config '%s' set to '%s'\n", key, value)
}

func runConfigGet(cmd *cobra.Command, args []string) {
	key := args[0]

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(key) {
	case "profile":
		fmt.Println(cfg.Profile)
	case "provider":
		fmt.Println(cfg.Provider)
	case "model":
		fmt.Println(cfg.Model)
	case "magic_home":
		fmt.Println(cfg.MagicHome)
	default:
		if strings.HasPrefix(key, "providers.") {
			parts := strings.Split(key, ".")
			if len(parts) == 3 {
				provider := parts[1]
				field := parts[2]

				provCfg, ok := cfg.Providers[provider]
				if !ok {
					fmt.Printf("Provider %s not found\n", provider)
					os.Exit(1)
				}

				switch field {
				case "api_key":
					fmt.Println(provCfg.APIKey)
				case "base_url":
					fmt.Println(provCfg.BaseURL)
				case "model":
					fmt.Println(provCfg.Model)
				default:
					fmt.Printf("Unknown field: %s\n", field)
					os.Exit(1)
				}
			}
		} else {
			fmt.Printf("Unknown config key: %s\n", key)
			os.Exit(1)
		}
	}
}

func runConfigList(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== magic Configuration ===")
	fmt.Printf("Profile:     %s\n", cfg.Profile)
	fmt.Printf("Magic Home:  %s\n", cfg.MagicHome)
	fmt.Printf("Provider:     %s\n", cfg.Provider)
	fmt.Printf("Model:        %s\n", cfg.Model)

	fmt.Println("\nProviders:")
	if len(cfg.Providers) == 0 {
		fmt.Println("  (none configured)")
	} else {
		for name, prov := range cfg.Providers {
			fmt.Printf("  %s:\n", name)
			fmt.Printf("    API Key:  %s\n", maskSecret(prov.APIKey))
			fmt.Printf("    Base URL: %s\n", prov.BaseURL)
			fmt.Printf("    Model:    %s\n", prov.Model)
		}
	}

	fmt.Println("\nTools:")
	fmt.Printf("  Enabled:  %v\n", cfg.Tools.Enabled)
	fmt.Printf("  Disabled: %v\n", cfg.Tools.Disabled)

	fmt.Println("\nGateway:")
	fmt.Printf("  Enabled: %v\n", cfg.Gateway.Enabled)
	if len(cfg.Gateway.Platforms) > 0 {
		fmt.Println("  Platforms:")
		for name, plat := range cfg.Gateway.Platforms {
			fmt.Printf("    %s: enabled=%v\n", name, plat.Enabled)
		}
	}
}

func runConfigPath(cmd *cobra.Command, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get home directory: %v\n", err)
		os.Exit(1)
	}

	configPath := filepath.Join(home, ".magic", "config.json")
	fmt.Println(configPath)

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("(file exists)")
	} else {
		fmt.Println("(file does not exist, will be created on save)")
	}
}

func runConfigReset(cmd *cobra.Command, args []string) {
	cfg := config.DefaultConfig()
	err := cfg.Save()
	if err != nil {
		fmt.Printf("Failed to reset config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configuration reset to defaults.")
	fmt.Println("Run 'magic config list' to see current configuration.")
}

func runConfigValidate(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	errors := make([]string, 0)

	// Check provider
	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		errors = append(errors, fmt.Sprintf("Provider '%s' not configured", cfg.Provider))
	} else {
		if provCfg.APIKey == "" {
			errors = append(errors, "API key not set for current provider")
		}
		if provCfg.Model == "" {
			errors = append(errors, "Model not set for current provider")
		}
	}

	// Check magic home
	home, err := os.UserHomeDir()
	if err == nil {
		magicDir := filepath.Join(home, ".magic")
		if _, err := os.Stat(magicDir); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("magic directory does not exist: %s", magicDir))
		}
	}

	if len(errors) == 0 {
		fmt.Println("✓ Configuration is valid!")
	} else {
		fmt.Println("Configuration issues found:")
		for _, e := range errors {
			fmt.Printf("  ✗ %s\n", e)
		}
		os.Exit(1)
	}
}

func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
