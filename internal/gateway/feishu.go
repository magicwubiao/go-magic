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

type FeishuGateway struct {
	*BasePlatform

	appID             string
	appSecret         string
	verificationToken string
	encryptKey        string

	tenantAccessToken string
	tokenExpiresAt    time.Time
	tokenMu           sync.RWMutex
}

func NewFeishuGateway(appID, appSecret string) *FeishuGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  5,
		"retry_delay":  5,
	}

	g := &FeishuGateway{
		appID:     appID,
		appSecret: appSecret,
	}

	g.BasePlatform = NewBasePlatform("feishu", config)
	g.SetCallbackPort(8081)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *FeishuGateway) onConnect(ctx context.Context) error {
	if g.appID == "" || g.appSecret == "" {
		log.Warn("[Feishu] No app_id/app_secret configured, skipping connect. Configure credentials to enable Feishu support.")
		return nil
	}

	log.Infof("[Feishu] Connecting gateway...")

	if err := g.refreshToken(); err != nil {
		log.Errorf("[Feishu] Failed to get token: %v", err)
		return err
	}

	go g.tokenRefresher(ctx)
	go g.startCallbackServer(ctx)

	log.Info("[Feishu] Gateway connected")
	return nil
}

func (g *FeishuGateway) onDisconnect() error {
	log.Info("[Feishu] Gateway disconnected")
	return nil
}

func (g *FeishuGateway) onSend(ctx context.Context, resp Response) error {
	if resp.ChannelID != "" {
		content := resp.Content
		if strings.HasPrefix(strings.TrimSpace(content), "{") ||
			strings.HasPrefix(strings.TrimSpace(content), "[") {
			if err := g.sendRichMessage(resp.ChannelID, content); err != nil {
				log.Warnf("[Feishu] Failed to send rich message, falling back to text: %v", err)
				return g.sendMessageAPI(resp.ChannelID, resp.Content)
			}
			return nil
		}
		return g.sendMessageAPI(resp.ChannelID, content)
	}

	return nil
}

func (g *FeishuGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - Feishu\n\n" +
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

func (g *FeishuGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "feishu"
	status.CallbackPort = g.GetCallbackPort()
	status.Details["callback_port"] = g.GetCallbackPort()

	if status.Platforms == nil {
		status.Platforms = make(map[string]PlatformStatus)
	}

	platformStatus := PlatformStatus{
		Name:   "feishu",
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
		status.Platforms["feishu"] = platformStatus
		return status
	}

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

	status.Platforms["feishu"] = platformStatus

	return status
}

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

	log.Debugf("[Feishu] Token refreshed, expires in %d seconds", result.Expire)
	return nil
}

func (g *FeishuGateway) tokenRefresher(ctx context.Context) {
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
				log.Errorf("[Feishu] Failed to refresh token: %v", err)
			}
		}
	}
}

func (g *FeishuGateway) startCallbackServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/feishu/callback", g.handleCallback)

	addr := fmt.Sprintf(":%d", g.GetCallbackPort())
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Infof("[Feishu] Callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("[Feishu] Callback server error: %v", err)
		g.HandleDisconnection(err)
	}
}

func (g *FeishuGateway) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Errorf("[Feishu] Failed to read callback body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var event struct {
		Schema string `json:"schema"`
		Header struct {
			EventID    string `json:"event_id"`
			EventType  string `json:"event_type"`
			AppID      string `json:"app_id"`
			TenantKey  string `json:"tenant_key"`
			CreateTime string `json:"create_time"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		log.Errorf("[Feishu] Failed to parse callback event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch event.Header.EventType {
	case "im.message.receive_v1":
		g.handleMessageEvent(event.Event)
	default:
		log.Debugf("[Feishu] Unhandled event type: %s", event.Header.EventType)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"code":0}`))
}

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
			MessageID   string `json:"message_id"`
			RootID      string `json:"root_id"`
			ParentID    string `json:"parent_id"`
			CreateTime  string `json:"create_time"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
			Mentions    []struct {
				Key       string `json:"key"`
				ID        string `json:"id"`
				IDType    string `json:"id_type"`
				Name      string `json:"name"`
				TenantKey string `json:"tenant_key"`
			} `json:"mentions"`
		} `json:"message"`
	}

	if err := json.Unmarshal(event, &msgEvent); err != nil {
		log.Errorf("[Feishu] Failed to parse message event: %v", err)
		return
	}

	if msgEvent.Sender.SenderType != "user" {
		return
	}

	if !g.ShouldProcessChannel(msgEvent.Message.ChatID) {
		return
	}

	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msgEvent.Message.Content), &content); err != nil {
		log.Errorf("[Feishu] Failed to parse message content: %v", err)
		return
	}

	isGroup := msgEvent.Message.ChatType != "p2p"
	isMentioned := len(msgEvent.Message.Mentions) > 0 || strings.Contains(content.Text, "@")

	msg := Message{
		ID:          msgEvent.Message.MessageID,
		ChannelID:   msgEvent.Message.ChatID,
		UserID:      msgEvent.Sender.SenderID.OpenID,
		Content:     content.Text,
		Timestamp:   time.Now(),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"chat_type":    msgEvent.Message.ChatType,
			"message_type": msgEvent.Message.MessageType,
		},
	}

	g.EmitMessage(msg)
}

func (g *FeishuGateway) sendMessageAPI(chatID, content string) error {
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	g.tokenMu.RUnlock()

	url := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id"

	textContent, _ := json.Marshal(content)
	msg := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    fmt.Sprintf(`{"text":%s}`, string(textContent)),
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

func (g *FeishuGateway) SendText(chatID, text string) error {
	return g.sendMessageAPI(chatID, text)
}

func (g *FeishuGateway) sendRichMessage(chatID, content string) error {
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	g.tokenMu.RUnlock()

	url := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id"

	var cardContent map[string]interface{}
	if err := json.Unmarshal([]byte(content), &cardContent); err == nil {
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

	paragraphs := []map[string]string{
		{"text": content},
	}
	return g.SendRichText(chatID, paragraphs)
}

func (g *FeishuGateway) SendRichText(chatID string, paragraphs []map[string]string) error {
	g.tokenMu.RLock()
	token := g.tenantAccessToken
	g.tokenMu.RUnlock()

	url := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id"

	postContent := make([]interface{}, len(paragraphs))
	for i, p := range paragraphs {
		postContent[i] = map[string]interface{}{
			"tag":  "text",
			"text": p["text"],
		}
		if p["href"] != "" {
			postContent[i] = map[string]interface{}{
				"tag":  "a",
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
