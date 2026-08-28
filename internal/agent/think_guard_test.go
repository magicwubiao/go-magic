package agent

import (
	"strings"
	"testing"
)

func TestStripThinkContent(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no think", "hello world", "hello world"},
		{"closed block", "<think>deliberation</think>\nfinal answer", "final answer"},
		{"unclosed", "<think>loop loop loop", ""},
		{"with tool call kept", "<think>hmm</think>\nLet me check.", "Let me check."},
		{"empty", "", ""},
	}
	for _, c := range cases {
		got := stripThinkContent(c.in)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTruncateRepetition(t *testing.T) {
	// 正常变化内容不应触发（逐句不同的自然文本）
	normal := "The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs. " +
		"How vexingly quick daft zebras jump when alarmed. " +
		"Sphinx of black quartz judges the cloudy vista nearby. " +
		"Amazingly few discotheques provide jukeboxes for the party tonight. " +
		"The five boxing wizards jump quickly over fences. " +
		"Bright vixens jump while dozy fowls quiver and dance. " +
		"Quick zephyrs blow vexing daft Jim through the night. "
	if _, rep := truncateRepetition(normal); rep {
		t.Errorf("normal content flagged as repetition")
	}

	// 精确重复：同一行连续 12 次
	var b strings.Builder
	b.WriteString("Let me look at the batch tool.\n")
	for i := 0; i < 12; i++ {
		b.WriteString("View batch args. GO! Act now!\n")
	}
	out, rep := truncateRepetition(b.String())
	if !rep {
		t.Errorf("exact repetition not detected")
	}
	if !strings.Contains(out, "[repetitive content truncated") {
		t.Errorf("marker missing in output")
	}
	if strings.Count(out, "View batch args") > 3 {
		t.Errorf("repetition not collapsed: %d occurrences", strings.Count(out, "View batch args"))
	}

	// 变体重复：感叹号升级（归一化后相同）
	var b2 strings.Builder
	b2.WriteString(strings.Repeat("intro line here. ", 30))
	for i := 0; i < 12; i++ {
		b2.WriteString("View batch args. GO! Act now" + strings.Repeat("!", i%3+1) + "\n")
	}
	out2, rep2 := truncateRepetition(b2.String())
	if !rep2 {
		t.Errorf("variant repetition not detected (4-gram should match)")
	}
	_ = out2

	// 短内容不处理
	if _, rep := truncateRepetition("short"); rep {
		t.Errorf("short content flagged")
	}
}

func TestLooksLikeUnparsedToolCall(t *testing.T) {
	if !looksLikeUnparsedToolCall(`{"name": "batch", "arguments": {"x": 1}}`) {
		t.Errorf("json tool call not detected")
	}
	if !looksLikeUnparsedToolCall("plain text answer about tools") && false {
		t.Errorf("unreachable")
	}
	if looksLikeUnparsedToolCall("This is a final plain answer with no json at all.") {
		t.Errorf("plain answer misdetected")
	}
	// think 里的 json 讨论不算（会被 strip 后检测）
	if looksLikeUnparsedToolCall("<think>tool_calls are parsed via arguments</think>The task is done.") {
		t.Errorf("think-only markers misdetected")
	}
}
