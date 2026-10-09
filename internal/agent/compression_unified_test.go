package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// longChatHistory builds a system message followed by n user/assistant pairs,
// each user message being `size` bytes.
func longChatHistory(n, size int) []provider.Message {
	out := []provider.Message{{Role: "system", Content: "sys"}}
	for i := 0; i < n; i++ {
		out = append(out,
			provider.Message{Role: "user", Content: strings.Repeat("u", size)},
			provider.Message{Role: "assistant", Content: "ok"},
		)
	}
	return out
}

func countCompactionSummaries(a *Agent) int {
	n := 0
	for _, m := range a.history {
		if strings.Contains(m.Content, compress.SummaryPrefix) {
			n++
		}
	}
	return n
}

// TestTruncateHistorySummarisesWithoutLegacyEnableFlag 锁死接线修复：
// truncateHistory 的破坏性分支必须看"摘要器是否装配"（compressor != nil，即
// web / gateway / bot / cron 走的 WithCompression），而**不是**看 TUI /compress
// 才设置的旧 compressionEnabled 布尔量。
//
// 旧行为（本用例应当判红）：四个非 TUI 入口从不置 compressionEnabled ⇒
// 兜底永远不触发 ⇒ sanitize 之后把超长历史原样发给模型；而真正的摘要压缩
// （compressor）明明已经装配好了，却在这条路径上被完全绕过。
func TestTruncateHistorySummarisesWithoutLegacyEnableFlag(t *testing.T) {
	// 3.2KB 历史 > 硬上限 3000（阈值 100 token 的触发点只有 400 字节，
	// 所以硬上限仍由 maxTotalLen 决定）。
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 3000, compressor: compress.NewCompressor(100)}
	a.history = longChatHistory(8, 400)
	before := len(a.history)

	a.truncateHistory()

	if got := countCompactionSummaries(a); got != 1 {
		t.Fatalf("truncateHistory did not ask the summariser to take over (summaries=%d, msgs %d -> %d); "+
			"the compressor is wired and the history is over the cap, so shrinking it without a "+
			"summary is exactly the 10-08 \"read it, then forgot it\" data-loss shape",
			got, before, len(a.history))
	}
	if len(a.history) >= before {
		t.Fatalf("history was not compacted at all: %d -> %d", before, len(a.history))
	}
	// The protected tail (the newest user input) must survive verbatim — the
	// summariser protects head+tail, byte truncation does not.
	last := a.history[len(a.history)-1]
	if last.Role != "assistant" || last.Content != "ok" {
		t.Fatalf("protected tail was damaged: %+v", last)
	}
}

// TestTruncateHistoryLeavesBelowCapHistoryUntouched 是 compressionRatio 恒为 0
// 那个 bug 的行为侧守卫。
//
// 旧代码的第一道判定是 `compressionEnabled && totalLen > limit*compressionRatio`，
// 而 SetCompressionRatio 全仓库零调用 ⇒ ratio 恒为 0 ⇒ `totalLen > 0` 恒真 ⇒
// 只要用户敲过一次 TUI 的 /compress，**每一次** truncateHistory 都会跑老的规则式
// compressHistory()：8 条 user 消息已经越过它 keepFirst(2)+keepRecent(4) 的门槛，
// 中段会被无条件换成"前 2 + 后 4"，与 token 阈值、与历史大小都无关。
//
// 现在：历史远低于硬上限 ⇒ 一个字节都不许动。
func TestTruncateHistoryLeavesBelowCapHistoryUntouched(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 200000, compressor: compress.NewCompressor(32000)}
	a.history = longChatHistory(8, 40) // ~340 bytes, far below the 200000-byte cap

	want := make([]string, 0, len(a.history))
	wantRoles := make([]string, 0, len(a.history))
	for _, m := range a.history {
		want = append(want, m.Content)
		wantRoles = append(wantRoles, m.Role)
	}

	a.truncateHistory()

	if len(a.history) != len(want) {
		t.Fatalf("below-cap history was rewritten: %d -> %d msgs", len(want), len(a.history))
	}
	for i, m := range a.history {
		if m.Content != want[i] || m.Role != wantRoles[i] {
			t.Fatalf("below-cap history changed at index %d (%s/%q -> %s/%q)",
				i, wantRoles[i], want[i], m.Role, m.Content)
		}
	}
	if got := countCompactionSummaries(a); got != 0 {
		t.Fatalf("below-cap history was summarised (%d summaries); the token threshold was never crossed", got)
	}
}

