package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// setupCmd represents the setup command
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	Long:  `Run the interactive setup wizard to configure go-magic.`,
	RunE:  runSetup,
}

var (
	skipModel   bool
	skipTools   bool
	skipGateway bool
)

func init() {
	setupCmd.Flags().BoolVar(&skipModel, "skip-model", false, "Skip model selection")
	setupCmd.Flags().BoolVar(&skipTools, "skip-tools", false, "Skip tool configuration")
	setupCmd.Flags().BoolVar(&skipGateway, "skip-gateway", false, "Skip gateway setup")
	rootCmd.AddCommand(setupCmd)
}

func runSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)
	homeDir, _ := os.UserHomeDir()
	magicDir := filepath.Join(homeDir, ".go-magic")

	// Banner
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║              go-magic Setup Wizard                        ║")
	fmt.Println("║         High-performance AI Agent in Go                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Create directories
	if err := os.MkdirAll(magicDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check for OpenClaw migration
	openclawDir := filepath.Join(homeDir, ".openclaw")
	if _, err := os.Stat(openclawDir); err == nil {
		fmt.Printf("📦 OpenClaw detected at %s\n", openclawDir)
		fmt.Print("   Would you like to migrate? (y/N): ")
		if answer, _ := reader.ReadString('\n'); strings.TrimSpace(strings.ToLower(answer)) == "y" {
			fmt.Println("   Run 'magic migrate' after setup to complete migration.")
		}
		fmt.Println()
	}

	// Step 1: Provider selection
	var selectedProvider, selectedModel string
	if !skipModel {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println(" Step 1: Select AI Provider")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		selectedProvider, selectedModel = runProviderSetup(reader)
	}

	// Step 2: Tool configuration
	var enabledTools []string
	if !skipTools {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println(" Step 2: Configure Tools")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		enabledTools = runToolsetSetup(reader)
	}

	// Step 3: Gateway setup
	if !skipGateway {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println(" Step 3: Gateway Setup (Optional)")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		runGatewaySetup(reader)
	}

	// Step 4: Save config
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println(" Step 4: Saving Configuration")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	configPath := filepath.Join(magicDir, "config.yaml")
	if err := saveSimpleConfig(configPath, selectedProvider, selectedModel, enabledTools); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Verify
	fmt.Println()
	fmt.Println("✓ Configuration saved!")
	verifySetup()

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Setup Complete!                         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  • Run 'magic' to start chatting")
	fmt.Println("  • Run 'magic model' to change AI provider")
	fmt.Println("  • Run 'magic gateway start' to enable messaging platforms")
	fmt.Println()
	fmt.Println("For help, visit: https://github.com/magicwubiao/go-magic")
	fmt.Println()

	return nil
}

func runProviderSetup(reader *bufio.Reader) (provider, model string) {
	providers := []struct {
		name        string
		displayName string
		description string
		models      []string
	}{
		{
			name:        "openai",
			displayName: "OpenAI",
			description: "GPT-4, GPT-4o, GPT-4o-mini",
			models:      []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-4"},
		},
		{
			name:        "anthropic",
			displayName: "Anthropic",
			description: "Claude 3.5 Sonnet, Claude Opus",
			models:      []string{"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022", "claude-3-opus-20240229"},
		},
		{
			name:        "deepseek",
			displayName: "DeepSeek",
			description: "DeepSeek V3, DeepSeek Coder",
			models:      []string{"deepseek-chat", "deepseek-coder"},
		},
		{
			name:        "ollama",
			displayName: "Ollama",
			description: "Local models (Llama, Mistral, etc.)",
			models:      []string{"llama3.2", "mistral", "codellama"},
		},
		{
			name:        "openrouter",
			displayName: "OpenRouter",
			description: "Access to 100+ models",
			models:      []string{"anthropic/claude-3.5-sonnet", "openai/gpt-4o", "deepseek/deepseek-chat-v3"},
		},
	}

	fmt.Println("   Select a provider:")
	fmt.Println()
	for i, p := range providers {
		fmt.Printf("      [%d] %s - %s\n", i+1, p.displayName, p.description)
	}
	fmt.Println()
	fmt.Print("   Enter number (default: 1): ")

	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(selection)
	if selection == "" {
		selection = "1"
	}

	num, err := strconv.Atoi(selection)
	if err != nil || num < 1 || num > len(providers) {
		num = 1
	}

	selected := providers[num-1]
	fmt.Printf("   Selected: %s\n", selected.displayName)
	fmt.Println()

	// Model selection
	if len(selected.models) > 0 {
		fmt.Printf("   Select a model for %s:\n", selected.displayName)
		for i, m := range selected.models {
			fmt.Printf("      [%d] %s\n", i+1, m)
		}
		fmt.Println()
		fmt.Print("   Enter number (default: 1): ")

		modelSel, _ := reader.ReadString('\n')
		modelSel = strings.TrimSpace(modelSel)
		if modelSel == "" {
			modelSel = "1"
		}

		mNum, _ := strconv.Atoi(modelSel)
		if mNum < 1 || mNum > len(selected.models) {
			mNum = 1
		}

		fmt.Printf("   Selected: %s\n", selected.models[mNum-1])
		fmt.Println()

		return selected.name, selected.models[mNum-1]
	}

	return selected.name, ""
}

func runToolsetSetup(reader *bufio.Reader) []string {
	toolsets := []struct {
		name        string
		description string
	}{
		{"web", "Web search and content extraction"},
		{"browser", "Browser automation"},
		{"terminal", "Terminal command execution"},
		{"file", "File read/write/edit operations"},
		{"code_execution", "Python code execution"},
		{"skills", "Skill management"},
		{"memory", "Persistent memory"},
		{"delegation", "Sub-agent delegation"},
		{"homeassistant", "Smart home control"},
		{"cron", "Scheduled tasks"},
	}

	fmt.Println("   Available toolsets:")
	fmt.Println()
	for i, ts := range toolsets {
		fmt.Printf("      [%d] %s - %s\n", i+1, ts.name, ts.description)
	}
	fmt.Println()
	fmt.Print("   Enter numbers to enable (e.g. 1,2,5) or 'all': ")

	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(selection)

	if selection == "" || selection == "all" {
		fmt.Println("   Enabled: all toolsets")
		fmt.Println()
		return []string{"all"}
	}

	parts := strings.Split(selection, ",")
	enabled := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if num, err := strconv.Atoi(part); err == nil {
			idx := num - 1
			if idx >= 0 && idx < len(toolsets) {
				enabled = append(enabled, toolsets[idx].name)
			}
		}
	}
	fmt.Printf("   Enabled: %s\n", strings.Join(enabled, ", "))
	fmt.Println()

	return enabled
}

