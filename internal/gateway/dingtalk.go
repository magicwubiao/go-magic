package gateway

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"

	"crypto/sha256"

	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

type DingTalkGateway struct {
	*BasePlatform

	appKey    string
	appSecret string
	agentID   string

	accessToken    string
	tokenExpiresAt time.Time
	tokenMu        sync.RWMutex

	agents map[string]*AgentSession
	mu     sync.RWMutex

	aesKey []byte

	server *http.Server
}

func NewDingTalkGateway(appKey, appSecret string) *DingTalkGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &DingTalkGateway{
		appKey:    appKey,
		appSecret: appSecret,
		agents:    make(map[string]*AgentSession),
	}

	g.BasePlatform = NewBasePlatform("dingtalk", config)
	g.SetCallbackPort(8080)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *DingTalkGateway) onConnect(ctx context.Context) error {
	log.Infof("[DingTalk] Connecting to DingTalk gateway...")

	if err := g.refreshToken(); err != nil {
		log.Errorf("[DingTalk] Failed to get DingTalk token: %v", err)
		return err
	}

	go g.tokenRefresher(ctx)
	go g.startCallbackServer(ctx)

	log.Info("[DingTalk] Gateway connected")
	return nil
}

func (g *DingTalkGateway) onDisconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.server != nil {
		g.server.Close()
		g.server = nil
	}

	log.Info("[DingTalk] Gateway disconnected")
	return nil
}

func (g *DingTalkGateway) onSend(ctx context.Context, resp Response) error {
	if resp.ChannelID != "" {
		content := resp.Content
		if strings.HasPrefix(strings.TrimSpace(content), "{") ||
			strings.HasPrefix(strings.TrimSpace(content), "[") {
			if err := g.sendRichMessage(resp.ChannelID, content); err != nil {
				log.Warnf("[DingTalk] Failed to send rich message, falling back to text: %v", err)
				return g.sendMessage(resp.ChannelID, content)
			}
			return nil
		}
		return g.sendMessage(resp.ChannelID, content)
	}
	return nil
}

func (g *DingTalkGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - DingTalk\n\n" +
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
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if g.IsConnected() {
			return Response{Content: "✅ Connected and ready!"}, nil
		}
		return Response{Content: "❌ Not connected"}, nil
	case "stats":
		return Response{Content: "Gateway is running"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

func (g *DingTalkGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "dingtalk"
	status.Platforms = make(map[string]PlatformStatus)

	platformStatus := PlatformStatus{
		Name:   "dingtalk",
		Status: "connected",
	}

	if !g.IsConnected() {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Platforms["dingtalk"] = platformStatus
		return status
	}

	g.tokenMu.RLock()
	token := g.accessToken
	tokenExpiry := g.tokenExpiresAt
	g.tokenMu.RUnlock()

	if token == "" {
		platformStatus.Error = "No access token"
		status.Status = "error"
	} else if !tokenExpiry.IsZero() && time.Now().After(tokenExpiry) {
		platformStatus.Error = "Token expired"
		status.Status = "error"
	}

	status.Details["token_available"] = token != ""
	status.Details["callback_port"] = g.GetCallbackPort()
	status.Platforms["dingtalk"] = platformStatus

	return status
}

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

	log.Debugf("[DingTalk] Token refreshed")
	return nil
}

func (g *DingTalkGateway) tokenRefresher(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !g.IsConnected() {
				return
			}

			if err := g.refreshToken(); err != nil {
				log.Errorf("[DingTalk] Failed to refresh DingTalk token: %v", err)
			}
		}
	}
}

func (g *DingTalkGateway) startCallbackServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/dingtalk/callback", g.handleCallback)

	addr := fmt.Sprintf(":%d", g.GetCallbackPort())
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	g.mu.Lock()
	g.server = server
	g.mu.Unlock()

	log.Infof("[DingTalk] Callback server starting on %s", addr)

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("[DingTalk] Callback server error: %v", err)
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		server.Close()
	case <-errCh:
		g.HandleDisconnection(fmt.Errorf("callback server error"))
	}
}