// TestCompressNowBypassesTokenThreshold 覆盖 TUI /compress 的正确语义：阈值只
// 约束自动触发（maybeCompressContext），用户显式敲的命令必须立刻生效。
func TestCompressNowBypassesTokenThreshold(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 200000}
	a.compressor = compress.NewCompressor(1 << 30) // 阈值高到自动触发永不发生
	a.history = longChatHistory(8, 400)

	a.maybeCompressContext()
	if got := countCompactionSummaries(a); got != 0 {
		t.Fatalf("threshold-gated path compressed without crossing the threshold (summaries=%d)", got)
	}

	if !a.CompressNow() {
		t.Fatalf("CompressNow reported no progress even though the history is summarisable")
	}
	if got := countCompactionSummaries(a); got != 1 {
		t.Fatalf("CompressNow claimed progress but left %d summary message(s)", got)
	}
}

// TestTruncateHistorySystemPromptCutStaysValidUTF8 覆盖第三处：maxSystemLen 是
// **字节**预算，裸的 Content[:maxSystemLen] 会劈开多字节 rune。
//
// 30000 个「中」= 90000 字节，而 50000 = 3×16666 + 2 ⇒ 切点落在第 16667 个字符
// 内部、后面还有 1 个字节 ⇒ 旧实现产出的字符串必然是非法 UTF-8（严格 provider
// 直接 400，宽松 provider 静默替换成 U+FFFD）。
func TestTruncateHistorySystemPromptCutStaysValidUTF8(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus(), maxTotalLen: 1000} // compressor nil：只剩字节级路径
	a.history = []provider.Message{
		{Role: "system", Content: strings.Repeat("中", 30000)},
		{Role: "user", Content: "hi"},
	}

	a.truncateHistory()

	if len(a.history) == 0 || a.history[0].Role != "system" {
		t.Fatalf("system prompt was moved or dropped: %+v", a.history)
	}
	sys := a.history[0].Content
	if len(sys) >= 90000 {
		t.Fatalf("system prompt was not capped: %d bytes", len(sys))
	}
	if !utf8.ValidString(sys) {
		tail := sys
		if len(tail) > 24 {
			tail = tail[len(tail)-24:]
		}
		t.Fatalf("system prompt is no longer valid UTF-8 (the byte cut split a multi-byte rune); tail=%q", tail)
	}
	if !strings.HasSuffix(sys, "[...system prompt truncated...]") {
		t.Fatalf("truncation marker missing (len=%d)", len(sys))
	}
}

// TestTruncateBytesOnRuneBoundary 直接覆盖边界助手的四类情形。
func TestTruncateBytesOnRuneBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		in       string
		maxBytes int
		want     string
	}{
		{"fits untouched", "中文", 6, "中文"},
		{"cut mid-rune backs off", "中文", 5, "中"},
		{"cut mid-rune backs off (2 bytes in)", "中文", 4, "中"},
		{"exact boundary", "中文", 3, "中"},
		{"zero budget", "中文", 0, ""},
		{"ascii unaffected", "abcd", 2, "ab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateBytesOnRuneBoundary(tc.in, tc.maxBytes)
			if got != tc.want {
				t.Fatalf("truncateBytesOnRuneBoundary(%q, %d) = %q, want %q", tc.in, tc.maxBytes, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("result is not valid UTF-8: %q", got)
			}
		})
	}
}
