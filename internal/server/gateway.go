package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/gateway"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// GatewayAPIPort is the port of the gateway's embedded API server.
const GatewayAPIPort = 8080

// GatewayHealthPort is the port of the gateway's health check server.
const GatewayHealthPort = 8081

func (s *Server) runAction(id, name string, fn func() (int, error)) {
	s.actionsMu.Lock()
	if prev, ok := s.actions[id]; ok && prev.Running {
		s.actionsMu.Unlock()
		return // action already in progress; do not clobber its state
	}
	s.actions[id] = &ActionStatus{
		Name:      name,
		Running:   true,
		Lines:     []string{},
		StartTime: time.Now(),
	}
	s.actionsMu.Unlock()

	safeGo(func() {
		exitCode, err := fn()

		s.actionsMu.Lock()
		defer s.actionsMu.Unlock()

		if action, ok := s.actions[id]; ok {
			action.Running = false
			action.ExitCode = &exitCode
			now := time.Now()
			action.EndTime = &now
			if err != nil {
				action.Lines = append(action.Lines, fmt.Sprintf("Error: %v", err))
			}
		}
	})
}

// qrProxyPlatforms are the only platforms proxied through the gateway's
// embedded API. Anything else is rejected to avoid path injection.
var qrProxyPlatforms = map[string]bool{"qq": true}

// GatewayAPIPort is the gateway's embedded API port (must match
// gateway.DefaultGatewayConfig / cmd-magic stop checks).

