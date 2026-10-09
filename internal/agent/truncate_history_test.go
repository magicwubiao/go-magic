package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// newTruncationAgent returns a minimally wired Agent suitable for exercising
// truncateHistory in isolation. Compression is disabled so that the test can
// distinguish byte-level truncation from LLM-driven summarisation; each test
// sets its own maxTotalLen to control the threshold.
func newTruncationAgent(maxTotalLen int) *Agent {
	return &Agent{
		maxTotalLen: maxTotalLen,
	}
}

// TestTruncateHistory_PreservesLastUserMessage reproduces the exact symptom
// from the zhipu/GLM 1214 bug: a long running conversation had
// (s=1, u=0, t=18) — only system, assistant+tool_calls, and tool messages,
// no user at all. The root cause was that the per-iteration "else" branch of
// truncateHistory would delete a single message regardless of role, sweeping
// every user message out of the history one by one.
//
// After the rewrite, the LAST user-role message (and everything that follows
// it) becomes a "protected tail block" that the byte-level trimer never
// touches. This test asserts that invariant directly.
func TestTruncateHistory_PreservesLastUserMessage(t *testing.T) {
	a := newTruncationAgent(60)

	// 4 large user blocks; each block is user + assistant+tool_calls + tool.
	// Push the total well above the 60-char cap so every iteration has to
	// delete something.
	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: strings.Repeat("U1", 60)}, // huge block 1
		{Role: "assistant", Content: "a1", ToolCalls: []types.ToolCall{
			{ID: "tc1", Type: "function", Function: types.Function{Name: "ls"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "result1"},
		{Role: "user", Content: strings.Repeat("U2", 60)}, // huge block 2
		{Role: "assistant", Content: "a2", ToolCalls: []types.ToolCall{
			{ID: "tc2", Type: "function", Function: types.Function{Name: "ls"}},
		}},
		{Role: "tool", ToolCallID: "tc2", Content: "result2"},
		{Role: "user", Content: "recent input"}, // last user — must survive
		{Role: "assistant", Content: "answer", ToolCalls: nil},
	}

	a.truncateHistory()

	// Invariant 1: at least one user message remains.
	hasUser := false
	for _, m := range a.history {
		if m.Role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Fatalf("truncateHistory erased every user message (reproduces zhipu 1214 s=1 u=0): %+v", a.history)
	}

	// Invariant 2: the most recent user input is still there.
	last := a.history[len(a.history)-1]
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == "user" {
			last = a.history[i]
			break
		}
	}
	if last.Content != "recent input" {
		t.Fatalf("most recent user input lost; kept %q", last.Content)
	}

	// Invariant 3: the protected tail block (last user + trailing assistant)
	// stayed intact together — never half-deleted.
	tail := a.history[len(a.history)-2:]
	roles := []string{tail[0].Role, tail[1].Role}
	if roles[0] != "user" || roles[1] != "assistant" {
		t.Fatalf("protected tail block was damaged; got roles %v", roles)
	}
}

// TestTruncateHistory_PreservesZhipu1214Shape guards the production failure
// shape from RC3: s=1, u=0, t=18 was the exact messageShapeSummary that
// surfaced from a real zhipu/GLM run. After the fix, the same input must
// keep userCnt >= 1 even after aggressive truncation.
func TestTruncateHistory_PreservesZhipu1214Shape(t *testing.T) {
	a := newTruncationAgent(40)

	// Build a long tool-heavy dialog (mimicking the real-world shape: many
	// tool rounds, no further user inputs after the first — exactly the
	// pattern that triggered 1214 in production).
	a.history = []provider.Message{
		{Role: "system", Content: "sys-prompt"},
		{Role: "user", Content: "first and only user input"},
	}
	for i := 0; i < 20; i++ {
		a.history = append(a.history,
			provider.Message{
				Role: "assistant", Content: "thinking...",
				ToolCalls: []types.ToolCall{
					{ID: idForRound(i), Type: "function",
						Function: types.Function{Name: "ls"}},
				},
			},
			provider.Message{
				Role: "tool", ToolCallID: idForRound(i),
				Content: strings.Repeat("data", 30),
			},
		)
	}

	a.truncateHistory()

	var userCnt, sysCnt, toolCnt int
	for _, m := range a.history {
		switch m.Role {
		case "user":
			userCnt++
		case "system":
			sysCnt++
		case "tool":
			toolCnt++
		}
	}

	if userCnt < 1 {
		t.Fatalf("history has 0 user messages (production 1214 shape): sys=%d user=%d tool=%d -> %+v",
			sysCnt, userCnt, toolCnt, a.history)
	}
}