func runGatewaySetup(reader *bufio.Reader) {
	fmt.Println("   Would you like to set up a messaging gateway?")
	fmt.Println("   This enables Telegram, Discord, Slack, etc.")
	fmt.Println()
	fmt.Print("   (y/N): ")

	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "y" {
		fmt.Println("   Run 'magic gateway setup' after completion.")
	}
	fmt.Println()
}

func saveSimpleConfig(path, provider, model string, tools []string) error {
	// Create backup if exists
	if _, err := os.Stat(path); err == nil {
		backup := path + ".bak"
		os.Rename(path, backup)
	}

	// Write new config
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Simple YAML output
	fmt.Fprintf(file, "# go-magic configuration\n")
	fmt.Fprintf(file, "# Generated by setup wizard\n\n")

	fmt.Fprintf(file, "cortex:\n")
	fmt.Fprintf(file, "  provider: %s\n", provider)
	if model != "" {
		fmt.Fprintf(file, "  model: %s\n", model)
	}
	fmt.Fprintf(file, "\n")

	if len(tools) > 0 {
		fmt.Fprintf(file, "plugin:\n")
		fmt.Fprintf(file, "  toolsets:\n")
		for _, t := range tools {
			fmt.Fprintf(file, "    - %s\n", t)
		}
	}

	return nil
}

func verifySetup() {
	fmt.Println()
	fmt.Println("   Verifying configuration...")
	fmt.Println()

	homeDir, _ := os.UserHomeDir()
	configPath := filepath.Join(homeDir, ".go-magic", "config.yaml")

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("   ✓ Config file created")
	} else {
		fmt.Println("   ⚠ Config file not found")
	}

	// Test command availability
	fmt.Println()
	fmt.Println("   Checking dependencies...")

	commands := []string{"curl", "git"}
	if runtime.GOOS == "windows" {
		commands = []string{"curl.exe", "git.exe"}
	}

	for _, cmdName := range commands {
		path, err := exec.LookPath(cmdName)
		if err != nil {
			fmt.Printf("   ⚠ %s not found (optional)\n", cmdName)
		} else {
			fmt.Printf("   ✓ %s: %s\n", cmdName, path)
		}
	}
}
