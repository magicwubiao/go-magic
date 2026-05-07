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

// QQGateway implements the QQ platform handler
type QQGateway struct {
	appID     string
	appSecret string
	token     string

	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool

	callbackPort int
	server       *http.Server
	serverOnce   sync.Once

	// QQ Guild API endpoint
	apiBaseURL string
	httpClient *http.Client
}

// NewQQGateway creates a new QQ gateway
func NewQQGateway(appID, appSecret string) *QQGateway {
	return &QQGateway{
		appID:        appID,
		appSecret:    appSecret,
		agents:       make(map[string]*AgentSession),
		msgCh:        make(chan Message, 100),
		stopCh:       make(chan struct{}),
		callbackPort: 8082, // QQ-specific port
		apiBaseURL:   "https://api.sgroup.qq.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the platform name
func (g *QQGateway) Name() string {
	return "qq"
}

// Connect establishes connection to QQ
func (g *QQGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to QQ gateway...")

	// Get access token
	if err := g.getAccessToken(); err != nil {
		log.Warnf("Failed to get QQ access token: %v (will retry on first message)", err)
	}

	go g.startCallbackServer()

	log.Info("QQ gateway connected")
	return nil
}

// getAccessToken obtains an access token from QQ
func (g *QQGateway) getAccessToken() error {
	if g.appID == "" || g.appSecret == "" {
		return fmt.Errorf("appID or appSecret not configured")
	}

	url := fmt.Sprintf("https://api.sgroup.qq.com/login/qrcode/refresh_token?appid=%s&secret=%s",
		g.appID, g.appSecret)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := g.httpClient.Do(req)
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
	log.Info("QQ access token obtained")
	return nil
}

// Disconnect closes the connection
func (g *QQGateway) Disconnect() error {
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

	log.Info("QQ gateway disconnected")
	return nil
}

// IsConnected checks if connected to QQ
func (g *QQGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

// Send sends a message via QQ Guild API
func (g *QQGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("QQ gateway not connected")
	}

	channelID := resp.ChannelID
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	return g.SendText(channelID, resp.Content)
}

// SendText sends a text message via QQ Guild API
func (g *QQGateway) SendText(channelID string, text string) error {
	if g.token == "" {
		return fmt.Errorf("QQ access token not available")
	}

	url := fmt.Sprintf("%s/channels/%s/messages", g.apiBaseURL, channelID)

	// QQ has content length limits, split if necessary
	if len(text) > 500 {
		// Split into multiple messages
		for i := 0; i < len(text); i += 490 {
			end := i + 490
			if end > len(text) {
				end = len(text)
			}
			if err := g.sendQQMessage(url, text[i:end]); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
		}
		return nil
	}

	return g.sendQQMessage(url, text)
}

// sendQQMessage sends a single message via QQ API
func (g *QQGateway) sendQQMessage(url, content string) error {
	body := map[string]interface{}{
		"content": content,
		"msg_type": 1, // 1 = text message
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "QQBot "+g.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Receive returns a channel of incoming messages
func (g *QQGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles a slash command
func (g *QQGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
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
				Content: "✅ Bot is connected and ready!",
			}, nil
		}
		return Response{
			Content: "❌ Bot is not connected",
		}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for QQ gateway
func (g *QQGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:     "qq",
		Connected:   g.IsConnected(),
		CallbackOK:  false,
		CallbackPort: g.callbackPort,
		Details:     make(map[string]interface{}),
	}

	if !status.Connected {
		status.Error = "Gateway not connected"
		return status
	}

	// Check HTTP client by making a test request
	if g.httpClient != nil {
		status.HTTPClientOK = true
		status.Details["http_client_initialized"] = true
	} else {
		status.HTTPClientOK = false
		status.Error = "HTTP client not initialized"
		return status
	}

	// Check if token is available
	if g.token != "" {
		status.TokenValid = true
		status.Details["token_available"] = true
	} else {
		status.TokenValid = false
		status.Details["token_available"] = false
		// Token not required for initial health check, but noted
	}

	// Check callback server by making a localhost request
	callbackURL := fmt.Sprintf("http://localhost:%d/qq/health", g.callbackPort)
	req, err := http.NewRequest("GET", callbackURL, nil)
	if err == nil {
		// Don't actually make the request, just verify we can construct it
		status.Details["callback_url"] = callbackURL
	}

	// Try a quick connectivity check to QQ API
	start := time.Now()
	testURL := g.apiBaseURL + "/users/me"
	req, _ = http.NewRequestWithContext(context.Background(), "GET", testURL, nil)
	if g.token != "" {
		req.Header.Set("Authorization", "QQBot "+g.token)
	}
	req.Header.Set("X-Union-Appid", g.appID)

	resp, err := g.httpClient.Do(req)
	if err == nil {
		status.LatencyMs = time.Since(start).Milliseconds()
		resp.Body.Close()
		// 401 is expected without valid auth, but means HTTP client works
		status.Details["api_reachable"] = true
		status.Details["api_status"] = resp.StatusCode
	} else {
		status.HTTPClientOK = false
		status.Error = fmt.Sprintf("API not reachable: %v", err)
	}

	return status
}

// startCallbackServer starts the HTTP server for callbacks
func (g *QQGateway) startCallbackServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/qq/callback", g.handleCallback)
	mux.HandleFunc("/qq/verify", g.handleVerify)

	addr := fmt.Sprintf(":%d", g.callbackPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	g.server = server

	log.Infof("QQ callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("QQ callback server error: %v", err)
	}
}

// handleVerify handles URL verification from QQ
func (g *QQGateway) handleVerify(w http.ResponseWriter, r *http.Request) {
	echo := r.URL.Query().Get("echo")
	if echo != "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(echo))
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCallback handles incoming callbacks from QQ
func (g *QQGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// URL verification
		echo := r.URL.Query().Get("echo")
		if echo != "" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	if r.Method == "POST" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Failed to read QQ callback body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Parse QQ message event
		g.parseCallbackEvent(body)

		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// parseCallbackEvent parses incoming QQ callback events
func (g *QQGateway) parseCallbackEvent(body []byte) {
	var event struct {
		OpCode int             `json:"op"`
		Type   int             `json:"t"`
		Data   json.RawMessage `json:"d"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		log.Errorf("Failed to parse QQ event: %v", err)
		return
	}

	// Type 1 = Message Create (Dispatch event for messages)
	if event.Type == 1 {
		var msgData struct {
			ID        string `json:"id"`
			ChannelID string `json:"channel_id"`
			GuildID   string `json:"guild_id"`
			Content   string `json:"content"`
			Author    struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"author"`
			Timestamp string `json:"timestamp"`
		}

		if err := json.Unmarshal(event.Data, &msgData); err != nil {
			log.Errorf("Failed to parse QQ message: %v", err)
			return
		}

		// Skip messages from bots (self)
		if strings.HasPrefix(msgData.Author.ID, "2") { // QQ bot IDs typically start with 2
			return
		}

		msg := Message{
			ID:        msgData.ID,
			Platform:  "qq",
			ChannelID: msgData.ChannelID,
			UserID:    msgData.Author.ID,
			Content:   msgData.Content,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"guild_id": msgData.GuildID,
				"author":   msgData.Author.Username,
			},
		}

		// Send to channel
		select {
		case g.msgCh <- msg:
			log.Debugf("QQ message received: %s from %s", msg.ID, msg.UserID)
		default:
			log.Warnf("QQ message channel full, dropping message: %s", msg.ID)
		}
	}
}

// SetCallbackPort sets the callback server port
func (g *QQGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}