func (s *Server) proxyGatewayQR(w http.ResponseWriter, r *http.Request, platform string) {
	if !qrProxyPlatforms[platform] {
		jsonResponse(w, QRStatus{Platform: platform, Status: "error", Message: "platform not supported for QR proxy"})
		return
	}
	gatewayURL := fmt.Sprintf("http://127.0.0.1:%d/api/login/qr/%s", GatewayAPIPort, platform)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(gatewayURL)
	if err != nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "Gateway is not running. Please start the gateway first.",
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  fmt.Sprintf("Gateway error: %s", string(body)),
		})
		return
	}

	var result struct {
		Platform  string `json:"platform"`
		Status    string `json:"status"`
		QRCode    string `json:"qr_code"`
		Message   string `json:"message"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "Failed to parse gateway response",
		})
		return
	}

	jsonResponse(w, QRStatus{
		Platform:  result.Platform,
		Status:    result.Status,
		QRCode:    result.QRCode,
		Message:   result.Message,
		ExpiresIn: result.ExpiresIn,
	})
}

func (s *Server) handleGatewayRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	actionID := "gateway-restart"
	doneCh := make(chan struct{})

	// Implement restart in-process: stop existing gateway, then start a new one
	// (avoiding the fork/exec permission issues with the restart subcommand)
	s.runAction(actionID, "gateway restart", func() (int, error) {
		defer close(doneCh)
		if err := s.restartGatewayProcess(); err != nil {
			return 1, err
		}
		return 0, nil
	})

	resp := map[string]interface{}{"action": actionID}
	select {
	case <-doneCh:
		status := s.getActionStatus(actionID)
		if status != nil {
			resp["ok"] = status.ExitCode == nil || *status.ExitCode == 0
			if len(status.Lines) > 0 {
				resp["message"] = strings.Join(status.Lines, "\n")
			}
			if status.ExitCode != nil && *status.ExitCode != 0 {
				resp["ok"] = false
			}
		} else {
			resp["ok"] = true
		}
	case <-time.After(30 * time.Second):
		resp["ok"] = false
		resp["message"] = "gateway restart timed out after 30s"
	}

	jsonResponse(w, resp)
}

// restartGatewayProcess 结束当前 gateway 子进程，再用本进程可执行文件重新
// 拉起一个，使 config.json 的最新改动（如 WeCom 扫码确认后写入的新
// bot_id/secret）生效。gateway 只在启动时读取一次平台凭据，因此凭据变更
// 必须走进程级重启。若 gateway 当前未运行，则直接启动一个新实例。
func (s *Server) restartGatewayProcess() error {
	pidFile := filepath.Join(s.magicHome, "gateway.pid")

	// Step 1: Stop the existing gateway (if running)
	if data, err := os.ReadFile(pidFile); err == nil {
		var pidData map[string]interface{}
		if json.Unmarshal(data, &pidData) == nil {
			if pid, ok := pidData["pid"].(float64); ok {
				pid := int(pid)
				if isProcessAlive(pid) {
					// Kill the process group
					pgid := -pid
					if err := syscallKill(pgid, syscallSIGTERM); err == nil {
						// Wait up to 8s for graceful exit
						for i := 0; i < 80; i++ {
							time.Sleep(100 * time.Millisecond)
							if !isProcessAlive(pid) {
								break
							}
						}
					}
					// Force kill if still alive
					if isProcessAlive(pid) {
						syscallKill(pgid, syscallSIGKILL)
						time.Sleep(500 * time.Millisecond)
					}
				}
			}
		}
	}
	os.Remove(pidFile)

	// Step 2: Wait for ports 8080/8081 to be free (up to 3s)
	portsFree := false
	for i := 0; i < 30; i++ {
		if isPort8080Free() && isPort8081Free() {
			portsFree = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Step 2b: If the ports are still held, the old instance was likely not
	// tracked by the PID file (orphan / stale PID). Locate a go-magic process
	// listening on 8080/8081 and terminate it, so the freshly started gateway
	// can bind its API server. Without this the new gateway would still run
	// platform connections (e.g. WeCom) but its 8080 API bind fails.
	if !portsFree {
		for _, port := range []int{8080, 8081} {
			if pid := portOwnerPID(port); pid > 0 {
				log.Warnf("[gateway] Port %d still held by orphan PID %d; terminating it", port, pid)
				_ = syscallKill(pid, syscallSIGKILL)
				time.Sleep(300 * time.Millisecond)
			}
		}
		for i := 0; i < 20; i++ {
			if isPort8080Free() && isPort8081Free() {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Step 3: Start a new gateway in-process
	execPath := s.execPath
	if execPath == "" {
		execPath, _ = os.Executable()
	}

	cmd := exec.Command(execPath, "gateway", "start")
	env := os.Environ()
	goMagicHomeSet := false
	for _, e := range env {
		if strings.HasPrefix(e, "GO_MAGIC_HOME=") {
			goMagicHomeSet = true
			break
		}
	}
	if !goMagicHomeSet && s.magicHome != "" {
		env = append(env, "GO_MAGIC_HOME="+s.magicHome)
	}
	cmd.Env = env

	// Set up log file
	logDir := filepath.Join(s.magicHome, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "gateway.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("gateway start failed: %w", err)
	}

	// Wait for PID file to appear (up to 10s)
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(pidFile); err == nil {
			if logFile != nil {
				logFile.Close()
			}
			return nil
		}
	}

	if logFile != nil {
		logFile.Close()
	}
	return fmt.Errorf("gateway PID file not created after 10s")
}

// registerWeComQRLoginHook 让 WeCom 官方智能机器人扫码确认后自动重启 gateway：
// 新 bot_id/secret 此时已写入 config.json，但 gateway 只在启动时读取一次凭据，
// 不重启则新 bot 永不连接——即用户看到"扫码成功却收不到消息"。重启在后台
// goroutine 执行，不阻塞扫码轮询与 HTTP 响应。
func (s *Server) registerWeComQRLoginHook() {
	gateway.GetQRManager().SetWeComConfirmedHook(func() {
		log.Infof("[gateway] WeCom QR login confirmed — auto-restarting gateway to apply new bot credentials")
		safeGo(func() {
			if err := s.restartGatewayProcess(); err != nil {
				log.Errorf("[gateway] Auto-restart after WeCom QR login failed: %v", err)
			}
		})
	})
}

func (s *Server) handleGatewayStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	actionID := "gateway-stop"
	doneCh := make(chan struct{})

	s.runAction(actionID, "gateway stop", func() (int, error) {
		defer close(doneCh)
		execPath := s.execPath
		if execPath == "" {
			execPath, _ = os.Executable()
		}

		cmd := exec.Command(execPath, "gateway", "stop")
		env := os.Environ()
		goMagicHomeSet := false
		for _, e := range env {
			if strings.HasPrefix(e, "GO_MAGIC_HOME=") {
				goMagicHomeSet = true
				break
			}
		}
		if !goMagicHomeSet && s.magicHome != "" {
			env = append(env, "GO_MAGIC_HOME="+s.magicHome)
		}
		cmd.Env = env

		output, err := cmd.CombinedOutput()
		if err != nil {
			return 1, fmt.Errorf("gateway stop failed: %w\nOutput: %s", err, string(output))
		}
		return 0, nil
	})

	resp := map[string]interface{}{"action": actionID}
	select {
	case <-doneCh:
		status := s.getActionStatus(actionID)
		if status != nil {
			resp["ok"] = status.ExitCode == nil || *status.ExitCode == 0
			if len(status.Lines) > 0 {
				resp["message"] = strings.Join(status.Lines, "\n")
			}
			if status.ExitCode != nil && *status.ExitCode != 0 {
				resp["ok"] = false
			}
		} else {
			resp["ok"] = true
		}
	case <-time.After(30 * time.Second):
		resp["ok"] = false
		resp["message"] = "gateway stop timed out after 30s"
	}

	jsonResponse(w, resp)
}

func (s *Server) handleGatewayQRStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	platform := r.URL.Query().Get("platform")
	if platform == "" {
		http.Error(w, "platform parameter is required", 400)
		return
	}

	if platform == "qq" {
		s.proxyGatewayQR(w, r, platform)
		return
	}

	qrManager := gateway.GetQRManager()
	if qrManager == nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "QR manager not available",
		})
		return
	}

	session := qrManager.GetSession(platform)
	if session == nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "No active QR session",
		})
		return
	}

	expiresIn := int(time.Until(session.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	jsonResponse(w, QRStatus{
		Platform:  session.Platform,
		Status:    session.Status,
		QRCode:    session.QRCode,
		Message:   session.Message,
		ExpiresIn: expiresIn,
	})
}

func (s *Server) handleMagicUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Start magic update as a background action
	actionID := "magic-update"
	s.runAction(actionID, "magic update", func() (int, error) {
		cmd := exec.Command(os.Args[0], "update")
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return 1, fmt.Errorf("magic update failed: %w\nOutput: %s", err, string(output))
		}
		return 0, nil
	})

	jsonResponse(w, map[string]interface{}{"ok": true, "action": actionID})
}

func (s *Server) handleGatewayStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	status := map[string]interface{}{
		"running":    false,
		"pid":        0,
		"health_ok":  false,
		"magic_home": s.magicHome, // debug: show which dir we look in
	}

	pidFile := filepath.Join(s.magicHome, "gateway.pid")
	status["pid_file"] = pidFile

	if data, err := os.ReadFile(pidFile); err != nil {
		status["pid_file_error"] = err.Error()
	} else {
		status["pid_file_content"] = string(data)
		var pidData map[string]interface{}
		if json.Unmarshal(data, &pidData) == nil {
			if pid, ok := pidData["pid"].(float64); ok {
				if isProcessAlive(int(pid)) {
					status["running"] = true
					status["pid"] = int(pid)
					if started, ok := pidData["started"].(string); ok {
						status["started"] = started
					}
				}
			}
		}
	}

	if running, _ := status["running"].(bool); running {
		client := &http.Client{Timeout: 2 * time.Second}
		healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", GatewayHealthPort)
		if resp, err := client.Get(healthURL); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				status["health_ok"] = true
				var health struct {
					Status    string `json:"status"`
					Platforms struct {
						Total   int             `json:"total"`
						Healthy int             `json:"healthy"`
						Detail  map[string]bool `json:"detail"`
					} `json:"platforms"`
					UptimeSeconds float64 `json:"uptime_seconds"`
					Version       string  `json:"version"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
					status["health_status"] = health.Status
					status["gateway_uptime_seconds"] = int64(health.UptimeSeconds)
					status["gateway_version"] = health.Version
					status["platforms_total"] = health.Platforms.Total
					status["platforms_healthy"] = health.Platforms.Healthy
					pl := make([]map[string]interface{}, 0, len(health.Platforms.Detail))
					for name, ok := range health.Platforms.Detail {
						pl = append(pl, map[string]interface{}{"name": name, "connected": ok})
					}
					sort.Slice(pl, func(i, j int) bool { return pl[i]["name"].(string) < pl[j]["name"].(string) })
					status["platforms"] = pl
				}
			}
		}
	}

	jsonResponse(w, status)
}

