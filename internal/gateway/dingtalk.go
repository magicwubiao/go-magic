package gateway

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// DingTalkGateway implements the DingTalk platform handler
type DingTalkGateway struct {
	appKey    string
	appSecret string
	agentID   string
	
	accessToken string
	tokenExpiresAt time.Time
	tokenMu      sync.RWMutex
	
	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool
	
	// Callback config
	callbackPort int
	
	// AES encryption
	aesKey []byte
	
	// Reconnection
	maxRetries     int
	retryDelay     time.Duration
	currentRetries int
}

// NewDingTalkGateway creates a new DingTalk gateway
func NewDingTalkGateway(appKey, appSecret string) *DingTalkGateway {
	return &DingTalkGateway{
		appKey:      appKey,
		appSecret:   appSecret,
		agents:      make(map[string]*AgentSession),
		msgCh:       make(chan Message, 100),
		stopCh:      make(chan struct{}),
		callbackPort: 8080,
		maxRetries:   5,
		retryDelay:   time.Second * 5,
	}
}

// Name returns the platform name
func (g *DingTalkGateway) Name() string {
	return "dingtalk"
}

// Connect establishes connection to DingTalk
func (g *DingTalkGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to DingTalk gateway...")
	
	if err := g.refreshToken(); err != nil {
		log.Errorf("Failed to get DingTalk token: %v", err)
		return err
	}
	
	go g.tokenRefresher()
	go g.startCallbackServer()
	
	log.Info("DingTalk gateway connected")
	return nil
}

// Disconnect closes the connection
func (g *DingTalkGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if !g.running {
		return nil
	}
	
	close(g.stopCh)
	g.running = false
	g.currentRetries = 0
	
	log.Info("DingTalk gateway disconnected")
	return nil
}

// Send sends a message (enhanced to support rich content)
func (g *DingTalkGateway) Send(ctx context.Context, resp Response) error {
	if resp.ChannelID != "" {
		content := resp.Content
		// Check if content is rich text (starts with { or [)
		if strings.HasPrefix(strings.TrimSpace(content), "{") ||
			strings.HasPrefix(strings.TrimSpace(content), "[") {
			// Try to send as rich content
			if err := g.sendRichMessage(resp.ChannelID, content); err != nil {
				log.Warnf("Failed to send rich message, falling back to text: %v", err)
				return g.sendMessage(resp.ChannelID, content)
			}
			return nil
		}
		return g.sendMessage(resp.ChannelID, content)
	}
	return nil
}

// Receive returns a channel of incoming messages
func (g *DingTalkGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *DingTalkGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{Content: "Available commands:\n/help - Show this help\n/stats - Show statistics"}, nil
	case "stats":
		return Response{Content: "Gateway is running"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for DingTalk gateway
func (g *DingTalkGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:     "dingtalk",
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
	token := g.accessToken
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

	// HTTP client is implicit via http.Get in refreshToken
	status.HTTPClientOK = true
	status.Details["http_client_initialized"] = true

	// Check DingTalk API connectivity
	start := time.Now()
	testURL := fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s",
		g.appKey, g.appSecret)

	resp, err := http.Get(testURL)
	if err == nil {
		status.LatencyMs = time.Since(start).Milliseconds()
		resp.Body.Close()
		status.Details["api_reachable"] = true
		status.Details["api_status"] = resp.StatusCode
	} else {
		status.HTTPClientOK = false
		status.Error = fmt.Sprintf("DingTalk API not reachable: %v", err)
	}

	return status
}

// IsConnected checks if connected to DingTalk
func (g *DingTalkGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.accessToken != ""
}

// refreshToken gets a new access token
func (g *DingTalkGateway) refreshToken() error {
	url := fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s",
		g.appKey, g.appSecret)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("token API error: %s", result.ErrMsg)
	}

	g.tokenMu.Lock()
	g.accessToken = result.AccessToken
	g.tokenExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	g.tokenMu.Unlock()

	log.Debugf("DingTalk token refreshed")
	return nil
}

// tokenRefresher periodically refreshes the token
func (g *DingTalkGateway) tokenRefresher() {
	ticker := time.NewTicker(time.Hour)
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
				log.Errorf("Failed to refresh DingTalk token: %v", err)
			}
		}
	}
}

// startCallbackServer starts the HTTP server for callbacks
func (g *DingTalkGateway) startCallbackServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/dingtalk/callback", g.handleCallback)
	
	addr := fmt.Sprintf(":%d", g.callbackPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	
	log.Infof("DingTalk callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("DingTalk callback server error: %v", err)
	}
}

// SetCallbackPort sets the callback server port
func (g *DingTalkGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}

// SetAESKey sets the AES key for callback encryption
func (g *DingTalkGateway) SetAESKey(key string) {
	hash := sha256.Sum256([]byte(key))
	g.aesKey = hash[:32]
}

