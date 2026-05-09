package gateway

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// WeComAppGateway implements WeCom (Enterprise WeChat) application message mode
// This mode requires:
// - A verified enterprise account (已认证企业)
// - Application credentials (corpID + secret)
// - Properly configured callback URL in WeCom admin console
type WeComAppGateway struct {
	corpID         string
	agentID        string
	secret         string
	encodingAESKey string

	accessToken    string
	tokenMu        sync.RWMutex
	tokenExpiresAt time.Time

	agents  map[string]*AgentSession
	msgCh   chan Message
	mu      sync.RWMutex
	stopCh  chan struct{}
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

// NewWeComAppGateway creates a new WeCom application message mode gateway
func NewWeComAppGateway(corpID, agentID, secret string) *WeComAppGateway {
	return &WeComAppGateway{
		corpID:       corpID,
		agentID:      agentID,
		secret:       secret,
		agents:       make(map[string]*AgentSession),
		msgCh:        make(chan Message, 100),
		stopCh:       make(chan struct{}),
		callbackPort: 8080,
		maxRetries:   5,
		retryDelay:   time.Second * 5,
	}
}

// Name returns the platform name
func (g *WeComAppGateway) Name() string {
	return "wecom_app"
}

// Connect establishes connection to WeCom
func (g *WeComAppGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to WeCom App gateway...")

	if err := g.refreshToken(); err != nil {
		log.Errorf("Failed to get WeCom token: %v", err)
		return err
	}

	go g.tokenRefresher()
	go g.startCallbackServer()

	log.Info("WeCom App gateway connected")
	return nil
}

// Disconnect closes the connection
func (g *WeComAppGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	close(g.stopCh)
	g.running = false
	g.currentRetries = 0

	log.Info("WeCom App gateway disconnected")
	return nil
}

// Send sends a message (enhanced to support rich content)
func (g *WeComAppGateway) Send(ctx context.Context, resp Response) error {
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
func (g *WeComAppGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *WeComAppGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{Content: "Available commands:\n/help - Show this help\n/stats - Show statistics"}, nil
	case "stats":
		return Response{Content: "Gateway is running"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for WeCom App gateway
func (g *WeComAppGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:     "wecom_app",
		Connected:    g.IsConnected(),
		Status:       "healthy",
		CallbackPort: g.callbackPort,
		Details:      make(map[string]interface{}),
		Platforms:    make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "wecom_app",
		Status: "connected",
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Platforms["wecom_app"] = platformStatus
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
		platformStatus.Error = "No access token"
		status.Status = "error"
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

	status.Details["mode"] = "app"
	status.Details["callback_port"] = g.callbackPort
	status.Platforms["wecom_app"] = platformStatus

	return status
}

// IsConnected checks if connected to WeCom
func (g *WeComAppGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.accessToken != ""
}

// refreshToken gets a new access token
func (g *WeComAppGateway) refreshToken() error {
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		g.corpID, g.secret)

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

	log.Debugf("WeCom token refreshed")
	return nil
}

// tokenRefresher periodically refreshes the token
func (g *WeComAppGateway) tokenRefresher() {
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
				log.Errorf("Failed to refresh WeCom token: %v", err)
			}
		}
	}
}

// startCallbackServer starts the HTTP server for callbacks
func (g *WeComAppGateway) startCallbackServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/wecom/callback", g.handleCallback)

	addr := fmt.Sprintf(":%d", g.callbackPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Infof("WeCom App callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("WeCom App callback server error: %v", err)
	}
}

// SetCallbackPort sets the callback server port
func (g *WeComAppGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}

// SetAESKey sets the AES key for callback encryption
func (g *WeComAppGateway) SetAESKey(key string) {
	g.encodingAESKey = key
	g.aesKey = []byte(key + "=")[:32]
}

