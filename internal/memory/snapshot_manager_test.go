package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnapshotCreation tests snapshot creation and basic operations
func TestSnapshotCreation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Test initial state
	if manager.GetVersion() != 1 {
		t.Errorf("initial version should be 1, got %d", manager.GetVersion())
	}

	// Test memory paths
	memoryPath := filepath.Join(tmpDir, "MEMORY.md")
	userPath := filepath.Join(tmpDir, "USER.md")

	// Update memory
	testMemory := "# Test Memory\n- User works with Python\n- Prefers dark mode"
	err = manager.UpdateMemory(testMemory)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		t.Error("MEMORY.md should exist")
	}

	// Update user
	testUser := "# User Profile\n- Name: Test User\n- Language: English"
	err = manager.UpdateUser(testUser)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		t.Error("USER.md should exist")
	}
}

// TestFrozenSnapshotMode tests that frozen snapshot is not updated mid-turn
func TestFrozenSnapshotMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Load initial state
	initialMemory := "# Initial Memory"
	manager.UpdateMemory(initialMemory)
	manager.RefreshSnapshot() // Freeze it

	// Get frozen snapshot
	frozenBefore := manager.GetMemoryForPrompt()

	// Update memory (simulates mid-turn update)
	manager.UpdateMemory("# Updated Memory - New Info Added")

	// Frozen snapshot should NOT change
	frozenAfter := manager.GetMemoryForPrompt()
	if frozenBefore != frozenAfter {
		t.Error("frozen snapshot should not change after UpdateMemory mid-turn")
	}

	// Latest memory should be updated
	latest := manager.GetLatestMemory()
	if !strings.Contains(latest, "Updated Memory") {
		t.Error("latest memory should contain updated content")
	}

	// OnTurnStart should not refresh (frozen remains frozen)
	manager.OnTurnStart()
	frozenStill := manager.GetMemoryForPrompt()
	if frozenStill != frozenBefore {
		t.Error("frozen snapshot should remain unchanged after OnTurnStart")
	}
}

// TestSnapshotCharacterLimits tests character limit enforcement
func TestSnapshotCharacterLimits(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	tests := []struct {
		name       string
		updateFunc func(string) error
		content    string
		wantLimit  int
	}{
		{
			name:       "memory_within_limit",
			updateFunc: manager.UpdateMemory,
			content:    "Short memory content",
			wantLimit:  MemoryLimitChars,
		},
		{
			name:       "memory_exceeds_limit",
			updateFunc: manager.UpdateMemory,
			content:    strings.Repeat("x", MemoryLimitChars+1000),
			wantLimit:  MemoryLimitChars,
		},
		{
			name:       "user_within_limit",
			updateFunc: manager.UpdateUser,
			content:    "Short user profile",
			wantLimit:  UserLimitChars,
		},
		{
			name:       "user_exceeds_limit",
			updateFunc: manager.UpdateUser,
			content:    strings.Repeat("x", UserLimitChars+500),
			wantLimit:  UserLimitChars,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.updateFunc(tt.content)
			if err != nil {
				t.Fatal(err)
			}

			latest := manager.GetLatestMemory()
			if tt.updateFunc == manager.UpdateUser {
				latest = manager.GetLatestUser()
			}

			if len(latest) > tt.wantLimit {
				t.Errorf("content length %d exceeds limit %d", len(latest), tt.wantLimit)
			}
		})
	}
}

// TestAppendToMemory tests memory append functionality
func TestAppendToMemory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Initial memory
	manager.UpdateMemory("# Memory\n- Entry 1")

	// Append new line
	err = manager.AppendToMemory("- Entry 2")
	if err != nil {
		t.Fatal(err)
	}

	latest := manager.GetLatestMemory()
	if !strings.Contains(latest, "Entry 1") {
		t.Error("should contain Entry 1")
	}
	if !strings.Contains(latest, "Entry 2") {
		t.Error("should contain Entry 2")
	}
}

// TestAppendToUser tests user profile append functionality
func TestAppendToUser(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Initial user
	manager.UpdateUser("# User\nName: Test")

	// Append new line
	err = manager.AppendToUser("Language: English")
	if err != nil {
		t.Fatal(err)
	}

	latest := manager.GetLatestUser()
	if !strings.Contains(latest, "Name: Test") {
		t.Error("should contain original content")
	}
	if !strings.Contains(latest, "Language: English") {
		t.Error("should contain appended content")
	}
}

// TestRefreshSnapshot tests snapshot refresh
func TestRefreshSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Set initial state
	manager.UpdateMemory("# Initial Memory")
	manager.RefreshSnapshot()

	// Get frozen before update
	frozenBefore := manager.GetMemoryForPrompt()

	// Update memory
	manager.UpdateMemory("# Updated Memory")

	// Frozen should still be old
	if frozenBefore != manager.GetMemoryForPrompt() {
		t.Error("frozen should not change after mid-turn update")
	}

	// Refresh should update frozen
	initialVersion := manager.GetVersion()
	manager.RefreshSnapshot()

	if manager.GetVersion() != initialVersion+1 {
		t.Error("version should increment after refresh")
	}

	// Now frozen should match latest
	frozenAfter := manager.GetMemoryForPrompt()
	latest := manager.GetLatestMemory()
	if frozenAfter != latest {
		t.Error("after refresh, frozen should match latest")
	}
}

