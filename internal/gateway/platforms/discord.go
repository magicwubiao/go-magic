package platforms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// DiscordPlatform implements Discord messaging
type DiscordPlatform struct {
	config    *PlatformConfig
	token     string
	botID     string
	connected bool
	updates   chan Message
	httpClient *http.Client
}

// DiscordMessage represents a Discord message payload
type DiscordMessage struct {
	Content   string            `json:"content,omitempty"`
	Embeds    []DiscordEmbed   `json:"embeds,omitempty"`
	File      *multipart.File   `json:"-"`
	Filename  string           `json:"-"`
}

// DiscordEmbed represents a Discord embed
type DiscordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       *struct {
		URL string `json:"url,omitempty"`
	} `json:"image,omitempty"`
	Thumbnail *struct {
		URL string `json:"url,omitempty"`
	} `json:"thumbnail,omitempty"`
}

// DiscordWebhookPayload represents a Discord webhook payload
type DiscordWebhookPayload struct {
	Content   string          `json:"content,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

// NewDiscordPlatform creates a new Discord platform
func NewDiscordPlatform(token string) *DiscordPlatform {
	return &DiscordPlatform{
		token:     token,
		connected: false,
		updates:   make(chan Message, 100),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the platform name
func (d *DiscordPlatform) Name() string {
	return "discord"
}

// Connect establishes connection to Discord
func (d *DiscordPlatform) Connect(ctx context.Context) error {
	// Verify token
	req, err := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/v10/users/@me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+d.token)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Discord: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Discord API error: status %d", resp.StatusCode)
	}

	var botInfo struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&botInfo)
	d.botID = botInfo.ID

	d.connected = true
	return nil
}

// Disconnect closes the connection
func (d *DiscordPlatform) Disconnect() error {
	d.connected = false
	close(d.updates)
	return nil
}

// IsConnected returns connection status
func (d *DiscordPlatform) IsConnected() bool {
	return d.connected
}

// Send sends a message
func (d *DiscordPlatform) Send(ctx context.Context, channelID string, message Message) error {
	if !d.connected {
		return fmt.Errorf("not connected")
	}

	// Handle media messages
	if message.Media != nil {
		return d.sendMedia(ctx, channelID, message)
	}

	payload := DiscordMessage{
		Content: message.Text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", 
		"https://discord.com/api/v10/channels/"+channelID+"/messages", 
		bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+d.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// sendMedia sends a media message
func (d *DiscordPlatform) sendMedia(ctx context.Context, channelID string, message Message) error {
	var endpoint string
	var body []byte
	var contentType string

	if message.Media.Type == "image" || message.Media.Type == "photo" {
		// Send as embed with image
		payload := DiscordMessage{
			Content: message.Text,
			Embeds: []DiscordEmbed{
				{
					Image: &struct{ URL string `json:"url,omitempty"` }{URL: message.Media.URL},
				},
			},
		}
		body, _ = json.Marshal(payload)
		contentType = "application/json"
		endpoint = "https://discord.com/api/v10/channels/" + channelID + "/messages"
	} else {
		// Send as regular message with URL
		payload := DiscordMessage{
			Content: message.Text + "\n" + message.Media.URL,
		}
		body, _ = json.Marshal(payload)
		contentType = "application/json"
		endpoint = "https://discord.com/api/v10/channels/" + channelID + "/messages"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+d.token)
	req.Header.Set("Content-Type", contentType)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Receive receives messages (polling - for webhook mode)
func (d *DiscordPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	// In a real implementation, this would use Discord's gateway API
	// For now, return a channel that can receive webhooks
	return d.updates, nil
}

// SetWebhook sets up webhook for this platform
func (d *DiscordPlatform) SetWebhook(ctx context.Context, webhookURL string) error {
	// Store webhook URL for later use
	return nil
}

// SendWebhook sends a message via webhook
func (d *DiscordPlatform) SendWebhook(ctx context.Context, webhookURL string, message Message) error {
	payload := DiscordWebhookPayload{
		Content: message.Text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// JoinVoiceChannel joins a voice channel
func (d *DiscordPlatform) JoinVoiceChannel(ctx context.Context, guildID, channelID string) error {
	// Voice channel implementation would go here
	// This requires the Discord gateway protocol and voice websocket
	return nil
}

// LeaveVoiceChannel leaves the current voice channel
func (d *DiscordPlatform) LeaveVoiceChannel(ctx context.Context) error {
	// Voice channel implementation would go here
	return nil
}

// GetBotInfo returns bot information
func (d *DiscordPlatform) GetBotInfo() (string, string) {
	return d.botID, d.token
}

// CreateDM creates a direct message channel with a user
func (d *DiscordPlatform) CreateDM(ctx context.Context, userID string) (string, error) {
	payload := map[string]string{
		"recipient_id": userID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://discord.com/api/v10/users/@me/channels",
		bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bot "+d.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var channel struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&channel)

	return channel.ID, nil
}

// GetChannelMessages gets messages from a channel
func (d *DiscordPlatform) GetChannelMessages(ctx context.Context, channelID string, limit int) ([]Message, error) {
	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages?limit=%d", channelID, limit)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+d.token)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var messages []struct {
		ID        string `json:"id"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"author"`
		Attachments []struct {
			URL      string `json:"url"`
			Filename string `json:"filename"`
			ContentType string `json:"content_type"`
		} `json:"attachments"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, err
	}

	result := make([]Message, 0, len(messages))
	for _, m := range messages {
		msg := Message{
			ID:      m.ID,
			ChatID:  channelID,
			Text:    m.Content,
			From: User{
				ID:       m.Author.ID,
				Username: m.Author.Username,
			},
		}

		if len(m.Attachments) > 0 {
			msg.Media = &Media{
				Type: "file",
				URL:  m.Attachments[0].URL,
			}
		}

		result = append(result, msg)
	}

	return result, nil
}

// FormatURLForDiscord creates a Discord-friendly URL
func FormatURLForDiscord(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// Ensure URL has https scheme for Discord embeds
	if parsed.Scheme == "http" {
		parsed.Scheme = "https"
	}
	return parsed.String()
}
