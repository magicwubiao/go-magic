package agent

import (
	"fmt"
	"strings"
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

// TestSanitizeHistoryRepairsEmptyAssistantContent verifies that empty
// assistant content is NOT patched in a.history (which would leak a
// placeholder to the UI) but IS handled correctly in the outbound copy via
// buildLLMMessages:
//   - empty assistant WITH tool_calls is kept and patched with a natural-language placeholder
//   - empty assistant WITHOUT tool_calls is dropped (best practice from
//     open-webui #25083 / Hermes #66429: such messages carry no payload and
//     poison the model's self-view), with consecutive user/system collapsed.
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

	// a.history should still have empty content — the UI must not see any placeholder.
	for i, m := range a.history {
		if m.Role == "assistant" && m.Content == emptyAssistantPlaceholder {
			t.Errorf("assistant[%d] leaked placeholder into stored history", i)
		}
	}

	// The outbound copy: tool-calling assistant keeps non-empty content
	// (patched to a natural-language placeholder); the empty plain
	// assistant (no tool_calls) is dropped entirely, and the resulting
	// user→user run is collapsed to the newest user message so
	// alternation stays valid.
	outbound := a.buildLLMMessages()
	for i, m := range outbound {
		if m.Role == "assistant" && strings.TrimSpace(m.Content) == "" {
			t.Errorf("buildLLMMessages outbound assistant[%d] has empty content", i)
		}
	}
	if v := ValidateMessageAlternation(outbound); len(v) > 0 {
		t.Fatalf("outbound copy invalid: %+v", v)
	}
	// Confirm the empty-no-tool assistant was dropped (only the tool-calling
	// assistant remains).
	assistantCount := 0
	for _, m := range outbound {
		if m.Role == "assistant" {
			assistantCount++
		}
	}
	if assistantCount != 1 {
		t.Fatalf("expected 1 assistant (tool-calling only) in outbound, got %d", assistantCount)
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

// TestBuildLLMMessagesNeverProducesWhitespaceEmptyAssistant is the regression
// test for the 1→9 violation-growth bug. The symptom was: every streaming
// tool turn produced a new "sanitizing N message-alternation violation(s)"
// WARN line with N monotonically increasing (1→2→3→…→9), and the sample
// always listed positions #4/#6/#8/... as "assistant message with empty
// content (provider 1214: messages 参数非法)".
//
// Root cause: buildLLMMessages wrote a single-space placeholder when an
// assistant had ToolCalls, but ValidateMessageAlternation uses
// strings.TrimSpace(msg.Content) == "", so a single-space placeholder still
// counted as empty → SanitizeMessageHistory ran on every call but only
// repaired the outbound req.Messages copy, not a.history. On the NEXT turn
// buildLLMMessages re-read the same a.history and produced the same
// single-space offender → validator flagged N violations again, growing N
// whenever a fresh tool round appended a new empty-content assistant with
// whitespace-only content (e.g. streaming returned no text chunks, only a
// tool_calls block, so fullContent="" reached a.history).
func TestBuildLLMMessagesNeverProducesWhitespaceEmptyAssistant(t *testing.T) {
	a := &Agent{}
	// Simulate 5 consecutive tool rounds, each one appending an assistant
	// whose content is ""/whitespace (the case that triggered the growth
	// pattern) plus a matching tool result.
	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "start"},
	}
	roundNo := 0
	appendRound := func(assistantContent, toolResult string) {
		roundNo++
		tc := types.ToolCall{
			ID:   fmt.Sprintf("call_r%d_%d", roundNo, len(a.history)),
			Type: "function",
			Function: types.Function{
				Name:      "ls",
				Arguments: "{}",
			},
		}
		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: []types.ToolCall{tc},
		})
		a.history = append(a.history, provider.Message{
			Role:       "tool",
			ToolCallID: tc.ID,
			Content:    toolResult,
		})
		a.history = append(a.history, provider.Message{
			Role:    "user",
			Content: "next",
		})
	}
	// Cover every flavor of "empty-looking" content the stream path can
	// produce after stripping a <think> block, after a newline-only chunk,
	// or after the provider never emitted text chunks at all.
	for _, tc := range []struct{ c, r string }{
		{c: "", r: "tool result 1"},
		{c: " ", r: "tool result 2"},
		{c: "\n", r: "tool result 3"},
		{c: " \t \n", r: "tool result 4"},
		{c: "<think>long deliberation</think>\n", r: "tool result 5"},
	} {
		appendRound(tc.c, tc.r)
	}

	// Now walk the whole history the way a real streaming loop does:
	// buildLLMMessages → validate once → expect ZERO violations because
	// the stored history never had a whitespace-only assistant.
	seen := -1
	for round := 0; round < 3; round++ {
		outbound := a.buildLLMMessages()
		v := ValidateMessageAlternation(outbound)
		if len(v) != 0 {
			t.Fatalf("round %d: buildLLMMessages still produced %d violations "+
				"(sample: %+v) — monotonic-growth regression NOT fixed",
				round, len(v), v)
		}
		if seen == -1 {
			seen = len(v)
		} else if len(v) > seen {
			t.Fatalf("round %d: violations grew %d→%d — monotonic-growth "+
				"regression NOT fixed", round, seen, len(v))
		}
	}

	// Also guarantee that when content truly comes back empty for a
	// no-tool assistant (extreme case), buildLLMMessages DROPS those
	// empty assistants and collapses the resulting consecutive user run
	// so the outbound copy still passes alternation validation.
	a2 := &Agent{}
	a2.history = []provider.Message{
		{Role: "user", Content: "ok"},
		{Role: "assistant", Content: "", ToolCalls: nil},
		{Role: "user", Content: "and?"},
		{Role: "assistant", Content: "  \n\t  ", ToolCalls: nil},
	}
	out2 := a2.buildLLMMessages()
	// No assistant should remain (both were empty with no tool_calls).
	for i, m := range out2 {
		if m.Role == "assistant" {
			t.Errorf("buildLLMMessages kept empty no-tool assistant at %d (should be dropped)", i)
		}
	}
	if v := ValidateMessageAlternation(out2); len(v) > 0 {
		t.Fatalf("ValidateMessageAlternation on buildLLMMessages output: %+v", v)
	}
}
