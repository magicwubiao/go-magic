package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
		"deepseek":  {"DeepSeek", "deepseek-chat", "https://api.deepseek.com/v1"},
		"anthropic": {"Anthropic (Claude)", "claude-3-5-sonnet-20241022", "https://api.anthropic.com/v1"},
		"openai":    {"OpenAI (GPT-4, GPT-3.5)", "gpt-4o", "https://api.openai.com/v1"},
		"kimi":      {"Kimi (Moonshot)", "moonshot-v1-8k", "https://api.moonshot.cn/v1"},
		"zhipu":     {"Zhipu (GLM)", "glm-4", "https://open.bigmodel.cn/api/paas/v4"},
		"huoshan":   {"Huoshan (Volcano Engine)", "ep-20250105-xxxxx", "https://volcengine.com/api/v1"},
		"minimax":   {"MiniMax", "abab6-chat", "https://api.minimax.chat/v1"},
		"dashscope": {"Dashscope (Qwen)", "qwen-turbo", "https://dashscope.aliyuncs.com/api/v1"},
		"ollama":    {"Ollama (Local)", "llama3.2", "http://localhost:11434/v1"},
		"vllm":      {"vLLM (Local)", "", "http://localhost:8000/v1"},
		"openrouter": {"OpenRouter", "openrouter/anthropic/claude-3.5-sonnet", "https://openrouter.ai/api/v1"},
		"custom":    {"Other (custom)", "", ""},
	}

	// Map provider to selection number
	providerToNum := map[string]string{
		"deepseek": "1", "anthropic": "2", "openai": "3", "kimi": "4",
		"zhipu": "5", "huoshan": "6", "minimax": "7", "dashscope": "8",
		"ollama": "9", "vllm": "10", "openrouter": "11", "custom": "12",
	}

	// Provider selection - show current value
	currentProviderName := provider
	if p, ok := providerDefaults[provider]; ok {
		currentProviderName = p.name
	}

	fmt.Printf("1. Choose your LLM provider (current: %s):\n", currentProviderName)
	fmt.Println("   [1] DeepSeek")
	fmt.Println("   [2] Anthropic (Claude)")
	fmt.Println("   [3] OpenAI (GPT-4, GPT-3.5)")
	fmt.Println("   [4] Kimi (Moonshot)")
	fmt.Println("   [5] Zhipu (GLM)")
	fmt.Println("   [6] Huoshan (Volcano Engine)")
	fmt.Println("   [7] MiniMax")
	fmt.Println("   [8] Dashscope (Qwen)")
	fmt.Println("   [9] Ollama (Local)")
	fmt.Println("   [10] vLLM (Local)")
	fmt.Println("   [11] OpenRouter")
	fmt.Println("   [12] Other (custom)")
	fmt.Printf("   Select (1-12, default %s - keep current): ", providerToNum[provider])

	choice := readInput(reader, providerToNum[provider])

	// Resolve provider, model, baseURL from choice
	newProvider := provider
	model := provCfg.Model
	baseURL := provCfg.BaseURL

	switch choice {
	case "1":
		newProvider = "deepseek"
	case "2":
		newProvider = "anthropic"
	case "3":
		newProvider = "openai"
	case "4":
		newProvider = "kimi"
	case "5":
		newProvider = "zhipu"
	case "6":
		newProvider = "huoshan"
	case "7":
		newProvider = "minimax"
	case "8":
		newProvider = "dashscope"
	case "9":
		newProvider = "ollama"
	case "10":
		newProvider = "vllm"
	case "11":
		newProvider = "openrouter"
	case "12":
		newProvider = "custom"
	}

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
	if provCfg.APIKey != "" && provider == newProvider {
		// Show last 4 chars only
		if len(provCfg.APIKey) > 4 {
			apiKeyDisplay = " (current: ..." + provCfg.APIKey[len(provCfg.APIKey)-4:] + ")"
		} else {
			apiKeyDisplay = " (current: ****)"
		}
	}
	fmt.Printf("\n2. Enter your %s API key%s: ", provider, apiKeyDisplay)
	apiKey := readInput(reader, provCfg.APIKey)

	if apiKey == "" && provider != "ollama" && provider != "custom" && provider != "vllm" {
		fmt.Println("\n   Warning: API key is empty. You may need to configure it later.")
		fmt.Println("   Press Enter to continue or Ctrl+C to cancel...")
		reader.ReadString('\n')
	}

	// Custom provider additional fields
	if provider == "custom" {
		fmt.Printf("3. Enter API base URL (current: %s): ", baseURL)
		baseURL = readInput(reader, baseURL)

		fmt.Printf("4. Enter model name (current: %s): ", model)
		model = readInput(reader, model)
	}

	// Build provider config
	provCfg = config.ProviderConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}
	cfg.Providers[provider] = provCfg
	cfg.Model = model

	// Model selection
	fmt.Printf("\n3. Choose model (current: %s): ", model)
	inputModel := readInput(reader, model)
	cfg.Model = inputModel
	provCfg.Model = inputModel
	cfg.Providers[provider] = provCfg

	// Profile name
	profileLabel := "default"
	if cfg.Profile != "" {
		profileLabel = cfg.Profile
	}
	fmt.Printf("\n4. Enter profile name (current: %s): ", profileLabel)
	profile := readInput(reader, cfg.Profile)
	cfg.Profile = profile

	// Cortex AI enhancement
	cortexDefault := "Y"
	if !cfg.CortexEnabled {
		cortexDefault = "n"
	}
	fmt.Printf("\n5. Enable Cortex AI enhancement? (Y/n, default %s): ", cortexDefault)
	cortexChoice := readInput(reader, cortexDefault)
	cfg.CortexEnabled = !(cortexChoice == "n" || cortexChoice == "N")

	// Gateway
	gatewayDefault := "N"
	if cfg.Gateway.Enabled {
		gatewayDefault = "y"
	}
	fmt.Printf("\n6. Enable messaging gateway? (y/N, default %s): ", gatewayDefault)
	gatewayChoice := readInput(reader, gatewayDefault)
	if gatewayChoice == "y" || gatewayChoice == "Y" {
		cfg.Gateway.Enabled = true
		fmt.Println("   Gateway can be configured later with: magic gateway start")
		fmt.Println("\n   Gateway Platform Configuration:")
		fmt.Println("   Configure messaging platforms (Telegram, Discord, etc.)")

		for {
			fmt.Println("\n   Available platforms:")
			fmt.Println("      [1] Telegram")
			fmt.Println("      [2] Discord")
			fmt.Println("      [3] WeCom (企业微信)")
			fmt.Println("      [4] Feishu (飞书/Lark)")
			fmt.Println("      [5] DingTalk (钉钉)")
			fmt.Println("      [6] QQ (QQ机器人/频道)")
			fmt.Println("      [7] WeChat (微信公众号/小程序)")
			fmt.Println("      [8] WeChat-iLink (个人微信 via iLink)")
			fmt.Println("      [9] Slack")
			fmt.Println("      [10] WhatsApp")
			fmt.Println("      [11] LINE")
			fmt.Println("      [12] Matrix")
			fmt.Println("      [0] Done (结束配置)")

			fmt.Print("\n   Select platform to configure (0-12): ") 
			platformChoice, _ := reader.ReadString('\n')
			platformChoice = strings.TrimSpace(platformChoice)

			switch platformChoice {
			case "0":
				fmt.Println("   Platform configuration complete.")
				goto donePlatforms
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
						Enabled: true,
					}
					fmt.Println("   [WeCom] configured successfully!")
				}
			case "4": // Feishu (飞书/Lark)
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
			case "5": // DingTalk (钉钉)
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
			case "6": // QQ (支持新版官方机器人 app_id/app_secret + 旧版 number/password)
				current := cfg.Gateway.Platforms["qq"]
				fmt.Println("   QQ supports two auth methods:")
				fmt.Println("     [a] Official QQ Bot (app_id + app_secret) - 推荐")
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
					// Official QQ Bot mode (推荐)
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
			case "7": // WeChat (微信公众号/小程序)
				current := cfg.Gateway.Platforms["wechat"]
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
						Enabled:   true,
					}
					fmt.Println("   [WeChat Official Account] configured successfully!")
				}
			case "8": // WeChat-iLink (个人微信)
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
						Enabled:     true,
					}
					fmt.Println("   [WhatsApp] configured successfully!")
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
				fmt.Printf("   Enter Matrix homeserver URL (current: %s): ", apiURLDefault)
				apiURL := readInput(reader, apiURLDefault)
				fmt.Printf("   Enter Matrix user ID / app_id (current: %s): ", current.AppID)
				appID := readInput(reader, current.AppID)
				fmt.Printf("   Enter Matrix access token (current: %s): ", maskString(current.Token))
				token := readInput(reader, current.Token)
				if apiURL != "" && token != "" {
					cfg.Gateway.Platforms["matrix"] = config.PlatformConfig{
						APIURL:  apiURL,
						AppID:   appID,
						Token:   token,
						Enabled: true,
					}
					fmt.Println("   [Matrix] configured successfully!")
				}
			default:
				fmt.Println("   Invalid choice. Please select 0-12.")
			}
		}
	donePlatforms:
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
