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

const (
	QQAPIBase     = "https://api.q.qq.com"
	QQSandboxBase = "https://sandbox.api.q.qq.com"

	QQOpDispatch     = 0
	QQOpHeartbeat    = 1
	QQOpIdentify     = 2
	QQOpResume       = 6
	QQOpReconnect    = 7
	QQOpInvalidSess  = 9
	QQOpHello        = 10
	QQOpHeartbeatACK = 11

	QQIntentGuildMessages   = 1 << 0
	QQIntentGuildAtMessages = 1 << 9
	QQIntentDirectMessages  = 1 << 12
	QQIntentGroupMessages   = 1 << 25
	QQIntentGuildMembers    = 1 << 1
	QQIntentAudit           = 1 << 30
)

type QQGateway struct {
	*BasePlatform

	appID     string
	appSecret string

	accessToken string
	tokenExpiry time.Time
	tokenMu     sync.RWMutex

	sandbox    bool
	intent     int
	apiBaseURL string
	httpClient *http.Client

	wsConn            *websocket.Conn
	wsMutex           sync.Mutex
	heartbeatInterval time.Duration
	seq               int
	sessionID         string
	shardCount        int
	shardID           int

	userID   string
	userName string

	heartbeatTicker *time.Ticker
	heartbeatDone   chan struct{}

	messageSourceCache map[string]string
	sourceCacheMu      sync.RWMutex
}

