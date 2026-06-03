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

	// Ensure maps are initialized
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	if cfg.Gateway.Platforms == nil {
		cfg.Gateway.Platforms = make(map[string]config.PlatformConfig)
	}

	switch {
	// Top-level keys
	case key == "profile":
		cfg.Profile = value
	case key == "provider":
		cfg.Provider = value
	case key == "model":
		cfg.Model = value
	case key == "gateway.enabled":
		cfg.Gateway.Enabled = value == "true" || value == "1" || value == "yes"

	// Provider config: providers.<name>.api_key / base_url / model
	case strings.HasPrefix(key, "providers."):
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
			default:
				fmt.Printf("Unknown provider field: %s\n", field)
				fmt.Println("Available fields: api_key, base_url, model")
				os.Exit(1)
			}

			cfg.Providers[provider] = provCfg
		} else {
			fmt.Println("Usage: config set providers.<name>.<field> <value>")
			fmt.Println("Example: config set providers.deepseek.api_key sk-xxx")
			os.Exit(1)
		}

	// Gateway platform config: gateway.platforms.<name>.<field>
	case strings.HasPrefix(key, "gateway.platforms."):
		parts := strings.Split(key, ".")
		// gateway.platforms.<name>.<field> -> 4 parts
		if len(parts) >= 4 {
			platform := parts[2]
			field := parts[3]

			platCfg, ok := cfg.Gateway.Platforms[platform]
			if !ok {
				platCfg = config.PlatformConfig{}
			}

			switch field {
			case "enabled":
				platCfg.Enabled = value == "true" || value == "1" || value == "yes"
			case "token":
				platCfg.Token = value
			case "corp_id":
				platCfg.CorpID = value
			case "agent_id":
				platCfg.AgentID = value
			case "secret":
				platCfg.Secret = value
			case "app_id":
				platCfg.AppID = value
			case "app_secret":
				platCfg.AppSecret = value
			case "app_key":
				platCfg.AppKey = value
			case "number":
				platCfg.Number = value
			case "password":
				platCfg.Password = value
			case "api_url":
				platCfg.APIURL = value
			case "api_key":
				platCfg.APIKey = value
			case "verify_token":
				platCfg.VerifyToken = value
			case "aes_key":
				platCfg.AESKey = value
			case "client_id":
				platCfg.ClientID = value
			case "data_dir":
				platCfg.DataDir = value
			case "auto_login":
				platCfg.AutoLogin = value == "true" || value == "1" || value == "yes"
			default:
				fmt.Printf("Unknown platform field: %s\n", field)
				fmt.Println("Available fields:")
				fmt.Println("  enabled, token, corp_id, agent_id, secret")
				fmt.Println("  app_id, app_secret, app_key, number, password")
				fmt.Println("  api_url, api_key, verify_token, aes_key")
				fmt.Println("  client_id, data_dir, auto_login")
				os.Exit(1)
			}

			cfg.Gateway.Platforms[platform] = platCfg
		} else {
			fmt.Println("Usage: config set gateway.platforms.<name>.<field> <value>")
			fmt.Println("Example: config set gateway.platforms.telegram.token 123:abc")
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown config key: %s\n", key)
		fmt.Println("Available keys:")
		fmt.Println("  profile, provider, model")
		fmt.Println("  providers.<name>.api_key, providers.<name>.base_url, providers.<name>.model")
		fmt.Println("  gateway.enabled")
		fmt.Println("  gateway.platforms.<name>.token, gateway.platforms.<name>.enabled, ...")
		os.Exit(1)
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

	switch {
	case key == "profile":
		fmt.Println(cfg.Profile)
	case key == "provider":
		fmt.Println(cfg.Provider)
	case key == "model":
		fmt.Println(cfg.Model)
	case key == "magic_home":
		fmt.Println(cfg.MagicHome)
	case key == "gateway.enabled":
		fmt.Println(cfg.Gateway.Enabled)

	// Provider config
	case strings.HasPrefix(key, "providers."):
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
		} else {
			fmt.Printf("Usage: config get providers.<name>.<field>\n")
			os.Exit(1)
		}

	// Gateway platform config
	case strings.HasPrefix(key, "gateway.platforms."):
		parts := strings.Split(key, ".")
		if len(parts) == 4 {
			platform := parts[2]
			field := parts[3]

			platCfg, ok := cfg.Gateway.Platforms[platform]
			if !ok {
				fmt.Printf("Platform %s not found\n", platform)
				os.Exit(1)
			}

			switch field {
			case "enabled":
				fmt.Println(platCfg.Enabled)
			case "token":
				fmt.Println(platCfg.Token)
			case "corp_id":
				fmt.Println(platCfg.CorpID)
			case "agent_id":
				fmt.Println(platCfg.AgentID)
			case "secret":
				fmt.Println(platCfg.Secret)
			case "app_id":
				fmt.Println(platCfg.AppID)
			case "app_secret":
				fmt.Println(platCfg.AppSecret)
			case "app_key":
				fmt.Println(platCfg.AppKey)
			case "number":
				fmt.Println(platCfg.Number)
			case "password":
				fmt.Println(platCfg.Password)
			case "api_url":
				fmt.Println(platCfg.APIURL)
			case "api_key":
				fmt.Println(platCfg.APIKey)
			case "verify_token":
				fmt.Println(platCfg.VerifyToken)
			case "aes_key":
				fmt.Println(platCfg.AESKey)
			case "client_id":
				fmt.Println(platCfg.ClientID)
			case "data_dir":
				fmt.Println(platCfg.DataDir)
			case "auto_login":
				fmt.Println(platCfg.AutoLogin)
			default:
				fmt.Printf("Unknown field: %s\n", field)
				os.Exit(1)
			}
		} else {
			fmt.Printf("Usage: config get gateway.platforms.<name>.<field>\n")
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown config key: %s\n", key)
		fmt.Println("Available keys:")
		fmt.Println("  profile, provider, model, magic_home, gateway.enabled")
		fmt.Println("  providers.<name>.api_key, providers.<name>.base_url, providers.<name>.model")
		fmt.Println("  gateway.platforms.<name>.<field>")
		os.Exit(1)
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
	fmt.Printf("Primary Provider: %s\n", cfg.Provider)
	fmt.Printf("Model:        %s\n", cfg.Model)

	fmt.Println("\nProviders:")
	if len(cfg.Providers) == 0 {
		fmt.Println("  (none configured)")
	} else {
		for name, prov := range cfg.Providers {
			isPrimary := ""
			if name == cfg.Provider {
				isPrimary = " (active)"
			}
			fmt.Printf("  %s%s:\n", name, isPrimary)
			fmt.Printf("    API Key:  %s\n", maskSecret(prov.APIKey))
			fmt.Printf("    Base URL: %s\n", prov.BaseURL)
			fmt.Printf("    Model:    %s\n", prov.Model)
		}
	}

	fmt.Println("\nTools:")
	fmt.Printf("  Enabled:  %v\n", cfg.Tools.Enabled)
	fmt.Printf("  Disabled: %v\n", cfg.Tools.Disabled)

	fmt.Println("\nSkills:")
	fmt.Printf("  Enabled:  %v\n", cfg.Skills.Enabled)
	fmt.Printf("  Disabled: %v\n", cfg.Skills.Disabled)

	fmt.Println("\nGateway:")
	fmt.Printf("  Enabled: %v\n", cfg.Gateway.Enabled)
	if len(cfg.Gateway.Platforms) > 0 {
		fmt.Println("  Platforms:")
		for name, plat := range cfg.Gateway.Platforms {
			status := "disabled"
			if plat.Enabled {
				status = "enabled"
			}
			fmt.Printf("    %s (%s):\n", name, status)
			if plat.Token != "" {
				fmt.Printf("      token:        %s\n", maskSecret(plat.Token))
			}
			if plat.CorpID != "" {
				fmt.Printf("      corp_id:      %s\n", plat.CorpID)
			}
			if plat.AgentID != "" {
				fmt.Printf("      agent_id:     %s\n", plat.AgentID)
			}
			if plat.Secret != "" {
				fmt.Printf("      secret:       %s\n", maskSecret(plat.Secret))
			}
			if plat.AppID != "" {
				fmt.Printf("      app_id:       %s\n", plat.AppID)
			}
			if plat.AppSecret != "" {
				fmt.Printf("      app_secret:   %s\n", maskSecret(plat.AppSecret))
			}
			if plat.AppKey != "" {
				fmt.Printf("      app_key:      %s\n", maskSecret(plat.AppKey))
			}
			if plat.Number != "" {
				fmt.Printf("      number:       %s\n", plat.Number)
			}
			if plat.ClientID != "" {
				fmt.Printf("      client_id:    %s\n", plat.ClientID)
			}
			if plat.APIURL != "" {
				fmt.Printf("      api_url:      %s\n", plat.APIURL)
			}
		}
	} else {
		fmt.Println("  (no platforms configured)")
	}

	// Show Cortex
	fmt.Printf("\nCortex AI:\n")
	fmt.Printf("  Enabled: %v\n", cfg.CortexEnabled)
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

	// Check gateway platforms
	if cfg.Gateway.Enabled {
		for name, plat := range cfg.Gateway.Platforms {
			if plat.Enabled {
				switch name {
				case "telegram", "discord":
					if plat.Token == "" {
						errors = append(errors, fmt.Sprintf("Gateway platform '%s' enabled but token is empty", name))
					}
				case "wecom":
					if plat.CorpID == "" || plat.Secret == "" {
						errors = append(errors, fmt.Sprintf("Gateway platform '%s' enabled but corp_id/secret missing", name))
					}
				}
			}
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
