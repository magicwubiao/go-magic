package bot

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestSanitizeBotHistoryOrphanBeforeToolDoesNotPanic is the regression test for
// the production crash:
//
//	panic: runtime error: index out of range [9] with length 9
//	internal/bot.sanitizeBotHistory (manager.go:1155)
//	internal/bot.(*Manager).loadHistory -> buildAgent -> getOrCreateAgentLocked
//
// The backward scan for a tool message's owner indexed `cleaned` (the kept
// messages) with an index taken from the source slice (`i-1`). cleaned is one
// element shorter for every orphan dropped before it, so as soon as ONE
// droppable tool message precedes another tool message the first `cleaned[j]`
// read runs past the end. A single stale/empty-id tool message in a persisted
// bot chat was therefore enough to kill the process on every message — the
// manager goroutine panics, so bot mode stays dead until restart.
func TestSanitizeBotHistoryOrphanBeforeToolDoesNotPanic(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "第一条"},
		// Legacy row: tool message saved without its tool_call id (or its
		// assistant header was lost) — this is the orphan that shortens cleaned.
		{Role: "tool", ToolCallID: "", Content: "stale result"},
		// Plain assistant reply: a scan start that is neither the owner nor a
		// user boundary, forcing the walk to continue.
		{Role: "assistant", Content: "我看看"},
		// The message whose scan panicked before the fix: j started at i-1=3
		// while cleaned only had 3 elements (indices 0..2).
		{Role: "tool", ToolCallID: "call_orphan", Content: "第二个孤儿"},
		{Role: "user", Content: "第二条"},
		{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{{
			ID:       "call_ok",
			Type:     "function",
			Function: types.Function{Name: "read_file", Arguments: `{"path":"a.txt"}`},
		}}},
		{Role: "tool", ToolCallID: "call_ok", Content: "文件内容"},
	}

	cleaned := sanitizeBotHistory(history)

	for _, m := range cleaned {
		if m.Content == "stale result" || m.Content == "第二个孤儿" {
			t.Fatalf("orphaned tool message survived sanitize: %q", m.Content)
		}
	}
	// The legitimate assistant/tool pair must survive intact — over-dropping
	// here would silently strip tool context from every bot turn.
	var haveAssistant, haveTool bool
	for _, m := range cleaned {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID == "call_ok" {
					haveAssistant = true
				}
			}
		}
		if m.Role == "tool" && m.ToolCallID == "call_ok" && m.Content == "文件内容" {
			haveTool = true
		}
	}
	if !haveAssistant || !haveTool {
		t.Fatalf("valid tool exchange dropped (assistant=%v tool=%v): %+v",
			haveAssistant, haveTool, cleaned)
	}
}

// TestSanitizeBotHistoryKeepsLeadingOrphansUnderCheck pins the shape that
// triggered the crash in the first place: several droppable tool messages in a
// row (each one shrinking cleaned further) followed by a legitimate exchange.
// With the old index arithmetic the panic point moved earlier on every drop.
func TestSanitizeBotHistoryKeepsLeadingOrphansUnderCheck(t *testing.T) {
	history := []provider.Message{
		{Role: "tool", ToolCallID: "", Content: "o1"},
		{Role: "tool", ToolCallID: "x", Content: "o2"},
		{Role: "tool", ToolCallID: "y", Content: "o3"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{{
			ID: "c1", Type: "function", Function: types.Function{Name: "ls"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "listing"},
	}

	cleaned := sanitizeBotHistory(history)
	if len(cleaned) != 3 {
		t.Fatalf("expected leading orphans dropped, got %d messages: %+v", len(cleaned), cleaned)
	}
	if cleaned[0].Role != "user" || cleaned[2].ToolCallID != "c1" {
		t.Fatalf("valid tail mangled: %+v", cleaned)
	}
}