// TestTruncateHistory_UserBlockIsRemovedAtomically verifies that when a
// non-trailing user block IS deleted, every assistant/tool/assistant entry
// belonging to it goes with it (rather than leaving an orphan tool result
// after the user is removed, which would trip Pass 1 of sanitizeHistory and
// force an unwanted drop on the next provider call).
func TestTruncateHistory_UserBlockIsRemovedAtomically(t *testing.T) {
	a := newTruncationAgent(80)

	a.history = []provider.Message{
		{Role: "system", Content: "sys"},
		// Big user block A — must be deleted wholesale once the cap forces
		// reduction past this index.
		{Role: "user", Content: strings.Repeat("x", 200)},
		{Role: "assistant", Content: "thinking-A", ToolCalls: []types.ToolCall{
			{ID: "A1", Type: "function", Function: types.Function{Name: "ls"}},
			{ID: "A2", Type: "function", Function: types.Function{Name: "pwd"}},
		}},
		{Role: "tool", ToolCallID: "A1", Content: "ls-out"},
		{Role: "tool", ToolCallID: "A2", Content: "pwd-out"},
		{Role: "assistant", Content: "interim-A"},
		// Tiny user block B + tail — protected.
		{Role: "user", Content: "tiny-B"},
		{Role: "assistant", Content: "tail-answer", ToolCalls: nil},
	}

	a.truncateHistory()

	// No assistant with tool_calls "A1"/"A2" should remain (the whole block
	// was deleted). If block removal were per-message, the assistant-with-
	// tool_calls might survive while the user is dropped, breaking the
	// tool_call ↔ tool-result pairing.
	for _, m := range a.history {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID == "A1" || tc.ID == "A2" {
					t.Fatalf("orphan tool_call %q survived (user block was not removed atomically): %+v",
						tc.ID, a.history)
				}
			}
		}
		if m.Role == "tool" {
			if m.ToolCallID == "A1" || m.ToolCallID == "A2" {
				t.Fatalf("orphan tool result %q survived (user block was not removed atomically): %+v",
					m.ToolCallID, a.history)
			}
		}
	}
	if len(a.history) == 0 {
		t.Fatalf("history was wiped entirely")
	}
}

// TestTruncateHistory_NoUserMessagesEverywhereSurvives covers the worst
// pre-existing case: a 2-message history containing only an over-long
// user message plus a recent user message. The first user block must be
// trimmed, but the last user block stays, and the system prompt (if any)
// stays at the head.
func TestTruncateHistory_NoUserMessagesEverywhereSurvives(t *testing.T) {
	a := newTruncationAgent(40)

	a.history = []provider.Message{
		{Role: "system", Content: "system-prompt"},
		{Role: "user", Content: strings.Repeat("Z", 200)},
		{Role: "user", Content: "latest user input"},
	}

	a.truncateHistory()

	// System at head, latest user at tail with content intact.
	if len(a.history) == 0 || a.history[0].Role != "system" {
		t.Fatalf("system prompt was dropped from head: %+v", a.history)
	}
	last := a.history[len(a.history)-1]
	if last.Role != "user" || last.Content != "latest user input" {
		t.Fatalf("last user message was damaged: got %+v", last)
	}
}

// TestTruncateHistory_DoesNotLoopForeverOnSingleHugeTail asserts the safety
// cap: when even the protected tail block alone is bigger than maxTotalLen
// (an extreme edge case — caller appends a multi-MB input), truncateHistory
// must terminate. The fix delegates this case to compressHistory (which is
// a no-op for short tails) and then runs sanitiser; the test mainly guards
// against an infinite loop in production by giving the call a budget of
// ~2s — Go's testing framework will fail with a clear timeout otherwise.
func TestTruncateHistory_DoesNotLoopForeverOnSingleHugeTail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping infinite-loop guard test in -short mode")
	}

	a := newTruncationAgent(10)
	a.compressionEnabled = false // belt-and-braces; isolate byte-level path

	a.history = []provider.Message{
		{Role: "user", Content: strings.Repeat("Q", 5000)},
	}

	// Run inside a goroutine with a hard timeout. If truncateHistory
	// spins, the parent test will time out instead of hanging.
	done := make(chan struct{})
	go func() {
		a.truncateHistory()
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("truncateHistory did not return within 2s; it likely spun on the protected tail forever")
	}

	// Whatever truncateHistory returned, the user message is preserved
	// (not deleted in the name of shrinking bytes).
	if len(a.history) == 0 {
		t.Fatalf("history was wiped entirely")
	}
	last := a.history[len(a.history)-1]
	if last.Role != "user" {
		t.Fatalf("protected tail vanished under single-message truncation: %+v", a.history)
	}
}

