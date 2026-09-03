package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAccessControl_CheckDM(t *testing.T) {
	ac := DefaultAccessControl()

	if !ac.CheckDM("user1") {
		t.Error("open policy should allow all DMs")
	}

	ac.DMPolicy = AccessPolicyAllowlist
	ac.DMAllowlist = []string{"user1", "user2"}

	if !ac.CheckDM("user1") {
		t.Error("allowlist should include user1")
	}
	if ac.CheckDM("user3") {
		t.Error("allowlist should exclude user3")
	}

	ac.DMPolicy = AccessPolicyDisabled
	if ac.CheckDM("user1") {
		t.Error("disabled policy should block all DMs")
	}
}

func TestAccessControl_CheckGroup(t *testing.T) {
	ac := DefaultAccessControl()

	if !ac.CheckGroup("group1", true) {
		t.Error("open policy should allow mentioned group messages")
	}
	if ac.CheckGroup("group1", false) {
		t.Error("open policy should block non-mentioned group messages")
	}

	ac.GroupPolicy = AccessPolicyAllowlist
	ac.GroupAllowlist = []string{"group1"}

	if !ac.CheckGroup("group1", true) {
		t.Error("allowlist group with mention should pass")
	}
	if ac.CheckGroup("group2", true) {
		t.Error("non-allowlist group should be blocked")
	}
}

func TestBasePlatform_ShouldProcessChannel(t *testing.T) {
	bp := NewBasePlatform("test", nil)

	if !bp.ShouldProcessChannel("chan1") {
		t.Error("empty allowlist should allow all channels")
	}

	bp.SetChannelFilter([]string{"chan1", "chan2"}, nil)
	if !bp.ShouldProcessChannel("chan1") {
		t.Error("allowlisted channel should pass")
	}
	if bp.ShouldProcessChannel("chan3") {
		t.Error("non-allowlisted channel should be blocked")
	}

	bp.SetChannelFilter(nil, []string{"blocked1"})
	if !bp.ShouldProcessChannel("chan1") {
		t.Error("non-blocked channel should pass")
	}
	if bp.ShouldProcessChannel("blocked1") {
		t.Error("blocked channel should not pass")
	}
}

func TestPlatformRegistry_AllPlatforms(t *testing.T) {
	registry := GetRegistry()

	expected := []string{
		"qq", "telegram", "discord", "slack",
		"feishu", "wechat_ilink", "wecom", "dingtalk",
		"line", "matrix", "teams", "googlechat", "email", "sms",
	}

	for _, id := range expected {
		info, ok := registry.GetInfo(id)
		if !ok {
			t.Errorf("platform %s not found in registry", id)
			continue
		}
		if info.ID != id {
			t.Errorf("platform %s has wrong ID: %s", id, info.ID)
		}
	}

	all := registry.List()
	if len(all) != len(expected) {
		t.Errorf("expected %d platforms, got %d", len(expected), len(all))
	}
}

