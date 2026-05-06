package cortex_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/cortex"
)

func TestManagerFlow(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cortex-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Cortex manager
	mgr := cortex.NewManager(tmpDir)

	// Start the manager
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}

	// Simulate a conversation flow
	t.Log("=== Simulating conversation flow ===")

	// User sends first message
	mgr.OnUserMessage("Create a Python script that reads CSV files")
	if mgr.GetTurnCount() != 1 {
		t.Errorf("Expected turn count 1, got %d", mgr.GetTurnCount())
	}

	// LLM turn starts - snapshot is frozen
	mgr.OnTurnStart()

	// Mid-turn memory update (written to disk, but not loaded into prompt)
	// This is the key Cortex pattern: write immediately, load on next session
	mgr.AppendMemory("- User often asks for Python scripts")
	mgr.AppendMemory("- CSV processing is a recurring task")

	// Turn ends - snapshot remains frozen (prefix cache intact!)
	mgr.OnTurnEnd()

	// User sends second message
	mgr.OnUserMessage("Now add error handling to the script")
	if mgr.GetTurnCount() != 2 {
		t.Errorf("Expected turn count 2, got %d", mgr.GetTurnCount())
	}

	// Second turn starts - still using frozen snapshot
	// Prefix cache is NOT invalidated by previous turn's memory updates
	mgr.OnTurnStart()

	// More memory updates during turn
	mgr.AppendMemory("- Error handling is important for user scripts")

	// Session ends - NOW the snapshot refreshes
	// Future sessions will see all memory updates
	mgr.OnSessionEnd()

	t.Log("Turn count:", mgr.GetTurnCount())
	t.Log("Memory version:", mgr.GetMemoryVersion())

	// Verify MEMORY.md was created
	memoryPath := filepath.Join(tmpDir, "MEMORY.md")
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		t.Error("MEMORY.md was not created")
	} else {
		content, _ := os.ReadFile(memoryPath)
		t.Logf("MEMORY.md content:\n%s", string(content))
	}
}

func TestFrozenSnapshotPattern(t *testing.T) {
	// This test demonstrates the critical "frozen snapshot" pattern
	// from Cortex Agent that protects prefix cache

	tmpDir, _ := os.MkdirTemp("", "cortex-snapshot")
	defer os.RemoveAll(tmpDir)

	mgr := cortex.NewManager(tmpDir)
	mgr.Start()

	// Initial state
	before := mgr.GetPromptContext()

	// Turn 1 starts
	mgr.OnTurnStart()

	// Memory is updated during the turn (written to disk)
	mgr.AppendMemory("- Important insight from turn 1")

	// BUT GetPromptContext still returns OLD value (frozen)
	// This is what protects the prefix cache!
	duringTurn := mgr.GetPromptContext()
	if duringTurn != before {
		t.Error("Prompt context changed during turn! Prefix cache would be invalidated")
	}

	// Turn ends (many more turns could happen)
	// ...

	// Session ends - NOW the snapshot refreshes
	mgr.OnSessionEnd()

	// NEXT session will see the updated memory
	afterSession := mgr.GetPromptContext()
	if afterSession == before {
		t.Error("Prompt context did not refresh after session end")
	}

	t.Log("✓ Frozen snapshot pattern working correctly")
	t.Log("  - During turn: memory frozen, cache protected")
	t.Log("  - After session: memory refreshed, next session gets updates")
}

func TestNudgeMechanism(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "cortex-nudge")
	defer os.RemoveAll(tmpDir)

	mgr := cortex.NewManager(tmpDir)
	mgr.Start()

	// Register a nudge handler
	nudgeCalled := false
	mgr.Trigger.RegisterNudgeHandler(func() {
		nudgeCalled = true
		t.Log("Nudge triggered! Background review would run now")
	})

	// Set threshold low for testing
	mgr.Trigger.SetNudgeThreshold(3)

	// Send 3 messages - should trigger nudge
	for i := 0; i < 3; i++ {
		mgr.OnUserMessage("Test message")
	}

	// Give async handler time to run
	time.Sleep(100 * time.Millisecond)

	if !nudgeCalled {
		t.Error("Nudge was not triggered at threshold")
	}

	t.Log("✓ Nudge mechanism working correctly")
}
