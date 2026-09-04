package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

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
  magic model deepseek:deepseek-chat  - Set provider and model

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
		fmt.Printf("Available models for %s:\n", listProvider)
		fmt.Println()

		switch strings.ToLower(listProvider) {
		case "openai":
			fmt.Println("  gpt-5.6        - GPT-5.6 Sol (latest flagship, 1M context)")
			fmt.Println("  gpt-5.6-terra  - GPT-5.6 Terra (balanced)")
			fmt.Println("  gpt-5.6-luna   - GPT-5.6 Luna (fastest/cheapest)")
			fmt.Println("  o3-mini        - o3 Mini (reasoning)")
			fmt.Println()
			fmt.Println("  Note: Set via: magic model openai:gpt-5.6")
		case "anthropic":
			fmt.Println("  claude-sonnet-5   - Claude Sonnet 5 (balanced)")
			fmt.Println("  claude-fable-5    - Claude Fable 5 (flagship)")
			fmt.Println("  claude-opus-5     - Claude Opus 5")
			fmt.Println("  claude-haiku-4-5  - Claude Haiku 4.5 (fast)")
		case "deepseek":
			fmt.Println("  deepseek-chat       - DeepSeek V3 (non-thinking)")
			fmt.Println("  deepseek-reasoner  - DeepSeek R1 (thinking/reasoning)")
			fmt.Println("  deepseek-v3.1      - DeepSeek V3.1")
			fmt.Println("  deepseek-coder     - DeepSeek Coder")
		case "minimax":
			fmt.Println("  MiniMax-M2    - MiniMax M2 (latest, 200K context)")
			fmt.Println("  MiniMax-M2.1  - MiniMax M2.1 (enhanced coding)")
			fmt.Println("  MiniMax-M2.5  - MiniMax M2.5 (advanced reasoning)")
		case "zhipu":
			fmt.Println("  glm-4         - GLM-4")
			fmt.Println("  glm-4-flash  - GLM-4 Flash")
			fmt.Println("  glm-4.6      - GLM-4.6 (200K context)")
			fmt.Println("  glm-4.7      - GLM-4.7 (latest, agentic coding)")
			fmt.Println("  glm-4v       - GLM-4V (multimodal)")
		case "ollama":
			fmt.Println("  llama3.3      - Llama 3.3 (latest)")
			fmt.Println("  qwen3         - Qwen 3")
			fmt.Println("  qwen2.5       - Qwen 2.5")
			fmt.Println("  codellama     - Code Llama")
			fmt.Println("  mistral       - Mistral")
			fmt.Println("  (models depend on your local Ollama installation)")
		case "openrouter":
			fmt.Println("  openai/gpt-5.6                  - GPT-5.6 Sol")
			fmt.Println("  anthropic/claude-sonnet-5       - Claude Sonnet 5")
			fmt.Println("  google/gemini-3.7-flash         - Gemini 3.7 Flash")
			fmt.Println("  deepseek/deepseek-chat          - DeepSeek V3")
			fmt.Println("  (see https://openrouter.ai/models for full list)")
		case "dashscope":
			fmt.Println("  qwen3-turbo       - Qwen 3 Turbo")
			fmt.Println("  qwen3-plus       - Qwen 3 Plus")
			fmt.Println("  qwen3-max        - Qwen 3 Max")
			fmt.Println("  qwen3-nano       - Qwen 3 Nano")
			fmt.Println("  qwq-32b          - QwQ 32B (reasoning)")
			fmt.Println("  qwen-turbo       - Qwen 2.5 Turbo (legacy)")
		case "vllm":
			fmt.Println("  (depends on your vLLM server configuration)")
		case "gemini":
			fmt.Println("  gemini-3.7-flash         - Gemini 3.7 Flash (latest stable)")
			fmt.Println("  gemini-3.6-flash         - Gemini 3.6 Flash")
			fmt.Println("  gemini-3.5-flash         - Gemini 3.5 Flash")
			fmt.Println("  gemini-3.1-pro-preview   - Gemini 3.1 Pro (preview)")
			fmt.Println("  gemini-2.5-pro           - Gemini 2.5 Pro")
			fmt.Println("  gemini-2.5-flash         - Gemini 2.5 Flash")
		case "groq":
			fmt.Println("  llama-3.3-70b-versatile   - Llama 3.3 70B (latest)")
			fmt.Println("  mixtral-8x7b-32768       - Mixtral 8x7B (fast inference)")
			fmt.Println("  llama-3.1-70b-versatile   - Llama 3.1 70B (legacy)")
			fmt.Println("  llama-3.1-8b-instant     - Llama 3.1 8B")
			fmt.Println("  gemma2-9b-it             - Gemma 2 9B")
		case "together":
			fmt.Println("  deepseek-ai/DeepSeek-V3              - DeepSeek V3")
			fmt.Println("  deepseek-ai/DeepSeek-R1              - DeepSeek R1 (reasoning)")
			fmt.Println("  meta-llama/Llama-3.3-70B-Instruct-Turbo")
			fmt.Println("  Qwen/QwQ-32B                        - QwQ 32B (reasoning)")
			fmt.Println("  mistralai/Mixtral-8x7B-Instruct-v0.1")
		case "mistral":
			fmt.Println("  mistral-large-3    - Mistral Large 3 (latest, 256K context)")
			fmt.Println("  mistral-small-3    - Mistral Small 3 (fast)")
			fmt.Println("  mistral-medium-3   - Mistral Medium 3")
			fmt.Println("  mistral-large-latest - Mistral Large (legacy)")
			fmt.Println("  open-mixtral-8x22b - Open Mixtral 8x22B")
		case "cohere":
			fmt.Println("  command-a-03-2025      - Command A (latest)")
			fmt.Println("  command-r-plus-08-2024 - Command R+ (legacy)")
			fmt.Println("  command-r7b-12-2024  - Command R7B")
			fmt.Println("  command               - Command (standard)")
		case "perplexity":
			fmt.Println("  sonar                    - Sonar (default)")
			fmt.Println("  sonar-pro                - Sonar Pro")
			fmt.Println("  sonar-reasoning-pro     - Sonar Reasoning Pro")
			fmt.Println("  sonar-deep-research      - Sonar Deep Research")
		case "huoshan":
			fmt.Println("  doubao-seed-1.6        - Doubao Seed 1.6 (multimodal, Volcengine Ark)")
			fmt.Println("  doubao-1.5-pro-32k   - Doubao 1.5 Pro 32K")
			fmt.Println("  doubao-1.5-thinking-pro - Doubao Thinking Pro")
			fmt.Println("  doubao-pro-256k      - Doubao Pro 256K context")
			fmt.Println("  doubao-pro-32k       - Doubao Pro 32K")
			fmt.Println("  doubao-lite-32k      - Doubao Lite 32K")
			fmt.Println("  Note: 豆包由火山引擎提供，doubao 为兼容别名，也支持火山方舟 endpoint ID (ep-xxx)")
			fmt.Println("  Try: magic model huoshan:doubao-seed-1.6 或 huoshan:ep-20250105-xxxxx")
		case "wenxin":
			fmt.Println("  ernie-4.0-8k-latest  - ERNIE 4.0 8K (latest)")
			fmt.Println("  ernie-4.0-turbo-8k  - ERNIE 4.0 Turbo 8K")
			fmt.Println("  ernie-x1            - ERNIE X1 (reasoning)")
			fmt.Println("  ernie-x1.1          - ERNIE X1.1 (enhanced reasoning)")
			fmt.Println("  ernie-3.5-8k        - ERNIE 3.5 8K")
		case "moonshot", "kimi": // kimi 为旧配置兼容别名
			fmt.Println("  moonshot-v1-128k - Moonshot V1 128K context")
			fmt.Println("  moonshot-v1-32k - Moonshot V1 32K context")
			fmt.Println("  moonshot-v1-8k  - Moonshot V1 8K context")
			fmt.Println("  kimi-k2-0905-preview - Kimi K2 0905 (MoE agentic model)")
		case "mimo":
			fmt.Println("  mimo-v2-flash  - MiMo V2 Flash (fast)")
			fmt.Println("  mimo-v2-pro    - MiMo V2 Pro (reasoning)")
			fmt.Println("  mimo-v2-omni  - MiMo V2 Omni (multimodal)")
		case "hunyuan":
			fmt.Println("  hunyuan-turbo         - Hunyuan Turbo")
			fmt.Println("  hunyuan-turbos-latest  - Hunyuan Turbo S (latest)")
			fmt.Println("  hunyuan-t1            - Hunyuan T1 (thinking model)")
		case "longcat":
			fmt.Println("  LongCat-Flash-Chat    - LongCat Flash Chat (general)")
			fmt.Println("  LongCat-Flash-Thinking - LongCat Flash Thinking (reasoning)")
			fmt.Println("  LongCat-Flash-Lite    - LongCat Flash Lite (lightweight)")
		default:
			fmt.Printf("  Unknown provider: %s\n", listProvider)
			fmt.Println("  Try: openai, anthropic, deepseek, ollama, zhipu, openrouter, gemini, groq, mistral, cohere, perplexity, huoshan, wenxin, moonshot")
		}
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

