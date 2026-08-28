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

	// Assistant with tool_calls and empty content must serialize as "".
	if got := out[1]["content"]; got != "" {
		t.Errorf("assistant tool_call content = %v (%T), want \"\"", got, got)
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
