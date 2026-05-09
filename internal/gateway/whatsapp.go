package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// WhatsAppGateway implements WhatsApp via whatsmeow (personal account, QR code login)
type WhatsAppGateway struct {
	client    *whatsmeow.Client
	container *sqlstore.Container
	device    *store.Device

	agents  map[string]*AgentSession
	msgCh   chan Message
	mu      sync.RWMutex
	stopCh  chan struct{}
	running bool

	dataDir    string // Where session data is stored
	qrCallback func(qr string) // Called when QR code is generated

	// Track own JID for filtering
	ownJID types.JID
}

// NewWhatsAppGateway creates a new WhatsApp gateway with QR login support
func NewWhatsAppGateway(dataDir string) *WhatsAppGateway {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".magic", "whatsapp")
	}

	return &WhatsAppGateway{
		agents:  make(map[string]*AgentSession),
		msgCh:   make(chan Message, 100),
		stopCh:  make(chan struct{}),
		dataDir: dataDir,
	}
}

// SetQRCallback sets a callback for QR code events (for CLI display or API push)
func (g *WhatsAppGateway) SetQRCallback(cb func(qr string)) {
	g.qrCallback = cb
}

// Name returns the platform name
func (g *WhatsAppGateway) Name() string {
	return "whatsapp"
}

// Connect establishes connection with QR code login
func (g *WhatsAppGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	// Ensure data directory exists
	if err := os.MkdirAll(g.dataDir, 0700); err != nil {
		return fmt.Errorf("failed to create whatsapp data dir: %w", err)
	}

	// Initialize SQL store for session persistence
	dbPath := filepath.Join(g.dataDir, "store.db")
	container, err := sqlstore.New("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), waLog.Noop)
	if err != nil {
		return fmt.Errorf("failed to create session store: %w", err)
	}
	g.container = container

	// Get or create device
	devices, err := container.GetAllDevices()
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	if len(devices) > 0 {
		// Use existing device (already logged in)
		g.device = devices[0]
		log.Info("Found existing WhatsApp session, connecting...")
	} else {
		// Create new device (will need QR login)
		g.device = container.NewDevice()
		log.Info("No WhatsApp session found, will generate QR code for login")
	}

	// Create client
	client := whatsmeow.NewClient(g.device, waLog.Noop)
	g.client = client

	// Register event handlers
	client.AddEventHandler(g.eventHandler)

	// Connect
	if client.IsConnected() {
		return nil
	}

	err = client.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to WhatsApp: %w", err)
	}

	log.Info("WhatsApp gateway connecting...")
	return nil
}

// Disconnect closes the connection
func (g *WhatsAppGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	if g.client != nil {
		g.client.Disconnect()
	}
	if g.container != nil {
		g.container.Close()
	}
	close(g.stopCh)
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

	status := &HealthStatus{
		Platform:  "whatsapp",
		Connected: running && g.client != nil && g.client.IsConnected(),
		Status:    "healthy",
		Details:   make(map[string]interface{}),
		Platforms: make(map[string]PlatformStatus),
	}

	if g.client != nil && g.client.IsLoggedIn() {
		status.Details["logged_in"] = true
		status.Details["own_jid"] = g.ownJID.String()
	} else {
		status.Details["logged_in"] = false
		status.Details["message"] = "Not logged in, use QR code to login"
		if !status.Connected {
			status.Status = "disconnected"
		} else {
			status.Status = "waiting_login"
		}
	}

	platformStatus := PlatformStatus{
		Name:   "whatsapp",
		Status: status.Status,
	}
	status.Platforms["whatsapp"] = platformStatus
	return status
}

// Receive returns the message channel
func (g *WhatsAppGateway) Receive() <-chan Message {
	return g.msgCh
}

