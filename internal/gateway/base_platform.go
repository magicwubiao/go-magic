package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// defaultConnectConfirmTimeout is how long Connect() waits (via a background
// watchdog) for a platform to confirm real connectivity before falling back to
// a truthful "disconnected" state. Override per platform with the
// "connect_timeout_ms" config key.
const defaultConnectConfirmTimeout = 15 * time.Second

// BasePlatform provides common functionality for all platform adapters
type BasePlatform struct {
	mu sync.RWMutex

	name         string
	accessCtrl   *AccessControl
	reconnect    *ReconnectManager
	msgChan      chan Message
	connected    bool
	ctx          context.Context
	cancel       context.CancelFunc
	callbackPort int
	// Channel-level filtering (allowed/blocked channel IDs)
	allowedChannels []string
	blockedChannels []string

	// Connect-attempt confirmation. A platform's onConnect is responsible for
	// reporting whether a real link exists by calling markConnected() /
	// markDisconnected(); Connect() never flips to connected on its own — it
	// used to do so right after onConnect returned, which reported platforms
	// as connected before any dial/handshake had actually succeeded.
	// See Connect() for details.
	connecting     bool          // a Connect() attempt is in flight (guards duplicate onConnect)
	confirmCh      chan struct{} // closed once the current attempt is confirmed
	confirmClosed  bool
	connectTimeout time.Duration

	// connectMu serializes Connect() attempts (Connect itself takes bp.mu only
	// for short critical sections; dialing must not happen under bp.mu).
	connectMu sync.Mutex

	// Subclass hooks
	onConnect    func(ctx context.Context) error
	onDisconnect func() error
	onSend       func(ctx context.Context, resp Response) error
}

// NewBasePlatform creates a new base platform
func NewBasePlatform(name string, config map[string]interface{}) *BasePlatform {
	bp := &BasePlatform{
		name:           name,
		accessCtrl:     NewAccessControl(config),
		reconnect:      NewReconnectManager(config),
		msgChan:        make(chan Message, 1000),
		connectTimeout: defaultConnectConfirmTimeout,
	}

	if v, ok := config["connect_timeout_ms"].(int); ok && v > 0 {
		bp.connectTimeout = time.Duration(v) * time.Millisecond
	}

	// Register with status manager
	GetStatusManager().Register(name)

	// Setup reconnect callbacks
	bp.reconnect.SetOnReconnect(func() error {
		if bp.onConnect != nil {
			return bp.onConnect(bp.liveConnectCtx())
		}
		return nil
	})

	bp.reconnect.SetOnStatusChange(func(status string, err error) {
		sm := GetStatusManager()
		switch status {
		case "disconnected":
			sm.SetState(name, StateDisconnected)
			if err != nil {
				sm.SetError(name, err)
			}
		case "connecting":
			sm.SetState(name, StateConnecting)
		case "connected":
			// A reconnect succeeded — flip to connected only through the same
			// confirmation primitive used at initial connect.
			bp.markConnected()
		case "reconnecting":
			sm.SetState(name, StateReconnecting)
			sm.SetRetryCount(name, bp.reconnect.RetryCount())
		case "fatal_error":
			sm.SetState(name, StateFatalError)
			if err != nil {
				sm.SetError(name, err)
			}
		case "max_retries_exceeded":
			sm.SetState(name, StateMaxRetries)
			if err != nil {
				sm.SetError(name, err)
			}
		}
	})

	return bp
}

// Name returns the platform name
func (bp *BasePlatform) Name() string {
	return bp.name
}

// Connect starts a connection attempt.
//
// onConnect is invoked synchronously. Platforms whose handshake completes
// inside onConnect (QQ/Discord WS dial, Slack RTM, token validation for
// Feishu/DingTalk/LINE, Telegram getMe, Matrix first sync) call markConnected()
// before returning; async-dial platforms (WeCom AI Bot, WeChat iLink) call it
// from their run loop once a real link is confirmed. Connect returns as soon as
// onConnect returns — it does NOT claim "connected" on its own, precisely
// because that used to report platforms as connected when no link existed
// (bad token, unreachable endpoint, unconfigured credentials).
//
// A background watchdog reverts the platform to disconnected if no
// markConnected()/markDisconnected() arrives within connectTimeout, so a
// silently failed dial can never leave the UI stuck on "connecting" forever.
func (bp *BasePlatform) Connect(ctx context.Context) error {
	bp.connectMu.Lock()
	defer bp.connectMu.Unlock()

	bp.mu.Lock()
	if bp.connected {
		bp.mu.Unlock()
		return nil // already connected
	}
	if bp.connecting {
		// A previous attempt is still awaiting confirmation (its onConnect
		// returned without error but nothing has confirmed yet). Join it
		// instead of spawning a duplicate dial/listener.
		bp.mu.Unlock()
		return nil
	}
	bp.ctx, bp.cancel = context.WithCancel(ctx)
	actx := bp.ctx
	bp.connecting = true
	bp.connected = false
	bp.confirmCh = make(chan struct{})
	bp.confirmClosed = false
	bp.mu.Unlock()

	GetStatusManager().SetState(bp.name, StateConnecting)

	// Platforms without an onConnect hook have nothing to link; treat them as
	// trivially connected (legacy/test placeholders).
	if bp.onConnect == nil {
		bp.markConnected()
		return nil
	}

	if err := bp.onConnect(actx); err != nil {
		bp.markDisconnected(err)
		return err
	}

	// Watchdog: if the platform never confirms real connectivity, fall back to
	// a truthful disconnected state instead of staying "connecting" forever.
	bp.mu.RLock()
	confirmCh := bp.confirmCh
	bp.mu.RUnlock()
	go bp.watchConnectConfirm(actx, confirmCh)
	return nil
}

