package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// todoReconcileMarker 是 nudgeTodoReconciliation 注入的对账消息前缀。
// 提供端 mock 用"最后一条 user 消息是否含它"来识别"这次是在对待办做对账"。
const todoReconcileMarker = "[todo reconcile]"

// todoNudgeProvider 复刻 2026-10-09 线上形态：模型建完待办、标完（或没标）
// 就直接给出最终回答。第 1 次调用发一个 todo 工具调用；之后若历史末尾出现
// 对账提醒，则给出最终回答。
type todoNudgeProvider struct {
	mu            sync.Mutex
	chats         int
	reconcileSeen int
}

func (p *todoNudgeProvider) Name() string { return "todo-nudge" }

func (p *todoNudgeProvider) Chat(_ context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chats++
	if lastMessageContains(messages, todoReconcileMarker) {
		p.reconcileSeen++
		return &provider.ChatResponse{Content: "marked and done"}, nil
	}
	if p.chats == 1 {
		return &provider.ChatResponse{
			ToolCalls: []types.ToolCall{{
				ID:   "call-todo-1",
				Type: "function",
				Function: types.Function{
					Name:      "todo",
					Arguments: `{"action":"create","title":"step-1"}`,
				},
			}},
		}, nil
	}
	// 模型"想收尾"，但还有待办没标 —— 应当被对账提醒拉回来一次。
	return &provider.ChatResponse{Content: "all done (forgetting the todos)"}, nil
}

func (p *todoNudgeProvider) snapshot() (chats, reconcileSeen int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chats, p.reconcileSeen
}

// TestTodoReconcileNudgePullsModelBack 锁死"建了待办却不标就收尾"。
//
// 线上证据（2026-10-09 20:08 的回合）：模型建了 4 条待办只标了 1 条，
// 其余 3 条到回合结束仍是 pending。提示词引导只有部分效果，所以回合末
// 需要一道确定性收口：模型给出无工具调用的最终回答、而本回合动过 todo
// 且会话仍有未完成项时，必须被拉回来补标记（每回合至多一次）。
func TestTodoReconcileNudgePullsModelBack(t *testing.T) {
	prov := &todoNudgeProvider{}
	ag := newLoopTestAgent(t, prov)
	ag.SetSession("sess-nudge")
	ag.todoPendingTitles = func(sessionID string) []string {
		if sessionID != "sess-nudge" {
			t.Errorf("pending query should target the agent session, got %q", sessionID)
		}
		return []string{"step-1", "step-2"}
	}

	resp, err := ag.RunConversation(context.Background(), "do the work")
	if err != nil {
		t.Fatalf("turn should complete gracefully: %v", err)
	}
	if resp != "marked and done" {
		t.Fatalf("expected the post-reconcile final answer, got %q", resp)
	}

	chats, reconcileSeen := prov.snapshot()
	if reconcileSeen != 1 {
		t.Fatalf("reconcile nudge should be injected exactly once, got %d (chats=%d)", reconcileSeen, chats)
	}
	// 对账消息必须真的进了历史（作为 user 消息，供 provider 与 sanitize 处理）。
	found := false
	for _, m := range ag.history {
		if m.Role == "user" && strings.Contains(m.Content, todoReconcileMarker) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reconcile message missing from history")
	}
}

// TestTodoReconcileNudgeConditions 固定触发条件的边界：
// 没会话 / 本回合没动过 todo / 没有未完成项 都不得注入；注入是一次性的。
func TestTodoReconcileNudgeConditions(t *testing.T) {
	newAgent := func(t *testing.T) *Agent {
		t.Helper()
		ag := newLoopTestAgent(t, &todoNudgeProvider{})
		ag.todoPendingTitles = func(string) []string { return []string{"step-1"} }
		return ag
	}

	t.Run("no session", func(t *testing.T) {
		ag := newAgent(t)
		ag.recordToolCall("todo")
		if ag.nudgeTodoReconciliation() {
			t.Fatalf("no session => no nudge")
		}
	})

	t.Run("todo not touched this turn", func(t *testing.T) {
		ag := newAgent(t)
		ag.SetSession("sess-x")
		ag.recordToolCall("read_file")
		if ag.nudgeTodoReconciliation() {
			t.Fatalf("turn without todo calls => no nudge (否则旧遗留会让每轮都多一次往返)")
		}
	})

	t.Run("nothing pending", func(t *testing.T) {
		ag := newAgent(t)
		ag.SetSession("sess-x")
		ag.recordToolCall("todo")
		ag.todoPendingTitles = func(string) []string { return nil }
		if ag.nudgeTodoReconciliation() {
			t.Fatalf("nothing pending => no nudge")
		}
	})

	t.Run("injects once and caps the list", func(t *testing.T) {
		ag := newAgent(t)
		ag.SetSession("sess-x")
		ag.recordToolCall("todo")
		ag.todoPendingTitles = func(string) []string {
			return []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} // 9 条 > 上限 8
		}
		if !ag.nudgeTodoReconciliation() {
			t.Fatalf("pending todos after todo calls => nudge expected")
		}
		last := ag.history[len(ag.history)-1]
		if last.Role != "user" || !strings.Contains(last.Content, todoReconcileMarker) {
			t.Fatalf("nudge must append a user message, got role=%s content=%q", last.Role, last.Content)
		}
		if !strings.Contains(last.Content, "...and 1 more") {
			t.Fatalf("list over the cap should be truncated with a count, got %q", last.Content)
		}
		// 第二次必须拒绝（每回合至多一次，防死循环）。
		if ag.nudgeTodoReconciliation() {
			t.Fatalf("nudge must be one-shot per turn")
		}
		// 新回合清零后可再次注入。注意 reset 会连工具调用历史一起清
		// （那是它的本职），所以新回合要先有"又动过 todo"这一前提。
		ag.resetToolLoopCounters()
		ag.recordToolCall("todo")
		if !ag.nudgeTodoReconciliation() {
			t.Fatalf("after per-turn reset the nudge must be allowed again")
		}
	})
}
