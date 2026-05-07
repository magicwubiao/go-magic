package gateway

import (
	"bytes"
	"context"
	
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// FeishuGateway implements the Feishu (Lark) platform handler
type FeishuGateway struct {
	appID      string
	appSecret  string
	verificationToken string
	encryptKey string
	
	tenantAccessToken string
	tokenExpiresAt    time.Time
	tokenMu           sync.RWMutex
	
	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool
	
	// Configurable callback port
	callbackPort int
	
	// Reconnection config
	maxRetries     int
	retryDelay     time.Duration
	currentRetries int
}

// NewFeishuGateway creates a new Feishu gateway
func NewFeishuGateway(appID, appSecret string) *FeishuGateway {
	return &FeishuGateway{
		appID:      appID,
		appSecret:  appSecret,
		agents:     make(map[string]*AgentSession),
		msgCh:      make(chan Message, 100),
		stopCh:     make(chan struct{}),
		callbackPort: 8081,
		maxRetries:   5,
		retryDelay:   time.Second * 5,
	}
}

// Name returns the platform name
func (g *FeishuGateway) Name() string {
	return "feishu"
}

// Connect establishes connection to Feishu
func (g *FeishuGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to Feishu gateway...")
	
	// Get initial token
	if err := g.refreshToken(); err != nil {
		log.Errorf("Failed to get Feishu token: %v", err)
		return err
	}
	
	// Start token refresh goroutine
	go g.tokenRefresher()
	
	// Start callback server
	go g.startCallbackServer()
	
	log.Info("Feishu gateway connected")
	return nil
}

// Disconnect closes the connection
func (g *FeishuGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if !g.running {
		return nil
	}
	
	close(g.stopCh)
	g.running = false
	g.currentRetries = 0
	
	log.Info("Feishu gateway disconnected")
	return nil
}

// Send sends a message (enhanced to support rich text)
func (g *FeishuGateway) Send(ctx context.Context, resp Response) error {
	// Try to send via API if we have channel info
	if resp.ChannelID != "" {
		// Check if content is rich text (starts with { or [)
		content := resp.Content
		if strings.HasPrefix(strings.TrimSpace(content), "{") ||
			strings.HasPrefix(strings.TrimSpace(content), "[") {
			// Try to send as rich text/card
			if err := g.sendRichMessage(resp.ChannelID, content); err != nil {
				log.Warnf("Failed to send rich message, falling back to text: %v", err)
				return g.sendMessageAPI(resp.ChannelID, resp.Content)
			}
			return nil
		}
		return g.sendMessageAPI(resp.ChannelID, content)
	}

	return nil
}

// Receive returns a channel of incoming messages
func (g *FeishuGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *FeishuGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	// Implement slash command handling
	switch cmd {
	case "help":
		return Response{Content: "Available commands:\n/help - Show this help\n/stats - Show statistics"}, nil
	case "stats":
		return Response{Content: "Gateway is running"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for Feishu gateway
func (g *FeishuGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:     "feishu",
		Connected:   g.IsConnected(),
		CallbackOK:  false,
		CallbackPort: g.callbackPort,
		Details:     make(map[string]interface{}),
	}

	if !status.Connected {
		status.Error = "Gateway not connected"
		return status
	}

	// Check token validity
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	tokenExpiry := g.tokenExpiresAt
	g.tokenMu.RUnlock()

	if token != "" {
		status.TokenValid = true
		status.Details["token_available"] = true
	} else {
		status.TokenValid = false
		status.Details["token_available"] = false
	}

	if !tokenExpiry.IsZero() {
		status.TokenExpiry = &tokenExpiry
		if time.Now().After(tokenExpiry) {
			status.TokenValid = false
			status.Details["token_expired"] = true
		}
	}

	// HTTP client is implicit via http.Post in refreshToken
	status.HTTPClientOK = true
	status.Details["http_client_initialized"] = true

	// Check Feishu API connectivity
	start := time.Now()
	testURL := "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
	body := map[string]string{
		"app_id":     g.appID,
		"app_secret": g.appSecret,
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(testURL, "application/json", bytes.NewReader(jsonBody))
	if err == nil {
		status.LatencyMs = time.Since(start).Milliseconds()
		resp.Body.Close()
		status.Details["api_reachable"] = true
		status.Details["api_status"] = resp.StatusCode
	} else {
		status.HTTPClientOK = false
		status.Error = fmt.Sprintf("Feishu API not reachable: %v", err)
	}

	return status
}

// IsConnected checks if connected to Feishu
func (g *FeishuGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.tenantAccessToken != ""
}

// refreshToken gets a new tenant access token
func (g *FeishuGateway) refreshToken() error {
	url := "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"

	body := map[string]string{
		"app_id":     g.appID,
		"app_secret": g.appSecret,
	}

	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	if result.Code != 0 {
		return fmt.Errorf("token API error: %s", result.Msg)
	}

	g.tokenMu.Lock()
	g.tenantAccessToken = result.TenantAccessToken
	g.tokenExpiresAt = time.Now().Add(time.Duration(result.Expire-60) * time.Second)
	g.tokenMu.Unlock()

	log.Debugf("Feishu token refreshed, expires in %d seconds", result.Expire)
	return nil
}

// tokenRefresher periodically refreshes the token
func (g *FeishuGateway) tokenRefresher() {
	ticker := time.NewTicker(time.Hour) // Refresh every hour
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.mu.RLock()
			running := g.running
			g.mu.RUnlock()
			
			if !running {
				return
			}
			
			if err := g.refreshToken(); err != nil {
				log.Errorf("Failed to refresh Feishu token: %v", err)
			}
		}
	}
}

// startCallbackServer starts the HTTP server for callbacks
func (g *FeishuGateway) startCallbackServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/feishu/callback", g.handleCallback)
	
	addr := fmt.Sprintf(":%d", g.callbackPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	
	log.Infof("Feishu callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("Feishu callback server error: %v", err)
	}
}

// SetCallbackPort sets the callback server port
func (g *FeishuGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}

// handleCallback handles incoming callbacks from Feishu
func (g *FeishuGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// URL verification challenge
		w.WriteHeader(http.StatusOK)
		return
	}
	
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Errorf("Failed to read callback body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	// Parse callback event
	var event struct {
		Schema string `json:"schema"`
		Header struct {
			EventID   string `json:"event_id"`
			EventType string `json:"event_type"`
			AppID     string `json:"app_id"`
			TenantKey string `json:"tenant_key"`
			CreateTime string `json:"create_time"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}
	
	if err := json.Unmarshal(body, &event); err != nil {
		log.Errorf("Failed to parse callback event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	// Handle different event types
	switch event.Header.EventType {
	case "im.message.receive_v1":
		g.handleMessageEvent(event.Event)
	default:
		log.Debugf("Unhandled event type: %s", event.Header.EventType)
	}
	
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"code":0}`))
}

// handleMessageEvent processes a message receive event
func (g *FeishuGateway) handleMessageEvent(event json.RawMessage) {
	var msgEvent struct {
		Sender struct {
			SenderID struct {
				OpenID  string `json:"open_id"`
				UnionID string `json:"union_id"`
			} `json:"sender_id"`
			SenderType string `json:"sender_type"`
			TenantKey  string `json:"tenant_key"`
		} `json:"sender"`
		Message struct {
			MessageID string `json:"message_id"`
			RootID    string `json:"root_id"`
			ParentID  string `json:"parent_id"`
			CreateTime string `json:"create_time"`
			ChatID    string `json:"chat_id"`
			ChatType  string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content   string `json:"content"`
		} `json:"message"`
	}
	
	if err := json.Unmarshal(event, &msgEvent); err != nil {
		log.Errorf("Failed to parse message event: %v", err)
		return
	}
	
	// Only handle user messages
	if msgEvent.Sender.SenderType != "user" {
		return
	}
	
	// Parse message content
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msgEvent.Message.Content), &content); err != nil {
		log.Errorf("Failed to parse message content: %v", err)
		return
	}
	
	msg := Message{
		ID:        msgEvent.Message.MessageID,
		Platform:  "feishu",
		ChannelID: msgEvent.Message.ChatID,
		UserID:    msgEvent.Sender.SenderID.OpenID,
		Content:   content.Text,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"chat_type":   msgEvent.Message.ChatType,
			"message_type": msgEvent.Message.MessageType,
		},
	}
	
	g.mu.RLock()
	msgCh := g.msgCh
	g.mu.RUnlock()
	
	select {
	case msgCh <- msg:
	default:
		log.Warnf("Feishu message channel full, dropping message")
	}
}

