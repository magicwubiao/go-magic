package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestCreateRoomValidatesMembers covers the room membership rules: a room needs
// 2-6 bots, every member must exist, and members may not repeat.
func TestCreateRoomValidatesMembers(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	cases := []struct {
		name    string
		members []string
	}{
		{"too-few", []string{"alice"}},
		{"unknown-member", []string{"alice", "nobody"}},
		{"duplicate-member", []string{"alice", "ALICE"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := mgr.CreateRoom(&RoomConfig{Name: tc.name, Members: tc.members}); err == nil {
				t.Fatalf("CreateRoom(%v) succeeded, want a validation error", tc.members)
			}
		})
	}

	// A nil config is rejected rather than panicking.
	if err := mgr.CreateRoom(nil); err == nil {
		t.Error("CreateRoom(nil) should fail")
	}

	// No partial state may be left behind by a rejected room.
	rooms, err := mgr.ListRooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 0 {
		t.Errorf("rejected rooms leaked into the store: %d", len(rooms))
	}
}

// TestCreateRoomEnforcesMaxMembers keeps the 6-member hard cap honest.
func TestCreateRoomEnforcesMaxMembers(t *testing.T) {
	names := []string{"b1", "b2", "b3", "b4", "b5", "b6", "b7"}
	mgr := newTestManager(t, names...)

	ok := append([]string(nil), names[:MaxRoomMembers]...)
	if err := mgr.CreateRoom(&RoomConfig{Name: "full", Members: ok}); err != nil {
		t.Fatalf("room with %d members should be accepted: %v", MaxRoomMembers, err)
	}
	if err := mgr.CreateRoom(&RoomConfig{Name: "over", Members: names}); err == nil {
		t.Fatalf("room with %d members should be rejected", len(names))
	}
}