// handleCallback handles incoming callbacks from WeCom
func (g *WeComAppGateway) handleCallback(w http.ResponseWriter, r *http.Request) {

	echostr := r.URL.Query().Get("echostr")

	// Handle URL verification
	if echostr != "" {
		decoded, err := base64.StdEncoding.DecodeString(echostr)
		if err != nil {
			log.Errorf("Failed to decode echostr: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if g.encodingAESKey != "" {
			decrypted, err := g.decryptWeCom(decoded)
			if err != nil {
				log.Errorf("Failed to decrypt echostr: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Write(decrypted)
		} else {
			w.Write(decoded)
		}
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

	// Parse XML callback
	var callback struct {
		XMLName      xml.Name `xml:"xml"`
		Encrypt      string   `xml:"Encrypt"`
		MsgSignature string   `xml:"MsgSignature"`
		TimeStamp    string   `xml:"TimeStamp"`
		Nonce        string   `xml:"Nonce"`
		Content      string   `xml:"Content"`
	}

	if err := xml.Unmarshal(body, &callback); err != nil {
		log.Errorf("Failed to parse callback XML: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Decrypt if needed
	var msgStr string
	if callback.Encrypt != "" && g.encodingAESKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(callback.Encrypt)
		if err != nil {
			log.Errorf("Failed to decode encrypt: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		decrypted, err := g.decryptWeCom(decoded)
		if err != nil {
			log.Errorf("Failed to decrypt callback: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		msgStr = string(decrypted)
	} else {
		msgStr = callback.Content
	}

	// Parse decrypted message
	var event struct {
		MsgType      string `xml:"MsgType"`
		Content      string `xml:"Content"`
		FromUserName string `xml:"FromUserName"`
		ToUserName   string `xml:"ToUserName"`
		MsgId        string `xml:"MsgId"`
		AgentID      string `xml:"AgentID"`
		Event        string `xml:"Event"`
		CreateTime   int64  `xml:"CreateTime"`
	}

	if err := xml.Unmarshal([]byte(msgStr), &event); err != nil {
		log.Errorf("Failed to parse callback event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle different message types
	switch event.MsgType {
	case "text", "event":
		g.handleMessageEvent(event)
	default:
		log.Debugf("Unhandled message type: %s", event.MsgType)
	}

	// Respond success
	w.WriteHeader(http.StatusOK)
}

// decryptWeCom decrypts a WeCom callback
func (g *WeComAppGateway) decryptWeCom(encrypted []byte) ([]byte, error) {
	if len(g.aesKey) != 32 {
		return nil, fmt.Errorf("invalid AES key length")
	}

	block, err := aes.NewCipher(g.aesKey)
	if err != nil {
		return nil, err
	}

	iv := encrypted[:aes.BlockSize]
	encrypted = encrypted[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(encrypted, encrypted)

	// Remove PKCS5 padding
	padding := int(encrypted[len(encrypted)-1])
	encrypted = encrypted[:len(encrypted)-padding]

	// Remove random bytes and appid from beginning
	// Format: random(16) + msg_len(4) + msg + appid
	msgLen := int(encrypted[16])<<24 | int(encrypted[17])<<16 | int(encrypted[18])<<8 | int(encrypted[19])
	msg := encrypted[20 : 20+msgLen]

	return msg, nil
}

// handleMessageEvent processes a message receive event
func (g *WeComAppGateway) handleMessageEvent(event struct {
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	FromUserName string `xml:"FromUserName"`
	ToUserName   string `xml:"ToUserName"`
	MsgId        string `xml:"MsgId"`
	AgentID      string `xml:"AgentID"`
	Event        string `xml:"Event"`
	CreateTime   int64  `xml:"CreateTime"`
}) {
	// Only process text messages or click events
	if event.MsgType != "text" && event.Event != "click" {
		return
	}

	// Check if this is for our agent
	if event.AgentID != "" && event.AgentID != g.agentID {
		return
	}

	msg := Message{
		ID:        event.MsgId,
		Platform:  "wecom_app",
		ChannelID: event.FromUserName, // User ID is channel ID in this context
		UserID:    event.FromUserName,
		Content:   event.Content,
		Timestamp: time.Unix(event.CreateTime, 0),
		Metadata: map[string]interface{}{
			"to_user":  event.ToUserName,
			"agent_id": event.AgentID,
			"event":    event.Event,
		},
	}

	// 企业微信事件消息（如 click 菜单事件）的 MsgId 可能为空
	// 此时使用 "wecom_" + 时间戳 + FromUserName 生成唯一ID
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("wecom_%d_%s", event.CreateTime, event.FromUserName)
	}

	g.mu.RLock()
	msgCh := g.msgCh
	g.mu.RUnlock()

	select {
	case msgCh <- msg:
	default:
		log.Warnf("WeCom App message channel full, dropping message")
	}
}

// sendMessage sends a message via WeCom API
func (g *WeComAppGateway) sendMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)

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
func (g *WeComAppGateway) SendText(userID, content string) error {
	return g.sendMessage(userID, content)
}

// sendRichMessage sends a rich text or markdown message
func (g *WeComAppGateway) sendRichMessage(userID, content string) error {
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

// SendToUser sends a message to a user
func (g *WeComAppGateway) SendToUser(userID, content string) error {
	return g.sendMessage(userID, content)
}

// Reconnect attempts to reconnect with exponential backoff
func (g *WeComAppGateway) Reconnect(ctx context.Context) error {
	g.mu.Lock()
	g.currentRetries++
	retryDelay := g.retryDelay * time.Duration(g.currentRetries)
	g.mu.Unlock()

	log.Infof("Attempting to reconnect to WeCom App (attempt %d, delay %v)", g.currentRetries, retryDelay)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(retryDelay):
	}

	if err := g.Connect(ctx); err != nil {
		if g.currentRetries < g.maxRetries {
			return g.Reconnect(ctx)
		}
		return err
	}
	return nil
}

// GetAccessToken returns the current access token
func (g *WeComAppGateway) GetAccessToken() string {
	g.tokenMu.RLock()
	defer g.tokenMu.RUnlock()
	return g.accessToken
}