func NewQQGateway(appID, appSecret string, sandbox bool, intent int) *QQGateway {
	baseURL := QQAPIBase
	if sandbox {
		baseURL = QQSandboxBase
	}

	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	if intent == 0 {
		intent = QQIntentGuildAtMessages | QQIntentDirectMessages | QQIntentGroupMessages | QQIntentAudit
	}

	g := &QQGateway{
		appID:              appID,
		appSecret:          appSecret,
		sandbox:            sandbox,
		intent:             intent,
		apiBaseURL:         baseURL,
		httpClient:         &http.Client{Timeout: 30 * time.Second},
		shardCount:         1,
		shardID:            0,
		messageSourceCache: make(map[string]string),
	}

	g.BasePlatform = NewBasePlatform("qq", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *QQGateway) onConnect(ctx context.Context) error {
	if g.appID == "" || g.appSecret == "" {
		log.Warn("[QQ] No app_id/app_secret configured, skipping connect. Configure credentials to enable QQ support.")
		return nil
	}

	log.Infof("[QQ] Connecting with appID=%s, sandbox=%v", g.appID, g.sandbox)

	gatewayURL, err := g.getGatewayURL()
	if err != nil {
		return fmt.Errorf("failed to get QQ gateway URL: %w", err)
	}
	log.Infof("[QQ] Gateway URL: %s", gatewayURL)

	if err := g.connectWebSocket(gatewayURL); err != nil {
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	go g.listenWebSocket()

	log.Info("[QQ] Gateway connected and listening for events")
	return nil
}

func (g *QQGateway) onDisconnect() error {
	g.wsMutex.Lock()
	if g.heartbeatTicker != nil {
		g.heartbeatTicker.Stop()
		g.heartbeatTicker = nil
	}
	if g.heartbeatDone != nil {
		close(g.heartbeatDone)
		g.heartbeatDone = nil
	}
	if g.wsConn != nil {
		g.wsConn.Close()
		g.wsConn = nil
	}
	g.wsMutex.Unlock()

	log.Info("[QQ] Gateway disconnected")
	return nil
}

func (g *QQGateway) onSend(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("QQ gateway not connected")
	}

	channelID := resp.ChannelID
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	msgType := g.detectMessageType(channelID, nil)
	content := resp.Content

	if len(content) > 2000 {
		for i := 0; i < len(content); i += 1900 {
			end := i + 1900
			if end > len(content) {
				end = len(content)
			}
			if err := g.sendMessage(channelID, content[i:end], msgType, resp.MessageID); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
			time.Sleep(500 * time.Millisecond)
		}
		return nil
	}

	return g.sendMessage(channelID, content, msgType, resp.MessageID)
}

func (g *QQGateway) detectMessageType(channelID string, metadata map[string]interface{}) string {
	if metadata != nil {
		if t, ok := metadata["type"].(string); ok {
			return t
		}
	}

	g.sourceCacheMu.RLock()
	if cached, ok := g.messageSourceCache[channelID]; ok {
		g.sourceCacheMu.RUnlock()
		return cached
	}
	g.sourceCacheMu.RUnlock()

	return "guild"
}

func (g *QQGateway) cacheMessageSource(channelID, msgType string) {
	g.sourceCacheMu.Lock()
	g.messageSourceCache[channelID] = msgType
	g.sourceCacheMu.Unlock()
}

func (g *QQGateway) getAccessToken() (string, error) {
	g.tokenMu.Lock()
	defer g.tokenMu.Unlock()

	if g.accessToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.accessToken, nil
	}

	tokenURL := fmt.Sprintf("%s/api/oauth2/access_token", g.apiBaseURL)
	reqBody := map[string]interface{}{
		"appId":     g.appID,
		"appSecret": g.appSecret,
		"grantType": "client_credentials",
	}

	jsonData, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", tokenURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Code        int    `json:"code"`
		Message     string `json:"message"`
		TraceID     string `json:"trace_id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if result.Code != 0 && result.AccessToken == "" {
		return "", fmt.Errorf("QQ token API error: code=%d, message=%s", result.Code, result.Message)
	}

	g.accessToken = result.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)

	log.Infof("[QQ] Access token refreshed, expires in %d seconds", result.ExpiresIn)
	return g.accessToken, nil
}

func (g *QQGateway) getGatewayURL() (string, error) {
	accessToken, err := g.getAccessToken()
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	url := fmt.Sprintf("%s/api/gateway", g.apiBaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create gateway request: %w", err)
	}
	req.Header.Set("Authorization", "QQBot "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read gateway response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gateway API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL    string `json:"url"`
		Shards int    `json:"shards"`
		Session struct {
			StartLimit struct {
				Total          int `json:"total"`
				Remaining      int `json:"remaining"`
				ResetAfter     int `json:"reset_after"`
				MaxConcurrency int `json:"max_concurrency"`
			} `json:"start_limit"`
		} `json:"session_start_limit"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse gateway response: %w", err)
	}
	if result.URL == "" {
		return "", fmt.Errorf("empty gateway URL in response")
	}

	if result.Shards > 0 && g.shardCount != result.Shards {
		log.Infof("[QQ] Recommended shard count: %d", result.Shards)
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
		g.HandleDisconnection(fmt.Errorf("websocket closed"))
	}()

	for {
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		g.wsMutex.Lock()
		conn := g.wsConn
		g.wsMutex.Unlock()
		if conn == nil {
			log.Warn("[QQ] WebSocket connection lost")
			return
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Warnf("[QQ] WebSocket read error: %v", err)
			}
			return
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
	case QQOpHello:
		var hello struct {
			HeartbeatInterval int `json:"heartbeat_interval"`
		}
		if err := json.Unmarshal(event.D, &hello); err == nil {
			g.heartbeatInterval = time.Duration(hello.HeartbeatInterval) * time.Millisecond
			log.Infof("[QQ] Received Hello, heartbeat interval: %v", g.heartbeatInterval)
		}
		g.sendIdentify()
		go g.heartbeatLoop()

	case QQOpHeartbeatACK:
		log.Debug("[QQ] Heartbeat ACK received")

	case QQOpDispatch:
		g.handleDispatchEvent(event.T, event.D)

	case QQOpReconnect:
		log.Info("[QQ] Reconnect requested by server")
		g.HandleDisconnection(fmt.Errorf("server requested reconnect"))

	case QQOpInvalidSess:
		log.Warn("[QQ] Invalid session, re-identifying")
		time.Sleep(2 * time.Second)
		g.sendIdentify()

	default:
		log.Debugf("[QQ] Unknown op code: %d, type: %s", event.Op, event.T)
	}
}

