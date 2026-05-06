package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// WhatsAppGateway implements WhatsApp Business API platform handler
type WhatsAppGateway struct {
	phoneNumberID string
	accessToken   string
	appSecret     string
	verifyToken   string
	webhookURL    string
	
	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool
	
	callbackPort int
	httpServer   *http.Server
}

// whatsappWebhookRequest represents incoming webhook data
type whatsappWebhookRequest struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Value struct {
				MessagingProduct string `json:"messaging_product"`
				Metadata         struct {
					DisplayPhoneNumber string `json:"display_phone_number"`
					PhoneNumberID      string `json:"phone_number_id"`
				} `json:"metadata"`
				Contacts []struct {
					Profile struct {
						Name string `json:"name"`
					} `json:"profile"`
					WAID string `json:"wa_id"`
				} `json:"contacts"`
				Messages []struct {
					From      string `json:"from"`
					ID        string `json:"id"`
					Timestamp string `json:"timestamp"`
					Type      string `json:"type"`
					Text      struct {
						Body string `json:"body"`
					} `json:"text,omitempty"`
					Image *struct {
						ID       string `json:"id"`
						MimeType string `json:"mime_type"`
						SHA256   string `json:"sha256"`
					} `json:"image,omitempty"`
					Audio *struct {
						ID       string `json:"id"`
						MimeType string `json:"mime_type"`
					} `json:"audio,omitempty"`
					Document *struct {
						ID          string `json:"id"`
						MimeType    string `json:"mime_type"`
						Filename    string `json:"filename"`
						Caption     string `json:"caption"`
					} `json:"document,omitempty"`
					Location *struct {
						Latitude  float64 `json:"latitude"`
						Longitude float64 `json:"longitude"`
						Name      string  `json:"name"`
						Address   string  `json:"address"`
					} `json:"location,omitempty"`
					Sticker *struct {
						ID       string `json:"id"`
						MimeType string `json:"mime_type"`
					} `json:"sticker,omitempty"`
					Context *struct {
						From string `json:"from"`
						ID   string `json:"id"`
					} `json:"context,omitempty"`
				} `json:"messages"`
			} `json:"value"`
			Field string `json:"field"`
		} `json:"changes"`
	} `json:"entry"`
}

