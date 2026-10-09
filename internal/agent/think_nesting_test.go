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
		{"无标签原样返回", "just text", "just text"},
		{"单个开标签", "A<think>B", "AB"},
		{"单对标签", "<think>A</think>B", "AB"},
		{"嵌套开标签", "The user wants X.<think>Three pages: a, b, c.", "The user wants X.Three pages: a, b, c."},
		{"大小写变体", "A<THINK>B</Think>C", "ABC"},
		{"只有标签", "<think></think>", ""},
		{"普通小于号必须保留", "if a < b && c > d", "if a < b && c > d"},
		{"近标签文本必须保留", "a<thin b<thinker", "a<thin b<thinker"},
		{"多轮累积", "R1</think>T1<think>R2</think>T2", "R1T1R2T2"},
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
	content := "已按要求调整导航。"

	got := wrapLLMReasoning(reasoning, content)

	if n, m := strings.Count(got, "<think>"), strings.Count(got, "</think>"); n != m {
		t.Fatalf("标签开闭失衡：%d 开 vs %d 闭\n%s", n, m, got)
	}
	if n, m := strings.Count(got, "<think>"), strings.Count(got, "</think>"); n != 1 || m != 1 {
		t.Fatalf("应恰好包一层 think 块，实际 %d 开 %d 闭", n, m)
	}
	if !strings.HasPrefix(got, "<think>") {
		t.Fatalf("必须以 think 块开头，实际：%q", got)
	}
	if !strings.HasSuffix(got, content) {
		t.Fatalf("正文必须原样保留在末尾，实际：%q", got)
	}
	// reasoning 的文字内容不能因为去标签而丢失。
	if !strings.Contains(got, "Three pages: index/docs/contact.") {
		t.Fatalf("reasoning 正文被误删：%q", got)
	}
	if strings.Contains(got, "<think>Three pages") {
		t.Fatalf("嵌套开标签未被中和：%q", got)
	}
}

// 空 reasoning / 空正文的既有语义不能被这次改动破坏。
func TestWrapLLMReasoningEdgeCases(t *testing.T) {
	if got := wrapLLMReasoning("", "answer"); got != "answer" {
		t.Fatalf("无 reasoning 时应原样返回正文，实际 %q", got)
	}
	if got := wrapLLMReasoning("thinking", ""); got != "" {
		t.Fatalf("无正文时不得拿 reasoning 冒充回答，实际 %q", got)
	}
	if got := wrapLLMReasoning("<think>", "answer"); got != "answer" {
		t.Fatalf("reasoning 只剩标签时应退化为纯正文，实际 %q", got)
	}
}
