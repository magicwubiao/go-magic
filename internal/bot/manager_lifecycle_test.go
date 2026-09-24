package bot

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// TestRuntimeStatusCountsRoutinesForMixedCaseBot is the regression test for the
// "active routines is always 0" bug: routine files are named after the bot's
// ORIGINAL name, but the lookup used the lowercased map key, so any bot with an
// uppercase letter reported zero active routines forever.
func TestRuntimeStatusCountsRoutinesForMixedCaseBot(t *testing.T) {
	mgr := newTestManager(t, "MixedCase")

	if err := mgr.store.SaveRoutines("MixedCase", []*RoutineConfig{
		{ID: "r1", Name: "digest", Schedule: "0 9 * * *", Enabled: true},
		{ID: "r2", Name: "off", Schedule: "0 10 * * *", Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}

	state := mgr.RuntimeStatus("MixedCase")
	if state.ActiveRoutines != 1 {
		t.Errorf("ActiveRoutines = %d, want 1 (enabled routines are filed under the bot's original name)", state.ActiveRoutines)
	}
	if state.Name != "MixedCase" {
		t.Errorf("state.Name = %q, want the caller's spelling", state.Name)
	}
	if state.Status != StatusActive {
		t.Errorf("state.Status = %q, want %q", state.Status, StatusActive)
	}
	if state.QueueDepth != 0 || state.HistoryLength != 0 {
		t.Errorf("unexpected runtime depth: queue=%d history=%d", state.QueueDepth, state.HistoryLength)
	}

	// A bot that is not running still reports its configured routines.
	if got := mgr.RuntimeStatus("MixedCase"); got.ActiveRoutines != 1 {
		t.Errorf("repeat lookup changed the answer: %d", got.ActiveRoutines)
	}
}

// TestConcurrentRoutineWritesDoNotClobber: routine bookkeeping is a
// load -> mutate -> save cycle over a single JSON file. Without serialization,
// several routines finishing at once overwrite one another's last_run /
// last_status (and can drop whole entries). This test drives that race.
func TestConcurrentRoutineWritesDoNotClobber(t *testing.T) {
	mgr := newTestManager(t, "writer")

	const n = 8
	routines := make([]*RoutineConfig, 0, n)
	for i := 0; i < n; i++ {
		routines = append(routines, &RoutineConfig{
			ID:       fmt.Sprintf("r%d", i),
			Name:     fmt.Sprintf("routine-%d", i),
			Schedule: "0 9 * * *",
			Enabled:  true,
		})
	}
	if err := mgr.store.SaveRoutines("writer", routines); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mgr.recordRoutineResult("writer", fmt.Sprintf("r%d", i), "success", fmt.Sprintf("output %d", i))
		}(i)
	}
	wg.Wait()

	got, err := mgr.store.LoadRoutines("writer")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("routine list has %d entries, want %d (concurrent writes lost some)", len(got), n)
	}
	for _, r := range got {
		if r.LastStatus != "success" {
			t.Errorf("routine %s lost its status write-back: last_status=%q", r.ID, r.LastStatus)
		}
		if r.LastRun == nil {
			t.Errorf("routine %s has no last_run", r.ID)
		}
		if want := "output " + r.ID[1:]; r.LastResult != want {
			t.Errorf("routine %s result = %q, want %q", r.ID, r.LastResult, want)
		}
	}
}

// TestReloadConfigConcurrentWithReaders guards the hot-reload path: ReloadConfig
// swaps m.cfg on a live manager while workers read the bot_mode settings on
// every turn. Reading the pointer without the manager lock raced here (caught by
// `go test -race`).
func TestReloadConfigConcurrentWithReaders(t *testing.T) {
	mgr := newTestManager(t, "alice")

	base := mustLoadConfig(t)
	alt := mustLoadConfig(t)
	alt.BotMode = &config.BotModeConfig{Enabled: true, HistoryWindow: 42, TurnTimeoutMinutes: 2}

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				mgr.ReloadConfig(base)
			} else {
				mgr.ReloadConfig(alt)
			}
		}
	}()

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if w := mgr.historyWindow(); w < 20 {
					t.Errorf("historyWindow = %d, want >= 20", w)
					return
				}
				if d := mgr.turnTimeout(); d <= 0 {
					t.Errorf("turnTimeout = %v, want > 0", d)
					return
				}
				_ = mgr.RuntimeStatus("alice")
				_ = mgr.botWorkDir(&Config{Name: "alice"})
			}
		}()
	}
	wg.Wait()

	// The last writer wins; the value must be one of the two candidates.
	if w := mgr.historyWindow(); w != 200 && w != 42 {
		t.Errorf("historyWindow = %d, want 200 or 42", w)
	}
}

