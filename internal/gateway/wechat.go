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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/mdp/qrterminal/v3"
)

// WeChatQRGateway implements WeChat Official Account QR code login mode
// This mode uses the WeChat Open Platform OAuth2 flow for easier setup
// Requirements:
// - WeChat Official Account (订阅号/服务号都可以)
// - No public IP required
// - No verified service account required
type WeChatQRGateway struct {
	appID     string
	appSecret string

	// Session data
	session     *WeChatSession
	sessionPath string

	// Token management
	accessToken    string
	refreshToken   string
	openID         string
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
}

// WeChatSession represents the OAuth session data
type WeChatSession struct {
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	OpenID         string    `json:"openid"`
	ExpiresAt      time.Time `json:"expires_at"`
	RefreshAt      time.Time `json:"refresh_at"`
	LastActiveTime time.Time `json:"last_active"`
}

// NewWeChatQRGateway creates a new WeChat QR code login gateway
func NewWeChatQRGateway(appID, appSecret string) *WeChatQRGateway {
	home, _ := os.UserHomeDir()
	sessionPath := filepath.Join(home, ".magic", "wechat", "session.json")

	return &WeChatQRGateway{
		appID:          appID,
		appSecret:      appSecret,
		sessionPath:    sessionPath,
		agents:         make(map[string]*AgentSession),
		msgCh:          make(chan Message, 100),
		stopCh:         make(chan struct{}),
		qrCallbackPort: 8083,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetQRCallback sets a callback for QR code URL (for CLI display or API push)
func (g *WeChatQRGateway) SetQRCallback(cb func(url string)) {
	g.qrCallback = cb
}

// SetQRCallbackPort sets the callback server port
func (g *WeChatQRGateway) SetQRCallbackPort(port int) {
	g.qrCallbackPort = port
}

// Name returns the platform name
func (g *WeChatQRGateway) Name() string {
	return "wechat"
}

// Connect establishes connection with QR code login
func (g *WeChatQRGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to WeChat QR gateway...")

	// Ensure session directory exists
	sessionDir := filepath.Dir(g.sessionPath)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return fmt.Errorf("failed to create session dir: %w", err)
	}

	// Try to load existing session
	if err := g.loadSession(); err != nil {
		log.Debugf("No existing session found: %v", err)
	}

	// Check if session is valid
	g.tokenMu.RLock()
	isValid := g.accessToken != "" && time.Now().Before(g.tokenExpiresAt)
	g.tokenMu.RUnlock()

	if isValid {
		log.Info("Found valid WeChat session, using cached token")
	} else if g.refreshToken != "" {
		// Try to refresh the token
		log.Info("Session expired, attempting to refresh token...")
		if err := g.refreshAccessToken(); err != nil {
			log.Warnf("Failed to refresh token: %v, need re-authentication", err)
			g.startQRServer(ctx)
		}
	} else {
		// Need new QR code login
		log.Info("[QR Login] No saved session, starting QR code login. Please scan with your app...")
		g.startQRServer(ctx)
	}

	log.Info("WeChat QR gateway connected")
	return nil
}

// startQRServer starts the local HTTP server for OAuth callback
func (g *WeChatQRGateway) startQRServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wechat/qr/callback", g.handleQROAuthCallback)
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

	log.Infof("WeChat QR server starting on %s", addr)

	// Display QR code in terminal
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  📱 微信扫码登录 / WeChat QR Login                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Instructions:")
	fmt.Println("  1. Open WeChat on your phone")
	fmt.Println("  2. Tap '+' → 'Scan'")
	fmt.Println("  3. Scan the QR code below")
	fmt.Println("  4. Confirm login on your phone")
	fmt.Println()
	fmt.Println("📱 QR Code (scan with WeChat):")
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
			log.Errorf("WeChat QR server error: %v", err)
		}
	}()
}

// pendingState stores the OAuth state for CSRF protection
var pendingState string

// buildAuthURL builds the WeChat OAuth authorization URL
func (g *WeChatQRGateway) buildAuthURL(state string) string {
	redirectURI := url.QueryEscape(fmt.Sprintf("http://localhost:%d/wechat/qr/callback", g.qrCallbackPort))
	return fmt.Sprintf(
		"https://open.weixin.qq.com/connect/qrconnect?appid=%s&redirect_uri=%s&response_type=code&scope=snsapi_login&state=%s#wechat_redirect",
		g.appID, redirectURI, state,
	)
}

