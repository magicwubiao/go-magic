package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

type LineGateway struct {
	*BasePlatform

	channelSecret string
	channelToken  string
	userID        string

	httpServer *http.Server
}

type lineWebhookRequest struct {
	Destination string `json:"destination"`
	Events      []struct {
		Type       string `json:"type"`
		ReplyToken string `json:"replyToken,omitempty"`
		Source     struct {
			Type    string `json:"type"`
			UserID  string `json:"userId,omitempty"`
			GroupID string `json:"groupId,omitempty"`
			RoomID  string `json:"roomId,omitempty"`
		} `json:"source"`
		Timestamp int64  `json:"timestamp"`
		Mode      string `json:"mode,omitempty"`
		Message   struct {
			Type            string `json:"type"`
			ID              string `json:"id"`
			Text            string `json:"text,omitempty"`
			ContentProvider *struct {
				Type               string `json:"type"`
				OriginalContentURL string `json:"originalContentUrl,omitempty"`
				PreviewImageURL    string `json:"previewImageUrl,omitempty"`
			} `json:"contentProvider,omitempty"`
			Duration   int     `json:"duration,omitempty"`
			Title      string  `json:"title,omitempty"`
			Address    string  `json:"address,omitempty"`
			Latitude   float64 `json:"latitude,omitempty"`
			Longitude  float64 `json:"longitude,omitempty"`
			PackageID  string  `json:"packageId,omitempty"`
			StickerID  string  `json:"stickerId,omitempty"`
			QuoteToken string  `json:"quoteToken,omitempty"`
		} `json:"message,omitempty"`
		Postback *struct {
			Data   string                 `json:"data"`
			Params map[string]interface{} `json:"params,omitempty"`
		} `json:"postback,omitempty"`
		Beacon *struct {
			Type          string `json:"type"`
			Hwid          string `json:"hwid"`
			DeviceMessage string `json:"deviceMessage,omitempty"`
		} `json:"beacon,omitempty"`
		Join         *struct{} `json:"join,omitempty"`
		Leave        *struct{} `json:"leave,omitempty"`
		MemberJoined *struct {
			Members []struct {
				Type   string `json:"type"`
				UserID string `json:"userId"`
			} `json:"members"`
		} `json:"memberJoined,omitempty"`
		MemberLeft *struct {
			Members []struct {
				Type   string `json:"type"`
				UserID string `json:"userId"`
			} `json:"members"`
		} `json:"memberLeft,omitempty"`
		Unsend *struct {
			MessageID string `json:"messageId"`
		} `json:"unsend,omitempty"`
		AccountLink *struct {
			Result string `json:"result"`
			Nonce  string `json:"nonce"`
		} `json:"accountLink,omitempty"`
		Things *struct {
			Type     string `json:"type"`
			DeviceID string `json:"deviceId"`
		} `json:"things,omitempty"`
	} `json:"events"`
}

type lineReplyRequest struct {
	ReplyToken string               `json:"replyToken"`
	Messages   []lineMessageContent `json:"messages"`
}

type linePushRequest struct {
	To       string               `json:"to"`
	Messages []lineMessageContent `json:"messages"`
}

type lineMessageContent struct {
	Type               string          `json:"type"`
	Text               string          `json:"text,omitempty"`
	OriginalContentURL string          `json:"originalContentUrl,omitempty"`
	PreviewImageURL    string          `json:"previewImageUrl,omitempty"`
	Title              string          `json:"title,omitempty"`
	Address            string          `json:"address,omitempty"`
	Latitude           float64         `json:"latitude,omitempty"`
	Longitude          float64         `json:"longitude,omitempty"`
	PackageID          string          `json:"packageId,omitempty"`
	StickerID          string          `json:"stickerId,omitempty"`
	AltText            string          `json:"altText,omitempty"`
	Template           interface{}     `json:"template,omitempty"`
	QuickReply         *lineQuickReply `json:"quickReply,omitempty"`
}

type lineQuickReply struct {
	Items []lineQuickReplyItem `json:"items"`
}

type lineQuickReplyItem struct {
	Type     string               `json:"type"`
	ImageURL string               `json:"imageUrl,omitempty"`
	Action   lineQuickReplyAction `json:"action"`
}

type lineQuickReplyAction struct {
	Type           string `json:"type"`
	Label          string `json:"label,omitempty"`
	Message        string `json:"message,omitempty"`
	URI            string `json:"uri,omitempty"`
	DatetimePicker *struct {
		Mode    string `json:"mode"`
		Initial string `json:"initial,omitempty"`
		Max     string `json:"max,omitempty"`
		Min     string `json:"min,omitempty"`
	} `json:"datetimePicker,omitempty"`
}

func NewLineGateway(channelSecret, channelToken string) *LineGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &LineGateway{
		channelSecret: channelSecret,
		channelToken:  channelToken,
	}

	g.BasePlatform = NewBasePlatform("line", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend
	g.SetCallbackPort(8087)

	return g
}

