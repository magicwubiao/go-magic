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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// LineGateway implements the LINE Messaging API platform handler
type LineGateway struct {
	channelSecret string
	channelToken  string
	userID        string
	
	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool
	
	callbackPort int
	httpServer   *http.Server
}

// lineWebhookRequest represents incoming webhook data
type lineWebhookRequest struct {
	Destination string `json:"destination"`
	Events      []struct {
		Type       string `json:"type"`
		ReplyToken string `json:"replyToken,omitempty"`
		Source     struct {
			Type   string `json:"type"`
			UserID string `json:"userId,omitempty"`
			GroupID string `json:"groupId,omitempty"`
			RoomID  string `json:"roomId,omitempty"`
		} `json:"source"`
		Timestamp int64 `json:"timestamp"`
		Mode      string `json:"mode,omitempty"`
		Message   struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Text string `json:"text,omitempty"`
			ContentProvider *struct {
				Type   string `json:"type"`
				OriginalContentURL string `json:"originalContentUrl,omitempty"`
				PreviewImageURL string `json:"previewImageUrl,omitempty"`
			} `json:"contentProvider,omitempty"`
			Duration int `json:"duration,omitempty"`
			Title    string `json:"title,omitempty"`
			Address  string `json:"address,omitempty"`
			Latitude float64 `json:"latitude,omitempty"`
			Longitude float64 `json:"longitude,omitempty"`
			PackageID string `json:"packageId,omitempty"`
			StickerID string `json:"stickerId,omitempty"`
			QuoteToken string `json:"quoteToken,omitempty"`
		} `json:"message,omitempty"`
		Postback *struct {
			Data string `json:"data"`
			Params map[string]interface{} `json:"params,omitempty"`
		} `json:"postback,omitempty"`
		Beacon *struct {
			Type   string `json:"type"`
			Hwid   string `json:"hwid"`
			DeviceMessage string `json:"deviceMessage,omitempty"`
		} `json:"beacon,omitempty"`
		Join     *struct{} `json:"join,omitempty"`
		Leave    *struct{} `json:"leave,omitempty"`
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
			Type string `json:"type"`
			DeviceID string `json:"deviceId"`
		} `json:"things,omitempty"`
	} `json:"events"`
}

// lineReplyRequest represents reply message payload
type lineReplyRequest struct {
	ReplyToken string               `json:"replyToken"`
	Messages   []lineMessageContent `json:"messages"`
}

// linePushRequest represents push message payload
type linePushRequest struct {
	To       string               `json:"to"`
	Messages []lineMessageContent `json:"messages"`
}

