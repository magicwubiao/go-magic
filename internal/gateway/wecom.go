package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/mdp/qrterminal/v3"
)

// WeComQRGateway implements WeCom (Enterprise WeChat) QR code login mode
// This mode uses the WeCom Open Platform OAuth2 flow for easier setup
// Requirements:
// - A WeCom enterprise account (任何版本都可以)
// - No public IP required
// - No verified enterprise required
type WeComQRGateway struct {
	corpID   string
	agentID  string
	secret   string

	// Session data
	session     *WeComSession
	sessionPath string

	// User info (obtained from QR scan)
	userID       string
	userName     string
	userDeptID   int

	// Token management
	accessToken    string
	tokenExpiresAt time.Time
	tokenMu        sync.RWMutex

	// Runtime state
	agents  map[string]*AgentSession
	msgCh   chan Message
	mu      sync.RWMutex
	stopCh  chan struct{}
	running bool

	// QR callback server
	qrCallbackPort int
	server         *http.Server
	serverOnce     sync.Once

	// Pending state for OAuth verification
	pendingState string

	// QR callback for external display
	qrCallback func(url string)

	// HTTP client
	httpClient *http.Client

	// Channel allowlist/blocklist
	allowedChannels []string
	blockedChannels []string
}

// WeComSession represents the QR login session data
type WeComSession struct {
	UserID       string    `json:"user_id"`
	UserName     string    `json:"user_name"`
	UserDeptID   int       `json:"user_dept_id"`
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastActive   time.Time `json:"last_active"`
}

// NewWeComQRGateway creates a new WeCom QR code login gateway
func NewWeComQRGateway(corpID, agentID, secret string) *WeComQRGateway {
	home, _ := os.UserHomeDir()
	sessionPath := filepath.Join(home, ".magic", "wecom", "session.json")

	return &WeComQRGateway{
		corpID:         corpID,
		agentID:        agentID,
		secret:         secret,
		sessionPath:    sessionPath,
		agents:         make(map[string]*AgentSession),
		msgCh:          make(chan Message, 100),
		stopCh:         make(chan struct{}),
		qrCallbackPort: 8080,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetQRCallback sets a callback for QR code URL (for CLI display or API push)
func (g *WeComQRGateway) SetQRCallback(cb func(url string)) {
	g.qrCallback = cb
}

// SetQRCallbackPort sets the callback server port
func (g *WeComQRGateway) SetQRCallbackPort(port int) {
	g.qrCallbackPort = port
}

// SetChannelFilter sets the channel allowlist and blocklist
func (g *WeComQRGateway) SetChannelFilter(allowed, blocked []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.allowedChannels = allowed
	g.blockedChannels = blocked
}

// Name returns the platform name
func (g *WeComQRGateway) Name() string {
	return "wecom"
}

// Connect establishes connection with QR code login
func (g *WeComQRGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to WeCom QR gateway...")

	// Ensure session directory exists
	sessionDir := filepath.Dir(g.sessionPath)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return fmt.Errorf("failed to create session dir: %w", err)
	}

	// Try to load existing session
	if err := g.loadSession(); err != nil {
		log.Debugf("No existing session found: %v", err)
	}

	// Check if we have a valid user session
	g.mu.RLock()
	hasUser := g.userID != ""
	g.mu.RUnlock()

	// Check if we need to get/refresh access token using corpsecret
	if err := g.refreshAccessToken(); err != nil {
		log.Warnf("Failed to refresh access token: %v", err)
	}

	if !hasUser {
		// Need new QR code login
		log.Info("[QR Login] No saved session, starting QR code login. Please scan with your app...")
		g.startQRServer(ctx)
	} else {
		log.Infof("Found user session: %s", g.userName)
	}

	log.Info("WeCom QR gateway connected")
	return nil
}

// startQRServer starts the local HTTP server for OAuth callback
func (g *WeComQRGateway) startQRServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wecom/qr/callback", g.handleQROAuthCallback)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf(":%d", g.qrCallbackPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	g.server = server

	// Generate and display QR code URL
	state := uuid.New().String()
	authURL := g.buildAuthURL(state)

	// Store state for verification
	g.mu.Lock()
	g.pendingState = state
	g.mu.Unlock()

	log.Infof("WeCom QR server starting on %s", addr)

	// Display QR code in terminal
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  📱 企业微信扫码登录 / WeCom QR Login                ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Instructions:")
	fmt.Println("  1. Open WeCom (Enterprise WeChat) on your phone")
	fmt.Println("  2. Tap '+' → 'Scan'")
	fmt.Println("  3. Scan the QR code below")
	fmt.Println("  4. Confirm login on your phone")
	fmt.Println()
	fmt.Println("📱 QR Code (scan with WeCom):")
	fmt.Println()
	qrterminal.GenerateHalfBlock(authURL, qrterminal.M, os.Stdout)
	fmt.Println()
	fmt.Println("QR Code URL:", authURL)
	fmt.Println()

	// Call QR callback if set (for backward compatibility)
	if g.qrCallback != nil {
		g.qrCallback(authURL)
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("WeCom QR server error: %v", err)
		}
	}()
}

