// Package gateway - WeChat iLink Bot Gateway implementation
//
// This implements the Tencent iLink Bot REST API (微信企点 iLink 机器人),
// the same API used by picoclaw (https://github.com/sipeed/picoclaw/tree/main/pkg/channels/weixin).
//
// The iLink Bot API enables WeChat bots to:
//   - Long-poll for incoming messages via GetUpdates
//   - Send text, image, video, and file messages
//   - Send typing indicators
//   - Upload/download media via CDN
//   - Login via QR code scanning
//   - Handle voice messages with built-in ASR transcription
//
// API Base URL: https://ilinkai.weixin.qq.com/
// Endpoints:
//   - GET  /ilink/bot/get_bot_qrcode       - Get QR code for login
//   - GET  /ilink/bot/get_qrcode_status     - Poll QR code scanning status
//   - POST /ilink/bot/getupdates            - Long-poll for new messages
//   - POST /ilink/bot/sendmessage           - Send a message
//   - POST /ilink/bot/getuploadurl          - Get CDN upload URL
//   - POST /ilink/bot/getconfig             - Get bot config (typing ticket, etc.)
//   - POST /ilink/bot/sendtyping            - Send typing indicator
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// iLink API constants
const (
	ilinkChannelVersion  = "2.1.1"
	ilinkAppID           = "bot"
	ilinkClientVersion   = 131329 // 2.1.1 encoded: 0x00020101
	ilinkDefaultBaseURL  = "https://ilinkai.weixin.qq.com/"
	ilinkDefaultBotType  = "3"

	// Long-poll defaults
	ilinkDefaultPollTimeoutMs = 35_000
	ilinkPollRetryDelay       = 2 * time.Second
	ilinkPollBackoffDelay     = 30 * time.Second
	ilinkMaxConsecutiveFails  = 3

	// Session management
	ilinkSessionExpiredCode    = -14
	ilinkSessionPauseDuration  = time.Hour
	ilinkConfigCacheTTL        = 24 * time.Hour
	ilinkConfigRetryInitial    = 2 * time.Second
	ilinkConfigRetryMax        = time.Hour

	// Media limits
	ilinkMediaMaxBytes          = 100 << 20 // 100MB
	ilinkUploadRetryMax         = 3
	ilinkDownloadRetryMax       = 2
	ilinkDownloadRetryDelay     = 300 * time.Millisecond

	// Typing indicator
	ilinkTypingKeepAlive = 5 * time.Second

	// Default CDN base URL
	ilinkDefaultCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"

	// Auth polling
	ilinkAuthPollInterval = 2 * time.Second
	ilinkAuthDefaultTimeout = 5 * time.Minute
)

// WeChatILinkConfig holds configuration for the WeChat iLink Bot gateway.
type WeChatILinkConfig struct {
	// Bot token obtained from QR code login
	Token string `json:"token"`

	// Base URL for iLink API (default: https://ilinkai.weixin.qq.com/)
	BaseURL string `json:"base_url"`

	// CDN base URL for media upload/download (default: https://novac2c.cdn.weixin.qq.com/c2c)
	CDNBaseURL string `json:"cdn_base_url"`

	// Proxy URL for HTTP requests (optional)
	Proxy string `json:"proxy"`

	// Data directory for persisting sync buffers and context tokens
	DataDir string `json:"data_dir"`

	// Auto-login: if true, start QR login flow when no token is available
	AutoLogin bool `json:"auto_login"`

	// Bot type (default: "3")
	BotType string `json:"bot_type"`

	// Max reconnection attempts (0 = unlimited)
	MaxReconnectAttempts int `json:"max_reconnect_attempts"`

	// Whether to enable media download for inbound messages
	EnableMediaDownload bool `json:"enable_media_download"`
}

