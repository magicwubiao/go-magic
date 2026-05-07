package gateway

import (
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

// SlackGateway implements the Slack platform handler
type SlackGateway struct {
	botToken   string
	appToken   string
	signingSecret string
	wsURL      string
	rtmConn    *rtmConnection
	
	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool
	
	callbackPort int
	httpServer   *http.Server
}

// rtmConnection represents RTM websocket connection
type rtmConnection struct {
	URL string `json:"url"`
}

// slackEvent represents incoming Slack events
type slackEvent struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge,omitempty"`
	Channel   string          `json:"channel,omitempty"`
	User      string          `json:"user,omitempty"`
	Text      string          `json:"text,omitempty"`
	Ts        string          `json:"ts,omitempty"`
	EventTs   string          `json:"event_ts,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// slackMessageEvent represents a message event
type slackMessageEvent struct {
	Type      string `json:"type"`
	Channel   string `json:"channel"`
	User      string `json:"user"`
	Text      string `json:"text"`
	Ts        string `json:"ts"`
	ThreadTs  string `json:"thread_ts,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
}

// NewSlackGateway creates a new Slack gateway
func NewSlackGateway(botToken, signingSecret string) *SlackGateway {
	return &SlackGateway{
		botToken:      botToken,
		signingSecret: signingSecret,
		agents:        make(map[string]*AgentSession),
		msgCh:         make(chan Message, 100),
		stopCh:        make(chan struct{}),
		callbackPort:  8085,
	}
}

// Name returns the platform name
func (g *SlackGateway) Name() string {
	return "slack"
}

// Connect establishes connection to Slack
func (g *SlackGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to Slack gateway...")

	// Get RTM websocket URL
	if err := g.startRTM(); err != nil {
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
		return fmt.Errorf("failed to start RTM: %w", err)
	}

	// Start HTTP server for events
	go g.startHTTPServer()

	log.Info("Slack gateway connected")
	return nil
}

// Disconnect closes the connection
func (g *SlackGateway) Disconnect() error {
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

	log.Info("Slack gateway disconnected")
	return nil
}

// CheckHealth returns health status
func (g *SlackGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	running := g.running
	g.mu.RUnlock()

	return &HealthStatus{
		Platform:   "slack",
		Connected: running,
		Details: map[string]interface{}{
			"callback_port": g.callbackPort,
		},
	}
}

// Receive returns the message channel
func (g *SlackGateway) Receive() <-chan Message {
	return g.msgCh
}

// Send sends a message to Slack
func (g *SlackGateway) Send(ctx context.Context, resp Response) error {
	channel := resp.ChannelID
	if channel == "" {
		return fmt.Errorf("channel ID is required")
	}

	text := resp.Content

	payload := map[string]interface{}{
		"channel": channel,
		"text":    text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.botToken)

	client := &http.Client{Timeout: 10 * time.Second}
	respAPI, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer respAPI.Body.Close()

	if respAPI.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respAPI.Body)
		return fmt.Errorf("slack API error: %s", string(body))
	}

	return nil
}

// HandleSlashCommand handles slash commands
func (g *SlackGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	// For slash commands, we respond in the same channel
	return Response{
		ChannelID: msg.ChannelID,
		Content:   fmt.Sprintf("Command /%s received", cmd),
	}, nil
}

// startRTM starts the RTM connection
func (g *SlackGateway) startRTM() error {
	req, err := http.NewRequest("POST", "https://slack.com/api/rtm.connect", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.botToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to RTM: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read RTM response: %w", err)
	}

	var result struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse RTM response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("RTM connect error: %s", result.Error)
	}

	g.wsURL = result.URL
	return nil
}

// startHTTPServer starts the HTTP server for events
func (g *SlackGateway) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/events", g.handleSlackEvents)
	mux.HandleFunc("/slack/interactive", g.handleInteractive)

	g.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.callbackPort),
		Handler: mux,
	}

	go func() {
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("Slack HTTP server error: %v", err)
		}
	}()
}

// handleSlackEvents handles incoming Slack events
func (g *SlackGateway) handleSlackEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// URL verification challenge
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Query().Get("challenge")))
		return
	}

	var event slackEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Handle URL verification
	if event.Type == "url_verification" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
		return
	}

	if event.Type == "event_callback" {
		var msgEvent slackMessageEvent
		if err := json.Unmarshal(event.Raw, &msgEvent); err != nil {
			log.Errorf("Failed to parse message event: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Ignore bot messages
		if msgEvent.User == "" || strings.HasPrefix(msgEvent.Text, "<@U") {
			w.WriteHeader(http.StatusOK)
			return
		}

		msg := Message{
			ID:         msgEvent.Ts,
			Platform:   "slack",
			ChannelID:  msgEvent.Channel,
			UserID:     msgEvent.User,
			Content:    msgEvent.Text,
			Timestamp:  time.Now(),
			Metadata:   map[string]interface{}{"thread_ts": msgEvent.ThreadTs},
		}

		select {
		case g.msgCh <- msg:
		default:
			log.Warnf("Slack message channel full, dropping message")
		}
	}

	w.WriteHeader(http.StatusOK)
}

// handleInteractive handles interactive payloads
func (g *SlackGateway) handleInteractive(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Channel   string `json:"channel"`
		User      string `json:"user"`
		Text      string `json:"text"`
		TriggerID string `json:"trigger_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// postMessage posts a message using chat.postMessage API
func (g *SlackGateway) postMessage(channel, text string) error {
	payload := map[string]interface{}{
		"channel": channel,
		"text":    text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.botToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}

	if !result.OK {
		return fmt.Errorf("slack error: %s", result.Error)
	}

	return nil
}
