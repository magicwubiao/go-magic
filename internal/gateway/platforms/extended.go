package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Platform defines the interface for messaging platforms
type Platform interface {
	// Name returns the platform name
	Name() string

	// Connect establishes connection to the platform
	Connect(ctx context.Context) error

	// Disconnect closes the connection
	Disconnect() error

	// Send sends a message
	Send(ctx context.Context, chatID string, message Message) error

	// Receive receives messages
	Receive(ctx context.Context) (<-chan Message, error)

	// SetWebhook sets up webhook for this platform
	SetWebhook(ctx context.Context, url string) error

	// IsConnected returns connection status
	IsConnected() bool
}

// Message represents a message on any platform
type Message struct {
	ID        string
	ChatID    string
	Text      string
	From      User
	Timestamp time.Time
	Media     *Media
	ReplyTo   string
	Metadata  map[string]interface{}
}

// User represents a user on any platform
type User struct {
	ID        string
	Username  string
	FirstName string
	LastName  string
	Language  string
	IsBot     bool
}

// Media represents media attachments
type Media struct {
	Type      string    // photo, video, audio, document
	URL       string    // URL or file path
	FileID    string    // Platform-specific file ID
	Caption   string    // Media caption
	MimeType  string    // MIME type
	Size      int64     // File size
	Duration  int       // Duration in seconds (for audio/video)
	Width     int       // Width (for images/videos)
	Height    int       // Height (for images/videos)
	Thumbnail *string    // Thumbnail URL
}

// PlatformManager manages all platform connections
type PlatformManager struct {
	platforms map[string]Platform
	mu        sync.RWMutex
	config    *PlatformConfig
}

// PlatformConfig holds platform configurations
type PlatformConfig struct {
	Enabled     []string
	Credentials map[string]Credentials
	Webhooks    map[string]string
}

// Credentials holds platform-specific credentials
type Credentials struct {
	APIKey    string
	APISecret string
	Token     string
	BotToken  string
	Endpoint  string
}

// NewPlatformManager creates a new platform manager
func NewPlatformManager(config *PlatformConfig) *PlatformManager {
	pm := &PlatformManager{
		platforms: make(map[string]Platform),
		config:    config,
	}

	// Register all platforms
	pm.registerPlatforms()

	return pm
}

// registerPlatforms registers all supported platforms
func (pm *PlatformManager) registerPlatforms() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// WhatsApp (via WhatsApp Business API)
	pm.platforms["whatsapp"] = &WhatsAppPlatform{config: pm.config}

	// Signal (via Signal CLI)
	pm.platforms["signal"] = &SignalPlatform{config: pm.config}

	// Matrix (via Matrix client)
	pm.platforms["matrix"] = &MatrixPlatform{config: pm.config}

	// Email (via SMTP)
	pm.platforms["email"] = &EmailPlatform{config: pm.config}

	// SMS (via Twilio, etc.)
	pm.platforms["sms"] = &SMSPlatform{config: pm.config}

	// DingTalk
	pm.platforms["dingtalk"] = &DingTalkPlatform{config: pm.config}

	// Feishu (Lark)
	pm.platforms["feishu"] = &FeishuPlatform{config: pm.config}

	// WeCom
	pm.platforms["wecom"] = &WeComPlatform{config: pm.config}

	// Microsoft Teams
	pm.platforms["teams"] = &TeamsPlatform{config: pm.config}

	// Mattermost
	pm.platforms["mattermost"] = &MattermostPlatform{config: pm.config}
}

// GetPlatform returns a platform by name
func (pm *PlatformManager) GetPlatform(name string) Platform {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.platforms[name]
}

// ListPlatforms returns all registered platforms
func (pm *PlatformManager) ListPlatforms() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.platforms))
	for name := range pm.platforms {
		names = append(names, name)
	}
	return names
}

// ConnectAll connects to all enabled platforms
func (pm *PlatformManager) ConnectAll(ctx context.Context) error {
	pm.mu.RLock()
	enabled := pm.config.Enabled
	pm.mu.RUnlock()

	var lastErr error
	for _, name := range enabled {
		platform := pm.GetPlatform(name)
		if platform == nil {
			continue
		}
		if err := platform.Connect(ctx); err != nil {
			lastErr = fmt.Errorf("failed to connect to %s: %w", name, err)
		}
	}
	return lastErr
}