// WeChatILinkGateway implements the WeChat iLink Bot API for go-magic.
//
// Architecture:
//   - Connect() starts the long-poll loop that receives messages via GetUpdates
//   - Messages are parsed and forwarded to the msgCh channel
//   - Send() sends messages via the SendMessage API
//   - Supports typing indicators via StartTyping/StopTyping
//   - Supports media upload/download via CDN
//   - Supports QR code login flow
type WeChatILinkGateway struct {
	config WeChatILinkConfig

	// API client
	api    *ILinkAPIClient
	client *httpClient

	// Message channel (incoming)
	msgCh  chan Message
	stopCh chan struct{}

	mu      sync.RWMutex
	running bool

	// Context management
	ctx    context.Context
	cancel context.CancelFunc

	// Session state
	pauseUntil     time.Time
	pauseMu        sync.Mutex
	contextTokens  sync.Map // from_user_id → context_token
	typingCache    map[string]typingCacheEntry
	typingMu       sync.Mutex
	syncBuf        string // get_updates_buf cursor

	// Connection tracking
	connectedAt    time.Time
	reconnectCount int
	reconnecting   bool
	loginMu        sync.Mutex

	// Stats
	msgCount  int64
	lastMsgAt atomic.Value // time.Time
}

// typingCacheEntry caches typing tickets with TTL and exponential backoff.
type typingCacheEntry struct {
	ticket      string
	nextFetchAt time.Time
	retryDelay  time.Duration
}

// NewWeChatILinkGateway creates a new WeChat iLink Bot gateway.
func NewWeChatILinkGateway(cfg WeChatILinkConfig) *WeChatILinkGateway {
	if cfg.BaseURL == "" {
		cfg.BaseURL = ilinkDefaultBaseURL
	}
	if cfg.CDNBaseURL == "" {
		cfg.CDNBaseURL = ilinkDefaultCDNBaseURL
	}
	if cfg.BotType == "" {
		cfg.BotType = ilinkDefaultBotType
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/wechat-ilink"
	}

	return &WeChatILinkGateway{
		config:      cfg,
		msgCh:       make(chan Message, 200),
		stopCh:      make(chan struct{}),
		typingCache: make(map[string]typingCacheEntry),
	}
}

// Name returns the platform name.
func (g *WeChatILinkGateway) Name() string {
	return "wechat-ilink"
}

// Connect establishes connection to the WeChat iLink Bot API.
func (g *WeChatILinkGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}

	g.ctx, g.cancel = context.WithCancel(ctx)
	g.running = true
	g.reconnectCount = 0
	g.reconnecting = false
	g.mu.Unlock()

	log.Infof("[WeChat-iLink] Connecting to %s", g.config.BaseURL)

	// Create API client
	api, err := NewILinkAPIClient(g.config.BaseURL, g.config.Token, g.config.Proxy)
	if err != nil {
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
		return fmt.Errorf("failed to create iLink API client: %w", err)
	}
	g.api = api

	// Create HTTP client for CDN operations
	g.client = newHTTPClient(g.config.Proxy)

	// If no token, try auto-login
	if g.config.Token == "" && g.config.AutoLogin {
		log.Info("[WeChat-iLink] No token available, starting QR code login...")
		token, _, _, baseURL, err := PerformILinkLogin(ctx, ILinkLoginOpts{
			BaseURL: g.config.BaseURL,
			BotType: g.config.BotType,
			Proxy:   g.config.Proxy,
			Timeout: ilinkAuthDefaultTimeout,
		})
		if err != nil {
			g.mu.Lock()
			g.running = false
			g.mu.Unlock()
			return fmt.Errorf("auto-login failed: %w", err)
		}
		g.config.Token = token
		g.config.BaseURL = baseURL
		_ = g.saveTokenToConfig(token, baseURL) // persist token for next restart

		// Re-create API client with new token
		api, err = NewILinkAPIClient(baseURL, token, g.config.Proxy)
		if err != nil {
			g.mu.Lock()
			g.running = false
			g.mu.Unlock()
			return fmt.Errorf("failed to re-create API client after login: %w", err)
		}
		g.api = api
	}

	if g.config.Token == "" {
		log.Warn("[WeChat-iLink] No token configured. Set token or enable auto-login.")
		// Still mark as "connected" - user can login later via /login command
	} else {
		log.Info("[WeChat-iLink] Token configured, starting message polling...")
	}

	g.connectedAt = time.Now()
	go g.pollLoop(g.ctx)

	log.Info("[WeChat-iLink] Gateway connected")
	return nil
}