// generateWeComAIBotQR 走企业微信官方「智能机器人」扫码：请求一个扫码会话，
// 二维码内容为 auth_url（用户用企业微信 App 扫后在 App 内确认），scode 用于
// 轮询 query_result 换取 bot_id/secret（由 QRCodeManager 后台轮询完成）。
func (s *Server) generateWeComAIBotQR() (authURL, qrImage, scode string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authURL, scode, err = gateway.GenerateWeComAIBotQR(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("WeCom AI Bot QR generate: %w", err)
	}

	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}
	return authURL, img, scode, nil
}

func (s *Server) getActionStatus(id string) *ActionStatus {
	s.actionsMu.RLock()
	defer s.actionsMu.RUnlock()
	return s.actions[id]
}

// handleGatewayPlatformAction proxies a runtime platform connect/disconnect to
// the gateway's embedded API (POST /api/platforms/{id}/{action}). This lets the
// web UI cancel an established connection (e.g. take a QR-logged-in bot
// offline) or reconnect it without restarting the whole gateway process.
func (s *Server) handleGatewayPlatformAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	platform := r.PathValue("id")
	action := r.PathValue("action")

	// Strict allowlist to avoid path/header injection into the proxy URL.
	if !isValidPlatformName(platform) {
		http.Error(w, "invalid platform name", 400)
		return
	}
	if action != "connect" && action != "disconnect" {
		http.Error(w, "invalid action (want connect or disconnect)", 400)
		return
	}

	gatewayURL := fmt.Sprintf("http://127.0.0.1:%d/api/platforms/%s/%s", GatewayAPIPort, platform, action)
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, gatewayURL, nil)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "platform": platform, "action": action, "error": err.Error()})
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"ok":       false,
			"platform": platform,
			"action":   action,
			"error":    "Gateway is not running. Please start the gateway first.",
		})
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "platform": platform, "action": action, "error": "failed to parse gateway response"})
		return
	}
	if resp.StatusCode != http.StatusOK {
		result["ok"] = false
	}
	jsonResponse(w, result)
}