// pendingState stores the OAuth state for CSRF protection
var pendingStateWecom string

// buildAuthURL builds the WeCom OAuth authorization URL
func (g *WeComQRGateway) buildAuthURL(state string) string {
	redirectURI := url.QueryEscape(fmt.Sprintf("http://localhost:%d/wecom/qr/callback", g.qrCallbackPort))
	return fmt.Sprintf(
		"https://login.work.weixin.qq.com/wwopen/sso/qrConnect?appid=%s&agentid=%s&redirect_uri=%s&state=%s",
		g.corpID, g.agentID, redirectURI, state,
	)
}

// handleQROAuthCallback handles the OAuth callback after QR scan
func (g *WeComQRGateway) handleQROAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// Verify state
	g.mu.RLock()
	expectedState := g.pendingState
	g.mu.RUnlock()

	if state != expectedState {
		log.Errorf("Invalid OAuth state: expected %s, got %s", expectedState, state)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid state"))
		return
	}

	if code == "" {
		log.Error("No code received in OAuth callback")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No code provided"))
		return
	}

	// Exchange code for user info
	if err := g.exchangeCodeForUser(code); err != nil {
		log.Errorf("Failed to exchange code for user: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to authenticate"))
		return
	}

	// Save session
	if err := g.saveSession(); err != nil {
		log.Errorf("Failed to save session: %v", err)
	}

	// Send success response
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>WeCom Login Success</title>
    <style>
        body { font-family: Arial, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f5f5f5; }
        .success { background: white; padding: 40px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); text-align: center; }
        .checkmark { color: #07C160; font-size: 48px; }
        h1 { color: #07C160; }
        p { color: #666; }
    </style>
</head>
<body>
    <div class="success">
        <div class="checkmark">✓</div>
        <h1>Login Successful!</h1>
        <p>You can close this window and return to the application.</p>
    </div>
</body>
</html>`))

	log.Infof("WeCom QR login successful! User: %s", g.userName)
}

// exchangeCodeForUser exchanges the OAuth code for user info
func (g *WeComQRGateway) exchangeCodeForUser(code string) error {
	// First get access token using corpsecret
	if err := g.refreshAccessToken(); err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	// Then get user info using the code
	userInfoURL := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo?access_token=%s&code=%s",
		token, code,
	)

	resp, err := g.httpClient.Get(userInfoURL)
	if err != nil {
		return fmt.Errorf("failed to request user info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		ErrCode   int    `json:"errcode"`
		ErrMsg    string `json:"errmsg"`
		UserID    string `json:"UserId"`
		OpenID    string `json:"OpenId"`
		Name      string `json:"name"`
		DeptID    []int  `json:"deptid"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("user info API error: %d - %s", result.ErrCode, result.ErrMsg)
	}

	// Store user info
	g.mu.Lock()
	g.userID = result.UserID
	g.userName = result.Name
	if len(result.DeptID) > 0 {
		g.userDeptID = result.DeptID[0]
	}
	g.mu.Unlock()

	log.Infof("WeCom user info obtained: %s (ID: %s)", result.Name, result.UserID)
	return nil
}

// refreshAccessToken refreshes the access token using corpsecret
func (g *WeComQRGateway) refreshAccessToken() error {
	tokenURL := fmt.Sprintf(
		"https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		g.corpID, g.secret,
	)

	resp, err := g.httpClient.Get(tokenURL)
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("token API error: %d - %s", result.ErrCode, result.ErrMsg)
	}

	// Store token
	g.tokenMu.Lock()
	g.accessToken = result.AccessToken
	g.tokenExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	g.tokenMu.Unlock()

	log.Debugf("WeCom access token refreshed")
	return nil
}

