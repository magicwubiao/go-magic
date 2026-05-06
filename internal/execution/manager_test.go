package execution

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/cognition"
)

// TestCheckpointPersistence tests checkpoint save and load
func TestCheckpointPersistence(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Create a plan
	plan := &cognition.ExecutionPlan{
		Description: "Test task",
		Steps: []cognition.Step{
			{ID: 1, Description: "Step 1"},
			{ID: 2, Description: "Step 2"},
		},
	}

	// Start checkpoint
	checkpoint := manager.StartCheckpoint("test-task", plan)
	if checkpoint == nil {
		t.Fatal("expected checkpoint, got nil")
	}

	// Update checkpoint
	manager.UpdateCheckpoint(checkpoint, 2, "Executing Step 2")
	manager.UpdateTurnCount(checkpoint, 5)

	// Store tool result
	manager.StoreToolResult(checkpoint, "read_file", map[string]string{"content": "test data"})

	// Verify checkpoint file exists
	checkpointPath := filepath.Join(tmpDir, "checkpoints", checkpoint.ID+".json")
	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
		t.Error("checkpoint file should exist")
	}

	// Complete checkpoint
	manager.CompleteCheckpoint(checkpoint)

	if !checkpoint.IsFinal {
		t.Error("checkpoint should be marked as final")
	}
}

// TestResumeFromCheckpoint tests checkpoint resume functionality
func TestResumeFromCheckpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Create and save a checkpoint
	plan := &cognition.ExecutionPlan{
		Description: "Resume test task",
		Steps: []cognition.Step{
			{ID: 1, Description: "Step 1"},
			{ID: 2, Description: "Step 2"},
		},
	}

	original := manager.StartCheckpoint("resume-task", plan)
	manager.UpdateCheckpoint(original, 2, "Step 2")
	manager.StoreToolResult(original, "write_file", "success")

	// Find checkpoint for same task
	resumed := manager.FindCheckpoint("Resume test task")
	if resumed == nil {
		t.Fatal("expected to find checkpoint for resume")
	}

	if resumed.StepID != 2 {
		t.Errorf("expected step 2, got %d", resumed.StepID)
	}

	if resumed.StepName != "Step 2" {
		t.Errorf("expected step name 'Step 2', got %s", resumed.StepName)
	}
}

// TestFailureRecovery tests intelligent failure recovery
func TestFailureRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	plan := &cognition.ExecutionPlan{Description: "Recovery test"}
	checkpoint := manager.StartCheckpoint("recovery-test", plan)
	checkpoint.StepID = 1

	tests := []struct {
		name          string
		failCount     int
		wantAction    RecoveryAction
	}{
		{"first_failure", 0, RecoveryRetry},
		{"second_failure", 1, RecoveryRetry},
		{"third_failure", 2, RecoveryAlternative},
		{"fourth_failure", 3, RecoveryAskUser},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset fail count
			checkpoint.State["failures_step_1"] = tt.failCount

			action := manager.SuggestRecoveryAction(checkpoint, nil)
			if action != tt.wantAction {
				t.Errorf("expected %s, got %s", tt.wantAction, action)
			}
		})
	}
}

// TestResultValidation tests four-level result validation
func TestResultValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	plan := &cognition.ExecutionPlan{Description: "Validation test"}
	checkpoint := manager.StartCheckpoint("validation-test", plan)

	tests := []struct {
		name          string
		result        interface{}
		level         ValidationLevel
		wantPassed    bool
		wantConfidence float64
	}{
		{
			name:   "none_level",
			result: "any result",
			level:  ValidationNone,
			wantPassed: true,
		},
		{
			name:   "basic_clean_result",
			result: "File created successfully",
			level:  ValidationBasic,
			wantPassed: true,
			wantConfidence: 0.8,
		},
		{
			name:   "basic_error_pattern",
			result: "Error: permission denied",
			level:  ValidationBasic,
			wantPassed: false,
			wantConfidence: 0.3,
		},
		{
			name:   "basic_failed_keyword",
			result: "Command failed to execute",
			level:  ValidationBasic,
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager.SetValidationLevel(tt.level)
			result := manager.ValidateResult(checkpoint, "test_tool", tt.result)

			if result.Passed != tt.wantPassed {
				t.Errorf("Passed: expected %v, got %v", tt.wantPassed, result.Passed)
			}

			if tt.level != ValidationNone && result.Confidence != tt.wantConfidence {
				// Allow some tolerance
				diff := result.Confidence - tt.wantConfidence
				if diff < -0.1 || diff > 0.1 {
					t.Errorf("Confidence: expected around %f, got %f", tt.wantConfidence, result.Confidence)
				}
			}
		})
	}
}

