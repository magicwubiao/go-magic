package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestConvertZhipu1214Regression covers the zhipu (GLM) error 1214
// ("messages 参数非法") regression: checkpoint-restored histories contain
// assistant messages with tool_calls and empty content, and tool messages
// with empty content. The converter must never emit JSON null content or
// empty-string arguments, both of which zhipu rejects with 1214.
func TestConvertZhipu1214Regression(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "帮我查下天气"},
		// Half-finished assistant turn from an interrupted session:
		// tool_calls present, content empty.
		{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{
			{ID: "call_1", Type: "function", Function: types.Function{Name: "weather", Arguments: ""}},
		}},
		// Tool result with empty content (e.g. tool produced no output).
		{Role: "tool", Content: "", ToolCallID: "call_1"},
		{Role: "assistant", Content: "今天晴。"},
	}

	out := ConvertMessagesWithConfig(msgs, nil)
	if len(out) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(out))
	}

	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	if strings.Contains(s, `"content":null`) {
		t.Errorf("converted output contains content:null (zhipu 1214 trigger):\n%s", s)
	}
	if strings.Contains(s, `"arguments":""`) {
		t.Errorf("converted output contains empty arguments (zhipu 1214 trigger):\n%s", s)
	}

	// Assistant with tool_calls and empty content must serialize as a
	// non-empty placeholder so Zhipu's TrimSpace-sensitive validator does
	// not reject it with 1214.
	if got := out[1]["content"]; got == "" || strings.TrimSpace(got.(string)) == "" {
		t.Errorf("assistant tool_call content = %v (%T), want non-empty placeholder", got, got)
	}

	// Tool message with empty content must get a placeholder, not null.
	if got := out[2]["content"]; got != "(empty tool result)" {
		t.Errorf("tool content = %v (%T), want \"(empty tool result)\"", got, got)
	}

	// Empty arguments must be normalized to "{}".
	fn := out[1]["tool_calls"].([]map[string]interface{})[0]["function"].(map[string]interface{})
	if fn["arguments"] != "{}" {
		t.Errorf("arguments = %v, want {}", fn["arguments"])
	}

	// tool_call_id must still be present on the tool message.
	if out[2]["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v, want call_1", out[2]["tool_call_id"])
	}
}

// TestConvertContentPartsNeverEmptyArray ensures a message whose parts are
// all dropped does not serialize as "content": [] which some providers
// reject as an invalid messages payload.
func TestConvertContentPartsNeverEmptyArray(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", ContentParts: []types.ContentPart{
			{Type: "unsupported_type"},
		}},
	}
	out := ConvertMessagesWithConfig(msgs, nil)
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), `"content":[]`) {
		t.Errorf("content serialized as empty array:\n%s", string(b))
	}
}

// TestNormalizeToolArguments unit-tests the arguments normalizer.
func TestNormalizeToolArguments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "{}"},
		{"   ", "{}"},
		{`{"a":1}`, `{"a":1}`},
	}
	for _, c := range cases {
		if got := normalizeToolArguments(c.in); got != c.want {
			t.Errorf("normalizeToolArguments(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestConvertEmptyUserSystemContent covers the role-agnostic empty-content
// guard: zhipu/GLM error 1214 also fires for a user or system message whose
// content is empty/whitespace-only — flows that bypass buildLLMMessages
// (compression rebuilds, injected recovery prompts, media turns) can carry
// such messages into the converter. Every non-tool message must serialize
// with a non-empty string content.
func TestConvertEmptyUserSystemContent(t *testing.T) {
	msgs := []types.Message{
		{Role: "system", Content: ""},
		{Role: "user", Content: "   "},
		{Role: "assistant", Content: ""}, // no tool_calls: bare empty assistant
		{Role: "user", Content: "今天怎么又 1214 了？"},
	}

	out := ConvertMessagesWithConfig(msgs, nil)
	if len(out) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(out))
	}

	for i, m := range out {
		c, ok := m["content"].(string)
		if !ok {
			t.Errorf("message %d (role=%v) content = %T, want string", i, m["role"], m["content"])
			continue
		}
		if strings.TrimSpace(c) == "" {
			t.Errorf("message %d (role=%v) content = %q, want non-empty placeholder", i, m["role"], c)
		}
		// The placeholder must be invisible: content may be the zero-width
		// space, but never a readable marker or the raw empty string.
		if c == "" || c == " " {
			t.Errorf("message %d (role=%v) content = %q, want zero-width placeholder", i, m["role"], c)
		}
	}

	b, _ := json.Marshal(out)
	s := string(b)
	if strings.Contains(s, `"content":""`) || strings.Contains(s, `"content":" "`) {
		t.Errorf("payload contains empty/whitespace content (zhipu 1214 trigger):\n%s", s)
	}
}
