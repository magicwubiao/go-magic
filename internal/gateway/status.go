package gateway

import (
	"sync"
	"time"
)

// ConnectionState represents the connection state of a platform
type ConnectionState string

const (
	StateDisconnected ConnectionState = "disconnected"
	StateConnecting   ConnectionState = "connecting"
	StateConnected    ConnectionState = "connected"
	StateReconnecting ConnectionState = "reconnecting"
	StateFatalError   ConnectionState = "fatal_error"
	StateMaxRetries   ConnectionState = "max_retries_exceeded"
)

// RuntimeStatus holds runtime status information for a platform
type RuntimeStatus struct {
	Platform      string                 `json:"platform"`
	State         ConnectionState        `json:"state"`
	Connected     bool                   `json:"connected"`
	LoginTime     *time.Time             `json:"login_time,omitempty"`
	LastError     string                 `json:"last_error,omitempty"`
	LastErrorTime *time.Time             `json:"last_error_time,omitempty"`
	UptimeSeconds int64                  `json:"uptime_seconds"`
	MessagesSent  int64                  `json:"messages_sent"`
	MessagesRecv  int64                  `json:"messages_received"`
	RetryCount    int                    `json:"retry_count"`
	LatencyMs     int64                  `json:"latency_ms"`
	UserID        string                 `json:"user_id,omitempty"`
	Username      string                 `json:"username,omitempty"`
	Extra         map[string]interface{} `json:"extra,omitempty"`
}

// StatusManager manages runtime status for all platforms
type StatusManager struct {
	mu        sync.RWMutex
	statuses  map[string]*RuntimeStatus
	listeners []func(status *RuntimeStatus)
}

var (
	statusManager     *StatusManager
	statusManagerOnce sync.Once
)

// GetStatusManager returns the global status manager
func GetStatusManager() *StatusManager {
	statusManagerOnce.Do(func() {
		statusManager = &StatusManager{
			statuses: make(map[string]*RuntimeStatus),
		}
	})
	return statusManager
}

// Register registers a platform status
func (sm *StatusManager) Register(platform string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.statuses[platform]; !ok {
		sm.statuses[platform] = &RuntimeStatus{
			Platform: platform,
			State:    StateDisconnected,
			Extra:    make(map[string]interface{}),
		}
	}
}

// Unregister removes a platform status
func (sm *StatusManager) Unregister(platform string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.statuses, platform)
}

// SetState sets the connection state for a platform
func (sm *StatusManager) SetState(platform string, state ConnectionState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	status, ok := sm.statuses[platform]
	if !ok {
		status = &RuntimeStatus{
			Platform: platform,
			Extra:    make(map[string]interface{}),
		}
		sm.statuses[platform] = status
	}

	status.State = state
	status.Connected = state == StateConnected

	if state == StateConnected {
		now := time.Now()
		status.LoginTime = &now
		status.RetryCount = 0
	}

	sm.notifyListeners(status)
}

// SetError sets the last error for a platform
func (sm *StatusManager) SetError(platform string, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	status, ok := sm.statuses[platform]
	if !ok {
		return
	}

	if err != nil {
		now := time.Now()
		status.LastError = err.Error()
		status.LastErrorTime = &now
	}
}

// IncrementSent increments the sent message counter
func (sm *StatusManager) IncrementSent(platform string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if status, ok := sm.statuses[platform]; ok {
		status.MessagesSent++
	}
}

// IncrementRecv increments the received message counter
func (sm *StatusManager) IncrementRecv(platform string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if status, ok := sm.statuses[platform]; ok {
		status.MessagesRecv++
	}
}

// SetLatency sets the latency for a platform
func (sm *StatusManager) SetLatency(platform string, latencyMs int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if status, ok := sm.statuses[platform]; ok {
		status.LatencyMs = latencyMs
	}
}

// SetUserInfo sets user information for a platform
func (sm *StatusManager) SetUserInfo(platform string, userID, username string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if status, ok := sm.statuses[platform]; ok {
		status.UserID = userID
		status.Username = username
	}
}

// SetExtra sets an extra field for a platform
func (sm *StatusManager) SetExtra(platform string, key string, value interface{}) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if status, ok := sm.statuses[platform]; ok {
		status.Extra[key] = value
		sm.notifyListeners(status)
	}
}

// SetRetryCount sets the retry count
func (sm *StatusManager) SetRetryCount(platform string, count int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if status, ok := sm.statuses[platform]; ok {
		status.RetryCount = count
	}
}

// Get returns the status for a platform
func (sm *StatusManager) Get(platform string) *RuntimeStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	status, ok := sm.statuses[platform]
	if !ok {
		return &RuntimeStatus{
			Platform: platform,
			State:    StateDisconnected,
			Extra:    make(map[string]interface{}),
		}
	}

	// Calculate uptime
	if status.LoginTime != nil && status.Connected {
		status.UptimeSeconds = int64(time.Since(*status.LoginTime).Seconds())
	}

	return status
}

// GetAll returns statuses for all platforms
func (sm *StatusManager) GetAll() map[string]*RuntimeStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*RuntimeStatus, len(sm.statuses))
	for k, v := range sm.statuses {
		status := *v
		if v.LoginTime != nil && v.Connected {
			status.UptimeSeconds = int64(time.Since(*v.LoginTime).Seconds())
		}
		result[k] = &status
	}
	return result
}

// AddListener adds a status change listener
func (sm *StatusManager) AddListener(fn func(status *RuntimeStatus)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.listeners = append(sm.listeners, fn)
}

// RemoveListener removes a status change listener
func (sm *StatusManager) RemoveListener(fn func(status *RuntimeStatus)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for i, l := range sm.listeners {
		if &l == &fn {
			sm.listeners = append(sm.listeners[:i], sm.listeners[i+1:]...)
			return
		}
	}
}

func (sm *StatusManager) notifyListeners(status *RuntimeStatus) {
	for _, fn := range sm.listeners {
		fn(status)
	}
}