// TestHistoryHardLimitNeverPreemptsCompression 锁死"硬截断晚于压缩触发"这个
// 不等式。
//
// 两条路径：压缩（compressor 的 LLM 摘要）会把中段摘要成可接力的记录；字节级
// 截断则直接删整段 user 块、不留摘要。所以硬上限必须恒 ≥ 压缩触发点。触发点是
// **配置**出来的（agent.compress_threshold_tokens → ThresholdTokens×4 字节），
// 硬上限却是构造时写死的 200000 字节 ⇒ 阈值一旦超过 50000 token，顺序就反过来，
// 模型会在没有任何摘要的情况下永久丢掉中段上下文。
func TestHistoryHardLimitNeverPreemptsCompression(t *testing.T) {
	// 默认 32K token：触发点 128K < 固定上限 200K ⇒ 顺序不变、行为零变化。
	def := &Agent{maxTotalLen: 200000, compressor: compress.NewCompressor(defaultCompressThresholdTokens)}
	if got := def.historyHardLimit(); got != 200000 {
		t.Fatalf("default hard cap = %d, want 200000 (ordering must stay unchanged)", got)
	}

	// 阈值抬到 60000 token：触发点 240K > 200K ⇒ 硬上限必须跟着抬起来。
	const raisedTokens = 60000
	raised := &Agent{maxTotalLen: 200000, compressor: compress.NewCompressor(raisedTokens)}
	trigger := raisedTokens * 4
	if got := raised.historyHardLimit(); got <= trigger {
		t.Fatalf("hard cap %d must stay above the compression trigger %d "+
			"(otherwise byte-truncation preempts compression and drops blocks with no summary)", got, trigger)
	}

	// 阈值低于固定上限时，硬上限不得被压低（老行为保留）。
	lowered := &Agent{maxTotalLen: 200000, compressor: compress.NewCompressor(16000)}
	if got := lowered.historyHardLimit(); got != 200000 {
		t.Fatalf("hard cap = %d, want 200000 (a lower compression threshold must not shrink it)", got)
	}

	// 未接线压缩的调用方（compressor == nil）仍以 maxTotalLen 为准。
	bare := &Agent{maxTotalLen: 200000}
	if got := bare.historyHardLimit(); got != 200000 {
		t.Fatalf("nil-compressor hard cap = %d, want 200000", got)
	}
}

// TestTruncateHistory_DoesNotDropBlocksBelowDerivedCap 是上一条的行为侧验证：
// 阈值抬高到硬上限跟着变高之后，一段"超过固定 200K、但低于派生的硬上限"的历史
// 必须原样返回 —— 字节级路径一次都不许动手，压缩才有机会先发生。
func TestTruncateHistory_DoesNotDropBlocksBelowDerivedCap(t *testing.T) {
	a := &Agent{maxTotalLen: 200000, compressor: compress.NewCompressor(60000)}

	a.history = []provider.Message{{Role: "system", Content: "sys"}}
	// 3 个各 90K 字节的 user 块 + 尾部 user（保护块）：合计 ~270K 字节，
	// 越过固定的 200K，但低于 60000×4×5/4 = 300K。
	for i := 0; i < 3; i++ {
		a.history = append(a.history, provider.Message{Role: "user", Content: strings.Repeat("A", 90000)})
	}
	a.history = append(a.history, provider.Message{Role: "user", Content: "latest"})
	before := len(a.history)

	a.truncateHistory()

	if len(a.history) != before {
		t.Fatalf("history was byte-truncated (%d -> %d msgs) while still below the derived hard cap; "+
			"compression must get its chance first", before, len(a.history))
	}
}

// idForRound returns a deterministic tool-call id for the unit-test rounds.
// Avoiding fmt in this hot loop keeps the test allocation-light.
func idForRound(i int) string {
	const digits = "0123456789abcdef"
	id := "tc"
	if i == 0 {
		return id + "0"
	}
	for i > 0 {
		id += string(digits[i&0xf])
		i >>= 4
	}
	return id
}
