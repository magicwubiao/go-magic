package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// 本文件锁死"循环检测先给模型一次换方法的机会，并把原因告诉用户"。
//
// 背景：此前循环一触发就 concludeAfterToolLoop —— 模型没有任何纠正机会，用户
// 拿到的是一个被包装成总结的半成品，而回复里完全没说"为什么停下"。现在的契约是：
//   1. 首次触发 → 注入一条带具体原因的"换方法"提示，继续循环（不消耗工具执行）；
//   2. 同一回合再次触发 → 才收口，且用户可见文本里带停止原因；
//   3. 提示每回合只注入一次（换回合重新计算）。

// recoverableLoopProvider 先重复同一个调用制造循环，看到"换方法"提示后改口给出
// 最终回答（模拟模型被纠正成功）。
type recoverableLoopProvider struct {
	mu       sync.Mutex
	calls    int
	nudges   int
	nudgeTxt string
}

func (p *recoverableLoopProvider) Name() string { return "recoverable-loop" }

func (p *recoverableLoopProvider) Chat(_ context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++

	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.Content, "Tool loop detected:") {
			p.nudges++
			p.nudgeTxt = m.Content
		}
	}
	if p.nudgeTxt != "" {
		// 纠正成功：不再重复同一个调用，直接汇报完成。
		return &provider.ChatResponse{Content: "switched approach and finished"}, nil
	}
	return &provider.ChatResponse{
		ToolCalls: []types.ToolCall{{
			ID:       "loop-call",
			Type:     "function",
			Function: types.Function{Name: testLoopToolName, Arguments: `{"same":1}`},
		}},
	}, nil
}

// TestLoopNudgeGivesModelASecondChance：模型在收到提示后换了方法，回合必须
// **正常结束**（拿最终回答），而不是被当成死循环收口。
func TestLoopNudgeGivesModelASecondChance(t *testing.T) {
	prov := &recoverableLoopProvider{}
	ag := newLoopTestAgent(t, prov)

	resp, err := ag.RunConversation(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("a recovered loop must not error out: %v", err)
	}
	if resp != "switched approach and finished" {
		t.Fatalf("the model's post-nudge answer must reach the user, got %q", resp)
	}
	if strings.Contains(resp, "已自动停止") {
		t.Fatalf("a recovered loop must not be reported as an automatic stop: %q", resp)
	}

	prov.mu.Lock()
	nudges, nudgeTxt := prov.nudges, prov.nudgeTxt
	prov.mu.Unlock()
	if nudges != 1 {
		t.Fatalf("the change-approach nudge must be injected exactly once, got %d", nudges)
	}
	// 提示必须带上具体原因，否则模型不知道该改什么。
	if !strings.Contains(nudgeTxt, "identical arguments") {
		t.Fatalf("the nudge must carry the concrete loop reason, got %q", nudgeTxt)
	}
	if !strings.Contains(nudgeTxt, "Change approach") {
		t.Fatalf("the nudge must tell the model what to do instead, got %q", nudgeTxt)
	}
}

// TestLoopNudgeOnlyOncePerTurn：提示每回合只注入一次，模型若继续重复就必须收口
// —— 给机会不等于放弃保护。
func TestLoopNudgeOnlyOncePerTurn(t *testing.T) {
	prov := &scriptedLoopProvider{} // 永远重复同一个调用，无视提示
	ag := newLoopTestAgent(t, prov)

	resp, err := ag.RunConversation(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("a dead loop should still conclude gracefully: %v", err)
	}
	if !strings.Contains(resp, testSummaryMarker) {
		t.Fatalf("expected a conclusion summary, got %q", resp)
	}

	nudges := 0
	for _, m := range ag.getHistory() {
		if strings.Contains(m.Content, "Tool loop detected:") {
			nudges++
		}
	}
	if nudges != 1 {
		t.Fatalf("exactly one nudge must be left in history for the turn, got %d", nudges)
	}
}

// TestLoopNudgeResetsBetweenTurns：提示计数按回合清零，否则第二个回合起模型
// 一次纠正机会都拿不到（与循环计数跨回合累积是同一类回归）。
func TestLoopNudgeResetsBetweenTurns(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	ag.mu.Lock()
	ag.loopNudgeUsed = true
	ag.mu.Unlock()

	ag.resetToolLoopCounters()
	ag.mu.RLock()
	used := ag.loopNudgeUsed
	ag.mu.RUnlock()
	if used {
		t.Fatal("loopNudgeUsed must be cleared at the start of every turn")
	}
}
