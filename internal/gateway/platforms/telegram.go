package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TelegramPlatform implements Telegram messaging
type TelegramPlatform struct {
	config     *PlatformConfig
	token      string
	apiBase    string
	connected  bool
	updates    chan Message
	httpClient *http.Client
}

// TelegramUpdate represents a Telegram update
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64        `json:"message_id"`
		From      TelegramUser `json:"from"`
		Chat      TelegramChat `json:"chat"`
		Text      string       `json:"text"`
		Date      int64        `json:"date"`
		Voice     *struct {
			FileID   string `json:"file_id"`
			Duration int    `json:"duration"`
			MimeType string `json:"mime_type"`
			FileSize int    `json:"file_size"`
		} `json:"voice"`
		Photo []struct {
			FileID string `json:"file_id"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"photo"`
		Video *struct {
			FileID   string `json:"file_id"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration int    `json:"duration"`
		} `json:"video"`
		Document *struct {
			FileID   string `json:"file_id"`
			FileName string `json:"file_name"`
			MimeType string `json:"mime_type"`
			FileSize int    `json:"file_size"`
		} `json:"document"`
		Caption string `json:"caption"`
	} `json:"message"`
}

// TelegramUser represents a Telegram user
type TelegramUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Language  string `json:"language_code"`
}

// TelegramChat represents a Telegram chat
type TelegramChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Username string `json:"username"`
	Title    string `json:"title"`
}

// TelegramMessage represents a Telegram message payload
type TelegramMessage struct {
	ChatID              int64  `json:"chat_id"`
	Text                string `json:"text"`
	ParseMode           string `json:"parse_mode,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

// NewTelegramPlatform creates a new Telegram platform
func NewTelegramPlatform(token string) *TelegramPlatform {
	return &TelegramPlatform{
		token:      token,
		apiBase:    "https://api.telegram.org/bot" + token,
		connected:  false,
		updates:    make(chan Message, 100),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the platform name
func (t *TelegramPlatform) Name() string {
	return "telegram"
}

// Connect establishes connection to Telegram
func (t *TelegramPlatform) Connect(ctx context.Context) error {
	// Verify token
	resp, err := t.httpClient.Get(t.apiBase + "/getMe")
	if err != nil {
		return fmt.Errorf("failed to connect to Telegram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram API error: status %d", resp.StatusCode)
	}

	t.connected = true
	return nil
}

// Disconnect closes the connection
func (t *TelegramPlatform) Disconnect() error {
	t.connected = false
	close(t.updates)
	return nil
}

// IsConnected returns connection status
func (t *TelegramPlatform) IsConnected() bool {
	return t.connected
}

// Send sends a message
func (t *TelegramPlatform) Send(ctx context.Context, chatID string, message Message) error {
	if !t.connected {
		return fmt.Errorf("not connected")
	}

	// Parse chat ID
	var chatIDInt int64
	fmt.Sscanf(chatID, "%d", &chatIDInt)

	// Handle media messages
	if message.Media != nil {
		return t.sendMedia(ctx, chatIDInt, message)
	}

	payload := TelegramMessage{
		ChatID: chatIDInt,
		Text:   message.Text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiBase+"/sendMessage", strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("send failed: status %d", resp.StatusCode)
	}

	return nil
}

// sendMedia sends a media message
func (t *TelegramPlatform) sendMedia(ctx context.Context, chatID int64, message Message) error {
	var endpoint string
	var form url.Values

	switch message.Media.Type {
	case "photo":
		endpoint = "/sendPhoto"
		form = url.Values{
			"chat_id": {fmt.Sprintf("%d", chatID)},
			"photo":   {message.Media.URL},
			"caption": {message.Text},
		}
	case "video":
		endpoint = "/sendVideo"
		form = url.Values{
			"chat_id": {fmt.Sprintf("%d", chatID)},
			"video":   {message.Media.URL},
			"caption": {message.Text},
		}
	case "audio":
		endpoint = "/sendAudio"
		form = url.Values{
			"chat_id": {fmt.Sprintf("%d", chatID)},
			"audio":   {message.Media.URL},
			"caption": {message.Text},
		}
	case "document":
		endpoint = "/sendDocument"
		form = url.Values{
			"chat_id":  {fmt.Sprintf("%d", chatID)},
			"document": {message.Media.URL},
			"caption":  {message.Text},
		}
	case "voice":
		endpoint = "/sendVoice"
		form = url.Values{
			"chat_id": {fmt.Sprintf("%d", chatID)},
			"voice":   {message.Media.URL},
			"caption": {message.Text},
		}
	default:
		return t.Send(ctx, fmt.Sprintf("%d", chatID), Message{Text: message.Text})
	}

	resp, err := t.httpClient.PostForm(t.apiBase+endpoint, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Receive receives messages (polling)
func (t *TelegramPlatform) Receive(ctx context.Context) (<-chan Message, error) {
	offset := int64(0)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				updates, err := t.getUpdates(offset)
				if err != nil {
					time.Sleep(time.Second)
					continue
				}

				for _, update := range updates {
					msg := t.convertUpdate(update)
					if msg.Text != "" || msg.Media != nil {
						t.updates <- msg
					}
					offset = update.UpdateID + 1
				}
			}
		}
	}()

	return t.updates, nil
}

// getUpdates fetches updates from Telegram
func (t *TelegramPlatform) getUpdates(offset int64) ([]TelegramUpdate, error) {
	url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=30", t.apiBase, offset)
	resp, err := t.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool             `json:"ok"`
		Result []TelegramUpdate `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Result, nil
}

// convertUpdate converts a Telegram update to our Message format
func (t *TelegramPlatform) convertUpdate(update TelegramUpdate) Message {
	msg := Message{
		ID:        fmt.Sprintf("%d", update.Message.MessageID),
		ChatID:    fmt.Sprintf("%d", update.Message.Chat.ID),
		Text:      update.Message.Text,
		Timestamp: time.Unix(update.Message.Date, 0),
		From: User{
			ID:        fmt.Sprintf("%d", update.Message.From.ID),
			Username:  update.Message.From.Username,
			FirstName: update.Message.From.FirstName,
			LastName:  update.Message.From.LastName,
			Language:  update.Message.From.Language,
		},
	}

	// Handle voice
	if update.Message.Voice != nil {
		msg.Media = &Media{
			Type:     "voice",
			FileID:   update.Message.Voice.FileID,
			Duration: update.Message.Voice.Duration,
			MimeType: update.Message.Voice.MimeType,
			Size:     int64(update.Message.Voice.FileSize),
		}
	}

	// Handle photo
	if len(update.Message.Photo) > 0 {
		msg.Media = &Media{
			Type:   "photo",
			FileID: update.Message.Photo[len(update.Message.Photo)-1].FileID,
			Width:  update.Message.Photo[len(update.Message.Photo)-1].Width,
			Height: update.Message.Photo[len(update.Message.Photo)-1].Height,
		}
	}

	// Handle video
	if update.Message.Video != nil {
		msg.Media = &Media{
			Type:     "video",
			FileID:   update.Message.Video.FileID,
			Width:    update.Message.Video.Width,
			Height:   update.Message.Video.Height,
			Duration: update.Message.Video.Duration,
		}
	}

	// Handle document
	if update.Message.Document != nil {
		msg.Media = &Media{
			Type:     "document",
			FileID:   update.Message.Document.FileID,
			MimeType: update.Message.Document.MimeType,
			Size:     int64(update.Message.Document.FileSize),
		}
	}

	// Caption
	if msg.Media != nil && update.Message.Caption != "" {
		msg.Text = update.Message.Caption
	}

	return msg
}

// SetWebhook sets up webhook for this platform
func (t *TelegramPlatform) SetWebhook(ctx context.Context, webhookURL string) error {
	form := url.Values{"url": {webhookURL}}
	resp, err := t.httpClient.PostForm(t.apiBase+"/setWebhook", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook setup failed: status %d", resp.StatusCode)
	}

	return nil
}

// GetFileURL gets the URL for a file
func (t *TelegramPlatform) GetFileURL(fileID string) (string, error) {
	resp, err := t.httpClient.Get(fmt.Sprintf("%s/getFile?file_id=%s", t.apiBase, fileID))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", t.token, result.Result.FilePath), nil
}
