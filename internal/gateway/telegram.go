package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/magicwubiao/go-magic/pkg/log"
)

type TelegramConfig struct {
	Token           string
	AdminUsers      []int64
	AllowGroups     bool
	StreamingReply  bool
	AllowedChannels []string `json:"allowed_channels,omitempty"`
	BlockedChannels []string `json:"blocked_channels,omitempty"`
}

type TelegramHandler struct {
	*BasePlatform

	bot    *tgbotapi.BotAPI
	config *TelegramConfig

	mu       sync.RWMutex
	stopOnce sync.Once
}

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

	acConfig := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	t := &TelegramHandler{
		bot:    bot,
		config: config,
	}

	t.BasePlatform = NewBasePlatform("telegram", acConfig)
	t.BasePlatform.onConnect = t.onConnect
	t.BasePlatform.onDisconnect = t.onDisconnect
	t.BasePlatform.onSend = t.onSend
	t.SetChannelFilter(config.AllowedChannels, config.BlockedChannels)

	return t, nil
}

func (t *TelegramHandler) onConnect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	log.Info("[Telegram] Starting update listener")

	go t.listenUpdates(ctx)

	return nil
}

func (t *TelegramHandler) onDisconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopOnce.Do(func() {
	})

	log.Info("[Telegram] Handler disconnected")
	return nil
}

func (t *TelegramHandler) onSend(ctx context.Context, resp Response) error {
	chatID, err := strconv.ParseInt(resp.ChannelID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid channel ID: %w", err)
	}

	if t.config.StreamingReply && len(resp.Content) > 4096 {
		return t.sendStreamingMessage(chatID, resp.Content)
	}

	msg := tgbotapi.NewMessage(chatID, resp.Content)
	msg.ParseMode = "Markdown"
	_, err = t.bot.Send(msg)
	return err
}

func (t *TelegramHandler) sendStreamingMessage(chatID int64, content string) error {
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

func (t *TelegramHandler) listenUpdates(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := t.bot.GetUpdatesChan(u)
	defer t.bot.StopReceivingUpdates()

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				t.HandleDisconnection(fmt.Errorf("update channel closed"))
				return
			}
			if update.Message == nil {
				continue
			}

			if update.Message.Chat.Type != "private" && !t.config.AllowGroups {
				continue
			}

			channelID := fmt.Sprintf("%d", update.Message.Chat.ID)
			if !t.ShouldProcessChannel(channelID) {
				continue
			}

			t.handleIncomingMessage(update.Message)
		}
	}
}

func (t *TelegramHandler) handleIncomingMessage(msg *tgbotapi.Message) {
	var content string
	var mediaURLs []MediaAttachment

	caption := msg.Caption

	text := msg.Text
	if text == "" && caption != "" {
		text = caption
	}
	content = text

	if msg.Photo != nil {
		photo := msg.Photo[len(msg.Photo)-1]
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
			content = "用户发送了一张图片"
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
			content = "用户发送了一个视频"
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
			content = fmt.Sprintf("用户发送了文件: %s", msg.Document.FileName)
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
			content = "用户发送了一段语音"
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
			content = "用户发送了一段音频"
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
			content = "用户发送了一个表情包"
		}
	}

	if msg.Location != nil {
		content = fmt.Sprintf("Location: %.6f, %.6f", msg.Location.Latitude, msg.Location.Longitude)
	}

	isGroup := msg.Chat.Type != "private"
	isMentioned := false
	if msg.Entities != nil && len(msg.Entities) > 0 {
		for _, entity := range msg.Entities {
			if entity.Type == "mention" {
				isMentioned = true
				break
			}
		}
	}

	gwMsg := Message{
		ID:          fmt.Sprintf("tg-%d-%d", msg.Chat.ID, msg.MessageID),
		ChannelID:   fmt.Sprintf("%d", msg.Chat.ID),
		Content:     content,
		Role:        "user",
		From:        msg.From.UserName,
		MediaURLs:   mediaURLs,
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"chat_type": msg.Chat.Type,
		},
	}

	t.EmitMessage(gwMsg)
}

func (t *TelegramHandler) CheckHealth() *HealthStatus {
	status := t.BasePlatform.CheckHealth()

	status.Platform = "telegram"
	status.Status = "healthy"
	status.Platforms = make(map[string]PlatformStatus)
	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}

	platformStatus := PlatformStatus{
		Name:   "telegram",
		Status: "connected",
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Telegram handler not connected"
		status.Status = "unhealthy"
		status.Error = "Telegram handler not connected"
	}

	status.Platforms["telegram"] = platformStatus
	return status
}

func (t *TelegramHandler) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch strings.ToLower(cmd) {
	case "help":
		return Response{
			Content: "🤖 Magic Bot - Telegram\n\n" +
				"📋 Commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Connection status\n" +
				"/new - Start new conversation\n" +
				"/compress - Compress context\n" +
				"/usage - Token usage stats\n" +
				"/model - Change AI model\n" +
				"/goal - Goal management\n" +
				"/kanban - Kanban board",
		}, nil
	case "ping":
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if t.IsConnected() {
			return Response{Content: "✅ Bot is connected and ready!"}, nil
		}
		return Response{Content: "❌ Bot is not connected"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}
