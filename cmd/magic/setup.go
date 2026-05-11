package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run the full setup wizard",
	Long:  "Interactive setup wizard that configures everything at once",
	Run:   runSetup,
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

// selectItem displays options and allows selection via number or custom input
// items: list of options
// defaultIdx: default selected index (0-based)
// allowCustom: whether to allow custom input
// title: display title for the selection
func selectItem(reader *bufio.Reader, items []string, defaultIdx int, allowCustom bool, title string) (string, error) {
	if len(items) == 0 {
		// No predefined items, just read input
		input, _ := reader.ReadString('\n')
		return strings.TrimSpace(input), nil
	}

	// Adjust default index if out of bounds
	if defaultIdx < 0 || defaultIdx >= len(items) {
		defaultIdx = 0
	}

	selected := defaultIdx

	for {
		// Clear lines and show selection
		fmt.Println()
		fmt.Printf("%s:\n", title)
		for i, item := range items {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			fmt.Printf("  %s[%d] %s\n", prefix, i+1, item)
		}

		if allowCustom {
			fmt.Println("  [Other] Custom input")
		}

		// Show prompt on the same line
		prompt := fmt.Sprintf("Select (1-%d, default %d, ↑↓ navigate, Enter confirm): ", len(items), defaultIdx+1)
		fmt.Print(prompt)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		// Empty input means confirm current selection
		if input == "" {
			return items[selected], nil
		}

		// Try to parse as number
		if num, err := strconv.Atoi(input); err == nil {
			if num >= 1 && num <= len(items) {
				return items[num-1], nil
			}
			fmt.Printf("  Invalid choice. Please enter 1-%d.\n", len(items))
			continue
		}

		// Check for "Other" or custom input
		if allowCustom {
			if strings.ToLower(input) == "other" || strings.ToLower(input) == "custom" {
				fmt.Println()
				fmt.Print("  Enter custom value: ")
				custom, _ := reader.ReadString('\n')
				return strings.TrimSpace(custom), nil
			}
			// Treat as custom input
			return input, nil
		}

		fmt.Printf("  Invalid input. Please enter 1-%d or Enter to confirm default.\n", len(items))
	}
}

// selectProvider displays provider options and allows selection via number
func selectProvider(reader *bufio.Reader, currentProvider string, providerToNum map[string]string) (string, error) {
	providers := []string{
		"deepseek", "anthropic", "openai", "kimi",
		"zhipu", "huoshan", "minimax", "dashscope",
		"ollama", "vllm", "openrouter",
		"gemini", "groq", "together", "mistral",
		"cohere", "perplexity", "doubao", "wenxin",
		"moonshot", "mimo", "hunyuan", "custom",
	}

	providerNames := []string{
		"DeepSeek",
		"Anthropic (Claude)",
		"OpenAI (GPT-4, GPT-3.5)",
		"Kimi (Moonshot)",
		"Zhipu (GLM)",
		"Huoshan (Volcano Engine)",
		"MiniMax",
		"Dashscope (Qwen)",
		"Ollama (Local)",
		"vLLM (Local)",
		"OpenRouter",
		"Google Gemini",
		"Groq",
		"Together AI",
		"Mistral",
		"Cohere",
		"Perplexity",
		"Doubao (ByteDance)",
		"Wenxin (Baidu)",
		"Moonshot",
		"MiMo (Xiaomi)",
		"Hunyuan (Tencent)",
		"Other (custom)",
	}

	// Find current selection
	currentIdx := 0
	if numStr, ok := providerToNum[currentProvider]; ok {
		if num, err := strconv.Atoi(numStr); err == nil && num >= 1 && num <= len(providers) {
			currentIdx = num - 1
		}
	}

	for {
		fmt.Println()
		fmt.Println("LLM Provider:")
		for i, name := range providerNames {
			prefix := "  "
			if i == currentIdx {
				prefix = "> "
			}
			fmt.Printf("  %s[%d] %s\n", prefix, i+1, name)
		}

		prompt := fmt.Sprintf("Select (1-%d, default %d, ↑↓ navigate, Enter confirm): ", len(providers), currentIdx+1)
		fmt.Print(prompt)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			return providers[currentIdx], nil
		}

		if num, err := strconv.Atoi(input); err == nil {
			if num >= 1 && num <= len(providers) {
				return providers[num-1], nil
			}
			fmt.Printf("Invalid choice. Please enter 1-%d.\n", len(providers))
			continue
		}

		fmt.Printf("Invalid input. Please enter 1-%d or Enter to confirm default.\n", len(providers))
	}
}