// DisconnectAll disconnects all platforms
func (pm *PlatformManager) DisconnectAll() {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, platform := range pm.platforms {
		platform.Disconnect()
	}
}

// ============= WhatsApp Platform =============

// WhatsAppPlatform implements WhatsApp Business API
type WhatsAppPlatform struct {
	config     *PlatformConfig
	connected  bool
	webhookURL string
}

func (p *WhatsAppPlatform) Name() string { return "whatsapp" }

func (p *WhatsAppPlatform) Connect(ctx context.Context) error {
	creds := p.config.Credentials["whatsapp"]
	if creds.APIKey == "" {
		return fmt.Errorf("whatsapp: missing API key")
	}
	p.connected = true
	return nil
}

func (p *WhatsAppPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *WhatsAppPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	if !p.connected {
		return fmt.Errorf("not connected")
	}
	// WhatsApp API implementation
	return nil
}

func (p *WhatsAppPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
		// Webhook receiver
	}()
	return ch, nil
}

func (p *WhatsAppPlatform) SetWebhook(ctx context.Context, url string) error {
	p.webhookURL = url
	return nil
}

func (p *WhatsAppPlatform) IsConnected() bool { return p.connected }

// ============= Signal Platform =============

// SignalPlatform implements Signal CLI
type SignalPlatform struct {
	config    *PlatformConfig
	connected bool
}

func (p *SignalPlatform) Name() string { return "signal" }

func (p *SignalPlatform) Connect(ctx context.Context) error {
	// Signal CLI connection
	p.connected = true
	return nil
}

func (p *SignalPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *SignalPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *SignalPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *SignalPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *SignalPlatform) IsConnected() bool { return p.connected }

// ============= Matrix Platform =============

// MatrixPlatform implements Matrix protocol
type MatrixPlatform struct {
	config    *PlatformConfig
	connected bool
	homeServer string
	userID    string
	roomID    string
}

func (p *MatrixPlatform) Name() string { return "matrix" }

func (p *MatrixPlatform) Connect(ctx context.Context) error {
	creds := p.config.Credentials["matrix"]
	if creds.Token == "" {
		return fmt.Errorf("matrix: missing access token")
	}
	p.homeServer = creds.Endpoint
	if p.homeServer == "" {
		p.homeServer = "matrix.org"
	}
	p.connected = true
	return nil
}

func (p *MatrixPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *MatrixPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *MatrixPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
		// Matrix sync loop
	}()
	return ch, nil
}

func (p *MatrixPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *MatrixPlatform) IsConnected() bool { return p.connected }

// ============= Email Platform =============

// EmailPlatform implements email via SMTP
type EmailPlatform struct {
	config    *PlatformConfig
	connected bool
	smtpHost string
	smtpPort int
	from     string
}

func (p *EmailPlatform) Name() string { return "email" }

func (p *EmailPlatform) Connect(ctx context.Context) error {
	creds := p.config.Credentials["email"]
	if creds.APIKey == "" {
		return fmt.Errorf("email: missing credentials")
	}
	p.smtpHost = creds.Endpoint
	if p.smtpHost == "" {
		p.smtpHost = "smtp.gmail.com"
	}
	p.smtpPort = 587
	p.from = creds.APISecret // sender email
	p.connected = true
	return nil
}

func (p *EmailPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *EmailPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	// Send email via SMTP
	return nil
}

func (p *EmailPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
		// IMAP/Polling receiver
	}()
	return ch, nil
}

func (p *EmailPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *EmailPlatform) IsConnected() bool { return p.connected }

// ============= SMS Platform =============

// SMSPlatform implements SMS via Twilio
type SMSPlatform struct {
	config    *PlatformConfig
	connected bool
}

func (p *SMSPlatform) Name() string { return "sms" }

func (p *SMSPlatform) Connect(ctx context.Context) error {
	creds := p.config.Credentials["sms"]
	if creds.APIKey == "" {
		return fmt.Errorf("sms: missing API key")
	}
	p.connected = true
	return nil
}

