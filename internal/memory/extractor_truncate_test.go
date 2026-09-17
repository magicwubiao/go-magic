package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestExtractMemoriesTruncatesMultibyteContent 回归测试：抽取提示词构建时
// 对每条消息的截断必须按 rune 计数。
//
// 历史 bug：`if len(content) > 1000`（字节）配合 `[]rune(content)[:1000]`
// 会在中文等宽字符场景下 panic —— 中文一个字符占 3 字节，字节数超限但
// rune 数不足 1000，切片直接越界：
//
//	panic: slice bounds out of range [:1000] with capacity 768
//
// 这里构造若干"字节超限、rune 不足"的输入，断言调用不 panic 且内容被截断。
func TestExtractMemoriesTruncatesMultibyteContent(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		// 复现线上 panic 的规模：700 个中文字 = 2100 字节 > 1000，
		// 而 []rune 的 cap 恰好是 768 < 1000 →
		// "slice bounds out of range [:1000] with capacity 768"。
		// 注意：并非所有"字节超限"的中文串都会 panic —— cap 会向上取整，
		// 例如 900 runes 的 cap 是 1024，[:1000] 反而落在容量内不报错。
		// 因此这里必须覆盖若干个真实的 panic 规模（cap < 1000）。
		{"chinese_700_repro", strings.Repeat("中", 700)}, // cap=768，线上同款
		{"chinese_500", strings.Repeat("中", 500)},       // cap=512
		{"chinese_600", strings.Repeat("中", 600)},       // cap=672
		{"chinese_800", strings.Repeat("中", 800)},       // cap=800
		{"chinese_334_min", strings.Repeat("中", 334)},   // cap=352，最小越界规模
		// 中英混排（每 4 字节 3 rune）
		{"mixed", strings.Repeat("abc中文", 400)},
		// 超长纯 ASCII（字节与 rune 一致，容量足够）
		{"ascii_5000", strings.Repeat("a", 5000)},
		// 边界：rune 正好 1000 / 1001
		{"chinese_exactly_1000", strings.Repeat("中", 1000)},
		{"chinese_1001", strings.Repeat("中", 1001)},
		// 空内容
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 用捕获型 provider 记录实际送进提示词的内容，再返回空数组，
			// 从而只验证提示词构建阶段（panic 发生地）而不依赖真实 LLM。
			cap := &capturingProvider{}
			ext := NewMemoryExtractor(cap, nil, DefaultMemoryExtractorConfig())

			// 不应 panic
			_, err := ext.ExtractMemories(context.Background(), []provider.Message{
				{Role: "user", Content: tc.body},
				{Role: "assistant", Content: tc.body},
			}, "test-session")
			if err != nil {
				t.Fatalf("ExtractMemories returned error: %v", err)
			}

			if cap.lastPrompt == "" {
				t.Fatal("provider was never called; prompt not built")
			}
			// 超过上限的内容必须已被截断：完整原文不应整段出现在提示词里
			if len([]rune(tc.body)) > extractContentMaxRunes {
				if strings.Contains(cap.lastPrompt, tc.body) {
					t.Fatalf("oversized content (%d runes) was not truncated",
						len([]rune(tc.body)))
				}
			}
		})
	}
}

// capturingProvider 记录最后一次收到的提示词，不产生任何网络调用。
type capturingProvider struct {
	lastPrompt string
}

func (c *capturingProvider) Name() string { return "capturing" }

func (c *capturingProvider) Chat(_ context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	for _, m := range messages {
		if m.Role == "user" {
			c.lastPrompt = m.Content
		}
	}
	return &provider.ChatResponse{Content: "[]"}, nil
}