func isValidPlatformName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) handleGatewayQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	platform := r.URL.Query().Get("platform")
	if platform == "" {
		http.Error(w, "platform parameter is required", 400)
		return
	}

	if platform == "qq" {
		s.proxyGatewayQR(w, r, platform)
		return
	}

	// Get the global QR manager
	qrManager := gateway.GetQRManager()
	if qrManager == nil {
		jsonResponse(w, QRStatus{
			Platform: platform,
			Status:   "error",
			Message:  "QR manager not available",
		})
		return
	}

	// Check for existing valid session
	session := qrManager.GetSession(platform)
	if session == nil || session.Status == "expired" || session.Status == "confirmed" || session.Status == "error" {
		// Generate new QR code based on platform
		var qrData string
		var qrImage string
		var err error
		wecomScode := ""

		// 支持的扫码平台白名单：dingtalk/feishu 是企业自建应用（app 凭据 + webhook），
		// 官方无个人扫码通道；旧 OAuth stub 的 redirect_uri 写死 localhost 不可达，
		// 已于 2026-09 删除（前端 supportsQR 同步关闭）。新增平台需在 QRCodeManager
		// pollPlatformStatus 中提供 confirmed 落库逻辑，否则轮询永 pending。
		switch platform {
		case "wechat_ilink":
			qrData, qrImage, err = s.generateWeChatILinkQR()
		case "wecom":
			// WeCom 走官方智能机器人扫码（含 scode 供后台轮询）
			qrData, qrImage, wecomScode, err = s.generateWeComAIBotQR()
		default:
			jsonResponse(w, QRStatus{
				Platform: platform,
				Status:   "error",
				Message:  fmt.Sprintf("QR login not supported for %s", platform),
			})
			return
		}

		if err != nil {
			jsonResponse(w, QRStatus{
				Platform: platform,
				Status:   "error",
				Message:  fmt.Sprintf("Failed to generate QR code: %v", err),
			})
			return
		}

		if wecomScode != "" {
			session, err = qrManager.CreateSessionWithMeta(platform, qrData, qrImage,
				map[string]string{"scode": wecomScode})
		} else {
			session, err = qrManager.CreateSession(platform, qrData, qrImage)
		}
		if err != nil {
			jsonResponse(w, QRStatus{
				Platform: platform,
				Status:   "error",
				Message:  fmt.Sprintf("Failed to create session: %v", err),
			})
			return
		}
		fmt.Printf("[QR] Created session for %s: qr_code_len=%d\n", platform, len(session.QRCode))
	}

	// Return current session state
	expiresIn := int(time.Until(session.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	jsonResponse(w, QRStatus{
		Platform:  session.Platform,
		Status:    session.Status,
		QRCode:    session.QRCode,
		Message:   session.Message,
		ExpiresIn: expiresIn,
	})
}

func (s *Server) generateWeChatILinkQR() (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create iLink API client
	api, err := gateway.NewILinkAPIClient("https://ilinkai.weixin.qq.com/", "", "")
	if err != nil {
		return "", "", fmt.Errorf("failed to create iLink API client: %w", err)
	}

	// Get QR code from iLink API
	qrResp, err := api.GetQRCode(ctx, "3")
	if err != nil {
		return "", "", fmt.Errorf("failed to get QR code: %w", err)
	}

	// qrData is the key for status polling (32-char hex string)
	qrData := qrResp.Qrcode

	fmt.Printf("[QR] iLink response: qrcode_len=%d, img_url=%s\n", len(qrData), qrResp.QrcodeImgContent)

	// The img_url is a webpage, not a direct image.
	// We need to generate QR code from the URL itself so users can scan it
	var qrImage string
	if qrResp.QrcodeImgContent != "" {
		// Generate QR code containing the URL (users scan this to open the page)
		img, err := gateway.GenerateQRCodePNG(qrResp.QrcodeImgContent)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate QR image from URL: %w", err)
		}
		qrImage = img
		fmt.Printf("[QR] Generated QR from URL, len=%d\n", len(qrImage))
	} else if qrData != "" {
		// Fallback: generate from qrData
		img, err := gateway.GenerateQRCodePNG(qrData)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate QR image: %w", err)
		}
		qrImage = img
		fmt.Printf("[QR] Generated image from qrData, len=%d\n", len(qrImage))
	}

	if qrImage == "" {
		return "", "", fmt.Errorf("no QR image available")
	}

	return qrData, qrImage, nil
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/actions/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.Error(w, "not found", 404)
		return
	}
	actionName := parts[0]
	subPath := parts[1]

	if subPath == "status" {
		status := s.getActionStatus(actionName)
		if status == nil {
			// Return empty status if action not found
			jsonResponse(w, map[string]interface{}{
				"exit_code": nil,
				"lines":     []string{},
				"name":      actionName,
				"pid":       nil,
				"running":   false,
				"status":    "unknown",
				"message":   "",
			})
			return
		}

		jsonResponse(w, map[string]interface{}{
			"exit_code": status.ExitCode,
			"lines":     status.Lines,
			"name":      status.Name,
			"pid":       nil,
			"running":   status.Running,
			"status":    map[bool]string{true: "running", false: "completed"}[status.Running],
			"message":   "",
		})
		return
	}
	http.Error(w, "not found", 404)
}

