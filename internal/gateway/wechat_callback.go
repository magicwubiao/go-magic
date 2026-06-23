package gateway

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"encoding/json"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// WeChatCallbackGateway implements the WeChat Official Account callback mode
// This mode requires:
// - A verified service account (已认证服务号)
// - A public IP address for the callback server
// - Properly configured callback URL in WeChat admin console
type WeChatCallbackGateway struct {
	appID          string
	appSecret      string
	token          string
	tokenExpiresAt time.Time
	encodingAESKey string

	agents  map[string]*AgentSession
	msgCh   chan Message
	mu      sync.RWMutex
	stopCh  chan struct{}
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
	// Media fields for various message types
	MediaID      string `xml:"MediaId,omitempty"`      // For image, voice, video, shortvideo
	PicURL       string `xml:"PicUrl,omitempty"`       // For image
	Format       string `xml:"Format,omitempty"`       // For voice (amr, mp3)
	Recognition  string `xml:"Recognition,omitempty"`  // For voice (ASR result)
	ThumbMediaID string `xml:"ThumbMediaId,omitempty"` // For video, music
	LocationX    string `xml:"Location_X,omitempty"`
	LocationY    string `xml:"Location_Y,omitempty"`
	Scale        string `xml:"Scale,omitempty"`
	Label        string `xml:"Label,omitempty"`
	Title        string `xml:"Title,omitempty"`
	Description  string `xml:"Description,omitempty"`
	URL          string `xml:"Url,omitempty"`
}