// Disconnect closes the connection and stops all goroutines.
func (g *WeChatILinkGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	if g.cancel != nil {
		g.cancel()
	}

	close(g.stopCh)
	g.running = false

	log.Info("[WeChat-iLink] Gateway disconnected")
	return nil
}

// IsConnected checks whether the gateway is running and has a valid token.
func (g *WeChatILinkGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.config.Token != ""
}

// Send sends a text message to a WeChat user.
func (g *WeChatILinkGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("wechat-ilink: not connected")
	}
	if resp.ChannelID == "" {
		return fmt.Errorf("wechat-ilink: ChannelID (from_user_id) is required")
	}
	if err := g.ensureSessionActive(); err != nil {
		return err
	}

	// Get context token for this user
	contextToken := g.getContextToken(resp.ChannelID)
	if contextToken == "" {
		return fmt.Errorf("wechat-ilink: missing context token for user %s (send a message first)", resp.ChannelID)
	}

	return g.sendTextMessage(ctx, resp.ChannelID, contextToken, resp.Content)
}

// SendText sends a text message directly to a user.
func (g *WeChatILinkGateway) SendText(ctx context.Context, toUserID, text string) error {
	if !g.IsConnected() {
		return fmt.Errorf("wechat-ilink: not connected")
	}
	if err := g.ensureSessionActive(); err != nil {
		return err
	}

	contextToken := g.getContextToken(toUserID)
	if contextToken == "" {
		return fmt.Errorf("wechat-ilink: missing context token for user %s", toUserID)
	}

	return g.sendTextMessage(ctx, toUserID, contextToken, text)
}

// Receive returns a channel of incoming messages.
func (g *WeChatILinkGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles slash commands.
func (g *WeChatILinkGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "Available commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Show connection status\n" +
				"/login - Start QR code login\n" +
				"/stats - Show message statistics\n" +
				"/health - Detailed health check",
		}, nil
	case "ping":
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if g.IsConnected() {
			return Response{
				Content: fmt.Sprintf("WeChat iLink Bot is connected and ready! 🟢\n"+
					"Connected for: %v\n"+
					"Reconnections: %d",
					time.Since(g.connectedAt).Round(time.Second),
					g.reconnectCount),
			}, nil
		}
		return Response{Content: "WeChat iLink Bot is not connected 🔴"}, nil
	case "login":
		go func() {
			ctx := context.Background()
			token, userID, botID, baseURL, err := PerformILinkLogin(ctx, ILinkLoginOpts{
				BaseURL: g.config.BaseURL,
				BotType: g.config.BotType,
				Proxy:   g.config.Proxy,
				Timeout: ilinkAuthDefaultTimeout,
			})
			if err != nil {
				log.Errorf("[WeChat-iLink] Login failed: %v", err)
				return
			}
			g.mu.Lock()
			g.config.Token = token
			g.config.BaseURL = baseURL
			_ = g.saveTokenToConfig(token, baseURL) // persist for next restart
			g.mu.Unlock()

			// Re-create API client
			api, err := NewILinkAPIClient(baseURL, token, g.config.Proxy)
			if err == nil {
				g.mu.Lock()
				g.api = api
				g.mu.Unlock()
			}

			log.Infof("[WeChat-iLink] Login successful! BotID: %s, UserID: %s", botID, userID)
		}()
		return Response{Content: "Starting QR code login... 📱 Please scan the QR code displayed in the terminal."}, nil
	case "stats":
		lastMsg := g.lastMsgAt.Load()
		lastMsgStr := "N/A"
		if t, ok := lastMsg.(time.Time); ok && !t.IsZero() {
			lastMsgStr = t.Format("15:04:05")
		}

		return Response{
			Content: fmt.Sprintf("📊 Message Statistics:\n"+
				"  Total messages: %d\n"+
				"  Last message: %s\n"+
				"  Connected since: %s",
				atomic.LoadInt64(&g.msgCount),
				lastMsgStr,
				g.connectedAt.Format("2006-01-02 15:04:05")),
		}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status.
func (g *WeChatILinkGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:  "wechat-ilink",
		Status:    "healthy",
		Details:   make(map[string]interface{}),
		Platforms: make(map[string]PlatformStatus),
	}

	platformStatus := PlatformStatus{
		Name:   "wechat-ilink",
		Status: "connected",
	}

	g.mu.RLock()
	status.Connected = g.running && g.config.Token != ""
	status.Details["has_token"] = g.config.Token != ""
	status.Details["base_url"] = g.config.BaseURL
	status.Details["reconnect_count"] = g.reconnectCount
	status.Details["reconnecting"] = g.reconnecting
	if !g.connectedAt.IsZero() {
		status.Details["connected_since"] = g.connectedAt.Format(time.RFC3339)
		status.Details["uptime"] = time.Since(g.connectedAt).Round(time.Second).String()
	}

	// Check session pause state
	g.pauseMu.Lock()
	if !g.pauseUntil.IsZero() {
		remaining := time.Until(g.pauseUntil)
		if remaining > 0 {
			status.Details["session_paused"] = true
			status.Details["session_resume_at"] = g.pauseUntil.Format(time.RFC3339)
			status.Details["session_pause_remaining"] = remaining.Round(time.Second).String()
		}
	}
	g.pauseMu.Unlock()

	// Check context tokens count
	tokenCount := 0
	g.contextTokens.Range(func(_, _ interface{}) bool {
		tokenCount++
		return true
	})
	status.Details["context_tokens"] = tokenCount
	status.Details["msg_count"] = atomic.LoadInt64(&g.msgCount)

	g.mu.RUnlock()

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Error = "Gateway not connected"
	}

	status.Platforms["wechat-ilink"] = platformStatus
	return status
}