func (s *Server) generateUserMD(data map[string]interface{}) string {
	var lines []string
	lines = append(lines, "# User Profile")
	lines = append(lines, "")
	lines = append(lines, "## About")

	name := "[Not set]"
	if v, ok := data["name"].(string); ok && v != "" {
		name = v
	}
	lines = append(lines, "- Name: "+name)

	role := "[Not set]"
	if v, ok := data["role"].(string); ok && v != "" {
		role = v
	}
	lines = append(lines, "- Role: "+role)

	lines = append(lines, "")
	lines = append(lines, "## Preferences")

	commStyle := "[Not set]"
	if v, ok := data["communication_style"].(string); ok && v != "" {
		commStyle = v
	}
	lines = append(lines, "- Communication style: "+commStyle)

	codeStyle := "[Not set]"
	if v, ok := data["code_style"].(string); ok && v != "" {
		codeStyle = v
	}
	lines = append(lines, "- Code style: "+codeStyle)

	lines = append(lines, "")
	lines = append(lines, "## Tech Stack")

	if stack, ok := data["tech_stack"].([]interface{}); ok && len(stack) > 0 {
		for _, tech := range stack {
			if t, ok := tech.(string); ok {
				lines = append(lines, "- "+t)
			}
		}
	} else {
		lines = append(lines, "- Languages: [Not set]")
		lines = append(lines, "- Frameworks: [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Interests")

	if interests, ok := data["interests"].([]interface{}); ok && len(interests) > 0 {
		for _, interest := range interests {
			if i, ok := interest.(string); ok {
				lines = append(lines, "- "+i)
			}
		}
	} else {
		lines = append(lines, "- [Not set]")
	}

	lines = append(lines, "")
	lines = append(lines, "## Notes")
	lines = append(lines, "[Auto-managed by go-magic]")

	return strings.Join(lines, "\n")
}

func (s *Server) handleGatewayStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	actionID := "gateway-start"
	doneCh := make(chan struct{})

	s.runAction(actionID, "gateway start", func() (int, error) {
		defer close(doneCh)
		execPath := s.execPath
		if execPath == "" {
			execPath, _ = os.Executable()
		}

		cmd := exec.Command(execPath, "gateway", "start")
		env := os.Environ()
		goMagicHomeSet := false
		for _, e := range env {
			if strings.HasPrefix(e, "GO_MAGIC_HOME=") {
				goMagicHomeSet = true
				break
			}
		}
		if !goMagicHomeSet && s.magicHome != "" {
			env = append(env, "GO_MAGIC_HOME="+s.magicHome)
		}
		cmd.Env = env

		output, err := cmd.CombinedOutput()
		if err != nil {
			return 1, fmt.Errorf("gateway start failed: %w\nOutput: %s", err, string(output))
		}
		return 0, nil
	})

	resp := map[string]interface{}{"action": actionID}
	select {
	case <-doneCh:
		status := s.getActionStatus(actionID)
		if status != nil {
			resp["ok"] = status.ExitCode == nil || *status.ExitCode == 0
			if len(status.Lines) > 0 {
				resp["message"] = strings.Join(status.Lines, "\n")
			}
			if status.ExitCode != nil && *status.ExitCode != 0 {
				resp["ok"] = false
			}
		} else {
			resp["ok"] = true
		}
	case <-time.After(30 * time.Second):
		resp["ok"] = false
		resp["message"] = "gateway start timed out after 30s"
	}

	jsonResponse(w, resp)
}