// TestManagerLifecycleAfterStop: once Stop() has run, no new worker or room
// coordinator may be spawned. Adding to the WaitGroup after Wait() started is a
// race (and panics with "WaitGroup is reused before previous Wait has returned").
// Stop() is also idempotent — the cleanup hook calls it a second time.
func TestManagerLifecycleAfterStop(t *testing.T) {
	mgr := newTestManager(t, "alice")

	mgr.Stop()
	mgr.Stop() // idempotent

	mgr.startBotLocked(&Config{Name: "late"})
	mgr.startRoomLocked(&RoomConfig{ID: "late-room", Name: "late", Members: []string{"alice", "bob"}})

	mgr.mu.Lock()
	_, botOnline := mgr.bots["late"]
	_, roomOnline := mgr.rooms["late-room"]
	stopping := mgr.stopping
	mgr.mu.Unlock()

	if !stopping {
		t.Error("manager should be flagged as stopping after Stop()")
	}
	if botOnline {
		t.Error("a bot came online after Stop()")
	}
	if roomOnline {
		t.Error("a room came online after Stop()")
	}
}

// TestDeleteBotCancelsInFlightTurn: a deleted bot must not keep running its
// current turn (it would keep burning tokens and write history for a bot that
// no longer exists). Queued messages are dropped too.
//
// The in-flight turn is simulated by installing a cancel func on the runtime
// rather than by enqueueing a message: enqueueing would make the worker start a
// real turn and overwrite turnCancel with its own context (which is exactly the
// production path, but not a deterministic assertion).
func TestDeleteBotCancelsInFlightTurn(t *testing.T) {
	mgr := newTestManager(t, "alice")

	canceled := make(chan struct{})
	var once sync.Once

	mgr.mu.Lock()
	rt := mgr.bots["alice"]
	if rt == nil {
		mgr.mu.Unlock()
		t.Fatal("bot runtime not registered")
	}
	rt.turnRunning = true
	rt.turnCancel = func() { once.Do(func() { close(canceled) }) }
	rt.queue = append(rt.queue, pendingMessage{Text: "queued while running"})
	mgr.mu.Unlock()

	if err := mgr.DeleteBot("alice"); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}

	select {
	case <-canceled:
	default:
		t.Error("DeleteBot did not cancel the in-flight turn")
	}
	if _, err := mgr.GetBot("alice"); err == nil {
		t.Error("bot config survived DeleteBot")
	}

	mgr.mu.Lock()
	_, stillRegistered := mgr.bots["alice"]
	mgr.mu.Unlock()
	if stillRegistered {
		t.Error("deleted bot is still registered")
	}

	if err := mgr.DeleteBot("alice"); err == nil {
		t.Error("deleting a missing bot should fail")
	}
}

// TestSetBotActivePausesQueue: a paused bot must not act on messages that were
// already queued, and the synchronous caller has to be told rather than left
// waiting for a reply that will never come.
func TestSetBotActivePausesQueue(t *testing.T) {
	mgr := newTestManager(t, "alice")

	cfg, err := mgr.SetBotActive("alice", false)
	if err != nil {
		t.Fatalf("SetBotActive(false): %v", err)
	}
	if cfg.IsActive() {
		t.Error("bot should be paused")
	}
	if cfg.Status != StatusPaused {
		t.Errorf("status = %q, want %q", cfg.Status, StatusPaused)
	}

	replyCh := make(chan turnResult, 1)
	if err := mgr.EnqueueMsg("alice", pendingMessage{Text: "hi", From: "user", replyCh: replyCh}); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-replyCh:
		if res.Err == nil {
			t.Error("a paused bot should report an error instead of replying")
		}
	case <-time.After(5 * time.Second):
		t.Error("a paused bot left its caller hanging")
	}

	// Resuming restores the active state.
	cfg, err = mgr.SetBotActive("alice", true)
	if err != nil {
		t.Fatalf("SetBotActive(true): %v", err)
	}
	if !cfg.IsActive() || cfg.Status != StatusActive {
		t.Errorf("bot not resumed: active=%v status=%q", cfg.IsActive(), cfg.Status)
	}
}
