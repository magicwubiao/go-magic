package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Message represents a platform-agnostic message
type Message struct {
	ID        string                 `json:"id"`
	ChatID    string                 `json:"chat_id"`
	Text      string                 `json:"text"`
	Timestamp time.Time              `json:"timestamp"`
	From      User                   `json:"from"`
	Media     *Media                 `json:"media,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Media represents attached media
type Media struct {
	Type     string `json:"type"`      // voice, photo, video, document, image
	URL      string `json:"url"`
	FileID   string `json:"file_id,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Duration int    `json:"duration,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

// User represents a platform-agnostic user
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username,omitempty"`
	FirstName  string `json:"first_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
	Language   string `json:"language,omitempty"`
	IsBot      bool   `json:"is_bot,omitempty"`
}

// Platform defines the interface for messaging platforms
type Platform interface {
	Name() string
	Connect(ctx context.Context) error
	Disconnect() error
	IsConnected() bool
	Send(ctx context.Context, chatID string, message Message) error
	Receive(ctx context.Context) (<-chan Message, error)
}

// Gateway manages multiple platforms
type Gateway struct {
	platforms map[string]Platform
	config    *GatewayConfig
}

// GatewayConfig holds gateway configuration
type GatewayConfig struct {
	Port         int               `json:"port"`
	Platforms    []PlatformConfig `json:"platforms"`
	AllowedUsers []string         `json:"allowed_users"`
	AutoReply    bool             `json:"auto_reply"`
	VoiceReply   bool             `json:"voice_reply"`
}

// PlatformConfig holds individual platform configuration
type PlatformConfig struct {
	Type   string                 `json:"type"` // telegram, discord, slack, etc.
	Token  string                 `json:"token,omitempty"`
	Config map[string]interface{} `json:"config,omitempty"`
}

// NewGateway creates a new gateway
func NewGateway(config *GatewayConfig) *Gateway {
	return &Gateway{
		platforms: make(map[string]Platform),
		config:    config,
	}
}

// Register adds a platform to the gateway
func (g *Gateway) Register(name string, platform Platform) {
	g.platforms[name] = platform
}

// Start initializes all platforms
func (g *Gateway) Start(ctx context.Context) error {
	for name, platform := range g.platforms {
		if err := platform.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect %s: %w", name, err)
		}
	}
	return nil
}

// Stop closes all platform connections
func (g *Gateway) Stop() error {
	for name, platform := range g.platforms {
		if err := platform.Disconnect(); err != nil {
			return fmt.Errorf("failed to disconnect %s: %w", name, err)
		}
	}
	return nil
}

// GetPlatform returns a platform by name
func (g *Gateway) GetPlatform(name string) (Platform, bool) {
	p, ok := g.platforms[name]
	return p, ok
}

// ListPlatforms returns all registered platforms
func (g *Gateway) ListPlatforms() []string {
	names := make([]string, 0, len(g.platforms))
	for name := range g.platforms {
		names = append(names, name)
	}
	return names
}

// ToJSON converts a message to JSON
func (m *Message) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// FromJSON parses a message from JSON
func (m *Message) FromJSON(data []byte) error {
	return json.Unmarshal(data, m)
}