// TestProgressReporting tests progress information
func TestProgressReporting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	plan := &cognition.ExecutionPlan{Description: "Progress test"}
	checkpoint := manager.StartCheckpoint("progress-test", plan)
	checkpoint.State["plan_steps"] = 5

	tests := []struct {
		stepID   int
		wantPct  float64
	}{
		{1, 20.0},
		{2, 40.0},
		{3, 60.0},
		{4, 80.0},
		{5, 100.0},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			checkpoint.StepID = tt.stepID
			progress := manager.GetProgress(checkpoint)

			if progress.PercentComplete != tt.wantPct {
				t.Errorf("expected %f%%, got %f%%", tt.wantPct, progress.PercentComplete)
			}

			if progress.CurrentStep != tt.stepID {
				t.Errorf("expected step %d, got %d", tt.stepID, progress.CurrentStep)
			}
		})
	}
}

// TestStoreToolResult tests tool result storage
func TestStoreToolResult(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	plan := &cognition.ExecutionPlan{Description: "Tool result test"}
	checkpoint := manager.StartCheckpoint("tool-result-test", plan)

	// Store various result types
	manager.StoreToolResult(checkpoint, "read_file", "file content")
	manager.StoreToolResult(checkpoint, "execute_command", 42)
	manager.StoreToolResult(checkpoint, "list_files", []string{"a.txt", "b.txt"})

	if len(checkpoint.ToolResults) != 3 {
		t.Errorf("expected 3 tool results, got %d", len(checkpoint.ToolResults))
	}
}

// TestStoreError tests error storage
func TestStoreError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	plan := &cognition.ExecutionPlan{Description: "Error test"}
	checkpoint := manager.StartCheckpoint("error-test", plan)

	testErr := os.ErrPermission
	manager.StoreError(checkpoint, "write_file", testErr)

	if checkpoint.State["last_error"] != testErr.Error() {
		t.Error("last_error should be set")
	}

	if checkpoint.State["failed_tool"] != "write_file" {
		t.Error("failed_tool should be set")
	}
}

// TestListRecentCheckpoints tests listing recent checkpoints
func TestListRecentCheckpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Create multiple checkpoints
	for i := 0; i < 5; i++ {
		plan := &cognition.ExecutionPlan{Description: "Task"}
		cp := manager.StartCheckpoint("", plan)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		_ = cp
	}

	recent := manager.ListRecentCheckpoints(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent checkpoints, got %d", len(recent))
	}
}

// TestDeleteCheckpoint tests checkpoint deletion
func TestDeleteCheckpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	plan := &cognition.ExecutionPlan{Description: "Delete test"}
	checkpoint := manager.StartCheckpoint("delete-test", plan)

	checkpointPath := filepath.Join(tmpDir, "checkpoints", checkpoint.ID+".json")

	// Verify file exists
	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
		t.Fatal("checkpoint file should exist before delete")
	}

	// Delete
	err = manager.DeleteCheckpoint(checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file is gone
	if _, err := os.Stat(checkpointPath); !os.IsNotExist(err) {
		t.Error("checkpoint file should be deleted")
	}
}