// watchConnectConfirm waits for the connect attempt to settle. It exits as soon
// as the platform confirms (markConnected/markDisconnected) or the attempt
// context is canceled; on timeout it marks the platform disconnected.
func (bp *BasePlatform) watchConnectConfirm(ctx context.Context, confirmCh chan struct{}) {
	select {
	case <-confirmCh:
		// Platform reported its outcome (success or failure) — nothing to do.
	case <-ctx.Done():
		// Canceled (e.g. Disconnect) before any confirmation.
		bp.mu.RLock()
		connected := bp.connected
		bp.mu.RUnlock()
		if !connected {
			GetStatusManager().SetState(bp.name, StateDisconnected)
		}
	case <-time.After(bp.connectTimeout):
		bp.mu.RLock()
		connected := bp.connected
		bp.mu.RUnlock()
		if !connected {
			bp.markDisconnected(fmt.Errorf("connection not confirmed within %v", bp.connectTimeout))
		}
	}
}

// markConnected records that a real connection has been established. It is the
// only place a platform flips to StateConnected — call it from onConnect after
// a synchronous handshake, or from an async run loop once the dial /
// subscribe-ack / first successful poll confirms the link. Safe to call
// repeatedly (e.g. after every successful reconnect).
func (bp *BasePlatform) markConnected() {
	bp.mu.Lock()
	bp.connected = true
	bp.connecting = false
	bp.closeConfirmLocked()
	bp.mu.Unlock()
	GetStatusManager().SetState(bp.name, StateConnected)
	log.Infof("[%s] Connected successfully", bp.name)
}

// markDisconnected reports that a connection attempt failed or was abandoned
// before a real link existed. It sets StateDisconnected and records err (when
// non-nil) so the UI shows why the platform is not connected.
func (bp *BasePlatform) markDisconnected(err error) {
	bp.mu.Lock()
	bp.connected = false
	bp.connecting = false
	bp.closeConfirmLocked()
	bp.mu.Unlock()
	GetStatusManager().SetState(bp.name, StateDisconnected)
	if err != nil {
		GetStatusManager().SetError(bp.name, err)
		log.Warnf("[%s] Not connected: %v", bp.name, err)
	}
}

// closeConfirmLocked closes the confirmation channel of the current connect
// attempt. Callers must hold bp.mu.
func (bp *BasePlatform) closeConfirmLocked() {
	if !bp.confirmClosed && bp.confirmCh != nil {
		close(bp.confirmCh)
		bp.confirmClosed = true
	}
}

// Disconnect closes the connection
func (bp *BasePlatform) Disconnect() error {
	bp.mu.Lock()
	if bp.cancel != nil {
		bp.cancel()
		bp.cancel = nil
	}
	bp.connecting = false
	bp.closeConfirmLocked()
	bp.mu.Unlock()

	bp.reconnect.Stop()

	if bp.onDisconnect != nil {
		if err := bp.onDisconnect(); err != nil {
			log.Warnf("[%s] Disconnect error: %v", bp.name, err)
		}
	}

	bp.setConnected(false)
	GetStatusManager().SetState(bp.name, StateDisconnected)
	log.Infof("[%s] Disconnected", bp.name)
	return nil
}

// liveConnectCtx returns a usable context for (re)connecting a platform.
//
// When a platform drops at runtime it goes through HandleDisconnection ->
// ReconnectManager, whose onReconnect callback must re-run onConnect with a
// context that is still alive. The ctx captured in Connect() (bp.ctx) may
// already have been canceled — e.g. Disconnect() canceled it, or the parent
// context (a prior request/gateway ctx) finished. Passing a canceled context
// into onConnect makes session/websocket setup fail ("failed to
// create session store: ... context canceled"), so the platform can never
// recover on its own and the user is forced to re-pair. Prefer the current
// bp.ctx when it is still alive; otherwise fall back to a fresh
// context.Background() so onConnect always sees a live context.
func (bp *BasePlatform) liveConnectCtx() context.Context {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	if bp.ctx != nil && bp.ctx.Err() == nil {
		return bp.ctx
	}
	return context.Background()
}

