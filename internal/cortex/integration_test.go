package cortex

import (
	"os"
	"testing"
	"time"
)

func TestFullPipeline(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cortex-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create manager
	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}

	// Test cases
	testCases := []struct {
		name         string
		input        string
		wantIntent   string
		wantComplex string
		wantTurns    int
	}{
		{
			name:         "simple task",
			input:        "run ls command",
			wantIntent:   "task",
			wantComplex:  "simple",
			wantTurns:    8,
		},
		{
			name:         "medium task",
			input:        "create a Python script to parse CSV files",
			wantIntent:   "task",
			wantComplex:  "medium",
			wantTurns:    15,
		},
		{
			name:         "advanced task",
			input:        "build a complete ETL pipeline and deploy to docker",
			wantIntent:   "task",
			wantComplex:  "advanced",
			wantTurns:    25,
		},
		{
			name:         "question",
			input:        "how do I install go?",
			wantIntent:   "question",
			wantComplex:  "simple",
		},
		{
			name:         "chitchat",
			input:        "hi",
			wantIntent:   "chitchat",
			wantComplex:  "simple",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Process user message (full pipeline: Perception → Cognition)
			mgr.OnUserMessage(tc.input)

			// Verify perception results
			if string(mgr.GetIntent()) != tc.wantIntent {
				t.Errorf("Got intent %q, want %q", mgr.GetIntent(), tc.wantIntent)
			}

			// Verify cognition results for tasks
			if tc.wantIntent == "task" {
				turns := mgr.GetRecommendedMaxTurns()
				if turns != tc.wantTurns {
					t.Errorf("Got %d max turns, want %d", turns, tc.wantTurns)
				}

				plan := mgr.GetExecutionPlan()
				if plan == nil {
					t.Error("Expected execution plan, got nil")
				} else if len(plan.Steps) < 2 {
					t.Errorf("Expected at least 2 steps, got %d", len(plan.Steps))
				}
			}

			// Memory should have been updated
			mem := mgr.Snapshot.GetLatestMemory()
			if mem == "" {
				t.Error("Expected memory to be updated, got empty")
			}
		})
	}
}

func TestFrozenSnapshotPattern(t *testing.T) {
	// This test verifies the critical frozen snapshot pattern
	// that protects prefix cache from invalidation

	tmpDir, _ := os.MkdirTemp("", "cortex-snapshot")
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	mgr.Start()

	// Initial state - empty memory
	before := mgr.GetPromptContext()

	// Turn starts - snapshot frozen
	mgr.OnTurnStart()

	// Memory is updated DURING the turn (written to disk)
	mgr.AppendMemory("- Important insight from current task")
	mgr.AppendMemory("- User prefers Python")

	// BUT GetPromptContext still returns OLD value (frozen)
	// This is what protects the prefix cache!
	duringTurn := mgr.GetPromptContext()
	if duringTurn != before {
		t.Error("Prompt context changed during turn! Prefix cache would be INVALIDATED")
	}

	// Turn ends - next session would refresh (we simulate here)
	mgr.OnSessionEnd()

	// NOW after session end, memory is refreshed
	after := mgr.GetPromptContext()
	if after == before {
		t.Error("Prompt context was not refreshed after session end")
	}

	t.Log("✓ Frozen snapshot pattern working correctly")
	t.Logf("  Before: %d chars", len(before))
	t.Logf("  During: %d chars (same as before, cache protected)", len(duringTurn))
	t.Logf("  After:  %d chars (refreshed)", len(after))
}

func TestNudgeMechanism(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "cortex-nudge")
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	mgr.Start()

	// Set threshold low for testing
	mgr.Trigger.SetNudgeThreshold(3)

	// Register nudge handler
	nudgeCalled := false
	mgr.Trigger.RegisterNudgeHandler(func() {
		nudgeCalled = true
		t.Log("Nudge triggered! Background review would run now")
	})

	// Send 3 messages - should trigger nudge
	for i := 0; i < 3; i++ {
		mgr.OnUserMessage("Test message")
	}

	// Give async handler time to run
	time.Sleep(50 * time.Millisecond)

	if !nudgeCalled {
		t.Error("Nudge was not triggered at threshold")
	}
}
