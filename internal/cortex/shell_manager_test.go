package cortex

import "testing"

// Regression: a Manager created with Enabled=false is an empty shell — every
// subsystem (Snapshot, Trigger, ...) stays nil. Agents only nil-check the
// Manager itself before calling the prompt getters, so those getters must be
// safe on the shell instead of panicking with a nil-pointer dereference
// (which killed the SSE stream and surfaced as "Connection lost" in the UI).
func TestDisabledManagerShellIsSafe(t *testing.T) {
	mgr := NewManagerWithProfileAndConfig(t.TempDir(), nil, "", &ManagerConfig{Enabled: false})
	if mgr.IsEnabled() {
		t.Fatal("manager should be disabled")
	}

	if got := mgr.GetPromptContext(); got != "" {
		t.Fatalf("GetPromptContext = %q, want empty", got)
	}
	if got := mgr.GetUserContext(); got != "" {
		t.Fatalf("GetUserContext = %q, want empty", got)
	}
	if got := mgr.GetMemoryVersion(); got != 0 {
		t.Fatalf("GetMemoryVersion = %d, want 0", got)
	}
	if err := mgr.AppendMemory("line"); err == nil {
		t.Fatal("AppendMemory on shell should error, not panic")
	}
	if err := mgr.AppendUser("line"); err == nil {
		t.Fatal("AppendUser on shell should error, not panic")
	}
	// Start/OnUserMessage already gate on enabled — just confirm no panic.
	_ = mgr.Start()
	mgr.OnUserMessage("hello")
}
