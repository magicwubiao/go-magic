package tool

import (
	"context"
	"strings"
	"testing"
)

// newTestTodoTool builds an isolated TodoTool (no singleton, temp data files).
func newTestTodoTool(t *testing.T) *TodoTool {
	t.Helper()
	dir := t.TempDir()
	return &TodoTool{
		todos:         make(map[string]*TodoItem),
		dataFile:      dir + "/todos.json",
		tombstones:    make(map[string]tombstoneInfo),
		tombstoneFile: dir + "/tombstones.json",
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

// TestTodoTombstoneSurvivesRestart verifies tombstones are persisted: after a
// simulated process restart (rebuild from the same data dir), late operations
// on auto-cleaned IDs still degrade to no-op instead of "todo not found".
// Regression: in-memory tombstones were wiped by deploy/upgrade restarts and
// the agent saw hard errors on IDs from its conversation history.
func TestTodoTombstoneSurvivesRestart(t *testing.T) {
	tt := newTestTodoTool(t)
	ctx := WithSessionID(context.Background(), "restart-session")

	mk, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "step A"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idA, _ := mk.(map[string]interface{})["id"].(string)
	if _, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "step B"}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Complete everything -> bucket auto-cleanup + tombstone persistence.
	list, _ := tt.Execute(ctx, map[string]interface{}{"action": "list"})
	for _, row := range list.(map[string]interface{})["todos"].([]map[string]interface{}) {
		if _, err := tt.Execute(ctx, map[string]interface{}{"action": "complete", "id": row["id"].(string)}); err != nil {
			t.Fatalf("complete: %v", err)
		}
	}

	// Simulate restart: fresh in-memory maps, same files.
	dir := tt.dataFile[:len(tt.dataFile)-len("/todos.json")]
	reborn := &TodoTool{
		todos:         make(map[string]*TodoItem),
		dataFile:      dir + "/todos.json",
		tombstones:    make(map[string]tombstoneInfo),
		tombstoneFile: dir + "/tombstones.json",
	}
	reborn.load()
	reborn.loadTombstones()
	if len(reborn.tombstones) == 0 {
		t.Fatal("tombstones.json missing entries after cleanup")
	}

	for _, op := range []map[string]interface{}{
		{"action": "complete", "id": idA},
		{"action": "update", "id": idA, "status": "completed"},
		{"action": "delete", "id": idA},
	} {
		resp, err := reborn.Execute(ctx, op)
		if err != nil {
			t.Fatalf("post-restart %v on tombstoned id returned error: %v", op["action"], err)
		}
		m, ok := resp.(map[string]interface{})
		if !ok || m["tombstoned"] != true {
			t.Fatalf("post-restart %v: expected tombstoned=true, got %#v", op["action"], resp)
		}
	}
}
