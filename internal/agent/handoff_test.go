package agent

import (
	"context"
	"testing"
)

func TestNewHandoffManager(t *testing.T) {
	hm := NewHandoffManager()
	if hm == nil {
		t.Fatal("expected non-nil HandoffManager")
	}
	if len(hm.handoffs) != 0 {
		t.Error("expected empty handoffs")
	}
}

func TestExecuteHandoff(t *testing.T) {
	hm := NewHandoffManager()
	req := HandoffRequest{
		TargetModel:     "gpt-4",
		TargetProfile:   "coding",
		Reason:          "switch to coding profile",
	}

	result := hm.ExecuteHandoff(context.Background(), "session-1", "claude-3", "default", req)

	if !result.Success {
		t.Error("expected handoff to succeed")
	}
	if result.FromModel != "claude-3" {
		t.Errorf("expected from model 'claude-3', got '%s'", result.FromModel)
	}
	if result.ToModel != "gpt-4" {
		t.Errorf("expected to model 'gpt-4', got '%s'", result.ToModel)
	}
	if result.FromProfile != "default" {
		t.Errorf("expected from profile 'default', got '%s'", result.FromProfile)
	}
	if result.ToProfile != "coding" {
		t.Errorf("expected to profile 'coding', got '%s'", result.ToProfile)
	}
	if result.HandoffID == "" {
		t.Error("expected non-empty handoff ID")
	}
}

func TestExecuteHandoff_Fallback(t *testing.T) {
	hm := NewHandoffManager()
	req := HandoffRequest{
		TargetModel: "", // empty, should fallback to current
		Reason:      "test fallback",
	}

	result := hm.ExecuteHandoff(context.Background(), "session-1", "claude-3", "default", req)

	if result.ToModel != "claude-3" {
		t.Errorf("expected fallback to current model 'claude-3', got '%s'", result.ToModel)
	}
}

func TestGetHandoffHistory(t *testing.T) {
	hm := NewHandoffManager()
	
	// Execute two handoffs
	req1 := HandoffRequest{TargetModel: "gpt-4", Reason: "first"}
	hm.ExecuteHandoff(context.Background(), "session-1", "claude-3", "default", req1)
	
	req2 := HandoffRequest{TargetModel: "gemini", Reason: "second"}
	hm.ExecuteHandoff(context.Background(), "session-1", "gpt-4", "coding", req2)

	// Check history for session-1
	history := hm.GetHandoffHistory("session-1")
	if len(history) != 2 {
		t.Errorf("expected 2 handoffs, got %d", len(history))
	}

	// Check no history for different session
	history2 := hm.GetHandoffHistory("session-2")
	if len(history2) != 0 {
		t.Errorf("expected 0 handoffs for session-2, got %d", len(history2))
	}
}

func TestBuildHandoffPrompt(t *testing.T) {
	result := &HandoffResult{
		HandoffID:   "handoff-123",
		FromModel:   "claude-3",
		ToModel:     "gpt-4",
		FromProfile: "default",
		ToProfile:   "coding",
	}

	prompt := BuildHandoffPrompt(result, "How do I implement a binary search?")

	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !contains(prompt, "claude-3") {
		t.Error("prompt should mention from model")
	}
	if !contains(prompt, "gpt-4") {
		t.Error("prompt should mention to model")
	}
	if !contains(prompt, "handoff-123") {
		t.Error("prompt should mention handoff ID")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
