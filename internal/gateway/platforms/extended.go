package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// =============================================================================
// WhatsApp Platform
// =============================================================================

// WhatsAppPlatform implements WhatsApp messaging
type WhatsAppPlatform struct {
	config      *PlatformConfig
	phoneID     string
	apiKey      string
	apiBase     string
	verifyToken string
	webhookPath string
	connected   bool
	httpClient  *http.Client
}

// WhatsAppMessage represents a WhatsApp message payload
type WhatsAppMessage struct {
	MessagingProduct string         `json:"messaging_product"`
	To               string         `json:"to"`
	Type             string         `json:"type"`
	Text             *WhatsAppText  `json:"text,omitempty"`
	Image            *WhatsAppMedia `json:"image,omitempty"`
	Audio            *WhatsAppMedia `json:"audio,omitempty"`
	Video            *WhatsAppMedia `json:"video,omitempty"`
	Document         *WhatsAppMedia `json:"document,omitempty"`
}

// WhatsAppText represents WhatsApp text message
type WhatsAppText struct {
	PreviewURL bool   `json:"preview_url,omitempty"`
	Body       string `json:"body"`
}

// WhatsAppMedia represents WhatsApp media message
type WhatsAppMedia struct {
	Link     string `json:"link,omitempty"`
	Caption  string `json:"caption,omitempty"`
	ID       string `json:"id,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// NewWhatsAppPlatform creates a new WhatsApp platform
func NewWhatsAppPlatform(phoneID, apiKey string) *WhatsAppPlatform {
	return &WhatsAppPlatform{
		phoneID:    phoneID,
		apiKey:     apiKey,
		apiBase:    "https://graph.facebook.com/v18.0/" + phoneID,
		connected:  false,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the platform name
func (w *WhatsAppPlatform) Name() string {
	return "whatsapp"
}

// Connect establishes connection to WhatsApp
func (w *WhatsAppPlatform) Connect(ctx context.Context) error {
	w.connected = true
	return nil
}

// Disconnect closes the connection
func (w *WhatsAppPlatform) Disconnect() error {
	w.connected = false
	return nil
}

// IsConnected returns connection status
func (w *WhatsAppPlatform) IsConnected() bool {
	return w.connected
}

// Send sends a message
func (w *WhatsAppPlatform) Send(ctx context.Context, to string, msg Message) error {
	if !w.connected {
		return fmt.Errorf("not connected")
	}

	var payload WhatsAppMessage
	payload.MessagingProduct = "whatsapp"
	payload.To = to

	if msg.Media != nil {
		switch msg.Media.Type {
		case "image":
			payload.Type = "image"
			payload.Image = &WhatsAppMedia{
				Link:    msg.Media.URL,
				Caption: msg.Media.Caption,
			}
		case "audio":
			payload.Type = "audio"
			payload.Audio = &WhatsAppMedia{
				Link: msg.Media.URL,
			}
		case "video":
			payload.Type = "video"
			payload.Video = &WhatsAppMedia{
				Link:    msg.Media.URL,
				Caption: msg.Media.Caption,
			}
		case "document":
			payload.Type = "document"
			payload.Document = &WhatsAppMedia{
				Link:    msg.Media.URL,
				Caption: msg.Media.Caption,
			}
		default:
			payload.Type = "text"
			payload.Text = &WhatsAppText{
				Body: msg.Text,
			}
		}
	} else {
		payload.Type = "text"
		payload.Text = &WhatsAppText{
			Body: msg.Text,
		}
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", w.apiBase+"/messages", strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Receive is not supported for WhatsApp (uses webhooks)
func (w *WhatsAppPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message, 100)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// =============================================================================
// Signal Platform
// =============================================================================

// SignalPlatform implements Signal messaging
type SignalPlatform struct {
	config     *PlatformConfig
	serviceURL string
	username   string
	password   string
	connected  bool
	httpClient *http.Client
}

// NewSignalPlatform creates a new Signal platform
func NewSignalPlatform(serviceURL, username, password string) *SignalPlatform {
	return &SignalPlatform{
		serviceURL: serviceURL,
		username:   username,
		password:   password,
		connected:  false,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the platform name
func (s *SignalPlatform) Name() string {
	return "signal"
}

// Connect establishes connection to Signal
func (s *SignalPlatform) Connect(ctx context.Context) error {
	s.connected = true
	return nil
}

// Disconnect closes the connection
func (s *SignalPlatform) Disconnect() error {
	s.connected = false
	return nil
}

// IsConnected returns connection status
func (s *SignalPlatform) IsConnected() bool {
	return s.connected
}

// Send sends a message
func (s *SignalPlatform) Send(ctx context.Context, recipient string, msg Message) error {
	if !s.connected {
		return fmt.Errorf("not connected")
	}

	payload := map[string]interface{}{
		"recipient": recipient,
		"message":   msg.Text,
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", s.serviceURL+"/send", strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Receive is not supported for Signal (uses external service)
func (s *SignalPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message, 100)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// =============================================================================
// Matrix Platform
// =============================================================================

// MatrixPlatform implements Matrix messaging
type MatrixPlatform struct {
	config      *PlatformConfig
	homeserver  string
	userID      string
	accessToken string
	roomID      string
	connected   bool
	httpClient  *http.Client
}

// NewMatrixPlatform creates a new Matrix platform
func NewMatrixPlatform(homeserver, userID, accessToken string) *MatrixPlatform {
	return &MatrixPlatform{
		homeserver:  homeserver,
		userID:      userID,
		accessToken: accessToken,
		connected:   false,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the platform name
func (m *MatrixPlatform) Name() string {
	return "matrix"
}

// Connect establishes connection to Matrix
func (m *MatrixPlatform) Connect(ctx context.Context) error {
	m.connected = true
	return nil
}

// Disconnect closes the connection
func (m *MatrixPlatform) Disconnect() error {
	m.connected = false
	return nil
}

// IsConnected returns connection status
func (m *MatrixPlatform) IsConnected() bool {
	return m.connected
}

// Send sends a message
func (m *MatrixPlatform) Send(ctx context.Context, roomID string, msg Message) error {
	if !m.connected {
		return fmt.Errorf("not connected")
	}

	content := map[string]interface{}{
		"msgtype": "m.text",
		"body":    msg.Text,
	}

	if msg.Media != nil && msg.Media.URL != "" {
		content["msgtype"] = "m.image"
		content["body"] = msg.Media.URL
		content["url"] = msg.Media.URL
	}

	payload := map[string]interface{}{
		"msgtype": content["msgtype"],
		"body":    content["body"],
	}

	jsonData, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("%s/_matrix/client/r0/rooms/%s/send/m.room.message?access_token=%s",
		m.homeserver, url.PathEscape(roomID), m.accessToken)

	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Receive is not supported for Matrix (uses webhooks)
func (m *MatrixPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message, 100)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// JoinRoom joins a Matrix room
func (m *MatrixPlatform) JoinRoom(ctx context.Context, roomID string) error {
	apiURL := fmt.Sprintf("%s/_matrix/client/r0/join/%s?access_token=%s",
		m.homeserver, url.PathEscape(roomID), m.accessToken)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, nil)
	if err != nil {
		return err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// =============================================================================
// Email Platform
// =============================================================================

// EmailPlatform implements Email messaging
type EmailPlatform struct {
	config    *PlatformConfig
	smtpHost  string
	smtpPort  int
	username  string
	password  string
	fromAddr  string
	connected bool
}

// NewEmailPlatform creates a new Email platform
func NewEmailPlatform(smtpHost string, smtpPort int, username, password, fromAddr string) *EmailPlatform {
	return &EmailPlatform{
		smtpHost:  smtpHost,
		smtpPort:  smtpPort,
		username:  username,
		password:  password,
		fromAddr:  fromAddr,
		connected: false,
	}
}

// Name returns the platform name
func (e *EmailPlatform) Name() string {
	return "email"
}

// Connect establishes connection to SMTP server
func (e *EmailPlatform) Connect(ctx context.Context) error {
	e.connected = true
	return nil
}

// Disconnect closes the connection
func (e *EmailPlatform) Disconnect() error {
	e.connected = false
	return nil
}

// IsConnected returns connection status
func (e *EmailPlatform) IsConnected() bool {
	return e.connected
}

// Send sends an email
func (e *EmailPlatform) Send(ctx context.Context, to string, msg Message) error {
	if !e.connected {
		return fmt.Errorf("not connected")
	}

	// Email sending would require net/smtp package
	// This is a placeholder implementation
	return nil
}

// Receive is not supported for Email (uses webhooks)
func (e *EmailPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message, 100)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// =============================================================================
// SMS Platform
// =============================================================================

// SMSPlatform implements SMS messaging
type SMSPlatform struct {
	config     *PlatformConfig
	apiKey     string
	apiURL     string
	from       string
	connected  bool
	httpClient *http.Client
}

// NewSMSPlatform creates a new SMS platform
func NewSMSPlatform(apiKey, apiURL, from string) *SMSPlatform {
	return &SMSPlatform{
		apiKey:     apiKey,
		apiURL:     apiURL,
		from:       from,
		connected:  false,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the platform name
func (s *SMSPlatform) Name() string {
	return "sms"
}

// Connect establishes connection to SMS gateway
func (s *SMSPlatform) Connect(ctx context.Context) error {
	s.connected = true
	return nil
}

// Disconnect closes the connection
func (s *SMSPlatform) Disconnect() error {
	s.connected = false
	return nil
}

// IsConnected returns connection status
func (s *SMSPlatform) IsConnected() bool {
	return s.connected
}

// Send sends an SMS
func (s *SMSPlatform) Send(ctx context.Context, to string, msg Message) error {
	if !s.connected {
		return fmt.Errorf("not connected")
	}

	// SMS API integration would go here
	// Different providers have different APIs
	return nil
}

// Receive is not supported for SMS
func (s *SMSPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	ch := make(chan Message, 100)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}