// Provider models (hardcoded for now)
var providerModels = map[string][]string{
	"openai":     {"gpt-5.6", "gpt-5.6-terra", "gpt-5.6-luna", "o3-mini"},
	"anthropic":  {"claude-sonnet-5", "claude-fable-5", "claude-opus-5", "claude-haiku-4-5"},
	"deepseek":   {"deepseek-chat", "deepseek-reasoner", "deepseek-v3.1", "deepseek-coder"},
	"minimax":    {"MiniMax-M2", "MiniMax-M2.1", "MiniMax-M2.5"},
	"zhipu":      {"glm-4-flash", "glm-4.6", "glm-4.7", "glm-4v"},
	"ollama":     {"llama3.3", "qwen3", "qwen2.5", "codellama", "mistral"}, // depends on local Ollama
	"openrouter": {"openai/gpt-5.6", "anthropic/claude-sonnet-5", "google/gemini-3.7-flash", "deepseek/deepseek-chat"},
	"dashscope":  {"qwen3-turbo", "qwen3-plus", "qwen3-max", "qwen3-nano", "qwq-32b", "qwen-turbo"},
	"vllm":       {}, // depends on vLLM server config
	// New providers
	"gemini":     {"gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash", "gemini-3.5-flash-lite", "gemini-3.1-pro-preview", "gemini-2.5-pro", "gemini-2.5-flash"},
	"groq":       {"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "gemma2-9b-it"},
	"together":   {"deepseek-ai/DeepSeek-V3", "deepseek-ai/DeepSeek-R1", "meta-llama/Llama-3.3-70B-Instruct-Turbo", "Qwen/QwQ-32B", "mistralai/Mixtral-8x7B-Instruct-v0.1"},
	"mistral":    {"mistral-large-3", "mistral-small-3", "mistral-medium-3", "mistral-large-latest", "open-mixtral-8x22b"},
	"cohere":     {"command-a-03-2025", "command-r-plus-08-2024", "command-r7b-12-2024", "command"},
	"perplexity": {"sonar", "sonar-pro", "sonar-reasoning-pro", "sonar-deep-research"},
	"huoshan":    {"doubao-seed-1.6", "doubao-1.5-pro-32k", "doubao-1.5-thinking-pro", "doubao-pro-256k", "doubao-pro-32k", "doubao-lite-32k"},
	"wenxin":     {"ernie-4.0-8k-latest", "ernie-4.0-turbo-8k", "ernie-x1", "ernie-x1.1", "ernie-3.5-8k"},
	"moonshot":   {"kimi-k2-0905-preview", "kimi-k2-turbo-preview", "moonshot-v1-128k"},
	"mimo":       {"mimo-v2-flash", "mimo-v2-pro", "mimo-v2-omni"},
	"hunyuan":    {"hunyuan-turbo", "hunyuan-turbos-latest", "hunyuan-t1"},
	"longcat":    {"LongCat-Flash-Chat", "LongCat-Flash-Thinking", "LongCat-Flash-Lite"},
	"meta":       {"muse-spark-1.3", "muse-spark-1.2", "muse-spark-1.1"},
}

// interactiveModelSelect presents an interactive UI for selecting provider and model.
func interactiveModelSelect(cfg *config.Config) (string, string) {
	providers := []string{"openai", "anthropic", "deepseek", "minimax", "zhipu", "ollama", "openrouter", "dashscope", "vllm", "gemini", "groq", "together", "mistral", "cohere", "perplexity", "huoshan", "wenxin", "moonshot", "mimo", "hunyuan", "longcat", "meta"}
	providerNames := []string{"OpenAI", "Anthropic", "DeepSeek", "MiniMax", "ZhiPu (GLM)", "Ollama (local)", "OpenRouter", "DashScope (Ali)", "vLLM (local)", "Gemini (Google)", "Groq (Fast)", "Together AI", "Mistral AI", "Cohere", "Perplexity", "Volcengine (Doubao)", "Wenxin (Baidu)", "Moonshot (Kimi)", "MiMo (Xiaomi)", "Hunyuan (Tencent)", "LongCat (Meituan)", "Meta (Muse Spark)"}

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
	models, ok := providerModels[selectedProvider]
	if !ok || len(models) == 0 {
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
