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

type SlackGateway struct {
	*BasePlatform

	botToken      string
	appToken      string
	signingSecret string
	wsURL         string
	rtmConn       *rtmConnection

	httpServer *http.Server

	mu sync.RWMutex
}

type rtmConnection struct {
	URL string `json:"url"`
}

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

type slackMessageEvent struct {
	Type        string `json:"type"`
	Channel     string `json:"channel"`
	User        string `json:"user"`
	Text        string `json:"text"`
	Ts          string `json:"ts"`
	ThreadTs    string `json:"thread_ts,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
}

func NewSlackGateway(botToken, signingSecret string) *SlackGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &SlackGateway{
		botToken:      botToken,
		signingSecret: signingSecret,
	}

	g.BasePlatform = NewBasePlatform("slack", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend
	g.SetCallbackPort(8085)

	return g
}

func (g *SlackGateway) onConnect(ctx context.Context) error {
	log.Infof("[Slack] Connecting to Slack gateway...")

	// startRTM validates the bot token via rtm.connect (returns an error on
	// invalid_auth etc.), so reaching this point means the credentials are real.
	if err := g.startRTM(); err != nil {
		return fmt.Errorf("failed to start RTM: %w", err)
	}

	g.markConnected()

	go g.startHTTPServer()

	log.Info("[Slack] Gateway connected")
	return nil
}

func (g *SlackGateway) onDisconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.httpServer != nil {
		g.httpServer.Shutdown(context.Background())
		g.httpServer = nil
	}

	log.Info("[Slack] Gateway disconnected")
	return nil
}

func (g *SlackGateway) onSend(ctx context.Context, resp Response) error {
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

func (g *SlackGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch strings.ToLower(cmd) {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - Slack\n\n" +
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
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

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
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
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

func (g *SlackGateway) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/events", g.handleSlackEvents)
	mux.HandleFunc("/slack/interactive", g.handleInteractive)

	g.mu.Lock()
	g.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.GetCallbackPort()),
		Handler: mux,
	}
	g.mu.Unlock()

	go func() {
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("[Slack] HTTP server error: %v", err)
		}
	}()
}

func (g *SlackGateway) handleSlackEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Query().Get("challenge")))
		return
	}

	var event slackEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if event.Type == "url_verification" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
		return
	}

	if event.Type == "event_callback" {
		var msgEvent slackMessageEvent
		if err := json.Unmarshal(event.Raw, &msgEvent); err != nil {
			log.Errorf("[Slack] Failed to parse message event: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		if msgEvent.User == "" || strings.HasPrefix(msgEvent.Text, "<@U") {
			w.WriteHeader(http.StatusOK)
			return
		}

		if !g.ShouldProcessChannel(msgEvent.Channel) {
			w.WriteHeader(http.StatusOK)
			return
		}

		isGroup := false
		if msgEvent.ChannelType != "" {
			isGroup = msgEvent.ChannelType == "channel" || msgEvent.ChannelType == "group"
		} else if len(msgEvent.Channel) > 0 {
			isGroup = msgEvent.Channel[0] == 'C' || msgEvent.Channel[0] == 'G'
		}

		isMentioned := strings.Contains(msgEvent.Text, "<@")

		msg := Message{
			ID:          msgEvent.Ts,
			Platform:    "slack",
			ChannelID:   msgEvent.Channel,
			UserID:      msgEvent.User,
			Content:     msgEvent.Text,
			Timestamp:   time.Now(),
			Metadata:    map[string]interface{}{"thread_ts": msgEvent.ThreadTs},
			IsGroup:     isGroup,
			IsMentioned: isMentioned,
		}

		g.EmitMessage(msg)
	}

	w.WriteHeader(http.StatusOK)
}

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

func (g *SlackGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()
	status.Platform = "slack"
	status.Platforms = make(map[string]PlatformStatus)

	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}
	status.Details["callback_port"] = g.GetCallbackPort()

	platformStatus := PlatformStatus{
		Name:   "slack",
		Status: "connected",
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
	} else {
		status.Status = "healthy"
	}

	status.Platforms["slack"] = platformStatus
	return status
}
