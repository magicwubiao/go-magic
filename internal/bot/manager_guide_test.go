package bot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGuideBotFallsBackWhenIdle: with no turn in flight there is nothing to
// steer — GuideBot must report injected=false (and no error) so the caller
// can fall back to a regular send.
func TestGuideBotFallsBackWhenIdle(t *testing.T) {
	mgr := newTestManager(t, "alice")

	injected, err := mgr.GuideBot("alice", "改用 SQLite")
	if err != nil {
		t.Fatalf("GuideBot: %v", err)
	}
	if injected {
		t.Error("GuideBot reported injection with no turn running")
	}
}

// TestGuideBotUnknownBot: an unknown bot name is an error, not a silent
// fallback — the UI should surface it rather than send to nobody.
func TestGuideBotUnknownBot(t *testing.T) {
	mgr := newTestManager(t, "alice")

	if _, err := mgr.GuideBot("ghost", "hi"); err == nil {
		t.Error("GuideBot for a missing bot should fail")
	}
}

// TestGuideBotBlankTextNoInboxGrowth: blank guides are silently ignored by
// InjectGuide, so a blank steer must not leave residue in the inbox.
func TestGuideBotBlankTextNoInboxGrowth(t *testing.T) {
	mgr := newTestManager(t, "alice")

	mgr.mu.Lock()
	rt := mgr.bots["alice"]
	if rt == nil {
		mgr.mu.Unlock()
		t.Fatal("bot runtime not registered")
	}
	// Simulate a running turn with a live agent; no worker is processing, so
	// nothing will drain the inbox behind the test's back.
	rt.turnRunning = true
	ag, err := mgr.getOrCreateAgentLocked(rt, "")
	if err != nil {
		rt.turnRunning = false
		mgr.mu.Unlock()
		t.Fatalf("getOrCreateAgentLocked: %v", err)
	}
	mgr.mu.Unlock()
	t.Cleanup(func() {
		mgr.mu.Lock()
		rt.turnRunning = false
		mgr.mu.Unlock()
	})

	injected, err := mgr.GuideBot("alice", "   \n\t  ")
	if err != nil || !injected {
		t.Fatalf("GuideBot blank text: injected=%v err=%v, want true/nil", injected, err)
	}
	if items := ag.DrainGuideItems(); len(items) != 0 {
		t.Errorf("blank guide left %d items in the inbox, want 0", len(items))
	}
}

// TestGuideBotConsumedByNextIteration: a guide injected while the turn is
// waiting on its first LLM response is drained at the top of the next agent
// iteration — the second call's trailing user message carries the "[Guide]"
// prefix, and the turn still completes normally.
func TestGuideBotConsumedByNextIteration(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	sawGuide := ""

	// guideSent blocks the mock's first response until the test has steered,
	// proving the turn was running when GuideBot fired.
	guideSent := make(chan struct{})
	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		mu.Lock()
		n := calls
		calls++
		mu.Unlock()

		if n == 0 && strings.Contains(lastUser, "delegate") {
			<-guideSent
			args, _ := json.Marshal(map[string]string{"target": "bob", "message": "please draft"})
			return toolCallResponse("call1", "message_agent", string(args))
		}
		// After the message_agent tool result, the next iteration drains the
		// guide: the trailing user message is "[Guide] <text>".
		if strings.HasPrefix(lastUser, "[Guide] ") {
			mu.Lock()
			sawGuide = lastUser
			mu.Unlock()
			return textResponse("steered")
		}
		return textResponse("done")
	})
	_ = cfg

	mgr, err := NewManager(mustLoadConfig(t))
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	go func() { _, _ = mgr.SendToBot("alice", "delegate this task please") }()

	// Wait until the first LLM call is in flight (turnRunning is guaranteed
	// by then: processMessage flips it before runTurn) and steer.
	waitFor(t, 15*time.Second, "the bot's first LLM call", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 1
	})
	injected, err := mgr.GuideBot("alice", "聚焦后端性能")
	if err != nil {
		t.Fatalf("GuideBot: %v", err)
	}
	if !injected {
		t.Fatal("GuideBot not injected although the turn was running")
	}
	close(guideSent)

	waitFor(t, 15*time.Second, "the guide to reach the next LLM call", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawGuide != ""
	})
	if sawGuide != "[Guide] 聚焦后端性能" {
		t.Errorf("steered call lastUser = %q, want %q", sawGuide, "[Guide] 聚焦后端性能")
	}
}

// TestGuideBotReclaimedWhenUnconsumed: the model was already producing its
// final answer when the user steered, so the turn ends without draining the
// inbox. processMessage must reclaim the leftover guide as a new queued user
// turn (mirroring the web chat queue's reclaimLeftoverGuides) — the guide
// then arrives at the mock as a plain user message with no prefix, and the
// persisted history contains it.
func TestGuideBotReclaimedWhenUnconsumed(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	sawReclaimed := false

	firstCallHeld := make(chan struct{})
	releaseFirst := make(chan struct{})
	// Idempotent release: the explicit close below runs on the happy path,
	// the deferred one only fires when an assertion failed earlier — without
	// the Once the double close would panic and mask the real failure.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	defer release()

	cfg, _, _ := setupEnv(t, func(m *mockLLM, lastUser string) map[string]interface{} {
		mu.Lock()
		n := calls
		calls++
		mu.Unlock()

		if n == 0 {
			close(firstCallHeld)
			<-releaseFirst
			// Final answer right away: the inbox never gets drained by this
			// turn, forcing the reclaim path.
			return textResponse("final answer")
		}
		if lastUser == "改用 SQLite 存储" {
			mu.Lock()
			sawReclaimed = true
			mu.Unlock()
		}
		return textResponse("ok")
	})
	_ = cfg

	mgr, err := NewManager(mustLoadConfig(t))
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	go func() { _, _ = mgr.SendToBot("alice", "写个 demo") }()

	<-firstCallHeld
	injected, err := mgr.GuideBot("alice", "改用 SQLite 存储")
	if err != nil {
		t.Fatalf("GuideBot: %v", err)
	}
	if !injected {
		t.Fatal("GuideBot not injected although the turn was running")
	}
	release()

	waitFor(t, 15*time.Second, "the unconsumed guide to be reclaimed as a new turn", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawReclaimed
	})

	// The reclaimed turn runs as an ordinary user message: the history must
	// contain it verbatim (no [Guide] prefix), persisted via saveHistory.
	hist := mgr.loadHistory(CanonicalSessionID("alice"))
	found := false
	for _, msg := range hist {
		if msg.Role == "user" && msg.Content == "改用 SQLite 存储" {
			found = true
		}
	}
	if !found {
		t.Error("reclaimed guide missing from persisted history as a user message")
	}
}