func (g *LineGateway) onConnect(ctx context.Context) error {
	if g.channelSecret == "" || g.channelToken == "" {
		return fmt.Errorf("LINE channel_secret and channel_token are required")
	}

	log.Infof("[LINE] Connecting with webhook server on port %d", g.GetCallbackPort())

	go g.startHTTPServer()

	log.Info("[LINE] Gateway connected (webhook server started)")
	return nil
}

func (g *LineGateway) onDisconnect() error {
	if g.httpServer != nil {
		g.httpServer.Shutdown(context.Background())
		g.httpServer = nil
	}

	log.Info("[LINE] Gateway disconnected")
	return nil
}

func (g *LineGateway) onSend(ctx context.Context, resp Response) error {
	to := resp.ChannelID
	if to == "" {
		return fmt.Errorf("user ID (channel_id) is required")
	}

	text := resp.Content
	if text == "" {
		return nil
	}

	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{Type: "text", Text: text},
		},
	}

	return g.pushMessage(ctx, reqBody)
}

func (g *LineGateway) Reply(replyToken, text string) error {
	if replyToken == "" {
		return fmt.Errorf("reply token is required")
	}

	reqBody := lineReplyRequest{
		ReplyToken: replyToken,
		Messages: []lineMessageContent{
			{Type: "text", Text: text},
		},
	}

	return g.sendReply(context.Background(), reqBody)
}

func (g *LineGateway) pushMessage(ctx context.Context, reqBody linePushRequest) error {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.line.me/v2/bot/message/push", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.channelToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LINE API error: %s", string(body))
	}

	return nil
}

func (g *LineGateway) sendReply(ctx context.Context, reqBody lineReplyRequest) error {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.line.me/v2/bot/message/reply", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.channelToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send reply: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LINE API error: %s", string(body))
	}

	return nil
}

func (g *LineGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch strings.ToLower(cmd) {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - LINE\n\n" +
				"📋 Commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Connection status\n" +
				"/new - New conversation\n" +
				"/compress - Compress context\n" +
				"/goal - Goal management",
		}, nil
	case "ping":
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if g.IsConnected() {
			return Response{Content: "✅ Connected and ready!"}, nil
		}
		return Response{Content: "❌ Not connected"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

func (g *LineGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "line"
	status.CallbackPort = g.GetCallbackPort()
	status.Details["callback_port"] = g.GetCallbackPort()

	if status.Platforms == nil {
		status.Platforms = make(map[string]PlatformStatus)
	}

	platformStatus := PlatformStatus{
		Name:   "line",
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
		status.Platforms["line"] = platformStatus
		return status
	}

	status.Details["user_id"] = g.userID
	status.Platforms["line"] = platformStatus
	return status
}

func (g *LineGateway) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", g.handleWebhook)

	g.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.GetCallbackPort()),
		Handler: mux,
	}

	go func() {
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("[LINE] HTTP server error: %v", err)
			g.HandleDisconnection(fmt.Errorf("http server error: %w", err))
		}
	}()
}

