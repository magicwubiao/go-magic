package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-magic/magic/internal/config"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose any issues",
	Long:  "Run diagnostics to check magic Agent setup and configuration",
	Run:   runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) {
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║                    magic Doctor                            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()

	allGood := true
	issues := []string{}

	// Check 1: System info
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ System Information                                        │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")
	fmt.Printf("  OS:       %s %s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go:       %s\n", runtime.Version())
	fmt.Printf("  Version:  %s\n", getVersion())
	fmt.Println()

	// Check 2: Config file
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ Configuration                                             │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("  %s Cannot determine home directory\n", cross())
		allGood = false
		issues = append(issues, "Cannot determine home directory")
	} else {
		configPath := filepath.Join(home, ".magic", "config.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Printf("  %s Config file not found: %s\n", cross(), configPath)
			fmt.Printf("    → Run 'magic setup' to create configuration\n")
			allGood = false
			issues = append(issues, "Config file not found")
		} else {
			fmt.Printf("  %s Config file exists\n", check())

			// Validate config
			cfg, err := config.Load()
			if err != nil {
				fmt.Printf("  %s Config file invalid: %v\n", cross(), err)
				allGood = false
				issues = append(issues, "Config file invalid: "+err.Error())
			} else {
				fmt.Printf("  %s Config valid\n", check())

				// Check provider
				if cfg.Provider == "" {
					fmt.Printf("  %s Provider not set\n", cross())
					fmt.Printf("    → Run 'magic model' to configure\n")
					allGood = false
					issues = append(issues, "Provider not set")
				} else {
					fmt.Printf("  %s Provider: %s\n", check(), cfg.Provider)
				}

				// Check model
				if cfg.Model == "" {
					fmt.Printf("  %s Model not set\n", cross())
					fmt.Printf("    → Run 'magic model' to configure\n")
					allGood = false
					issues = append(issues, "Model not set")
				} else {
					fmt.Printf("  %s Model: %s\n", check(), cfg.Model)
				}

				// Check API key
				if cfg.APIKey == "" && cfg.Providers != nil {
					if provCfg, ok := cfg.Providers[cfg.Provider]; ok && provCfg.APIKey == "" {
						fmt.Printf("  %s API key not configured for %s\n", cross(), cfg.Provider)
						fmt.Printf("    → Run 'magic setup' or set API key in .env\n")
						allGood = false
						issues = append(issues, "API key not configured")
					} else {
						fmt.Printf("  %s API key configured\n", check())
					}
				} else {
					fmt.Printf("  %s API key configured\n", check())
				}

				// Check toolsets
				if len(cfg.Tools.Enabled) == 0 {
					fmt.Printf("  %s No toolsets enabled\n", cross())
					fmt.Printf("    → Run 'magic tools' to configure toolsets\n")
				} else {
					fmt.Printf("  %s Toolsets: %d enabled\n", check(), len(cfg.Tools.Enabled))
				}
			}
		}
	}
	fmt.Println()

	// Check 3: Network connectivity
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ Network Connectivity                                     │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")

	// Check internet
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://www.google.com")
	if err != nil {
		fmt.Printf("  %s Internet connectivity failed\n", cross())
		fmt.Printf("    → Check your network connection\n")
		allGood = false
		issues = append(issues, "Internet connectivity failed")
	} else {
		resp.Body.Close()
		fmt.Printf("  %s Internet connectivity OK\n", check())
	}

	// Check provider endpoint (if configured)
	cfg, _ := config.Load()
	if cfg != nil && cfg.Provider != "" {
		fmt.Printf("  Checking %s endpoint... ", cfg.Provider)
		ep := getProviderEndpoint(cfg.Provider)
		if ep != "" {
			req, _ := http.NewRequest("GET", ep, nil)
			req.Header.Set("Authorization", "Bearer test")
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("unreachable\n")
			} else {
				resp.Body.Close()
				if resp.StatusCode == 401 || resp.StatusCode == 403 {
					fmt.Printf("reachable (auth required)\n")
					fmt.Printf("    %s Provider endpoint is accessible\n", check())
				} else if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					fmt.Printf("reachable\n")
					fmt.Printf("    %s Provider endpoint is accessible\n", check())
				} else {
					fmt.Printf("error (%d)\n", resp.StatusCode)
				}
			}
		}
	}
	fmt.Println()

	// Check 4: Directory structure
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ Directory Structure                                       │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")

	dirs := []struct {
		name    string
		path    string
		created bool
	}{
		{"Config", filepath.Join(home, ".magic")},
		{"Sessions", filepath.Join(home, ".magic", "sessions")},
		{"Skills", filepath.Join(home, ".magic", "skills")},
		{"Memory", filepath.Join(home, ".magic", "memory")},
		{"Logs", filepath.Join(home, ".magic", "logs")},
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir.path); os.IsNotExist(err) {
			fmt.Printf("  %s %s directory missing\n", cross(), dir.name)
		} else {
			fmt.Printf("  %s %s directory OK\n", check(), dir.name)
		}
	}
	fmt.Println()

	// Check 5: MCP Servers (if configured)
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ MCP Servers                                              │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")

	mcpConfigured := false
	if cfg != nil && cfg.MCP != nil && len(cfg.MCP.Servers) > 0 {
		mcpConfigured = true
		for name, server := range cfg.MCP.Servers {
			if server.Enabled == false {
				fmt.Printf("  %s MCP server '%s' (disabled)\n", info(), name)
			} else {
				fmt.Printf("  %s MCP server '%s' configured\n", check(), name)
			}
		}
	}

	if !mcpConfigured {
		fmt.Printf("  %s No MCP servers configured\n", info())
		fmt.Printf("    → Add MCP servers in config.yaml to extend capabilities\n")
	}
	fmt.Println()

	// Check 6: Platform channels (if configured)
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ Platform Channels                                         │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")

	platformsConfigured := false
	if cfg != nil && cfg.Gateway != nil {
		platforms := []string{}
		if cfg.Gateway.Telegram != nil && cfg.Gateway.Telegram.BotToken != "" {
			platforms = append(platforms, "Telegram")
		}
		if cfg.Gateway.Discord != nil && cfg.Gateway.Discord.BotToken != "" {
			platforms = append(platforms, "Discord")
		}
		if cfg.Gateway.Slack != nil && cfg.Gateway.Slack.BotToken != "" {
			platforms = append(platforms, "Slack")
		}
		if cfg.Gateway.WeCom != nil && cfg.Gateway.WeCom.CorpID != "" {
			platforms = append(platforms, "WeCom")
		}
		if cfg.Gateway.Feishu != nil && cfg.Gateway.Feishu.AppID != "" {
			platforms = append(platforms, "Feishu")
		}

		if len(platforms) > 0 {
			platformsConfigured = true
			for _, p := range platforms {
				fmt.Printf("  %s %s configured\n", check(), p)
			}
		}
	}

	if !platformsConfigured {
		fmt.Printf("  %s No platform channels configured\n", info())
		fmt.Printf("    → Run 'magic gateway setup' to configure messaging\n")
	}
	fmt.Println()

	// Summary
	fmt.Println("┌──────────────────────────────────────────────────────────┐")
	fmt.Println("│ Summary                                                   │")
	fmt.Println("└──────────────────────────────────────────────────────────┘")
	if allGood {
		fmt.Println("  ✓ All checks passed! magic is ready to use.")
		fmt.Println()
		fmt.Println("  Next steps:")
		fmt.Println("    • Run 'magic chat' to start chatting")
		fmt.Println("    • Run 'magic model' to change model")
		fmt.Println("    • Run 'magic tools' to configure toolsets")
	} else {
		fmt.Println("  ✗ Some checks failed. Please review the issues above.")
		fmt.Println()
		fmt.Println("  Quick fixes:")
		fmt.Println("    • Run 'magic setup' for interactive configuration")
		fmt.Println("    • Run 'magic model' to configure your AI provider")
		fmt.Println("    • Run 'magic tools' to configure tool access")
	}
	fmt.Println()
}

