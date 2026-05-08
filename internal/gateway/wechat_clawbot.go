package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	clawbot "github.com/importcjj/wechat-clawbot-client-go"
	"github.com/importcjj/wechat-clawbot-client-go/store"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// WeChatClawConfig holds configuration for WeChat ClawBot gateway
type WeChatClawConfig struct {
	// Bot ID / client identifier
	ClientID string `json:"client_id"`
	// Directory to persist login credentials and sync buffers
	DataDir string `json:"data_dir"`
	// Base URL for the ilink bot API (default: https://ilinkai.weixin.qq.com)
	BaseURL string `json:"base_url"`
	// Whether to auto-login (scan QR code) when no credentials
	AutoLogin bool `json:"auto_login"`
	// Max reconnection attempts on disconnect (default: 5)
	MaxReconnectAttempts int `json:"max_reconnect_attempts"`
	// Reconnection delay between attempts (default: 5s)
	ReconnectDelay time.Duration `json:"reconnect_delay"`
}

// WeChatClawGateway implements WeChat personal account via ClawBot (iLink Bot API)
type WeChatClawGateway struct {
	config  WeChatClawConfig
	client  *clawbot.DefaultClient
	msgCh   chan Message
	stopCh  chan struct{}
	mu      sync.RWMutex
	running bool
	loginMu sync.Mutex

	// Message bridge to agent (for Nudge system integration)
	agentCh chan<- Message

	// Reconnection state
	reconnectCount int

	// Health tracking
	connectedAt time.Time
	lastMsgTime time.Time
	msgCount    int64
}

// NewWeChatClawGateway creates a new WeChat ClawBot gateway
func NewWeChatClawGateway(cfg WeChatClawConfig) *WeChatClawGateway {
	if cfg.ClientID == "" {
		cfg.ClientID = "wechat-clawbot"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/wechat-clawbot"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://ilinkai.weixin.qq.com"
	}
	if cfg.MaxReconnectAttempts <= 0 {
		cfg.MaxReconnectAttempts = 5
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}

	return &WeChatClawGateway{
		config: cfg,
		msgCh:  make(chan Message, 200), // increased buffer for busy chats
		stopCh: make(chan struct{}),
	}
}

// Name returns the platform name
func (g *WeChatClawGateway) Name() string {
	return "wechat-clawbot"
}

// Connect establishes connection to WeChat via ClawBot
func (g *WeChatClawGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.reconnectCount = 0
	g.mu.Unlock()

	log.Infof("[WeChat-ClawBot] Connecting... (data dir: %s)", g.config.DataDir)

	// Create filestore for credential persistence
	fileStore := store.NewFileStore(g.config.DataDir)

	// Create clawbot client with DefaultEventHooks
	g.client = clawbot.NewDefault(g.config.ClientID, fileStore,
		clawbot.WithDefaultEventHooks(clawbot.DefaultEventHooks{
			OnMessage: func(clientID string, msg *clawbot.Message) {
				g.handleIncomingMessage(clientID, msg)
			},
			OnConnected: func(clientID string) {
				g.mu.Lock()
				g.connectedAt = time.Now()
				g.reconnectCount = 0
				g.mu.Unlock()
				log.Infof("[WeChat-ClawBot:%s] Connected!", clientID)
			},
			OnQRCode: func(clientID string, qrURL string) {
				log.Infof("[WeChat-ClawBot:%s] QR Code URL: %s", clientID, qrURL)
				log.Info("[WeChat-ClawBot] Please scan the QR code with WeChat to login")
			},
			OnQRScanned: func(clientID string) {
				log.Info("[WeChat-ClawBot] QR code scanned! Waiting for confirmation...")
			},
			OnQRExpired: func(clientID string, refreshCount int) {
				log.Warnf("[WeChat-ClawBot] QR code expired (refresh #%d)", refreshCount)
			},
			OnSessionExpired: func(clientID string) {
				log.Warn("[WeChat-ClawBot] Session expired, need to re-login")
				// If auto-login is enabled, trigger re-login
				if g.config.AutoLogin {
					go func() {
						reCtx := context.Background()
						if err := g.login(reCtx); err != nil {
							log.Errorf("[WeChat-ClawBot] Re-login failed: %v", err)
						} else {
							log.Info("[WeChat-ClawBot] Re-login successful, restarting...")
							if err := g.client.Start(reCtx); err != nil {
								log.Errorf("[WeChat-ClawBot] Restart after re-login failed: %v", err)
							}
						}
					}()
				}
			},
			OnDisconnected: func(clientID string, err error) {
				log.Warnf("[WeChat-ClawBot] Disconnected: %v", err)
				// Auto-reconnect if running
				g.mu.RLock()
				running := g.running
				g.mu.RUnlock()
				if running {
					go g.reconnect()
				}
			},
			OnError: func(clientID string, err error) {
				log.Errorf("[WeChat-ClawBot] Error: %v", err)
			},
		}),
	)

	// Start the client in a goroutine (Start() blocks)
	go func() {
		err := g.client.Start(ctx)
		if err != nil {
			if err == clawbot.ErrNotLoggedIn {
				log.Info("[WeChat-ClawBot] No saved credentials found, need to login")

				if g.config.AutoLogin {
					if err := g.login(ctx); err != nil {
						log.Errorf("[WeChat-ClawBot] Login failed: %v", err)
						return
					}
					// Retry start after login
					if err := g.client.Start(ctx); err != nil {
						log.Errorf("[WeChat-ClawBot] Start after login failed: %v", err)
					}
				} else {
					log.Info("[WeChat-ClawBot] Auto-login disabled. Use /login command to login manually")
				}
				return
			}
			log.Errorf("[WeChat-ClawBot] Start failed: %v", err)
		}
	}()

	log.Info("[WeChat-ClawBot] Connection initiated")
	return nil
}