func (g *LineGateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	signature := r.Header.Get("X-Line-Signature")
	if !g.verifySignature(r, signature) {
		log.Warnf("[LINE] Invalid webhook signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookReq lineWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&webhookReq); err != nil {
		log.Errorf("[LINE] Failed to decode webhook: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	for _, event := range webhookReq.Events {
		msg := g.processEvent(&event)
		if msg != nil {
			if g.ShouldProcessChannel(msg.ChannelID) {
				g.EmitMessage(*msg)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (g *LineGateway) verifySignature(r *http.Request, signature string) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}

	r.Body = io.NopCloser(bytes.NewReader(body))

	hash := hmac.New(sha256.New, []byte(g.channelSecret))
	hash.Write(body)
	expectedSig := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

func (g *LineGateway) processEvent(event *struct {
	Type       string `json:"type"`
	ReplyToken string `json:"replyToken,omitempty"`
	Source     struct {
		Type    string `json:"type"`
		UserID  string `json:"userId,omitempty"`
		GroupID string `json:"groupId,omitempty"`
		RoomID  string `json:"roomId,omitempty"`
	} `json:"source"`
	Timestamp int64  `json:"timestamp"`
	Mode      string `json:"mode,omitempty"`
	Message   struct {
		Type            string `json:"type"`
		ID              string `json:"id"`
		Text            string `json:"text,omitempty"`
		ContentProvider *struct {
			Type               string `json:"type"`
			OriginalContentURL string `json:"originalContentUrl,omitempty"`
			PreviewImageURL    string `json:"previewImageUrl,omitempty"`
		} `json:"contentProvider,omitempty"`
		Duration   int     `json:"duration,omitempty"`
		Title      string  `json:"title,omitempty"`
		Address    string  `json:"address,omitempty"`
		Latitude   float64 `json:"latitude,omitempty"`
		Longitude  float64 `json:"longitude,omitempty"`
		PackageID  string  `json:"packageId,omitempty"`
		StickerID  string  `json:"stickerId,omitempty"`
		QuoteToken string  `json:"quoteToken,omitempty"`
	} `json:"message,omitempty"`
	Postback *struct {
		Data   string                 `json:"data"`
		Params map[string]interface{} `json:"params,omitempty"`
	} `json:"postback,omitempty"`
	Beacon *struct {
		Type          string `json:"type"`
		Hwid          string `json:"hwid"`
		DeviceMessage string `json:"deviceMessage,omitempty"`
	} `json:"beacon,omitempty"`
	Join         *struct{} `json:"join,omitempty"`
	Leave        *struct{} `json:"leave,omitempty"`
	MemberJoined *struct {
		Members []struct {
			Type   string `json:"type"`
			UserID string `json:"userId"`
		} `json:"members"`
	} `json:"memberJoined,omitempty"`
	MemberLeft *struct {
		Members []struct {
			Type   string `json:"type"`
			UserID string `json:"userId"`
		} `json:"members"`
	} `json:"memberLeft,omitempty"`
	Unsend *struct {
		MessageID string `json:"messageId"`
	} `json:"unsend,omitempty"`
	AccountLink *struct {
		Result string `json:"result"`
		Nonce  string `json:"nonce"`
	} `json:"accountLink,omitempty"`
	Things *struct {
		Type     string `json:"type"`
		DeviceID string `json:"deviceId"`
	} `json:"things,omitempty"`
}) *Message {
	var userID string
	var channelID string
	switch event.Source.Type {
	case "user":
		userID = event.Source.UserID
		channelID = event.Source.UserID
	case "group":
		userID = event.Source.UserID
		channelID = event.Source.GroupID
	case "room":
		userID = event.Source.UserID
		channelID = event.Source.RoomID
	}

	var content string
	var msgType string

	switch event.Type {
	case "message":
		msgType = event.Message.Type
		switch event.Message.Type {
		case "text":
			content = event.Message.Text
		case "image":
			content = "Image:"
		case "video":
			content = "Video:"
		case "audio":
			content = "Audio:"
		case "file":
			content = "File:"
		case "location":
			content = fmt.Sprintf("Location: %s, %s",
				event.Message.Title, event.Message.Address)
		case "sticker":
			content = "Sticker:"
		default:
			content = fmt.Sprintf("%s:", event.Message.Type)
		}
	case "postback":
		content = event.Postback.Data
		msgType = "postback"
	case "beacon":
		content = fmt.Sprintf("Beacon: %s", event.Beacon.Hwid)
		msgType = "beacon"
	case "join":
		content = "Bot joined the group/room"
		msgType = "system"
	case "leave":
		content = "Bot left the group/room"
		msgType = "system"
	case "memberJoined":
		content = "Member joined"
		msgType = "system"
	case "memberLeft":
		content = "Member left"
		msgType = "system"
	case "unsend":
		content = "Message unsent"
		msgType = "system"
	case "accountLink":
		content = "Account linked"
		msgType = "system"
	default:
		return nil
	}

	isGroup := event.Source.Type == "group" || event.Source.Type == "room"
	isMentioned := false

	return &Message{
		ID:          event.Message.ID,
		Platform:    "line",
		ChannelID:   channelID,
		UserID:      userID,
		Content:     content,
		Timestamp:   time.Unix(event.Timestamp/1000, 0),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"type":         event.Type,
			"message_type": msgType,
			"reply_token":  event.ReplyToken,
			"source_type":  event.Source.Type,
		},
	}
}

func (g *LineGateway) SendText(to, text string) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{Type: "text", Text: text},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

func (g *LineGateway) SendImage(to, originalURL, previewURL string) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{
				Type:               "image",
				OriginalContentURL: originalURL,
				PreviewImageURL:    previewURL,
			},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

func (g *LineGateway) SendLocation(to, title, address string, lat, lon float64) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{
				Type:      "location",
				Title:     title,
				Address:   address,
				Latitude:  lat,
				Longitude: lon,
			},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

func (g *LineGateway) SendTemplate(to, altText string, template interface{}) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{
				Type:     "template",
				AltText:  altText,
				Template: template,
			},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

func (g *LineGateway) GetUserProfile(userID string) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", "https://api.line.me/v2/bot/profile/"+userID, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+g.channelToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var profile map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return profile, nil
}

func (g *LineGateway) GetGroupMemberProfile(groupID, userID string) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.line.me/v2/bot/group/%s/member/%s", groupID, userID), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+g.channelToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var profile map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return profile, nil
}

func (g *LineGateway) LeaveGroup(groupID string) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("https://api.line.me/v2/bot/group/%s/leave", groupID), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+g.channelToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (g *LineGateway) LeaveRoom(roomID string) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("https://api.line.me/v2/bot/room/%s/leave", roomID), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+g.channelToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

type RichMenu struct {
	Size        richMenuSize `json:"size"`
	Selected    bool         `json:"selected"`
	Name        string       `json:"name"`
	ChatBarText string       `json:"chatBarText"`
	Areas       []struct {
		Bounds richMenuBounds `json:"bounds"`
		Action interface{}    `json:"action"`
	} `json:"areas"`
}

type richMenuSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type richMenuBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