// handleQROAuthCallback handles the OAuth callback after QR scan
func (g *WeChatQRGateway) handleQROAuthCallback(w http.ResponseWriter, r *http.Request) {
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

	// Exchange code for access token
	if err := g.exchangeCodeForToken(code); err != nil {
		log.Errorf("Failed to exchange code for token: %v", err)
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
    <title>WeChat Login Success</title>
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

	log.Info("WeChat QR login successful!")
}

// exchangeCodeForToken exchanges the OAuth code for access token
func (g *WeChatQRGateway) exchangeCodeForToken(code string) error {
	tokenURL := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/oauth2/access_token?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		g.appID, g.appSecret, code,
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
		ErrCode       int    `json:"errcode"`
		ErrMsg        string `json:"errmsg"`
		AccessToken   string `json:"access_token"`
		RefreshToken  string `json:"refresh_token"`
		OpenID        string `json:"openid"`
		ExpiresIn     int    `json:"expires_in"`
		RefreshExpires int   `json:"refresh_expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("token API error: %d - %s", result.ErrCode, result.ErrMsg)
	}

	// Store tokens
	g.tokenMu.Lock()
	g.accessToken = result.AccessToken
	g.refreshToken = result.RefreshToken
	g.openID = result.OpenID
	g.tokenExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	g.tokenMu.Unlock()

	log.Infof("WeChat access token obtained for OpenID: %s", result.OpenID)
	return nil
}

// refreshAccessToken refreshes the access token using refresh token
func (g *WeChatQRGateway) refreshAccessToken() error {
	g.tokenMu.RLock()
	refreshToken := g.refreshToken
	g.tokenMu.RUnlock()

	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	refreshURL := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/oauth2/refresh_token?appid=%s&grant_type=refresh_token&refresh_token=%s",
		g.appID, refreshToken,
	)

	resp, err := g.httpClient.Get(refreshURL)
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		ErrCode       int    `json:"errcode"`
		ErrMsg        string `json:"errmsg"`
		AccessToken   string `json:"access_token"`
		RefreshToken  string `json:"refresh_token"`
		OpenID        string `json:"openid"`
		ExpiresIn     int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("refresh token API error: %d - %s", result.ErrCode, result.ErrMsg)
	}

	// Store new tokens
	g.tokenMu.Lock()
	g.accessToken = result.AccessToken
	g.refreshToken = result.RefreshToken
	g.openID = result.OpenID
	g.tokenExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	g.tokenMu.Unlock()

	// Save updated session
	if err := g.saveSession(); err != nil {
		log.Warnf("Failed to save session after refresh: %v", err)
	}

	log.Info("WeChat access token refreshed successfully")
	return nil
}

// loadSession loads the session from disk
func (g *WeChatQRGateway) loadSession() error {
	data, err := os.ReadFile(g.sessionPath)
	if err != nil {
		return err
	}

	var session WeChatSession
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}

	g.tokenMu.Lock()
	g.accessToken = session.AccessToken
	g.refreshToken = session.RefreshToken
	g.openID = session.OpenID
	g.tokenExpiresAt = session.ExpiresAt
	g.tokenMu.Unlock()

	return nil
}

// saveSession saves the session to disk
func (g *WeChatQRGateway) saveSession() error {
	g.tokenMu.RLock()
	session := WeChatSession{
		AccessToken:    g.accessToken,
		RefreshToken:  g.refreshToken,
		OpenID:         g.openID,
		ExpiresAt:      g.tokenExpiresAt,
		RefreshAt:      time.Now(),
		LastActiveTime: time.Now(),
	}
	g.tokenMu.RUnlock()

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(g.sessionPath, data, 0600)
}

// Disconnect closes the connection
func (g *WeChatQRGateway) Disconnect() error {
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

	log.Info("WeChat QR gateway disconnected")
	return nil
}

// IsConnected checks if connected to WeChat
func (g *WeChatQRGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.accessToken != ""
}

// Send sends a message via WeChat API
func (g *WeChatQRGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("WeChat gateway not connected")
	}

	openID := resp.ChannelID
	if openID == "" {
		g.tokenMu.RLock()
		openID = g.openID
		g.tokenMu.RUnlock()
	}

	if openID == "" {
		return fmt.Errorf("OpenID (channel ID) is required")
	}

	return g.SendText(openID, resp.Content)
}

// SendText sends a text message via WeChat customer service API
func (g *WeChatQRGateway) SendText(openID, text string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	if token == "" {
		return fmt.Errorf("WeChat access token not available")
	}

	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/custom/send?access_token=%s", token)

	// WeChat has a 2048 character limit per message
	if len(text) > 2040 {
		// Split into multiple messages
		for i := 0; i < len(text); i += 2030 {
			end := i + 2030
			if end > len(text) {
				end = len(text)
			}
			if err := g.sendMessage(url, openID, text[i:end]); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
		}
		return nil
	}

	return g.sendMessage(url, openID, text)
}

// sendMessage sends a single text message via WeChat API
func (g *WeChatQRGateway) sendMessage(url, openID, content string) error {
	body := map[string]interface{}{
		"touser":  openID,
		"msgtype": "text",
		"text": map[string]string{
			"content": content,
		},
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	resp, err := g.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("WeChat API error: %d - %s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

// Receive returns a channel of incoming messages
func (g *WeChatQRGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *WeChatQRGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - WeChat\n\n" +
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
		g.tokenMu.RLock()
		hasToken := g.accessToken != ""
		openID := g.openID
		g.tokenMu.RUnlock()

		if hasToken {
			return Response{
				Content: fmt.Sprintf("Bot is connected!\nOpenID: %s", openID),
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

// CheckHealth returns detailed health status for WeChat QR gateway
func (g *WeChatQRGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	running := g.running
	g.mu.RUnlock()

	g.tokenMu.RLock()
	hasToken := g.accessToken != ""
	openID := g.openID
	tokenExpiry := g.tokenExpiresAt
	g.tokenMu.RUnlock()

	status := &HealthStatus{
		Platform:     "wechat",
		Connected:    running && hasToken,
		Status:       "healthy",
		CallbackPort: g.qrCallbackPort,
		Details:      make(map[string]interface{}),
		Platforms:    make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "wechat",
		Status: "connected",
	}

	if !running {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not running"
		status.Status = "error"
		status.Platforms["wechat"] = platformStatus
		return status
	}

	if !hasToken {
		platformStatus.Status = "waiting_login"
		platformStatus.Error = "Not authenticated, need QR login"
		status.Status = "waiting_login"
		status.Details["authenticated"] = false
	} else {
		status.TokenValid = true
		status.Details["authenticated"] = true
		status.Details["openid"] = openID
	}

	if !tokenExpiry.IsZero() {
		status.TokenExpiry = &tokenExpiry
		if time.Now().After(tokenExpiry) {
			status.TokenValid = false
			status.Details["token_expired"] = true
			platformStatus.Error = "Token expired"
			status.Status = "error"
		}
	}

	status.Details["mode"] = "qr"
	status.Details["callback_port"] = g.qrCallbackPort
	status.Platforms["wechat"] = platformStatus

	return status
}

// GetOpenID returns the authenticated user's OpenID
func (g *WeChatQRGateway) GetOpenID() string {
	g.tokenMu.RLock()
	defer g.tokenMu.RUnlock()
	return g.openID
}

// IsAuthenticated returns whether the gateway is authenticated
func (g *WeChatQRGateway) IsAuthenticated() bool {
	g.tokenMu.RLock()
	defer g.tokenMu.RUnlock()
	return g.accessToken != ""
}

// TriggerReauth starts a new QR authentication flow
func (g *WeChatQRGateway) TriggerReauth(ctx context.Context) {
	g.tokenMu.Lock()
	g.accessToken = ""
	g.refreshToken = ""
	g.openID = ""
	g.tokenMu.Unlock()

	// Clear session file
	os.Remove(g.sessionPath)

	g.startQRServer(ctx)
}

// Backward compatibility: NewWeChatGateway creates a QR gateway by default
// This is for API compatibility with existing code
func NewWeChatGateway(appID, appSecret, token, aesKey string) *WeChatQRGateway {
	gw := NewWeChatQRGateway(appID, appSecret)
	// Note: token and aesKey are not used in QR mode, kept for backward compatibility
	return gw
}