// reconnect attempts to reconnect after a disconnect
func (g *WeChatClawGateway) reconnect() {
	g.mu.Lock()
	if g.reconnectCount >= g.config.MaxReconnectAttempts {
		g.mu.Unlock()
		log.Errorf("[WeChat-ClawBot] Max reconnection attempts (%d) reached, giving up", g.config.MaxReconnectAttempts)
		return
	}
	g.reconnectCount++
	count := g.reconnectCount
	g.mu.Unlock()

	delay := g.config.ReconnectDelay * time.Duration(count)
	log.Infof("[WeChat-ClawBot] Reconnecting in %v (attempt %d/%d)...", delay, count, g.config.MaxReconnectAttempts)

	time.Sleep(delay)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := g.client.Start(ctx); err != nil {
		if err == clawbot.ErrNotLoggedIn {
			log.Info("[WeChat-ClawBot] Session lost, need to re-login")
			if g.config.AutoLogin {
				loginCtx := context.Background()
				if err := g.login(loginCtx); err != nil {
					log.Errorf("[WeChat-ClawBot] Re-login failed: %v", err)
				} else {
					if err := g.client.Start(loginCtx); err != nil {
						log.Errorf("[WeChat-ClawBot] Start after re-login failed: %v", err)
					} else {
						g.mu.Lock()
						g.reconnectCount = 0
						g.mu.Unlock()
					}
				}
			}
		} else {
			log.Warnf("[WeChat-ClawBot] Reconnect attempt %d failed: %v", count, err)
			// Try again
			go g.reconnect()
		}
	} else {
		g.mu.Lock()
		g.reconnectCount = 0
		g.mu.Unlock()
	}
}

// login performs QR code login
func (g *WeChatClawGateway) login(ctx context.Context) error {
	g.loginMu.Lock()
	defer g.loginMu.Unlock()

	log.Info("[WeChat-ClawBot] Starting QR code login...")

	session, err := g.client.Login(ctx)
	if err != nil {
		return fmt.Errorf("failed to start login: %w", err)
	}

	log.Infof("[WeChat-ClawBot] Scan this QR code with WeChat: %s", session.QRCodeURL())
	log.Info("[WeChat-ClawBot] Waiting for scan...")

	if err := session.Wait(ctx); err != nil {
		return fmt.Errorf("login wait failed: %w", err)
	}

	log.Info("[WeChat-ClawBot] Login successful! Credentials saved.")
	return nil
}

// Disconnect closes the connection
func (g *WeChatClawGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	if g.client != nil {
		g.client.Stop()
	}

	close(g.stopCh)
	g.running = false

	log.Info("[WeChat-ClawBot] Disconnected")
	return nil
}

// IsConnected checks if connected
func (g *WeChatClawGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running && g.client != nil && g.client.State() == clawbot.StateRunning
}

// Send sends a message via WeChat ClawBot
func (g *WeChatClawGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("WeChat ClawBot not connected")
	}

	if resp.ChannelID == "" {
		return fmt.Errorf("ChannelID (WeChat user/group ID) is required")
	}

	return g.client.SendText(ctx, resp.ChannelID, resp.Content)
}

// SendText sends a text message to a specific WeChat user/group
func (g *WeChatClawGateway) SendText(ctx context.Context, to, text string) error {
	if !g.IsConnected() {
		return fmt.Errorf("WeChat ClawBot not connected")
	}
	return g.client.SendText(ctx, to, text)
}