// NewWeChatCallbackGateway creates a new WeChat callback mode gateway
func NewWeChatCallbackGateway(appID, appSecret, token, aesKey string) *WeChatCallbackGateway {
	return &WeChatCallbackGateway{
		appID:          appID,
		appSecret:      appSecret,
		token:          token,
		encodingAESKey: aesKey,
		agents:         make(map[string]*AgentSession),
		msgCh:          make(chan Message, 100),
		stopCh:         make(chan struct{}),
		callbackPort:   8083, // WeChat-specific port
		apiBaseURL:     "https://api.weixin.qq.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the platform name
func (g *WeChatCallbackGateway) Name() string {
	return "wechat_callback"
}

// Connect establishes connection to WeChat callback server
func (g *WeChatCallbackGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to WeChat callback gateway...")

	// Get access token
	if err := g.getAccessToken(); err != nil {
		log.Warnf("Failed to get WeChat access token: %v (will retry on first message)", err)
	}

	go g.startCallbackServer()

	log.Info("WeChat callback gateway connected")
	return nil
}

// getAccessToken obtains an access token from WeChat
func (g *WeChatCallbackGateway) getAccessToken() error {
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

	g.mu.Lock()
	g.token = result.AccessToken
	g.tokenExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	g.mu.Unlock()

	log.Info("WeChat access token obtained")
	return nil
}

// downloadMedia downloads a media file from WeChat using media_id
func (g *WeChatCallbackGateway) downloadMedia(mediaID, mediaType string) (string, error) {
	g.mu.RLock()
	token := g.token
	g.mu.RUnlock()

	if token == "" {
		return "", fmt.Errorf("WeChat access token not available")
	}

	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/media/get?access_token=%s&media_id=%s", token, mediaID)

	resp, err := g.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download media: status %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	var ext string
	switch mediaType {
	case "image":
		if strings.Contains(contentType, "png") {
			ext = "png"
		} else if strings.Contains(contentType, "gif") {
			ext = "gif"
		} else {
			ext = "jpg"
		}
	case "voice":
		if strings.Contains(contentType, "mpeg") || strings.Contains(contentType, "mp3") {
			ext = "mp3"
		} else {
			ext = "amr"
		}
	case "video":
		ext = "mp4"
	default:
		ext = "bin"
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read media data: %w", err)
	}

	if len(data) == 0 {
		return "", fmt.Errorf("empty media data")
	}

	// Save to disk
	dir := filepath.Join(config.GetMagicHome(), "wechat", "media", mediaType)
	os.MkdirAll(dir, 0755)

	filename := fmt.Sprintf("%s_%s.%s", mediaID, mediaType, ext)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to save media: %w", err)
	}

	return path, nil
}

// Disconnect closes the connection
func (g *WeChatCallbackGateway) Disconnect() error {
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

	log.Info("WeChat callback gateway disconnected")
	return nil
}

// IsConnected checks if connected to WeChat
func (g *WeChatCallbackGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

// Send sends a message via WeChat API
func (g *WeChatCallbackGateway) Send(ctx context.Context, resp Response) error {
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
func (g *WeChatCallbackGateway) SendText(openID, text string) error {
	g.mu.RLock()
	token := g.token
	g.mu.RUnlock()

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
			if err := g.sendWeChatMessage(url, openID, text[i:end]); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
		}
		return nil
	}

	return g.sendWeChatMessage(url, openID, text)
}

// sendWeChatMessage sends a single text message via WeChat API
func (g *WeChatCallbackGateway) sendWeChatMessage(url, openID, content string) error {
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
func (g *WeChatCallbackGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *WeChatCallbackGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
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
				"/kanban - Kanban board",
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

// CheckHealth returns detailed health status for WeChat callback gateway
func (g *WeChatCallbackGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:     "wechat_callback",
		Connected:    g.IsConnected(),
		Status:       "healthy",
		CallbackPort: g.callbackPort,
		Details:      make(map[string]interface{}),
		Platforms:    make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "wechat_callback",
		Status: "connected",
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Platforms["wechat_callback"] = platformStatus
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

	status.Details["callback_port"] = g.callbackPort
	status.Details["mode"] = "callback"
	status.Platforms["wechat_callback"] = platformStatus

	return status
}

// startCallbackServer starts the HTTP server for callbacks
func (g *WeChatCallbackGateway) startCallbackServer() {
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
func (g *WeChatCallbackGateway) handleVerify(w http.ResponseWriter, r *http.Request) {
	// WeChat verification GET request
	signature := r.URL.Query().Get("signature")
	echostr := r.URL.Query().Get("echostr")
	timestamp := r.URL.Query().Get("timestamp")
	nonce := r.URL.Query().Get("nonce")

	if signature != "" && echostr != "" {
		// Verify signature
		if g.verifySignature(signature, timestamp, nonce) {
			// 微信验证要求直接返回原始的 echostr 字符串（明文）
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(echostr))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature verifies WeChat callback signature.
// 微信公众平台使用 SHA1 签名验证算法
func (g *WeChatCallbackGateway) verifySignature(signature, timestamp, nonce string) bool {
	strs := sort.StringSlice{g.token, timestamp, nonce}
	sort.Strings(strs)
	str := strings.Join(strs, "")
	h := sha1.Sum([]byte(str))
	return fmt.Sprintf("%x", h) == signature
}

// handleCallback handles incoming callbacks from WeChat
func (g *WeChatCallbackGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
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
func (g *WeChatCallbackGateway) parseCallbackEvent(body []byte) {
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

	// Determine message type and handle accordingly
	var content string
	var mediaURLs []MediaAttachment

	switch msg.MsgType {
	case "text":
		content = msg.Content

	case "image":
		content = "[用户发送了一张图片]"
		if msg.MediaID != "" {
			if path, err := g.downloadMedia(msg.MediaID, "image"); err == nil {
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:    "image",
					URL:     path,
					Caption: msg.PicURL,
				})
				// Image downloaded successfully, set default content
				content = "[用户发送了一张图片]"
			} else {
				log.Debugf("Failed to download WeChat image: %v", err)
				content = "[图片]"
			}
		}

	case "voice":
		content = "[语音消息]"
		// Use ASR result if available
		if msg.Recognition != "" {
			content = msg.Recognition
		} else if msg.MediaID != "" {
			if path, err := g.downloadMedia(msg.MediaID, "voice"); err == nil {
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "audio",
					URL:      path,
					MimeType: "audio/" + msg.Format,
				})
			} else {
				log.Debugf("Failed to download WeChat voice: %v", err)
				content = "[Voice message]"
			}
		} else {
			content = "[Voice message]"
		}

	case "video", "shortvideo":
		content = "[用户发送了一个视频]"
		if msg.MediaID != "" {
			if path, err := g.downloadMedia(msg.MediaID, "video"); err == nil {
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:    "video",
					URL:     path,
					Caption: msg.Description,
				})
			} else {
				log.Debugf("Failed to download WeChat video: %v", err)
				content = "[Video]"
			}
		} else {
			content = "[Video]"
		}

	case "location":
		content = fmt.Sprintf("[Location: %s, %s] %s", msg.LocationX, msg.LocationY, msg.Label)

	case "link":
		content = fmt.Sprintf("[Link: %s] %s - %s", msg.Title, msg.Description, msg.URL)

	case "music":
		content = fmt.Sprintf("[Music: %s] %s", msg.Title, msg.Description)

	default:
		// For unknown types, just log and skip
		log.Debugf("Ignoring WeChat message type: %s", msg.MsgType)
		return
	}

	msgData := Message{
		ID:        msg.MsgID,
		Platform:  "wechat_callback",
		ChannelID: msg.FromUserName, // OpenID
		UserID:    msg.FromUserName,
		Content:   content,
		Timestamp: time.Unix(msg.CreateTime, 0),
		Metadata: map[string]interface{}{
			"msg_type": msg.MsgType,
			"to_user":  msg.ToUserName,
		},
		MediaURLs: mediaURLs,
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
func (g *WeChatCallbackGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}

// GetAccessToken returns the current access token
func (g *WeChatCallbackGateway) GetAccessToken() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.token
}

// RefreshToken forces a token refresh
func (g *WeChatCallbackGateway) RefreshToken() error {
	return g.getAccessToken()
}