// Send sends a message via WhatsApp
func (g *WhatsAppGateway) Send(ctx context.Context, resp Response) error {
	if g.client == nil || !g.client.IsLoggedIn() {
		return fmt.Errorf("WhatsApp not logged in")
	}

	to := resp.ChannelID
	if to == "" {
		return fmt.Errorf("recipient (channel_id) is required")
	}

	// Parse JID
	jid, err := g.parseJID(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}

	// Send text message
	_, err = g.client.SendMessage(ctx, jid, &whatsmeow.Message{
		Conversation: &resp.Content,
	})
	if err != nil {
		return fmt.Errorf("failed to send WhatsApp message: %w", err)
	}

	return nil
}

// HandleSlashCommand handles commands
func (g *WhatsAppGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "logout":
		if g.client != nil {
			g.client.Logout()
		}
		return Response{
			ChannelID: msg.UserID,
			Content:   "Logged out of WhatsApp. Restart to login again.",
		}, nil
	case "status":
		loggedIn := g.client != nil && g.client.IsLoggedIn()
		connected := g.client != nil && g.client.IsConnected()
		return Response{
			ChannelID: msg.UserID,
			Content:   fmt.Sprintf("WhatsApp: connected=%v, logged_in=%v", connected, loggedIn),
		}, nil
	default:
		return Response{
			ChannelID: msg.UserID,
			Content:   "Available commands: /logout, /status",
		}, nil
	}
}

// IsLoggedIn returns whether the client is logged in
func (g *WhatsAppGateway) IsLoggedIn() bool {
	return g.client != nil && g.client.IsLoggedIn()
}

// GetQRCode returns the current QR code for login (if waiting)
// This is called after Connect() if the session is new
func (g *WhatsAppGateway) GetQRCode() string {
	// QR code is delivered via event handler, this method is for polling
	return ""
}

// parseJID parses a phone number or JID string into a types.JID
func (g *WhatsAppGateway) parseJID(input string) (types.JID, error) {
	// If already a full JID
	if strings.Contains(input, "@") {
		return types.ParseJID(input)
	}

	// Treat as phone number - clean it
	number := input
	number = strings.TrimPrefix(number, "+")
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")

	// Determine if it's a group or personal
	if strings.HasPrefix(number, "g-") || strings.HasPrefix(number, "group-") {
		// Group JID
		groupID := strings.TrimPrefix(number, "g-")
		groupID = strings.TrimPrefix(groupID, "group-")
		return types.JID{
			Server: types.GroupServer,
			User:   groupID,
		}, nil
	}

	// Personal JID
	return types.JID{
		Server: types.DefaultUserServer,
		User:   number,
	}, nil
}

// eventHandler processes WhatsApp events
func (g *WhatsAppGateway) eventHandler(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.QR:
		// QR code generated, display it
		qrData := evt.Code
		log.Infof("WhatsApp QR Code generated. Scan with WhatsApp app to login.")

		if g.qrCallback != nil {
			g.qrCallback(qrData)
		}

		// Also print QR to console for CLI usage
		fmt.Printf("\n--- WhatsApp QR Code ---\n%s\n--- Scan with WhatsApp > Linked Devices ---\n\n", qrData)

	case *events.QRScannedWithoutMultidevice:
		log.Warn("QR scanned but multi-device not enabled. Please enable multi-device on your WhatsApp.")

	case *events.Connected:
		log.Info("WhatsApp connected to servers")

	case *events.LoggedOut:
		log.Warn("WhatsApp logged out")
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()

	case *events.Disconnected:
		log.Warn("WhatsApp disconnected, will attempt to reconnect...")

	case *events.PairSuccess:
		log.Infof("WhatsApp paired successfully: %s", evt.ID.String())
		g.mu.Lock()
		g.ownJID = evt.ID
		g.mu.Unlock()

	case *events.Contact:
		// Contact sync

	case *events.PushName:
		// Push name update

	case *events.Message:
		g.handleIncomingMessage(evt)

	case *events.Receipt:
		// Message receipt (read/delivered)

	case *events.AppStateSyncComplete:
		if len(g.client.Store.PushName) > 0 && g.client.Store.PushName != "go-magic" {
			_ = g.client.SendPresence(types.PresenceAvailable)
		}

	case *events.ConnInfo:
		// Connection info update

	case *events.OfflineSyncCompleted:
		log.Info("WhatsApp offline sync completed")
	}
}