// readInput reads a line from stdin, trims it, and returns the result.
// If the input is empty, it returns the default value.
func readInput(reader *bufio.Reader, defaultValue string) string {
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

func runSetup(cmd *cobra.Command, args []string) {
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║       magic Agent Setup Wizard         ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Println()

	homeDir, _ := os.UserHomeDir()
	magicDir := filepath.Join(homeDir, ".magic")

	// Load existing config if any
	cfg, loadErr := config.Load()
	if loadErr != nil || cfg == nil {
		cfg = &config.Config{
			Profile:    "default",
			MagicHome:  magicDir,
			WorkingDir: "",
			Providers:  make(map[string]config.ProviderConfig),
			Tools: config.ToolsConfig{
				Enabled: []string{"all"},
			},
			Gateway: config.GatewayConfig{
				Enabled:   false,
				Platforms: make(map[string]config.PlatformConfig),
			},
		}
	}
	// Ensure maps are initialized
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	if cfg.Gateway.Platforms == nil {
		cfg.Gateway.Platforms = make(map[string]config.PlatformConfig)
	}

	// Determine current provider and its config
	provider := cfg.Provider
	if provider == "" {
		provider = "deepseek"
	}

	provCfg, hasProv := cfg.Providers[provider]
	if !hasProv {
		provCfg = config.ProviderConfig{}
	}

	reader := bufio.NewReader(os.Stdin)

	// Provider default values lookup
	providerDefaults := map[string]struct {
		name    string
		model   string
		baseURL string
	}{
		"deepseek":   {"DeepSeek", "deepseek-chat", "https://api.deepseek.com/v1"},
		"anthropic":  {"Anthropic (Claude)", "claude-3-5-sonnet-20241022", "https://api.anthropic.com/v1"},
		"openai":     {"OpenAI (GPT-4, GPT-3.5)", "gpt-4o", "https://api.openai.com/v1"},
		"kimi":       {"Kimi (Moonshot)", "moonshot-v1-8k", "https://api.moonshot.cn/v1"},
		"zhipu":      {"Zhipu (GLM)", "glm-4", "https://open.bigmodel.cn/api/paas/v4"},
		"huoshan":    {"Huoshan (Volcano Engine)", "ep-20250105-xxxxx", "https://ark.cn-beijing.volces.com/api/v3"},
		"minimax":    {"MiniMax", "MiniMax-M2", "https://api.minimax.chat/v1"},
		"dashscope":  {"Dashscope (Qwen)", "qwen-turbo", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		"ollama":     {"Ollama (Local)", "llama3.2", "http://localhost:11434/v1"},
		"vllm":       {"vLLM (Local)", "", "http://localhost:8000/v1"},
		"openrouter": {"OpenRouter", "openrouter/anthropic/claude-3.5-sonnet", "https://openrouter.ai/api/v1"},
		"gemini":     {"Google Gemini", "gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com/v1beta/openai"},
		"groq":       {"Groq", "llama-3.3-70b-versatile", "https://api.groq.com/openai/v1"},
		"together":   {"Together AI", "deepseek-ai/DeepSeek-V3", "https://api.together.xyz/v1"},
		"mistral":    {"Mistral", "mistral-large-latest", "https://api.mistral.ai/v1"},
		"cohere":     {"Cohere", "command-r-plus-08-2024", "https://api.cohere.ai/v1"},
		"perplexity": {"Perplexity", "sonar", "https://api.perplexity.ai"},
		"doubao":     {"Doubao (ByteDance)", "doubao-pro-32k", "https://ark.cn-beijing.volces.com/api/v3"},
		"wenxin":     {"Wenxin (Baidu)", "ernie-4.0-8k-latest", "https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop"},
		"moonshot":   {"Moonshot", "moonshot-v1-128k", "https://api.moonshot.cn/v1"},
		"mimo":       {"MiMo (Xiaomi)", "mimo-v2-pro", "https://api.xiaomimimo.com/v1"},
		"hunyuan":    {"Hunyuan (Tencent)", "hunyuan-turbo", "https://api.hunyuan.cloud.tencent.com/v1"},
		"custom":     {"Other (custom)", "", ""},
	}

	// Map provider to selection number
	providerToNum := map[string]string{
		"deepseek": "1", "anthropic": "2", "openai": "3", "kimi": "4",
		"zhipu": "5", "huoshan": "6", "minimax": "7", "dashscope": "8",
		"ollama": "9", "vllm": "10", "openrouter": "11",
		"gemini": "12", "groq": "13", "together": "14", "mistral": "15",
		"cohere": "16", "perplexity": "17", "doubao": "18", "wenxin": "19",
		"moonshot": "20", "mimo": "21", "hunyuan": "22", "custom": "23",
	}

	// Provider selection
	// fmt.Printf("1. Choose your LLM provider (current: %s):\n", currentProviderName)
	newProvider, _ := selectProvider(reader, provider, providerToNum)

	// Resolve provider, model, baseURL from selection
	model := provCfg.Model
	baseURL := provCfg.BaseURL

	// If provider changed, use defaults for the new provider
	if newProvider != provider {
		if p, ok := providerDefaults[newProvider]; ok {
			model = p.model
			baseURL = p.baseURL
		}
		// Check if new provider already has saved config
		if existingProv, ok := cfg.Providers[newProvider]; ok {
			if existingProv.Model != "" {
				model = existingProv.Model
			}
			if existingProv.BaseURL != "" {
				baseURL = existingProv.BaseURL
			}
		}
		provider = newProvider
	}

	cfg.Provider = provider

	// API Key - show masked existing key if present
	apiKeyDisplay := ""
	currentProvCfg := cfg.Providers[provider]
	if currentProvCfg.APIKey != "" {
		// Show last 4 chars only
		if len(currentProvCfg.APIKey) > 4 {
			apiKeyDisplay = " (current: ..." + currentProvCfg.APIKey[len(currentProvCfg.APIKey)-4:] + ")"
		} else {
			apiKeyDisplay = " (current: ****)"
		}
	}
	fmt.Println()
	fmt.Printf("API Key for %s%s:\n", provider, apiKeyDisplay)
	fmt.Print("> ")
	apiKey := readInput(reader, currentProvCfg.APIKey)

	if apiKey == "" && provider != "ollama" && provider != "custom" && provider != "vllm" {
		fmt.Println("\n   Warning: API key is empty. You may need to configure it later.")
		fmt.Println("   Press Enter to continue or Ctrl+C to cancel...")
		reader.ReadString('\n')
	}

	// Custom provider additional fields
	if provider == "custom" {
		fmt.Println()
		fmt.Printf("API Base URL (current: %s):\n", baseURL)
		fmt.Print("> ")
		baseURL = readInput(reader, baseURL)

		fmt.Println()
		fmt.Println("Model name:")
		fmt.Print("> ")
		model = readInput(reader, model)
	}

	// Model selection (skip for custom, already collected above)
	if provider != "custom" {
	modelTitle := fmt.Sprintf("Model for %s (current: %s)", provider, model)
	// Find default index for current model
	defaultIdx := 0
	models := providerModels[provider]
	for i, m := range models {
		if m == model {
			defaultIdx = i
			break
		}
	}
	// For providers with no predefined models (ollama, vllm, custom)
	allowCustom := len(models) == 0
	if !allowCustom {
		// Add "Other" option
		allowCustom = true
	}

	selectedModel, _ := selectItem(reader, models, defaultIdx, allowCustom, modelTitle)
	model = selectedModel
	} // end if provider != "custom"

	// Build provider config
	provCfg = config.ProviderConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}
	cfg.Providers[provider] = provCfg
	cfg.Model = model

	// Profile name
	profileLabel := "default"
	if cfg.Profile != "" {
		profileLabel = cfg.Profile
	}
	fmt.Println()
	fmt.Printf("Profile name (current: %s):\n", profileLabel)
	fmt.Print("> ")
	profile := readInput(reader, cfg.Profile)
	cfg.Profile = profile

	// Toolset selection
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────┐")
	fmt.Println("│           Toolset Configuration             │")
	fmt.Println("└─────────────────────────────────────────────┘")
	runToolsetSetup(cfg, reader)

	// Verify configuration
	cortexDefault := "N"
	if cfg.CortexEnabled {
		cortexDefault = "Y"
	}
	fmt.Println()
	fmt.Printf("Enable Cortex AI enhancement? (y/N, default %s): ", cortexDefault)
	cortexChoice := readInput(reader, cortexDefault)
	cfg.CortexEnabled = !(cortexChoice == "n" || cortexChoice == "N")

	// Gateway
	gatewayDefault := "N"
	if cfg.Gateway.Enabled {
		gatewayDefault = "y"
	}
	fmt.Printf("\nEnable messaging gateway? (y/N, default %s): ", gatewayDefault)
	gatewayChoice := readInput(reader, gatewayDefault)
	if gatewayChoice == "y" || gatewayChoice == "Y" {
		cfg.Gateway.Enabled = true
		fmt.Println("   Gateway can be configured later with: magic gateway start")
		// Run the platform setup wizard
		runGatewayPlatformSetup(cfg, reader, magicDir)
	} else {
		cfg.Gateway.Enabled = false
	}

	// Save config
	err := cfg.Save()
	if err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ Setup complete!")
	fmt.Println("Configuration saved to ~/.magic/config.json")
	fmt.Println()
	fmt.Println("You can now start chatting with:")
	fmt.Println("  magic chat")

	// Verify configuration by testing API connection
	verifyConfig(cfg)
}

// runToolsetSetup runs the interactive toolset configuration wizard
func runToolsetSetup(cfg *config.Config, reader *bufio.Reader) {
	// Get all available toolsets
	ts := tool.NewManager()
	toolsets := ts.List()

	if len(toolsets) == 0 {
		fmt.Println("   No toolsets available.")
		return
	}

	// Get current enabled toolsets
	currentTools := cfg.Tools.Enabled
	currentMap := make(map[string]bool)
	for _, t := range currentTools {
		if t == "all" {
			currentMap["all"] = true
			break
		}
		currentMap[t] = true
	}

	// Check if "all" is currently enabled
	isAllEnabled := currentMap["all"]

	fmt.Println("   Select toolsets to enable (comma-separated numbers, or 'all'):")
	fmt.Println()

	// Build display list with numbers
	numberedToolsets := make([]string, 0, len(toolsets))
	toolsetNames := make([]string, 0, len(toolsets))
	for i, ts := range toolsets {
		num := strconv.Itoa(i + 1)
		numberedToolsets = append(numberedToolsets, num)
		toolsetNames = append(toolsetNames, ts.Name)

		// Check if selected
		selected := ""
		if isAllEnabled || currentMap[ts.Name] {
			selected = " [enabled]"
		}

		// Truncate long descriptions
		desc := ts.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}

		fmt.Printf("      [%d] %s - %s%s\n", i+1, ts.Name, desc, selected)
	}

	fmt.Println()
	fmt.Print("   Enter numbers (e.g. 1,3,5) or 'all': ")

	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(selection)

	if selection == "" {
		// Keep current selection
		return
	}

	if strings.ToLower(selection) == "all" {
		cfg.Tools.Enabled = []string{"all"}
		fmt.Println("   ✓ All toolsets enabled")
		return
	}

	// Parse comma-separated numbers
	selectedTools := []string{}
	parts := strings.Split(selection, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if num, err := strconv.Atoi(part); err == nil {
			idx := num - 1
			if idx >= 0 && idx < len(toolsetNames) {
				selectedTools = append(selectedTools, toolsetNames[idx])
			}
		}
	}

	if len(selectedTools) > 0 {
		cfg.Tools.Enabled = selectedTools
		fmt.Printf("   ✓ %d toolsets enabled: %s\n", len(selectedTools), strings.Join(selectedTools, ", "))
	}
}

