package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// TestContextBlocksStablePrefixFirst 锁死 withContextBlocks 的「稳定优先」顺序。
//
// 前缀缓存（OpenAI/Gemini 服务端自动缓存、Anthropic 显式 cache_control）按**最长
// 公共前缀**命中。旧顺序把最易变的 [Active Plan] 排在最前，等于每轮第一个注入段就
// 变，可缓存前缀长度恒为 0 —— 几百轮任务里 system / 规则链 / 工作目录每轮原价重发。
//
// 这里模拟两轮（记忆与在案计划都发生变化），断言：
//  1. system prompt → 规则链 → [Workspace] 三段逐字节不变；
//  2. 首个出现差异的位置正好是**动态记忆块**（index 3）；
//  3. 最易变的 [Active Plan] 排在最后，紧挨对话。
func TestContextBlocksStablePrefixFirst(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	ag.SetSession("sess-prefix")
	// 顺序有讲究：SetMemoryScope 会清空 dynamicMemory，必须先设 scope。
	ag.SetMemoryScope("/projects/demo")
	// ruleDir 为空 ⇒ ensureRuleContext 直接返回，手工设置的 ruleContext 原样保留。
	ag.ruleContext = "[Rules]\nAGENTS.md: always run gofmt."

	plan := func(n int) func(string) []tool.TodoPlanEntry {
		return func(string) []tool.TodoPlanEntry {
			out := make([]tool.TodoPlanEntry, 0, n)
			for i := 0; i < n; i++ {
				out = append(out, tool.TodoPlanEntry{ID: "todo_step", Title: "step", Status: "pending"})
			}
			return out
		}
	}

	ag.history = []provider.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "go"},
	}

	// --- turn 1 ---
	ag.dynamicMemory = "recalled memory A"
	ag.todoPlanSnapshot = plan(1)
	first := ag.buildLLMMessages()

	// --- turn 2：记忆与在案计划都变了 ---
	ag.dynamicMemory = "recalled memory B (different content)"
	ag.todoPlanSnapshot = plan(3)
	second := ag.buildLLMMessages()

	const stableLen = 3 // system prompt + 规则链 + [Workspace]
	if len(first) < stableLen+3 || len(second) < stableLen+3 {
		t.Fatalf("outbound request too short: first=%d second=%d (want >= 6)", len(first), len(second))
	}

	// ① 稳定前缀逐字节相同。
	for i := 0; i < stableLen; i++ {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("stable prefix diverged at index %d:\n  turn1: role=%s content=%q\n  turn2: role=%s content=%q",
				i, first[i].Role, first[i].Content, second[i].Role, second[i].Content)
		}
	}

	// ② 顺序断言：规则 → workspace → 记忆 → 计划。
	if first[1].Content != ag.ruleContext {
		t.Fatalf("index 1 must be the static rule chain, got %q", first[1].Content)
	}
	if !strings.HasPrefix(first[2].Content, "[Workspace]") {
		t.Fatalf("index 2 must be the [Workspace] block, got %q", first[2].Content)
	}
	if !strings.Contains(first[3].Content, "recalled memory A") {
		t.Fatalf("index 3 must be the dynamic memory block, got %q", first[3].Content)
	}
	if !strings.Contains(first[4].Content, activePlanMarker) {
		t.Fatalf("the volatile [Active Plan] block must sit right before the conversation, got %q", first[4].Content)
	}
	if first[5].Role != "user" {
		t.Fatalf("conversation must start after the injected blocks, got role=%q", first[5].Role)
	}

	// ③ 两轮之间首个差异必须落在动态记忆块（index 3）——即稳定前缀确实是 3 段。
	diff := 0
	for diff < len(first) && diff < len(second) {
		if first[diff].Role != second[diff].Role || first[diff].Content != second[diff].Content {
			break
		}
		diff++
	}
	if diff != stableLen {
		t.Fatalf("first divergence must be the dynamic memory block at index %d, got index %d", stableLen, diff)
	}
}