// lineMessageContent represents LINE message content
type lineMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// For image/video/audio messages
	OriginalContentURL string `json:"originalContentUrl,omitempty"`
	PreviewImageURL    string `json:"previewImageUrl,omitempty"`
	// For location message
	Title       string  `json:"title,omitempty"`
	Address     string  `json:"address,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	// For sticker message
	PackageID string `json:"packageId,omitempty"`
	StickerID string `json:"stickerId,omitempty"`
	// For template message
	AltText  string                 `json:"altText,omitempty"`
	Template interface{}            `json:"template,omitempty"`
	// For quick reply
	QuickReply *lineQuickReply      `json:"quickReply,omitempty"`
}

// lineQuickReply represents quick reply buttons
type lineQuickReply struct {
	Items []lineQuickReplyItem `json:"items"`
}

// lineQuickReplyItem represents a quick reply item
type lineQuickReplyItem struct {
	Type  string `json:"type"`
	ImageURL string `json:"imageUrl,omitempty"`
	Action lineQuickReplyAction `json:"action"`
}

// lineQuickReplyAction represents a quick reply action
type lineQuickReplyAction struct {
	Type        string `json:"type"`
	Label       string `json:"label,omitempty"`
	Message     string `json:"message,omitempty"`
	URI         string `json:"uri,omitempty"`
	DatetimePicker *struct {
		Mode      string `json:"mode"`
		Initial   string `json:"initial,omitempty"`
		Max       string `json:"max,omitempty"`
		Min       string `json:"min,omitempty"`
	} `json:"datetimePicker,omitempty"`
}

// NewLineGateway creates a new LINE gateway
func NewLineGateway(channelSecret, channelToken string) *LineGateway {
	return &LineGateway{
		channelSecret: channelSecret,
		channelToken:  channelToken,
		agents:        make(map[string]*AgentSession),
		msgCh:         make(chan Message, 100),
		stopCh:        make(chan struct{}),
		callbackPort:  8087,
	}
}

// Name returns the platform name
func (g *LineGateway) Name() string {
	return "line"
}

// Connect establishes connection to LINE
func (g *LineGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to LINE gateway...")

	// Start webhook server
	go g.startHTTPServer()

	log.Info("LINE gateway connected (webhook server started)")
	return nil
}

// Disconnect closes the connection
func (g *LineGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	close(g.stopCh)
	if g.httpServer != nil {
		g.httpServer.Shutdown(context.Background())
	}
	close(g.msgCh)
	g.running = false

	log.Info("LINE gateway disconnected")
	return nil
}

// CheckHealth returns health status
func (g *LineGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	running := g.running
	g.mu.RUnlock()

	return &HealthStatus{
		Platform:   "line",
		Connected: running,
		Details: map[string]interface{}{
			"callback_port": g.callbackPort,
		},
	}
}

// Receive returns the message channel
func (g *LineGateway) Receive() <-chan Message {
	return g.msgCh
}

// Send sends a push message via LINE
func (g *LineGateway) Send(ctx context.Context, resp Response) error {
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

// Reply sends a reply using reply token
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

// pushMessage sends a push message
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

// sendReply sends a reply message
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

// HandleSlashCommand handles commands (not applicable for LINE)
func (g *LineGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	return Response{
		ChannelID: msg.UserID,
		Content:   "Commands are not supported on LINE",
	}, nil
}

// startHTTPServer starts the webhook server
func (g *LineGateway) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", g.handleWebhook)

	g.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.callbackPort),
		Handler: mux,
	}

	go func() {
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("LINE HTTP server error: %v", err)
		}
	}()
}

// handleWebhook handles incoming LINE webhooks
func (g *LineGateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Line-Signature")
	if !g.verifySignature(r, signature) {
		log.Warnf("Invalid LINE webhook signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookReq lineWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&webhookReq); err != nil {
		log.Errorf("Failed to decode webhook: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Process events
	for _, event := range webhookReq.Events {
		msg := g.processEvent(&event)
		if msg != nil {
			select {
			case g.msgCh <- *msg:
			default:
				log.Warnf("LINE message channel full, dropping message")
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature verifies the webhook signature
func (g *LineGateway) verifySignature(r *http.Request, signature string) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}

	// Restore body
	r.Body = io.NopCloser(bytes.NewReader(body))

	hash := hmac.New(sha256.New, []byte(g.channelSecret))
	hash.Write(body)
	expectedSig := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// processEvent processes a LINE event and converts it to our Message format
func (g *LineGateway) processEvent(event *struct {
	Type       string `json:"type"`
	ReplyToken string `json:"replyToken,omitempty"`
	Source     struct {
		Type   string `json:"type"`
		UserID string `json:"userId,omitempty"`
		GroupID string `json:"groupId,omitempty"`
		RoomID  string `json:"roomId,omitempty"`
	} `json:"source"`
	Timestamp int64 `json:"timestamp"`
	Mode      string `json:"mode,omitempty"`
	Message   struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Text string `json:"text,omitempty"`
		ContentProvider *struct {
			Type   string `json:"type"`
			OriginalContentURL string `json:"originalContentUrl,omitempty"`
			PreviewImageURL string `json:"previewImageUrl,omitempty"`
		} `json:"contentProvider,omitempty"`
		Duration int `json:"duration,omitempty"`
		Title    string `json:"title,omitempty"`
		Address  string `json:"address,omitempty"`
		Latitude float64 `json:"latitude,omitempty"`
		Longitude float64 `json:"longitude,omitempty"`
		PackageID string `json:"packageId,omitempty"`
		StickerID string `json:"stickerId,omitempty"`
		QuoteToken string `json:"quoteToken,omitempty"`
	} `json:"message,omitempty"`
	Postback *struct {
		Data string `json:"data"`
		Params map[string]interface{} `json:"params,omitempty"`
	} `json:"postback,omitempty"`
}) *Message {
	// Get user ID from source
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

	// Process different event types
	var content string
	var msgType string

	switch event.Type {
	case "message":
		msgType = event.Message.Type
		switch event.Message.Type {
		case "text":
			content = event.Message.Text
		case "image":
			content = "[Image]"
		case "video":
			content = "[Video]"
		case "audio":
			content = "[Audio]"
		case "file":
			content = "[File]"
		case "location":
			content = fmt.Sprintf("[Location: %s, %s]", 
				event.Message.Title, event.Message.Address)
		case "sticker":
			content = "[Sticker]"
		default:
			content = fmt.Sprintf("[%s]", event.Message.Type)
		}
	case "postback":
		content = event.Postback.Data
		msgType = "postback"
	case "beacon":
		content = fmt.Sprintf("[Beacon: %s]", event.Beacon.Hwid)
		msgType = "beacon"
	case "join":
		content = "[Bot joined the group/room]"
		msgType = "system"
	case "leave":
		content = "[Bot left the group/room]"
		msgType = "system"
	case "memberJoined":
		content = "[Member joined]"
		msgType = "system"
	case "memberLeft":
		content = "[Member left]"
		msgType = "system"
	case "unsend":
		content = "[Message unsent]"
		msgType = "system"
	case "accountLink":
		content = "[Account linked]"
		msgType = "system"
	default:
		return nil
	}

	return &Message{
		ID:         event.Message.ID,
		Platform:   "line",
		ChannelID:  channelID,
		UserID:     userID,
		Content:    content,
		Timestamp:  time.Unix(event.Timestamp/1000, 0),
		Metadata: map[string]interface{}{
			"type":        event.Type,
			"message_type": msgType,
			"reply_token":  event.ReplyToken,
			"source_type":   event.Source.Type,
		},
	}
}

// SendText sends a text message
func (g *LineGateway) SendText(to, text string) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{Type: "text", Text: text},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

// SendImage sends an image message
func (g *LineGateway) SendImage(to, originalURL, previewURL string) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{
				Type: "image",
				OriginalContentURL: originalURL,
				PreviewImageURL: previewURL,
			},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

// SendLocation sends a location message
func (g *LineGateway) SendLocation(to, title, address string, lat, lon float64) error {
	reqBody := linePushRequest{
		To: to,
		Messages: []lineMessageContent{
			{
				Type:    "location",
				Title:   title,
				Address: address,
				Latitude: lat,
				Longitude: lon,
			},
		},
	}
	return g.pushMessage(context.Background(), reqBody)
}

// SendTemplate sends a template message
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

// GetUserProfile gets user profile information
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

// GetGroupMemberProfile gets group member profile
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

// LeaveGroup makes the bot leave a group
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

// LeaveRoom makes the bot leave a room
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

// RichMenu represents LINE rich menu
type RichMenu struct {
	Size        richMenuSize `json:"size"`
	Selected    bool         `json:"selected"`
	Name        string       `json:"name"`
	ChatBarText string       `json:"chatBarText"`
	Areas       []struct {
		Bounds   richMenuBounds `json:"bounds"`
		Action   interface{}    `json:"action"`
	} `json:"areas"`
}

// richMenuSize represents rich menu size
type richMenuSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// richMenuBounds represents rich menu bounds
type richMenuBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
