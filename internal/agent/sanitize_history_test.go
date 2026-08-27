package agent

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestSanitizeHistoryCollapsesConsecutiveUsers reproduces the zhipu/GLM
// 1214 failure: an aborted turn leaves an injected user-role recovery prompt
// at the tail of the persisted history; the next real user input then makes
// two consecutive user messages, which strict providers reject forever.
func TestSanitizeHistoryCollapsesConsecutiveUsers(t *testing.T) {
	a := &Agent{}
	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		// Injected recovery/summary prompt left behind by an aborted turn:
		{Role: "user", Content: "Please provide a final summary of what has been accomplished so far."},
		// Real new input:
		{Role: "user", Content: "继续处理"},
	}

	a.sanitizeHistory()

	if v := ValidateMessageAlternation(a.history); len(v) > 0 {
		t.Fatalf("history still invalid after sanitize: %v", v)
	}
	last := a.history[len(a.history)-1]
	if last.Content != "继续处理" {
		t.Fatalf("real user input lost: kept %q", last.Content)
	}
	for _, m := range a.history {
		if m.Content == "Please provide a final summary of what has been accomplished so far." {
			t.Fatalf("injected prompt survived as duplicate user message")
		}
	}
}

// TestSanitizeHistoryDropsOrphanTool verifies the pre-existing behavior is
// preserved by the rewrite.
func TestSanitizeHistoryDropsOrphanTool(t *testing.T) {
	a := &Agent{}
	a.history = []provider.Message{
		{Role: "user", Content: "run it"},
		{Role: "tool", ToolCallID: "call_1", Content: "result"},
	}
	a.sanitizeHistory()
	if len(a.history) != 1 || a.history[0].Role != "user" {
		t.Fatalf("orphan tool not dropped: %+v", a.history)
	}
}

// TestStreamSanitizeUsesSameRules ensures streaming path sanitization uses the
// shared validator (compile-level guard against drift between loops).
func TestStreamSanitizeUsesSameRules(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "a"},
		{Role: "user", Content: "b"},
	}
	cleaned := SanitizeMessageHistory(msgs)
	if v := ValidateMessageAlternation(cleaned); len(v) > 0 {
		t.Fatalf("generic sanitizer left violations: %v", v)
	}
}
