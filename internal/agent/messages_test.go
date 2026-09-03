package agent

import (
	"fmt"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

func msg(role string) provider.Message {
	return provider.Message{Role: role, Content: role}
}

func msgToolCalls(role string) provider.Message {
	return msgToolCallsN(role, 1)
}

func msgToolCallsN(role string, n int) provider.Message {
	tcs := make([]types.ToolCall, 0, n)
	for i := 0; i < n; i++ {
		tcs = append(tcs, types.ToolCall{
			ID:   fmt.Sprintf("tc_%d", i),
			Name: fmt.Sprintf("tool_%d", i),
		})
	}
	return provider.Message{Role: role, ToolCalls: tcs}
}

func TestValidateMessageAlternationValid(t *testing.T) {
	cases := [][]provider.Message{
		nil,
		{msg("system")},
		{msg("system"), msg("user")},
		{msg("system"), msg("user"), msg("assistant")},
		{msg("system"), msg("user"), msg("assistant"), msg("user")},
		// Tool calling block: assistant(tool_calls) -> tool -> tool -> assistant.
		// NOTE: tool messages MUST carry a non-empty ToolCallID; providers like
		// Zhipu reject empty tool_call_id with 1214 ("messages 参数非法").
		// Assistant content MUST be non-empty (RC2); ToolCallIDs must be a
		// perfect match with the assistant's ToolCalls (RC1).
		{
			msg("system"), msg("user"),
			{Role: "assistant", Content: "calling tools",
				ToolCalls: []types.ToolCall{{ID: "tc_0", Name: "t0"}, {ID: "tc_1", Name: "t1"}}},
			{Role: "tool", ToolCallID: "tc_0", Content: "res0"},
			{Role: "tool", ToolCallID: "tc_1", Content: "res1"},
			msg("assistant"),
		},
	}
	for i, c := range cases {
		if v := ValidateMessageAlternation(c); len(v) != 0 {
			t.Errorf("case %d: expected no violations, got %v", i, v)
		}
	}
}

func TestValidateMessageAlternationIllegal(t *testing.T) {
	cases := []struct {
		name    string
		msgs    []provider.Message
		wantMin int // minimum expected number of violations
	}{
		{"two users in a row",
			[]provider.Message{msg("system"), msg("user"), msg("user")},
			1},
		{"two assistants in a row",
			[]provider.Message{msg("system"), msg("user"), msg("assistant"), msg("assistant")},
			1},
		{"tool without preceding assistant tool_call",
			[]provider.Message{msg("system"), msg("user"), msg("tool")},
			1},
		{"tool after assistant with no tool_calls",
			[]provider.Message{msg("system"), msg("user"), msg("assistant"), msg("tool")},
			1},
		{"history starts with tool",
			[]provider.Message{msg("tool")},
			1},
		{"empty role",
			[]provider.Message{msg("system"), {Role: "", Content: "x"}},
			1},
		{"tool with empty tool_call_id",
			// Zhipu/Minimax throw 1214 "messages 参数非法" for any tool
			// message whose tool_call_id is empty. The validator now flags
			// this so SanitizeMessageHistory drops the offender.
			[]provider.Message{msg("system"), msg("user"), msgToolCalls("assistant"),
				{Role: "tool", ToolCallID: "", Content: "res"}},
			1},
		{"assistant with orphan tool_calls -> next non-tool (user)",
			// RC1 root cause of 26-turn 1214: truncateHistory deleted only
			// the *tail* of a tool-call block, leaving an assistant message
			// that still carries ToolCalls but has ZERO corresponding tool
			// results following it, followed immediately by a user/system.
			// Zhipu rejects this with 1214 "messages 参数非法".
			[]provider.Message{msg("system"), msg("user"), msgToolCalls("assistant"), msg("user")},
			1},
		{"assistant with partial tool_calls -> only 1 of 2 tool results present",
			// RC1 variant: assistant declares 2 tool calls, only 1 tool result
			// follows, then a user. The orphan 2nd tool_call is the
			// truncation artefact.
			[]provider.Message{msg("system"), msg("user"),
				msgToolCallsN("assistant", 2),
				{Role: "tool", ToolCallID: "tc_0", Content: "ok"},
				msg("user")},
			1},
		{"assistant with empty content and orphan tool_calls",
			// Empty content alone is no longer a violation (patched only in
			// the outbound copy via buildLLMMessages). But this assistant
			// also has tool_calls with no following tool results → orphan
			// check (RC1) still flags it.
			[]provider.Message{msg("system"), msg("user"),
				{Role: "assistant", Content: "", ToolCalls: msgToolCalls("assistant").ToolCalls}},
			1},
	}
	for _, c := range cases {
		v := ValidateMessageAlternation(c.msgs)
		if len(v) < c.wantMin {
			t.Errorf("%s: expected >= %d violations, got %d: %v", c.name, c.wantMin, len(v), v)
		}
	}
}

func TestSanitizeMessageHistoryDropsOffenders(t *testing.T) {
	// Two user messages in a row -> the second user is dropped.
	in := []provider.Message{msg("system"), msg("user"), msg("user"), msg("assistant")}
	out := SanitizeMessageHistory(in)
	if v := ValidateMessageAlternation(out); len(v) != 0 {
		t.Errorf("sanitized history still has violations: %v", v)
	}
	if len(out) != 3 {
		t.Errorf("expected 3 messages after sanitize, got %d: %+v", len(out), out)
	}
}

func TestSanitizeMessageHistoryNoopForValid(t *testing.T) {
	in := []provider.Message{msg("system"), msg("user"), msg("assistant")}
	out := SanitizeMessageHistory(in)
	if v := ValidateMessageAlternation(out); len(v) != 0 {
		t.Errorf("valid history became invalid: %v", v)
	}
	if len(out) != len(in) {
		t.Errorf("valid history changed length: %d -> %d", len(in), len(out))
	}
}

func TestSanitizeMessageHistoryToolWithoutAssistant(t *testing.T) {
	// tool following a bare assistant (no tool_calls) -> tool is dropped.
	in := []provider.Message{msg("system"), msg("user"), msg("assistant"), msg("tool"), msg("user")}
	out := SanitizeMessageHistory(in)
	if v := ValidateMessageAlternation(out); len(v) != 0 {
		t.Errorf("sanitized history still has violations: %v", v)
	}
}
