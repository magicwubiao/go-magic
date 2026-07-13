package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/gateway"
)

func (s *Server) runAction(id, name string, fn func() (int, error)) {
	s.actionsMu.Lock()
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

func (s *Server) proxyGatewayQR(w http.ResponseWriter, r *http.Request, platform string) {
	gatewayURL := fmt.Sprintf("http://127.0.0.1:8080/api/login/qr/%s", platform)

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
		for i := 0; i < 30; i++ {
			if isPort8080Free() && isPort8081Free() {
				break
			}
			time.Sleep(100 * time.Millisecond)
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
			return 1, fmt.Errorf("gateway start failed: %w", err)
		}

		// Wait for PID file to appear (up to 10s)
		for i := 0; i < 100; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(pidFile); err == nil {
				if logFile != nil {
					logFile.Close()
				}
				return 0, nil
			}
		}

		if logFile != nil {
			logFile.Close()
		}
		return 1, fmt.Errorf("gateway PID file not created after 10s")
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

	if platform == "whatsapp" || platform == "qq" {
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

func (s *Server) generateDingTalkQR() (string, string, error) {
	appKey := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["dingtalk"]; ok {
			appKey = p.AppKey
		}
	}

	if appKey == "" {
		return "", "", fmt.Errorf("DingTalk QR login requires app_key. Please configure it in gateway settings.")
	}

	// Build DingTalk OAuth URL
	state := uuid.New().String()
	redirectURI := "http://localhost:8080/dingtalk/qr/callback"
	authURL := fmt.Sprintf(
		"https://oapi.dingtalk.com/connect/qrconnect?appid=%s&response_type=code&scope=snsapi_login&state=%s&redirect_uri=%s",
		appKey, state, url.QueryEscape(redirectURI),
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
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
		if resp, err := client.Get("http://localhost:8081/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				status["health_ok"] = true
			}
		}
	}

	jsonResponse(w, status)
}

func (s *Server) generateWeComQR() (string, string, error) {
	// WeCom requires corp_id and agent_id from config
	corpID := ""
	agentID := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["wecom"]; ok {
			corpID = p.CorpID
			agentID = p.AgentID
		}
	}

	if corpID == "" || agentID == "" {
		return "", "", fmt.Errorf("WeCom QR login requires corp_id and agent_id. Please configure them in gateway settings.")
	}

	// Build WeCom OAuth URL
	state := uuid.New().String()
	redirectURI := fmt.Sprintf("http://localhost:8080/wecom/qr/callback")
	authURL := fmt.Sprintf(
		"https://login.work.weixin.qq.com/wwopen/sso/qrConnect?appid=%s&agentid=%s&redirect_uri=%s&state=%s",
		corpID, agentID, url.QueryEscape(redirectURI), state,
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
}

func (s *Server) getActionStatus(id string) *ActionStatus {
	s.actionsMu.RLock()
	defer s.actionsMu.RUnlock()
	return s.actions[id]
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

	if platform == "whatsapp" || platform == "qq" {
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

		switch platform {
		case "wechat_ilink":
			qrData, qrImage, err = s.generateWeChatILinkQR()
		case "wecom":
			qrData, qrImage, err = s.generateWeComQR()
		case "dingtalk":
			qrData, qrImage, err = s.generateDingTalkQR()
		case "feishu":
			qrData, qrImage, err = s.generateFeishuQR()
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

		session, err = qrManager.CreateSession(platform, qrData, qrImage)
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

func (s *Server) generateWhatsAppQR() (string, string, error) {
	qrManager := gateway.GetQRManager()
	dataDir := filepath.Join(s.magicHome, "whatsapp")
	waGw := qrManager.GetOrCreateWhatsApp(dataDir)

	// If the persistent gateway is already logged in, the user wants to
	// re-link. Reset everything so we get a fresh client and new QR.
	if waGw.IsLoggedIn() {
		qrManager.ResetWhatsApp()
		waGw = qrManager.GetOrCreateWhatsApp(dataDir)
	}

	qrData, err := waGw.StartQRLogin(context.Background())
	if err != nil {
		return "", "", fmt.Errorf("failed to generate WhatsApp QR: %w", err)
	}

	// If StartQRLogin returned empty but the manager has a cached QR
	// (rotating code from an already-connected client), use that instead.
	if qrData == "" {
		cachedData, cachedImg := qrManager.GetLatestWhatsAppQR()
		if cachedData != "" {
			if cachedImg != "" {
				return cachedData, cachedImg, nil
			}
			img, err := gateway.GenerateQRCodePNG(cachedData)
			if err != nil {
				return cachedData, cachedData, nil
			}
			return cachedData, img, nil
		}
		return "", "", fmt.Errorf("WhatsApp already logged in, no QR code needed")
	}

	qrImage, err := gateway.GenerateQRCodePNG(qrData)
	if err != nil {
		return qrData, qrData, nil
	}

	return qrData, qrImage, nil
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

func (s *Server) generateFeishuQR() (string, string, error) {
	appID := ""

	if s.cfg.Gateway.Platforms != nil {
		if p, ok := s.cfg.Gateway.Platforms["feishu"]; ok {
			appID = p.AppID
		}
	}

	if appID == "" {
		return "", "", fmt.Errorf("Feishu QR login requires app_id. Please configure it in gateway settings.")
	}

	// Build Feishu OAuth URL
	state := uuid.New().String()
	redirectURI := "http://localhost:8080/feishu/qr/callback"
	authURL := fmt.Sprintf(
		"https://open.feishu.cn/open-apis/authen/v1/authorize?redirect_uri=%s&app_id=%s&state=%s",
		url.QueryEscape(redirectURI), appID, state,
	)

	// Generate QR image from the URL
	img, err := gateway.GenerateQRCodePNG(authURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate QR image: %w", err)
	}

	return authURL, img, nil
}