func (g *QQGateway) sendIdentify() {
	accessToken, err := g.getAccessToken()
	if err != nil {
		log.Errorf("[QQ] Failed to get access token for identify: %v", err)
		return
	}

	payload := map[string]interface{}{
		"op": QQOpIdentify,
		"d": map[string]interface{}{
			"token":   accessToken,
			"intents": g.intent,
			"shards":  []int{g.shardID, g.shardCount},
			"properties": map[string]string{
				"os":      "go-magic",
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

	g.wsMutex.Lock()
	g.heartbeatTicker = time.NewTicker(g.heartbeatInterval)
	g.heartbeatDone = make(chan struct{})
	ticker := g.heartbeatTicker
	done := g.heartbeatDone
	g.wsMutex.Unlock()

	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			g.sendHeartbeat()
		}
	}
}

func (g *QQGateway) sendHeartbeat() {
	payload := map[string]interface{}{
		"op": QQOpHeartbeat,
		"d":  g.seq,
	}
	g.sendWSMessage(payload)
	log.Debugf("[QQ] Heartbeat sent, seq=%d", g.seq)
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
			g.userID = ready.User.ID
			g.userName = ready.User.Username
			g.SetUserInfo(ready.User.ID, ready.User.Username)
			log.Infof("[QQ] Ready! Bot: %s (ID: %s), session: %s", ready.User.Username, ready.User.ID, ready.SessionID)
		}

	case "MESSAGE_CREATE", "AT_MESSAGE_CREATE":
		g.handleGuildMessageEvent(data)

	case "DIRECT_MESSAGE_CREATE":
		g.handleDirectMessageEvent(data)

	case "GROUP_AT_MESSAGE_CREATE":
		g.handleGroupMessageEvent(data)

	case "GUILD_MEMBER_ADD":
		log.Debug("[QQ] Guild member added")

	case "AUDIT":
		log.Debugf("[QQ] Audit event received")

	default:
		log.Debugf("[QQ] Dispatch event: %s", eventType)
	}
}

func (g *QQGateway) handleGuildMessageEvent(data json.RawMessage) {
	var msgData struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Mentions  []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"mentions"`
		Author struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"author"`
		Timestamp string `json:"timestamp"`
	}

	if err := json.Unmarshal(data, &msgData); err != nil {
		log.Errorf("[QQ] Failed to parse guild message: %v", err)
		return
	}

	if msgData.Author.Bot {
		return
	}

	content := strings.TrimSpace(msgData.Content)
	if content == "" {
		return
	}

	if !g.ShouldProcessChannel(msgData.ChannelID) {
		return
	}

	g.cacheMessageSource(msgData.ChannelID, "guild")

	isMentioned := len(msgData.Mentions) > 0 || strings.Contains(msgData.Content, "@")

	msg := Message{
		ID:          msgData.ID,
		Platform:    "qq",
		ChannelID:   msgData.ChannelID,
		UserID:      msgData.Author.ID,
		Content:     content,
		Timestamp:   time.Now(),
		IsGroup:     true,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"guild_id":   msgData.GuildID,
			"author":     msgData.Author.Username,
			"author_bot": msgData.Author.Bot,
			"type":       "guild",
		},
	}

	g.EmitMessage(msg)
}

func (g *QQGateway) handleDirectMessageEvent(data json.RawMessage) {
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
		log.Errorf("[QQ] Failed to parse direct message: %v", err)
		return
	}

	if msgData.Author.Bot {
		return
	}

	content := strings.TrimSpace(msgData.Content)
	if content == "" {
		return
	}

	g.cacheMessageSource(msgData.Author.ID, "dm")

	msg := Message{
		ID:          msgData.ID,
		Platform:    "qq",
		ChannelID:   msgData.Author.ID,
		UserID:      msgData.Author.ID,
		Content:     content,
		Timestamp:   time.Now(),
		IsGroup:     false,
		IsMentioned: true,
		Metadata: map[string]interface{}{
			"guild_id":   msgData.GuildID,
			"channel_id": msgData.ChannelID,
			"author":     msgData.Author.Username,
			"type":       "dm",
		},
	}

	g.EmitMessage(msg)
}