// ============================================================================
// Poll Loop
// ============================================================================

// pollLoop is the main long-poll receive loop.
func (g *WeChatILinkGateway) pollLoop(ctx context.Context) {
	nextTimeoutMs := ilinkDefaultPollTimeoutMs
	consecutiveFails := 0

	log.Info("[WeChat-iLink] Poll loop started")

	for {
		select {
		case <-ctx.Done():
			log.Info("[WeChat-iLink] Poll loop stopped")
			return
		default:
		}

		// Wait if session is paused (e.g., token expired)
		if err := g.waitWhilePaused(ctx); err != nil {
			return
		}

		// Skip poll if no token
		g.mu.RLock()
		token := g.config.Token
		g.mu.RUnlock()
		if token == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Build context with timeout slightly longer than long-poll timeout
		pollCtx, pollCancel := context.WithTimeout(ctx,
			time.Duration(nextTimeoutMs+5000)*time.Millisecond)

		// Get updates via long-poll
		resp, err := g.api.GetUpdates(pollCtx, ILinkGetUpdatesReq{
			GetUpdatesBuf: g.syncBuf,
		})
		pollCancel()

		if err != nil {
			if ctx.Err() != nil {
				return
			}

			consecutiveFails++
			log.Warnf("[WeChat-iLink] GetUpdates failed (attempt %d): %v",
				consecutiveFails, err)

			if consecutiveFails >= ilinkMaxConsecutiveFails {
				log.Warnf("[WeChat-iLink] Too many consecutive failures, backing off %v",
					ilinkPollBackoffDelay)
				consecutiveFails = 0
				select {
				case <-ctx.Done():
					return
				case <-time.After(ilinkPollBackoffDelay):
				}
			} else {
				select {
				case <-ctx.Done():
					return
				case <-time.After(ilinkPollRetryDelay):
				}
			}
			continue
		}

		// Check for session expiration
		if isSessionExpired(resp.Ret, resp.Errcode) {
			remaining := g.pauseSession(resp.Ret, resp.Errcode, resp.Errmsg)
			log.Warnf("[WeChat-iLink] Session expired, pausing for %v", remaining)
			select {
			case <-ctx.Done():
				return
			case <-time.After(remaining):
			}
			continue
		}

		// Handle API errors
		if resp.Errcode != 0 || resp.Ret != 0 {
			consecutiveFails++
			log.Warnf("[WeChat-iLink] GetUpdates API error: ret=%d errcode=%d errmsg=%s",
				resp.Ret, resp.Errcode, resp.Errmsg)
			select {
			case <-ctx.Done():
				return
			case <-time.After(ilinkPollRetryDelay):
			}
			continue
		}

		// Success - reset failure counter
		consecutiveFails = 0

		// Update long-poll timeout from server hint
		if resp.LongpollingTimeoutMs > 0 {
			nextTimeoutMs = resp.LongpollingTimeoutMs
		}

		// Advance sync cursor
		if resp.GetUpdatesBuf != "" {
			g.syncBuf = resp.GetUpdatesBuf
		}

		// Process messages
		for _, msg := range resp.Msgs {
			g.handleIncomingMessage(msg)
		}
	}
}

