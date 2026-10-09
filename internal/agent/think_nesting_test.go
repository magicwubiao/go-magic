package agent

import (
	"strings"
	"testing"
)

// 背景（线上实测）：模型会模仿 <think> 约定，在自己的 reasoning 里也写 <think>；
// 包装方（wrapLLMReasoning / finalizeFullContent）直接拼接就得到嵌套标签
// <think>A<think>B</think>，开闭数量失衡。线上会话库最近 40 个会话、381 条
// assistant 消息里 96 条嵌套、21 条截断，单条最长 262,923 字符。
// 这组用例把"包装后必须开闭平衡、且 reasoning 正文不丢"锁死。

func TestNeutralizeThinkTagsRemovesOnlyTags(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no tags: returned as-is", "just text", "just text"},
		{"single opening tag", "A<think>B", "AB"},
		{"one tag pair", "<think>A</think>B", "AB"},
		{"nested opening tag", "The user wants X.<think>Three pages: a, b, c.", "The user wants X.Three pages: a, b, c."},
		{"case variants", "A<THINK>B</Think>C", "ABC"},
		{"tags only", "<think></think>", ""},
		{"plain less-than signs must survive", "if a < b && c > d", "if a < b && c > d"},
		{"tag-like text must survive", "a<thin b<thinker", "a<thin b<thinker"},
		{"accumulated over turns", "R1</think>T1<think>R2</think>T2", "R1T1R2T2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := neutralizeThinkTags(tc.in); got != tc.want {
				t.Fatalf("neutralizeThinkTags(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestWrapLLMReasoningNeverNestsTags 是本轮修复的核心回归：
// 旧的 wrapLLMReasoning 在 reasoning 自带 <think> 时产出
// "<think>A<think>B</think>\ncontent"（2 开 1 闭），旧代码下本用例必红。
func TestWrapLLMReasoningNeverNestsTags(t *testing.T) {
	reasoning := "The user wants to change the nav.<think>Three pages: index/docs/contact."
	content := "Navigation updated as requested."

	got := wrapLLMReasoning(reasoning, content)

	if n, m := strings.Count(got, "<think>"), strings.Count(got, "</think>"); n != m {
		t.Fatalf("unbalanced tags: %d open vs %d close\n%s", n, m, got)
	}
	if n, m := strings.Count(got, "<think>"), strings.Count(got, "</think>"); n != 1 || m != 1 {
		t.Fatalf("expected exactly one think block, got %d open %d close", n, m)
	}
	if !strings.HasPrefix(got, "<think>") {
		t.Fatalf("output must start with a think block, got: %q", got)
	}
	if !strings.HasSuffix(got, content) {
		t.Fatalf("the body must be preserved verbatim at the end, got: %q", got)
	}
	// reasoning 的文字内容不能因为去标签而丢失。
	if !strings.Contains(got, "Three pages: index/docs/contact.") {
		t.Fatalf("reasoning text was dropped: %q", got)
	}
	if strings.Contains(got, "<think>Three pages") {
		t.Fatalf("the nested opening tag was not neutralized: %q", got)
	}
}

// 空 reasoning / 空正文的既有语义不能被这次改动破坏。
func TestWrapLLMReasoningEdgeCases(t *testing.T) {
	if got := wrapLLMReasoning("", "answer"); got != "answer" {
		t.Fatalf("without reasoning the body should be returned as-is, got %q", got)
	}
	if got := wrapLLMReasoning("thinking", ""); got != "" {
		t.Fatalf("with no body, reasoning must not masquerade as the answer, got %q", got)
	}
	if got := wrapLLMReasoning("<think>", "answer"); got != "answer" {
		t.Fatalf("when reasoning is only tags it should degrade to the plain body, got %q", got)
	}
}

// 流式推送路径：provider 会把 "<think>" 切成多个 token，逐 chunk 中和必须保证
// 半截标签不会被推给客户端（否则前端拼接后又看到标签）。
func TestThinkStreamNeutralizerHoldsBackSplitTags(t *testing.T) {
	// 极端情形：逐字符喂，标签被切得最碎。
	n := &thinkStreamNeutralizer{}
	var out strings.Builder
	chunks := []string{}
	for _, r := range "Hi<think>secret</think>Bye" {
		c := n.push(string(r))
		chunks = append(chunks, c)
		out.WriteString(c)
	}
	out.WriteString(n.flush())

	got := out.String()
	if got != "HisecretBye" {
		t.Fatalf("char-by-char stream neutralized = %q, want %q (chunks=%q)", got, "HisecretBye", chunks)
	}
	if strings.Contains(got, "<think") || strings.Contains(got, "</think") {
		t.Fatalf("output still contains tags: %q", got)
	}
}

// 单 chunk 内出现完整标签时立刻中和，不做多余暂存。
func TestThinkStreamNeutralizerSingleChunk(t *testing.T) {
	n := &thinkStreamNeutralizer{}
	if got := n.push("a<think>b</think>c"); got != "abc" {
		t.Fatalf("single chunk neutralized = %q, want %q", got, "abc")
	}
	if tail := n.flush(); tail != "" {
		t.Fatalf("flush must be empty when nothing is pending, got %q", tail)
	}
}

// 暂存只能在流结束时吐出，且不能吞掉真实文本。
// 关键回归点：末尾是"像标签前缀"的普通文本（"x<thin"），
// 若下一个 chunk 补成 "<thing>" 这种非标签文本，必须原样还原。
func TestThinkStreamNeutralizerFlushAndFalsePositive(t *testing.T) {
	n := &thinkStreamNeutralizer{}
	if got := n.push("abc<thi"); got != "abc" {
		t.Fatalf("the suspicious suffix should be held back, got %q", got)
	}
	if got := n.push("ng>d"); got != "<thing>d" {
		t.Fatalf("<thing> is not a tag and must be preserved as-is, got %q", got)
	}
	if got := n.flush(); got != "" {
		t.Fatalf("flush must be empty, got %q", got)
	}

	n2 := &thinkStreamNeutralizer{}
	if got := n2.push("tail<"); got != "tail" {
		t.Fatalf("the trailing '<' should be held back, got %q", got)
	}
	if got := n2.flush(); got != "<" {
		t.Fatalf("stream end must flush the pending tail, got %q", got)
	}
}
