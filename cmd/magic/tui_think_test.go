package main

import (
	"strings"
	"testing"
)

func TestSplitThinkContent(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantReasoning string
		wantBody      string
	}{
		{
			name:          "no thinking",
			input:         "plain answer",
			wantReasoning: "",
			wantBody:      "plain answer",
		},
		{
			name:          "closed think block",
			input:         "<think>let me analyze</think>\nFinal answer here.",
			wantReasoning: "let me analyze",
			wantBody:      "Final answer here.",
		},
		{
			name:          "unterminated think tail (mid-stream)",
			input:         "<think>still reasoning...",
			wantReasoning: "still reasoning...",
			wantBody:      "",
		},
		{
			name:          "empty reasoning",
			input:         "<think></think>\nbody only",
			wantReasoning: "",
			wantBody:      "body only",
		},
		{
			name:          "multiple think blocks",
			input:         "pre <think>one</think> mid <think>two</think> post",
			wantReasoning: "onetwo",
			wantBody:      "pre  mid  post",
		},
		{
			name:          "multi-line reasoning",
			input:         "<think>line1\nline2</think>\n\ntext",
			wantReasoning: "line1\nline2",
			wantBody:      "text",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reasoning, body := splitThinkContent(c.input)
			if reasoning != c.wantReasoning {
				t.Errorf("reasoning = %q, want %q", reasoning, c.wantReasoning)
			}
			if body != c.wantBody {
				t.Errorf("body = %q, want %q", body, c.wantBody)
			}
		})
	}
}

func TestRenderThinkingBlock(t *testing.T) {
	// Short reasoning renders fully with a label
	out := renderThinkingBlock("quick check", false)
	if !strings.Contains(out, "💭 Thought") {
		t.Errorf("expected 💭 Thought label, got: %s", out)
	}
	if !strings.Contains(out, "quick check") {
		t.Errorf("expected reasoning content, got: %s", out)
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("short reasoning should not be truncated: %s", out)
	}

	// Streaming variant uses 🧠 Thinking label
	streaming := renderThinkingBlock("step 1", true)
	if !strings.Contains(streaming, "🧠 Thinking") {
		t.Errorf("expected 🧠 Thinking label while streaming, got: %s", streaming)
	}

	// Long reasoning gets truncated
	long := strings.Repeat("line of thinking\n", 30)
	trunc := renderThinkingBlock(long, false)
	if !strings.Contains(trunc, "truncated") {
		t.Errorf("long reasoning should be truncated: %s", trunc)
	}

	// Empty reasoning renders nothing
	if out := renderThinkingBlock("   ", false); out != "" {
		t.Errorf("blank reasoning should render empty, got: %q", out)
	}
}
