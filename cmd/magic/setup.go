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

	"github.com/magicwubiao/go-magic/pkg/config"
)

// setupCmd represents the setup command
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard",
	Long:  `Run the interactive setup wizard to configure go-magic AI Provider, model, tools and Gateway.`,
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
	setupCmd.Flags().BoolVar(&skipGateway, "skip-gateway", false, "Skip Gateway configuration")
	rootCmd.AddCommand(setupCmd)
}

// providerInfo describes an AI Provider

type providerInfo struct {
	Name         string
	DisplayName  string
	Description  string
	Models       []string
	NeedsAPIKey  bool
	NeedsBaseURL bool
	DefaultURL   string
}

// allProviders returns all supported Providers
func allProviders() []providerInfo {
	return []providerInfo{
		// Recommended
		{Name: "deepseek", DisplayName: "DeepSeek", Description: "DeepSeek V3/R1, high cost-effective reasoning models", Models: []string{"deepseek-chat", "deepseek-reasoner"}, NeedsAPIKey: true, DefaultURL: "https://api.deepseek.com"},
		{Name: "openai", DisplayName: "OpenAI", Description: "GPT-5.6 series models", Models: []string{"gpt-5.6", "gpt-5.6-terra", "gpt-5.6-luna"}, NeedsAPIKey: true, DefaultURL: "https://api.openai.com/v1"},
		{Name: "anthropic", DisplayName: "Anthropic", Description: "Claude Sonnet 5, Opus 5, etc.", Models: []string{"claude-sonnet-5", "claude-fable-5", "claude-opus-5", "claude-haiku-4-5"}, NeedsAPIKey: true, DefaultURL: "https://api.anthropic.com"},

		// China Providers
		{Name: "dashscope", DisplayName: "Tongyi Qianwen (DashScope)", Description: "Alibaba Cloud Tongyi Qianwen LLM", Models: []string{"qwen3-max", "qwen3-plus", "qwen-plus", "qwen-turbo", "qwen-long"}, NeedsAPIKey: true, DefaultURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		{Name: "minimax", DisplayName: "MiniMax", Description: "MiniMax LLM", Models: []string{"abab6.5s-chat", "abab6.5-chat"}, NeedsAPIKey: true, DefaultURL: "https://api.minimax.chat/v1"},
		{Name: "zhipu", DisplayName: "Zhipu AI (GLM)", Description: "Zhipu GLM series models", Models: []string{"glm-4.7", "glm-4.6", "glm-4-flash", "glm-4v"}, NeedsAPIKey: true, DefaultURL: "https://open.bigmodel.cn/api/paas/v4"},
		{Name: "huoshan", DisplayName: "火山引擎 (豆包)", Description: "字节跳动豆包大模型，经火山方舟 Ark 提供", Models: []string{"doubao-seed-1.6", "doubao-1.5-pro-32k", "doubao-1.5-thinking-pro", "doubao-pro-32k"}, NeedsAPIKey: true, DefaultURL: "https://ark.cn-beijing.volces.com/api/v3"},
		{Name: "wenxin", DisplayName: "Wenxin Yiyan", Description: "Baidu Wenxin Yiyan LLM", Models: []string{"ernie-4.0-8k", "ernie-3.5-8k", "ernie-speed-128k"}, NeedsAPIKey: true, DefaultURL: "https://aip.baidubce.com/rpc/2.0/ai_qianfan_200/v1"},
		{Name: "moonshot", DisplayName: "Moonshot (Kimi)", Description: "Moonshot Kimi LLM", Models: []string{"kimi-k2-0905-preview", "kimi-k2-turbo-preview", "moonshot-v1-128k"}, NeedsAPIKey: true, DefaultURL: "https://api.moonshot.cn/v1"},
		{Name: "hunyuan", DisplayName: "Hunyuan", Description: "Tencent Hunyuan LLM", Models: []string{"hunyuan-pro", "hunyuan-standard", "hunyuan-lite"}, NeedsAPIKey: true, DefaultURL: "https://hunyuan.cloud.tencent.com/v1"},

		// Local
		{Name: "ollama", DisplayName: "Ollama", Description: "Local models (Llama, Mistral, Qwen, etc.)", Models: []string{"llama3.3", "qwen3", "mistral", "codellama", "phi3"}, NeedsAPIKey: false, DefaultURL: "http://localhost:11434"},
		{Name: "vllm", DisplayName: "vLLM", Description: "Local high-performance inference service", Models: []string{"llama3", "qwen2.5"}, NeedsAPIKey: false, DefaultURL: "http://localhost:8000"},

		// Aggregators
		{Name: "openrouter", DisplayName: "OpenRouter", Description: "Unified API for 100+ models", Models: []string{"openai/gpt-5.6", "anthropic/claude-sonnet-5", "google/gemini-3.7-flash", "deepseek/deepseek-chat"}, NeedsAPIKey: true, DefaultURL: "https://openrouter.ai/api/v1"},
		{Name: "together", DisplayName: "Together AI", Description: "Open source model hosting platform", Models: []string{"meta-llama/Llama-3.3-70B-Instruct-Turbo", "deepseek-ai/DeepSeek-V3", "deepseek-ai/DeepSeek-R1"}, NeedsAPIKey: true, DefaultURL: "https://api.together.xyz/v1"},
		{Name: "groq", DisplayName: "Groq", Description: "Ultra-fast LLM inference", Models: []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant"}, NeedsAPIKey: true, DefaultURL: "https://api.groq.com/openai/v1"},
		{Name: "perplexity", DisplayName: "Perplexity", Description: "AI search engine models", Models: []string{"sonar-pro", "sonar", "sonar-reasoning-pro", "sonar-deep-research"}, NeedsAPIKey: true, DefaultURL: "https://api.perplexity.ai"},

		// Others
		{Name: "gemini", DisplayName: "Google Gemini", Description: "Google Gemini series models", Models: []string{"gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash", "gemini-3.1-pro-preview", "gemini-2.5-pro"}, NeedsAPIKey: true, DefaultURL: "https://generativelanguage.googleapis.com/v1beta"},
		{Name: "mistral", DisplayName: "Mistral AI", Description: "Mistral series open source models", Models: []string{"mistral-large-latest", "mistral-medium-latest", "mistral-small-latest", "codestral-latest"}, NeedsAPIKey: true, DefaultURL: "https://api.mistral.ai/v1"},
		{Name: "cohere", DisplayName: "Cohere", Description: "Cohere Command series models", Models: []string{"command-r-plus", "command-r", "command"}, NeedsAPIKey: true, DefaultURL: "https://api.cohere.ai/v2"},
		{Name: "mimo", DisplayName: "MiMo", Description: "MiMo LLM", Models: []string{"mimo-3-7b"}, NeedsAPIKey: true, DefaultURL: "https://api.mymimo.ai/v1"},
		{Name: "custom", DisplayName: "Custom (OpenAI Compatible)", Description: "Custom service compatible with OpenAI API format", Models: []string{}, NeedsAPIKey: true, NeedsBaseURL: true},
	}
}

// providerGroup represents a group of Providers for display
type providerGroup struct {
	Label     string
	Providers []providerInfo
}

// groupedProviders groups all Providers by category
func groupedProviders() []providerGroup {
	all := allProviders()
	groups := []providerGroup{
		{Label: "Recommended", Providers: all[0:3]},
		{Label: "China", Providers: all[3:11]},
		{Label: "Local", Providers: all[11:13]},
		{Label: "Aggregators", Providers: all[13:17]},
		{Label: "Others", Providers: all[17:]},
	}
	return groups
}

// platformInfo describes a Gateway platform
type platformInfo struct {
	Name        string
	DisplayName string
	Description string
}

// allPlatforms returns all supported Gateway platforms
func allPlatforms() []platformInfo {
	return []platformInfo{
		{Name: "telegram", DisplayName: "Telegram", Description: "Telegram Bot"},
		{Name: "discord", DisplayName: "Discord", Description: "Discord Bot"},
		{Name: "slack", DisplayName: "Slack", Description: "Slack Bot"},
		{Name: "wechat", DisplayName: "WeChat", Description: "WeChat Personal Account (iLink QR Login)"},
		{Name: "wecom", DisplayName: "WeCom", Description: "WeCom (Enterprise WeChat)"},
		{Name: "qq", DisplayName: "QQ", Description: "QQ Bot"},
		{Name: "dingtalk", DisplayName: "DingTalk", Description: "DingTalk Bot"},
		{Name: "feishu", DisplayName: "Feishu/Lark", Description: "Feishu/Lark Bot"},
		{Name: "whatsapp", DisplayName: "WhatsApp", Description: "WhatsApp Bot"},
		{Name: "line", DisplayName: "LINE", Description: "LINE Bot"},
		{Name: "matrix", DisplayName: "Matrix", Description: "Matrix Protocol"},
	}
}

// runSetup is the main entry point for setup command
func runSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	// Load or create default config
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Ensure Providers map is not nil
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}

	// Banner
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║              go-magic Setup Wizard                        ║")
	fmt.Println("║         High-performance AI Agent Framework               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Check OpenClaw migration
	homeDir, _ := os.UserHomeDir()
	openclawDir := filepath.Join(homeDir, ".openclaw")
	if _, statErr := os.Stat(openclawDir); statErr == nil {
		fmt.Printf("  Detected OpenClaw config: %s\n", openclawDir)
		fmt.Print("  Migrate config? (y/N): ")
		if answer, _ := reader.ReadString('\n'); strings.TrimSpace(strings.ToLower(answer)) == "y" {
			fmt.Println("  Run 'magic migrate' after setup to complete migration.")
		}
		fmt.Println()
	}

	_ = homeDir // kept for backward compatibility / future use

	// Step 1: Provider selection
	var selectedProvider *providerInfo
	if !skipModel {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  Step 1: Select AI Provider")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		selectedProvider = runProviderSelection(reader, cfg)
	}

	// Step 2: API Key input
	if selectedProvider != nil && selectedProvider.NeedsAPIKey {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  Step 2: Enter API Key")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		runAPIKeyInput(reader, cfg, selectedProvider)
	}

	// Step 3: Model selection
	if selectedProvider != nil && !skipModel {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  Step 3: Select Model")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		runModelSelection(reader, cfg, selectedProvider)
	}

	// Step 4: Base URL input
	if selectedProvider != nil {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  Step 4: Configure Base URL")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		runBaseURLInput(reader, cfg, selectedProvider)
	}

	// Step 5: Gateway configuration (inline)
	if !skipGateway {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  Step 5: Gateway Configuration (Optional)")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		runGatewaySetupInline(reader, cfg)
	}

	// Step 6: Save configuration
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Step 6: Save Configuration")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Verify
	fmt.Println()
	fmt.Println("  Configuration saved!")
	verifySetup()

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Setup Complete!                        ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  - Run 'magic' to start chatting")
	fmt.Println("  - Run 'magic model' to switch AI Provider/model")
	fmt.Println("  - Run 'magic gateway start' to start messaging gateway")
	fmt.Println()
	fmt.Println("Config file: " + filepath.Join(config.GetMagicHome(), "config.json"))
	fmt.Println()

	return nil
}

// runProviderSelection displays grouped Provider list and lets user select
func runProviderSelection(reader *bufio.Reader, cfg *config.Config) *providerInfo {
	groups := groupedProviders()

	// Build index to Provider mapping
	indexMap := make(map[int]*providerInfo)
	currentIndex := 1

	fmt.Println()
	fmt.Println("  Select AI Provider (enter number or search by name):")
	fmt.Println()

	for _, group := range groups {
		fmt.Printf("  --- %s ---\n", group.Label)
		for i := range group.Providers {
			p := &group.Providers[i]
			indexMap[currentIndex] = p

			// Mark current selection if configured
			marker := ""
			if cfg.Provider == p.Name {
				marker = " [current]"
			}
			fmt.Printf("    [%d] %s - %s%s\n", currentIndex, p.DisplayName, p.Description, marker)
			currentIndex++
		}
		fmt.Println()
	}

	fmt.Print("  Enter number (default: 1): ")
	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(selection)

	if selection == "" {
		selection = "1"
	}

	// Try to select by number
	num, err := strconv.Atoi(selection)
	if err == nil {
		if p, ok := indexMap[num]; ok {
			fmt.Printf("  Selected: %s\n\n", p.DisplayName)
			cfg.Provider = p.Name
			return p
		}
	}

	// Try to search by name
	lower := strings.ToLower(selection)
	var matches []providerInfo
	for _, p := range allProviders() {
		if strings.Contains(strings.ToLower(p.Name), lower) ||
			strings.Contains(strings.ToLower(p.DisplayName), lower) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 1 {
		fmt.Printf("  Selected: %s\n\n", matches[0].DisplayName)
		cfg.Provider = matches[0].Name
		return &matches[0]
	} else if len(matches) > 1 {
		fmt.Println("  Multiple matches found:")
		for i, m := range matches {
			fmt.Printf("    [%d] %s - %s\n", i+1, m.DisplayName, m.Description)
		}
		fmt.Print("  Enter number: ")
		sel2, _ := reader.ReadString('\n')
		sel2 = strings.TrimSpace(sel2)
		if num2, err2 := strconv.Atoi(sel2); err2 == nil && num2 >= 1 && num2 <= len(matches) {
			fmt.Printf("  Selected: %s\n\n", matches[num2-1].DisplayName)
			cfg.Provider = matches[num2-1].Name
			return &matches[num2-1]
		}
	}

	// Default to first (deepseek)
	defaultP := allProviders()[0]
	fmt.Printf("  No match found, using default: %s\n\n", defaultP.DisplayName)
	cfg.Provider = defaultP.Name
	return &defaultP
}

// runAPIKeyInput collects API Key
func runAPIKeyInput(reader *bufio.Reader, cfg *config.Config, p *providerInfo) {
	fmt.Println()

	// Check if API Key already exists
	existingCfg, ok := cfg.Providers[p.Name]
	if ok && existingCfg.APIKey != "" {
		fmt.Printf("  %s API Key already configured (****%s)\n", p.DisplayName, maskKey(existingCfg.APIKey))
		fmt.Print("  Re-enter? (y/N): ")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("  Keeping existing API Key")
			fmt.Println()
			return
		}
	}

	fmt.Printf("  Enter %s API Key:\n", p.DisplayName)
	fmt.Println("  (Note: input will be displayed on screen, ensure no one is watching)")
	fmt.Print("  API Key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	if apiKey != "" {
		if cfg.Providers[p.Name].APIKey != "" || cfg.Providers[p.Name].BaseURL != "" || cfg.Providers[p.Name].Model != "" {
			// Preserve existing config
			existing := cfg.Providers[p.Name]
			existing.APIKey = apiKey
			cfg.Providers[p.Name] = existing
		} else {
			cfg.Providers[p.Name] = config.ProviderConfig{
				APIKey: apiKey,
			}
		}
		fmt.Printf("  API Key saved for %s\n", p.DisplayName)
	} else {
		fmt.Println("  No API Key entered, skipped")
	}
	fmt.Println()
}

// runModelSelection lets user select a model
func runModelSelection(reader *bufio.Reader, cfg *config.Config, p *providerInfo) {
	fmt.Println()

	if len(p.Models) == 0 {
		// Custom provider, let user input model name
		fmt.Printf("  Enter model name for %s: ", p.DisplayName)
		model, _ := reader.ReadString('\n')
		model = strings.TrimSpace(model)
		if model != "" {
			cfg.Model = model
			existing := cfg.Providers[p.Name]
			existing.Model = model
			cfg.Providers[p.Name] = existing
			fmt.Printf("  Model set to: %s\n", model)
		}
		fmt.Println()
		return
	}

	// Show current model
	currentModel := cfg.Model
	if existing, ok := cfg.Providers[p.Name]; ok && existing.Model != "" {
		currentModel = existing.Model
	}

	fmt.Printf("  Available models for %s:\n", p.DisplayName)
	for i, m := range p.Models {
		marker := ""
		if m == currentModel {
			marker = " [current]"
		}
		fmt.Printf("    [%d] %s%s\n", i+1, m, marker)
	}
	fmt.Println()
	fmt.Printf("  Enter number (default: 1): ")

	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(selection)

	if selection == "" {
		selection = "1"
	}

	num, err := strconv.Atoi(selection)
	if err != nil || num < 1 || num > len(p.Models) {
		num = 1
	}

	selectedModel := p.Models[num-1]
	cfg.Model = selectedModel

	existing := cfg.Providers[p.Name]
	existing.Model = selectedModel
	cfg.Providers[p.Name] = existing

	fmt.Printf("  Model set to: %s\n", selectedModel)
	fmt.Println()
}

// runBaseURLInput lets user configure Base URL
func runBaseURLInput(reader *bufio.Reader, cfg *config.Config, p *providerInfo) {
	fmt.Println()

	// Get current base URL
	currentURL := p.DefaultURL
	if existing, ok := cfg.Providers[p.Name]; ok && existing.BaseURL != "" {
		currentURL = existing.BaseURL
	}

	fmt.Printf("  Base URL for %s:\n", p.DisplayName)
	fmt.Printf("  Current: %s\n", currentURL)

	if p.NeedsBaseURL || p.Name == "custom" {
		fmt.Print("  Enter Base URL (required): ")
	} else {
		fmt.Print("  Enter Base URL (Enter to keep current): ")
	}

	url, _ := reader.ReadString('\n')
	url = strings.TrimSpace(url)

	if url != "" {
		existing := cfg.Providers[p.Name]
		existing.BaseURL = url
		cfg.Providers[p.Name] = existing
		fmt.Printf("  Base URL updated: %s\n", url)
	} else if p.NeedsBaseURL || p.Name == "custom" {
		// For custom provider, base URL is required
		fmt.Printf("  Using default: %s\n", currentURL)
		existing := cfg.Providers[p.Name]
		existing.BaseURL = currentURL
		cfg.Providers[p.Name] = existing
	} else {
		fmt.Println("  Keeping current Base URL")
	}
	fmt.Println()
}

// runGatewaySetupInline handles Gateway configuration inline
func runGatewaySetupInline(reader *bufio.Reader, cfg *config.Config) {
	fmt.Println()
	fmt.Println("  Configure messaging gateway to enable Telegram, Discord, etc.")
	fmt.Print("  Setup Gateway now? (y/N): ")

	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Println("  Gateway setup skipped")
		fmt.Println("  Run 'magic gateway setup' later to configure")
		fmt.Println()
		return
	}

	// Enable Gateway
	cfg.Gateway.Enabled = true
	if cfg.Gateway.Platforms == nil {
		cfg.Gateway.Platforms = make(map[string]config.PlatformConfig)
	}

	fmt.Println()
	fmt.Println("  Available platforms:")
	platforms := allPlatforms()
	for i, p := range platforms {
		marker := ""
		if existing, ok := cfg.Gateway.Platforms[p.Name]; ok && existing.Enabled {
			marker = " [configured]"
		}
		fmt.Printf("    [%d] %s - %s%s\n", i+1, p.DisplayName, p.Description, marker)
	}
	fmt.Println()
	fmt.Println("  Enter platform numbers to configure (comma-separated, e.g., 1,3,5)")
	fmt.Print("  Or type 'all' to configure all: ")

	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(strings.ToLower(selection))

	if selection == "" {
		fmt.Println("  No platforms selected")
		fmt.Println()
		return
	}

	var toConfigure []string
	if selection == "all" {
		for _, p := range platforms {
			toConfigure = append(toConfigure, p.Name)
		}
	} else {
		parts := strings.Split(selection, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if num, err := strconv.Atoi(part); err == nil && num >= 1 && num <= len(platforms) {
				toConfigure = append(toConfigure, platforms[num-1].Name)
			}
		}
	}

	// Configure each selected platform
	for _, name := range toConfigure {
		fmt.Println()
		runPlatformSetup(reader, cfg, name)
	}

	fmt.Println()
	fmt.Printf("  Gateway enabled with %d platform(s)\n", len(toConfigure))
	fmt.Println()
}