// handleIncomingMessage processes an incoming WhatsApp message
func (g *WhatsAppGateway) handleIncomingMessage(evt *events.Message) {
	msg := evt.Message
	info := evt.Info

	// Skip messages from self
	if info.Sender.User == g.ownJID.User {
		return
	}

	// Extract text content and media attachments
	var content string
	var msgType string
	var mediaURLs []MediaAttachment

	switch {
	case msg.Conversation != nil && *msg.Conversation != "":
		content = *msg.Conversation
		msgType = "text"

	case msg.ExtendedTextMessage != nil:
		content = msg.ExtendedTextMessage.GetText()
		msgType = "text"
		if msg.ExtendedTextMessage.ContextInfo != nil && msg.ExtendedTextMessage.ContextInfo.QuotedMessage != nil {
			msgType = "reply"
		}

	case msg.ImageMessage != nil:
		caption := msg.ImageMessage.GetCaption()
		if caption != "" {
			content = caption
		} else {
			content = ""
		}
		msgType = "image"
		// Download image
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(msg.ImageMessage)
			if err == nil && len(data) > 0 {
				ext := "jpg"
				if msg.ImageMessage.GetMimetype() == "image/webp" {
					ext = "webp"
				} else if msg.ImageMessage.GetMimetype() == "image/png" {
					ext = "png"
				}
				path := saveMedia(data, info.ID, "whatsapp", "image", ext)
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "image",
					URL:      path,
					MimeType: msg.ImageMessage.GetMimetype(),
					Caption:  caption,
					Size:     int64(len(data)),
				})
			} else if err != nil {
				log.Debugf("Failed to download WhatsApp image: %v", err)
			}
		}

	case msg.VideoMessage != nil:
		caption := msg.VideoMessage.GetCaption()
		if caption != "" {
			content = caption
		} else {
			content = ""
		}
		msgType = "video"
		// Download video
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(msg.VideoMessage)
			if err == nil && len(data) > 0 {
				path := saveMedia(data, info.ID, "whatsapp", "video", "mp4")
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "video",
					URL:      path,
					MimeType: msg.VideoMessage.GetMimetype(),
					Caption:  caption,
					Size:     int64(len(data)),
				})
			} else if err != nil {
				log.Debugf("Failed to download WhatsApp video: %v", err)
			}
		}

	case msg.AudioMessage != nil:
		content = ""
		msgType = "audio"
		// Download audio
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(msg.AudioMessage)
			if err == nil && len(data) > 0 {
				ext := "ogg"
				if msg.AudioMessage.GetMimetype() == "audio/ogg; codecs=opus" {
					ext = "opus"
				} else if msg.AudioMessage.GetMimetype() == "audio/mpeg" {
					ext = "mp3"
				}
				path := saveMedia(data, info.ID, "whatsapp", "audio", ext)
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "audio",
					URL:      path,
					MimeType: msg.AudioMessage.GetMimetype(),
					Size:     int64(len(data)),
				})
			} else if err != nil {
				log.Debugf("Failed to download WhatsApp audio: %v", err)
			}
		}

	case msg.DocumentMessage != nil:
		content = ""
		msgType = "document"
		filename := msg.DocumentMessage.GetFileName()
		// Download document
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(msg.DocumentMessage)
			if err == nil && len(data) > 0 {
				// Extract extension from filename or mime type
				ext := "bin"
				if idx := strings.LastIndex(filename, "."); idx > 0 {
					ext = filename[idx+1:]
				}
				path := saveMedia(data, info.ID, "whatsapp", "file", ext)
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "file",
					URL:      path,
					MimeType: msg.DocumentMessage.GetMimetype(),
					Filename: filename,
					Size:     int64(len(data)),
				})
			} else if err != nil {
				log.Debugf("Failed to download WhatsApp document: %v", err)
			}
		}

	case msg.LocationMessage != nil:
		content = fmt.Sprintf("[Location: %.6f, %.6f]",
			msg.LocationMessage.GetDegreesLatitude(),
			msg.LocationMessage.GetDegreesLongitude())
		msgType = "location"

	case msg.ContactMessage != nil:
		content = "[Contact: " + msg.ContactMessage.GetDisplayName() + "]"
		msgType = "contact"

	case msg.StickerMessage != nil:
		content = ""
		msgType = "sticker"
		// Download sticker (as image)
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(msg.StickerMessage)
			if err == nil && len(data) > 0 {
				path := saveMedia(data, info.ID, "whatsapp", "image", "webp")
				mediaURLs = append(mediaURLs, MediaAttachment{
					Type:     "image",
					URL:      path,
					MimeType: msg.StickerMessage.GetMimetype(),
					Caption:  "Sticker",
					Size:     int64(len(data)),
				})
			} else if err != nil {
				log.Debugf("Failed to download WhatsApp sticker: %v", err)
			}
		}

	case msg.ReactionMessage != nil:
		content = "[Reaction: " + msg.ReactionMessage.GetText() + "]"
		msgType = "reaction"

	default:
		content = "[Unsupported message]"
		msgType = "unknown"
	}

	// Determine source (group or DM)
	sender := info.Sender.String()
	chatJID := info.Chat
	isGroup := chatJID.Server == types.GroupServer

	metadata := map[string]interface{}{
		"type":      msgType,
		"sender":    sender,
		"is_group":  isGroup,
		"push_name": info.PushName,
	}

	if isGroup {
		metadata["group_jid"] = chatJID.String()
	}

	// Build gateway message
	waMsg := Message{
		ID:        info.ID,
		Platform:  "whatsapp",
		ChannelID: chatJID.String(),
		UserID:    sender,
		Content:   content,
		Timestamp: info.Timestamp,
		Metadata:  metadata,
		MediaURLs: mediaURLs,
	}

	select {
	case g.msgCh <- waMsg:
	default:
		log.Warnf("WhatsApp message channel full, dropping message")
	}
}

