package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"

	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"

	_ "modernc.org/sqlite"
)

type WhatsAppGateway struct {
	*BasePlatform

	client    *whatsmeow.Client
	container *sqlstore.Container
	device    *store.Device

	dataDir    string
	qrCallback func(qr string)
	latestQR   string

	ownJID types.JID

	mu sync.RWMutex
}

func NewWhatsAppGateway(dataDir string) *WhatsAppGateway {
	acConfig := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	if dataDir == "" {
		dataDir = filepath.Join(config.GetMagicHome(), "whatsapp")
	}

	g := &WhatsAppGateway{
		dataDir: dataDir,
	}

	g.BasePlatform = NewBasePlatform("whatsapp", acConfig)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *WhatsAppGateway) SetQRCallback(cb func(qr string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.qrCallback = cb
}

func (g *WhatsAppGateway) onConnect(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.client != nil && g.client.IsConnected() {
		return nil
	}

	if g.client != nil {
		g.client.Disconnect()
		g.client = nil
		g.container = nil
		g.device = nil
	}

	if err := os.MkdirAll(g.dataDir, 0700); err != nil {
		return fmt.Errorf("failed to create whatsapp data dir: %w", err)
	}

	dbPath := filepath.Join(g.dataDir, "store.db")
	container, err := sqlstore.New(ctx, "sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", dbPath), waLog.Noop)
	if err != nil {
		return fmt.Errorf("failed to create session store: %w", err)
	}
	g.container = container

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	if len(devices) > 0 {
		g.device = devices[0]
		log.Info("Found existing WhatsApp session, connecting...")
	} else {
		g.device = container.NewDevice()
		log.Info("No WhatsApp session found, will generate QR code for login")
	}

	client := whatsmeow.NewClient(g.device, waLog.Noop)
	g.client = client

	client.AddEventHandler(g.eventHandler)

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

func (g *WhatsAppGateway) onDisconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.client != nil {
		g.client.Disconnect()
		g.client = nil
	}
	if g.container != nil {
		g.container.Close()
		g.container = nil
	}
	g.device = nil

	log.Info("WhatsApp gateway disconnected")
	return nil
}

func (g *WhatsAppGateway) onSend(ctx context.Context, resp Response) error {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	if client == nil || !client.IsLoggedIn() {
		return fmt.Errorf("WhatsApp not logged in")
	}

	to := resp.ChannelID
	if to == "" {
		return fmt.Errorf("recipient (channel_id) is required")
	}

	jid, err := g.parseJID(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}

	_, err = client.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: &resp.Content,
	})
	if err != nil {
		return fmt.Errorf("failed to send WhatsApp message: %w", err)
	}

	return nil
}

func (g *WhatsAppGateway) IsLoggedIn() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.client != nil && g.client.IsLoggedIn()
}

func (g *WhatsAppGateway) GetLoginStatus() string {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	if client == nil {
		return "not_initialized"
	}
	if client.IsLoggedIn() {
		return "confirmed"
	}
	if client.IsConnected() {
		return "waiting_scan"
	}
	return "disconnected"
}

func (g *WhatsAppGateway) StartQRLogin(ctx context.Context) (string, error) {
	g.mu.Lock()

	if g.client != nil && g.client.IsLoggedIn() {
		g.mu.Unlock()
		return "", nil
	}

	if g.client != nil && g.client.IsConnected() && g.latestQR != "" {
		cachedQR := g.latestQR
		g.mu.Unlock()
		return cachedQR, nil
	}

	qrChan := make(chan string, 1)
	originalCallback := g.qrCallback

	g.qrCallback = func(qr string) {
		select {
		case qrChan <- qr:
		default:
		}
		if originalCallback != nil {
			originalCallback(qr)
		}
	}
	g.mu.Unlock()

	if err := g.Connect(ctx); err != nil {
		g.mu.Lock()
		g.qrCallback = originalCallback
		g.mu.Unlock()
		return "", fmt.Errorf("failed to connect: %w", err)
	}

	select {
	case qr := <-qrChan:
		g.mu.Lock()
		g.qrCallback = originalCallback
		g.mu.Unlock()
		return qr, nil
	case <-time.After(30 * time.Second):
		g.mu.Lock()
		g.qrCallback = originalCallback
		cachedQR := g.latestQR
		g.mu.Unlock()
		return cachedQR, nil
	case <-ctx.Done():
		g.mu.Lock()
		g.qrCallback = originalCallback
		g.mu.Unlock()
		return "", ctx.Err()
	}
}

func (g *WhatsAppGateway) parseJID(input string) (types.JID, error) {
	if strings.Contains(input, "@") {
		return types.ParseJID(input)
	}

	number := input
	number = strings.TrimPrefix(number, "+")
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")

	if strings.HasPrefix(number, "g-") || strings.HasPrefix(number, "group-") {
		groupID := strings.TrimPrefix(number, "g-")
		groupID = strings.TrimPrefix(groupID, "group-")
		return types.JID{
			Server: types.GroupServer,
			User:   groupID,
		}, nil
	}

	return types.JID{
		Server: types.DefaultUserServer,
		User:   number,
	}, nil
}

