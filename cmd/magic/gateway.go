package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/gateway"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

const pidFileName = "gateway.pid"

var gatewayPlatform string // --platform 参数

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the messaging gateway (with health check on :8080)",
	Long:  "Start the messaging gateway for Telegram, Discord, WeCom, etc.\nHealth check endpoint available at http://localhost:8080/health",
}

var gatewayStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the gateway",
	Run:   runGatewayStart,
}

var gatewayStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the gateway",
	Run:   runGatewayStop,
}

var gatewayStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check gateway status",
	Run:   runGatewayStatus,
}

func init() {
	// Note: 'p' shorthand is already used by root's --profile persistent flag.
	// We use 'P' (uppercase) for --platform.
	gatewayStartCmd.Flags().StringVarP(&gatewayPlatform, "platform", "P", "",
		"Start only this platform (e.g., wechat_ilink, telegram)")

	rootCmd.AddCommand(gatewayCmd)
}

// shouldStartPlatform checks if a platform should be started based on --platform flag.
// If --platform is empty, all enabled platforms start.
// If --platform is set, only the matching platform starts.
func shouldStartPlatform(name string) bool {
	if gatewayPlatform == "" {
		return true
	}
	return gatewayPlatform == name
}

// gatewayAgentHandler implements gateway.AgentHandler to bridge gateway messages
// to the magic AI agent for processing and response.
type gatewayAgentHandler struct {
	provider provider.Provider
	registry *tool.Registry
	mu       sync.Mutex

	// Per-user agents for conversation context
	agents    map[string]*agent.Agent
	systemPrompt string
}

// NewGatewayAgentHandler creates a new gateway agent handler with the configured provider.
func NewGatewayAgentHandler() *gatewayAgentHandler {
	cfg, err := config.Load()
	if err != nil {
		log.Warnf("[Gateway] Failed to load config for agent handler: %v", err)
		return &gatewayAgentHandler{
			agents: make(map[string]*agent.Agent),
		}
	}

	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		log.Warnf("[Gateway] Provider %s not configured", cfg.Provider)
		return &gatewayAgentHandler{
			agents: make(map[string]*agent.Agent),
		}
	}

	prov := createProvider(cfg.Provider, provCfg)
	registry := tool.NewRegistry()

	// Get workDir with fallback to current directory
	workDir := cfg.WorkingDir
	if workDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workDir = cwd
		}
	}
	registry.RegisterAll(workDir)

	// Generate system prompt
	systemPrompt := generateGatewaySystemPrompt(cfg)

	return &gatewayAgentHandler{
		provider:     prov,
		registry:     registry,
		agents:       make(map[string]*agent.Agent),
		systemPrompt: systemPrompt,
	}
}

// generateGatewaySystemPrompt creates a system prompt for gateway agent
func generateGatewaySystemPrompt(cfg *config.Config) string {
	basePrompt := `You are a helpful AI assistant responding via a chat platform.

Guidelines:
- Be concise and friendly in responses
- If asked to perform actions or use tools, use the available tools to help the user
- Keep responses appropriate for chat format
- Remember context within the conversation for this user`

	// Check for custom system prompt in config (if field exists in future)
	// For now, use the base prompt
	return basePrompt
}

// getOrCreateAgent gets or creates an agent for a specific user
func (h *gatewayAgentHandler) getOrCreateAgent(userID string) (*agent.Agent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ag, ok := h.agents[userID]; ok {
		return ag, nil
	}

	if h.provider == nil {
		return nil, fmt.Errorf("provider not configured")
	}

	// Generate tools schema
	toolsSchema := getToolsSchema(h.registry)

	// Create new agent for this user
	newAgent := agent.NewAIAgent(h.provider, h.registry, toolsSchema, h.systemPrompt)
	newAgent.SetSession(userID)

	h.agents[userID] = newAgent
	return newAgent, nil
}

