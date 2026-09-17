package trigger

import (
	"testing"
	"time"
)

// A nil *MessageTrigger is the "Cortex disabled" state.
//
// cortex.NewManager returns early when the Cortex system is disabled, so
// Manager.Trigger stays nil while Manager itself is non-nil. Call sites such as
// agent.go guard only on `a.cortexManager != nil`, so they reach these methods
// with a nil receiver. Before the guards were added, `mt.mu.Lock()` panicked
// with a nil pointer dereference and killed the whole turn the first time a
// tool was invoked.
//
// These tests exist to keep that from coming back: if a future refactor drops a
// guard, the call panics and the test fails loudly instead of only showing up
// as a dead chat turn at runtime.
func TestNilTriggerDoesNotPanic(t *testing.T) {
	var mt *MessageTrigger

	// Every exported method must tolerate the nil receiver.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil receiver panicked: %v", r)
		}
	}()

	mt.OnToolCall("bash", nil)
	mt.OnUserMessage("hi")
	mt.OnTaskComplete()
	mt.RegisterNudgeHandler(func() {})
	mt.SetNudgeThreshold(5)
	mt.Reset()
	_ = mt.GetTurnCount()
	_ = mt.GetCurrentTask()
	_ = mt.GetToolCalls()
	_ = mt.GetToolCallCount()
}

// The nil receiver must behave as a silent no-op, not just avoid panicking.
func TestNilTriggerReturnsZeroValues(t *testing.T) {
	var mt *MessageTrigger

	if got := mt.GetTurnCount(); got != 0 {
		t.Errorf("GetTurnCount = %d, want 0", got)
	}
	if got := mt.GetToolCallCount(); got != 0 {
		t.Errorf("GetToolCallCount = %d, want 0", got)
	}
	if got := mt.GetToolCalls(); got != nil {
		t.Errorf("GetToolCalls = %v, want nil", got)
	}
	if got := mt.GetCurrentTask(); got != "" {
		t.Errorf("GetCurrentTask = %q, want empty", got)
	}
	if got := mt.OnTaskComplete(); got != 0 {
		t.Errorf("OnTaskComplete = %v, want 0", got)
	}
}

// The non-nil path must still work — the guards should not have neutered it.
func TestNonNilTriggerStillTracks(t *testing.T) {
	mt := NewMessageTrigger()

	mt.OnUserMessage("do something")
	mt.OnToolCall("bash", nil)
	mt.OnToolCall("Read", nil)

	if got := mt.GetTurnCount(); got != 1 {
		t.Errorf("GetTurnCount = %d, want 1", got)
	}
	if got := mt.GetToolCallCount(); got != 2 {
		t.Errorf("GetToolCallCount = %d, want 2", got)
	}
	calls := mt.GetToolCalls()
	if len(calls) != 2 || calls[0] != "bash" || calls[1] != "Read" {
		t.Errorf("GetToolCalls = %v, want [bash Read]", calls)
	}
	if got := mt.GetCurrentTask(); got != "do something" {
		t.Errorf("GetCurrentTask = %q", got)
	}

	mt.OnTaskComplete()
	if got := mt.GetToolCallCount(); got != 0 {
		t.Errorf("after OnTaskComplete GetToolCallCount = %d, want 0", got)
	}
	if got := mt.GetToolCalls(); got != nil {
		t.Errorf("after OnTaskComplete GetToolCalls = %v, want nil", got)
	}
}

// A zero-value struct (nudgeThreshold not set) must not divide by zero.
func TestZeroThresholdDoesNotPanic(t *testing.T) {
	mt := &MessageTrigger{} // threshold 0

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("zero threshold panicked: %v", r)
		}
	}()
	mt.OnUserMessage("hi")
}

// OnTaskComplete on a trigger that never saw a message should not underflow.
func TestOnTaskCompleteWithoutStart(t *testing.T) {
	mt := NewMessageTrigger()
	d := mt.OnTaskComplete()
	if d < 0 {
		t.Errorf("duration = %v, want >= 0", d)
	}
	if d > time.Minute {
		t.Errorf("duration = %v, want a small value", d)
	}
}
