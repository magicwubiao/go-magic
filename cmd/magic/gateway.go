package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/gateway"
	"github.com/magicwubiao/go-magic/pkg/config"
)

const pidFileName = "gateway.pid"

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
	gatewayCmd.AddCommand(gatewayStartCmd)
	gatewayCmd.AddCommand(gatewayStopCmd)
	gatewayCmd.AddCommand(gatewayStatusCmd)
	rootCmd.AddCommand(gatewayCmd)
}

func runGatewayStart(cmd *cobra.Command, args []string) {
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

	// Start health check server
	go startHealthServer(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down gateway...")
		cancel()
	}()

	platformCount := 0

	// Start Telegram if configured
	if tgCfg, ok := cfg.Gateway.Platforms["telegram"]; ok && tgCfg.Enabled {
		platformCount++
		if tgCfg.Token == "" {
			fmt.Println("Telegram token not configured!")
		} else {
			fmt.Println("[Telegram] Starting...")
			gw, err := gateway.NewGateway(tgCfg.Token)
			if err != nil {
				fmt.Printf("[Telegram] Failed: %v\n", err)
			} else {
				go gw.Start(ctx)
			}
		}
	}

	// Start Discord if configured
	if dcCfg, ok := cfg.Gateway.Platforms["discord"]; ok && dcCfg.Enabled {
		platformCount++
		if dcCfg.Token == "" {
			fmt.Println("Discord token not configured!")
		} else {
			fmt.Println("[Discord] Starting...")
			dgw, err := gateway.NewDiscordGateway(dcCfg.Token)
			if err != nil {
				fmt.Printf("[Discord] Failed: %v\n", err)
			} else {
				go dgw.Start(ctx)
			}
		}
	}

	// Start WeCom if configured
	if wcCfg, ok := cfg.Gateway.Platforms["wecom"]; ok && wcCfg.Enabled {
		platformCount++
		if wcCfg.CorpID == "" || wcCfg.Secret == "" {
			fmt.Println("WeCom config incomplete (need corp_id and secret)!")
		} else {
			fmt.Println("[WeCom] Starting...")
			wcgw := gateway.NewWeComGateway(wcCfg.CorpID, wcCfg.AgentID, wcCfg.Secret)
			go wcgw.Start(ctx)
		}
	}

	// Start QQ if configured
	if qqCfg, ok := cfg.Gateway.Platforms["qq"]; ok && qqCfg.Enabled {
		platformCount++
		fmt.Println("[QQ] Starting...")
		fmt.Println("  Note: QQ gateway is a framework.")
		fmt.Println("  Install a library like MiraiGo or cq-http first.")
		qqGw := gateway.NewQQGateway(qqCfg.Number, qqCfg.Password)
		go qqGw.Start(ctx)
	}

	// Start DingTalk if configured
	if dtCfg, ok := cfg.Gateway.Platforms["dingtalk"]; ok && dtCfg.Enabled {
		platformCount++
		if dtCfg.AppKey == "" || dtCfg.AppSecret == "" {
			fmt.Println("DingTalk config incomplete (need app_key and app_secret)!")
		} else {
			fmt.Println("[DingTalk] Starting...")
			dtGw := gateway.NewDingTalkGateway(dtCfg.AppKey, dtCfg.AppSecret)
			go dtGw.Start(ctx)
		}
	}

	// Start Feishu/Lark if configured
	if fsCfg, ok := cfg.Gateway.Platforms["feishu"]; ok && fsCfg.Enabled {
		platformCount++
		if fsCfg.AppID == "" || fsCfg.AppSecret == "" {
			fmt.Println("Feishu config incomplete (need app_id and app_secret)!")
		} else {
			fmt.Println("[Feishu/Lark] Starting...")
			fsGw := gateway.NewFeishuGateway(fsCfg.AppID, fsCfg.AppSecret)
			go fsGw.Start(ctx)
		}
	}

	// Start WeChat (clawbot) if configured
	if wcCfg, ok := cfg.Gateway.Platforms["wechat"]; ok && wcCfg.Enabled {
		platformCount++
		if wcCfg.APIURL == "" {
			fmt.Println("WeChat config incomplete (need api_url)!")
		} else {
			fmt.Println("[WeChat] Starting...")
			wcGw := gateway.NewWeChatGateway(wcCfg.APIURL, wcCfg.APIKey)
			go wcGw.Start(ctx)
		}
	}

	// Start Slack if configured
	if slackCfg, ok := cfg.Gateway.Platforms["slack"]; ok && slackCfg.Enabled {
		platformCount++
		if slackCfg.Token == "" || slackCfg.AppSecret == "" {
			fmt.Println("Slack config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[Slack] Starting...")
			slackGw := gateway.NewSlackGateway(slackCfg.Token, slackCfg.AppSecret)
			go slackGw.Start(ctx)
		}
	}

	// Start WhatsApp if configured
	if waCfg, ok := cfg.Gateway.Platforms["whatsapp"]; ok && waCfg.Enabled {
		platformCount++
		if waCfg.Token == "" || waCfg.AppSecret == "" {
			fmt.Println("WhatsApp config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[WhatsApp] Starting...")
			waGw := gateway.NewWhatsAppGateway(waCfg.AppID, waCfg.Token, waCfg.AppSecret, waCfg.VerifyToken)
			go waGw.Start(ctx)
		}
	}

	// Start LINE if configured
	if lineCfg, ok := cfg.Gateway.Platforms["line"]; ok && lineCfg.Enabled {
		platformCount++
		if lineCfg.Token == "" || lineCfg.AppSecret == "" {
			fmt.Println("LINE config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[LINE] Starting...")
			lineGw := gateway.NewLineGateway(lineCfg.AppSecret, lineCfg.Token)
			go lineGw.Start(ctx)
		}
	}

	// Start Matrix if configured
	if matrixCfg, ok := cfg.Gateway.Platforms["matrix"]; ok && matrixCfg.Enabled {
		platformCount++
		if matrixCfg.Token == "" {
			fmt.Println("Matrix config incomplete (need token/homeserver)!")
		} else {
			fmt.Println("[Matrix] Starting...")
			matrixGw := gateway.NewMatrixGateway(matrixCfg.APIURL, matrixCfg.AppID, matrixCfg.Token)
			go matrixGw.Start(ctx)
		}
	}

	if platformCount == 0 {
		fmt.Println("No platforms configured/enabled.")
		fmt.Println("Configure in ~/.magic/config.json")
		fmt.Println("Supported: telegram, discord, wecom, qq, dingtalk, feishu, wechat, slack, whatsapp, line, matrix")
		return
	}

	// Save PID file for stop command
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

	// Wait for signal
	<-ctx.Done()

	// Clean up PID file
	os.Remove(pidFile)
}

