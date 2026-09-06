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
			fmt.Println("  claude-fable-5-1  - Claude Fable 5.1 (flagship)")
			fmt.Println("  claude-opus-5     - Claude Opus 5")
			fmt.Println("  claude-sonnet-5   - Claude Sonnet 5 (balanced)")
			fmt.Println("  claude-haiku-4-5  - Claude Haiku 4.5 (fast)")
		case "deepseek":
			fmt.Println("  deepseek-v4-flash - DeepSeek V4 Flash (default, non/thinking modes)")
			fmt.Println("  deepseek-v4-pro   - DeepSeek V4 Pro (flagship reasoning)")
			fmt.Println("  Note: deepseek-chat/deepseek-reasoner deprecated 2026-07-24")
		case "minimax":
			fmt.Println("  MiniMax-M3    - MiniMax M3 (latest, coding)")
			fmt.Println("  MiniMax-M2.7  - MiniMax M2.7 (enhanced coding)")
			fmt.Println("  MiniMax-M2.5  - MiniMax M2.5 (advanced reasoning)")
		case "zhipu":
			fmt.Println("  glm-5.3        - GLM-5.3 (latest, agentic coding)")
			fmt.Println("  glm-5.3-flash - GLM-5.3 Flash (native multimodal)")
			fmt.Println("  glm-5.2        - GLM-5.2 (1M context)")
		case "ollama":
			fmt.Println("  qwen3.8      - Qwen 3.8")
			fmt.Println("  gpt-oss      - GPT-OSS (OpenAI open-weight)")
			fmt.Println("  deepseek-r1  - DeepSeek R1")
			fmt.Println("  gemma4       - Gemma 4")
			fmt.Println("  (models depend on your local Ollama installation)")
		case "openrouter":
			fmt.Println("  openai/gpt-5.6                  - GPT-5.6 Sol")
			fmt.Println("  anthropic/claude-sonnet-5       - Claude Sonnet 5")
			fmt.Println("  google/gemini-3.8-flash         - Gemini 3.8 Flash")
			fmt.Println("  deepseek/deepseek-v4-pro        - DeepSeek V4 Pro")
			fmt.Println("  (see https://openrouter.ai/models for full list)")
		case "dashscope":
			fmt.Println("  qwen3.8-max       - Qwen 3.8 Max (multimodal flagship)")
			fmt.Println("  qwen3.7-plus      - Qwen 3.7 Plus")
			fmt.Println("  qwen3.7-flash     - Qwen 3.7 Flash")
			fmt.Println("  qwen3.5-omni-plus - Qwen 3.5 Omni Plus (multimodal)")
			fmt.Println("  qwen-long         - Qwen Long (1M context)")
		case "vllm":
			fmt.Println("  (depends on your vLLM server configuration)")
		case "gemini":
			fmt.Println("  gemini-3.8-flash    - Gemini 3.8 Flash (latest stable)")
			fmt.Println("  gemini-3.7-flash    - Gemini 3.7 Flash")
			fmt.Println("  gemini-3.6-flash    - Gemini 3.6 Flash")
			fmt.Println("  gemini-3.1-pro      - Gemini 3.1 Pro (strongest reasoning)")
			fmt.Println("  gemini-2.5-pro      - Gemini 2.5 Pro")
			fmt.Println("  gemini-2.5-flash    - Gemini 2.5 Flash")
		case "groq":
			fmt.Println("  llama-3.3-70b-versatile   - Llama 3.3 70B")
			fmt.Println("  openai/gpt-oss-120b       - GPT-OSS 120B")
			fmt.Println("  openai/gpt-oss-20b        - GPT-OSS 20B")
			fmt.Println("  llama-3.1-8b-instant      - Llama 3.1 8B (fastest)")
		case "together":
			fmt.Println("  deepseek-ai/DeepSeek-V4-Pro             - DeepSeek V4 Pro")
			fmt.Println("  deepseek-ai/DeepSeek-V4-Flash           - DeepSeek V4 Flash")
			fmt.Println("  meta-llama/Llama-4-Maverick-17B-128E-Instruct")
			fmt.Println("  Qwen/Qwen3.8-2.4T-A95B                  - Qwen 3.8 Max open weights")
			fmt.Println("  moonshotai/Kimi-K3                      - Kimi K3")
		case "mistral":
			fmt.Println("  mistral-large-latest    - Mistral Large 3 (flagship, 256K context)")
			fmt.Println("  mistral-medium-3-5      - Mistral Medium 3.5")
			fmt.Println("  mistral-small-2603      - Mistral Small 4 (fast)")
			fmt.Println("  magistral-medium-latest - Magistral Medium (reasoning)")
		case "cohere":
			fmt.Println("  command-a-plus-05-2026      - Command A+ (latest, MoE)")
			fmt.Println("  command-a-reasoning-08-2025 - Command A Reasoning")
			fmt.Println("  command-a-03-2025           - Command A")
			fmt.Println("  command-r7b-12-2024         - Command R7B")
		case "perplexity":
			fmt.Println("  sonar                    - Sonar (default)")
			fmt.Println("  sonar-pro                - Sonar Pro")
			fmt.Println("  sonar-reasoning-pro     - Sonar Reasoning Pro")
			fmt.Println("  sonar-deep-research      - Sonar Deep Research")
		case "huoshan":
			fmt.Println("  doubao-seed-2.1-pro      - Doubao Seed 2.1 Pro (flagship)")
			fmt.Println("  doubao-seed-2.1-turbo    - Doubao Seed 2.1 Turbo (balanced)")
			fmt.Println("  doubao-seed-2.0-lite     - Doubao Seed 2.0 Lite (omni)")
			fmt.Println("  doubao-seed-evolving     - Doubao Seed Evolving (always-latest Agent model)")
			fmt.Println("  Note: 豆包由火山引擎提供，doubao 为兼容别名，也支持火山方舟 endpoint ID (ep-xxx)")
			fmt.Println("  Try: magic model huoshan:doubao-seed-2.1-pro 或 huoshan:ep-20250105-xxxxx")
		case "wenxin":
			fmt.Println("  ernie-5.1             - ERNIE 5.1 (latest flagship)")
			fmt.Println("  ernie-5.0             - ERNIE 5.0 (native omni-modal)")
			fmt.Println("  ernie-4.5-turbo-128k  - ERNIE 4.5 Turbo 128K")
			fmt.Println("  ernie-x1.1-preview    - ERNIE X1.1 (deep reasoning)")
		case "moonshot", "kimi": // kimi 为旧配置兼容别名
			fmt.Println("  kimi-k3                  - Kimi K3 (flagship, 1M context)")
			fmt.Println("  kimi-k2.6                - Kimi K2.6 (general)")
			fmt.Println("  kimi-k2.7-code           - Kimi K2.7 Code (coding)")
			fmt.Println("  kimi-k2.7-code-highspeed - Kimi K2.7 Code (faster)")
		case "mimo":
			fmt.Println("  mimo-v2-flash  - MiMo V2 Flash (fast)")
			fmt.Println("  mimo-v2-pro    - MiMo V2 Pro (reasoning)")
			fmt.Println("  mimo-v2-omni  - MiMo V2 Omni (multimodal)")
		case "hunyuan":
			fmt.Println("  hy3               - Tencent Hy3 (latest, MoE agent model)")
			fmt.Println("  hy-2.0-think      - HY 2.0 Think (deep reasoning)")
			fmt.Println("  hy-2.0-instruct   - HY 2.0 Instruct")
			fmt.Println("  hunyuan-turbos    - Hunyuan TurboS (fast)")
		case "longcat":
			fmt.Println("  LongCat-2.0-Preview - LongCat 2.0 (flagship, agentic; Flash series retired 2026-05-29)")
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
	"openai":     {"gpt-5.6", "gpt-5.6-terra", "gpt-5.6-luna"},
	"anthropic":  {"claude-fable-5-1", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5"},
	"deepseek":   {"deepseek-v4-flash", "deepseek-v4-pro"},
	"minimax":    {"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.5"},
	"zhipu":      {"glm-5.3", "glm-5.3-flash", "glm-5.2"},
	"ollama":     {"qwen3.8", "gpt-oss", "deepseek-r1", "gemma4"}, // depends on local Ollama
	"openrouter": {"openai/gpt-5.6", "anthropic/claude-sonnet-5", "google/gemini-3.8-flash", "deepseek/deepseek-v4-pro"},
	"dashscope":  {"qwen3.8-max", "qwen3.7-plus", "qwen3.7-flash", "qwen3.5-omni-plus", "qwen-long"},
	"vllm":       {}, // depends on vLLM server config
	// New providers
	"gemini":     {"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.1-pro", "gemini-2.5-pro", "gemini-2.5-flash"},
	"groq":       {"llama-3.3-70b-versatile", "openai/gpt-oss-120b", "openai/gpt-oss-20b", "llama-3.1-8b-instant"},
	"together":   {"deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4-Flash", "meta-llama/Llama-4-Maverick-17B-128E-Instruct", "Qwen/Qwen3.8-2.4T-A95B", "moonshotai/Kimi-K3"},
	"mistral":    {"mistral-large-latest", "mistral-medium-3-5", "mistral-small-2603", "magistral-medium-latest"},
	"cohere":     {"command-a-plus-05-2026", "command-a-reasoning-08-2025", "command-a-03-2025", "command-r7b-12-2024"},
	"perplexity": {"sonar", "sonar-pro", "sonar-reasoning-pro", "sonar-deep-research"},
	"huoshan":    {"doubao-seed-2.1-pro", "doubao-seed-2.1-turbo", "doubao-seed-2.0-lite", "doubao-seed-2.0-mini", "doubao-seed-evolving"},
	"wenxin":     {"ernie-5.1", "ernie-5.0", "ernie-4.5-turbo-128k", "ernie-x1.1-preview"},
	"moonshot":   {"kimi-k3", "kimi-k2.6", "kimi-k2.7-code", "kimi-k2.7-code-highspeed"},
	"mimo":       {"mimo-v2-flash", "mimo-v2-pro", "mimo-v2-omni"},
	"hunyuan":    {"hy3", "hy-2.0-think", "hy-2.0-instruct", "hunyuan-turbos"},
	"longcat":    {"LongCat-2.0-Preview"},
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