// Process processes a message from a gateway platform and returns a response.
func (h *gatewayAgentHandler) Process(ctx context.Context, msg gateway.Message) (string, error) {
	if h.provider == nil {
		// Lazy init
		h.mu.Lock()
		if h.provider == nil {
			cfg, err := config.Load()
			if err == nil {
				provCfg, ok := cfg.Providers[cfg.Provider]
				if ok {
					h.provider = createProvider(cfg.Provider, provCfg)

					// Get workDir with fallback
					workDir := cfg.WorkingDir
					if workDir == "" {
						if cwd, err := os.Getwd(); err == nil {
							workDir = cwd
						}
					}

					h.registry = tool.NewRegistry()
					h.registry.RegisterAll(workDir)
					h.systemPrompt = generateGatewaySystemPrompt(cfg)
				}
			}
		}
		h.mu.Unlock()
	}

	if h.provider == nil {
		return fmt.Sprintf("Hello! I received your message but the AI provider is not configured.\n"+
			"Please run 'magic setup' to configure an AI provider.\n\n"+
			"Message: %s", msg.Content), nil
	}

	// Format message with platform context
	prompt := fmt.Sprintf("[Message from %s] %s", msg.Platform, msg.Content)

	// Get or create agent for this user
	ag, err := h.getOrCreateAgent(msg.UserID)
	if err != nil {
		return "", fmt.Errorf("failed to get agent: %w", err)
	}

	// Run conversation with full agent capabilities
	response, err := ag.RunConversation(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("AI processing failed: %w", err)
	}

	return response, nil
}

// ResetSession resets a user's session (clears conversation history).
func (h *gatewayAgentHandler) ResetSession(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ag, ok := h.agents[userID]; ok {
		ag.Reset()
	}
	delete(h.agents, userID)
	log.Infof("[Gateway] Session reset for user: %s", userID)
}

