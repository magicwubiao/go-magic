package provider

import (
	"strings"
	"testing"
)

// TestOpenAIStreamParser_ReasoningShapes covers reasoning deltas across the
// shapes real endpoints emit: canonical string field, alias strings, and the
// object / summary-parts array shapes used by OpenAI gpt-5/o-series style
// endpoints. Regression: non-string `reasoning` payloads used to be dropped
// by a plain `.(string)` assertion, so custom/openai providers showed no
// thinking process at all.
func TestOpenAIStreamParser_ReasoningShapes(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "canonical reasoning_content string",
			line: `data: {"choices":[{"delta":{"reasoning_content":"thinking hard"}}]}`,
			want: "thinking hard",
		},
		{
			name: "reasoning alias string (gpt-5 style)",
			line: `data: {"choices":[{"delta":{"reasoning":"let me see"}}]}`,
			want: "let me see",
		},
		{
			name: "thinking alias string",
			line: `data: {"choices":[{"delta":{"thinking":"hmm"}}]}`,
			want: "hmm",
		},
		{
			name: "reasoning summary object",
			line: `data: {"choices":[{"delta":{"reasoning":{"text":"step by step"}}}]}`,
			want: "step by step",
		},
		{
			name: "reasoning summary parts array",
			line: `data: {"choices":[{"delta":{"reasoning":[{"type":"summary_text","text":"part one"},{"text":"part two"}]}}]}`,
			want: "part onepart two",
		},
		{
			name: "reasoning object with content parts",
			line: `data: {"choices":[{"delta":{"reasoning":{"content":[{"text":"deep thought"}]}}}]}`,
			want: "deep thought",
		},
		{
			name: "null reasoning ignored",
			line: `data: {"choices":[{"delta":{"reasoning":null,"content":"answer"}}]}`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &OpenAIStreamParser{}
			resp, err := p.Parse(tc.line)
			if err != nil {
				t.Fatalf("Parse(%s) error: %v", tc.line, err)
			}
			if resp == nil {
				t.Fatal("Parse returned nil response")
			}
			if got := resp.ReasoningContent; got != tc.want {
				t.Fatalf("ReasoningContent = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOpenAIStreamParser_ReasoningWithContent verifies reasoning and content
// in one delta both survive parsing.
func TestOpenAIStreamParser_ReasoningWithContent(t *testing.T) {
	p := &OpenAIStreamParser{}
	resp, err := p.Parse(`data: {"choices":[{"delta":{"reasoning":{"summary":[{"text":"plan"}]},"content":"hello"}}]}`)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if resp.ReasoningContent != "plan" || resp.Content != "hello" {
		t.Fatalf("got reasoning=%q content=%q, want plan/hello", resp.ReasoningContent, resp.Content)
	}
}

// TestParseTypedChatResponse_ReasoningObjectShape covers the non-streaming
// path: message-level `reasoning` as an object must be unwrapped, not dropped.
func TestParseTypedChatResponse_ReasoningObjectShape(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"ok","reasoning":{"text":"quiet thought"}}}],"usage":{}}`)
	resp, err := parseTypedChatResponse(body)
	if err != nil {
		t.Fatalf("parseTypedChatResponse error: %v", err)
	}
	if resp.ReasoningContent != "quiet thought" {
		t.Fatalf("ReasoningContent = %q, want %q", resp.ReasoningContent, "quiet thought")
	}
	if !strings.Contains(resp.Content, "ok") {
		t.Fatalf("Content = %q, want it preserved", resp.Content)
	}
}
