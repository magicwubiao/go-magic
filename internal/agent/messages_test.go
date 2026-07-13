package agent

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

func msg(role string) provider.Message {
	return provider.Message{Role: role, Content: role}
}

func msgToolCalls(role string) provider.Message {
	return provider.Message{Role: role, ToolCalls: []types.ToolCall{{ID: "c1", Name: "t"}}}
}

func TestValidateMessageAlternationValid(t *testing.T) {
	cases := [][]provider.Message{
		nil,
		{msg("system")},
		{msg("system"), msg("user")},
		{msg("system"), msg("user"), msg("assistant")},
		{msg("system"), msg("user"), msg("assistant"), msg("user")},
		// Tool calling block: assistant(tool_calls) -> tool -> tool -> assistant
		{
			msg("system"), msg("user"),
			msgToolCalls("assistant"), msg("tool"), msg("tool"),
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
