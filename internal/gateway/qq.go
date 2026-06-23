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

	"github.com/gorilla/websocket"
	"github.com/magicwubiao/go-magic/pkg/log"
)

type QQGateway struct {
	appID     string
	appSecret string
	token     string

	agents  map[string]*AgentSession
	msgCh   chan Message
	mu      sync.RWMutex
	stopCh  chan struct{}
	running bool

	apiBaseURL string
	httpClient *http.Client

	wsConn            *websocket.Conn
	wsMutex           sync.Mutex
	heartbeatInterval time.Duration
	seq               int
	sessionID         string
	shardCount        int
	shardID           int
}

func NewQQGateway(appID, appSecret string) *QQGateway {
	return &QQGateway{
		appID:      appID,
		appSecret:  appSecret,
		agents:     make(map[string]*AgentSession),
		msgCh:      make(chan Message, 100),
		stopCh:     make(chan struct{}),
		apiBaseURL: "https://api.sgroup.qq.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		shardCount: 1,
		shardID:    0,
	}
}

func (g *QQGateway) Name() string {
	return "qq"
}

func (g *QQGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()

	if g.appID == "" || g.appSecret == "" {
		return fmt.Errorf("QQ app_id and app_secret/token are required")
	}

	g.token = g.appSecret
	log.Infof("[QQ] Connecting with appID=%s", g.appID)

	gatewayURL, err := g.getGatewayURL()
	if err != nil {
		return fmt.Errorf("failed to get QQ gateway URL: %w", err)
	}
	log.Infof("[QQ] Gateway URL: %s", gatewayURL)

	if err := g.connectWebSocket(gatewayURL); err != nil {
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	g.mu.Lock()
	g.running = true
	g.mu.Unlock()

	go g.listenWebSocket()

	log.Info("[QQ] Gateway connected and listening for events")
	return nil
}

func (g *QQGateway) getGatewayURL() (string, error) {
	url := fmt.Sprintf("%s/gateway", g.apiBaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s.%s", g.appID, g.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gateway API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse gateway response: %w", err)
	}
	if result.URL == "" {
		return "", fmt.Errorf("empty gateway URL in response")
	}
	return result.URL, nil
}

func (g *QQGateway) connectWebSocket(url string) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}

	g.wsMutex.Lock()
	g.wsConn = conn
	g.wsMutex.Unlock()

	return nil
}