func (g *WhatsAppGateway) eventHandler(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.QR:
		qrData := evt.Codes[len(evt.Codes)-1]
		log.Infof("WhatsApp QR code received, length: %d", len(qrData))

		ForceDisplayQR(qrData)

		g.mu.Lock()
		if g.qrCallback != nil {
			g.qrCallback(qrData)
		}
		g.latestQR = qrData
		g.mu.Unlock()

	case *events.QRScannedWithoutMultidevice:
		log.Warn("QR scanned but multi-device not enabled on the phone.")
		GetQRManager().OnWhatsAppPairError("multi-device not enabled on WhatsApp phone")

	case *events.PairError:
		log.Errorf("Pairing failed: %v", evt.Error)
		g.mu.Lock()
		g.mu.Unlock()
		if g.client != nil {
			g.client.Disconnect()
		}
		if g.device != nil {
			if err := g.device.Delete(context.Background()); err != nil {
				log.Warnf("Failed to delete invalid WhatsApp device: %v", err)
			}
		}
		g.mu.Lock()
		g.device = nil
		g.client = nil
		g.container = nil
		g.mu.Unlock()
		GetQRManager().OnWhatsAppPairError(fmt.Sprintf("%v", evt.Error))

	case *events.Connected:
		log.Info("WhatsApp connected to servers")

	case *events.LoggedOut:
		log.Warn("WhatsApp logged out")
		GetQRManager().OnWhatsAppLogout()

	case *events.Disconnected:
		log.Warn("WhatsApp disconnected, will attempt to reconnect...")
		g.HandleDisconnection(fmt.Errorf("whatsapp disconnected"))

	case *events.PairSuccess:
		log.Infof("WhatsApp paired successfully: %s", evt.ID.String())
		g.mu.Lock()
		g.ownJID = evt.ID
		g.mu.Unlock()
		g.SetUserInfo(evt.ID.String(), evt.ID.User)
		GetQRManager().OnWhatsAppLoginSuccess()

	case *events.Contact:

	case *events.PushName:

	case *events.Message:
		g.handleIncomingMessage(evt)

	case *events.Receipt:

	case *events.AppStateSyncComplete:
		if g.client != nil && len(g.client.Store.PushName) > 0 && g.client.Store.PushName != "go-magic" {
			_ = g.client.SendPresence(context.Background(), types.PresenceAvailable)
		}
		log.Info("WhatsApp offline sync completed")
	}
}

func (g *WhatsAppGateway) handleIncomingMessage(evt *events.Message) {
	msg := evt.Message
	info := evt.Info

	if info.Sender.User == g.ownJID.User {
		return
	}

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
			content = "用户发送了一张图片"
		}
		msgType = "image"
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(context.Background(), msg.ImageMessage)
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
			content = "用户发送了一个视频"
		}
		msgType = "video"
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(context.Background(), msg.VideoMessage)
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
		content = "语音消息"
		msgType = "audio"
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(context.Background(), msg.AudioMessage)
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
		content = "文件:"
		msgType = "document"
		filename := msg.DocumentMessage.GetFileName()
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(context.Background(), msg.DocumentMessage)
			if err == nil && len(data) > 0 {
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
		content = fmt.Sprintf("Location: %.6f, %.6f",
			msg.LocationMessage.GetDegreesLatitude(),
			msg.LocationMessage.GetDegreesLongitude())
		msgType = "location"

	case msg.ContactMessage != nil:
		content = "Contact: " + msg.ContactMessage.GetDisplayName()
		msgType = "contact"

	case msg.StickerMessage != nil:
		content = "贴纸:"
		msgType = "sticker"
		if g.client != nil && g.client.IsConnected() {
			data, err := g.client.Download(context.Background(), msg.StickerMessage)
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
		content = "Reaction: " + msg.ReactionMessage.GetText()
		msgType = "reaction"

	default:
		content = "Unsupported message"
		msgType = "unknown"
	}

	sender := info.Sender.String()
	chatJID := info.Chat
	isGroup := chatJID.Server == types.GroupServer

	isMentioned := false
	if etm := msg.GetExtendedTextMessage(); etm != nil {
		if ctxInfo := etm.GetContextInfo(); ctxInfo != nil {
			if len(ctxInfo.GetMentionedJID()) > 0 {
				isMentioned = true
			}
		}
	}

	metadata := map[string]interface{}{
		"type":      msgType,
		"sender":    sender,
		"is_group":  isGroup,
		"push_name": info.PushName,
	}

	if isGroup {
		metadata["group_jid"] = chatJID.String()
	}

	waMsg := Message{
		ID:          info.ID,
		Platform:    "whatsapp",
		ChannelID:   chatJID.String(),
		UserID:      sender,
		Content:     content,
		Timestamp:   info.Timestamp,
		Metadata:    metadata,
		MediaURLs:   mediaURLs,
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
	}

	if !g.ShouldProcessChannel(chatJID.String()) {
		return
	}

	g.EmitMessage(waMsg)
}