// ============================================================================
// Message Handling
// ============================================================================

// handleIncomingMessage converts an iLink message to a gateway Message
// and forwards it to the msgCh channel.
func (g *WeChatILinkGateway) handleIncomingMessage(msg ILinkMessage) {
	fromUserID := msg.FromUserID
	if fromUserID == "" {
		return
	}

	messageID := msg.ClientID
	if messageID == "" {
		messageID = uuid.New().String()
	}

	// Build text content from item_list
	var parts []string
	for _, item := range msg.ItemList {
		switch item.Type {
		case ILinkItemTypeText:
			if item.TextItem != nil && item.TextItem.Text != "" {
				parts = append(parts, item.TextItem.Text)
			}
		case ILinkItemTypeVoice:
			if item.VoiceItem != nil && item.VoiceItem.Text != "" {
				// Use server-side ASR transcription
				parts = append(parts, item.VoiceItem.Text)
			} else {
				parts = append(parts, "[语音消息]")
			}
		case ILinkItemTypeImage:
			parts = append(parts, "[图片]")
		case ILinkItemTypeFile:
			if item.FileItem != nil && item.FileItem.FileName != "" {
				parts = append(parts, fmt.Sprintf("[文件: %s]", item.FileItem.FileName))
			} else {
				parts = append(parts, "[文件]")
			}
		case ILinkItemTypeVideo:
			parts = append(parts, "[视频]")
		}
	}

	content := strings.Join(parts, "\n")
	if content == "" {
		return
	}

	// Determine chat type
	chatType := "direct"
	if msg.GroupID != "" {
		chatType = "group"
	}

	// Store context token for outbound replies
	if msg.ContextToken != "" {
		g.contextTokens.Store(fromUserID, msg.ContextToken)
	}

	// Update stats
	atomic.AddInt64(&g.msgCount, 1)
	g.lastMsgAt.Store(time.Now())

	gatewayMsg := Message{
		ID:        messageID,
		Platform:  "wechat-ilink",
		ChannelID: fromUserID,
		UserID:    fromUserID,
		Content:   content,
		Timestamp: time.UnixMilli(msg.CreateTimeMs),
		Metadata: map[string]interface{}{
			"from_user_id":  fromUserID,
			"context_token": msg.ContextToken,
			"session_id":    msg.SessionID,
			"group_id":      msg.GroupID,
			"message_type":  msg.MessageType,
			"chat_type":     chatType,
		},
	}

	log.Debugf("[WeChat-iLink] 📨 Message from %s: %s",
		gatewayMsg.UserID, truncateString(content, 50))

	select {
	case g.msgCh <- gatewayMsg:
	default:
		log.Warnf("[WeChat-iLink] ⚠️ Message channel full, dropping from %s", fromUserID)
	}
}

// ============================================================================
// Message Sending
// ============================================================================

// sendTextMessage sends a text message to a WeChat user via the iLink API.
func (g *WeChatILinkGateway) sendTextMessage(ctx context.Context, toUserID, contextToken, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	return g.api.SendMessage(ctx, ILinkSendMessageReq{
		Msg: ILinkMessage{
			ToUserID:     toUserID,
			ClientID:     "magic-" + uuid.New().String(),
			MessageType:  ILinkMsgTypeBot,
			MessageState: ILinkMsgStateFinish,
			ContextToken: contextToken,
			ItemList: []ILinkMessageItem{
				{
					Type: ILinkItemTypeText,
					TextItem: &ILinkTextItem{
						Text: text,
					},
				},
			},
		},
	})
}