// Send sends a message
func (bp *BasePlatform) Send(ctx context.Context, resp Response) error {
	if bp.onSend != nil {
		err := bp.onSend(ctx, resp)
		if err == nil {
			GetStatusManager().IncrementSent(bp.name)
		}
		return err
	}
	return nil
}

// Receive returns the message channel
func (bp *BasePlatform) Receive() <-chan Message {
	return bp.msgChan
}

// EmitMessage sends a message to the receive channel
func (bp *BasePlatform) EmitMessage(msg Message) {
	msg.Platform = bp.name

	if !bp.checkAccessControl(msg) {
		return
	}

	select {
	case bp.msgChan <- msg:
		GetStatusManager().IncrementRecv(bp.name)
	default:
		log.Warnf("[%s] Message channel full, dropping message", bp.name)
	}
}

func (bp *BasePlatform) checkAccessControl(msg Message) bool {
	if bp.accessCtrl == nil {
		return true
	}

	if msg.IsGroup {
		return bp.accessCtrl.CheckGroup(msg.ChannelID, msg.IsMentioned)
	}
	return bp.accessCtrl.CheckDM(msg.UserID)
}

// ShouldAcceptMessage reports whether msg passes this platform's access control
// (policies + allow/block lists) — the same check EmitMessage applies before
// enqueueing. Gateways that act on a message *before* EmitMessage (e.g. WeCom
// streaming placeholder claims) must call this first, otherwise messages that
// will never be delivered can still trigger side effects.
func (bp *BasePlatform) ShouldAcceptMessage(msg Message) bool {
	return bp.checkAccessControl(msg)
}

// IsConnected returns whether the platform is connected
func (bp *BasePlatform) IsConnected() bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.connected
}

func (bp *BasePlatform) setConnected(val bool) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.connected = val
}

// CheckHealth returns detailed health status
func (bp *BasePlatform) CheckHealth() *HealthStatus {
	status := GetStatusManager().Get(bp.name)
	return &HealthStatus{
		Connected: status.Connected,
		Error:     status.LastError,
		LatencyMs: status.LatencyMs,
		Details: map[string]interface{}{
			"user_id":        status.UserID,
			"username":       status.Username,
			"uptime_seconds": status.UptimeSeconds,
		},
	}
}

// HandleSlashCommand handles a slash command (default: no-op)
func (bp *BasePlatform) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	return Response{MessageID: msg.ID}, nil
}

// AccessControl returns the access control
func (bp *BasePlatform) AccessControl() *AccessControl {
	return bp.accessCtrl
}

// ReconnectManager returns the reconnect manager
func (bp *BasePlatform) ReconnectManager() *ReconnectManager {
	return bp.reconnect
}

// HandleDisconnection handles a disconnection event
func (bp *BasePlatform) HandleDisconnection(err error) {
	bp.setConnected(false)
	bp.reconnect.HandleDisconnection(err)
}

// SetUserInfo sets user information
func (bp *BasePlatform) SetUserInfo(userID, username string) {
	GetStatusManager().SetUserInfo(bp.name, userID, username)
}

// SetLatency sets the latency
func (bp *BasePlatform) SetLatency(latencyMs int64) {
	GetStatusManager().SetLatency(bp.name, latencyMs)
}

// SetExtra sets an extra status field
func (bp *BasePlatform) SetExtra(key string, value interface{}) {
	GetStatusManager().SetExtra(bp.name, key, value)
}

// SetChannelFilter sets the allowed and blocked channel lists
func (bp *BasePlatform) SetChannelFilter(allowed, blocked []string) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.allowedChannels = allowed
	bp.blockedChannels = blocked
}

// GetChannelFilter returns the allowed and blocked channel lists
func (bp *BasePlatform) GetChannelFilter() ([]string, []string) {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.allowedChannels, bp.blockedChannels
}

// SetCallbackPort sets the callback port for webhook-based platforms
func (bp *BasePlatform) SetCallbackPort(port int) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.callbackPort = port
}

// GetCallbackPort returns the callback port
func (bp *BasePlatform) GetCallbackPort() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.callbackPort
}

// ShouldProcessChannel checks if a channel should be processed based on filter
func (bp *BasePlatform) ShouldProcessChannel(channelID string) bool {
	bp.mu.RLock()
	allowed := bp.allowedChannels
	blocked := bp.blockedChannels
	bp.mu.RUnlock()

	// Blocked channels take precedence
	for _, b := range blocked {
		if b == channelID {
			return false
		}
	}

	// If allowed list is empty, allow all; otherwise only allow listed
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == channelID {
			return true
		}
	}
	return false
}
