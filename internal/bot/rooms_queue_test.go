package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStopRoomRoundInvalidatesIdleQueue covers the gap between two rounds:
// RoomRoundRunning reports "running" while requests are queued, but stopping
// used to be a no-op there (it only bumped stopGen when a round was executing),
// so the queued round started anyway — the "群聊停不下来" report.
func TestStopRoomRoundInvalidatesIdleQueue(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	room := &RoomConfig{ID: "room_testq", Name: "r", Members: []string{"alice", "bob"}}
	rt := &roomRuntime{
		cfg:       room,
		triggerCh: make(chan roomRequest, 4),
		stopCh:    make(chan struct{}),
	}
	req := roomRequest{Text: "排队中", From: "user", Reply: make(chan *RoomResult, 1)}
	rt.triggerCh <- req

	mgr.mu.Lock()
	mgr.rooms[room.ID] = rt
	mgr.mu.Unlock()

	// Sanity: the queue counts as "running" for the UI.
	if !mgr.RoomRoundRunning(room.ID) {
		t.Fatal("a room with a queued request must report running")
	}
	if !mgr.StopRoomRound(room.ID) {
		t.Fatal("stop on a room with only a queued request must report true")
	}
	if !mgr.roomRequestStale(rt, req) {
		t.Error("queued request was not invalidated by the stop")
	}
}

// TestUpdateRoomMetadataKeepsCoordinator: renaming a room (or editing its
// topic) must not tear down the running coordinator — that used to cancel the
// in-flight round and orphan every queued sender. Only a member change needs
// a fresh coordinator.
func TestUpdateRoomMetadataKeepsCoordinator(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	room := &RoomConfig{Name: "r", Members: []string{"alice", "bob"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	old := mgr.rooms[strings.ToLower(room.ID)]
	mgr.mu.Unlock()
	if old == nil {
		t.Fatal("room coordinator was not started")
	}

	if _, err := mgr.UpdateRoom(room.ID, func(r *RoomConfig) {
		r.Name = "renamed"
		r.Topic = "new topic"
	}); err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}

	if mgr.roomClosed(old) {
		t.Error("a metadata-only update killed the live coordinator")
	}
	mgr.mu.Lock()
	live := mgr.rooms[strings.ToLower(room.ID)]
	mgr.mu.Unlock()
	if live != old {
		t.Error("a metadata-only update replaced the coordinator")
	}
	if live.cfg.Name != "renamed" || live.cfg.Topic != "new topic" {
		t.Errorf("live coordinator kept a stale config: %+v", live.cfg)
	}
}

// TestRoomRoundDoesNotBlockDirectChat pins the queue separation: a room round
// holds its member turn open on the LLM, and a direct message to the same bot
// must still be answered immediately (it used to sit behind the round for
// minutes with no visible reason).
func TestRoomRoundDoesNotBlockDirectChat(t *testing.T) {
	roomStarted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	handler := func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		if strings.Contains(string(raw), "Your turn (round") {
			once.Do(func() { close(roomStarted) })
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse("ok"))
	}

	mgr, roomID, _ := newRoomRigHandler(t, handler, 1)
	t.Cleanup(func() { close(release) })

	go func() {
		_, _ = mgr.SendToRoom(context.Background(), roomID, "开个会", "")
	}()
	waitTimeout(t, roomStarted, 10*time.Second, "the room round reaching the LLM")

	done := make(chan error, 1)
	go func() {
		_, err := mgr.SendToBot("alice", "在吗")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("direct chat failed while a room round was running: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("direct chat was blocked by the running room round")
	}
}
