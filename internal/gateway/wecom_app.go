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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

type WeComAppGateway struct {
	*BasePlatform

	corpID         string
	agentID        string
	secret         string
	encodingAESKey string

	accessToken    string
	tokenMu        sync.RWMutex
	tokenExpiresAt time.Time

	agents map[string]*AgentSession
	mu     sync.RWMutex

	aesKey []byte

	httpClient *http.Client
}

func NewWeComAppGateway(corpID, agentID, secret string) *WeComAppGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  5,
		"retry_delay":  5,
	}

	g := &WeComAppGateway{
		corpID:     corpID,
		agentID:    agentID,
		secret:     secret,
		agents:     make(map[string]*AgentSession),
		httpClient: &http.Client{},
	}

	g.BasePlatform = NewBasePlatform("wecom", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend
	g.SetCallbackPort(8080)

	return g
}

func (g *WeComAppGateway) onConnect(ctx context.Context) error {
	if g.corpID == "" || g.secret == "" {
		log.Warn("[WeCom App] No corp_id/secret configured, skipping connect. Configure credentials to enable WeCom support.")
		return nil
	}

	log.Infof("[WeCom App] Connecting gateway...")

	if err := g.refreshToken(); err != nil {
		log.Errorf("[WeCom App] Failed to get token: %v", err)
		return err
	}

	go g.tokenRefresher(ctx)
	go g.startCallbackServer(ctx)

	log.Info("[WeCom App] Gateway connected")
	return nil
}

func (g *WeComAppGateway) onDisconnect() error {
	log.Info("[WeCom App] Gateway disconnected")
	return nil
}

func (g *WeComAppGateway) onSend(ctx context.Context, resp Response) error {
	if resp.ChannelID != "" {
		content := resp.Content
		if strings.HasPrefix(strings.TrimSpace(content), "{") ||
			strings.HasPrefix(strings.TrimSpace(content), "[") {
			if err := g.sendRichMessage(resp.ChannelID, content); err != nil {
				log.Warnf("[WeCom App] Failed to send rich message, falling back to text: %v", err)
				return g.sendMessage(resp.ChannelID, content)
			}
			return nil
		}
		return g.sendMessage(resp.ChannelID, content)
	}
	return nil
}

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

func (g *WeComAppGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "wecom"
	status.CallbackPort = g.GetCallbackPort()
	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}
	if status.Platforms == nil {
		status.Platforms = make(map[string]PlatformStatus)
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
	status.Details["callback_port"] = g.GetCallbackPort()
	status.Platforms["wecom_app"] = platformStatus

	return status
}

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

	log.Debugf("[WeCom App] Token refreshed")
	return nil
}

func (g *WeComAppGateway) tokenRefresher(ctx context.Context) {
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
				log.Errorf("[WeCom App] Failed to refresh token: %v", err)
			}
		}
	}
}

func (g *WeComAppGateway) startCallbackServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wecom/callback", g.handleCallback)

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

	log.Infof("[WeCom App] Callback server starting on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("[WeCom App] Callback server error: %v", err)
		g.HandleDisconnection(err)
	}
}

func (g *WeComAppGateway) SetAESKey(key string) {
	g.encodingAESKey = key
	g.aesKey = []byte(key + "=")[:32]
}