func (g *DingTalkGateway) SetAESKey(key string) {
	hash := sha256.Sum256([]byte(key))
	g.aesKey = hash[:32]
}

func (g *DingTalkGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
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
		log.Errorf("[DingTalk] Failed to read callback body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var msgStr string
	if g.aesKey != nil && len(body) > 0 {
		msgStr, err = g.decryptCallback(body)
		if err != nil {
			log.Errorf("[DingTalk] Failed to decrypt callback: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else {
		msgStr = string(body)
	}

	var event struct {
		EventType string `json:"EventType"`
		Text      struct {
			Content string `json:"content"`
		} `json:"text"`
		RobotCode      string `json:"robotCode"`
		SenderNick     string `json:"senderNick"`
		ConversationID string `json:"conversationId"`
		SenderID       string `json:"senderStaffId"`
		MsgId          string `json:"msgId"`
		CreateAt       int64  `json:"createAt"`
	}

	if err := json.Unmarshal([]byte(msgStr), &event); err != nil {
		log.Errorf("[DingTalk] Failed to parse callback event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch event.EventType {
	case "robot", "o2o":
		g.handleMessageEvent(event)
	default:
		log.Debugf("[DingTalk] Unhandled event type: %s", event.EventType)
	}

	w.WriteHeader(http.StatusOK)
}

func (g *DingTalkGateway) decryptCallback(encrypted []byte) (string, error) {
	block, err := aes.NewCipher(g.aesKey)
	if err != nil {
		return "", err
	}

	iv := encrypted[:aes.BlockSize]
	encrypted = encrypted[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(encrypted, encrypted)

	padding := int(encrypted[len(encrypted)-1])
	encrypted = encrypted[:len(encrypted)-padding]

	return string(encrypted), nil
}

func (g *DingTalkGateway) handleMessageEvent(event struct {
	EventType string `json:"EventType"`
	Text      struct {
		Content string `json:"content"`
	} `json:"text"`
	RobotCode      string `json:"robotCode"`
	SenderNick     string `json:"senderNick"`
	ConversationID string `json:"conversationId"`
	SenderID       string `json:"senderStaffId"`
	MsgId          string `json:"msgId"`
	CreateAt       int64  `json:"createAt"`
}) {
	if event.EventType != "robot" && event.EventType != "o2o" {
		return
	}

	isGroup := strings.HasPrefix(event.ConversationID, "cid") || event.EventType == "robot"
	isMentioned := false

	msg := Message{
		ID:          event.MsgId,
		Platform:    "dingtalk",
		ChannelID:   event.ConversationID,
		UserID:      event.SenderID,
		Content:     event.Text.Content,
		Timestamp:   time.Unix(event.CreateAt/1000, 0),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"sender_nick": event.SenderNick,
			"robot_code":  event.RobotCode,
		},
	}

	if !g.ShouldProcessChannel(event.ConversationID) {
		return
	}

	g.EmitMessage(msg)
}

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

func (g *DingTalkGateway) SendText(userID, content string) error {
	return g.sendMessage(userID, content)
}

func (g *DingTalkGateway) sendRichMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	var richContent map[string]interface{}
	if err := json.Unmarshal([]byte(content), &richContent); err != nil {
		return g.sendMessage(userID, content)
	}

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
				"title":      title,
				"text":       richContent["text"],
				"messageUrl": richContent["messageUrl"],
				"picUrl":     richContent["picUrl"],
			}
		} else {
			msg["link"] = map[string]interface{}{
				"title":      "Message",
				"text":       content,
				"messageUrl": richContent["messageUrl"],
			}
		}
	case "action_card":
		msg["action_card"] = map[string]interface{}{
			"title":        richContent["title"],
			"markdown":     content,
			"single_title": richContent["single_title"],
			"single_url":   richContent["single_url"],
		}
	default:
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

func (g *DingTalkGateway) SetAgentID(agentID string) {
	g.agentID = agentID
}
