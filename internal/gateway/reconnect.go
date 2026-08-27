package gateway

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// ErrorSeverity classifies connection errors
type ErrorSeverity int

const (
	ErrorTransient   ErrorSeverity = iota // 瞬态错误，可重试
	ErrorRecoverable                      // 可恢复错误，延迟重试
	ErrorFatal                            // 致命错误，放弃重试
)

// ReconnectManager handles automatic reconnection with exponential backoff
type ReconnectManager struct {
	mu sync.Mutex

	MaxRetries    int           // 最大重试次数 (-1 为无限)
	InitialDelay  time.Duration // 初始延迟
	MaxDelay      time.Duration // 最大延迟
	BackoffFactor float64       // 退避因子
	Jitter        bool          // 是否添加抖动

	currentDelay   time.Duration
	retryCount     int
	isReconnecting bool
	cancelFunc     context.CancelFunc

	onReconnect    func() error
	onStatusChange func(status string, err error)
}

// DefaultReconnectManager creates a reconnect manager with sensible defaults
func DefaultReconnectManager() *ReconnectManager {
	return &ReconnectManager{
		MaxRetries:    -1,
		InitialDelay:  1 * time.Second,
		MaxDelay:      5 * time.Minute,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// NewReconnectManager creates a reconnect manager from config
func NewReconnectManager(config map[string]interface{}) *ReconnectManager {
	rm := DefaultReconnectManager()

	if v, ok := config["max_retries"].(int); ok {
		rm.MaxRetries = v
	}
	if v, ok := config["initial_delay_ms"].(int); ok {
		rm.InitialDelay = time.Duration(v) * time.Millisecond
	}
	if v, ok := config["max_delay_ms"].(int); ok {
		rm.MaxDelay = time.Duration(v) * time.Millisecond
	}
	if v, ok := config["backoff_factor"].(float64); ok {
		rm.BackoffFactor = v
	}
	if v, ok := config["jitter"].(bool); ok {
		rm.Jitter = v
	}

	return rm
}

// SetOnReconnect sets the reconnection callback
func (rm *ReconnectManager) SetOnReconnect(fn func() error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onReconnect = fn
}

// SetOnStatusChange sets the status change callback
func (rm *ReconnectManager) SetOnStatusChange(fn func(status string, err error)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onStatusChange = fn
}

// ClassifyError classifies an error by severity
func ClassifyError(err error) ErrorSeverity {
	if err == nil {
		return ErrorTransient
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return ErrorTransient
		}
		return ErrorRecoverable
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return ErrorRecoverable
	}

	errStr := err.Error()

	// 瞬态错误模式
	transientPatterns := []string{
		"connection reset by peer",
		"connection timed out",
		"i/o timeout",
		"temporary failure",
		"too many requests",
		"rate limit",
		"eof",
	}

	for _, p := range transientPatterns {
		if containsLower(errStr, p) {
			return ErrorTransient
		}
	}

	// 致命错误模式
	fatalPatterns := []string{
		"invalid token",
		"unauthorized",
		"forbidden",
		"authentication failed",
		"invalid credentials",
		"permission denied",
		"account banned",
	}

	for _, p := range fatalPatterns {
		if containsLower(errStr, p) {
			return ErrorFatal
		}
	}

	return ErrorRecoverable
}

func containsLower(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// HandleDisconnection handles a disconnection and starts reconnection if needed
func (rm *ReconnectManager) HandleDisconnection(err error) {
	severity := ClassifyError(err)
	log.Warnf("[Reconnect] Disconnected. Severity: %d, Error: %v", severity, err)

	// Callbacks may call back into ReconnectManager methods; run them
	// without holding rm.mu to avoid deadlocks.
	if rm.onStatusChange != nil {
		rm.onStatusChange("disconnected", err)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.isReconnecting {
		return
	}

	if severity == ErrorFatal {
		log.Errorf("[Reconnect] Fatal error, giving up: %v", err)
		if rm.onStatusChange != nil {
			rm.onStatusChange("fatal_error", err)
		}
		return
	}

	if rm.MaxRetries >= 0 && rm.retryCount >= rm.MaxRetries {
		log.Errorf("[Reconnect] Max retries (%d) reached, giving up", rm.MaxRetries)
		if rm.onStatusChange != nil {
			rm.onStatusChange("max_retries_exceeded", err)
		}
		return
	}

	rm.isReconnecting = true
	rm.startReconnectLoop()
}

func (rm *ReconnectManager) startReconnectLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	rm.cancelFunc = cancel

	go func() {
		for {
			rm.mu.Lock()
			if rm.currentDelay == 0 {
				rm.currentDelay = rm.InitialDelay
			} else {
				rm.currentDelay = time.Duration(float64(rm.currentDelay) * rm.BackoffFactor)
				if rm.currentDelay > rm.MaxDelay {
					rm.currentDelay = rm.MaxDelay
				}
			}
			delay := rm.currentDelay
			if rm.Jitter {
				delay = addJitter(delay)
			}
			rm.retryCount++
			retryCount := rm.retryCount
			rm.mu.Unlock()

			log.Infof("[Reconnect] Attempt %d after %v", retryCount, delay)

			if rm.onStatusChange != nil {
				rm.onStatusChange("reconnecting", nil)
			}

			select {
			case <-ctx.Done():
				log.Infof("[Reconnect] Reconnect cancelled")
				return
			case <-time.After(delay):
			}

			if rm.onReconnect != nil {
				err := rm.onReconnect()
				if err == nil {
					rm.mu.Lock()
					rm.isReconnecting = false
					rm.currentDelay = 0
					rm.retryCount = 0
					rm.cancelFunc = nil
					rm.mu.Unlock()

					log.Infof("[Reconnect] Reconnected successfully")
					if rm.onStatusChange != nil {
						rm.onStatusChange("connected", nil)
					}
					return
				}

				severity := ClassifyError(err)
				log.Warnf("[Reconnect] Attempt %d failed: %v (severity: %d)", retryCount, err, severity)

				if severity == ErrorFatal {
					rm.mu.Lock()
					rm.isReconnecting = false
					rm.cancelFunc = nil
					rm.mu.Unlock()

					log.Errorf("[Reconnect] Fatal error on reconnect, giving up: %v", err)
					if rm.onStatusChange != nil {
						rm.onStatusChange("fatal_error", err)
					}
					return
				}
			}
		}
	}()
}

// Stop stops the reconnection loop
func (rm *ReconnectManager) Stop() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.cancelFunc != nil {
		rm.cancelFunc()
		rm.cancelFunc = nil
	}
	rm.isReconnecting = false
	rm.currentDelay = 0
	rm.retryCount = 0
}

// Reset resets the reconnection state
func (rm *ReconnectManager) Reset() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.currentDelay = 0
	rm.retryCount = 0
}

// IsReconnecting returns whether reconnection is in progress
func (rm *ReconnectManager) IsReconnecting() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.isReconnecting
}

// RetryCount returns the current retry count
func (rm *ReconnectManager) RetryCount() int {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.retryCount
}

func addJitter(d time.Duration) time.Duration {
	if d == 0 {
		return d
	}
	jitter := time.Duration(int64(float64(d) * 0.1 * (float64(randInt(0, 100))/100.0 - 0.5)))
	return d + jitter
}

func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.IntN(max-min+1)
}
