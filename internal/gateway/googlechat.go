package gateway

// Google Chat adapter.
//
// Sends through a space's incoming webhook URL (chat.googleapis.com) — the
// simplest credential-free way to post into a space. Receives through the
// Google Chat Events API: Google delivers event JSON to
// POST /gchat/events?token=<events_token> on the callback port.
// Configuration (gateway.platforms.googlechat):
//
//	webhook_url   — space incoming webhook URL (https://chat.googleapis.com/v1/spaces/…)
//	events_token  — optional shared secret appended as ?token= on inbound events;
//	                when set, inbound requests without it are rejected
//
// Notes / limitations:
//   - Connect validates the webhook URL shape, acquires the OAuth/authorization
//     is not needed for incoming-webhook sends, then binds the callback
//     listener. "Connected" is only reported once both are live.
//   - Replies are routed to the space that owns the configured webhook_url.
//     Events arriving from a *different* space (the bot added to several
//     spaces under one app) cannot be answered through this single webhook —
//     the adapter reports an error for that case.
//   - Inbound events are only accepted for bot mention (argumentText) or
//     direct messages, matching the gateway's mention-based group semantics.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

const (
	// googleChatWebhookPrefix is the base URL of every space incoming webhook.
	googleChatWebhookPrefix = "https://chat.googleapis.com/v1/spaces/"
	// defaultGoogleChatCallbackPort receives Google Chat events.
	defaultGoogleChatCallbackPort = 8089
)

type GoogleChatGateway struct {
	*BasePlatform

	webhookURL  string // space incoming webhook URL (used to send)
	eventsToken string // optional shared secret checked on inbound events

	mu         sync.RWMutex
	spaceName  string // parsed from webhookURL ("spaces/…")
	httpServer *http.Server
}

// googleChatEvent is the subset of the Google Chat event payload we consume.
type googleChatEvent struct {
	Type    string `json:"type"` // "message" | "membership" | …
	Message struct {
		Name         string `json:"name"` // "spaces/AAA/messages/BBB"
		Text         string `json:"text"`
		ArgumentText string `json:"argumentText"` // text after the bot @mention
		CreateTime   string `json:"createTime"`
		Sender       struct {
			Name        string `json:"name"` // "users/123"
			DisplayName string `json:"displayName"`
			Type        string `json:"type"` // "HUMAN" | "BOT"
		} `json:"sender"`
		Space struct {
			Name        string `json:"name"` // "spaces/AAA"
			Type        string `json:"type"` // "ROOM" | "DM"
			DisplayName string `json:"displayName"`
		} `json:"space"`
		SingleUserBotDm bool `json:"singleUserBotDm"`
	} `json:"message"`
	Space struct {
		Name string `json:"name"`
	} `json:"space"`
}

func NewGoogleChatGateway(webhookURL, eventsToken string) *GoogleChatGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &GoogleChatGateway{
		webhookURL:  strings.TrimRight(webhookURL, "/"),
		eventsToken: eventsToken,
	}
	if idx := strings.Index(g.webhookURL, googleChatWebhookPrefix); idx >= 0 {
		rest := strings.TrimPrefix(g.webhookURL[idx:], googleChatWebhookPrefix)
		if i := strings.IndexAny(rest, "/?"); i >= 0 {
			rest = rest[:i]
		}
		if strings.HasPrefix(rest, "spaces/") {
			g.spaceName = rest
		}
	}

	g.BasePlatform = NewBasePlatform("googlechat", config)
	g.SetCallbackPort(defaultGoogleChatCallbackPort)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *GoogleChatGateway) onConnect(ctx context.Context) error {
	if g.webhookURL == "" || !strings.HasPrefix(g.webhookURL, googleChatWebhookPrefix) {
		g.markDisconnected(fmt.Errorf("googlechat not configured: a valid space webhook_url (https://chat.googleapis.com/v1/spaces/…) is required (set gateway.platforms.googlechat to enable)"))
		return nil
	}

	log.Infof("[GoogleChat] Connecting with webhook space %s, callback port %d",
		g.spaceName, g.GetCallbackPort())

	// Bind the callback listener synchronously so "connected" means the events
	// endpoint is actually live.
	addr := fmt.Sprintf(":%d", g.GetCallbackPort())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("googlechat callback listen on %s failed: %w", addr, err)
	}

	g.markConnected()
	go g.serve(ctx, ln)

	log.Infof("[GoogleChat] Gateway connected (callback on %s)", addr)
	return nil
}

