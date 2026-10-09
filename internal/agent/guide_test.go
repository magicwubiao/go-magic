package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestGuideInboxRoundTrip 验证收件箱 FIFO、排水后清空、空白文本忽略，
// 以及 id 在两种排水视图下的一致性。
func TestGuideInboxRoundTrip(t *testing.T) {
	a := &Agent{}
	a.InjectGuide("g1", "do A first")
	a.InjectGuide("g2", "   ") // 空白：必须被忽略
	a.InjectGuide("g3", "then do B")

	got := a.DrainGuides()
	if len(got) != 2 || got[0] != "do A first" || got[1] != "then do B" {
		t.Fatalf("drain = %#v, want [do A first then do B]", got)
	}
	if again := a.DrainGuides(); len(again) != 0 {
		t.Fatalf("second drain = %#v, want empty", again)
	}

	// 带 id 的排水视图：server 收尾回收依赖它把「未消费的引导」还原成
	// 与落库消息同 id 的排队回合（收件箱是一次性排水，两个视图不可叠加使用）。
	a.InjectGuide("g4", "closing addendum")
	items := a.DrainGuideItems()
	if len(items) != 1 || items[0].ID != "g4" || items[0].Text != "closing addendum" {
		t.Fatalf("item drain = %#v, want [{g4 closing addendum}]", items)
	}
	if left := a.DrainGuides(); len(left) != 0 {
		t.Fatalf("drain after item drain = %#v, want empty", left)
	}
}

// TestApplyGuidesMergesIntoTrailingUser 迭代 0 场景：历史尾部是本回合输入的
// user 消息，引导必须并入而不是追加——否则连续两条 user，清洗器会丢掉原始
// 输入（见 TestApplyGuidesNaiveAppendLosesInput 的反向验证）。
func TestApplyGuidesMergesIntoTrailingUser(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "write me a script"},
	}}
	a.applyGuides([]string{"write it in Python", "add logging"})

	if len(a.history) != 2 {
		t.Fatalf("history len = %d, want 2 (merge must not append a message)", len(a.history))
	}
	last := a.history[1]
	if last.Role != "user" {
		t.Fatalf("last role = %q, want user", last.Role)
	}
	want := "write me a script\n\n[Guide] write it in Python\n\n[Guide] add logging"
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
			{Type: "text", Text: "look at this screenshot"},
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,xxx"}},
		}},
	}}
	a.applyGuides([]string{"focus on the top-left corner"})

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
	if added.Type != "text" || added.Text != "[Guide] focus on the top-left corner" {
		t.Fatalf("added part = %#v, want text [Guide] focus on the top-left corner", added)
	}
}

// TestApplyGuidesAppendsAfterToolResult 工具执行后的迭代：历史尾部是 tool
// 消息，追加带前缀的新 user 消息是合法序列。
func TestApplyGuidesAppendsAfterToolResult(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "user", Content: "list the directory"},
		{Role: "assistant", Content: "", ToolCalls: []types.ToolCall{{ID: "call_1", Name: "list_files"}}},
		{Role: "tool", Content: "a.txt\nb.txt", ToolCallID: "call_1"},
	}}
	a.applyGuides([]string{"only .md files"})

	if len(a.history) != 4 {
		t.Fatalf("history len = %d, want 4", len(a.history))
	}
	last := a.history[3]
	if last.Role != "user" || last.Content != "[Guide] only .md files" {
		t.Fatalf("last = %#v, want user [Guide] only .md files", last)
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
		{Role: "user", Content: "write me a script"},
		{Role: "user", Content: "[Guide] write it in Python"},
	}
	if v := ValidateMessageAlternation(naive); len(v) == 0 {
		t.Fatal("naive append should be flagged as a violation")
	}
	sanitized := SanitizeMessageHistory(naive)
	if len(sanitized) != 1 || strings.Contains(sanitized[0].Content, "write me a script") {
		t.Fatalf("sanitizer should drop the OLDER user message (original input lost); got %#v", sanitized)
	}
}

// TestDrainGuidesIntoHistory 端到端：注入 → 排水并入历史 → 收件箱清空。
func TestDrainGuidesIntoHistory(t *testing.T) {
	a := &Agent{history: []provider.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", Content: "reply"},
	}}
	a.InjectGuide("g1", "additional note")
	a.drainGuidesIntoHistory()

	if len(a.history) != 3 {
		t.Fatalf("history len = %d, want 3", len(a.history))
	}
	if a.history[2].Role != "user" || !strings.Contains(a.history[2].Content, "additional note") {
		t.Fatalf("guide not merged: %#v", a.history[2])
	}
	if left := a.DrainGuides(); len(left) != 0 {
		t.Fatalf("inbox not empty after drain: %#v", left)
	}
}
