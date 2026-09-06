package provider

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// splitThinkPrefix / ApplyReasoningPassback cover the DeepSeek V4 thinking
// rule: prior assistant turns must carry their reasoning_content back or the
// API answers 400 "The reasoning_content in the thinking mode must be passed
// back to the API". Agent history stores reasoning inline as a leading
// <think> block (wrapLLMReasoning format: "<think>R</think>\n" + content).

func TestSplitThinkPrefix(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		reasoning string
		rest      string
		ok        bool
	}{
		{"thinking plus content", "<think>let me check</think>\nthe answer", "let me check", "the answer", true},
		{"thinking only", "<think>hmm</think>", "hmm", "", true},
		{"no think block", "plain answer", "", "plain answer", false},
		{"think not at start", "hello <think>late</think>", "", "hello <think>late</think>", false},
		{"unterminated think", "<think>cut off mid", "", "<think>cut off mid", false},
		{"leading whitespace", "\n  <think>r</think> c", "r", "c", true},
		{"empty reasoning", "<think></think>answer", "", "answer", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reasoning, rest, ok := SplitThinkPrefix(tc.in)
			if ok != tc.ok || reasoning != tc.reasoning || rest != tc.rest {
				t.Fatalf("SplitThinkPrefix(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, reasoning, rest, ok, tc.reasoning, tc.rest, tc.ok)
			}
		})
	}
}

func TestApplyReasoningPassbackDeepSeekV4(t *testing.T) {
	converted := []map[string]interface{}{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "hi"},
		{ // full turn: thinking + visible answer
			"role":    "assistant",
			"content": "<think>step 1...</think>\nvisible answer",
		},
		{ // tool-call turn whose visible part is empty
			"role":    "assistant",
			"content": "<think>calling tool</think>",
			"tool_calls": []map[string]interface{}{
				{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "f", "arguments": "{}"}},
			},
		},
		{"role": "tool", "content": "result"},
		{ // no thinking on this turn
			"role":    "assistant",
			"content": "final reply",
		},
	}

	out := ApplyReasoningPassback(converted, "deepseek-v4-flash")

	if got := out[2]["reasoning_content"]; got != "step 1..." {
		t.Errorf("assistant turn reasoning_content = %q, want %q", got, "step 1...")
	}
	if got := out[2]["content"]; got != "visible answer" {
		t.Errorf("assistant turn content = %q, want think block stripped", got)
	}
	// Reasoning-only tool-call turn: reasoning extracted, content falls back
	// to the invisible placeholder (empty-content defenses preserved).
	if got := out[3]["reasoning_content"]; got != "calling tool" {
		t.Errorf("tool-call turn reasoning_content = %q, want %q", got, "calling tool")
	}
	if got := out[3]["content"]; got != emptyAssistantPlaceholder {
		t.Errorf("tool-call turn content = %q, want placeholder", got)
	}
	// Non-assistant roles and think-less assistant turns untouched.
	for _, idx := range []int{0, 1, 5} {
		if _, has := out[idx]["reasoning_content"]; has {
			t.Errorf("message %d unexpectedly got reasoning_content", idx)
		}
	}
	if got := out[4]["content"]; got != "result" {
		t.Errorf("tool message content = %q, want unchanged", got)
	}
	if got := out[5]["content"]; got != "final reply" {
		t.Errorf("plain assistant content = %q, want unchanged", got)
	}
}

func TestApplyReasoningPassbackModelGating(t *testing.T) {
	msg := []map[string]interface{}{
		{"role": "assistant", "content": "<think>r</think>\nanswer"},
	}
	for _, model := range []string{"gpt-5.6", "kimi-k3", "glm-5.3", "LongCat-2.0-Preview", "deepseek-chat", "qwen3.8-max"} {
		out := ApplyReasoningPassback(msg, model)
		if _, has := out[0]["reasoning_content"]; has {
			t.Errorf("model %q: reasoning_content must NOT be added (passback is opt-in per model)", model)
		}
		if got := out[0]["content"]; got != "<think>r</think>\nanswer" {
			t.Errorf("model %q: content must stay unchanged, got %q", model, got)
		}
	}
	if !RequiresReasoningPassback("deepseek-v4-pro") || !RequiresReasoningPassback("deepseek-v4-flash") {
		t.Error("deepseek-v4* models must require reasoning passback")
	}
}

// Integration: the DeepSeek provider's message preparation (the shared
// OpenAI-compatible path used by Chat/ChatWithTools/stream) must emit
// reasoning_content for thinking history, which is what unblocks the
// reported 400 in chat mode.
func TestDeepSeekPrepMessagesPassback(t *testing.T) {
	p := NewDeepSeekProvider("key", "", "deepseek-v4-flash", nil)
	messages := []types.Message{
		{Role: "user", Content: "run the tests"},
		{
			Role:    "assistant",
			Content: "<think>need to check the failure</think>\nI'll check the failing test.",
			ToolCalls: []types.ToolCall{
				{ID: "call_1", Type: "function", Function: types.Function{Name: "execute_command", Arguments: `{"command":"go test"}`}},
			},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}

	out := p.prepMessages(messages)
	var assistant map[string]interface{}
	for _, m := range out {
		if r, _ := m["role"].(string); r == "assistant" {
			assistant = m
		}
	}
	if assistant == nil {
		t.Fatal("no assistant message in converted output")
	}
	if got, _ := assistant["reasoning_content"].(string); !strings.Contains(got, "check the failure") {
		t.Errorf("reasoning_content = %q, want the extracted thinking text", got)
	}
	if got, _ := assistant["content"].(string); strings.Contains(got, "<think>") {
		t.Errorf("content still carries the think block: %q", got)
	}
}