// runPlatformSetup configures a single platform
func runPlatformSetup(reader *bufio.Reader, cfg *config.Config, name string) {
	switch name {
	case "telegram":
		fmt.Println("  Telegram Bot Configuration")
		fmt.Print("  Enter Bot Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			cfg.Gateway.Platforms["telegram"] = config.PlatformConfig{
				Enabled: true,
				Token:   token,
			}
			fmt.Printf("  ✓ Telegram configured\n")
		} else {
			fmt.Println("  ✗ Skipped (Bot Token is required)")
		}

	case "discord":
		fmt.Println("  Discord Bot Configuration")
		fmt.Print("  Enter Bot Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			cfg.Gateway.Platforms["discord"] = config.PlatformConfig{
				Enabled: true,
				Token:   token,
			}
			fmt.Printf("  ✓ Discord configured\n")
		} else {
			fmt.Println("  ✗ Skipped (Bot Token is required)")
		}

	case "slack":
		fmt.Println("  Slack Bot Configuration")
		fmt.Print("  Enter Bot Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token == "" {
			fmt.Println("  ✗ Skipped (Bot Token is required)")
			return
		}
		fmt.Print("  Enter App Secret (optional): ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		cfg.Gateway.Platforms["slack"] = config.PlatformConfig{
			Enabled: true,
			Token:   token,
			Secret:  secret,
		}
		fmt.Printf("  ✓ Slack configured\n")

	case "wechat":
		fmt.Println("  WeChat (iLink) Configuration")
		fmt.Println("  This uses the iLink Bot API for personal WeChat accounts.")
		fmt.Println("  You will need to scan a QR code to login after starting the gateway.")
		fmt.Print("  Enable WeChat? (y/N): ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" {
			cfg.Gateway.Platforms["wechat_ilink"] = config.PlatformConfig{
				Enabled: true,
			}
			fmt.Printf("  ✓ WeChat configured.\n")
			fmt.Println("  Note: Run 'magic gateway start' and scan the QR code to login.")
		} else {
			fmt.Println("  ✗ Skipped")
		}

	case "wecom":
		fmt.Println("  WeCom Configuration")
		fmt.Print("  Enter Corp ID: ")
		corpID, _ := reader.ReadString('\n')
		corpID = strings.TrimSpace(corpID)
		if corpID == "" {
			fmt.Println("  ✗ Skipped (Corp ID is required)")
			return
		}
		fmt.Print("  Enter Agent ID: ")
		agentID, _ := reader.ReadString('\n')
		agentID = strings.TrimSpace(agentID)
		fmt.Print("  Enter Secret: ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		fmt.Print("  Mode (app/qr, default qr): ")
		mode, _ := reader.ReadString('\n')
		mode = strings.TrimSpace(mode)
		if mode == "" {
			mode = "qr"
		}
		cfg.Gateway.Platforms["wecom"] = config.PlatformConfig{
			Enabled: true,
			CorpID:  corpID,
			AgentID: agentID,
			Secret:  secret,
			Mode:    mode,
		}
		fmt.Printf("  ✓ WeCom configured (mode: %s)\n", mode)

	case "qq":
		fmt.Println("  QQ Bot Configuration")
		fmt.Print("  Enter QQ Number: ")
		number, _ := reader.ReadString('\n')
		number = strings.TrimSpace(number)
		if number == "" {
			fmt.Println("  ✗ Skipped (QQ Number is required)")
			return
		}
		fmt.Print("  Enter Password (optional): ")
		password, _ := reader.ReadString('\n')
		password = strings.TrimSpace(password)
		qqCfg := config.PlatformConfig{
			Enabled: true,
			Number:  number,
		}
		if password != "" {
			qqCfg.Password = password
		}
		cfg.Gateway.Platforms["qq"] = qqCfg
		fmt.Printf("  ✓ QQ configured\n")

	case "dingtalk":
		fmt.Println("  DingTalk Bot Configuration")
		fmt.Print("  Enter App Key: ")
		appKey, _ := reader.ReadString('\n')
		appKey = strings.TrimSpace(appKey)
		if appKey == "" {
			fmt.Println("  ✗ Skipped (App Key is required)")
			return
		}
		fmt.Print("  Enter App Secret: ")
		appSecret, _ := reader.ReadString('\n')
		appSecret = strings.TrimSpace(appSecret)
		cfg.Gateway.Platforms["dingtalk"] = config.PlatformConfig{
			Enabled:   true,
			AppKey:    appKey,
			AppSecret: appSecret,
		}
		fmt.Printf("  ✓ DingTalk configured\n")

	case "feishu":
		fmt.Println("  Feishu/Lark Bot Configuration")
		fmt.Print("  Enter App ID: ")
		appID, _ := reader.ReadString('\n')
		appID = strings.TrimSpace(appID)
		if appID == "" {
			fmt.Println("  ✗ Skipped (App ID is required)")
			return
		}
		fmt.Print("  Enter App Secret: ")
		appSecret, _ := reader.ReadString('\n')
		appSecret = strings.TrimSpace(appSecret)
		cfg.Gateway.Platforms["feishu"] = config.PlatformConfig{
			Enabled:   true,
			AppID:     appID,
			AppSecret: appSecret,
		}
		fmt.Printf("  ✓ Feishu configured\n")

	case "whatsapp":
		fmt.Println("  WhatsApp Configuration")
		fmt.Print("  Enter Verify Token (optional, press Enter to skip): ")
		verifyToken, _ := reader.ReadString('\n')
		verifyToken = strings.TrimSpace(verifyToken)
		fmt.Print("  Mode (personal/business, default personal): ")
		mode, _ := reader.ReadString('\n')
		mode = strings.TrimSpace(mode)
		if mode == "" {
			mode = "personal"
		}
		cfg.Gateway.Platforms["whatsapp"] = config.PlatformConfig{
			Enabled:     true,
			VerifyToken: verifyToken,
			Mode:        mode,
		}
		fmt.Printf("  ✓ WhatsApp configured (mode: %s)\n", mode)

	case "line":
		fmt.Println("  LINE Bot Configuration")
		fmt.Print("  Enter Channel Access Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token == "" {
			fmt.Println("  ✗ Skipped (Channel Access Token is required)")
			return
		}
		fmt.Print("  Enter Channel Secret (optional): ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		cfg.Gateway.Platforms["line"] = config.PlatformConfig{
			Enabled: true,
			Token:   token,
			Secret:  secret,
		}
		fmt.Printf("  ✓ LINE configured\n")

	case "matrix":
		fmt.Println("  Matrix Configuration")
		fmt.Print("  Enter Homeserver URL (e.g., https://matrix.org): ")
		homeserver, _ := reader.ReadString('\n')
		homeserver = strings.TrimSpace(homeserver)
		if homeserver == "" {
			fmt.Println("  ✗ Skipped (Homeserver URL is required)")
			return
		}
		fmt.Print("  Enter Access Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token == "" {
			fmt.Println("  ✗ Skipped (Access Token is required)")
			return
		}
		cfg.Gateway.Platforms["matrix"] = config.PlatformConfig{
			Enabled: true,
			Token:   token,
			APIURL:  homeserver,
		}
		fmt.Printf("  ✓ Matrix configured\n")
	}
}

// maskKey masks an API key for display
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[len(key)-4:]
}

// verifySetup verifies the installation
func verifySetup() {
	fmt.Println()
	fmt.Println("  Verifying configuration...")
	fmt.Println()

	configPath := filepath.Join(config.GetMagicHome(), "config.json")

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("  Config file created")
	} else {
		fmt.Println("  Warning: Config file not found")
	}

	// Test command availability
	fmt.Println()
	fmt.Println("  Checking dependencies...")

	commands := []string{"curl", "git"}
	if runtime.GOOS == "windows" {
		commands = []string{"curl.exe", "git.exe"}
	}

	for _, cmdName := range commands {
		path, err := exec.LookPath(cmdName)
		if err != nil {
			fmt.Printf("  %s not found (optional)\n", cmdName)
		} else {
			fmt.Printf("  %s: %s\n", cmdName, path)
		}
	}
}
