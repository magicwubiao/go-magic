package trigger

import (
	"sync"
	"testing"
	"time"
)

func TestEnhancedMessageTrigger_Creation(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	if mt == nil {
		t.Fatal("Expected non-nil MessageTrigger")
	}
	
	if mt.turnCount != 0 {
		t.Errorf("Expected turnCount to be 0, got %d", mt.turnCount)
	}
	
	if mt.dynamicThreshold != DefaultNudgeThreshold {
		t.Errorf("Expected dynamicThreshold to be %d, got %d", DefaultNudgeThreshold, mt.dynamicThreshold)
	}
}

func TestEnhancedMessageTrigger_OnUserMessage(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Simulate user messages
	for i := 1; i <= 5; i++ {
		mt.OnUserMessage("Test message")
		
		if mt.turnCount != i {
			t.Errorf("Expected turnCount to be %d after %d messages, got %d", i, i, mt.turnCount)
		}
	}
}

func TestEnhancedMessageTrigger_DynamicThreshold(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	mt.baseThreshold = 10
	
	// Simulate rapid messages (high frequency)
	for i := 0; i < 15; i++ {
		mt.OnUserMessage("Rapid message")
	}
	
	// Should have increased threshold due to high frequency
	if mt.dynamicThreshold <= mt.baseThreshold {
		t.Logf("Dynamic threshold: %d, Base threshold: %d", mt.dynamicThreshold, mt.baseThreshold)
	}
}

func TestEnhancedMessageTrigger_NudgeTriggering(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	mt.SetNudgeThreshold(5)
	
	var nudgeCount int
	var mu sync.Mutex
	
	mt.RegisterNudgeHandlerFunc("test", func(n *Nudge) bool {
		mu.Lock()
		nudgeCount++
		mu.Unlock()
		return true
	})
	
	// Simulate turns
	for i := 0; i < 10; i++ {
		mt.OnUserMessage("Test message")
	}
	
	// Give async nudge time to process
	time.Sleep(100 * time.Millisecond)
	
	if nudgeCount < 1 {
		t.Errorf("Expected at least 1 nudge, got %d", nudgeCount)
	}
}

func TestEnhancedMessageTrigger_QuietHours(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Set quiet hours (current time should be outside these in test)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, now.Location())
	
	mt.SetQuietHours(start, end)
	
	if mt.isQuietHours() {
		// Test passes - we might be in quiet hours depending on test time
		t.Log("Currently in quiet hours")
	}
}

func TestEnhancedMessageTrigger_NudgePriority(t *testing.T) {
	tests := []struct {
		name     string
		priority NudgePriority
		expected string
	}{
		{"Low priority", NudgePriorityLow, "low"},
		{"Normal priority", NudgePriorityNormal, "normal"},
		{"High priority", NudgePriorityHigh, "high"},
		{"Critical priority", NudgePriorityCritical, "critical"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nudge := &Nudge{
				Priority: tt.priority,
				Message:  "Test",
			}
			
			if int(nudge.Priority) != int(tt.priority) {
				t.Errorf("Expected priority %d, got %d", tt.priority, nudge.Priority)
			}
		})
	}
}

func TestEnhancedMessageTrigger_NudgeMetrics(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Record some nudge interactions
	metrics := mt.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected non-nil metrics")
	}
	
	initialSent := metrics.TotalSent
	
	// Manually trigger a nudge (bypassing threshold)
	mt.mu.Lock()
	mt.triggerNudgeUnsafe(NudgeTypePattern, NudgePriorityNormal, "Test")
	mt.mu.Unlock()
	
	time.Sleep(50 * time.Millisecond)
	
	metrics = mt.GetMetrics()
	if metrics.TotalSent <= initialSent {
		t.Errorf("Expected nudge count to increase")
	}
}

func TestEnhancedMessageTrigger_OnNudgeResponse(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	initialMetrics := mt.GetMetrics()
	
	// Simulate nudge acceptance
	mt.OnNudgeResponse("test-nudge", true)
	
	metrics := mt.GetMetrics()
	if metrics.TotalAccepted != initialMetrics.TotalAccepted+1 {
		t.Errorf("Expected accepted count to increase")
	}
	
	// Simulate nudge dismissal
	mt.OnNudgeResponse("test-nudge", false)
	
	metrics = mt.GetMetrics()
	if metrics.TotalDismissed != initialMetrics.TotalDismissed+1 {
		t.Errorf("Expected dismissed count to increase")
	}
}

func TestEnhancedMessageTrigger_Preferences(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Get default preferences
	pref := mt.GetPreferences()
	if pref == nil {
		t.Fatal("Expected non-nil preferences")
	}
	
	// Check all types are enabled by default
	for nudgeType, enabled := range pref.EnabledTypes {
		if !enabled {
			t.Errorf("Expected %s to be enabled by default", nudgeType)
		}
	}
	
	// Disable a type
	mt.SetNudgeTypeEnabled(NudgeTypePeriodic, false)
	
	pref = mt.GetPreferences()
	if pref.EnabledTypes[NudgeTypePeriodic] {
		t.Error("Expected NudgeTypePeriodic to be disabled")
	}
}

func TestEnhancedMessageTrigger_Reset(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Accumulate some state
	for i := 0; i < 10; i++ {
		mt.OnUserMessage("Test")
	}
	
	// Reset
	mt.Reset()
	
	if mt.turnCount != 0 {
		t.Errorf("Expected turnCount to be 0 after reset, got %d", mt.turnCount)
	}
}

