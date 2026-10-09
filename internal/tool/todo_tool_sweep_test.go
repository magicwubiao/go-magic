package tool

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestSweepSessionTerminalRemovesFinishedOnly 覆盖线上现象：
// 一个会话里只要还有一条没做完，"整桶全完成才清"就永远不触发，
// 已完成项会一直堆在侧栏。SweepSessionTerminal 必须在回合结束时把
// 终态项收走，同时**保留**未完成项、**不动**其它会话。
func TestSweepSessionTerminalRemovesFinishedOnly(t *testing.T) {
	tt := newTestTodoTool(t)
	ctxA := WithSessionID(context.Background(), "sess-A")
	ctxB := WithSessionID(context.Background(), "sess-B")

	// 会话 A：3 条，完成 2 条、留 1 条 pending。
	idsA := make([]string, 0, 3)
	for _, title := range []string{"step-1", "step-2", "step-3"} {
		res, err := tt.Execute(ctxA, map[string]interface{}{"action": "create", "title": title})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		idsA = append(idsA, res.(map[string]interface{})["id"].(string))
	}
	for _, id := range idsA[:2] {
		if _, err := tt.Execute(ctxA, map[string]interface{}{"action": "complete", "id": id}); err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
	}

	// 留了 1 条 pending ⇒ 整桶未全完成 ⇒ 旧逻辑一条都不清（这正是残留的成因）。
	listA, _ := tt.Execute(ctxA, map[string]interface{}{"action": "list"})
	if got := listA.(map[string]interface{})["total"].(int); got != 3 {
		t.Fatalf("expected 3 items still present before sweep, got %d", got)
	}

	// 会话 B：先建两条，再完成其中一条。
	// 顺序很重要 —— 必须让 pending 兄弟**先存在**，否则完成那一刻整桶全终态，
	// 会被另一条通道（cleanupSessionIfAllDoneLocked）当场清掉，就测不出越界了。
	resB, err := tt.Execute(ctxB, map[string]interface{}{"action": "create", "title": "other-done"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	idB := resB.(map[string]interface{})["id"].(string)
	if _, err := tt.Execute(ctxB, map[string]interface{}{"action": "create", "title": "other-pending"}); err != nil {
		t.Fatalf("create B2: %v", err)
	}
	if _, err := tt.Execute(ctxB, map[string]interface{}{"action": "complete", "id": idB}); err != nil {
		t.Fatalf("complete B: %v", err)
	}

	// 回合结束清扫会话 A。
	if n := tt.SweepSessionTerminal("sess-A"); n != 2 {
		t.Fatalf("expected 2 finished items swept, got %d", n)
	}

	listA, _ = tt.Execute(ctxA, map[string]interface{}{"action": "list"})
	rows := listA.(map[string]interface{})["todos"].([]map[string]interface{})
	if len(rows) != 1 {
		t.Fatalf("expected only the unfinished item to remain, got %d", len(rows))
	}
	if rows[0]["id"].(string) != idsA[2] || rows[0]["status"].(string) != "pending" {
		t.Fatalf("wrong survivor: %#v", rows[0])
	}

	// 被清扫的 ID 变成墓碑：迟到操作幂等成功而不是硬报错。
	for _, id := range idsA[:2] {
		resp, err := tt.Execute(ctxA, map[string]interface{}{"action": "complete", "id": id})
		if err != nil {
			t.Fatalf("late complete on swept id %s: %v", id, err)
		}
		if resp.(map[string]interface{})["tombstoned"] != true {
			t.Fatalf("expected tombstoned=true for %s, got %#v", id, resp)
		}
	}

	// 幂等：再扫一次无事可做。
	if n := tt.SweepSessionTerminal("sess-A"); n != 0 {
		t.Fatalf("second sweep should be a no-op, got %d", n)
	}

	// 会话 B 完全没被动过（含那条已完成项）。
	listB, _ := tt.Execute(ctxB, map[string]interface{}{"action": "list"})
	rowsB := listB.(map[string]interface{})["todos"].([]map[string]interface{})
	if len(rowsB) != 2 {
		t.Fatalf("session B must be untouched, got %d items: %#v", len(rowsB), rowsB)
	}
}

// TestSweepSessionTerminalKeepsUnfinished 确认清扫不会把未完成项当成残留删掉。
func TestSweepSessionTerminalKeepsUnfinished(t *testing.T) {
	tt := newTestTodoTool(t)
	ctx := WithSessionID(context.Background(), "sess-keep")

	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "todo-a"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if n := tt.SweepSessionTerminal("sess-keep"); n != 0 {
		t.Fatalf("nothing should be swept, got %d", n)
	}
	list, _ := tt.Execute(ctx, map[string]interface{}{"action": "list"})
	if got := list.(map[string]interface{})["total"].(int); got != 1 {
		t.Fatalf("unfinished item must survive the sweep, got %d", got)
	}
}

// TestPendingTitlesForSession 覆盖回合末对账提醒的数据源：
// 只返回未完成项、按创建时间排序、超过 8 条截断并带省略计数。
func TestPendingTitlesForSession(t *testing.T) {
	tt := newTestTodoTool(t)
	ctx := WithSessionID(context.Background(), "sess-pending")
	other := WithSessionID(context.Background(), "sess-other")

	// 本会话 3 条：完成 1 条、留 2 条；另会话 1 条（不得混入）。
	res, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "alpha"})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	idAlpha := res.(map[string]interface{})["id"].(string)
	for _, title := range []string{"beta", "gamma"} {
		if _, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": title}); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
	}
	if _, err := tt.Execute(other, map[string]interface{}{"action": "create", "title": "others"}); err != nil {
		t.Fatalf("create other: %v", err)
	}
	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "complete", "id": idAlpha}); err != nil {
		t.Fatalf("complete alpha: %v", err)
	}

	got := tt.PendingTitlesForSession("sess-pending")
	if len(got) != 2 || got[0] != "beta" || got[1] != "gamma" {
		t.Fatalf("expected [beta gamma] in creation order, got %#v", got)
	}

	// 不截断：返回全部 9 条（长度预算由调用方 nudgeTodoReconciliation 负责）。
	ctx9 := WithSessionID(context.Background(), "sess-cap")
	for i := 0; i < 9; i++ {
		if _, err := tt.Execute(ctx9, map[string]interface{}{"action": "create", "title": fmt.Sprintf("t-%d", i)}); err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
	}
	capped := tt.PendingTitlesForSession("sess-cap")
	if len(capped) != 9 {
		t.Fatalf("expected all 9 titles in creation order, got %#v", capped)
	}
	if capped[0] != "t-0" || capped[8] != "t-8" {
		t.Fatalf("titles must keep creation order, got %#v", capped)
	}
}