func TestBasePlatform_AccessControlInEmit(t *testing.T) {
	bp := NewBasePlatform("test", map[string]interface{}{
		"dm_policy":    string(AccessPolicyAllowlist),
		"dm_allowlist": []string{"allowed_user"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bp.onConnect = func(ctx context.Context) error { return nil }
	bp.Connect(ctx)

	msg := Message{
		ID:        "test1",
		ChannelID: "dm1",
		UserID:    "allowed_user",
		Content:   "hello",
		IsGroup:   false,
	}
	bp.EmitMessage(msg)

	select {
	case received := <-bp.Receive():
		if received.UserID != "allowed_user" {
			t.Error("expected allowed_user message")
		}
	default:
		t.Error("allowed user message should be emitted")
	}

	msg2 := Message{
		ID:        "test2",
		ChannelID: "dm2",
		UserID:    "blocked_user",
		Content:   "hello",
		IsGroup:   false,
	}
	bp.EmitMessage(msg2)

	select {
	case <-bp.Receive():
		t.Error("blocked user message should not be emitted")
	default:
	}
}

type fakeAgentHandler struct{}

func (f *fakeAgentHandler) Process(ctx context.Context, msg Message) (string, error) {
	return "ok", nil
}

func (f *fakeAgentHandler) ProcessWithStats(ctx context.Context, msg Message) (string, int, int, int, error) {
	return "ok", 0, 0, 0, nil
}

func (f *fakeAgentHandler) ResetSession(userID string) {}

// TestBasePlatform_ConnectConfirmation locks in the unified connection
// semantics: Connect() never flips a platform to "connected" on its own —
// only markConnected() after a real link does. It covers the four outcomes:
// async confirm, sync confirm inside onConnect, never-confirms (watchdog
// fallback to disconnected+error), and synchronous onConnect error.
func TestBasePlatform_ConnectConfirmation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1) Async platform: onConnect returns nil, markConnected arrives later
	//    from the run loop. Between the two, the platform must NOT report
	//    connected (this is the "reports connected before it actually
	//    connected" regression guard).
	async := NewBasePlatform("test_async", nil)
	async.onConnect = func(ctx context.Context) error {
		go func() {
			time.Sleep(50 * time.Millisecond)
			async.markConnected()
		}()
		return nil
	}
	if err := async.Connect(ctx); err != nil {
		t.Fatalf("async Connect returned error: %v", err)
	}
	if async.IsConnected() {
		t.Fatal("async platform reported connected before confirming its link")
	}
	waitConnected(t, async, "async platform")
	_ = async.Disconnect()

	// 2) Sync platform: handshake completes inside onConnect, which calls
	//    markConnected() before returning — connected right after Connect.
	syncP := NewBasePlatform("test_sync", nil)
	syncP.onConnect = func(ctx context.Context) error {
		syncP.markConnected()
		return nil
	}
	if err := syncP.Connect(ctx); err != nil {
		t.Fatalf("sync Connect returned error: %v", err)
	}
	if !syncP.IsConnected() {
		t.Fatal("sync platform should be connected right after Connect")
	}
	_ = syncP.Disconnect()

	// 3) Hung platform: onConnect returns nil but nothing ever confirms. The
	//    watchdog must fall back to disconnected with a clear error instead of
	//    leaving the UI on "connected" (or "connecting") forever.
	hung := NewBasePlatform("test_hung", map[string]interface{}{"connect_timeout_ms": 150})
	hung.onConnect = func(ctx context.Context) error { return nil }
	if err := hung.Connect(ctx); err != nil {
		t.Fatalf("hung Connect returned error (must be non-blocking): %v", err)
	}
	if hung.IsConnected() {
		t.Fatal("hung platform must not report connected")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := GetStatusManager().Get("test_hung")
		if st.State == StateDisconnected && strings.Contains(st.LastError, "not confirmed") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := GetStatusManager().Get("test_hung")
	if st.State != StateDisconnected {
		t.Fatalf("hung platform state = %q, want disconnected after watchdog timeout", st.State)
	}
	if !strings.Contains(st.LastError, "not confirmed") {
		t.Fatalf("hung platform error = %q, want a 'not confirmed' message", st.LastError)
	}
	_ = hung.Disconnect()

	// 4) Synchronous onConnect failure: Connect surfaces it and the platform
	//    ends disconnected (bad token / unreachable endpoint case).
	bad := NewBasePlatform("test_bad", nil)
	bad.onConnect = func(ctx context.Context) error { return fmt.Errorf("invalid token") }
	if err := bad.Connect(ctx); err == nil {
		t.Fatal("Connect must surface synchronous onConnect errors")
	}
	if bad.IsConnected() {
		t.Fatal("bad platform must not report connected")
	}
	if st := GetStatusManager().Get("test_bad"); st.State != StateDisconnected {
		t.Fatalf("bad platform state = %q, want disconnected", st.State)
	}
	_ = bad.Disconnect()
}

func waitConnected(t *testing.T, bp *BasePlatform, label string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bp.IsConnected() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not confirm connected within deadline", label)
}

// Regression test for the gateway startup deadlock: Gateway.Start() used to
// call mountConfiguredMiddleware() while still holding g.mu, and that helper
// calls g.Use() which takes g.mu again -> self-deadlock on the non-reentrant
// mutex. With RateLimitPerUser > 0 the middleware mount path is exercised, so
// Start() must still return promptly and the API server must come up.
func TestGatewayStart_NoDeadlockWithRateLimit(t *testing.T) {
	g := NewGateway(&fakeAgentHandler{}, &GatewayConfig{
		MaxSessions:      10,
		EnableSlashCmd:   true,
		EnableAPI:        true,
		APIPort:          0, // bind an ephemeral port, avoid clashes
		RateLimitPerUser: 20,
		RateLimitWindow:  time.Minute,
	})

	bp := NewBasePlatform("fake", nil)
	g.RegisterPlatform("fake", bp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- g.Start(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Gateway.Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Gateway.Start deadlocked: did not return within 3s (mountConfiguredMiddleware re-locking g.mu?)")
	}

	// The message handler goroutine for the fake platform must have been
	// launched; Stop() cleans up without hanging.
	if err := g.Stop(); err != nil {
		t.Fatalf("Gateway.Stop returned error: %v", err)
	}
}