func (g *QQGateway) listenWebSocket() {
	defer func() {
		g.wsMutex.Lock()
		if g.wsConn != nil {
			g.wsConn.Close()
			g.wsConn = nil
		}
		g.wsMutex.Unlock()
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
	}()

	for {
		select {
		case <-g.stopCh:
			return
		default:
		}

		g.wsMutex.Lock()
		conn := g.wsConn
		g.wsMutex.Unlock()
		if conn == nil {
			log.Warn("[QQ] WebSocket connection lost, attempting reconnect...")
			if err := g.reconnect(); err != nil {
				log.Errorf("[QQ] Reconnect failed: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			continue
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Warnf("[QQ] WebSocket read error: %v", err)
			}
			time.Sleep(2 * time.Second)
			continue
		}

		g.handleWSMessage(msg)
	}
}

func (g *QQGateway) handleWSMessage(data []byte) {
	var event struct {
		Op int             `json:"op"`
		S  int             `json:"s"`
		T  string          `json:"t"`
		D  json.RawMessage `json:"d"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		log.Errorf("[QQ] Failed to parse WebSocket message: %v", err)
		return
	}

	if event.S > 0 {
		g.seq = event.S
	}

	switch event.Op {
	case 10:
		var hello struct {
			HeartbeatInterval int `json:"heartbeat_interval"`
		}
		if err := json.Unmarshal(event.D, &hello); err == nil {
			g.heartbeatInterval = time.Duration(hello.HeartbeatInterval) * time.Millisecond
			log.Infof("[QQ] Received Hello, heartbeat interval: %v", g.heartbeatInterval)
		}
		g.sendIdentify()
		go g.heartbeatLoop()

	case 11:
		log.Debug("[QQ] Heartbeat ACK received")

	case 0:
		g.handleDispatchEvent(event.T, event.D)

	case 7:
		log.Info("[QQ] Reconnect requested by server")
		g.reconnect()

	case 9:
		log.Warn("[QQ] Invalid session, re-identifying")
		time.Sleep(2 * time.Second)
		g.sendIdentify()

	default:
		log.Debugf("[QQ] Unknown op code: %d, type: %s", event.Op, event.T)
	}
}

func (g *QQGateway) sendIdentify() {
	payload := map[string]interface{}{
		"op": 2,
		"d": map[string]interface{}{
			"token":   fmt.Sprintf("Bot %s.%s", g.appID, g.token),
			"intents": 1<<30 | 1<<0 | 1<<1 | 1<<25 | 1<<9,
			"shards":  []int{g.shardID, g.shardCount},
			"properties": map[string]string{
				"os":      "linux",
				"browser": "go-magic",
				"device":  "go-magic",
			},
		},
	}

	g.sendWSMessage(payload)
	log.Info("[QQ] Identify sent")
}

func (g *QQGateway) sendWSMessage(payload interface{}) {
	g.wsMutex.Lock()
	defer g.wsMutex.Unlock()

	if g.wsConn == nil {
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("[QQ] Failed to marshal WS payload: %v", err)
		return
	}

	if err := g.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Errorf("[QQ] Failed to write WS message: %v", err)
	}
}

func (g *QQGateway) heartbeatLoop() {
	if g.heartbeatInterval == 0 {
		g.heartbeatInterval = 30 * time.Second
	}

	ticker := time.NewTicker(g.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.sendHeartbeat()
		}
	}
}

func (g *QQGateway) sendHeartbeat() {
	payload := map[string]interface{}{
		"op": 1,
		"d":  g.seq,
	}
	g.sendWSMessage(payload)
	log.Debugf("[QQ] Heartbeat sent, seq=%d", g.seq)
}

func (g *QQGateway) reconnect() error {
	g.wsMutex.Lock()
	if g.wsConn != nil {
		g.wsConn.Close()
		g.wsConn = nil
	}
	g.wsMutex.Unlock()

	time.Sleep(2 * time.Second)

	gatewayURL, err := g.getGatewayURL()
	if err != nil {
		return fmt.Errorf("failed to get gateway URL for reconnect: %w", err)
	}

	if err := g.connectWebSocket(gatewayURL); err != nil {
		return err
	}

	return nil
}

func (g *QQGateway) handleDispatchEvent(eventType string, data json.RawMessage) {
	switch eventType {
	case "READY":
		var ready struct {
			Version   int    `json:"version"`
			SessionID string `json:"session_id"`
			User      struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Bot      bool   `json:"bot"`
			} `json:"user"`
			Shard []int `json:"shard"`
		}
		if err := json.Unmarshal(data, &ready); err == nil {
			g.sessionID = ready.SessionID
			log.Infof("[QQ] Ready! Bot: %s (ID: %s), session: %s", ready.User.Username, ready.User.ID, ready.SessionID)
		}

	case "MESSAGE_CREATE", "AT_MESSAGE_CREATE":
		g.handleMessageEvent(data)

	case "GUILD_MEMBER_ADD":
		log.Debug("[QQ] Guild member added")

	default:
		log.Debugf("[QQ] Dispatch event: %s", eventType)
	}
}

func (g *QQGateway) handleMessageEvent(data json.RawMessage) {
	var msgData struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Author    struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"author"`
		Timestamp string `json:"timestamp"`
	}

	if err := json.Unmarshal(data, &msgData); err != nil {
		log.Errorf("[QQ] Failed to parse message: %v", err)
		return
	}

	if msgData.Author.Bot {
		return
	}

	content := strings.TrimSpace(msgData.Content)
	if content == "" {
		return
	}

	msg := Message{
		ID:        msgData.ID,
		Platform:  "qq",
		ChannelID: msgData.ChannelID,
		UserID:    msgData.Author.ID,
		Content:   content,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"guild_id":   msgData.GuildID,
			"author":     msgData.Author.Username,
			"author_bot": msgData.Author.Bot,
		},
	}

	select {
	case g.msgCh <- msg:
		log.Debugf("[QQ] Message received: id=%s from=%s", msg.ID, msg.UserID)
	default:
		log.Warnf("[QQ] Message channel full, dropping: %s", msg.ID)
	}
}

func (g *QQGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	close(g.stopCh)

	g.wsMutex.Lock()
	if g.wsConn != nil {
		g.wsConn.Close()
		g.wsConn = nil
	}
	g.wsMutex.Unlock()

	g.running = false
	log.Info("[QQ] Gateway disconnected")
	return nil
}

func (g *QQGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.wsConn != nil
}

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

func (g *QQGateway) SendText(channelID string, text string) error {
	if g.token == "" {
		return fmt.Errorf("QQ access token not available")
	}

	url := fmt.Sprintf("%s/channels/%s/messages", g.apiBaseURL, channelID)

	if len(text) > 500 {
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

func (g *QQGateway) sendQQMessage(url, content string) error {
	body := map[string]interface{}{
		"content":  content,
		"msg_type": 0,
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bot %s.%s", g.appID, g.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to send message: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
		return fmt.Errorf("QQ API error: code %d, message: %s", result.Code, result.Message)
	}

	return nil
}

func (g *QQGateway) Receive() <-chan Message {
	return g.msgCh
}

func (g *QQGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - QQ\n\n" +
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

func (g *QQGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:  "qq",
		Connected: g.IsConnected(),
		Status:    "healthy",
		Details:   make(map[string]interface{}),
		Platforms: make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "qq",
		Status: "connected",
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Platforms["qq"] = platformStatus
		return status
	}

	if g.httpClient != nil {
		status.HTTPClientOK = true
		status.Details["http_client_initialized"] = true
	} else {
		status.HTTPClientOK = false
		status.Error = "HTTP client not initialized"
		return status
	}

	if g.token != "" {
		status.TokenValid = true
		status.Details["token_available"] = true
	} else {
		status.TokenValid = false
		status.Details["token_available"] = false
		platformStatus.Error = "No access token"
		status.Status = "error"
	}

	status.Details["seq"] = g.seq
	status.Details["session_id"] = g.sessionID
	status.Platforms["qq"] = platformStatus

	return status
}

func (g *QQGateway) SetCallbackPort(port int) {
}
