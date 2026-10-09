package tool

import (
	"context"
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