// TestLoad tests loading memory from disk
func TestLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create manager and save memory
	manager1 := NewSnapshotManager(tmpDir)
	manager1.UpdateMemory("# Test Memory Content")
	manager1.UpdateUser("# Test User Content")

	// Create new manager instance
	manager2 := NewSnapshotManager(tmpDir)
	err = manager2.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Verify loaded content
	memory := manager2.GetLatestMemory()
	user := manager2.GetLatestUser()

	if !strings.Contains(memory, "Test Memory Content") {
		t.Error("memory should contain saved content")
	}
	if !strings.Contains(user, "Test User Content") {
		t.Error("user should contain saved content")
	}
}

// TestGetMemoryForPrompt tests frozen snapshot for prompts
func TestGetMemoryForPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Set memory and freeze
	manager.UpdateMemory("# Frozen Memory")
	manager.RefreshSnapshot()

	// Get for prompt
	promptMemory := manager.GetMemoryForPrompt()
	if !strings.Contains(promptMemory, "Frozen Memory") {
		t.Error("prompt memory should contain frozen content")
	}

	// Update memory (simulates another session)
	manager.UpdateMemory("# New Memory Content")

	// Prompt should still return frozen version
	if promptMemory != manager.GetMemoryForPrompt() {
		t.Error("prompt should return same frozen snapshot")
	}
}

// TestGetUserForPrompt tests frozen user profile for prompts
func TestGetUserForPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Set user and freeze
	manager.UpdateUser("# User Profile\nName: Test")
	manager.RefreshSnapshot()

	// Get for prompt
	promptUser := manager.GetUserForPrompt()
	if !strings.Contains(promptUser, "User Profile") {
		t.Error("prompt user should contain frozen content")
	}
}

// TestOnTurnStart tests turn start behavior
func TestOnTurnStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Set and freeze
	manager.UpdateMemory("# Memory")
	manager.RefreshSnapshot()
	frozenBefore := manager.GetMemoryForPrompt()

	// Call OnTurnStart multiple times (simulating multiple turns in same session)
	manager.OnTurnStart()
	manager.OnTurnStart()
	manager.OnTurnStart()

	// Frozen should remain same
	if frozenBefore != manager.GetMemoryForPrompt() {
		t.Error("frozen should remain unchanged across turns")
	}
}

// TestVersionTracking tests version number tracking
func TestVersionTracking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	initialVersion := manager.GetVersion()
	if initialVersion != 1 {
		t.Errorf("initial version should be 1, got %d", initialVersion)
	}

	// Refresh should increment version
	manager.RefreshSnapshot()
	if manager.GetVersion() != 2 {
		t.Error("version should increment after refresh")
	}

	// Update should not change version
	manager.UpdateMemory("# New Memory")
	if manager.GetVersion() != 2 {
		t.Error("version should not change after update")
	}

	// Another refresh
	manager.RefreshSnapshot()
	if manager.GetVersion() != 3 {
		t.Error("version should be 3 after second refresh")
	}
}

// TestCompression tests memory compression behavior
func TestCompression(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)

	// Create long memory content
	longMemory := ""
	for i := 0; i < 100; i++ {
		longMemory += "Line " + string(rune('0'+i%10)) + " of memory content\n"
	}

	err = manager.UpdateMemory(longMemory)
	if err != nil {
		t.Fatal(err)
	}

	latest := manager.GetLatestMemory()

	// Should be compressed/truncated to fit limit
	if len(latest) > MemoryLimitChars {
		t.Errorf("compressed memory should be within limit, got %d chars", len(latest))
	}
}

// TestConcurrentAccess tests thread safety (simplified)
func TestConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)
	manager.UpdateMemory("# Initial")
	manager.RefreshSnapshot()

	// Sequential reads and writes
	for i := 0; i < 10; i++ {
		// Read
		_ = manager.GetMemoryForPrompt()
		_ = manager.GetLatestMemory()

		// Write (but don't refresh)
		manager.UpdateMemory("# Update " + string(rune('0'+i)))
	}

	// Final state should be consistent
	frozen := manager.GetMemoryForPrompt()
	if !strings.Contains(frozen, "Initial") {
		t.Error("frozen should still have initial content")
	}
}

// TestTruncateString tests string truncation
func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		limit    int
		wantLen  int
		wantEllipsis bool
	}{
		{"short", 10, 5, false},
		{"exactly", 7, 7, false},
		{"long string", 5, 5, true},
		{"", 10, 0, false},
	}

	for _, tt := range tests {
		result := truncateString(tt.input, tt.limit)

		if len(result) != tt.wantLen {
			t.Errorf("truncateString(%q, %d): got len %d, want %d",
				tt.input, tt.limit, len(result), tt.wantLen)
		}

		if tt.wantEllipsis && !strings.HasSuffix(result, "...") {
			t.Error("expected ellipsis suffix")
		}
	}
}

// BenchmarkSnapshotOperations benchmarks snapshot operations
func BenchmarkSnapshotOperations(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "snapshot-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)
	manager.UpdateMemory("# Initial")
	manager.RefreshSnapshot()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.UpdateMemory("# Update content")
		_ = manager.GetMemoryForPrompt()
	}
}

// BenchmarkGetFrozenSnapshot benchmarks frozen snapshot retrieval
func BenchmarkGetFrozenSnapshot(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "snapshot-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)
	manager.UpdateMemory("# Memory content with details")
	manager.RefreshSnapshot()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.GetMemoryForPrompt()
	}
}

// BenchmarkAppendMemory benchmarks memory append performance
func BenchmarkAppendMemory(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "snapshot-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)
	manager.UpdateMemory("# Initial")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.AppendToMemory("- Entry")
	}
}

// BenchmarkRefreshSnapshot benchmarks refresh performance
func BenchmarkRefreshSnapshot(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "snapshot-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewSnapshotManager(tmpDir)
	manager.UpdateMemory("# Memory")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.RefreshSnapshot()
	}
}