// handleCallback handles incoming callbacks from DingTalk
func (g *DingTalkGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Check signature for callback verification
	
	// Handle verification challenge
	if r.URL.Query().Get("type") == "verification" {
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
	
	// Decrypt if encrypted
	var msgStr string
	if g.aesKey != nil && len(body) > 0 {
		msgStr, err = g.decryptCallback(body)
		if err != nil {
			log.Errorf("Failed to decrypt callback: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else {
		msgStr = string(body)
	}
	
	// Parse callback event
	var event struct {
		EventType string `json:"EventType"`
		Text      struct {
			Content string `json:"content"`
		} `json:"text"`
		RobotCode string `json:"robotCode"`
		SenderNick string `json:"senderNick"`
		ConversationID string `json:"conversationId"`
		SenderID string `json:"senderStaffId"`
		MsgId string `json:"msgId"`
		CreateAt int64 `json:"createAt"`
	}
	
	if err := json.Unmarshal([]byte(msgStr), &event); err != nil {
		log.Errorf("Failed to parse callback event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	// Handle different event types
	switch event.EventType {
	case "robot", "o2o":
		g.handleMessageEvent(event)
	default:
		log.Debugf("Unhandled event type: %s", event.EventType)
	}
	
	w.WriteHeader(http.StatusOK)
}

// decryptCallback decrypts an encrypted DingTalk callback
func (g *DingTalkGateway) decryptCallback(encrypted []byte) (string, error) {
	// Simplified decryption - in production, implement full AES/CBC decryption
	// DingTalk uses AES/CBC/PKCS5Padding encryption
	
	block, err := aes.NewCipher(g.aesKey)
	if err != nil {
		return "", err
	}
	
	iv := encrypted[:aes.BlockSize]
	encrypted = encrypted[aes.BlockSize:]
	
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(encrypted, encrypted)
	
	// Remove PKCS5 padding
	padding := int(encrypted[len(encrypted)-1])
	encrypted = encrypted[:len(encrypted)-padding]
	
	return string(encrypted), nil
}

// handleMessageEvent processes a message receive event
func (g *DingTalkGateway) handleMessageEvent(event struct {
	EventType string `json:"EventType"`
	Text      struct {
		Content string `json:"content"`
	} `json:"text"`
	RobotCode string `json:"robotCode"`
	SenderNick string `json:"senderNick"`
	ConversationID string `json:"conversationId"`
	SenderID string `json:"senderStaffId"`
	MsgId string `json:"msgId"`
	CreateAt int64 `json:"createAt"`
}) {
	// Only process robot messages
	if event.EventType != "robot" && event.EventType != "o2o" {
		return
	}
	
	msg := Message{
		ID:        event.MsgId,
		Platform:  "dingtalk",
		ChannelID: event.ConversationID,
		UserID:    event.SenderID,
		Content:   event.Text.Content,
		Timestamp: time.Unix(event.CreateAt/1000, 0),
		Metadata: map[string]interface{}{
			"sender_nick": event.SenderNick,
			"robot_code":  event.RobotCode,
		},
	}
	
	g.mu.RLock()
	msgCh := g.msgCh
	g.mu.RUnlock()
	
	select {
	case msgCh <- msg:
	default:
		log.Warnf("DingTalk message channel full, dropping message")
	}
}

// sendMessage sends a message via DingTalk API
func (g *DingTalkGateway) sendMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()
	
	url := fmt.Sprintf("https://oapi.dingtalk.com/topapi/message/corpconversation/send?access_token=%s", token)

	msg := map[string]interface{}{
		"userid_list": userID,
		"msgtype":     "text",
		"agent_id":    g.agentID,
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
func (g *DingTalkGateway) SendText(userID, content string) error {
	return g.sendMessage(userID, content)
}

// sendRichMessage sends a rich text or markdown message
func (g *DingTalkGateway) sendRichMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	// Try to parse as JSON (rich content format)
	var richContent map[string]interface{}
	if err := json.Unmarshal([]byte(content), &richContent); err != nil {
		// Fallback to text
		return g.sendMessage(userID, content)
	}

	// Extract message type, default to text if not specified
	msgType, _ := richContent["msgtype"].(string)
	if msgType == "" {
		msgType = "text"
	}

	url := fmt.Sprintf("https://oapi.dingtalk.com/topapi/message/corpconversation/send?access_token=%s", token)

	msg := map[string]interface{}{
		"userid_list": userID,
		"msgtype":     msgType,
		"agent_id":    g.agentID,
	}

	// Build message content based on type
	switch msgType {
	case "markdown":
		if title, ok := richContent["title"].(string); ok {
			msg["markdown"] = map[string]string{
				"title": title,
				"text":  content,
			}
		} else {
			msg["markdown"] = map[string]string{
				"title": "Message",
				"text":  content,
			}
		}
	case "link":
		if title, ok := richContent["title"].(string); ok {
			msg["link"] = map[string]interface{}{
				"title":       title,
				"text":        richContent["text"],
				"messageUrl":   richContent["messageUrl"],
				"picUrl":      richContent["picUrl"],
			}
		} else {
			msg["link"] = map[string]interface{}{
				"title":     "Message",
				"text":      content,
				"messageUrl": richContent["messageUrl"],
			}
		}
	case "action_card":
		msg["action_card"] = map[string]interface{}{
			"title":          richContent["title"],
			"markdown":       content,
			"single_title":   richContent["single_title"],
			"single_url":     richContent["single_url"],
		}
	default:
		// Default to text
		msg["text"] = map[string]string{
			"content": content,
		}
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
		return fmt.Errorf("failed to send rich message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send rich message error (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// SendToConversation sends a message to a conversation
func (g *DingTalkGateway) SendToConversation(conversationID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()
	
	url := fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", token)

	msg := map[string]interface{}{
		"msgtype": "text",
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

// SetAgentID sets the DingTalk agent ID
func (g *DingTalkGateway) SetAgentID(agentID string) {
	g.agentID = agentID
}

// Reconnect attempts to reconnect with exponential backoff
func (g *DingTalkGateway) Reconnect(ctx context.Context) error {
	g.mu.Lock()
	g.currentRetries++
	retryDelay := g.retryDelay * time.Duration(g.currentRetries)
	g.mu.Unlock()
	
	log.Infof("Attempting to reconnect to DingTalk (attempt %d, delay %v)", g.currentRetries, retryDelay)
	
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
