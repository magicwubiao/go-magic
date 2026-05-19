package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate OpenClaw config to go-magic",
	Long:  `Migrate existing OpenClaw configuration and data to go-magic format.`,
	RunE:  runMigrate,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	openclawDir := filepath.Join(homeDir, ".openclaw")
	magicDir := filepath.Join(homeDir, ".magic")

	// Check if OpenClaw config exists
	if _, err := os.Stat(openclawDir); os.IsNotExist(err) {
		return fmt.Errorf("no OpenClaw config found at %s", openclawDir)
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║              go-magic Migration Tool                      ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Source: %s\n", openclawDir)
	fmt.Printf("Target: %s\n", magicDir)
	fmt.Println()

	// Ensure .magic directory exists
	if err := os.MkdirAll(magicDir, 0755); err != nil {
		return fmt.Errorf("failed to create .magic directory: %w", err)
	}

	// Load or create config
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Migrate config.json if exists
	openclawConfig := filepath.Join(openclawDir, "config.json")
	if _, err := os.Stat(openclawConfig); err == nil {
		fmt.Println("Migrating config.json...")
		if err := migrateConfig(openclawConfig, cfg); err != nil {
			fmt.Printf("  Warning: failed to migrate config: %v\n", err)
		} else {
			fmt.Println("  ✓ Config migrated")
		}
	}

	// Migrate sessions if exists
	openclawSessions := filepath.Join(openclawDir, "sessions.db")
	if _, err := os.Stat(openclawSessions); err == nil {
		fmt.Println("Migrating sessions database...")
		magicSessions := filepath.Join(magicDir, "sessions.db")
		if err := copyFileSimple(openclawSessions, magicSessions); err != nil {
			fmt.Printf("  Warning: failed to migrate sessions: %v\n", err)
		} else {
			fmt.Println("  ✓ Sessions migrated")
		}
	}

	// Migrate skills if exists
	openclawSkills := filepath.Join(openclawDir, "skills")
	if _, err := os.Stat(openclawSkills); err == nil {
		fmt.Println("Migrating skills...")
		magicSkills := filepath.Join(magicDir, "skills")
		if err := copyDirRecursive(openclawSkills, magicSkills); err != nil {
			fmt.Printf("  Warning: failed to migrate skills: %v\n", err)
		} else {
			fmt.Println("  ✓ Skills migrated")
		}
	}

	// Save migrated config
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save migrated config: %w", err)
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║               Migration Complete!                         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Your OpenClaw config has been migrated to go-magic.")
	fmt.Println("You can now use 'magic' command with your existing configuration.")
	fmt.Println()

	return nil
}

// migrateConfig migrates OpenClaw config to go-magic format
func migrateConfig(srcPath string, cfg *config.Config) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	// Try to parse as generic JSON
	var openclawConfig map[string]interface{}
	if err := json.Unmarshal(data, &openclawConfig); err != nil {
		return err
	}

	// Migrate common fields
	if provider, ok := openclawConfig["provider"].(string); ok {
		cfg.Provider = provider
	}
	if model, ok := openclawConfig["model"].(string); ok {
		cfg.Model = model
	}

	// Migrate providers map
	if providers, ok := openclawConfig["providers"].(map[string]interface{}); ok {
		for name, p := range providers {
			if providerMap, ok := p.(map[string]interface{}); ok {
				pc := config.ProviderConfig{}
				if apiKey, ok := providerMap["api_key"].(string); ok {
					pc.APIKey = apiKey
				}
				if baseURL, ok := providerMap["base_url"].(string); ok {
					pc.BaseURL = baseURL
				}
				if model, ok := providerMap["model"].(string); ok {
					pc.Model = model
				}
				if cfg.Providers == nil {
					cfg.Providers = make(map[string]config.ProviderConfig)
				}
				cfg.Providers[name] = pc
			}
		}
	}

	// Migrate gateway config
	if gateway, ok := openclawConfig["gateway"].(map[string]interface{}); ok {
		if enabled, ok := gateway["enabled"].(bool); ok {
			cfg.Gateway.Enabled = enabled
		}
		if platforms, ok := gateway["platforms"].(map[string]interface{}); ok {
			for name, p := range platforms {
				if platformMap, ok := p.(map[string]interface{}); ok {
					pc := config.PlatformConfig{}
					if enabled, ok := platformMap["enabled"].(bool); ok {
						pc.Enabled = enabled
					}
					if token, ok := platformMap["token"].(string); ok {
						pc.Token = token
					}
					if cfg.Gateway.Platforms == nil {
						cfg.Gateway.Platforms = make(map[string]config.PlatformConfig)
					}
					cfg.Gateway.Platforms[name] = pc
				}
			}
		}
	}

	return nil
}

// copyFileSimple copies a file from src to dst (simple version for migration)
func copyFileSimple(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// copyDirRecursive copies a directory from src to dst recursively
func copyDirRecursive(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFileSimple(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}