func (g *WeComAppGateway) handleCallback(w http.ResponseWriter, r *http.Request) {

	echostr := r.URL.Query().Get("echostr")

	if echostr != "" {
		decoded, err := base64.StdEncoding.DecodeString(echostr)
		if err != nil {
			log.Errorf("[WeCom App] Failed to decode echostr: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if g.encodingAESKey != "" {
			decrypted, err := g.decryptWeCom(decoded)
			if err != nil {
				log.Errorf("[WeCom App] Failed to decrypt echostr: %v", err)
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
		log.Errorf("[WeCom App] Failed to read callback body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var callback struct {
		XMLName      xml.Name `xml:"xml"`
		Encrypt      string   `xml:"Encrypt"`
		MsgSignature string   `xml:"MsgSignature"`
		TimeStamp    string   `xml:"TimeStamp"`
		Nonce        string   `xml:"Nonce"`
		Content      string   `xml:"Content"`
	}

	if err := xml.Unmarshal(body, &callback); err != nil {
		log.Errorf("[WeCom App] Failed to parse callback XML: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var msgStr string
	if callback.Encrypt != "" && g.encodingAESKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(callback.Encrypt)
		if err != nil {
			log.Errorf("[WeCom App] Failed to decode encrypt: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		decrypted, err := g.decryptWeCom(decoded)
		if err != nil {
			log.Errorf("[WeCom App] Failed to decrypt callback: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		msgStr = string(decrypted)
	} else {
		msgStr = callback.Content
	}

	var event struct {
		MsgType      string `xml:"MsgType"`
		Content      string `xml:"Content"`
		FromUserName string `xml:"FromUserName"`
		ToUserName   string `xml:"ToUserName"`
		MsgId        string `xml:"MsgId"`
		AgentID      string `xml:"AgentID"`
		Event        string `xml:"Event"`
		CreateTime   int64  `xml:"CreateTime"`
		MediaID      string `xml:"MediaId,omitempty"`
		PicURL       string `xml:"PicUrl,omitempty"`
		Format       string `xml:"Format,omitempty"`
		ThumbURL     string `xml:"ThumbMediaId,omitempty"`
		Location     string `xml:"Location,omitempty"`
		Scale        string `xml:"Scale,omitempty"`
		Label        string `xml:"Label,omitempty"`
		Title        string `xml:"Title,omitempty"`
		Desc         string `xml:"Description,omitempty"`
		URL          string `xml:"Url,omitempty"`
	}

	if err := xml.Unmarshal([]byte(msgStr), &event); err != nil {
		log.Errorf("[WeCom App] Failed to parse callback event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch event.MsgType {
	case "text", "event":
		g.handleMessageEvent(event)
	default:
		log.Debugf("[WeCom App] Unhandled message type: %s", event.MsgType)
	}

	w.WriteHeader(http.StatusOK)
}

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

	padding := int(encrypted[len(encrypted)-1])
	encrypted = encrypted[:len(encrypted)-padding]

	msgLen := int(encrypted[16])<<24 | int(encrypted[17])<<16 | int(encrypted[18])<<8 | int(encrypted[19])
	msg := encrypted[20 : 20+msgLen]

	return msg, nil
}

func (g *WeComAppGateway) handleMessageEvent(event struct {
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	FromUserName string `xml:"FromUserName"`
	ToUserName   string `xml:"ToUserName"`
	MsgId        string `xml:"MsgId"`
	AgentID      string `xml:"AgentID"`
	Event        string `xml:"Event"`
	CreateTime   int64  `xml:"CreateTime"`
	MediaID      string `xml:"MediaId,omitempty"`
	PicURL       string `xml:"PicUrl,omitempty"`
	Format       string `xml:"Format,omitempty"`
	ThumbURL     string `xml:"ThumbMediaId,omitempty"`
	Location     string `xml:"Location,omitempty"`
	Scale        string `xml:"Scale,omitempty"`
	Label        string `xml:"Label,omitempty"`
	Title        string `xml:"Title,omitempty"`
	Desc         string `xml:"Description,omitempty"`
	URL          string `xml:"Url,omitempty"`
}) {
	if event.AgentID != "" && event.AgentID != g.agentID {
		return
	}

	if !g.ShouldProcessChannel(event.FromUserName) {
		return
	}

	var content string
	var mediaURLs []MediaAttachment

	switch event.MsgType {
	case "text":
		content = event.Content

	case "image":
		content = "[用户发送了一张图片]"
		if event.MediaID != "" {
			if path, err := g.downloadMedia(event.MediaID, "image"); err == nil {
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:    "image",
					URL:     path,
					Caption: event.PicURL,
				})
			} else {
				log.Debugf("[WeCom App] Failed to download image: %v", err)
				content = "[Image]"
			}
		}

	case "voice":
		content = "[语音消息]"
		if event.MediaID != "" {
			if path, err := g.downloadMedia(event.MediaID, "voice"); err == nil {
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "audio",
					URL:      path,
					MimeType: "audio/" + event.Format,
				})
			} else {
				log.Debugf("[WeCom App] Failed to download voice: %v", err)
				content = "[Voice message]"
			}
		} else {
			content = "[Voice message]"
		}

	case "video":
		content = "[用户发送了一个视频]"
		if event.MediaID != "" {
			if path, err := g.downloadMedia(event.MediaID, "video"); err == nil {
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:    "video",
					URL:     path,
					Caption: event.Desc,
				})
			} else {
				log.Debugf("[WeCom App] Failed to download video: %v", err)
				content = "[Video]"
			}
		} else {
			content = "[Video]"
		}

	case "location":
		content = "[Location] " + event.Label + " (" + event.Location + ")"

	case "link":
		content = fmt.Sprintf("[Link: %s] %s - %s", event.Title, event.Desc, event.URL)

	case "event":
		content = "[事件]"

	default:
		log.Debugf("[WeCom App] Unhandled message type: %s", event.MsgType)
		return
	}

	isGroup := strings.HasPrefix(event.FromUserName, "wr")
	isMentioned := false

	msg := Message{
		ID:          event.MsgId,
		Platform:    "wecom_app",
		ChannelID:   event.FromUserName,
		UserID:      event.FromUserName,
		Content:     content,
		Timestamp:   time.Unix(event.CreateTime, 0),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"to_user":  event.ToUserName,
			"agent_id": event.AgentID,
			"event":    event.Event,
			"msg_type": event.MsgType,
		},
		MediaURLs: mediaURLs,
	}

	if msg.ID == "" {
		msg.ID = fmt.Sprintf("wecom_%d_%s", event.CreateTime, event.FromUserName)
	}

	g.EmitMessage(msg)
}

func (g *WeComAppGateway) downloadMedia(mediaID, mediaType string) (string, error) {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	if token == "" {
		return "", fmt.Errorf("WeCom access token not available")
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/media/get?access_token=%s&media_id=%s", token, mediaID)

	resp, err := g.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download media: status %d", resp.StatusCode)
	}

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

	if strings.HasPrefix(string(data), "{\"errcode\"") {
		return "", fmt.Errorf("WeCom API error: %s", string(data))
	}

	dir := filepath.Join(config.GetMagicHome(), "wecom", "media", mediaType)
	os.MkdirAll(dir, 0755)

	filename := fmt.Sprintf("%s_%s.%s", mediaID, mediaType, ext)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to save media: %w", err)
	}

	return path, nil
}

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

func (g *WeComAppGateway) SendText(userID, content string) error {
	return g.sendMessage(userID, content)
}

func (g *WeComAppGateway) sendRichMessage(userID, content string) error {
	g.tokenMu.RLock()
	token := g.accessToken
	g.tokenMu.RUnlock()

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)

	var richContent map[string]interface{}
	if err := json.Unmarshal([]byte(content), &richContent); err == nil {
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

	return g.sendMessage(userID, content)
}

func (g *WeComAppGateway) SendToUser(userID, content string) error {
	return g.sendMessage(userID, content)
}

func (g *WeComAppGateway) Reconnect(ctx context.Context) error {
	return g.Connect(ctx)
}

func (g *WeComAppGateway) GetAccessToken() string {
	g.tokenMu.RLock()
	defer g.tokenMu.RUnlock()
	return g.accessToken
}