// ============================================================================
// Session Management
// ============================================================================

// ensureSessionActive checks whether the session is paused and returns an error if so.
func (g *WeChatILinkGateway) ensureSessionActive() error {
	g.pauseMu.Lock()
	defer g.pauseMu.Unlock()

	if g.pauseUntil.IsZero() {
		return nil
	}

	remaining := time.Until(g.pauseUntil)
	if remaining <= 0 {
		g.pauseUntil = time.Time{}
		return nil
	}

	return fmt.Errorf("wechat-ilink: session paused (%d min remaining)",
		int((remaining+time.Minute-1)/time.Minute))
}

// pauseSession pauses the session due to an expired token or API error.
func (g *WeChatILinkGateway) pauseSession(ret, errcode int, errmsg string) time.Duration {
	g.pauseMu.Lock()
	defer g.pauseMu.Unlock()

	until := time.Now().Add(ilinkSessionPauseDuration)
	if until.After(g.pauseUntil) {
		g.pauseUntil = until
	}

	remaining := time.Until(g.pauseUntil)
	log.Errorf("[WeChat-iLink] Session paused: ret=%d errcode=%d errmsg=%q (resume in %v)",
		ret, errcode, errmsg, remaining.Round(time.Second))

	return remaining
}

// waitWhilePaused blocks until the session pause expires or the context is done.
func (g *WeChatILinkGateway) waitWhilePaused(ctx context.Context) error {
	g.pauseMu.Lock()
	if g.pauseUntil.IsZero() {
		g.pauseMu.Unlock()
		return nil
	}
	remaining := time.Until(g.pauseUntil)
	if remaining <= 0 {
		g.pauseUntil = time.Time{}
		g.pauseMu.Unlock()
		return nil
	}
	g.pauseMu.Unlock()

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// getContextToken returns the stored context token for a user.
func (g *WeChatILinkGateway) getContextToken(userID string) string {
	if v, ok := g.contextTokens.Load(userID); ok {
		if token, ok := v.(string); ok {
			return token
		}
	}
	return ""
}

// ============================================================================
// Typing Indicator
// ============================================================================

// StartTyping starts a typing indicator for the given user.
// Returns a stop function that cancels the typing indicator.
func (g *WeChatILinkGateway) StartTyping(ctx context.Context, chatID string) (func(), error) {
	if chatID == "" || !g.IsConnected() {
		return func() {}, nil
	}

	if err := g.ensureSessionActive(); err != nil {
		return func() {}, nil // silently ignore when paused
	}

	contextToken := g.getContextToken(chatID)
	ticket, err := g.getTypingTicket(ctx, chatID, contextToken)
	if err != nil || ticket == "" {
		return func() {}, nil // silently fail - typing is best-effort
	}

	typingCtx, cancel := context.WithCancel(ctx)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			_ = g.api.SendTyping(stopCtx, ILinkSendTypingReq{
				IlinkUserID:  chatID,
				TypingTicket: ticket,
				Status:       ILinkTypingCancel,
			})
		})
	}

	// Start typing
	if err := g.api.SendTyping(typingCtx, ILinkSendTypingReq{
		IlinkUserID:  chatID,
		TypingTicket: ticket,
		Status:       ILinkTypingTyping,
	}); err != nil {
		stop()
		return func() {}, err
	}

	// Keep-alive ticker
	ticker := time.NewTicker(ilinkTypingKeepAlive)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				_ = g.api.SendTyping(typingCtx, ILinkSendTypingReq{
					IlinkUserID:  chatID,
					TypingTicket: ticket,
					Status:       ILinkTypingTyping,
				})
			}
		}
	}()

	return stop, nil
}

// StopTyping stops the typing indicator for the given user.
func (g *WeChatILinkGateway) StopTyping(chatID string) {
	// The stop function returned by StartTyping handles this
}

