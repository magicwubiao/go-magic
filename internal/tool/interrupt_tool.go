package tool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InterruptTool allows the agent to interrupt its own execution
// This is useful for stopping long-running operations, canceling pending tasks,
// or allowing the user to interrupt the agent mid-thought
type InterruptTool struct {
	BaseTool
	mu            sync.RWMutex
	interruptCh   chan struct{}
	isInterrupted bool
	reason        string
	timestamp     time.Time
	callbacks     []func(reason string)
}

// InterruptRequest represents a request to interrupt execution
type InterruptRequest struct {
	Reason string `json:"reason"` // Why the interruption is happening
	Force  bool   `json:"force"`  // If true, force immediate interruption
}

// InterruptResult represents the result of an interrupt operation
type InterruptResult struct {
	Success       bool      `json:"success"`
	WasInterrupted bool     `json:"was_interrupted"`
	Reason        string    `json:"reason,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	Message       string    `json:"message"`
}

// InterruptStatus represents the current interrupt status
type InterruptStatus struct {
	IsInterrupted bool      `json:"is_interrupted"`
	Reason        string    `json:"reason,omitempty"`
	Timestamp     time.Time `json:"timestamp,omitempty"`
}

// NewInterruptTool creates a new interrupt tool
func NewInterruptTool() *InterruptTool {
	return &InterruptTool{
		BaseTool: *NewBaseTool(
			"interrupt",
			"Interrupt the agent's current execution. Use this when you need to stop a long-running operation, cancel a task, or respond to a user interruption request. The agent can also use this to interrupt itself when it detects it should stop (e.g., user said 'stop', task completed early, error condition detected).",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Reason for the interruption (e.g., 'user requested stop', 'task completed', 'error detected')",
					},
					"force": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, force immediate interruption without cleanup",
						"default":     false,
					},
				},
				"required": []string{"reason"},
			},
		),
		interruptCh: make(chan struct{}, 1),
		callbacks:   make([]func(reason string), 0),
	}
}

// Execute triggers an interrupt
func (t *InterruptTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	reason, _ := params["reason"].(string)
	force := false
	if v, ok := params["force"].(bool); ok {
		force = v
	}

	if reason == "" {
		reason = "interruption requested"
	}

	result := &InterruptResult{
		Reason:    reason,
		Timestamp: time.Now(),
	}

	t.mu.Lock()
	wasInterrupted := t.isInterrupted
	t.isInterrupted = true
	t.reason = reason
	t.timestamp = result.Timestamp
	
	// Notify callbacks
	for _, cb := range t.callbacks {
		go cb(reason)
	}
	
	// Signal interrupt channel (non-blocking)
	select {
	case t.interruptCh <- struct{}{}:
	default:
	}
	t.mu.Unlock()

	result.Success = true
	result.WasInterrupted = wasInterrupted
	
	if force {
		result.Message = fmt.Sprintf("Force interrupted: %s", reason)
	} else {
		result.Message = fmt.Sprintf("Interrupted gracefully: %s", reason)
	}

	return result, nil
}

// IsInterrupted returns true if an interrupt has been triggered
func (t *InterruptTool) IsInterrupted() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isInterrupted
}

// GetStatus returns the current interrupt status
func (t *InterruptTool) GetStatus() *InterruptStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return &InterruptStatus{
		IsInterrupted: t.isInterrupted,
		Reason:        t.reason,
		Timestamp:     t.timestamp,
	}
}

// GetInterruptChannel returns a channel that receives a signal when interrupted
func (t *InterruptTool) GetInterruptChannel() <-chan struct{} {
	return t.interruptCh
}

// Reset clears the interrupt state
func (t *InterruptTool) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.isInterrupted = false
	t.reason = ""
	t.timestamp = time.Time{}
	// Drain the channel
	select {
	case <-t.interruptCh:
	default:
	}
}

// OnInterrupt registers a callback to be called when interrupted
func (t *InterruptTool) OnInterrupt(callback func(reason string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callbacks = append(t.callbacks, callback)
}

// CheckAndClear checks if interrupted and clears the state atomically
// Returns true if was interrupted
func (t *InterruptTool) CheckAndClear() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasInterrupted := t.isInterrupted
	if wasInterrupted {
		t.isInterrupted = false
		t.reason = ""
		t.timestamp = time.Time{}
		select {
		case <-t.interruptCh:
		default:
		}
	}
	return wasInterrupted
}

// WaitForInterrupt blocks until an interrupt is received or timeout
func (t *InterruptTool) WaitForInterrupt(timeout time.Duration) bool {
	select {
	case <-t.interruptCh:
		return true
	case <-time.After(timeout):
		return false
	}
}
