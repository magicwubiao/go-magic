package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestCortexStaticIsInjectedWithoutTouchingHistory
//
// SOUL.md 等 cortex 静态上下文必须作为**独立的注入块**出现在出站消息里，且
// 绝不写回 history。旧实现（AddSystemContext / injectCortexContext）把内容追加
// 到 history 里的 system 消息上（history[0].Content += ...）：基础 system prompt
// 随轮数无限膨胀，且每轮内容都变 ⇒ prompt 前缀缓存永远命中不了。
//
// 这里把「不落 history + 每轮恰好注入一次」两个不变量同时钉死。
func TestCortexStaticIsInjectedWithoutTouchingHistory(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	ag.SetSession("sess-cortexstatic")
	ag.ruleContext = "[Rules]\nbe nice"
	ag.cortexStatic = "[SOUL]\nAlways be precise."

	const base = "You are a helpful assistant."
	ag.history = []provider.Message{
		{Role: "system", Content: base},
		{Role: "user", Content: "go"},
	}

	for round := 0; round < 3; round++ {
		out := ag.buildLLMMessages()
		n := 0
		for _, m := range out {
			if strings.Contains(m.Content, "[SOUL]") {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("round %d: [SOUL] 块出现 %d 次，want 恰好 1 次（重复注入 = 旧 bug 复发）", round, n)
		}
	}

	if ag.history[0].Content != base {
		t.Fatalf("history[0] 被改写（注入必须只留在出站副本里）: %q", ag.history[0].Content)
	}
	if len(ag.history) != 2 {
		t.Fatalf("history 长度被改动: %d, want 2", len(ag.history))
	}
}

// TestContextInjectionDoesNotAccumulate 直接对着旧 bug 的形态断言：连续多轮构建
// 出站消息后 history[0] 不得增长（旧实现每轮 += 一段）。
func TestContextInjectionDoesNotAccumulate(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	ag.history = []provider.Message{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "go"},
	}
	first := ag.history[0].Content

	ag.cortexStatic = "[SOUL]\npersona"
	ag.snapshotMemory = "[MEMORY]\nremember this"
	ag.dynamicMemory = "recalled memory"

	for i := 0; i < 5; i++ {
		if out := ag.buildLLMMessages(); len(out) == 0 {
			t.Fatal("buildLLMMessages returned nothing")
		}
	}
	if ag.history[0].Content != first {
		t.Fatalf("连续 5 轮后 history[0] 发生变化（基础 system prompt 膨胀 = 旧 bug 复发）:\n  before=%q\n  after =%q",
			first, ag.history[0].Content)
	}
}

// TestNoHistoryAppendingInjectionHelper 守卫：曾把内容 `+=` 进 history 的两个
// 助手（AddSystemContext / injectCortexContext 的追加分支）不得复活。
func TestNoHistoryAppendingInjectionHelper(t *testing.T) {
	for _, f := range []string{"agent.go", "cortex_integration.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(data)
		for _, bad := range []string{"func (a *Agent) AddSystemContext", "a.history[0].Content +=", "a.history[i].Content +="} {
			if strings.Contains(src, bad) {
				t.Errorf("%s 里出现 %q：往 history 的 system 消息里追加注入内容会"+
					"让基础 prompt 逐轮膨胀并废掉前缀缓存，注入只能走 withContextBlocks", f, bad)
			}
		}
	}
}
