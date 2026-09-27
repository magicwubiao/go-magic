package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/pkg/catalog"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var modelCmd = &cobra.Command{
	Use:   "model [provider:model]",
	Short: "Choose LLM provider and model",
	Long: `Choose or view the LLM provider and model to use.

Supported providers: openai, anthropic, deepseek, minimax, ollama, dashscope, vllm, zhipu, openrouter, gemini, groq, together, mistral, cohere, perplexity, huoshan, wenxin, moonshot, mimo, hunyuan, longcat, meta.

Formats:
  magic model                  - View current provider and model
  magic model gpt-5.6        - Set model for current provider
  magic model deepseek:deepseek-v4-flash  - Set provider and model

Flags:
  -l, --list <provider>  - List available models for a provider

Examples:
  magic model
  magic model gpt-5.6
  magic model huoshan:ep-20250105-xxxxx
  magic model --list openai`,
	Args: cobra.MaximumNArgs(1),
	Run:  runModel,
}

func init() {
	modelCmd.Flags().StringP("list", "l", "", "List available models for a provider")
	rootCmd.AddCommand(modelCmd)
}

func runModel(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	listProvider, _ := cmd.Flags().GetString("list")
	if listProvider != "" {
		// 目录数据派生自 pkg/catalog（唯一目录源），不再维护本文件副本。
		name := strings.ToLower(strings.TrimSpace(listProvider))
		prov, ok := catalog.Find(name)
		if !ok {
			fmt.Printf("  Unknown provider: %s\n", listProvider)
			fmt.Println("  Try: deepseek, openai, anthropic, dashscope, moonshot(kimi), zhipu, huoshan(doubao), ...")
			return
		}
		fmt.Printf("Available models for %s:\n", prov.DisplayName)
		fmt.Println()
		for _, m := range prov.Models {
			line := "  " + m.ID
			if m.Name != "" && m.Name != m.ID && m.Description != "" {
				line += " - " + m.Name + " (" + m.Description + ")"
			} else if m.Description != "" {
				line += " - " + m.Description
			}
			fmt.Println(line)
		}
		if prov.Note != "" {
			fmt.Println("  Note:", prov.Note)
		}
		fmt.Println()
		fmt.Printf("  Set via: magic model %s:<model>\n", prov.Name)
		return
	}

	if len(args) == 0 {
		// Show current model
		fmt.Printf("Current provider: %s\n", cfg.Provider)
		fmt.Printf("Current model: %s\n", cfg.Model)
		return
	}

	// Set new model
	model := args[0]

	// Parse provider:model format
	parts := strings.Split(model, ":")
	if len(parts) == 2 {
		providerName := parts[0]
		model = parts[1]

		cfg.Provider = providerName
		cfg.Model = model

		// Update provider config - set model as first element of Models array
		if provCfg, ok := cfg.Providers[providerName]; ok {
			if len(provCfg.Models) == 0 {
				provCfg.Models = []string{model}
			} else {
				provCfg.Models[0] = model
			}
			cfg.Providers[providerName] = provCfg
		}

		err = cfg.Save()
		if err != nil {
			fmt.Printf("Failed to save config: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Switched to provider: %s, model: %s\n", providerName, model)
	} else {
		// Just update model for current provider
		cfg.Model = model

		// Update provider config - set model as first element of Models array
		if provCfg, ok := cfg.Providers[cfg.Provider]; ok {
			if len(provCfg.Models) == 0 {
				provCfg.Models = []string{model}
			} else {
				provCfg.Models[0] = model
			}
			cfg.Providers[cfg.Provider] = provCfg
		}

		err = cfg.Save()
		if err != nil {
			fmt.Printf("Failed to save config: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Model switched to: %s\n", model)
	}
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// Provider models derive from pkg/catalog（唯一目录源）— interactiveModelSelect
// 直接查 catalog.ModelIDs，不再维护本文件副本。

// interactiveModelSelect 之后的模型选择使用 catalog.ModelIDs(selectedProvider)。

// interactiveModelSelect presents an interactive UI for selecting provider and model.
func interactiveModelSelect(cfg *config.Config) (string, string) {
	// 目录数据派生自 pkg/catalog（唯一目录源），custom 由用户手填不进列表。
	var providers, providerNames []string
	for _, p := range catalog.All() {
		if p.Group == "custom" {
			continue
		}
		providers = append(providers, p.Name)
		providerNames = append(providerNames, p.DisplayName)
	}

	fmt.Println("\n=== Interactive Model Selection ===")
	fmt.Println("Use arrow keys (up/down) to navigate, Enter to select, q to quit.")
	fmt.Println()

	// Arrow-key navigation using term
	reader := bufio.NewReader(os.Stdin)
	selected := 0
	maxSelected := len(providers)

	for {
		// Clear line and print menu
		fmt.Print("\r\033[K")
		fmt.Println("Select Provider:")
		for i, name := range providerNames {
			prefix := "  "
			cursor := "  "
			if i == selected {
				prefix = "> "
				cursor = "←"
			}
			fmt.Printf("%s%s%d. %s %s\n", prefix, cursor, i+1, name, strings.Repeat(" ", 20-len(name)))
		}
		fmt.Println()
		fmt.Println("↑/↓: Navigate  |  Enter: Select  |  q: Quit")

		// Read a single key press
		char, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}

		// Handle escape sequences (arrow keys)
		if len(char) > 0 && char[0] == '\r' || char[0] == '\n' {
			// Enter pressed - select current
			break
		}

		// Check for escape sequence
		if len(char) >= 3 && char[0] == 27 && char[1] == '[' {
			switch char[2] {
			case 'A': // Up arrow
				selected--
				if selected < 0 {
					selected = 0
				}
			case 'B': // Down arrow
				selected++
				if selected >= maxSelected {
					selected = maxSelected - 1
				}
			}
		}

		// Check for 'q' to quit
		if len(char) > 0 && (char[0] == 'q' || char[0] == 'Q') {
			fmt.Println("\nCancelled.")
			return cfg.Provider, cfg.Model
		}
	}

	selectedProvider := providers[selected]
	fmt.Printf("\nSelected provider: %s\n", selectedProvider)

	// Show models for this provider
	models := catalog.ModelIDs(selectedProvider)
	if len(models) == 0 {
		fmt.Printf("No predefined models for %s. Please set model manually.\n", selectedProvider)
		return selectedProvider, cfg.Model
	}

	// Model selection
	fmt.Println("\nSelect Model:")
	modelSelected := 0
	maxModelSelected := len(models)

	for {
		// Clear and print model menu
		fmt.Print("\r\033[K")
		fmt.Printf("Models for %s:\n", selectedProvider)
		for i, m := range models {
			prefix := "  "
			cursor := "  "
			if i == modelSelected {
				prefix = "> "
				cursor = "←"
			}
			fmt.Printf("%s%s%d. %s %s\n", prefix, cursor, i+1, m, strings.Repeat(" ", 30-len(m)))
		}
		fmt.Println()
		fmt.Println("0. Keep current model  |  ↑/↓: Navigate  |  Enter: Select")

		// Read a single key press
		char, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}

		// Handle enter
		if len(char) > 0 && (char[0] == '\r' || char[0] == '\n') {
			break
		}

		// Check for escape sequence
		if len(char) >= 3 && char[0] == 27 && char[1] == '[' {
			switch char[2] {
			case 'A': // Up arrow
				modelSelected--
				if modelSelected < 0 {
					modelSelected = 0
				}
			case 'B': // Down arrow
				modelSelected++
				if modelSelected >= maxModelSelected {
					modelSelected = maxModelSelected - 1
				}
			}
		}
	}

	if modelSelected == 0 {
		fmt.Println("Keeping current model.")
		return selectedProvider, cfg.Model
	}

	selectedModel := models[modelSelected-1]
	fmt.Printf("\nSelected: provider=%s, model=%s\n", selectedProvider, selectedModel)
	return selectedProvider, selectedModel
}
