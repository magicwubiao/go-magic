package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestGuideInboxRoundTrip 验证收件箱 FIFO、排水后清空、空白文本忽略。
func TestGuideInboxRoundTrip(t *testing.T) {
	a := &Agent{}
	a.InjectGuide("先做 A")
	a.InjectGuide("   ") // 空白：必须被忽略
	a.InjectGuide("再做 B")

	got := a.DrainGuides()
	if len(got) != 2 || got[0] != "先做 A" || got[1] != "再做 B" {
		t.Fatalf("drain = %#v, want [先做 A 再做 B]", got)
	}
	if again := a.DrainGuides(); len(again) != 0 {
		t.Fatalf("second drain = %#v, want empty", again)
	}
}

// TestApplyGuidesMergesIntoTrailingUser 迭代 0 场景：历史尾部是本回合输入的
// user 消息，引导必须并入而不是追加——否则连续两条 user，清洗器会丢掉原始
// 输入（见 TestApplyGuidesNaiveAppendLosesInput 的反向验证）。
func TestApplyGuidesMergesIntoTrailingUser(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "帮我写个脚本"},
	}}
	a.applyGuides([]string{"用 Python 写", "加上日志"})

	if len(a.history) != 2 {
		t.Fatalf("history len = %d, want 2 (merge must not append a message)", len(a.history))
	}
	last := a.history[1]
	if last.Role != "user" {
		t.Fatalf("last role = %q, want user", last.Role)
	}
	want := "帮我写个脚本\n\n[Guide] 用 Python 写\n\n[Guide] 加上日志"
	if last.Content != want {
		t.Fatalf("merged content = %q, want %q", last.Content, want)
	}
	if v := ValidateMessageAlternation(a.history); len(v) != 0 {
		t.Fatalf("merged history has alternation violations: %v", v)
	}
}

// TestApplyGuidesMergesIntoMultimodalUser 带图输入（ContentParts）：引导作为
// text part 追加，Content 保持为空——不能同时写 Content 和 ContentParts。
func TestApplyGuidesMergesIntoMultimodalUser(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "user", ContentParts: []types.ContentPart{
			{Type: "text", Text: "看这张截图"},
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,xxx"}},
		}},
	}}
	a.applyGuides([]string{"重点看左上角"})

	if len(a.history) != 1 {
		t.Fatalf("history len = %d, want 1 (merge in place)", len(a.history))
	}
	last := a.history[0]
	if last.Content != "" {
		t.Fatalf("Content = %q, want empty (multimodal merge must touch ContentParts only)", last.Content)
	}
	if len(last.ContentParts) != 3 {
		t.Fatalf("ContentParts len = %d, want 3", len(last.ContentParts))
	}
	added := last.ContentParts[2]
	if added.Type != "text" || added.Text != "[Guide] 重点看左上角" {
		t.Fatalf("added part = %#v, want text [Guide] 重点看左上角", added)
	}
}

// TestApplyGuidesAppendsAfterToolResult 工具执行后的迭代：历史尾部是 tool
// 消息，追加带前缀的新 user 消息是合法序列。
func TestApplyGuidesAppendsAfterToolResult(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "user", Content: "列出目录"},
		{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{{ID: "call_1", Name: "list_files"}}},
		{Role: "tool", Content: "a.txt\nb.txt", ToolCallID: "call_1"},
	}}
	a.applyGuides([]string{"只要 .md 文件"})

	if len(a.history) != 4 {
		t.Fatalf("history len = %d, want 4", len(a.history))
	}
	last := a.history[3]
	if last.Role != "user" || last.Content != "[Guide] 只要 .md 文件" {
		t.Fatalf("last = %#v, want user [Guide] 只要 .md 文件", last)
	}
	if v := ValidateMessageAlternation(a.history); len(v) != 0 {
		t.Fatalf("history has alternation violations: %v", v)
	}
}

// TestApplyGuidesSkipsBlank 空白引导不产生任何消息。
func TestApplyGuidesSkipsBlank(t *testing.T) {
	a := &Agent{history: []provider.Message{{Role: "user", Content: "q"}}}
	a.applyGuides([]string{"  ", ""})
	if len(a.history) != 1 || a.history[0].Content != "q" {
		t.Fatalf("blank guides mutated history: %#v", a.history)
	}
}

// TestApplyGuidesNaiveAppendLosesInput 反向验证（血债铁律：先证明坏实现真的坏，
// 修了才看得出区别）：如果迭代 0 时无脑 append 新 user 消息，原始输入会被
// SanitizeMessageHistory 整条丢掉——这正是合并策略存在的理由。
func TestApplyGuidesNaiveAppendLosesInput(t *testing.T) {
	naive := []provider.Message{
		{Role: "user", Content: "帮我写个脚本"},
		{Role: "user", Content: "[Guide] 用 Python 写"},
	}
	if v := ValidateMessageAlternation(naive); len(v) == 0 {
		t.Fatal("naive append should be flagged as a violation")
	}
	sanitized := SanitizeMessageHistory(naive)
	if len(sanitized) != 1 || strings.Contains(sanitized[0].Content, "帮我写个脚本") {
		t.Fatalf("sanitizer should drop the OLDER user message (original input lost); got %#v", sanitized)
	}
}

// TestDrainGuidesIntoHistory 端到端：注入 → 排水并入历史 → 收件箱清空。
func TestDrainGuidesIntoHistory(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "user", Content: "起点"},
		{Role: "assistant", Content: "回复"},
	}}
	a.InjectGuide("补充说明")
	a.drainGuidesIntoHistory()

	if len(a.history) != 3 {
		t.Fatalf("history len = %d, want 3", len(a.history))
	}
	if a.history[2].Role != "user" || !strings.Contains(a.history[2].Content, "补充说明") {
		t.Fatalf("guide not merged: %#v", a.history[2])
	}
	if left := a.DrainGuides(); len(left) != 0 {
		t.Fatalf("inbox not empty after drain: %#v", left)
	}
}
