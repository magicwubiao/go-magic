package gateway

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// TelegramConfig holds Telegram-specific configuration
type TelegramConfig struct {
	Token          string
	AdminUsers     []int64 // List of admin user IDs
	AllowGroups    bool    // Allow bot in groups
	StreamingReply bool    // Enable streaming reply for long messages
	AllowedChannels []string `json:"allowed_channels,omitempty"` // Whitelist of channel/chat IDs
	BlockedChannels []string `json:"blocked_channels,omitempty"` // Blacklist of channel/chat IDs
}

// TelegramHandler implements PlatformHandler for Telegram
type TelegramHandler struct {
	bot      *tgbotapi.BotAPI
	config   *TelegramConfig
	gateway  *Gateway
	stopCh   chan struct{}
	running  bool
	mu       sync.RWMutex
	stopOnce sync.Once
}

// NewTelegramHandler creates a new Telegram platform handler
func NewTelegramHandler(token string, config *TelegramConfig) (*TelegramHandler, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	if config == nil {
		config = &TelegramConfig{
			StreamingReply: true,
			AllowGroups:    true,
		}
	}

	return &TelegramHandler{
		bot:    bot,
		config: config,
		stopCh: make(chan struct{}),
	}, nil
}

// Connect establishes connection to Telegram
func (t *TelegramHandler) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("Telegram handler already running")
	}

	t.running = true
	return nil
}

// Disconnect closes the connection
func (t *TelegramHandler) Disconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
	t.running = false

	log.Info("Telegram handler disconnected")
	return nil
}

// Name returns the platform name
func (t *TelegramHandler) Name() string {
	return "telegram"
}

// IsConnected checks if connected to Telegram
func (t *TelegramHandler) IsConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// Send sends a message to Telegram
func (t *TelegramHandler) Send(ctx context.Context, resp Response) error {
	chatID, err := strconv.ParseInt(resp.ChannelID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid channel ID: %w", err)
	}

	// For streaming responses, split into chunks
	if t.config.StreamingReply && len(resp.Content) > 4096 {
		return t.sendStreamingMessage(chatID, resp.Content)
	}

	msg := tgbotapi.NewMessage(chatID, resp.Content)
	msg.ParseMode = "Markdown"
	_, err = t.bot.Send(msg)
	return err
}

// sendStreamingMessage sends a long message in chunks
func (t *TelegramHandler) sendStreamingMessage(chatID int64, content string) error {
	// Split into chunks of ~4000 chars (leaving room for formatting)
	const chunkSize = 4000
	messages := splitMessage(content, chunkSize)

	for i, chunk := range messages {
		msg := tgbotapi.NewMessage(chatID, chunk)
		if i == 0 {
			msg.ParseMode = "Markdown"
		}
		_, err := t.bot.Send(msg)
		if err != nil {
			return fmt.Errorf("failed to send chunk %d: %w", i, err)
		}
	}
	return nil
}