// sendMessageAPI sends a message via Feishu API
func (g *FeishuGateway) sendMessageAPI(chatID, content string) error {
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	g.tokenMu.RUnlock()
	
	url := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id"

	msg := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    fmt.Sprintf(`{"text":"%s"}`, content),
	}

	jsonBody, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
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
func (g *FeishuGateway) SendText(chatID, text string) error {
	return g.sendMessageAPI(chatID, text)
}

// sendRichMessage sends a rich text or card message
func (g *FeishuGateway) sendRichMessage(chatID, content string) error {
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	g.tokenMu.RUnlock()

	url := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id"

	// Try to parse as JSON (card format)
	var cardContent map[string]interface{}
	if err := json.Unmarshal([]byte(content), &cardContent); err == nil {
		// It's valid JSON, try as interactive card
		msg := map[string]interface{}{
			"receive_id": chatID,
			"msg_type":   "interactive",
			"content":    string(content),
		}

		jsonBody, _ := json.Marshal(msg)
		req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send card message: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("send card error (%d): %s", resp.StatusCode, string(body))
		}
		return nil
	}

	// If not JSON, try as post (rich text)
	paragraphs := []map[string]string{
		{"text": content},
	}
	return g.SendRichText(chatID, paragraphs)
}

// SendRichText sends a rich text message with paragraphs
func (g *FeishuGateway) SendRichText(chatID string, paragraphs []map[string]string) error {
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	g.tokenMu.RUnlock()
	
	url := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id"
	
	// Build post content
	postContent := make([]interface{}, len(paragraphs))
	for i, p := range paragraphs {
		postContent[i] = map[string]interface{}{
			"tag": "text",
			"text": p["text"],
		}
		if p["href"] != "" {
			postContent[i] = map[string]interface{}{
				"tag": "a",
				"text": p["text"],
				"href": p["href"],
			}
		}
	}
	
	msg := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "post",
		"content": map[string]interface{}{
			"post": map[string]interface{}{
				"zh_cn": map[string]interface{}{
					"title":   "",
					"content": [][]interface{}{postContent},
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
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

// Reconnect attempts to reconnect with exponential backoff
func (g *FeishuGateway) Reconnect(ctx context.Context) error {
	g.mu.Lock()
	g.currentRetries++
	retryDelay := g.retryDelay * time.Duration(g.currentRetries)
	g.mu.Unlock()
	
	log.Infof("Attempting to reconnect to Feishu (attempt %d, delay %v)", g.currentRetries, retryDelay)
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(retryDelay):
	}
	
	if err := g.Connect(ctx); err != nil {
		if g.currentRetries < g.maxRetries {
			return g.Reconnect(ctx)
		}
		return fmt.Errorf("max retries exceeded")
	}
	
	return nil
}
