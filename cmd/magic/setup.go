package main

import (
	"bufio"
	"fmt"
	"os"
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

func runSetup(cmd *cobra.Command, args []string) {
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║       magic Agent Setup Wizard         ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Println()

	cfg := &config.Config{
		Profile:    "default",
		MagicHome:  "~/.magic",
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

	reader := bufio.NewReader(os.Stdin)

	// Provider selection
	fmt.Println("1. Choose your LLM provider:")
	fmt.Println("   [1] OpenAI (GPT-4, GPT-3.5)")
	fmt.Println("   [2] Anthropic (Claude)")
	fmt.Println("   [3] DeepSeek")
	fmt.Println("   [4] Kimi (Moonshot)")
	fmt.Println("   [5] Zhipu (GLM)")
	fmt.Println("   [6] Huoshan (Volcano Engine)")
	fmt.Println("   [7] MiniMax")
	fmt.Println("   [8] Dashscope (Qwen)")
	fmt.Println("   [9] Ollama (Local)")
	fmt.Println("   [10] vLLM (Local)")
	fmt.Println("   [11] OpenRouter")
	fmt.Println("   [12] Other (custom)")
	fmt.Print("   Select (1-12, default 1): ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	provider := "openai"
	model := "gpt-4o"
	baseURL := "https://api.openai.com/v1"

	switch choice {
	case "2":
		provider = "anthropic"
		model = "claude-3-5-sonnet-20241022"
		baseURL = "https://api.anthropic.com/v1"
	case "3":
		provider = "deepseek"
		model = "deepseek-chat"
		baseURL = "https://api.deepseek.com/v1"
	case "4":
		provider = "kimi"
		model = "moonshot-v1-8k"
		baseURL = "https://api.moonshot.cn/v1"
	case "5":
		provider = "zhipu"
		model = "glm-4"
		baseURL = "https://open.bigmodel.cn/api/paas/v4"
	case "6":
		provider = "huoshan"
		model = "ep-20250105-xxxxx"
		baseURL = "https://volcengine.com/api/v1"
	case "7":
		provider = "minimax"
		model = "abab6-chat"
		baseURL = "https://api.minimax.chat/v1"
	case "8":
		provider = "dashscope"
		model = "qwen-turbo"
		baseURL = "https://dashscope.aliyuncs.com/api/v1"
	case "9":
		provider = "ollama"
		model = "llama3.2"
		baseURL = "http://localhost:11434/v1"
	case "10":
		provider = "vllm"
		model = ""
		baseURL = "http://localhost:8000/v1"
	case "11":
		provider = "openrouter"
		model = "openrouter/anthropic/claude-3.5-sonnet"
		baseURL = "https://openrouter.ai/api/v1"
	case "12":
		provider = "custom"
		model = ""
		baseURL = ""
		fmt.Println("\n   Note: You will need to configure custom provider manually.")
	default:
		// Use defaults for OpenAI
	}

	cfg.Provider = provider

	// API Key
	fmt.Printf("\n2. Enter your %s API key: ", provider)
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	// Validate API key is not empty (except for Ollama which may not need one)
	if apiKey == "" && provider != "ollama" && provider != "custom" {
		fmt.Println("\n   Warning: API key is empty. You may need to configure it later.")
		fmt.Println("   Press Enter to continue or Ctrl+C to cancel...")
		reader.ReadString('\n')
	}

	// Custom base URL for "other" option
	if provider == "custom" {
		fmt.Print("\n3. Enter API base URL: ")
		baseURL, _ = reader.ReadString('\n')
		baseURL = strings.TrimSpace(baseURL)

		fmt.Print("4. Enter model name: ")
		model, _ = reader.ReadString('\n')
		model = strings.TrimSpace(model)
	}

	provCfg := config.ProviderConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}

	cfg.Providers[provider] = provCfg
	cfg.Model = model

	// Model selection
	if model != "" {
		fmt.Printf("\n3. Choose model (default: %s): ", model)
		inputModel, _ := reader.ReadString('\n')
		inputModel = strings.TrimSpace(inputModel)
		if inputModel != "" {
			cfg.Model = inputModel
			provCfg.Model = inputModel
			cfg.Providers[provider] = provCfg
		}
	}

	// Profile name
	fmt.Printf("\n4. Enter profile name (default: default): ")
	profile, _ := reader.ReadString('\n')
	profile = strings.TrimSpace(profile)
	if profile != "" {
		cfg.Profile = profile
	}

	// Working directory
	fmt.Printf("\n5. Working directory (default: current directory): ")
	workDir, _ := reader.ReadString('\n')
	workDir = strings.TrimSpace(workDir)
	if workDir != "" {
		cfg.WorkingDir = workDir
	}

	// Ask about gateway
	fmt.Print("\n6. Enable messaging gateway? (y/N): ")
	gatewayChoice, _ := reader.ReadString('\n')
	gatewayChoice = strings.TrimSpace(gatewayChoice)
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
			fmt.Println("      [4] Feishu (飞书)")
			fmt.Println("      [5] DingTalk (钉钉)")
			fmt.Println("      [6] QQ")
			fmt.Println("      [7] WeChat (微信)")
			fmt.Println("      [0] Done (结束配置)")

			fmt.Print("\n   Select platform to configure (0-7): ")
			platformChoice, _ := reader.ReadString('\n')
			platformChoice = strings.TrimSpace(platformChoice)

			switch platformChoice {
			case "0":
				fmt.Println("   Platform configuration complete.")
				break
			case "1": // Telegram
				fmt.Print("   Enter Telegram bot token: ")
				token, _ := reader.ReadString('\n')
				token = strings.TrimSpace(token)
				if token != "" {
					cfg.Gateway.Platforms["telegram"] = config.PlatformConfig{
						Token:   token,
						Enabled: true,
					}
					fmt.Println("   [Telegram] configured successfully!")
				}
			case "2": // Discord
				fmt.Print("   Enter Discord bot token: ")
				token, _ := reader.ReadString('\n')
				token = strings.TrimSpace(token)
				if token != "" {
					cfg.Gateway.Platforms["discord"] = config.PlatformConfig{
						Token:   token,
						Enabled: true,
					}
					fmt.Println("   [Discord] configured successfully!")
				}
			case "3": // WeCom
				fmt.Print("   Enter WeCom corp_id: ")
				corpID, _ := reader.ReadString('\n')
				corpID = strings.TrimSpace(corpID)
				fmt.Print("   Enter WeCom agent_id: ")
				agentID, _ := reader.ReadString('\n')
				agentID = strings.TrimSpace(agentID)
				fmt.Print("   Enter WeCom secret: ")
				secret, _ := reader.ReadString('\n')
				secret = strings.TrimSpace(secret)
				fmt.Print("   Enter WeCom token (for callback verification): ")
				token, _ := reader.ReadString('\n')
				token = strings.TrimSpace(token)
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
			case "4": // Feishu
				fmt.Print("   Enter Feishu app_id: ")
				appID, _ := reader.ReadString('\n')
				appID = strings.TrimSpace(appID)
				fmt.Print("   Enter Feishu app_secret (or API key): ")
				appSecret, _ := reader.ReadString('\n')
				appSecret = strings.TrimSpace(appSecret)
				if appID != "" && appSecret != "" {
					cfg.Gateway.Platforms["feishu"] = config.PlatformConfig{
						AppID:     appID,
						AppSecret: appSecret,
						Enabled:   true,
					}
					fmt.Println("   [Feishu] configured successfully!")
				}
			case "5": // DingTalk
				fmt.Print("   Enter DingTalk app_key: ")
				appKey, _ := reader.ReadString('\n')
				appKey = strings.TrimSpace(appKey)
				fmt.Print("   Enter DingTalk app_secret: ")
				appSecret, _ := reader.ReadString('\n')
				appSecret = strings.TrimSpace(appSecret)
				if appKey != "" && appSecret != "" {
					cfg.Gateway.Platforms["dingtalk"] = config.PlatformConfig{
						AppKey:    appKey,
						AppSecret: appSecret,
						Enabled:   true,
					}
					fmt.Println("   [DingTalk] configured successfully!")
				}
			case "6": // QQ
				fmt.Print("   Enter QQ number: ")
				number, _ := reader.ReadString('\n')
				number = strings.TrimSpace(number)
				fmt.Print("   Enter QQ password: ")
				password, _ := reader.ReadString('\n')
				password = strings.TrimSpace(password)
				if number != "" && password != "" {
					cfg.Gateway.Platforms["qq"] = config.PlatformConfig{
						Number:   number,
						Password: password,
						Enabled:  true,
					}
					fmt.Println("   [QQ] configured successfully!")
				}
			case "7": // WeChat
				fmt.Print("   Enter WeChat API URL: ")
				apiURL, _ := reader.ReadString('\n')
				apiURL = strings.TrimSpace(apiURL)
				fmt.Print("   Enter WeChat token: ")
				token, _ := reader.ReadString('\n')
				token = strings.TrimSpace(token)
				fmt.Print("   Enter WeChat AES key: ")
				aesKey, _ := reader.ReadString('\n')
				aesKey = strings.TrimSpace(aesKey)
				fmt.Print("   Enter WeChat API key (optional): ")
				apiKey, _ := reader.ReadString('\n')
				apiKey = strings.TrimSpace(apiKey)
				if apiURL != "" && token != "" {
					cfg.Gateway.Platforms["wechat"] = config.PlatformConfig{
						APIURL: apiURL,
						Token:  token,
						AESKey: aesKey,
						APIKey: apiKey,
						Enabled: true,
					}
					fmt.Println("   [WeChat] configured successfully!")
				}
			default:
				fmt.Println("   Invalid choice. Please select 0-7.")
			}

			if platformChoice == "0" {
				break
			}
		}
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
