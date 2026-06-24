package gateway

import (
	"context"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/log"
)

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

	// Subclass hooks
	onConnect    func(ctx context.Context) error
	onDisconnect func() error
	onSend       func(ctx context.Context, resp Response) error
}

// NewBasePlatform creates a new base platform
func NewBasePlatform(name string, config map[string]interface{}) *BasePlatform {
	bp := &BasePlatform{
		name:       name,
		accessCtrl: NewAccessControl(config),
		reconnect:  NewReconnectManager(config),
		msgChan:    make(chan Message, 1000),
	}

	// Register with status manager
	GetStatusManager().Register(name)

	// Setup reconnect callbacks
	bp.reconnect.SetOnReconnect(func() error {
		if bp.onConnect != nil {
			return bp.onConnect(bp.ctx)
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
			sm.SetState(name, StateConnected)
			bp.setConnected(true)
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

// Connect establishes connection (subclasses should implement onConnect)
func (bp *BasePlatform) Connect(ctx context.Context) error {
	bp.mu.Lock()
	bp.ctx, bp.cancel = context.WithCancel(ctx)
	ctx = bp.ctx
	bp.mu.Unlock()

	GetStatusManager().SetState(bp.name, StateConnecting)

	if bp.onConnect != nil {
		err := bp.onConnect(ctx)
		if err != nil {
			GetStatusManager().SetError(bp.name, err)
			GetStatusManager().SetState(bp.name, StateDisconnected)
			return err
		}
	}

	bp.setConnected(true)
	GetStatusManager().SetState(bp.name, StateConnected)
	log.Infof("[%s] Connected successfully", bp.name)
	return nil
}

// Disconnect closes the connection
func (bp *BasePlatform) Disconnect() error {
	bp.mu.Lock()
	if bp.cancel != nil {
		bp.cancel()
		bp.cancel = nil
	}
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