func runGatewayStop(cmd *cobra.Command, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get home directory: %v\n", err)
		os.Exit(1)
	}

	pidFile := filepath.Join(home, ".magic", pidFileName)

	// Check if PID file exists
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

	// Try to send signal to process
	process, err := os.FindProcess(int(pid))
	if err != nil {
		fmt.Printf("Failed to find process: %v\n", err)
		os.Exit(1)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("Failed to stop gateway: %v\n", err)
		fmt.Println("Try killing the process manually: kill", int(pid))
		os.Exit(1)
	}

	// Wait for process to terminate
	fmt.Printf("Sent stop signal to gateway (PID: %d)...\n", int(pid))
	time.Sleep(2 * time.Second)

	// Check if process is still running
	if process.Pid == 0 {
		// Process may have already exited
	} else {
		process.Kill()
		fmt.Println("Process forcefully killed.")
	}

	// Clean up PID file
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

	// Check if gateway is actually running
	home, _ := os.UserHomeDir()
	pidFile := filepath.Join(home, ".magic", pidFileName)

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Println("\n● Gateway: NOT RUNNING")
	} else {
		// Read PID file
		data, err := os.ReadFile(pidFile)
		if err == nil {
			var pidData map[string]interface{}
			if json.Unmarshal(data, &pidData) == nil {
				if pid, ok := pidData["pid"].(float64); ok {
					process, err := os.FindProcess(int(pid))
					if err == nil && process.Pid != 0 {
						// Try to check if process is responsive
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

		// Try to query health endpoint
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