// RequestAppState requests full app state sync (contacts, etc.)
func (g *WhatsAppGateway) RequestAppState() error {
	if g.client == nil || !g.client.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}
	err := g.client.FetchAppState(appstate.WAPatchCriticalBlock, false, false)
	if err != nil {
		return fmt.Errorf("failed to fetch app state: %w", err)
	}
	err = g.client.FetchAppState(appstate.WAPatchRegularHigh, false, false)
	if err != nil {
		return fmt.Errorf("failed to fetch regular app state: %w", err)
	}
	return nil
}

// SendPresence updates the user's presence (available/unavailable/composing)
func (g *WhatsAppGateway) SendPresence(presence types.Presence) error {
	if g.client == nil || !g.client.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}
	return g.client.SendPresence(presence)
}

// SendTyping sends a typing indicator to a chat
func (g *WhatsAppGateway) SendTyping(chatJID types.JID) error {
	if g.client == nil || !g.client.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}
	return g.client.SendChatPresence(chatJID, types.ChatPresenceComposing, types.ChatPresenceMediaText)
}

// GetContactName resolves a JID to a contact name
func (g *WhatsAppGateway) GetContactName(jid types.JID) string {
	if g.client == nil {
		return jid.User
	}
	contact, err := g.client.Store.Contacts.GetContact(jid)
	if err != nil || contact.FullName == "" {
		return jid.User
	}
	return contact.FullName
}

// ExportSession exports the current session as JSON (for backup)
func (g *WhatsAppGateway) ExportSession() ([]byte, error) {
	if g.device == nil {
		return nil, fmt.Errorf("no device session")
	}
	return json.Marshal(g.device)
}

// ---- Keep backward compatibility with old Business API constructor ----

// NewWhatsAppBusinessGateway creates a WhatsApp Business API gateway (legacy)
// Deprecated: Use NewWhatsAppGateway for personal accounts with QR login
func NewWhatsAppBusinessGateway(phoneNumberID, accessToken, appSecret, verifyToken string) *WhatsAppBusinessGateway {
	return &WhatsAppBusinessGateway{
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