// whatsappMessageRequest represents outgoing message payload
type whatsappMessageRequest struct {
	MessagingProduct string `json:"messaging_product"`
	RecipientType    string `json:"recipient_type"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Text             *struct {
		Body string `json:"body"`
	} `json:"text,omitempty"`
	Image *struct {
		ID    string `json:"id,omitempty"`
		Link  string `json:"link,omitempty"`
		Caption string `json:"caption,omitempty"`
	} `json:"image,omitempty"`
	Document *struct {
		ID       string `json:"id,omitempty"`
		Link     string `json:"link,omitempty"`
		Caption  string `json:"caption,omitempty"`
		Filename string `json:"filename,omitempty"`
	} `json:"document,omitempty"`
	Audio *struct {
		ID   string `json:"id,omitempty"`
		Link string `json:"link,omitempty"`
	} `json:"audio,omitempty"`
}

// NewWhatsAppGateway creates a new WhatsApp gateway
func NewWhatsAppGateway(phoneNumberID, accessToken, appSecret, verifyToken string) *WhatsAppGateway {
	return &WhatsAppGateway{
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		appSecret:     appSecret,
		verifyToken:   verifyToken,
		agents:        make(map[string]*AgentSession),
		msgCh:         make(chan Message, 100),
		stopCh:        make(chan struct{}),
		callbackPort:  8086,
	}
}

// Name returns the platform name
func (g *WhatsAppGateway) Name() string {
	return "whatsapp"
}

// Connect establishes connection to WhatsApp Business API
func (g *WhatsAppGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to WhatsApp gateway...")

	// Start webhook server
	go g.startHTTPServer()

	log.Info("WhatsApp gateway connected (webhook server started)")
	return nil
}

// Disconnect closes the connection
func (g *WhatsAppGateway) Disconnect() error {
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

	log.Info("WhatsApp gateway disconnected")
	return nil
}

// CheckHealth returns health status
func (g *WhatsAppGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	running := g.running
	g.mu.RUnlock()

	return &HealthStatus{
		Platform:   "whatsapp",
		Connected: running,
		Details: map[string]interface{}{
			"phone_number_id": g.phoneNumberID,
			"callback_port":  g.callbackPort,
		},
	}
}

// Receive returns the message channel
func (g *WhatsAppGateway) Receive() <-chan Message {
	return g.msgCh
}

// Send sends a message via WhatsApp Business API
func (g *WhatsAppGateway) Send(ctx context.Context, resp Response) error {
	to := resp.ChannelID
	if to == "" {
		return fmt.Errorf("recipient phone number (channel_id) is required")
	}

	text := resp.Content
	if text == "" {
		return nil // Skip empty messages
	}

	reqBody := whatsappMessageRequest{
		MessagingProduct: "whisperers",
		RecipientType:    "individual",
		To:               to,
		Type:             "text",
		Text:             &struct{ Body string }{Body: text},
	}

	return g.sendMessage(ctx, reqBody)
}

// sendMessage sends a message payload to WhatsApp API
func (g *WhatsAppGateway) sendMessage(ctx context.Context, reqBody whatsappMessageRequest) error {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", g.phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.accessToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WhatsApp API error: %s", string(body))
	}

	return nil
}

// HandleSlashCommand handles commands (not applicable for WhatsApp)
func (g *WhatsAppGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	return Response{
		ChannelID: msg.UserID,
		Content:   "Slash commands are not supported on WhatsApp",
	}, nil
}

// startHTTPServer starts the webhook server
func (g *WhatsAppGateway) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", g.handleWebhook)

	g.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.callbackPort),
		Handler: mux,
	}

	go func() {
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("WhatsApp HTTP server error: %v", err)
		}
	}()
}

// handleWebhook handles incoming WhatsApp webhooks
func (g *WhatsAppGateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Verify webhook
	if r.Method == "GET" {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode == "subscribe" && token == g.verifyToken {
			log.Info("WhatsApp webhook verified")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge))
			return
		}

		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Verify signature for POST requests
	signature := r.Header.Get("X-Hub-Signature-256")
	if g.appSecret != "" && !g.verifySignature(r, signature) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookReq whatsappWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&webhookReq); err != nil {
		log.Errorf("Failed to decode webhook: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Process messages
	for _, entry := range webhookReq.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				var content string
				var msgType string

				switch msg.Type {
				case "text":
					content = msg.Text.Body
					msgType = "text"
				case "image":
					content = "[Image]"
					msgType = "image"
				case "audio":
					content = "[Audio]"
					msgType = "audio"
				case "document":
					content = "[Document: " + msg.Document.Filename + "]"
					msgType = "document"
				case "location":
					content = fmt.Sprintf("[Location: %s, %s]", 
						strconv.FormatFloat(msg.Location.Latitude, 'f', 6, 64),
						strconv.FormatFloat(msg.Location.Longitude, 'f', 6, 64))
					msgType = "location"
				case "sticker":
					content = "[Sticker]"
					msgType = "sticker"
				default:
					content = fmt.Sprintf("[%s]", msg.Type)
					msgType = msg.Type
				}

				timestamp, _ := strconv.ParseInt(msg.Timestamp, 10, 64)

				waMsg := Message{
					ID:         msg.ID,
					Platform:   "whatsapp",
					ChannelID:  msg.From,
					UserID:     msg.From,
					Content:    content,
					Timestamp:  time.Unix(timestamp, 0),
					Metadata: map[string]interface{}{
						"type":           msgType,
						"phone_number_id": g.phoneNumberID,
						"reply_to":       msg.Context.ID,
					},
				}

				select {
				case g.msgCh <- waMsg:
				default:
					log.Warnf("WhatsApp message channel full, dropping message")
				}
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature verifies the webhook signature
func (g *WhatsAppGateway) verifySignature(r *http.Request, signature string) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}

	// Restore body for later use
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	expectedSig := "sha256=" + hex.EncodeToString(
		hmac.New(sha256.New, []byte(g.appSecret)).Sum(body),
	)

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// DownloadMedia downloads media from WhatsApp
func (g *WhatsAppGateway) DownloadMedia(mediaID string) ([]byte, string, error) {
	// Get media URL
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s", mediaID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+g.accessToken)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	var mediaInfo struct {
		URL       string `json:"url"`
		MimeType  string `json:"mime_type"`
		Hash      string `json:"sha256"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mediaInfo); err != nil {
		return nil, "", err
	}

	// Download media
	req, err = http.NewRequest("GET", mediaInfo.URL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+g.accessToken)

	resp, err = client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	return data, mediaInfo.MimeType, nil
}

// MarkMessageAsRead marks a message as read
func (g *WhatsAppGateway) MarkMessageAsRead(messageID string) error {
	payload := map[string]interface{}{
		"messaging_product": "whisperers",
		"message_id":        messageID,
	}

	jsonData, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", g.phoneNumberID)
	
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