// TestCleanupCompleted tests completed checkpoint cleanup
func TestCleanupCompleted(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Create completed checkpoints with old timestamps
	for i := 0; i < 3; i++ {
		plan := &cognition.ExecutionPlan{Description: "Old completed"}
		cp := manager.StartCheckpoint("", plan)
		manager.CompleteCheckpoint(cp)
		cp.UpdatedAt = time.Now().Add(-24 * time.Hour) // 24 hours old
		manager.saveCheckpoint(cp)
	}

	// Create a recent incomplete checkpoint
	plan := &cognition.ExecutionPlan{Description: "Recent incomplete"}
	manager.StartCheckpoint("", plan)

	// Cleanup old completed checkpoints
	deleted := manager.CleanupCompleted(12 * time.Hour)
	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}
}

// TestAutoResume tests auto-resume setting
func TestAutoResume(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	if manager.autoResume != true {
		t.Error("autoResume should be true by default")
	}

	manager.SetAutoResume(false)
	if manager.autoResume != false {
		t.Error("autoResume should be false after SetAutoResume(false)")
	}
}

// TestValidationLevel tests validation level configuration
func TestValidationLevel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	if manager.validationLevel != ValidationBasic {
		t.Error("default validation level should be Basic")
	}

	manager.SetValidationLevel(ValidationStrict)
	if manager.validationLevel != ValidationStrict {
		t.Error("validation level should be Strict")
	}

	manager.SetValidationLevel(ValidationFull)
	if manager.validationLevel != ValidationFull {
		t.Error("validation level should be Full")
	}
}

// TestValidationResultIssues tests that validation captures issues
func TestValidationResultIssues(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	manager.SetValidationLevel(ValidationBasic)

	plan := &cognition.ExecutionPlan{Description: "Issues test"}
	checkpoint := manager.StartCheckpoint("issues-test", plan)

	result := manager.ValidateResult(checkpoint, "test", "Error: connection refused")
	if result.Passed {
		t.Error("should detect error pattern")
	}

	if len(result.Issues) == 0 {
		t.Error("should have captured issue")
	}
}

// TestCheckpointPersistenceAcrossRestart tests checkpoint survives manager recreation
func TestCheckpointPersistenceAcrossRestart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execution-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create manager and checkpoint
	manager1 := NewManager(tmpDir)
	plan := &cognition.ExecutionPlan{Description: "Persistence test"}
	checkpoint := manager1.StartCheckpoint("persist-task", plan)
	manager1.UpdateCheckpoint(checkpoint, 2, "Step 2")

	// Create new manager instance (simulating restart)
	manager2 := NewManager(tmpDir)

	// Find should still work
	found := manager2.FindCheckpoint("Persistence test")
	if found == nil {
		t.Fatal("checkpoint should persist across manager restart")
	}

	if found.StepID != 2 {
		t.Errorf("expected step 2, got %d", found.StepID)
	}
}

// BenchmarkCheckpointSave benchmarks checkpoint save performance
func BenchmarkCheckpointSave(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "execution-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	plan := &cognition.ExecutionPlan{
		Description: "Benchmark task",
		Steps: []cognition.Step{
			{ID: 1, Description: "Step 1"},
			{ID: 2, Description: "Step 2"},
			{ID: 3, Description: "Step 3"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cp := manager.StartCheckpoint("bench-task", plan)
		manager.UpdateCheckpoint(cp, 2, "Step 2")
		manager.StoreToolResult(cp, "tool1", "result")
		manager.CompleteCheckpoint(cp)
	}
}

// BenchmarkCheckpointFind benchmarks checkpoint search performance
func BenchmarkCheckpointFind(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "execution-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Create many checkpoints
	for i := 0; i < 100; i++ {
		plan := &cognition.ExecutionPlan{Description: "Task"}
		manager.StartCheckpoint("", plan)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.FindCheckpoint("Task")
	}
}

// BenchmarkValidation benchmarks validation performance
func BenchmarkValidation(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "execution-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	manager.SetValidationLevel(ValidationBasic)

	plan := &cognition.ExecutionPlan{Description: "Validation bench"}
	checkpoint := manager.StartCheckpoint("validation-bench", plan)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.ValidateResult(checkpoint, "test_tool", "Command executed successfully")
	}
}
