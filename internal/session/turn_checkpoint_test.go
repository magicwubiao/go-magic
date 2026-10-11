package session

import (
	"context"
	"github.com/magicwubiao/go-magic/pkg/types"
	"path/filepath"
	"testing"
)

func TestTurnCheckpointSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(ctx, &Session{ID: "s", Platform: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendSessionMessages(ctx, "s", types.Message{ID: "user_t", Role: "user", Content: "task"}); err != nil {
		t.Fatal(err)
	}
	msg := types.Message{ID: "assistant_t", Role: "assistant", Content: "checkpoint", ToolCallsSnapshot: []types.ToolExecutionSnapshot{{ID: "tool_1", Name: "write_file", Status: "running", Args: `{"path":"a.txt"}`}}}
	if ok, err := store.UpsertSessionMessage(ctx, "s", msg); err != nil || !ok {
		t.Fatalf("checkpoint: %v %v", ok, err)
	}
	// A guide may be appended concurrently between checkpoints; never overwrite it.
	if _, err := store.AppendSessionMessages(ctx, "s", types.Message{ID: "guide_1", Role: "user", Content: "guide"}); err != nil {
		t.Fatal(err)
	}
	msg.ToolCallsSnapshot[0].Status = "completed"
	msg.ToolCallsSnapshot[0].Content = "written"
	if _, err := store.UpsertSessionMessage(ctx, "s", msg); err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	sess, err := reopened.LoadSession(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 3 {
		t.Fatalf("messages=%d, want user + checkpoint + guide", len(sess.Messages))
	}
	snap := sess.Messages[1].ToolCallsSnapshot
	if len(snap) != 1 || snap[0].Status != "completed" || snap[0].Content != "written" {
		t.Fatalf("lost snapshot: %+v", snap)
	}
	if sess.Messages[2].ID != "guide_1" {
		t.Fatal("guide overwritten")
	}
}
