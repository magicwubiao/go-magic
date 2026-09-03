package tool

import (
	"context"
	"strings"
	"testing"
)

// newTestTodoTool builds an isolated TodoTool (no singleton, temp data file).
func newTestTodoTool(t *testing.T) *TodoTool {
	t.Helper()
	return &TodoTool{
		todos:      make(map[string]*TodoItem),
		dataFile:   t.TempDir() + "/todos.json",
		tombstones: make(map[string]tombstoneInfo),
	}
}

// TestTodoTombstoneIdempotentOps verifies that after auto-cleanup removes a
// session's todos, later update/complete/delete on those IDs return an
// idempotent success (with tombstoned=true) instead of "todo not found".
// Regression: the hard error used to abort agent runs mid-task.
func TestTodoTombstoneIdempotentOps(t *testing.T) {
	tt := newTestTodoTool(t)
	// Session-scoped ctx so todos belong to one bucket and auto-cleanup fires.
	ctx := WithSessionID(context.Background(), "test-session")

	mk, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "step A"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idA, _ := mk.(map[string]interface{})["id"].(string)
	if idA == "" {
		t.Fatal("created todo has empty id")
	}
	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "step B"}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Complete everything -> cleanupSessionIfAllDoneLocked wipes the bucket.
	// Note: list WITHOUT explicit args lets Execute inject the ctx session,
	// returning exactly this session's bucket ("session_id": "" would instead
	// strictly match the empty/global bucket and hide these todos).
	list1, _ := tt.Execute(ctx, map[string]interface{}{"action": "list"})
	var allIDs []string
	for _, row := range list1.(map[string]interface{})["todos"].([]map[string]interface{}) {
		allIDs = append(allIDs, row["id"].(string))
	}
	if len(allIDs) < 2 {
		t.Fatalf("expected 2 todos before cleanup, got %d", len(allIDs))
	}
	for _, tid := range allIDs {
		if _, err := tt.Execute(ctx, map[string]interface{}{"action": "complete", "id": tid}); err != nil {
			t.Fatalf("complete %s: %v", tid, err)
		}
	}
	list, _ := tt.Execute(ctx, map[string]interface{}{"action": "list"})
	total, _ := list.(map[string]interface{})["total"].(int)
	if total != 0 {
		t.Fatalf("expected bucket cleaned up, got %d remaining", total)
	}

	// Late ops on tombstoned IDs must succeed idempotently.
	for _, op := range []map[string]interface{}{
		{"action": "complete", "id": idA},
		{"action": "update", "id": idA, "status": "completed"},
		{"action": "delete", "id": idA},
	} {
		resp, err := tt.Execute(ctx, op)
		if err != nil {
			t.Fatalf("%v on tombstoned id returned error: %v", op["action"], err)
		}
		m, ok := resp.(map[string]interface{})
		if !ok || m["tombstoned"] != true {
			t.Fatalf("%v: expected tombstoned=true response, got %#v", op["action"], resp)
		}
	}

	// Unknown (never-existed) IDs must still fail loudly so real bugs surface.
	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "complete", "id": "todo_nope"}); err == nil ||
		!strings.Contains(err.Error(), "todo not found") {
		t.Fatalf("unknown id should fail with 'todo not found', got %v", err)
	}
}
