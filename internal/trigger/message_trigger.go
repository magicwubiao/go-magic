package trigger

import (
	"sync"
	"time"
)

// DefaultNudgeThreshold is the number of turns between nudges
// Hermes Agent uses 15 turns as the default
const DefaultNudgeThreshold = 15

// MessageTrigger handles the six-system's first two components:
// 1. User message trigger - starts skill creation flow
// 2. Periodic Nudge mechanism - triggers background review every N turns
type MessageTrigger struct {
	mu             sync.RWMutex
	turnCount      int
	nudgeThreshold int
	nudgeHandlers  []func()      // Functions to call on nudge
	taskStartTime  time.Time
	currentTask    string
}

// NewMessageTrigger creates a new message trigger
func NewMessageTrigger() *MessageTrigger {
	return &MessageTrigger{
		nudgeThreshold: DefaultNudgeThreshold,
		nudgeHandlers:  make([]func(), 0),
	}
}

// OnUserMessage is called when a new user message arrives
// This marks the start of a task and increments turn counter
func (mt *MessageTrigger) OnUserMessage(input string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.turnCount++
	mt.currentTask = input
	mt.taskStartTime = time.Now()

	// Check if we should trigger a nudge
	if mt.turnCount%mt.nudgeThreshold == 0 {
		mt.triggerNudge()
	}
}

// triggerNudge calls all registered nudge handlers asynchronously
// This does NOT block the user conversation
func (mt *MessageTrigger) triggerNudge() {
	for _, handler := range mt.nudgeHandlers {
		go handler()
	}
}

// OnToolCall increments the tool call counter for skill creation
func (mt *MessageTrigger) OnToolCall(toolName string, args map[string]interface{}) {
	// Track tool calls for skill creation pattern detection
}

// OnTaskComplete marks the end of a task, returns duration
func (mt *MessageTrigger) OnTaskComplete() time.Duration {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	duration := time.Since(mt.taskStartTime)
	mt.currentTask = ""
	return duration
}

// RegisterNudgeHandler registers a function to call on nudge
func (mt *MessageTrigger) RegisterNudgeHandler(handler func()) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.nudgeHandlers = append(mt.nudgeHandlers, handler)
}

// GetTurnCount returns the current turn count
func (mt *MessageTrigger) GetTurnCount() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.turnCount
}

// SetNudgeThreshold sets the nudge threshold
func (mt *MessageTrigger) SetNudgeThreshold(threshold int) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if threshold > 0 {
		mt.nudgeThreshold = threshold
	}
}

// Reset resets the turn counter (for new sessions)
func (mt *MessageTrigger) Reset() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.turnCount = 0
	mt.currentTask = ""
}

// GetCurrentTask returns the current task description
func (mt *MessageTrigger) GetCurrentTask() string {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.currentTask
}