// splitMessage splits a message into chunks
func splitMessage(text string, chunkSize int) []string {
	var chunks []string
	lines := make([]string, 0, len(text)/50)
	currentLen := 0

	for _, line := range splitLines(text) {
		lineLen := len(line)
		if currentLen+lineLen > chunkSize && currentLen > 0 {
			chunks = append(chunks, joinLines(lines))
			lines = make([]string, 0, len(text)/50)
			currentLen = 0
		}
		lines = append(lines, line)
		currentLen += lineLen
	}

	if len(lines) > 0 {
		chunks = append(chunks, joinLines(lines))
	}

	return chunks
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

// Receive returns a channel of incoming messages
func (t *TelegramHandler) Receive() <-chan Message {
	msgCh := make(chan Message, 100)

	go func() {
		defer close(msgCh)

		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60

		updates := t.bot.GetUpdatesChan(u)

		for {
			select {
			case <-t.stopCh:
				return
			case update, ok := <-updates:
				if !ok {
					return
				}
				if update.Message == nil {
					continue
				}

				// Check group permissions
				if update.Message.Chat.Type != "private" && !t.config.AllowGroups {
					continue
				}

				// Check channel allowlist/blocklist
				channelID := fmt.Sprintf("%d", update.Message.Chat.ID)
				if !ShouldProcessChannel(channelID, t.config.AllowedChannels, t.config.BlockedChannels) {
					continue
				}

				// Handle different message types
				var content string
				var mediaURLs []MediaAttachment

				msg := update.Message

				// Check for caption (photos, videos, documents can have captions)
				caption := msg.Caption

				// Get text content
				text := msg.Text
				if text == "" && caption != "" {
					text = caption
				}
				content = text

				// Handle media
				if msg.Photo != nil {
					// Download photo
					photo := msg.Photo[len(msg.Photo)-1] // Get largest photo
					if file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: photo.FileID}); err == nil {
						mediaURLs = append(mediaURLs, MediaAttachment{
							Type:     "image",
							URL:      file.Link(t.bot.Token),
							MimeType: "image/jpeg",
							Caption:  caption,
							Size:     int64(photo.FileSize),
						})
					}
					if content == "" {
						content = ""
					}
				}

				if msg.Video != nil {
					if file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: msg.Video.FileID}); err == nil {
						mediaURLs = append(mediaURLs, MediaAttachment{
							Type:     "video",
							URL:      file.Link(t.bot.Token),
							MimeType: "video/mp4",
							Caption:  caption,
							Size:     int64(msg.Video.FileSize),
						})
					}
					if content == "" {
						content = ""
					}
				}

				if msg.Document != nil {
					if file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: msg.Document.FileID}); err == nil {
						mediaURLs = append(mediaURLs, MediaAttachment{
							Type:     "file",
							URL:      file.Link(t.bot.Token),
							MimeType: msg.Document.MimeType,
							Filename: msg.Document.FileName,
							Caption:  caption,
							Size:     int64(msg.Document.FileSize),
						})
					}
					if content == "" {
						content = ""
					}
				}

				if msg.Voice != nil {
					if file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: msg.Voice.FileID}); err == nil {
						mediaURLs = append(mediaURLs, MediaAttachment{
							Type:     "audio",
							URL:      file.Link(t.bot.Token),
							MimeType: msg.Voice.MimeType,
							Size:     int64(msg.Voice.FileSize),
						})
					}
					if content == "" {
						content = ""
					}
				}

				if msg.Audio != nil {
					if file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: msg.Audio.FileID}); err == nil {
						mediaURLs = append(mediaURLs, MediaAttachment{
							Type:     "audio",
							URL:      file.Link(t.bot.Token),
							MimeType: msg.Audio.MimeType,
							Filename: msg.Audio.Title,
							Caption:  caption,
							Size:     int64(msg.Audio.FileSize),
						})
					}
					if content == "" {
						content = ""
					}
				}

				if msg.Sticker != nil {
					if file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: msg.Sticker.FileID}); err == nil {
						mediaURLs = append(mediaURLs, MediaAttachment{
							Type:     "image",
							URL:      file.Link(t.bot.Token),
							MimeType: "image/webp",
							Caption:  "Sticker",
							Size:     int64(msg.Sticker.FileSize),
						})
					}
					if content == "" {
						content = ""
					}
				}

				if msg.Location != nil {
					content = fmt.Sprintf("[Location: %.6f, %.6f]", msg.Location.Latitude, msg.Location.Longitude)
				}

				gwMsg := Message{
					ID:        fmt.Sprintf("tg-%d-%d", msg.Chat.ID, msg.MessageID),
					ChannelID: fmt.Sprintf("%d", msg.Chat.ID),
					Content:   content,
					Role:      "user",
					From:      msg.From.UserName,
					MediaURLs: mediaURLs,
					Metadata: map[string]interface{}{
						"chat_type": msg.Chat.Type,
					},
				}

				select {
				case msgCh <- gwMsg:
				case <-t.stopCh:
					return
				}
			}
		}
	}()

	return msgCh
}

// CheckHealth returns detailed health status for Telegram gateway
func (t *TelegramHandler) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:  "telegram",
		Connected: t.IsConnected(),
		Status:    "healthy",
		Details:   make(map[string]interface{}),
		Platforms: make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "telegram",
		Status: "connected",
	}

	if !t.IsConnected() {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Telegram handler not connected"
		status.Status = "unhealthy"
		status.Error = "Telegram handler not connected"
	}

	status.Platforms["telegram"] = platformStatus
	return status
}

// HandleSlashCommand handles a slash command
func (t *TelegramHandler) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	return Response{Content: "Slash commands not supported for Telegram"}, nil
}