func (g *WhatsAppGateway) RequestAppState() error {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	if client == nil || !client.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}
	err := client.FetchAppState(context.Background(), appstate.WAPatchCriticalBlock, false, false)
	if err != nil {
		return fmt.Errorf("failed to fetch app state: %w", err)
	}
	err = client.FetchAppState(context.Background(), appstate.WAPatchRegularHigh, false, false)
	if err != nil {
		return fmt.Errorf("failed to fetch regular app state: %w", err)
	}
	return nil
}

func (g *WhatsAppGateway) SendPresence(presence types.Presence) error {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	if client == nil || !client.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}
	return client.SendPresence(context.Background(), presence)
}

func (g *WhatsAppGateway) SendTyping(chatJID types.JID) error {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	if client == nil || !client.IsLoggedIn() {
		return fmt.Errorf("not logged in")
	}
	return client.SendChatPresence(context.Background(), chatJID, types.ChatPresenceComposing, types.ChatPresenceMediaText)
}

func (g *WhatsAppGateway) GetContactName(jid types.JID) string {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	if client == nil {
		return jid.User
	}
	contact, err := client.Store.Contacts.GetContact(context.Background(), jid)
	if err != nil || contact.FullName == "" {
		return jid.User
	}
	return contact.FullName
}

func (g *WhatsAppGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "logout":
		g.mu.RLock()
		client := g.client
		g.mu.RUnlock()
		if client != nil {
			client.Logout(context.Background())
		}
		return Response{
			ChannelID: msg.UserID,
			Content:   "Logged out of WhatsApp. Restart to login again.",
		}, nil
	case "status":
		g.mu.RLock()
		client := g.client
		g.mu.RUnlock()
		loggedIn := client != nil && client.IsLoggedIn()
		connected := client != nil && client.IsConnected()
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

func (g *WhatsAppGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	client := g.client
	g.mu.RUnlock()

	status := g.BasePlatform.CheckHealth()

	status.Platform = "whatsapp"
	status.Status = "healthy"
	status.Platforms = make(map[string]PlatformStatus)
	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}

	if client != nil && client.IsLoggedIn() {
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

func (g *WhatsAppGateway) GetLatestQR() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.latestQR
}

func (g *WhatsAppGateway) GetQRCodeForAPI() *QRCodeResponse {
	qr := g.GetLatestQR()
	if qr == "" {
		return nil
	}
	return &QRCodeResponse{
		QRCode: qr,
		Expiry: 60,
	}
}

func ForceDisplayQR(qrData string) {
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("        WhatsApp QR Code - Scan with WhatsApp App")
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println("IMPORTANT: Make sure your phone has:")
	fmt.Println("  - WhatsApp updated to latest version")
	fmt.Println("  - Multi-device enabled (Settings > Linked Devices)")
	fmt.Println("  - Less than 4 devices already linked")
	fmt.Println()
	fmt.Println("Instructions:")
	fmt.Println("  1. Open WhatsApp on your phone")
	fmt.Println("  2. Go to Settings > Linked Devices")
	fmt.Println("  3. Tap 'Link a Device'")
	fmt.Println("  4. Scan the QR code below")
	fmt.Println()

	qrFile, err := saveQRToFile(qrData)
	if err != nil {
		fmt.Printf("  [ERROR] Failed to save QR image: %v\n", err)
	} else {
		fmt.Printf("  [NEW] QR code PNG file: %s\n", qrFile)
		fmt.Println("  Please open this file and scan with WhatsApp!")
	}
	fmt.Println()

	fmt.Println("  [ASCII QR Code - if not displayed correctly, use the PNG file above]")
	fmt.Println()
	qrterminal.GenerateHalfBlock(qrData, qrterminal.L, os.Stdout)

	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("  WARNING: QR code expires in 60 seconds! Please scan quickly!")
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println("If QR code doesn't work, try:")
	fmt.Println("  1. Restart WhatsApp on your phone")
	fmt.Println("  2. Clear existing linked devices (Settings > Linked Devices)")
	fmt.Println("  3. Check your phone and computer have correct time")
	fmt.Println("  4. Ensure both devices have stable internet connection")
	fmt.Println()

	os.Stdout.Sync()
}

func saveQRToFile(qrData string) (string, error) {
	tmpDir := os.TempDir()
	qrFile := filepath.Join(tmpDir, "whatsapp-qr.png")

	err := qrcode.WriteFile(qrData, qrcode.Medium, 256, qrFile)
	if err != nil {
		return "", err
	}

	return qrFile, nil
}

type QRCodeResponse struct {
	QRCode string `json:"qr_code"`
	Expiry int    `json:"expires_in_seconds"`
}