// TestCreateRoomGeneratesIdentityAndNormalizesMembers: rooms without an ID/name
// get one, and member names are stored lowercased so the coordinator can match
// them against bot map keys.
func TestCreateRoomGeneratesIdentityAndNormalizesMembers(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	room := &RoomConfig{Members: []string{"Alice", "BOB"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.ID == "" {
		t.Error("room ID should be generated")
	}
	if room.Name != room.ID {
		t.Errorf("room name = %q, want the ID %q", room.Name, room.ID)
	}
	if room.Members[0] != "alice" || room.Members[1] != "bob" {
		t.Errorf("members not normalized: %v", room.Members)
	}

	// Persisted and reachable by ID.
	stored, err := mgr.GetRoom(room.ID)
	if err != nil {
		t.Fatalf("GetRoom after create: %v", err)
	}
	if stored.Name != room.Name || len(stored.Members) != 2 {
		t.Errorf("stored room mismatch: %+v", stored)
	}
}

// TestSendToRoomRejectsUnknownRoomAndTarget: both guards must fire before any
// turn is scheduled, so a typo cannot silently run a round with nobody in it.
func TestSendToRoomRejectsUnknownRoomAndTarget(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	if _, err := mgr.SendToRoom(context.Background(), "no-such-room", "hi", ""); err == nil {
		t.Error("SendToRoom on an unknown room should fail")
	}

	room := &RoomConfig{Name: "r", Members: []string{"alice", "bob"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SendToRoom(context.Background(), room.ID, "hi", "@ghost"); err == nil {
		t.Error("SendToRoom with an unknown target tag should fail")
	}
}

// TestUpdateRoomTearsDownOldCoordinator is the regression test for rooms whose
// members changed while a round was still running: UpdateRoom closes the old
// coordinator's stop channel, and runRoomRound now observes it instead of
// delivering the rest of the round to the old member list.
func TestUpdateRoomTearsDownOldCoordinator(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob", "carol")

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

	updated, err := mgr.UpdateRoom(room.ID, func(r *RoomConfig) {
		r.Name = "renamed"
		r.Topic = "new topic"
		r.Members = []string{"bob", "carol"}
		r.MaxRounds = 2
	})
	if err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}
	if updated.Name != "renamed" || updated.MaxRounds != 2 || len(updated.Members) != 2 {
		t.Errorf("update not applied: %+v", updated)
	}

	if !mgr.roomClosed(old) {
		t.Error("the previous coordinator is still alive; in-flight rounds would keep talking to removed members")
	}
	mgr.mu.Lock()
	replaced := mgr.rooms[strings.ToLower(room.ID)]
	mgr.mu.Unlock()
	if replaced == nil || replaced == old {
		t.Error("a fresh coordinator should have replaced the old one")
	}

	// The new member set survives a reload.
	stored, err := mgr.GetRoom(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stored.Members, ",") != "bob,carol" {
		t.Errorf("stored members = %v, want [bob carol]", stored.Members)
	}
}

// TestDeleteRoomRemovesSessionsAndCoordinator: deleting a room must stop its
// coordinator and purge the per-member room sessions + the shared room log.
func TestDeleteRoomRemovesSessionsAndCoordinator(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	room := &RoomConfig{Name: "r", Members: []string{"alice", "bob"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	mgr.appendRoomMessage(room, "user", "hello room", nil)

	if msgs, _ := mgr.RoomMessages(room.ID); len(msgs) != 1 {
		t.Fatalf("room log not written: %d messages", len(msgs))
	}

	mgr.mu.Lock()
	rt := mgr.rooms[strings.ToLower(room.ID)]
	mgr.mu.Unlock()

	if err := mgr.DeleteRoom(room.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	if !mgr.roomClosed(rt) {
		t.Error("room coordinator still running after delete")
	}
	if _, err := mgr.GetRoom(room.ID); err == nil {
		t.Error("room config still on disk after delete")
	}

	mgr.mu.Lock()
	_, stillThere := mgr.rooms[strings.ToLower(room.ID)]
	mgr.mu.Unlock()
	if stillThere {
		t.Error("room still registered in the manager after delete")
	}

	// Sessions (member room history + shared log) must be gone.
	ctx := context.Background()
	for _, member := range room.Members {
		sess, err := mgr.Sessions().LoadSession(ctx, RoomSessionID(member, room.ID))
		if err == nil && sess != nil {
			t.Errorf("room session for %s survived the delete", member)
		}
	}
	if sess, err := mgr.Sessions().LoadSession(ctx, roomHistorySessionID(room.ID)); err == nil && sess != nil {
		t.Error("shared room log survived the delete")
	}
}

// TestRoomHistoryAndPromptWindowAreSeparate: MaxMessages is the PROMPT window
// (token cost), HistoryCap is what is persisted and shown to the human. They
// used to be the same number, so tuning the prompt window silently deleted the
// user's chat history after 10 messages.
func TestRoomHistoryAndPromptWindowAreSeparate(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	room := &RoomConfig{Name: "r", Members: []string{"alice", "bob"}, MaxMessages: 3, HistoryCap: 5}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		mgr.appendRoomMessage(room, "user", fmt.Sprintf("m%d", i), nil)
	}

	// Persisted/visible history keeps HistoryCap entries, not the prompt cap.
	msgs, err := mgr.RoomMessages(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("history length = %d, want the 5-message history cap", len(msgs))
	}
	if msgs[0].Content != "m1" || msgs[4].Content != "m5" {
		t.Errorf("history cap kept the wrong window: %s..%s", msgs[0].Content, msgs[4].Content)
	}

	// The prompt still carries only the last MaxMessages entries.
	prompt := mgr.buildRoomPrompt(room, "alice", msgs, 0, 1)
	if !strings.Contains(prompt, "m5") || !strings.Contains(prompt, "m3") {
		t.Errorf("prompt window dropped recent messages:\n%s", prompt)
	}
	if strings.Contains(prompt, "m1") || strings.Contains(prompt, "m2") {
		t.Errorf("prompt carried more than the MaxMessages window:\n%s", prompt)
	}
}

// TestRoomRoundsAreClamped keeps a hand-written max_rounds=1000 from turning
// one user message into an unbounded run of member turns.
func TestRoomRoundsAreClamped(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")
	room := &RoomConfig{Name: "r", Members: []string{"alice", "bob"}, MaxRounds: 1000}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	if got := room.Rounds(); got != MaxRoomRounds {
		t.Errorf("Rounds() = %d, want the %d ceiling", got, MaxRoomRounds)
	}
	if room.MaxRounds != MaxRoomRounds {
		t.Errorf("persisted MaxRounds = %d, want it clamped to %d", room.MaxRounds, MaxRoomRounds)
	}
}

// TestRoomHelpers covers the small pure helpers that shape a round.
func TestRoomHelpers(t *testing.T) {
	if got := prependMember([]string{"a", "b", "c"}, "c"); strings.Join(got, "") != "cab" {
		t.Errorf("prependMember = %v, want [c a b]", got)
	}
	if got := prependMember([]string{"a", "b"}, "zz"); strings.Join(got, "") != "zzab" {
		t.Errorf("prependMember with an unknown member = %v, want [zz a b]", got)
	}

	escalations := []string{"@user please decide", "@ user please decide", "  @USER decide", "Needs your input here", "Escalating to user now"}
	for _, s := range escalations {
		if !needsHuman(s) {
			t.Errorf("needsHuman(%q) = false, want true", s)
		}
	}
	normal := []string{
		"@alice what do you think",
		"looks good to me",
		"",
		// 正文里提到协议不算升级：旧实现用全串 Contains，成员只要复述一句
		// "然后升级给 user" 就会把整轮提前终止。
		"if the plan is unclear we could escalate to user later",
	}
	for _, s := range normal {
		if needsHuman(s) {
			t.Errorf("needsHuman(%q) = true, want false", s)
		}
	}
}

// TestBuildRoomPromptCarriesContextAndRoster: every member turn must see the
// topic, the roster, the recent messages and which round it is on.
func TestBuildRoomPromptCarriesContextAndRoster(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	room := &RoomConfig{Name: "Planning", Topic: "Ship v2", Members: []string{"alice", "bob"}, MaxMessages: 10}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	history := []RoomMessage{
		{From: "user", Content: "state of play?"},
		{From: "alice", Content: "on track"},
	}

	prompt := mgr.buildRoomPrompt(room, "bob", history, 1, 3)
	for _, want := range []string{
		"Group chat room: Planning",
		"Topic: Ship v2",
		"@alice",
		"@bob",
		"state of play?",
		"on track",
		"round 2/3",
		"You are @bob",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("room prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// An empty history is called out rather than left blank.
	empty := mgr.buildRoomPrompt(room, "alice", nil, 0, 3)
	if !strings.Contains(empty, "(no messages yet)") {
		t.Errorf("empty history not announced:\n%s", empty)
	}
}