func runGatewayStart(cmd *cobra.Command, args []string) {
	if gatewayPlatform != "" {
		fmt.Printf("🔧 Platform filter: only starting '%s'\n", gatewayPlatform)
		fmt.Printf("   (omit --platform to start all enabled platforms)\n\n")
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if !cfg.Gateway.Enabled {
		fmt.Println("Gateway is not enabled in config.")
		fmt.Println("Please run 'magic setup' or edit ~/.magic/config.json")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startHealthServer(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down gateway...")
		cancel()
	}()

	platformCount := 0
	agentHandler := &gatewayAgentHandler{}
	gw := gateway.NewGateway(agentHandler, &gateway.GatewayConfig{})

	// Start Telegram if configured
	if tgCfg, ok := cfg.Gateway.Platforms["telegram"]; ok && tgCfg.Enabled && shouldStartPlatform("telegram") {
		platformCount++
		if tgCfg.Token == "" {
			fmt.Println("[Telegram] Token not configured!")
		} else {
			fmt.Println("[Telegram] Starting...")
			tgGw, err := gateway.NewTelegramHandler(tgCfg.Token, nil)
			if err != nil {
				fmt.Printf("[Telegram] Failed: %v\n", err)
			} else {
				gw.RegisterPlatform("telegram", tgGw)
			}
		}
	}

	// Start Discord if configured
	if dcCfg, ok := cfg.Gateway.Platforms["discord"]; ok && dcCfg.Enabled && shouldStartPlatform("discord") {
		platformCount++
		if dcCfg.Token == "" {
			fmt.Println("[Discord] Token not configured!")
		} else {
			fmt.Println("[Discord] Starting...")
			dgw, err := gateway.NewDiscordGateway(dcCfg.Token)
			if err != nil {
				fmt.Printf("[Discord] Failed: %v\n", err)
			} else {
				if err := dgw.Connect(ctx); err != nil {
					fmt.Printf("[Discord] Failed to connect: %v\n", err)
				} else {
					gw.RegisterPlatform("discord", dgw)
				}
			}
		}
	}

	// Start WeCom if configured
	if wcCfg, ok := cfg.Gateway.Platforms["wecom"]; ok && wcCfg.Enabled && shouldStartPlatform("wecom") {
		platformCount++
		if wcCfg.CorpID == "" || wcCfg.Secret == "" {
			fmt.Println("[WeCom] Config incomplete (need corp_id and secret)!")
		} else {
			fmt.Println("[WeCom] Starting...")
			wcgw := gateway.NewWeComGateway(wcCfg.CorpID, wcCfg.AgentID, wcCfg.Secret)
			if err := wcgw.Connect(ctx); err != nil {
				fmt.Printf("[WeCom] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wecom", wcgw)
			}
		}
	}

	// Start QQ if configured
	if qqCfg, ok := cfg.Gateway.Platforms["qq"]; ok && qqCfg.Enabled && shouldStartPlatform("qq") {
		platformCount++
		if qqCfg.Number == "" && qqCfg.AppID == "" {
			fmt.Println("[QQ] Config incomplete (need app_id/number and app_secret)!")
		} else {
			fmt.Println("[QQ] Starting...")
			appID := qqCfg.AppID
			if appID == "" {
				appID = qqCfg.Number
			}
			appSecret := qqCfg.AppSecret
			if appSecret == "" {
				appSecret = qqCfg.Password
			}
			qqGw := gateway.NewQQGateway(appID, appSecret)
			if err := qqGw.Connect(ctx); err != nil {
				fmt.Printf("[QQ] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("qq", qqGw)
			}
		}
	}

	// Start DingTalk if configured
	if dtCfg, ok := cfg.Gateway.Platforms["dingtalk"]; ok && dtCfg.Enabled && shouldStartPlatform("dingtalk") {
		platformCount++
		if dtCfg.AppKey == "" || dtCfg.AppSecret == "" {
			fmt.Println("[DingTalk] Config incomplete (need app_key and app_secret)!")
		} else {
			fmt.Println("[DingTalk] Starting...")
			dtGw := gateway.NewDingTalkGateway(dtCfg.AppKey, dtCfg.AppSecret)
			if dtCfg.AgentID != "" {
				dtGw.SetAgentID(dtCfg.AgentID)
			}
			if err := dtGw.Connect(ctx); err != nil {
				fmt.Printf("[DingTalk] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("dingtalk", dtGw)
			}
		}
	}

	// Start Feishu/Lark if configured
	if fsCfg, ok := cfg.Gateway.Platforms["feishu"]; ok && fsCfg.Enabled && shouldStartPlatform("feishu") {
		platformCount++
		if fsCfg.AppID == "" || fsCfg.AppSecret == "" {
			fmt.Println("[Feishu] Config incomplete (need app_id and app_secret)!")
		} else {
			fmt.Println("[Feishu/Lark] Starting...")
			fsGw := gateway.NewFeishuGateway(fsCfg.AppID, fsCfg.AppSecret)
			if err := fsGw.Connect(ctx); err != nil {
				fmt.Printf("[Feishu] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("feishu", fsGw)
			}
		}
	}

	// Start WeChat (Official Account / Mini Program) if configured
	if wxCfg, ok := cfg.Gateway.Platforms["wechat"]; ok && wxCfg.Enabled && shouldStartPlatform("wechat") {
		platformCount++
		if wxCfg.AppID == "" || wxCfg.AppSecret == "" {
			fmt.Println("[WeChat] Config incomplete (need app_id and app_secret)!")
			fmt.Println("[WeChat] For personal WeChat account, use 'wechat_ilink' platform instead.")
		} else {
			fmt.Println("[WeChat] Starting...")
			wxGw := gateway.NewWeChatGateway(wxCfg.AppID, wxCfg.AppSecret, wxCfg.Token, wxCfg.AESKey)
			if err := wxGw.Connect(ctx); err != nil {
				fmt.Printf("[WeChat] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wechat", wxGw)
			}
		}
	}

	// Start Slack if configured
	if slackCfg, ok := cfg.Gateway.Platforms["slack"]; ok && slackCfg.Enabled && shouldStartPlatform("slack") {
		platformCount++
		if slackCfg.Token == "" || slackCfg.AppSecret == "" {
			fmt.Println("[Slack] Config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[Slack] Starting...")
			slackGw := gateway.NewSlackGateway(slackCfg.Token, slackCfg.AppSecret)
			if err := slackGw.Connect(ctx); err != nil {
				fmt.Printf("[Slack] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("slack", slackGw)
			}
		}
	}

	// Start WhatsApp if configured
	if waCfg, ok := cfg.Gateway.Platforms["whatsapp"]; ok && waCfg.Enabled && shouldStartPlatform("whatsapp") {
		platformCount++
		if waCfg.Token == "" || waCfg.AppSecret == "" {
			fmt.Println("[WhatsApp] Config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[WhatsApp] Starting...")
			waGw := gateway.NewWhatsAppGateway(waCfg.AppID, waCfg.Token, waCfg.AppSecret, waCfg.VerifyToken)
			if err := waGw.Connect(ctx); err != nil {
				fmt.Printf("[WhatsApp] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("whatsapp", waGw)
			}
		}
	}

	// Start LINE if configured
	if lineCfg, ok := cfg.Gateway.Platforms["line"]; ok && lineCfg.Enabled && shouldStartPlatform("line") {
		platformCount++
		if lineCfg.Token == "" || lineCfg.AppSecret == "" {
			fmt.Println("[LINE] Config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[LINE] Starting...")
			lineGw := gateway.NewLineGateway(lineCfg.AppSecret, lineCfg.Token)
			if err := lineGw.Connect(ctx); err != nil {
				fmt.Printf("[LINE] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("line", lineGw)
			}
		}
	}

	// Start Matrix if configured
	if matrixCfg, ok := cfg.Gateway.Platforms["matrix"]; ok && matrixCfg.Enabled && shouldStartPlatform("matrix") {
		platformCount++
		if matrixCfg.Token == "" {
			fmt.Println("[Matrix] Config incomplete (need token/homeserver)!")
		} else {
			fmt.Println("[Matrix] Starting...")
			matrixGw := gateway.NewMatrixGateway(matrixCfg.APIURL, matrixCfg.AppID, matrixCfg.Token)
			if err := matrixGw.Connect(ctx); err != nil {
				fmt.Printf("[Matrix] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("matrix", matrixGw)
			}
		}
	}

	// Start WeChat iLink (Personal WeChat via iLink Bot API) if configured
	if ilinkCfg, ok := cfg.Gateway.Platforms["wechat_ilink"]; ok && ilinkCfg.Enabled && shouldStartPlatform("wechat_ilink") {
		platformCount++
		fmt.Println("[WeChat-iLink] Starting...")

		dataDir := ilinkCfg.DataDir
		if dataDir == "" {
			homeDir, _ := os.UserHomeDir()
			dataDir = filepath.Join(homeDir, ".magic", "wechat_ilink")
		}

		baseURL := ilinkCfg.APIURL
		if baseURL == "" {
			baseURL = "https://ilinkai.weixin.qq.com"
		}
		ilinkGw := gateway.NewWeChatILinkGateway(gateway.WeChatILinkConfig{
			Token:     ilinkCfg.Token,
			DataDir:   dataDir,
			BaseURL:   baseURL,
			AutoLogin: ilinkCfg.AutoLogin,
		})

		if err := ilinkGw.Connect(ctx); err != nil {
			fmt.Printf("[WeChat-iLink] Failed to connect: %v\n", err)
		} else {
			gw.RegisterPlatform("wechat_ilink", ilinkGw)
		}
	}

	// Also support the old "wechat_clawbot" config name for backward compatibility
	if clawCfg, ok := cfg.Gateway.Platforms["wechat_clawbot"]; ok && clawCfg.Enabled &&
		(shouldStartPlatform("wechat_clawbot") || shouldStartPlatform("wechat_ilink")) {
		if _, already := cfg.Gateway.Platforms["wechat_ilink"]; !already {
			platformCount++
			fmt.Println("[WeChat-ClawBot] Starting (using iLink API)...")

			dataDir := clawCfg.DataDir
			if dataDir == "" {
				homeDir, _ := os.UserHomeDir()
				dataDir = filepath.Join(homeDir, ".magic", "clawbot")
			}

			baseURL := clawCfg.APIURL
			if baseURL == "" {
				baseURL = "https://ilinkai.weixin.qq.com"
			}

			clawGw := gateway.NewWeChatILinkGateway(gateway.WeChatILinkConfig{
				DataDir:   dataDir,
				BaseURL:   baseURL,
				AutoLogin: clawCfg.AutoLogin,
			})

			if err := clawGw.Connect(ctx); err != nil {
				fmt.Printf("[WeChat-ClawBot] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wechat_clawbot", clawGw)
			}
		}
	}

	if platformCount == 0 {
		if gatewayPlatform != "" {
			fmt.Printf("Platform '%s' is not configured or not enabled.\n", gatewayPlatform)
			fmt.Println("Run 'magic gateway status' to see configured platforms.")
		} else {
			fmt.Println("No platforms configured/enabled.")
			fmt.Println("Configure in ~/.magic/config.json")
			fmt.Println("Supported: telegram, discord, wecom, qq, dingtalk, feishu, wechat, slack, whatsapp, line, matrix, wechat_ilink, wechat_clawbot")
		}
		return
	}

	if err := gw.Start(ctx); err != nil {
		fmt.Printf("Failed to start gateway: %v\n", err)
		os.Exit(1)
	}

	// Save PID file
	home, _ := os.UserHomeDir()
	pidDir := filepath.Join(home, ".magic")
	os.MkdirAll(pidDir, 0755)
	pidFile := filepath.Join(pidDir, pidFileName)
	pidData := map[string]interface{}{
		"pid":     os.Getpid(),
		"started": time.Now().Format(time.RFC3339),
	}
	pidBytes, _ := json.MarshalIndent(pidData, "", "  ")
	os.WriteFile(pidFile, pidBytes, 0644)

	fmt.Printf("\nStarted %d platform(s). Press Ctrl+C to stop.\n", platformCount)
	fmt.Printf("PID saved: %s\n", pidFile)

	<-ctx.Done()
	os.Remove(pidFile)
}

func runGatewayStop(cmd *cobra.Command, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get home directory: %v\n", err)
		os.Exit(1)
	}

	pidFile := filepath.Join(home, ".magic", pidFileName)

	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Gateway is not running (no PID file found).")
			return
		}
		fmt.Printf("Failed to read PID file: %v\n", err)
		os.Exit(1)
	}

	var pidData map[string]interface{}
	if err := json.Unmarshal(data, &pidData); err != nil {
		fmt.Printf("Failed to parse PID file: %v\n", err)
		os.Exit(1)
	}

	pid, ok := pidData["pid"].(float64)
	if !ok {
		fmt.Println("Invalid PID file format.")
		os.Exit(1)
	}

	process, err := os.FindProcess(int(pid))
	if err != nil {
		fmt.Printf("Failed to find process: %v\n", err)
		os.Exit(1)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("Failed to stop gateway: %v\n", err)
		fmt.Println("Try killing the process manually: kill", int(pid))
		os.Exit(1)
	}

	fmt.Printf("Sent stop signal to gateway (PID: %d)...\n", int(pid))
	time.Sleep(2 * time.Second)

	if process.Pid == 0 {
	} else {
		process.Kill()
		fmt.Println("Process forcefully killed.")
	}

	os.Remove(pidFile)
	fmt.Println("✓ Gateway stopped.")
}

func runGatewayStatus(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}

	fmt.Println("Gateway Status")
	fmt.Println("==============")
	fmt.Printf("Enabled in config: %v\n", cfg.Gateway.Enabled)

	home, _ := os.UserHomeDir()
	pidFile := filepath.Join(home, ".magic", pidFileName)

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Println("\n● Gateway: NOT RUNNING")
	} else {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			var pidData map[string]interface{}
			if json.Unmarshal(data, &pidData) == nil {
				if pid, ok := pidData["pid"].(float64); ok {
					process, err := os.FindProcess(int(pid))
					if err == nil && process.Pid != 0 {
						fmt.Printf("\n● Gateway: RUNNING (PID: %d)\n", int(pid))
						if started, ok := pidData["started"].(string); ok {
							fmt.Printf("  Started: %s\n", started)
						}
					} else {
						fmt.Println("\n● Gateway: NOT RUNNING (stale PID file)")
					}
				}
			}
		}

		client := &http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Get("http://localhost:8080/health"); err == nil {
			resp.Body.Close()
			fmt.Println("● Health endpoint: REACHABLE")
		} else {
			fmt.Println("○ Health endpoint: NOT REACHABLE")
		}
	}

	if len(cfg.Gateway.Platforms) == 0 {
		fmt.Println("\nNo platforms configured.")
	} else {
		fmt.Println("\nConfigured Platforms:")
		for name, plat := range cfg.Gateway.Platforms {
			status := "○ disabled"
			if plat.Enabled {
				status = "● enabled"
			}
			fmt.Printf("  %s: %s\n", name, status)
		}
	}
}
