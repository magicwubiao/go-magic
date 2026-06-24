package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

type WhatsAppBusinessGateway struct {
	*BasePlatform

	phoneNumberID string
	accessToken   string
	appSecret     string
	verifyToken   string
	webhookURL    string

	mu         sync.RWMutex
	httpServer *http.Server
}

func NewWhatsAppBusinessGateway(phoneNumberID, accessToken, appSecret, verifyToken string) *WhatsAppBusinessGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &WhatsAppBusinessGateway{
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		appSecret:     appSecret,
		verifyToken:   verifyToken,
	}

	g.BasePlatform = NewBasePlatform("whatsapp_business", config)
	g.SetCallbackPort(8082)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

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
						ID       string `json:"id"`
						MimeType string `json:"mime_type"`
						Filename string `json:"filename"`
						Caption  string `json:"caption"`
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

type whatsappMessageRequest struct {
	MessagingProduct string `json:"messaging_product"`
	RecipientType    string `json:"recipient_type"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Text             *struct {
		Body string `json:"body"`
	} `json:"text,omitempty"`
	Image *struct {
		ID      string `json:"id,omitempty"`
		Link    string `json:"link,omitempty"`
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

func (g *WhatsAppBusinessGateway) onConnect(ctx context.Context) error {
	log.Infof("[WhatsApp Business] Connecting gateway...")

	go g.startHTTPServer(ctx)

	log.Info("[WhatsApp Business] Gateway connected (webhook server started)")
	return nil
}

func (g *WhatsAppBusinessGateway) onDisconnect() error {
	g.mu.Lock()
	if g.httpServer != nil {
		g.httpServer.Shutdown(context.Background())
		g.httpServer = nil
	}
	g.mu.Unlock()

	log.Info("[WhatsApp Business] Gateway disconnected")
	return nil
}

func (g *WhatsAppBusinessGateway) onSend(ctx context.Context, resp Response) error {
	to := resp.ChannelID
	if to == "" {
		return fmt.Errorf("recipient phone number (channel_id) is required")
	}

	text := resp.Content
	if text == "" {
		return nil
	}

	reqBody := whatsappMessageRequest{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               to,
		Type:             "text",
		Text: &struct {
			Body string `json:"body"`
		}{Body: text},
	}

	return g.sendMessage(ctx, reqBody)
}

func (g *WhatsAppBusinessGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch strings.ToLower(cmd) {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - WhatsApp Business\n\n" +
				"📋 Commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Connection status",
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

func (g *WhatsAppBusinessGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "whatsapp_business"
	status.Platforms = make(map[string]PlatformStatus)

	platformStatus := PlatformStatus{
		Name:   "whatsapp_business",
		Status: "connected",
	}

	if !g.IsConnected() {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Platforms["whatsapp_business"] = platformStatus
		return status
	}

	status.Details["phone_number_id"] = g.phoneNumberID
	status.Details["callback_port"] = g.GetCallbackPort()
	status.Platforms["whatsapp_business"] = platformStatus

	return status
}

func (g *WhatsAppBusinessGateway) startHTTPServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", g.handleWebhook)

	addr := fmt.Sprintf(":%d", g.GetCallbackPort())
	g.mu.Lock()
	g.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	g.mu.Unlock()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		g.mu.Lock()
		if g.httpServer != nil {
			g.httpServer.Shutdown(shutdownCtx)
		}
		g.mu.Unlock()
	}()

	log.Infof("[WhatsApp Business] Webhook server starting on %s", addr)
	if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("[WhatsApp Business] HTTP server error: %v", err)
		g.HandleDisconnection(err)
	}
}

func (g *WhatsAppBusinessGateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode == "subscribe" && token == g.verifyToken {
			log.Info("[WhatsApp Business] Webhook verified")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge))
			return
		}

		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if g.appSecret != "" && !g.verifySignature(r, signature) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookReq whatsappWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&webhookReq); err != nil {
		log.Errorf("[WhatsApp Business] Failed to decode webhook: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	for _, entry := range webhookReq.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if !g.ShouldProcessChannel(msg.From) {
					continue
				}

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
				default:
					content = fmt.Sprintf("[%s]", msg.Type)
					msgType = msg.Type
				}

				timestamp, _ := strconv.ParseInt(msg.Timestamp, 10, 64)

				isGroup := false
				isMentioned := false

				waMsg := Message{
					ID:          msg.ID,
					ChannelID:   msg.From,
					UserID:      msg.From,
					Content:     content,
					Timestamp:   time.Unix(timestamp, 0),
					IsGroup:     isGroup,
					IsMentioned: isMentioned,
					Metadata: map[string]interface{}{
						"type":            msgType,
						"phone_number_id": g.phoneNumberID,
					},
				}

				g.EmitMessage(waMsg)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (g *WhatsAppBusinessGateway) verifySignature(r *http.Request, signature string) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}

	r.Body = io.NopCloser(strings.NewReader(string(body)))

	expectedSig := "sha256=" + hex.EncodeToString(
		hmac.New(sha256.New, []byte(g.appSecret)).Sum(body),
	)

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

func (g *WhatsAppBusinessGateway) sendMessage(ctx context.Context, reqBody whatsappMessageRequest) error {
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