// Helper functions
func check() string {
	return "\033[32m✓\033[0m"
}

func cross() string {
	return "\033[31m✗\033[0m"
}

func info() string {
	return "\033[33mℹ\033[0m"
}

func getVersion() string {
	return "0.0.1c"
}

func getProviderEndpoint(provider string) string {
	endpoints := map[string]string{
		"openai":    "https://api.openai.com/v1/models",
		"anthropic": "https://api.anthropic.com/v1/models",
		"deepseek":  "https://api.deepseek.com/v1/models",
		"google":    "https://generativelanguage.googleapis.com/v1/models",
		"azure":     "https://openai.azure.com/v1/models",
	}
	return endpoints[provider]
}

// QuickFix attempts to fix common issues
func QuickFix(issue string) {
	fmt.Println()
	fmt.Printf("Attempting to fix: %s\n", issue)

	reader := bufio.NewReader(os.Stdin)

	switch issue {
	case "config":
		fmt.Println("Running setup wizard...")
		// Run setup
	case "api_key":
		fmt.Print("Enter API key: ")
		apiKey, _ := reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		if apiKey != "" {
			// Save to .env
			home, _ := os.UserHomeDir()
			envPath := filepath.Join(home, ".magic", ".env")
			os.MkdirAll(filepath.Dir(envPath), 0755)
			os.WriteFile(envPath, []byte("API_KEY="+apiKey), 0600)
			fmt.Println("API key saved to ~/.magic/.env")
		}
	}
}