func (g *GoogleChatGateway) onDisconnect() error {
	g.mu.Lock()
	srv := g.httpServer
	g.httpServer = nil
	g.mu.Unlock()

	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}

	log.Info("[GoogleChat] Gateway disconnected")
	return nil
}

func (g *GoogleChatGateway) serve(ctx context.Context, ln net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gchat/events", g.handleEvent)

	srv := &http.Server{Handler: mux}
	g.mu.Lock()
	g.httpServer = srv
	g.mu.Unlock()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Errorf("[GoogleChat] Callback server error: %v", err)
	}
}

func (g *GoogleChatGateway) onSend(ctx context.Context, resp Response) error {
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return nil
	}
	if g.webhookURL == "" {
		return fmt.Errorf("googlechat: webhook_url is not configured")
	}

	// The configured webhook only owns one space. Reject replies that target a
	// different space (bot added to several spaces under a single app) instead
	// of silently dropping the message into the wrong space.
	if resp.ChannelID != "" && g.spaceName != "" && resp.ChannelID != g.spaceName {
		return fmt.Errorf("googlechat: reply targets space %q but webhook_url owns %q — add a webhook per space or configure one bot app per space", resp.ChannelID, g.spaceName)
	}

	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("googlechat: failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.webhookURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("googlechat: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	respAPI, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("googlechat: send request failed: %w", err)
	}
	defer respAPI.Body.Close()

	if respAPI.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(respAPI.Body)
		return fmt.Errorf("googlechat: send API error (%d): %s", respAPI.StatusCode, string(rb))
	}
	return nil
}

// handleEvent processes Google Chat events delivered by Google.
func (g *GoogleChatGateway) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Optional shared-secret check: when events_token is configured, Google's
	// delivery URL must carry ?token=events_token.
	if g.eventsToken != "" && r.URL.Query().Get("token") != g.eventsToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var ev googleChatEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}

	msg := ev.Message
	spaceID := msg.Space.Name
	if spaceID == "" {
		spaceID = ev.Space.Name
	}

	if ev.Type != "message" || msg.Name == "" || strings.TrimSpace(msg.Text) == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	// Never loop on our own bot messages (sender.type "BOT").
	if strings.EqualFold(msg.Sender.Type, "BOT") {
		w.WriteHeader(http.StatusOK)
		return
	}

	isGroup := !msg.SingleUserBotDm && !strings.EqualFold(msg.Space.Type, "DM")
	// Google removes the @Bot prefix into argumentText when the bot is
	// mentioned; DMs need no mention.
	isMentioned := !isGroup || strings.TrimSpace(msg.ArgumentText) != ""

	content := msg.ArgumentText
	if strings.TrimSpace(content) == "" {
		content = msg.Text
	}

	outMsg := Message{
		ID:          msg.Name,
		Platform:    "googlechat",
		ChannelID:   spaceID,
		UserID:      msg.Sender.Name,
		Content:     strings.TrimSpace(content),
		Timestamp:   time.Now(),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"sender_name":   msg.Sender.DisplayName,
			"space_type":    msg.Space.Type,
			"space_display": msg.Space.DisplayName,
			"create_time":   msg.CreateTime,
		},
	}

	g.EmitMessage(outMsg)
	w.WriteHeader(http.StatusOK)
}
