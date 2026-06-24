package gateway

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/base64"
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
	*BasePlatform

	appID     string
	appSecret string
	token     string

	accessToken string
	tokenExpiry time.Time

	sandbox bool
	intent  int

	apiBaseURL string
	httpClient *http.Client

	wsConn            *websocket.Conn
	wsMutex           sync.Mutex
	heartbeatInterval time.Duration
	seq               int
	sessionID         string
	shardCount        int
	shardID           int

	scanKey string // 用于扫码登录的 AES 密钥
}

func NewQQGateway(appID, appSecret string, sandbox bool, intent int) *QQGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &QQGateway{
		appID:      appID,
		appSecret:  appSecret,
		sandbox:    sandbox,
		intent:     intent,
		apiBaseURL: "https://api.sgroup.qq.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		shardCount: 1,
		shardID:    0,
	}

	g.BasePlatform = NewBasePlatform("qq", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	if g.intent == 0 {
		g.intent = 1<<30 | 1<<0 | 1<<1 | 1<<25 | 1<<9
	}

	return g
}

func (g *QQGateway) onConnect(ctx context.Context) error {
	if g.appID == "" || g.appSecret == "" {
		return fmt.Errorf("QQ app_id and app_secret are required")
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

	go g.listenWebSocket()

	log.Info("[QQ] Gateway connected and listening for events")
	return nil
}

func (g *QQGateway) onDisconnect() error {
	g.wsMutex.Lock()
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

	return g.SendText(channelID, resp.Content)
}

func (g *QQGateway) getAccessToken() (string, error) {
	g.wsMutex.Lock()
	defer g.wsMutex.Unlock()

	if g.accessToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.accessToken, nil
	}

	tokenURL := fmt.Sprintf("%s/app/token", g.apiBaseURL)
	reqBody := fmt.Sprintf("app_id=%s&app_secret=%s", g.appID, g.appSecret)
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		return "", fmt.Errorf("failed to get access token: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Code        int    `json:"code"`
		Message     string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if result.Code != 0 {
		return "", fmt.Errorf("QQ token API error: code %d, message: %s", result.Code, result.Message)
	}

	g.accessToken = result.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)

	log.Info("[QQ] Access token refreshed, expires in %d seconds", result.ExpiresIn)
	return g.accessToken, nil
}

func (g *QQGateway) getGatewayURL() (string, error) {
	accessToken, err := g.getAccessToken()
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	url := fmt.Sprintf("%s/gateway", g.apiBaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+accessToken)
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
		g.HandleDisconnection(fmt.Errorf("server requested reconnect"))

	case 9:
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
		"op": 2,
		"d": map[string]interface{}{
			"token":   accessToken,
			"intents": g.intent,
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
		case <-g.ctx.Done():
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
			g.SetUserInfo(ready.User.ID, ready.User.Username)
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

	if !g.ShouldProcessChannel(msgData.ChannelID) {
		return
	}

	isGroup := msgData.GuildID != ""
	isMentioned := len(msgData.Mentions) > 0 || strings.Contains(msgData.Content, "@")

	msg := Message{
		ID:          msgData.ID,
		Platform:    "qq",
		ChannelID:   msgData.ChannelID,
		UserID:      msgData.Author.ID,
		Content:     content,
		Timestamp:   time.Now(),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"guild_id":   msgData.GuildID,
			"author":     msgData.Author.Username,
			"author_bot": msgData.Author.Bot,
		},
	}

	g.EmitMessage(msg)
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

	accessToken, err := g.getAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
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
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
		return fmt.Errorf("QQ API error: code %d, message: %s", result.Code, result.Message)
	}

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

func (g *QQGateway) StartQRLogin(ctx context.Context) (string, error) {
	return g.StartQQScanLogin(ctx)
}

func (g *QQGateway) IsLoggedIn() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.accessToken != "" || (g.appID != "" && g.appSecret != "")
}

func (g *QQGateway) GetLoginStatus() string {
	if g.IsLoggedIn() {
		if g.IsConnected() {
			return "confirmed"
		}
		return "configured"
	}
	return "waiting_qr"
}

type QQScanLoginResult struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
	UserID    string `json:"user_id"`
}

const (
	QQBindAPIBase    = "https://q.qq.com/api/v2/bind_robot"
	QQBindCreatePath = "/create"
	QQBindPollPath   = "/poll"
)