func (g *QQGateway) handleGroupMessageEvent(data json.RawMessage) {
	var msgData struct {
		ID        string `json:"id"`
		GroupID   string `json:"group_id"`
		GroupCode string `json:"group_code"`
		Content   string `json:"content"`
		Author    struct {
			ID           string `json:"id"`
			MemberOpenID string `json:"member_openid"`
		} `json:"author"`
		Timestamp string `json:"timestamp"`
	}

	if err := json.Unmarshal(data, &msgData); err != nil {
		log.Errorf("[QQ] Failed to parse group message: %v", err)
		return
	}

	content := strings.TrimSpace(msgData.Content)
	if content == "" {
		return
	}

	userID := msgData.Author.MemberOpenID
	if userID == "" {
		userID = msgData.Author.ID
	}

	g.cacheMessageSource(msgData.GroupID, "group")

	msg := Message{
		ID:          msgData.ID,
		Platform:    "qq",
		ChannelID:   msgData.GroupID,
		UserID:      userID,
		Content:     content,
		Timestamp:   time.Now(),
		IsGroup:     true,
		IsMentioned: true,
		Metadata: map[string]interface{}{
			"group_id":   msgData.GroupID,
			"group_code": msgData.GroupCode,
			"type":       "group",
		},
	}

	g.EmitMessage(msg)
}

func (g *QQGateway) SendText(channelID string, text string) error {
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	msgType := g.detectMessageType(channelID, nil)

	if len(text) > 2000 {
		for i := 0; i < len(text); i += 1900 {
			end := i + 1900
			if end > len(text) {
				end = len(text)
			}
			if err := g.sendMessage(channelID, text[i:end], msgType, ""); err != nil {
				return fmt.Errorf("failed to send message part: %w", err)
			}
			time.Sleep(500 * time.Millisecond)
		}
		return nil
	}

	return g.sendMessage(channelID, text, msgType, "")
}

func (g *QQGateway) sendMessage(channelID, content, msgType, msgID string) error {
	accessToken, err := g.getAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	var url string
	body := map[string]interface{}{
		"content": content,
		"msg_type": 0,
	}

	switch msgType {
	case "group":
		url = fmt.Sprintf("%s/v2/groups/%s/messages", g.apiBaseURL, channelID)
		if msgID != "" {
			body["msg_id"] = msgID
		}
	case "dm":
		url = fmt.Sprintf("%s/v2/users/%s/messages", g.apiBaseURL, channelID)
		if msgID != "" {
			body["msg_id"] = msgID
		}
	default:
		url = fmt.Sprintf("%s/channels/%s/messages", g.apiBaseURL, channelID)
		if msgID != "" {
			body["msg_id"] = msgID
		}
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "QQBot "+accessToken)
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
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
		return fmt.Errorf("QQ API error: code %d, message: %s, trace_id: %s", result.Code, result.Message, result.TraceID)
	}

	log.Debugf("[QQ] Message sent to channel %s (type: %s)", channelID, msgType)
	return nil
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
	status := g.BasePlatform.CheckHealth()

	status.Platform = "qq"
	status.Status = "healthy"
	status.Platforms = make(map[string]PlatformStatus)
	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}

	platformStatus := PlatformStatus{
		Name:   "qq",
		Status: "connected",
	}

	if g.appID == "" || g.appSecret == "" {
		platformStatus.Status = "not_configured"
		platformStatus.Error = "No app_id/app_secret configured"
		status.Status = "not_configured"
		status.Platforms["qq"] = platformStatus
		return status
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		if status.Error != "" {
			platformStatus.Error = status.Error
		} else {
			platformStatus.Error = "Gateway not connected"
		}
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
		status.Platforms["qq"] = platformStatus
		return status
	}

	g.tokenMu.RLock()
	hasToken := g.accessToken != ""
	g.tokenMu.RUnlock()

	if hasToken {
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
	status.Details["sandbox"] = g.sandbox
	status.Details["user_name"] = g.userName
	status.Details["api_base_url"] = g.apiBaseURL
	status.Platforms["qq"] = platformStatus

	return status
}

func (g *QQGateway) IsLoggedIn() bool {
	g.tokenMu.RLock()
	defer g.tokenMu.RUnlock()
	return g.accessToken != "" || (g.appID != "" && g.appSecret != "")
}

func (g *QQGateway) GetLoginStatus() string {
	if g.IsLoggedIn() {
		if g.IsConnected() {
			return "confirmed"
		}
		return "configured"
	}
	return "not_configured"
}