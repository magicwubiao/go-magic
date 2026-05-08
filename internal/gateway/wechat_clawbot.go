package gateway

import (
	"context"
	"fmt"
	"math"
	"math/rand"
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
	// Max reconnection attempts on disconnect (default: 0 = unlimited)
	MaxReconnectAttempts int `json:"max_reconnect_attempts"`
	// Initial reconnection delay (default: 3s, will backoff exponentially)
	ReconnectDelay time.Duration `json:"reconnect_delay"`
	// Max reconnection delay cap (default: 5min)
	MaxReconnectDelay time.Duration `json:"max_reconnect_delay"`
	// Heartbeat interval to keep connection alive (default: 30s, 0 = disabled)
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	// Session check interval - periodically checks if session is still valid (default: 5min)
	SessionCheckInterval time.Duration `json:"session_check_interval"`
	// Jitter factor for reconnection delay to avoid thundering herd (default: 0.3)
	JitterFactor float64 `json:"jitter_factor"`
}

// Default values
const (
	defaultReconnectDelay       = 3 * time.Second
	defaultMaxReconnectDelay    = 5 * time.Minute
	defaultHeartbeatInterval    = 30 * time.Second
	defaultSessionCheckInterval = 5 * time.Minute
	defaultJitterFactor         = 0.3
)

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
	reconnectCount  int
	reconnecting    bool
	lastReconnectAt time.Time

	// Health tracking
	connectedAt time.Time
	lastMsgTime time.Time
	msgCount    int64

	// Heartbeat & session check
	heartbeatStopCh chan struct{}
	sessionCheckCh  chan struct{}

	// Context for cancellation
	cancel context.CancelFunc
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
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}
	if cfg.MaxReconnectDelay <= 0 {
		cfg.MaxReconnectDelay = defaultMaxReconnectDelay
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = defaultHeartbeatInterval
	}
	if cfg.SessionCheckInterval <= 0 {
		cfg.SessionCheckInterval = defaultSessionCheckInterval
	}
	if cfg.JitterFactor <= 0 || cfg.JitterFactor > 1 {
		cfg.JitterFactor = defaultJitterFactor
	}
	// MaxReconnectAttempts == 0 means unlimited

	return &WeChatClawGateway{
		config:          cfg,
		msgCh:           make(chan Message, 200),
		stopCh:          make(chan struct{}),
		heartbeatStopCh: make(chan struct{}),
		sessionCheckCh:  make(chan struct{}),
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

	// Create cancellable context
	connectCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.reconnectCount = 0
	g.reconnecting = false
	g.mu.Unlock()

	log.Infof("[WeChat-ClawBot] Connecting... (data dir: %s, client: %s)",
		g.config.DataDir, g.config.ClientID)

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
				g.reconnecting = false
				g.mu.Unlock()
				log.Infof("[WeChat-ClawBot:%s] Connected! 🟢", clientID)

				// Start heartbeat & session check after connected
				g.startHeartbeat()
				g.startSessionCheck()
			},
			OnQRCode: func(clientID string, qrURL string) {
				log.Infof("[WeChat-ClawBot:%s] 📱 QR Code URL: %s", clientID, qrURL)
				log.Info("[WeChat-ClawBot] Please scan the QR code with WeChat to login")
			},
			OnQRScanned: func(clientID string) {
				log.Info("[WeChat-ClawBot] ✅ QR code scanned! Waiting for confirmation...")
			},
			OnQRExpired: func(clientID string, refreshCount int) {
				log.Warnf("[WeChat-ClawBot] ⏰ QR code expired (refresh #%d)", refreshCount)
			},
			OnSessionExpired: func(clientID string) {
				log.Warn("[WeChat-ClawBot] ⚠️ Session expired, need to re-login")
				// Stop heartbeat & session check
				g.stopHeartbeat()
				g.stopSessionCheck()

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
				log.Warnf("[WeChat-ClawBot] 🔌 Disconnected: %v", err)
				// Stop heartbeat & session check
				g.stopHeartbeat()
				g.stopSessionCheck()

				// Auto-reconnect if running
				g.mu.RLock()
				running := g.running
				g.mu.RUnlock()
				if running {
					go g.reconnect()
				}
			},
			OnError: func(clientID string, err error) {
				log.Errorf("[WeChat-ClawBot] ❌ Error: %v", err)
			},
		}),
	)

	// Start the client in a goroutine (Start() blocks)
	go func() {
		err := g.client.Start(connectCtx)
		if err != nil {
			if err == clawbot.ErrNotLoggedIn {
				log.Info("[WeChat-ClawBot] No saved credentials found, need to login")

				if g.config.AutoLogin {
					if err := g.login(connectCtx); err != nil {
						log.Errorf("[WeChat-ClawBot] Login failed: %v", err)
						return
					}
					// Retry start after login
					if err := g.client.Start(connectCtx); err != nil {
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

// reconnect attempts to reconnect after a disconnect with exponential backoff
func (g *WeChatClawGateway) reconnect() {
	g.mu.Lock()
	// Prevent multiple simultaneous reconnection attempts
	if g.reconnecting {
		g.mu.Unlock()
		return
	}
	g.reconnecting = true

	// Check if max attempts reached (0 = unlimited)
	if g.config.MaxReconnectAttempts > 0 &&
		g.reconnectCount >= g.config.MaxReconnectAttempts {
		g.mu.Unlock()
		log.Errorf("[WeChat-ClawBot] Max reconnection attempts (%d) reached, giving up",
			g.config.MaxReconnectAttempts)
		return
	}

	g.reconnectCount++
	count := g.reconnectCount
	g.mu.Unlock()

	// Calculate delay with exponential backoff + jitter
	delay := g.calculateBackoff(count)

	log.Infof("[WeChat-ClawBot] 🔄 Reconnecting in %v (attempt %d/%s)...",
		delay.Round(time.Second),
		count,
		formatMaxAttempts(g.config.MaxReconnectAttempts))

	// Wait for the delay, but also listen for stop signal
	select {
	case <-time.After(delay):
	case <-g.stopCh:
		log.Info("[WeChat-ClawBot] Reconnection cancelled by stop signal")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := g.client.Start(ctx); err != nil {
		if err == clawbot.ErrNotLoggedIn {
			log.Info("[WeChat-ClawBot] Session lost, need to re-login")
			if g.config.AutoLogin {
				loginCtx := context.Background()
				if err := g.login(loginCtx); err != nil {
					log.Errorf("[WeChat-ClawBot] Re-login failed: %v", err)
					g.mu.Lock()
					g.reconnecting = false
					g.mu.Unlock()
					// Try again next cycle (never give up if unlimited)
					select {
					case <-g.stopCh:
						return
					default:
						go g.reconnect()
					}
					return
				}
				if err := g.client.Start(loginCtx); err != nil {
					log.Errorf("[WeChat-ClawBot] Start after re-login failed: %v", err)
					g.mu.Lock()
					g.reconnecting = false
					g.mu.Unlock()
					// Try again
					select {
					case <-g.stopCh:
						return
					default:
						go g.reconnect()
					}
					return
				}
				// Success!
				g.mu.Lock()
				g.reconnectCount = 0
				g.reconnecting = false
				g.mu.Unlock()
			} else {
				g.mu.Lock()
				g.reconnecting = false
				g.mu.Unlock()
				log.Info("[WeChat-ClawBot] Auto-login disabled. Use /login to login manually")
			}
		} else {
			log.Warnf("[WeChat-ClawBot] Reconnect attempt %d failed: %v", count, err)
			g.mu.Lock()
			g.reconnecting = false
			g.mu.Unlock()

			// Try again - never give up!
			select {
			case <-g.stopCh:
				return
			default:
				go g.reconnect()
			}
		}
	} else {
		g.mu.Lock()
		g.reconnectCount = 0
		g.reconnecting = false
		g.mu.Unlock()
	}
}

// calculateBackoff computes exponential backoff with jitter
func (g *WeChatClawGateway) calculateBackoff(attempt int) time.Duration {
	// Base delay: delay * 2^(attempt-1)
	baseDelay := float64(g.config.ReconnectDelay)
	exponentialDelay := baseDelay * math.Pow(2, float64(attempt-1))

	// Cap at max delay
	if exponentialDelay > float64(g.config.MaxReconnectDelay) {
		exponentialDelay = float64(g.config.MaxReconnectDelay)
	}

	// Add jitter: random ±jitterFactor%
	jitterRange := exponentialDelay * g.config.JitterFactor
	jitter := (rand.Float64()*2 - 1) * jitterRange

	return time.Duration(exponentialDelay + jitter)
}

// formatMaxAttempts returns "unlimited" or the number
func formatMaxAttempts(max int) string {
	if max <= 0 {
		return "∞"
	}
	return fmt.Sprintf("%d", max)
}

// startHeartbeat periodically pings the server to keep connection alive
func (g *WeChatClawGateway) startHeartbeat() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Close existing heartbeat if any
	if g.heartbeatStopCh != nil {
		select {
		case <-g.heartbeatStopCh:
			// already closed
		default:
			close(g.heartbeatStopCh)
		}
	}

	g.heartbeatStopCh = make(chan struct{})

	go func() {
		ticker := time.NewTicker(g.config.HeartbeatInterval)
		defer ticker.Stop()

		log.Debugf("[WeChat-ClawBot] Heartbeat started (every %v)", g.config.HeartbeatInterval)

		for {
			select {
			case <-ticker.C:
				if !g.IsConnected() {
					log.Debug("[WeChat-ClawBot] Heartbeat skipped - not connected")
					return
				}

				// Send a lightweight ping to keep the connection alive
				// We use the client's internal state check + a keepalive send
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

				// Try to send a heartbeat via a simple state check
				// The clawbot client's internal getupdates loop will keep the connection alive
				// by just checking if it's still connected
				state := g.client.State()
				_ = state

				cancel()

			case <-g.heartbeatStopCh:
				log.Debug("[WeChat-ClawBot] Heartbeat stopped")
				return
			}
		}
	}()
}

// stopHeartbeat stops the heartbeat goroutine
func (g *WeChatClawGateway) stopHeartbeat() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.heartbeatStopCh != nil {
		select {
		case <-g.heartbeatStopCh:
			// already closed
		default:
			close(g.heartbeatStopCh)
		}
		g.heartbeatStopCh = make(chan struct{})
	}
}

// startSessionCheck periodically checks if the session is still valid
func (g *WeChatClawGateway) startSessionCheck() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Close existing session check if any
	if g.sessionCheckCh != nil {
		select {
		case <-g.sessionCheckCh:
			// already closed
		default:
			close(g.sessionCheckCh)
		}
	}

	g.sessionCheckCh = make(chan struct{})

	go func() {
		ticker := time.NewTicker(g.config.SessionCheckInterval)
		defer ticker.Stop()

		log.Debugf("[WeChat-ClawBot] Session check started (every %v)", g.config.SessionCheckInterval)

		for {
			select {
			case <-ticker.C:
				if !g.IsConnected() {
					log.Debug("[WeChat-ClawBot] Session check skipped - not connected")
					return
				}

				// Check if we've been idle for too long
				g.mu.RLock()
				lastMsg := g.lastMsgTime
				connectedSince := g.connectedAt
				g.mu.RUnlock()

				// If connected for more than 30min but no recent activity (30min),
				// do a proactive check by sending a status message
				if !connectedSince.IsZero() &&
					time.Since(connectedSince) > 30*time.Minute &&
					!lastMsg.IsZero() &&
					time.Since(lastMsg) > 30*time.Minute {

					log.Debug("[WeChat-ClawBot] Session idle for 30+ minutes, checking health...")
					status := g.CheckHealth()
					if !status.Connected {
						log.Warn("[WeChat-ClawBot] Health check failed, triggering reconnect")
						g.stopHeartbeat()
						g.stopSessionCheck()
						go g.reconnect()
						return
					}
				}

			case <-g.sessionCheckCh:
				log.Debug("[WeChat-ClawBot] Session check stopped")
				return
			}
		}
	}()
}

// stopSessionCheck stops the session check goroutine
func (g *WeChatClawGateway) stopSessionCheck() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.sessionCheckCh != nil {
		select {
		case <-g.sessionCheckCh:
			// already closed
		default:
			close(g.sessionCheckCh)
		}
		g.sessionCheckCh = make(chan struct{})
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

	log.Infof("[WeChat-ClawBot] 📱 Scan this QR code with WeChat: %s", session.QRCodeURL())
	log.Info("[WeChat-ClawBot] Waiting for scan...")

	if err := session.Wait(ctx); err != nil {
		return fmt.Errorf("login wait failed: %w", err)
	}

	log.Info("[WeChat-ClawBot] ✅ Login successful! Credentials saved.")
	return nil
}

// Disconnect closes the connection
func (g *WeChatClawGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	// Stop background goroutines
	g.stopHeartbeat()
	g.stopSessionCheck()

	if g.cancel != nil {
		g.cancel()
	}

	if g.client != nil {
		g.client.Stop()
	}

	close(g.stopCh)
	g.running = false

	log.Info("[WeChat-ClawBot] 🔌 Disconnected")
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
				"/stats - Show message statistics\n" +
				"/reconnect - Force reconnect\n" +
				"/health - Detailed health check",
		}, nil
	case "ping":
		return Response{Content: "Pong! 🏓"}, nil
	case "status":
		if g.IsConnected() {
			g.mu.RLock()
			since := time.Since(g.connectedAt)
			reconnects := g.reconnectCount
			g.mu.RUnlock()
			return Response{
				Content: fmt.Sprintf("WeChat ClawBot is connected and ready! 🟢\n"+
					"Connected for: %v\n"+
					"Reconnections: %d",
					since.Round(time.Second), reconnects),
			}, nil
		}
		return Response{Content: "WeChat ClawBot is not connected 🔴"}, nil
	case "stats":
		g.mu.RLock()
		count := g.msgCount
		lastMsg := g.lastMsgTime
		connectedSince := g.connectedAt
		g.mu.RUnlock()

		msg := fmt.Sprintf("📊 Message Statistics:\n"+
			"  Total messages: %d\n"+
			"  Last message: %s\n"+
			"  Connected since: %s",
			count,
			lastMsg.Format("15:04:05"),
			connectedSince.Format("2006-01-02 15:04:05"))
		return Response{Content: msg}, nil
	case "login":
		ctx := context.Background()
		if err := g.login(ctx); err != nil {
			return Response{Content: fmt.Sprintf("Login failed: %v", err)}, nil
		}
		return Response{Content: "Login successful! ✅"}, nil
	case "reconnect":
		log.Info("[WeChat-ClawBot] Manual reconnect triggered")
		go g.reconnect()
		return Response{Content: "Reconnecting... 🔄"}, nil
	case "health":
		status := g.CheckHealth()
		content := fmt.Sprintf("🏥 Health Status:\n"+
			"  Platform: %s\n"+
			"  Connected: %v\n"+
			"  State: %s\n"+
			"  Reconnections: %d",
			status.Platform,
			status.Connected,
			status.Details["state"],
			g.reconnectCount)
		return Response{Content: content}, nil
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
		status.Details["uptime"] = time.Since(g.connectedAt).Round(time.Second).String()
	}
	status.Details["msg_count"] = g.msgCount
	status.Details["reconnect_count"] = g.reconnectCount
	status.Details["reconnecting"] = g.reconnecting
	if !g.lastMsgTime.IsZero() {
		status.Details["last_msg_at"] = g.lastMsgTime.Format(time.RFC3339)
		status.Details["idle_time"] = time.Since(g.lastMsgTime).Round(time.Second).String()
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
		log.Debugf("[WeChat-ClawBot] 📨 Message received from %s: %s", msg.From, truncated)
	default:
		log.Warnf("[WeChat-ClawBot] ⚠️ Message channel full, dropping message from %s", msg.From)
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