// loadSession loads the session from disk
func (g *WeComQRGateway) loadSession() error {
	data, err := os.ReadFile(g.sessionPath)
	if err != nil {
		return err
	}

	var session WeComSession
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}

	g.mu.Lock()
	g.userID = session.UserID
	g.userName = session.UserName
	g.userDeptID = session.UserDeptID
	g.mu.Unlock()

	g.tokenMu.Lock()
	g.accessToken = session.AccessToken
	g.tokenExpiresAt = session.ExpiresAt
	g.tokenMu.Unlock()

	return nil
}

// saveSession saves the session to disk
func (g *WeComQRGateway) saveSession() error {
	g.mu.RLock()
	userID := g.userID
	userName := g.userName
	userDeptID := g.userDeptID
	g.mu.RUnlock()

	g.tokenMu.RLock()
	session := WeComSession{
		UserID:      userID,
		UserName:    userName,
		UserDeptID:  userDeptID,
		AccessToken: g.accessToken,
		ExpiresAt:  g.tokenExpiresAt,
		LastActive:  time.Now(),
	}
	g.tokenMu.RUnlock()

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(g.sessionPath, data, 0600)
}

// Disconnect closes the connection
func (g *WeComQRGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	g.serverOnce.Do(func() {
		if g.server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			g.server.Shutdown(shutdownCtx)
		}
		close(g.stopCh)
		close(g.msgCh)
	})
	g.running = false

	log.Info("WeCom QR gateway disconnected")
	return nil
}

// IsConnected checks if connected to WeCom
func (g *WeComQRGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.userID != ""
}

// Send sends a message via WeCom API
func (g *WeComQRGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("WeCom gateway not connected")
	}

	userID := resp.ChannelID
	if userID == "" {
		// Default to the logged-in user
		g.mu.RLock()
		userID = g.userID
		g.mu.RUnlock()
	}

	if userID == "" {
		return fmt.Errorf("User ID (channel ID) is required")
	}

	content := resp.Content

	// Check if content is rich text (starts with { or [)
	if strings.HasPrefix(strings.TrimSpace(content), "{") ||
		strings.HasPrefix(strings.TrimSpace(content), "[") {
		// Try to send as rich content
		if err := g.sendRichMessage(userID, content); err != nil {
			log.Warnf("Failed to send rich message, falling back to text: %v", err)
			return g.sendMessage(userID, content)
		}
		return nil
	}
	return g.sendMessage(userID, content)
}

// sendMessage sends a message via WeCom API
func (g *WeComQRGateway) sendMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)

	// WeCom has a 2048 character limit per message
	var err error
	if len(content) > 2040 {
		// Split into multiple messages
		for i := 0; i < len(content); i += 2030 {
			end := i + 2030
			if end > len(content) {
				end = len(content)
			}
			if err = g.doSendMessage(url, userID, content[i:end]); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
		}
		return nil
	}

	return g.doSendMessage(url, userID, content)
}

