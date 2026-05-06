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

				msg := Message{
					ID:        fmt.Sprintf("tg-%d-%d", update.Message.Chat.ID, update.Message.MessageID),
					ChannelID: fmt.Sprintf("%d", update.Message.Chat.ID),
					Content:   update.Message.Text,
					Role:      "user",
					From:      update.Message.From.UserName,
				}

				select {
				case msgCh <- msg:
				case <-t.stopCh:
					return
				}
			}
		}
	}()

	return msgCh
}

// CheckHealth checks if the handler is healthy
func (t *TelegramHandler) CheckHealth() error {
	if !t.IsConnected() {
		return fmt.Errorf("Telegram handler not connected")
	}
	return nil
}