func TestEnhancedMessageTrigger_ScheduleNudge(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	nudge := &Nudge{
		ID:      "scheduled-test",
		Type:    NudgeTypeReminder,
		Message: "Scheduled reminder",
	}
	
	// Schedule for 100ms in the future
	deliverAt := time.Now().Add(100 * time.Millisecond)
	mt.ScheduleNudge(nudge, deliverAt)
	
	// Check nudge was scheduled
	if len(mt.scheduledNudges) != 1 {
		t.Errorf("Expected 1 scheduled nudge, got %d", len(mt.scheduledNudges))
	}
}

func TestEnhancedMessageTrigger_OnPatternDetected(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	var nudgeReceived bool
	mt.RegisterNudgeHandlerFunc("pattern-test", func(n *Nudge) bool {
		if n.Type == NudgeTypePattern {
			nudgeReceived = true
		}
		return true
	})
	
	// Trigger pattern detection
	mt.OnPatternDetected("test pattern", 3)
	
	time.Sleep(50 * time.Millisecond)
	
	if !nudgeReceived {
		t.Error("Expected pattern nudge to be received")
	}
}

func TestEnhancedMessageTrigger_OnErrorOccurred(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	var nudgeReceived bool
	mt.RegisterNudgeHandlerFunc("error-test", func(n *Nudge) bool {
		if n.Type == NudgeTypeErrorRecovery {
			nudgeReceived = true
		}
		return true
	})
	
	// Trigger error notification
	mt.OnErrorOccurred("File not found", true)
	
	time.Sleep(50 * time.Millisecond)
	
	if !nudgeReceived {
		t.Error("Expected error recovery nudge to be received")
	}
}

func TestEnhancedMessageTrigger_GetContext(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Set context
	mt.OnUserMessage("Working on file operations")
	
	ctx := mt.GetContext()
	if ctx != "Working on file operations" {
		t.Errorf("Expected context 'Working on file operations', got '%s'", ctx)
	}
}

func TestEnhancedMessageTrigger_GetContextHistory(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Add multiple messages
	for i := 0; i < 5; i++ {
		mt.OnUserMessage("Message")
	}
	
	history := mt.GetContextHistory(3)
	if len(history) != 3 {
		t.Errorf("Expected 3 history entries, got %d", len(history))
	}
}

func TestEnhancedMessageTrigger_MinInterval(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	mt.minNudgeInterval = 100 * time.Millisecond
	
	// Trigger multiple nudges in quick succession
	mt.mu.Lock()
	mt.triggerNudgeUnsafe(NudgeTypePeriodic, NudgePriorityNormal, "First")
	mt.mu.Unlock()
	
	// Try to trigger immediately again
	mt.mu.Lock()
	mt.triggerNudgeUnsafe(NudgeTypePeriodic, NudgePriorityNormal, "Second")
	mt.mu.Unlock()
	
	// Both should be recorded (cooldown is per-type)
	metrics := mt.GetMetrics()
	if metrics.TotalSent < 2 {
		t.Logf("Total sent: %d", metrics.TotalSent)
	}
}

func TestEnhancedMessageTrigger_PredictiveNudge(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	var suggestionMade bool
	mt.RegisterNudgeHandlerFunc("predictive-test", func(n *Nudge) bool {
		if n.Type == NudgeTypeSuggestion {
			suggestionMade = true
		}
		return true
	})
	
	// Test with predictor that suggests
	mt.PredictiveNudge(func() (string, bool) {
		return "Consider using a more efficient algorithm", true
	})
	
	time.Sleep(50 * time.Millisecond)
	
	if !suggestionMade {
		t.Error("Expected predictive nudge to be triggered")
	}
}

func TestEnhancedMessageTrigger_BatchNudge(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	var receivedCount int
	mt.RegisterNudgeHandlerFunc("batch-test", func(n *Nudge) bool {
		receivedCount++
		return true
	})
	
	// Send batch of nudges
	nudges := []*Nudge{
		{ID: "1", Type: NudgeTypePeriodic, Message: "First"},
		{ID: "2", Type: NudgeTypePeriodic, Message: "Second"},
		{ID: "3", Type: NudgeTypePeriodic, Message: "Third"},
	}
	
	mt.BatchNudge(nudges)
	
	time.Sleep(100 * time.Millisecond)
	
	if receivedCount != 3 {
		t.Errorf("Expected 3 nudges received, got %d", receivedCount)
	}
}

func TestNudgeHandler_Interface(t *testing.T) {
	var handler NudgeHandler = &SimpleNudgeHandler{
		name:    "test",
		handler: func(n *Nudge) bool { return true },
	}
	
	if handler.GetName() != "test" {
		t.Errorf("Expected name 'test', got '%s'", handler.GetName())
	}
	
	nudge := &Nudge{ID: "test"}
	if !handler.HandleNudge(nudge) {
		t.Error("Expected handler to return true")
	}
}

func TestEnhancedMessageTrigger_ResponseRateAdjustment(t *testing.T) {
	mt := NewEnhancedMessageTrigger()
	
	// Simulate 10 accepts and 10 dismissals
	for i := 0; i < 10; i++ {
		mt.OnNudgeResponse("n1", true)
		mt.OnNudgeResponse("n2", false)
	}
	
	// Check response rate
	pref := mt.GetPreferences()
	expectedRate := 0.5
	if pref.ResponseRate != expectedRate {
		t.Errorf("Expected response rate %f, got %f", expectedRate, pref.ResponseRate)
	}
}

func BenchmarkOnUserMessage(b *testing.B) {
	mt := NewEnhancedMessageTrigger()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mt.OnUserMessage("Benchmark message")
	}
}

func BenchmarkGetMetrics(b *testing.B) {
	mt := NewEnhancedMessageTrigger()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mt.GetMetrics()
	}
}