// doSendMessage performs the actual HTTP request to send a message
func (g *WeComQRGateway) doSendMessage(url, userID, content string) error {
	msg := map[string]interface{}{
		"touser":  userID,
		"msgtype": "text",
		"agentid": g.agentID,
		"text": map[string]string{
			"content": content,
		},
	}

	jsonBody, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send message error (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// SendText sends a text message
func (g *WeComQRGateway) SendText(userID, content string) error {
	return g.sendMessage(userID, content)
}

// sendRichMessage sends a rich text or markdown message
func (g *WeComQRGateway) sendRichMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)

	// Try to parse as JSON (rich content format)
	var richContent map[string]interface{}
	if err := json.Unmarshal([]byte(content), &richContent); err == nil {
		// It's valid JSON, use it directly
		richContent["touser"] = userID
		richContent["agentid"] = g.agentID

		jsonBody, _ := json.Marshal(richContent)
		req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send rich message: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("send rich message error (%d): %s", resp.StatusCode, string(body))
		}
		return nil
	}

	// Fallback to text
	return g.sendMessage(userID, content)
}

// Receive returns a channel of incoming messages
func (g *WeComQRGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *WeComQRGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - WeCom\n\n" +
				"📋 Commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Connection status\n" +
				"/new - New conversation\n" +
				"/compress - Compress context\n" +
				"/usage - Token usage\n" +
				"/model - Change model\n" +
				"/goal - Goal management\n" +
				"/kanban - Kanban board\n" +
				"/login - Re-authenticate if disconnected",
		}, nil
	case "ping":
		return Response{
			Content: "Pong! 🏓",
		}, nil
	case "status":
		g.mu.RLock()
		hasUser := g.userID != ""
		userName := g.userName
		g.mu.RUnlock()

		if hasUser {
			return Response{
				Content: fmt.Sprintf("Bot is connected!\nUser: %s (ID: %s)", userName, g.userID),
			}, nil
		}
		return Response{
			Content: "Bot is not connected. Please re-authenticate.",
		}, nil
	case "login":
		go g.startQRServer(context.Background())
		return Response{
			Content: "Starting QR code login... Please check the terminal for the QR code URL.",
		}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for WeCom QR gateway
func (g *WeComQRGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	running := g.running
	userID := g.userID
	userName := g.userName
	g.mu.RUnlock()

	g.tokenMu.RLock()
	tokenExpiry := g.tokenExpiresAt
	g.tokenMu.RUnlock()

	status := &HealthStatus{
		Platform:     "wecom",
		Connected:    running && userID != "",
		Status:       "healthy",
		CallbackPort: g.qrCallbackPort,
		Details:      make(map[string]interface{}),
		Platforms:    make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "wecom",
		Status: "connected",
	}

	if !running {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not running"
		status.Status = "error"
		status.Platforms["wecom"] = platformStatus
		return status
	}

	if userID == "" {
		platformStatus.Status = "waiting_login"
		platformStatus.Error = "Not authenticated, need QR login"
		status.Status = "waiting_login"
		status.Details["authenticated"] = false
	} else {
		status.TokenValid = true
		status.Details["authenticated"] = true
		status.Details["user_id"] = userID
		status.Details["user_name"] = userName
	}

	if !tokenExpiry.IsZero() {
		status.TokenExpiry = &tokenExpiry
		if time.Now().After(tokenExpiry) {
			status.Details["token_expired"] = true
		}
	}

	status.Details["mode"] = "qr"
	status.Details["callback_port"] = g.qrCallbackPort
	status.Platforms["wecom"] = platformStatus

	return status
}

// GetUserID returns the authenticated user's ID
func (g *WeComQRGateway) GetUserID() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.userID
}

// GetUserName returns the authenticated user's name
func (g *WeComQRGateway) GetUserName() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.userName
}

// IsAuthenticated returns whether the gateway is authenticated
func (g *WeComQRGateway) IsAuthenticated() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.userID != ""
}

// TriggerReauth starts a new QR authentication flow
func (g *WeComQRGateway) TriggerReauth(ctx context.Context) {
	g.mu.Lock()
	g.userID = ""
	g.userName = ""
	g.userDeptID = 0
	g.mu.Unlock()

	// Clear session file
	os.Remove(g.sessionPath)

	g.startQRServer(ctx)
}

// Backward compatibility: NewWeComGateway creates a QR gateway by default
// This is for API compatibility with existing code
func NewWeComGateway(corpID, agentID, secret string) *WeComQRGateway {
	return NewWeComQRGateway(corpID, agentID, secret)
}