// Receive returns a channel of incoming messages
func (g *WeChatClawGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles slash commands
func (g *WeChatClawGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch cmd {
	case "help":
		return Response{
			Content: "Available commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Show connection status\n" +
				"/login - Start QR code login\n" +
				"/stats - Show message statistics",
		}, nil
	case "ping":
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if g.IsConnected() {
			g.mu.RLock()
			since := time.Since(g.connectedAt)
			g.mu.RUnlock()
			return Response{Content: fmt.Sprintf("WeChat ClawBot is connected and ready! (connected for %v)", since.Round(time.Second))}, nil
		}
		return Response{Content: "WeChat ClawBot is not connected"}, nil
	case "stats":
		g.mu.RLock()
		count := g.msgCount
		lastMsg := g.lastMsgTime
		g.mu.RUnlock()
		msg := fmt.Sprintf("📊 Message Statistics:\n  Total messages: %d\n  Last message: %v", count, lastMsg.Format("15:04:05"))
		return Response{Content: msg}, nil
	case "login":
		ctx := context.Background()
		if err := g.login(ctx); err != nil {
			return Response{Content: fmt.Sprintf("Login failed: %v", err)}, nil
		}
		return Response{Content: "Login successful!"}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns health status
func (g *WeChatClawGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:  "wechat-clawbot",
		Connected: g.IsConnected(),
		Details:   make(map[string]interface{}),
	}

	status.Details["data_dir"] = g.config.DataDir
	status.Details["client_id"] = g.config.ClientID

	g.mu.RLock()
	if !g.connectedAt.IsZero() {
		status.Details["connected_since"] = g.connectedAt.Format(time.RFC3339)
	}
	status.Details["msg_count"] = g.msgCount
	if !g.lastMsgTime.IsZero() {
		status.Details["last_msg_at"] = g.lastMsgTime.Format(time.RFC3339)
	}
	g.mu.RUnlock()

	if g.client != nil {
		status.Details["state"] = g.client.State().String()
		status.Details["has_credentials"] = g.client.HasCredentials()
	}

	if !status.Connected {
		status.Error = "Gateway not connected"
	}

	return status
}

// SetAgentChannel sets the agent message channel for bridging (for Nudge system)
func (g *WeChatClawGateway) SetAgentChannel(ch chan<- Message) {
	g.agentCh = ch
}

// handleIncomingMessage converts ClawBot message to gateway.Message
// and forwards it to both msgCh and agentCh (if configured)
func (g *WeChatClawGateway) handleIncomingMessage(clientID string, msg *clawbot.Message) {
	if msg == nil {
		return
	}

	// Update stats
	g.mu.Lock()
	g.msgCount++
	g.lastMsgTime = time.Now()
	g.mu.Unlock()

	// Extract sender info for ChannelID
	channelID := msg.From
	if channelID == "" {
		channelID = msg.To
	}

	// Determine message type based on ClawBot message fields
	msgType := "text"
	switch {
	case len(msg.Images) > 0:
		msgType = "image"
	case msg.Voice != nil:
		msgType = "voice"
	case len(msg.Files) > 0:
		msgType = "file"
	}

	gatewayMsg := Message{
		ID:        fmt.Sprintf("clawbot_%s_%d", clientID, time.Now().UnixNano()),
		Platform:  "wechat-clawbot",
		ChannelID: channelID,
		UserID:    msg.From,
		Content:   msg.Text,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"client_id": clientID,
			"from":      msg.From,
			"to":        msg.To,
			"msg_type":  msgType,
		},
	}

	// Send to main message channel (for gateway processing)
	select {
	case g.msgCh <- gatewayMsg:
		truncated := msg.Text
		if len(truncated) > 50 {
			truncated = truncated[:50]
		}
		log.Debugf("[WeChat-ClawBot] Message received from %s: %s", msg.From, truncated)
	default:
		log.Warnf("[WeChat-ClawBot] Message channel full, dropping message from %s", msg.From)
	}

	// Also forward to agent channel if configured (for Nudge system / async processing)
	if g.agentCh != nil {
		select {
		case g.agentCh <- gatewayMsg:
			// forwarded to agent
		default:
			// agent channel full, skip (non-blocking)
		}
	}
}

// Login triggers a manual QR code login
func (g *WeChatClawGateway) Login(ctx context.Context) error {
	return g.login(ctx)
}

// Client returns the underlying clawbot client
func (g *WeChatClawGateway) Client() *clawbot.DefaultClient {
	return g.client
}

// SetDataDir sets the data directory for credential storage
func (g *WeChatClawGateway) SetDataDir(dir string) {
	g.config.DataDir = dir
}

// SetAutoLogin enables or disables auto-login
func (g *WeChatClawGateway) SetAutoLogin(auto bool) {
	g.config.AutoLogin = auto
}
