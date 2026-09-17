package trigger

import (
	"sync"
	"time"
)

// DefaultNudgeThreshold is the number of turns between nudges
// Agent uses 15 turns as the default
const DefaultNudgeThreshold = 15

// MessageTrigger handles the six-system's first two components:
// 1. User message trigger - starts skill creation flow
// 2. Periodic Nudge mechanism - triggers background review every N turns
//
// Nil-safety: a nil *MessageTrigger is a valid "disabled" state, and every
// method tolerates it. cortex.NewManager returns early when the Cortex system
// is disabled, leaving Manager.Trigger nil while Manager itself stays non-nil —
// so call sites that only check `cortexManager != nil` still reach these
// methods with a nil receiver. Dereferencing mt.mu there panicked the whole
// turn with a nil pointer dereference the moment a tool was called. Guarding
// here (rather than at each call site) keeps that from recurring as new call
// sites appear.
type MessageTrigger struct {
	mu             sync.RWMutex
	turnCount      int
	nudgeThreshold int
	nudgeHandlers  []func() // Functions to call on nudge
	taskStartTime  time.Time
	currentTask    string

	// Tool call tracking for skill auto-creation
	currentToolCalls []string // Ordered tool names in current task
	toolCallCount    int      // Total tool calls in current task
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
	if mt == nil {
		return
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.turnCount++
	mt.currentTask = input
	mt.taskStartTime = time.Now()

	// Check if we should trigger a nudge
	if mt.nudgeThreshold > 0 && mt.turnCount%mt.nudgeThreshold == 0 {
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

// OnToolCall records a tool call for skill creation pattern detection.
// This feeds into System 6 (Skill Evolution) to detect repeated tool sequences.
func (mt *MessageTrigger) OnToolCall(toolName string, args map[string]interface{}) {
	if mt == nil {
		return
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.toolCallCount++
	mt.currentToolCalls = append(mt.currentToolCalls, toolName)
}

// OnTaskComplete marks the end of a task, returns duration and resets tool tracking
func (mt *MessageTrigger) OnTaskComplete() time.Duration {
	if mt == nil {
		return 0
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// taskStartTime is zero unless OnUserMessage ran. time.Since(zeroTime)
	// overflows int64 nanoseconds and comes back as the maximum Duration
	// (~292 years), which would be reported as a task that ran forever.
	// A zero start means "no task was started", so report no duration.
	var duration time.Duration
	if !mt.taskStartTime.IsZero() {
		duration = time.Since(mt.taskStartTime)
	}
	mt.currentTask = ""
	mt.currentToolCalls = nil
	mt.toolCallCount = 0
	return duration
}

// RegisterNudgeHandler registers a function to call on nudge
func (mt *MessageTrigger) RegisterNudgeHandler(handler func()) {
	if mt == nil {
		return
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.nudgeHandlers = append(mt.nudgeHandlers, handler)
}

// GetTurnCount returns the current turn count
func (mt *MessageTrigger) GetTurnCount() int {
	if mt == nil {
		return 0
	}
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.turnCount
}

// SetNudgeThreshold sets the nudge threshold
func (mt *MessageTrigger) SetNudgeThreshold(threshold int) {
	if mt == nil {
		return
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if threshold > 0 {
		mt.nudgeThreshold = threshold
	}
}

// Reset resets the turn counter and tool tracking for new sessions
func (mt *MessageTrigger) Reset() {
	if mt == nil {
		return
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.turnCount = 0
	mt.currentTask = ""
	mt.currentToolCalls = nil
	mt.toolCallCount = 0
}

// GetCurrentTask returns the current task description
func (mt *MessageTrigger) GetCurrentTask() string {
	if mt == nil {
		return ""
	}
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.currentTask
}

// GetToolCalls returns the ordered tool call names for the current task.
// Returns a copy to avoid data races.
func (mt *MessageTrigger) GetToolCalls() []string {
	if mt == nil {
		return nil
	}
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	if mt.currentToolCalls == nil {
		return nil
	}
	result := make([]string, len(mt.currentToolCalls))
	copy(result, mt.currentToolCalls)
	return result
}

// GetToolCallCount returns the total number of tool calls in current task
func (mt *MessageTrigger) GetToolCallCount() int {
	if mt == nil {
		return 0
	}
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.toolCallCount
}