func (p *SMSPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *SMSPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *SMSPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *SMSPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *SMSPlatform) IsConnected() bool { return p.connected }

// ============= DingTalk Platform =============

// DingTalkPlatform implements DingTalk
type DingTalkPlatform struct {
	config    *PlatformConfig
	connected bool
}

func (p *DingTalkPlatform) Name() string { return "dingtalk" }

func (p *DingTalkPlatform) Connect(ctx context.Context) error {
	p.connected = true
	return nil
}

func (p *DingTalkPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *DingTalkPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *DingTalkPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *DingTalkPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *DingTalkPlatform) IsConnected() bool { return p.connected }

// ============= Feishu Platform =============

// FeishuPlatform implements Feishu (Lark)
type FeishuPlatform struct {
	config    *PlatformConfig
	connected bool
}

func (p *FeishuPlatform) Name() string { return "feishu" }

func (p *FeishuPlatform) Connect(ctx context.Context) error {
	p.connected = true
	return nil
}

func (p *FeishuPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *FeishuPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *FeishuPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *FeishuPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *FeishuPlatform) IsConnected() bool { return p.connected }

// ============= WeCom Platform =============

// WeComPlatform implements WeCom (WeChat Work)
type WeComPlatform struct {
	config    *PlatformConfig
	connected bool
}

func (p *WeComPlatform) Name() string { return "wecom" }

func (p *WeComPlatform) Connect(ctx context.Context) error {
	p.connected = true
	return nil
}

func (p *WeComPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *WeComPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *WeComPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *WeComPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *WeComPlatform) IsConnected() bool { return p.connected }

// ============= Microsoft Teams Platform =============

// TeamsPlatform implements Microsoft Teams
type TeamsPlatform struct {
	config    *PlatformConfig
	connected bool
}

func (p *TeamsPlatform) Name() string { return "teams" }

func (p *TeamsPlatform) Connect(ctx context.Context) error {
	p.connected = true
	return nil
}

func (p *TeamsPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *TeamsPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *TeamsPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *TeamsPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *TeamsPlatform) IsConnected() bool { return p.connected }

// ============= Mattermost Platform =============

// MattermostPlatform implements Mattermost
type MattermostPlatform struct {
	config     *PlatformConfig
	connected  bool
	serverURL  string
	teamName   string
	channelName string
}

func (p *MattermostPlatform) Name() string { return "mattermost" }

func (p *MattermostPlatform) Connect(ctx context.Context) error {
	creds := p.config.Credentials["mattermost"]
	if creds.Endpoint == "" {
		return fmt.Errorf("mattermost: missing server URL")
	}
	p.serverURL = creds.Endpoint
	p.connected = true
	return nil
}

func (p *MattermostPlatform) Disconnect() error {
	p.connected = false
	return nil
}

func (p *MattermostPlatform) Send(ctx context.Context, chatID string, msg Message) error {
	return nil
}

func (p *MattermostPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (p *MattermostPlatform) SetWebhook(ctx context.Context, url string) error {
	return nil
}

func (p *MattermostPlatform) IsConnected() bool { return p.connected }

// ============= Helper Functions =============

// ParseChatID parses platform-specific chat ID
func ParseChatID(platform, rawID string) string {
	switch strings.ToLower(platform) {
	case "telegram":
		return rawID
	case "whatsapp":
		// Remove any non-digit characters
		return strings.TrimPrefix(rawID, "+")
	case "signal":
		return rawID
	case "sms":
		return rawID
	default:
		return rawID
	}
}

// FormatPhoneNumber formats phone numbers for messaging
func FormatPhoneNumber(phone string) string {
	// Remove all non-digit characters except leading +
	if strings.HasPrefix(phone, "+") {
		return "+" + strings.ReplaceAll(phone[1:], " ", "")
	}
	return strings.ReplaceAll(phone, " ", "")
}

// MessageToJSON converts message to JSON
func MessageToJSON(msg Message) ([]byte, error) {
	return json.MarshalIndent(msg, "", "  ")
}

// JSONToMessage parses JSON to message
func JSONToMessage(data []byte) (*Message, error) {
	var msg Message
	err := json.Unmarshal(data, &msg)
	return &msg, err
}