// verifyConfig tests the API configuration by making a simple request
func verifyConfig(cfg *config.Config) {
	if cfg.Provider == "" || cfg.Model == "" {
		fmt.Println("\n⚠ Skipping verification: Provider or model not configured")
		return
	}

	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok || provCfg.APIKey == "" {
		if cfg.Provider != "ollama" && cfg.Provider != "vllm" && cfg.Provider != "custom" {
			fmt.Println("\n⚠ Skipping verification: API key not configured")
			return
		}
	}

	fmt.Println("\n┌─────────────────────────────────────────────┐")
	fmt.Println("│         Configuration Verification           │")
	fmt.Println("└─────────────────────────────────────────────┘")

	// Build request
	var reqBody map[string]interface{}
	var apiURL string
	var headers map[string]string

	switch cfg.Provider {
	case "openai", "azure":
		apiURL = provCfg.BaseURL + "/chat/completions"
		reqBody = map[string]interface{}{
			"model": cfg.Model,
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
			"max_tokens": 10,
		}
		headers = map[string]string{
			"Authorization": "Bearer " + provCfg.APIKey,
		}
	case "anthropic":
		apiURL = "https://api.anthropic.com/v1/messages"
		reqBody = map[string]interface{}{
			"model": cfg.Model,
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
			"max_tokens": 10,
		}
		headers = map[string]string{
			"x-api-key":             provCfg.APIKey,
			"anthropic-version":     "2023-06-01",
			"anthropic-dangerous-direct-https-access-allowed": "true",
		}
	default:
		// Generic OpenAI-compatible
		apiURL = provCfg.BaseURL + "/chat/completions"
		reqBody = map[string]interface{}{
			"model": cfg.Model,
			"messages": []map[string]string{
				{"role": "user", "content": "Hi"},
			},
			"max_tokens": 10,
		}
		headers = map[string]string{
			"Authorization": "Bearer " + provCfg.APIKey,
		}
	}

	// Create request
	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		fmt.Printf("   ✗ Request creation failed: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Send request with timeout
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("   ✗ Connection failed: %v\n", err)
		fmt.Println("   This may be normal if the API requires additional configuration.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Println("   ✓ API connection successful!")
		fmt.Printf("   Provider: %s, Model: %s\n", cfg.Provider, cfg.Model)
	} else {
		fmt.Printf("   ✗ API returned status %d\n", resp.StatusCode)
		if resp.StatusCode == 401 {
			fmt.Println("   Please check your API key.")
		} else if resp.StatusCode == 403 {
			fmt.Println("   Access forbidden. Please check your permissions.")
		} else if resp.StatusCode == 429 {
			fmt.Println("   Rate limited. Try again later.")
		}
	}
}

// runGatewayPlatformSetup runs the interactive gateway platform configuration wizard
func runGatewayPlatformSetup(cfg *config.Config, reader *bufio.Reader, magicDir string) {
	fmt.Println("\n   Gateway Platform Configuration:")
	fmt.Println("   Configure messaging platforms (Telegram, Discord, etc.)")

	for {
		fmt.Println("\n   Available platforms:")
		fmt.Println("      [1] Telegram")
		fmt.Println("      [2] Discord")
		fmt.Println("      [3] WeCom (Enterprise WeChat)")
		fmt.Println("      [4] Feishu (Lark)")
		fmt.Println("      [5] DingTalk")
		fmt.Println("      [6] QQ (Bot/Channel)")
		fmt.Println("      [7] WeChat (Official Account/Mini Program)")
		fmt.Println("      [8] WeChat-iLink (Personal WeChat via iLink)")
		fmt.Println("      [9] Slack")
		fmt.Println("      [10] WhatsApp")
		fmt.Println("      [11] LINE")
		fmt.Println("      [12] Matrix")
		fmt.Println("      [0] Done")

		fmt.Print("\n   Select platform to configure (0-12, default 0): ")
		platformChoice, _ := reader.ReadString('\n')
		platformChoice = strings.TrimSpace(platformChoice)
		if platformChoice == "" {
			platformChoice = "0"
		}

		switch platformChoice {
		case "0":
			fmt.Println("   Platform configuration complete.")
			return
		case "1": // Telegram
			currentToken := ""
			if p, ok := cfg.Gateway.Platforms["telegram"]; ok {
				currentToken = p.Token
			}
			tokenDisplay := ""
			if currentToken != "" {
				if len(currentToken) > 4 {
					tokenDisplay = " (current: ..." + currentToken[len(currentToken)-4:] + ")"
				} else {
					tokenDisplay = " (current: ****)"
				}
			}
			fmt.Printf("   Enter Telegram bot token%s: ", tokenDisplay)
			token := readInput(reader, currentToken)
			if token != "" {
				cfg.Gateway.Platforms["telegram"] = config.PlatformConfig{
					Token:   token,
					Enabled: true,
				}
				fmt.Println("   [Telegram] configured successfully!")
			}
		case "2": // Discord
			currentToken := ""
			if p, ok := cfg.Gateway.Platforms["discord"]; ok {
				currentToken = p.Token
			}
			tokenDisplay := ""
			if currentToken != "" {
				if len(currentToken) > 4 {
					tokenDisplay = " (current: ..." + currentToken[len(currentToken)-4:] + ")"
				} else {
					tokenDisplay = " (current: ****)"
				}
			}
			fmt.Printf("   Enter Discord bot token%s: ", tokenDisplay)
			token := readInput(reader, currentToken)
			if token != "" {
				cfg.Gateway.Platforms["discord"] = config.PlatformConfig{
					Token:   token,
					Enabled: true,
				}
				fmt.Println("   [Discord] configured successfully!")
			}
		case "3": // WeCom
			current := cfg.Gateway.Platforms["wecom"]
			fmt.Println("   WeCom connection mode:")
			fmt.Println("      [1] QR Code Login (recommended - scan to login)")
			fmt.Println("      [2] API Mode (corp_id + secret)")
			fmt.Printf("   Select mode (1-2, default 1): ")
			modeChoice := readInput(reader, "1")

			if modeChoice == "2" {
				// Traditional API mode
				fmt.Printf("   Enter WeCom corp_id (current: %s): ", maskString(current.CorpID))
				corpID := readInput(reader, current.CorpID)
				fmt.Printf("   Enter WeCom agent_id (current: %s): ", current.AgentID)
				agentID := readInput(reader, current.AgentID)
				fmt.Printf("   Enter WeCom secret (current: %s): ", maskString(current.Secret))
				secret := readInput(reader, current.Secret)
				fmt.Printf("   Enter WeCom token for callback verification (current: %s): ", maskString(current.Token))
				token := readInput(reader, current.Token)
				if corpID != "" && secret != "" {
					cfg.Gateway.Platforms["wecom"] = config.PlatformConfig{
						CorpID:  corpID,
						AgentID: agentID,
						Secret:  secret,
						Token:   token,
						Mode:    "app",
						Enabled: true,
					}
					fmt.Println("   [WeCom] configured in API mode!")
				}
			} else {
				// QR Code login mode
				fmt.Printf("   Enter WeCom corp_id (current: %s): ", maskString(current.CorpID))
				corpID := readInput(reader, current.CorpID)
				fmt.Printf("   Enter WeCom agent_id (current: %s): ", current.AgentID)
				agentID := readInput(reader, current.AgentID)
				if corpID != "" && agentID != "" {
					cfg.Gateway.Platforms["wecom"] = config.PlatformConfig{
						CorpID:  corpID,
						AgentID: agentID,
						Mode:    "qr",
						Enabled: true,
					}
					fmt.Println("   [WeCom] configured in QR mode!")
					fmt.Println("   To login, start the gateway and scan the QR code:")
					fmt.Println("     magic gateway start")
				} else {
					fmt.Println("   [WeCom] corp_id and agent_id are required for QR login")
				}
			}
		case "4": // Feishu (Lark)
			current := cfg.Gateway.Platforms["feishu"]
			fmt.Printf("   Enter Feishu app_id (current: %s): ", current.AppID)
			appID := readInput(reader, current.AppID)
			fmt.Printf("   Enter Feishu app_secret (current: %s): ", maskString(current.AppSecret))
			appSecret := readInput(reader, current.AppSecret)
			if appID != "" && appSecret != "" {
				cfg.Gateway.Platforms["feishu"] = config.PlatformConfig{
					AppID:     appID,
					AppSecret: appSecret,
					Enabled:   true,
				}
				fmt.Println("   [Feishu] configured successfully!")
			}
		case "5": // DingTalk
			current := cfg.Gateway.Platforms["dingtalk"]
			fmt.Printf("   Enter DingTalk app_key (current: %s): ", current.AppKey)
			appKey := readInput(reader, current.AppKey)
			fmt.Printf("   Enter DingTalk app_secret (current: %s): ", maskString(current.AppSecret))
			appSecret := readInput(reader, current.AppSecret)
			fmt.Printf("   Enter DingTalk agent_id (current: %s): ", current.AgentID)
			agentID := readInput(reader, current.AgentID)
			if appKey != "" && appSecret != "" {
				cfg.Gateway.Platforms["dingtalk"] = config.PlatformConfig{
					AppKey:    appKey,
					AppSecret: appSecret,
					AgentID:   agentID,
					Enabled:   true,
				}
				fmt.Println("   [DingTalk] configured successfully!")
			}
		case "6": // QQ (Bot/Channel)
			current := cfg.Gateway.Platforms["qq"]
			fmt.Println("   QQ supports two auth methods:")
			fmt.Println("     [a] Official QQ Bot (app_id + app_secret) - Recommended")
			fmt.Println("     [b] Legacy QQ (number + password)")
			fmt.Printf("   Select method (default a): ")
			qqMethod := readInput(reader, "a")
			if qqMethod == "b" || qqMethod == "B" {
				// Legacy mode
				fmt.Printf("   Enter QQ number (current: %s): ", current.Number)
				number := readInput(reader, current.Number)
				fmt.Printf("   Enter QQ password (current: %s): ", maskString(current.Password))
				password := readInput(reader, current.Password)
				if number != "" && password != "" {
					cfg.Gateway.Platforms["qq"] = config.PlatformConfig{
						Number:   number,
						Password: password,
						Enabled:  true,
					}
					fmt.Println("   [QQ Legacy] configured successfully!")
				}
			} else {
				// Official QQ Bot mode (Recommended)
				fmt.Printf("   Enter QQ Bot app_id (current: %s): ", current.AppID)
				appID := readInput(reader, current.AppID)
				fmt.Printf("   Enter QQ Bot app_secret (current: %s): ", maskString(current.AppSecret))
				appSecret := readInput(reader, current.AppSecret)
				if appID != "" && appSecret != "" {
					cfg.Gateway.Platforms["qq"] = config.PlatformConfig{
						AppID:     appID,
						AppSecret: appSecret,
						Enabled:   true,
					}
					fmt.Println("   [QQ Official Bot] configured successfully!")
				}
			}
		case "7": // WeChat (Official Account/Mini Program)
			current := cfg.Gateway.Platforms["wechat"]
			fmt.Println("   WeChat connection mode:")
			fmt.Println("      [1] QR Code Login (recommended - scan to login)")
			fmt.Println("      [2] Callback Mode (webhook - requires public IP)")
			fmt.Printf("   Select mode (1-2, default 1): ")
			modeChoice := readInput(reader, "1")

			if modeChoice == "2" {
				// Traditional callback mode
				fmt.Printf("   Enter WeChat Official Account app_id (current: %s): ", current.AppID)
				appID := readInput(reader, current.AppID)
				fmt.Printf("   Enter WeChat app_secret (current: %s): ", maskString(current.AppSecret))
				appSecret := readInput(reader, current.AppSecret)
				fmt.Printf("   Enter WeChat token for callback verification (current: %s): ", maskString(current.Token))
				token := readInput(reader, current.Token)
				fmt.Printf("   Enter WeChat AESKey (current: %s): ", maskString(current.AESKey))
				aesKey := readInput(reader, current.AESKey)
				if appID != "" && appSecret != "" {
					cfg.Gateway.Platforms["wechat"] = config.PlatformConfig{
						AppID:     appID,
						AppSecret: appSecret,
						Token:     token,
						AESKey:    aesKey,
						Mode:      "callback",
						Enabled:   true,
					}
					fmt.Println("   [WeChat Official Account] configured in callback mode!")
				}
			} else {
				// QR Code login mode
				fmt.Printf("   Enter WeChat Official Account app_id (current: %s): ", current.AppID)
				appID := readInput(reader, current.AppID)
				fmt.Printf("   Enter WeChat app_secret (current: %s): ", maskString(current.AppSecret))
				appSecret := readInput(reader, current.AppSecret)
				if appID != "" && appSecret != "" {
					cfg.Gateway.Platforms["wechat"] = config.PlatformConfig{
						AppID:     appID,
						AppSecret: appSecret,
						Mode:      "qr",
						Enabled:   true,
					}
					fmt.Println("   [WeChat Official Account] configured in QR mode!")
					fmt.Println("   To login, start the gateway and scan the QR code:")
					fmt.Println("     magic gateway start")
				} else {
					fmt.Println("   [WeChat] app_id and app_secret are required for QR login")
				}
			}
		case "8": // WeChat-iLink (Personal WeChat)
			current := cfg.Gateway.Platforms["wechat_ilink"]
			clientIDDefault := "wechat-ilink"
			if current.ClientID != "" {
				clientIDDefault = current.ClientID
			}
			fmt.Printf("   Enter Client ID (current: %s): ", clientIDDefault)
			clientID := readInput(reader, clientIDDefault)

			baseURLDefault := "https://ilinkai.weixin.qq.com"
			if current.APIURL != "" {
				baseURLDefault = current.APIURL
			}
			fmt.Printf("   Enter API Base URL (current: %s): ", baseURLDefault)
			apiURL := readInput(reader, baseURLDefault)

			autoLoginDefault := "Y"
			if !current.AutoLogin {
				autoLoginDefault = "n"
			}
			fmt.Printf("   Enable auto-login (QR scan on startup)? (Y/n, default %s): ", autoLoginDefault)
			autoLoginChoice := readInput(reader, autoLoginDefault)
			autoLogin := !(autoLoginChoice == "n" || autoLoginChoice == "N")

			cfg.Gateway.Platforms["wechat_ilink"] = config.PlatformConfig{
				ClientID:  clientID,
				APIURL:    apiURL,
				AutoLogin: autoLogin,
				DataDir:   filepath.Join(magicDir, "wechat_ilink"),
				Enabled:   true,
			}
			fmt.Println("   [WeChat-iLink] configured successfully!")
			fmt.Println("   To bind WeChat, start the gateway and scan the QR code:")
			fmt.Println("     magic gateway start")
		case "9": // Slack
			current := cfg.Gateway.Platforms["slack"]
			fmt.Printf("   Enter Slack bot token (current: %s): ", maskString(current.Token))
			token := readInput(reader, current.Token)
			fmt.Printf("   Enter Slack signing secret (current: %s): ", maskString(current.AppSecret))
			appSecret := readInput(reader, current.AppSecret)
			if token != "" && appSecret != "" {
				cfg.Gateway.Platforms["slack"] = config.PlatformConfig{
					Token:     token,
					AppSecret: appSecret,
					Enabled:   true,
				}
				fmt.Println("   [Slack] configured successfully!")
			}
		case "10": // WhatsApp
			current := cfg.Gateway.Platforms["whatsapp"]
			fmt.Println("   WhatsApp connection mode:")
			fmt.Println("      [1] QR Code Login (recommended - scan with WhatsApp)")
			fmt.Println("      [2] Business API Mode (requires Meta Developer account)")
			fmt.Printf("   Select mode (1-2, default 1): ")
			waModeChoice := readInput(reader, "1")

			if waModeChoice == "2" {
				// Business API mode
				fmt.Printf("   Enter WhatsApp Phone Number ID (current: %s): ", current.AppID)
				appID := readInput(reader, current.AppID)
				fmt.Printf("   Enter WhatsApp access token (current: %s): ", maskString(current.Token))
				token := readInput(reader, current.Token)
				fmt.Printf("   Enter WhatsApp app_secret (current: %s): ", maskString(current.AppSecret))
				appSecret := readInput(reader, current.AppSecret)
				fmt.Printf("   Enter WhatsApp verify_token (current: %s): ", maskString(current.VerifyToken))
				verifyToken := readInput(reader, current.VerifyToken)
				if appID != "" && token != "" {
					cfg.Gateway.Platforms["whatsapp"] = config.PlatformConfig{
						AppID:       appID,
						Token:       token,
						AppSecret:   appSecret,
						VerifyToken: verifyToken,
						Mode:        "business",
						Enabled:     true,
					}
					fmt.Println("   [WhatsApp] configured in Business API mode!")
				}
			} else {
				// QR Code login mode (default)
				dataDir := current.DataDir
				if dataDir == "" {
					dataDir = filepath.Join(magicDir, "whatsapp")
				}
				fmt.Printf("   Enter data directory (current: %s): ", dataDir)
				dataDir = readInput(reader, dataDir)
				cfg.Gateway.Platforms["whatsapp"] = config.PlatformConfig{
					DataDir: dataDir,
					Mode:    "qr",
					Enabled: true,
				}
				fmt.Println("   [WhatsApp] configured in QR mode!")
				fmt.Println("   To login, start the gateway and scan the QR code:")
				fmt.Println("     magic gateway start")
			}
		case "11": // LINE
			current := cfg.Gateway.Platforms["line"]
			fmt.Printf("   Enter LINE Channel Access Token (current: %s): ", maskString(current.Token))
			token := readInput(reader, current.Token)
			fmt.Printf("   Enter LINE Channel Secret (current: %s): ", maskString(current.AppSecret))
			appSecret := readInput(reader, current.AppSecret)
			if token != "" && appSecret != "" {
				cfg.Gateway.Platforms["line"] = config.PlatformConfig{
					Token:     token,
					AppSecret: appSecret,
					Enabled:   true,
				}
				fmt.Println("   [LINE] configured successfully!")
			}
		case "12": // Matrix
			current := cfg.Gateway.Platforms["matrix"]
			apiURLDefault := "https://matrix.example.com"
			if current.APIURL != "" {
				apiURLDefault = current.APIURL
			}
			fmt.Println("   Matrix login mode:")
			fmt.Println("      [1] Access Token (recommended - get from Element or other client)")
			fmt.Println("      [2] Password Login (login with username + password)")
			fmt.Printf("   Select mode (1-2, default 1): ")
			matrixModeChoice := readInput(reader, "1")

			fmt.Printf("   Enter Matrix homeserver URL (current: %s): ", apiURLDefault)
			apiURL := readInput(reader, apiURLDefault)
			fmt.Printf("   Enter Matrix user ID (current: %s): ", current.AppID)
			appID := readInput(reader, current.AppID)

			if matrixModeChoice == "2" {
				// Password login mode
				fmt.Printf("   Enter Matrix password (current: %s): ", maskString(current.AppSecret))
				password := readInput(reader, current.AppSecret)
				if apiURL != "" && appID != "" && password != "" {
					cfg.Gateway.Platforms["matrix"] = config.PlatformConfig{
						APIURL:    apiURL,
						AppID:     appID,
						AppSecret: password,
						Mode:      "password",
						Enabled:   true,
					}
					fmt.Println("   [Matrix] configured in password login mode!")
				}
			} else {
				// Access token mode
				fmt.Printf("   Enter Matrix access token (current: %s): ", maskString(current.Token))
				token := readInput(reader, current.Token)
				if apiURL != "" && token != "" {
					cfg.Gateway.Platforms["matrix"] = config.PlatformConfig{
						APIURL: apiURL,
						AppID:  appID,
						Token:  token,
						Mode:   "token",
						Enabled: true,
					}
					fmt.Println("   [Matrix] configured in access token mode!")
				}
			}
		default:
			fmt.Println("   Invalid choice. Please select 0-12.")
		}
	}
}

// maskString returns a masked version of s for display.
// If s is empty, returns "(empty)".
func maskString(s string) string {
	if s == "" {
		return "(empty)"
	}
	if len(s) > 4 {
		return "..." + s[len(s)-4:]
	}
	return "****"
}
