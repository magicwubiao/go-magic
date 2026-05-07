package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"encoding/json"
)

// WeChatGateway implements the WeChat platform handler
type WeChatGateway struct {
	appID          string
	appSecret      string
	token          string
	tokenExpiresAt time.Time
	encodingAESKey string

	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool

	callbackPort int
	server       *http.Server
	serverOnce   sync.Once

	// WeChat API endpoints
	apiBaseURL string
	httpClient *http.Client
}

// WeChatMessage represents a WeChat message
type WeChatMessage struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	MsgID        string `xml:"MsgId"`
	Encrypt      string `xml:"Encrypt"`
}

// NewWeChatGateway creates a new WeChat gateway
func NewWeChatGateway(appID, appSecret, token, aesKey string) *WeChatGateway {
	return &WeChatGateway{
		appID:          appID,
		appSecret:      appSecret,
		token:          token,
		encodingAESKey: aesKey,
		agents:         make(map[string]*AgentSession),
		msgCh:          make(chan Message, 100),
		stopCh:         make(chan struct{}),
		callbackPort:   8083, // WeChat-specific port
		apiBaseURL:    "https://api.weixin.qq.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the platform name
func (g *WeChatGateway) Name() string {
	return "wechat"
}

// Connect establishes connection to WeChat
func (g *WeChatGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to WeChat gateway...")

	// Get access token
	if err := g.getAccessToken(); err != nil {
		log.Warnf("Failed to get WeChat access token: %v (will retry on first message)", err)
	}

	go g.startCallbackServer()

	log.Info("WeChat gateway connected")
	return nil
}

// getAccessToken obtains an access token from WeChat
func (g *WeChatGateway) getAccessToken() error {
	if g.appID == "" || g.appSecret == "" {
		return fmt.Errorf("appID or appSecret not configured")
	}

	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s",
		g.appID, g.appSecret)

	resp, err := g.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get access token: status %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	g.token = result.AccessToken
	log.Info("WeChat access token obtained")
	return nil
}

// Disconnect closes the connection
func (g *WeChatGateway) Disconnect() error {
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

	log.Info("WeChat gateway disconnected")
	return nil
}

// IsConnected checks if connected to WeChat
func (g *WeChatGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

// Send sends a message via WeChat API
func (g *WeChatGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("WeChat gateway not connected")
	}

	openID := resp.ChannelID // In WeChat, ChannelID is typically the OpenID
	if openID == "" {
		return fmt.Errorf("OpenID (channel ID) is required")
	}

	return g.SendText(openID, resp.Content)
}

// SendText sends a text message via WeChat API
func (g *WeChatGateway) SendText(openID, text string) error {
	if g.token == "" {
		return fmt.Errorf("WeChat access token not available")
	}

	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/custom/send?access_token=%s", g.token)

	// WeChat has a 2048 character limit per message
	if len(text) > 2040 {
		// Split into multiple messages
		for i := 0; i < len(text); i += 2030 {
			end := i + 2030
			if end > len(text) {
				end = len(text)
			}
			if err := g.sendWeChatMessage(url, openID, text[i:end]); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
		}
		return nil
	}

	return g.sendWeChatMessage(url, openID, text)
}

// sendWeChatMessage sends a single text message via WeChat API
func (g *WeChatGateway) sendWeChatMessage(url, openID, content string) error {
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
func (g *WeChatGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *WeChatGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "Available commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Show connection status",
		}, nil
	case "ping":
		return Response{
			Content: "Pong! 🏓",
		}, nil
	case "status":
		if g.IsConnected() {
			return Response{
				Content: "Bot is connected and ready!",
			}, nil
		}
		return Response{
			Content: "Bot is not connected",
		}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for WeChat gateway
func (g *WeChatGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:     "wechat",
		Connected:   g.IsConnected(),
		CallbackOK:  false,
		CallbackPort: g.callbackPort,
		Details:     make(map[string]interface{}),
	}

	if !status.Connected {
		status.Error = "Gateway not connected"
		return status
	}

	// Check HTTP client
	if g.httpClient != nil {
		status.HTTPClientOK = true
		status.Details["http_client_initialized"] = true
	} else {
		status.HTTPClientOK = false
		status.Error = "HTTP client not initialized"
		return status
	}

	// Check token validity
	g.mu.RLock()
	token := g.token
	tokenExpiry := g.tokenExpiresAt
	g.mu.RUnlock()

	if token != "" {
		status.TokenValid = true
		status.Details["token_available"] = true
	} else {
		status.TokenValid = false
		status.Details["token_available"] = false
	}

	if !tokenExpiry.IsZero() {
		status.TokenExpiry = &tokenExpiry
		// Token is expired if expiry time is in the past
		if time.Now().After(tokenExpiry) {
			status.TokenValid = false
			status.Details["token_expired"] = true
		}
	}

	// Check WeChat API connectivity
	start := time.Now()
	testURL := g.apiBaseURL + "/cgi-bin/getcallbackip?access_token=" + token
	req, _ := http.NewRequestWithContext(context.Background(), "GET", testURL, nil)

	resp, err := g.httpClient.Do(req)
	if err == nil {
		status.LatencyMs = time.Since(start).Milliseconds()
		resp.Body.Close()
		status.Details["api_reachable"] = true
		status.Details["api_status"] = resp.StatusCode
	} else {
		// Network error - could be no internet or blocked
		status.HTTPClientOK = false
		status.Error = fmt.Sprintf("WeChat API not reachable: %v", err)
	}

	return status
}

// startCallbackServer starts the HTTP server for callbacks
func (g *WeChatGateway) startCallbackServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/wechat/callback", g.handleCallback)
	mux.HandleFunc("/wechat/verify", g.handleVerify)

	addr := fmt.Sprintf(":%d", g.callbackPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	g.server = server

	log.Infof("WeChat callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("WeChat callback server error: %v", err)
	}
}

// handleVerify handles URL verification from WeChat
func (g *WeChatGateway) handleVerify(w http.ResponseWriter, r *http.Request) {
	// WeChat verification GET request
	signature := r.URL.Query().Get("signature")
	echostr := r.URL.Query().Get("echostr")
	timestamp := r.URL.Query().Get("timestamp")
	nonce := r.URL.Query().Get("nonce")

	if signature != "" && echostr != "" {
		// Verify signature
		if g.verifySignature(signature, timestamp, nonce) {
			// Return encrypted echo
			encrypted, _ := g.encrypt(echostr)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(encrypted))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature verifies WeChat callback signature
func (g *WeChatGateway) verifySignature(signature, timestamp, nonce string) bool {
	strs := sort.StringSlice{g.token, timestamp, nonce}
	sort.Strings(strs)
	str := strings.Join(strs, "")
	h := sha256.Sum256([]byte(str))
	return fmt.Sprintf("%x", h) == signature
}

// encrypt encrypts content using AES (simplified - in production use proper AES-CBC)
func (g *WeChatGateway) encrypt(content string) (string, error) {
	// This is a simplified implementation
	// In production, use proper AES encryption with the encodingAESKey
	return content, nil
}

// handleCallback handles incoming callbacks from WeChat
func (g *WeChatGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		g.handleVerify(w, r)
		return
	}

	if r.Method == "POST" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Failed to read WeChat callback body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Parse WeChat message event
		g.parseCallbackEvent(body)

		// WeChat requires a "success" response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// parseCallbackEvent parses incoming WeChat callback events
func (g *WeChatGateway) parseCallbackEvent(body []byte) {
	var msg WeChatMessage
	if err := xml.Unmarshal(body, &msg); err != nil {
		log.Errorf("Failed to parse WeChat message: %v", err)
		return
	}

	// Check if it's encrypted
	if msg.Encrypt != "" {
		// Decrypt the message (simplified)
		log.Debugf("Received encrypted WeChat message")
		return
	}

	// Only process text messages
	if msg.MsgType != "text" && msg.MsgType != "voice" {
		return
	}

	content := msg.Content
	if content == "" {
		// For voice messages, we might need ASR
		content = "[Voice message]"
	}

	msgData := Message{
		ID:        msg.MsgID,
		Platform:  "wechat",
		ChannelID: msg.FromUserName, // OpenID
		UserID:    msg.FromUserName,
		Content:   content,
		Timestamp: time.Unix(msg.CreateTime, 0),
		Metadata: map[string]interface{}{
			"msg_type": msg.MsgType,
			"to_user":  msg.ToUserName,
		},
	}

	// Send to channel
	select {
	case g.msgCh <- msgData:
		log.Debugf("WeChat message received: %s from %s", msgData.ID, msgData.UserID)
	default:
		log.Warnf("WeChat message channel full, dropping message: %s", msgData.ID)
	}
}

// SetCallbackPort sets the callback server port
func (g *WeChatGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}