const (
	QQBindStatusPending   = 0
	QQBindStatusScanned   = 1
	QQBindStatusConfirmed = 2
	QQBindStatusExpired   = 3
)

type QQBindResponse struct {
	RetCode int    `json:"retcode"`
	Msg     string `json:"msg"`
	Data    struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

type QQBindPollResponse struct {
	RetCode int    `json:"retcode"`
	Msg     string `json:"msg"`
	Data    struct {
		Status           int    `json:"status"`
		BotAppID         string `json:"bot_appid"`
		BotEncryptSecret string `json:"bot_encrypt_secret"`
		UserOpenID       string `json:"user_openid"`
	} `json:"data"`
}

func generateAESKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func decryptQQSecret(encrypted, keyBase64 string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode key: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	if len(ciphertext) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], ciphertext[i:i+aes.BlockSize])
	}

	if len(plaintext) == 0 {
		return "", fmt.Errorf("decrypted to empty plaintext")
	}
	paddingLen := int(plaintext[len(plaintext)-1])
	if paddingLen > aes.BlockSize || paddingLen == 0 {
		return "", fmt.Errorf("invalid PKCS7 padding")
	}
	plaintext = plaintext[:len(plaintext)-paddingLen]

	return string(plaintext), nil
}

func createQQBindTask(key string) (*QQBindResponse, error) {
	body := map[string]string{"key": key}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", QQBindAPIBase+QQBindCreatePath, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QQBotSDK/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result QQBindResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("create bind task failed: retcode=%d, msg=%s", result.RetCode, result.Msg)
	}

	return &result, nil
}

func pollQQBindResult(taskID string) (*QQBindPollResponse, error) {
	body := map[string]string{"task_id": taskID}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", QQBindAPIBase+QQBindPollPath, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QQBotSDK/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result QQBindPollResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("poll bind result failed: retcode=%d, msg=%s", result.RetCode, result.Msg)
	}

	return &result, nil
}

func (g *QQGateway) StartQQScanLogin(ctx context.Context) (string, error) {
	key, err := generateAESKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate AES key: %w", err)
	}

	result, err := createQQBindTask(key)
	if err != nil {
		return "", fmt.Errorf("failed to create bind task: %w", err)
	}

	taskID := result.Data.TaskID
	if taskID == "" {
		return "", fmt.Errorf("empty task_id in response")
	}

	g.scanKey = taskID + ":" + key

	qrURL := fmt.Sprintf("https://q.qq.com/qrcode?task_id=%s", taskID)
	log.Infof("[QQ] Bind task created: %s", taskID)

	return qrURL, nil
}

type QQScanStatusResponse struct {
	Stat      int    `json:"stat"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
	Token     string `json:"token"`
	OpenID    string `json:"open_id"`
}

func PollQQScanStatus(ctx context.Context, sig string) (*QQScanStatusResponse, error) {
	taskID := sig
	key := ""
	if idx := strings.Index(sig, ":"); idx != -1 {
		taskID = sig[:idx]
		key = sig[idx+1:]
	}

	if taskID == "" {
		return nil, fmt.Errorf("empty task_id")
	}

	result, err := pollQQBindResult(taskID)
	if err != nil {
		return nil, err
	}

	status := &QQScanStatusResponse{
		Stat:   result.Data.Status,
		OpenID: result.Data.UserOpenID,
	}

	switch result.Data.Status {
	case QQBindStatusConfirmed:
		status.AppID = result.Data.BotAppID
		if key != "" && result.Data.BotEncryptSecret != "" {
			decryptedSecret, err := decryptQQSecret(result.Data.BotEncryptSecret, key)
			if err != nil {
				log.Warnf("[QQ] Failed to decrypt secret: %v, using encrypted", err)
				status.AppSecret = result.Data.BotEncryptSecret
			} else {
				status.AppSecret = decryptedSecret
			}
		}
		log.Infof("[QQ] Bind confirmed! AppID: %s", status.AppID)
	case QQBindStatusExpired:
		log.Infof("[QQ] Bind QR code expired")
	case QQBindStatusScanned:
		log.Infof("[QQ] QR code scanned, waiting for confirmation")
	default:
		log.Debugf("[QQ] Bind pending, status: %d", result.Data.Status)
	}

	return status, nil
}
