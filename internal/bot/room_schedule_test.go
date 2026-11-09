package bot

import (
	"strings"
	"testing"
)

// roomEntryCount reports how many cron entries the live coordinator has for a
// room (test helper; same package so it can read the scheduler state).
func roomEntryCount(m *Manager, roomID string) int {
	m.mu.Lock()
	rt := m.rooms[strings.ToLower(roomID)]
	m.mu.Unlock()
	if rt == nil || rt.sched == nil {
		return 0
	}
	rt.sched.mu.Lock()
	defer rt.sched.mu.Unlock()
	return len(rt.sched.entries)
}

// TestRoomScheduleLifecycle covers create -> update -> pause -> delete for a
// scheduled room push, including that the live cron entries follow along
// (a schedule edit must not need a restart).
func TestRoomScheduleLifecycle(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")
	room := &RoomConfig{Name: "standup", Members: []string{"alice", "bob"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	cfg, err := mgr.AddRoomSchedule(room.ID, &RoomSchedule{
		Name:     "早会",
		Schedule: "0 9 * * *",
		Prompt:   "请各自汇报进度",
		Target:   "@alice",
	})
	if err != nil {
		t.Fatalf("AddRoomSchedule: %v", err)
	}
	if len(cfg.Schedules) != 1 {
		t.Fatalf("schedules = %d, want 1", len(cfg.Schedules))
	}
	sc := cfg.Schedules[0]
	if sc.ID == "" || sc.CreatedAt == 0 {
		t.Fatalf("schedule missing generated identity: %+v", sc)
	}
	if sc.Target != "alice" {
		t.Errorf("target = %q, want the @ stripped tag", sc.Target)
	}
	if got := roomEntryCount(mgr, room.ID); got != 1 {
		t.Fatalf("cron entries = %d, want 1", got)
	}

	// Persisted, not just in memory.
	stored, err := mgr.GetRoom(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Schedules) != 1 || stored.Schedules[0].Prompt != "请各自汇报进度" {
		t.Fatalf("schedule not persisted: %+v", stored.Schedules)
	}

	// Pausing removes the live entry but keeps the config.
	if _, err := mgr.UpdateRoomSchedule(room.ID, sc.ID, func(s *RoomSchedule) { s.Disabled = true }); err != nil {
		t.Fatalf("pause schedule: %v", err)
	}
	if got := roomEntryCount(mgr, room.ID); got != 0 {
		t.Fatalf("paused schedule still has %d cron entr(ies)", got)
	}
	if _, err := mgr.UpdateRoomSchedule(room.ID, sc.ID, func(s *RoomSchedule) { s.Disabled = false }); err != nil {
		t.Fatalf("resume schedule: %v", err)
	}
	if got := roomEntryCount(mgr, room.ID); got != 1 {
		t.Fatalf("resumed schedule has %d cron entr(ies), want 1", got)
	}

	// Changing the expression replaces the entry rather than stacking a second.
	if _, err := mgr.UpdateRoomSchedule(room.ID, sc.ID, func(s *RoomSchedule) { s.Schedule = "*/5 * * * *" }); err != nil {
		t.Fatalf("edit schedule: %v", err)
	}
	if got := roomEntryCount(mgr, room.ID); got != 1 {
		t.Fatalf("cron entries = %d after edit, want 1", got)
	}

	cfg, err = mgr.RemoveRoomSchedule(room.ID, sc.ID)
	if err != nil {
		t.Fatalf("RemoveRoomSchedule: %v", err)
	}
	if len(cfg.Schedules) != 0 {
		t.Fatalf("schedules = %d after delete, want 0", len(cfg.Schedules))
	}
	if got := roomEntryCount(mgr, room.ID); got != 0 {
		t.Fatalf("cron entries = %d after delete, want 0", got)
	}
}

// TestRoomScheduleValidation rejects bad input *before* it is persisted: a
// room saved with an unparseable cron would silently never fire.
func TestRoomScheduleValidation(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")
	room := &RoomConfig{Name: "validate", Members: []string{"alice", "bob"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		sc   *RoomSchedule
	}{
		{"invalid cron", &RoomSchedule{Schedule: "not a cron", Prompt: "x"}},
		{"missing prompt", &RoomSchedule{Schedule: "0 9 * * *"}},
		{"missing schedule", &RoomSchedule{Prompt: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mgr.AddRoomSchedule(room.ID, tc.sc); err == nil {
				t.Fatalf("AddRoomSchedule(%+v) succeeded, want an error", tc.sc)
			}
		})
	}
	stored, err := mgr.GetRoom(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Schedules) != 0 {
		t.Fatalf("rejected schedules leaked into the room: %+v", stored.Schedules)
	}
}

// TestRoomScheduleDiesWithRoom: deleting a room must stop its ticker, or a
// deleted room would keep pushing rounds into a log that no longer exists.
func TestRoomScheduleDiesWithRoom(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")
	room := &RoomConfig{Name: "short-lived", Members: []string{"alice", "bob"}}
	if err := mgr.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddRoomSchedule(room.ID, &RoomSchedule{Schedule: "0 9 * * *", Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}

	mgr.mu.Lock()
	rt := mgr.rooms[strings.ToLower(room.ID)]
	mgr.mu.Unlock()
	if rt == nil {
		t.Fatal("room runtime missing")
	}

	if err := mgr.DeleteRoom(room.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	rt.sched.mu.Lock()
	stopped := rt.sched.stopped
	rt.sched.mu.Unlock()
	if !stopped {
		t.Fatal("room scheduler still running after the room was deleted")
	}
}