// getTypingTicket gets or refreshes a typing ticket for the user.
func (g *WeChatILinkGateway) getTypingTicket(ctx context.Context, userID, contextToken string) (string, error) {
	now := time.Now()

	g.typingMu.Lock()
	entry, ok := g.typingCache[userID]
	if ok && now.Before(entry.nextFetchAt) {
		ticket := entry.ticket
		g.typingMu.Unlock()
		return ticket, nil
	}
	cachedTicket := entry.ticket
	retryDelay := entry.retryDelay
	g.typingMu.Unlock()

	resp, err := g.api.GetConfig(ctx, ILinkGetConfigReq{
		IlinkUserID:  userID,
		ContextToken: contextToken,
	})
	if err == nil && resp != nil && resp.Ret == 0 && resp.Errcode == 0 {
		ticket := strings.TrimSpace(resp.TypingTicket)
		g.typingMu.Lock()
		g.typingCache[userID] = typingCacheEntry{
			ticket:      ticket,
			nextFetchAt: now.Add(ilinkConfigCacheTTL),
			retryDelay:  ilinkConfigRetryInitial,
		}
		g.typingMu.Unlock()
		return ticket, nil
	}

	if resp != nil && isSessionExpired(resp.Ret, resp.Errcode) {
		g.pauseSession(resp.Ret, resp.Errcode, resp.Errmsg)
	}

	// Exponential backoff
	if retryDelay <= 0 {
		retryDelay = ilinkConfigRetryInitial
	} else {
		retryDelay = time.Duration(math.Min(
			float64(retryDelay*2),
			float64(ilinkConfigRetryMax),
		))
	}

	g.typingMu.Lock()
	g.typingCache[userID] = typingCacheEntry{
		ticket:      cachedTicket,
		nextFetchAt: now.Add(retryDelay),
		retryDelay:  retryDelay,
	}
	g.typingMu.Unlock()

	if err != nil {
		return cachedTicket, err
	}
	return cachedTicket, fmt.Errorf("getconfig: ret=%d errcode=%d", resp.Ret, resp.Errcode)
}

// ============================================================================
// Token Persistence
// ============================================================================

// saveTokenToConfig persists the bot token to ~/.magic/config.json
// so the token survives process restarts (no re-scan needed).
func (g *WeChatILinkGateway) saveTokenToConfig(token, baseURL string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home dir: %w", err)
	}

	configPath := filepath.Join(homeDir, ".magic", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config file not found; save token to data dir as fallback
		tokenFile := filepath.Join(g.config.DataDir, "token.json")
		if mkdirErr := os.MkdirAll(g.config.DataDir, 0755); mkdirErr != nil {
			return fmt.Errorf("failed to create data dir: %w", mkdirErr)
		}
		tokenData := map[string]string{
			"token":    token,
			"base_url": baseURL,
		}
		tokenBytes, _ := json.MarshalIndent(tokenData, "", "  ")
		return os.WriteFile(tokenFile, tokenBytes, 0644)
	}

	// Parse existing config as raw map to preserve all fields
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Navigate/initialize gateway.platforms.wechat_ilink
	gatewaySection := ensureMap(cfg, "gateway")
	platforms := ensureMap(gatewaySection, "platforms")
	ilinkSection := ensureMap(platforms, "wechat_ilink")
	ilinkSection["token"] = token
	ilinkSection["api_url"] = baseURL
	platforms["wechat_ilink"] = ilinkSection
	gatewaySection["platforms"] = platforms
	cfg["gateway"] = gatewaySection

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(configPath, newData, 0644)
}

// ensureMap gets or creates a nested map[string]interface{} at the given key.
func ensureMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if sub, ok := v.(map[string]interface{}); ok {
			return sub
		}
	}
	sub := make(map[string]interface{})
	m[key] = sub
	return sub
}

// ============================================================================
// Utility
// ============================================================================

func isSessionExpired(ret, errcode int) bool {
	return ret == ilinkSessionExpiredCode || errcode == ilinkSessionExpiredCode
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// randomHex generates a hex-encoded random string of n bytes.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to UUID
		return strings.ReplaceAll(uuid.New().String(), "-", "")[:n*2]
	}
	return hex.EncodeToString(buf)
}