// TestActivePlanForSession 锁住"供请求注入的权威在案计划"的语义。
//
// 存在理由见 ActivePlanForSession 的注释：待办 ID 只出现在 create 的返回值里，
// 而那份返回值会随历史压缩蒸发 —— 一旦丢了，模型既无法 complete、也不再知道
// 列表在案（2026-10-09 事故：建 4 条只标 1 条）。所以请求组装必须能从持久层
// 重新拿到"未完成项 + ID"。
func TestActivePlanForSession(t *testing.T) {
	tt := newTestTodoTool(t)
	ctx := WithSessionID(context.Background(), "sess-plan")

	// 建 3 条：第 1 条标完成（不该出现在在案计划里），第 2 条置 in_progress
	// （应排最前），第 3 条留 pending。
	ids := make([]string, 0, 3)
	for _, title := range []string{"alpha", "beta", "gamma"} {
		res, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": title})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		ids = append(ids, res.(map[string]interface{})["id"].(string))
	}
	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "complete", "id": ids[0]}); err != nil {
		t.Fatalf("complete alpha: %v", err)
	}
	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "update", "id": ids[1], "status": "in_progress"}); err != nil {
		t.Fatalf("in_progress beta: %v", err)
	}

	// 另一个会话的项不得混进来。
	other := WithSessionID(context.Background(), "sess-other")
	if _, err := tt.Execute(other, map[string]interface{}{"action": "create", "title": "other"}); err != nil {
		t.Fatalf("create other: %v", err)
	}

	plan := tt.ActivePlanForSession("sess-plan")
	if len(plan) != 2 {
		t.Fatalf("only unfinished items belong in the plan, got %#v", plan)
	}
	// in_progress 排最前（正在做的先看），其余按创建时间。
	if plan[0].ID != ids[1] || plan[0].Status != "in_progress" || plan[0].Title != "beta" {
		t.Fatalf("in_progress item must lead the plan, got %#v", plan[0])
	}
	if plan[1].ID != ids[2] || plan[1].Status != "pending" {
		t.Fatalf("pending item must follow, got %#v", plan[1])
	}
	// ID 必须原样带出来 —— 那是 complete 的唯一凭据。
	if !strings.HasPrefix(plan[0].ID, "todo_") {
		t.Fatalf("entry must carry the real todo id, got %q", plan[0].ID)
	}

	// 没有会话上下文时返回 nil：宁可少注入，也不要把全局桶/别人的计划注进本次请求。
	if got := tt.ActivePlanForSession(""); got != nil {
		t.Fatalf("empty session must yield nil (never inject the global bucket), got %#v", got)
	}
	// 未知会话同理。
	if got := tt.ActivePlanForSession("nope"); len(got) != 0 {
		t.Fatalf("unknown session must yield nothing, got %#v", got)
	}
}
