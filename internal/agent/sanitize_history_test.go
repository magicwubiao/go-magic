package agent

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
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

// TestSanitizeHistoryRepairsEmptyAssistantContent verifies Pass 2.5 patches
// assistant messages with empty/whitespace content regardless of ToolCalls —
// Zhipu 1214 fires on both empty-content-only assistants (old case covered by
// the previous implementation) AND tool-call-carrying assistants with empty
// content (the case missed by the old pass, reproduced by mid-conversation
// truncateHistory after a streaming partial assistant write).
func TestSanitizeHistoryRepairsEmptyAssistantContent(t *testing.T) {
	a := &Agent{}
	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "ls"},
		// tool-calling assistant whose content is empty because the stream
		// parser only captured the ToolCalls chunk before truncation.
		{Role: "assistant", Content: "",
			ToolCalls: []types.ToolCall{{ID: "call_1", Type: "function",
				Function: types.Function{Name: "ls", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "a\nb\nc"},
		{Role: "assistant", Content: "", ToolCalls: nil}, // empty plain assistant
		{Role: "user", Content: "next"},
	}
	a.sanitizeHistory()

	for i, m := range a.history {
		if m.Role == "assistant" {
			// Pass 2.5 fills truly-empty/whitespace content with a one-space
			// placeholder (TrimSpace on a space-placeholder returns "" so we
			// must compare on actual emptiness instead).
			if m.Content == "" {
				t.Errorf("assistant[%d] still has truly empty content (len tc=%d)",
					i, len(m.ToolCalls))
			}
		}
	}
	if v := ValidateMessageAlternation(a.history); len(v) > 0 {
		t.Fatalf("final history invalid: %+v", v)
	}
}

// TestSanitizeHistoryStripsOrphanToolCalls verifies Pass 4 removes ToolCalls
// from an assistant when the corresponding tool-role result messages were
// lost (most commonly: truncateHistory dropped a completed assistant+tool
// pair mid-structure). Leaving orphan ToolCalls is a top cause of Zhipu 1214
// in mid-conversation (turn 26+) because the provider sees an asymmetrical
// tool_calls/tool sequence.
func TestSanitizeHistoryStripsOrphanToolCalls(t *testing.T) {
	a := &Agent{}
	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "run cmd"},
		// assistant called 2 tools but truncation only left 1 result behind.
		{Role: "assistant", Content: "Invoking tools", ToolCalls: []types.ToolCall{
			{ID: "keep_1", Type: "function", Function: types.Function{Name: "ls"}},
			{ID: "drop_2", Type: "function", Function: types.Function{Name: "rm"}},
		}},
		{Role: "tool", ToolCallID: "keep_1", Content: "dir listing"},
		// drop_2 tool result lost during previous truncateHistory.
		{Role: "user", Content: "what now?"},
	}
	a.sanitizeHistory()

	// Find the assistant with tool calls.
	var assisted *provider.Message
	for i := range a.history {
		m := &a.history[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			assisted = m
			break
		}
	}
	if assisted == nil {
		t.Fatalf("expected at least one assistant with ToolCalls preserved, got %+v", a.history)
	}
	for _, tc := range assisted.ToolCalls {
		if tc.ID == "drop_2" {
			t.Errorf("orphan tool_call drop_2 was NOT stripped; assistant still carries %+v",
				assisted.ToolCalls)
		}
	}
	found := false
	for _, tc := range assisted.ToolCalls {
		if tc.ID == "keep_1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("claimed tool_call keep_1 was incorrectly removed")
	}
	if v := ValidateMessageAlternation(a.history); len(v) > 0 {
		t.Fatalf("final history invalid: %+v", v)
	}
}

// TestSanitizeHistoryDropsLeadingIllegalRoles verifies Pass 5 drops a leading
// assistant/tool message after truncateHistory dropped the system+user head.
// The resulting history must start with system or user.
func TestSanitizeHistoryDropsLeadingIllegalRoles(t *testing.T) {
	a := &Agent{}
	a.history = []provider.Message{
		// system/user head was dropped by truncateHistory → assistant first.
		{Role: "assistant", Content: "hello", ToolCalls: nil},
		{Role: "user", Content: "hi"},
	}
	a.sanitizeHistory()
	if len(a.history) == 0 {
		t.Fatal("all messages dropped unexpectedly")
	}
	first := a.history[0].Role
	if first != "user" && first != "system" {
		t.Errorf("history still starts with illegal role %q: %+v", first, a.history)
	}
	if v := ValidateMessageAlternation(a.history); len(v) > 0 {
		t.Fatalf("final history invalid: %+v", v)
	}
}
