package execution

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/cognition"
)

// Manager handles execution checkpointing, resumption, and validation
// This is Layer 3 of the Cortex three-layer architecture:
// Perception → Cognition → Execution
type Manager struct {
	baseDir          string
	checkpoints      map[string]*Checkpoint
	results          map[string][]*ExecutionResult
	validationLevel  ValidationLevel
	autoResume       bool
}

// NewManager creates a new execution manager
func NewManager(baseDir string) *Manager {
	checkpointDir := filepath.Join(baseDir, "checkpoints")
	os.MkdirAll(checkpointDir, 0755)

	return &Manager{
		baseDir:         checkpointDir,
		checkpoints:     make(map[string]*Checkpoint),
		results:         make(map[string][]*ExecutionResult),
		validationLevel: ValidationBasic,
		autoResume:      true,
	}
}

// StartCheckpoint begins a new checkpoint for a task
func (m *Manager) StartCheckpoint(taskID string, plan *cognition.ExecutionPlan) *Checkpoint {
	if taskID == "" {
		taskID = m.generateTaskID(plan.Description)
	}

	checkpoint := &Checkpoint{
		ID:          taskID + "-" + time.Now().Format("20060102-150405"),
		TaskID:      taskID,
		StepID:      1,
		StepName:    "Initializing",
		TurnCount:   0,
		State:       make(map[string]interface{}),
		ToolResults: make(map[string]interface{}),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		IsFinal:     false,
	}

	// Store plan info in state
	checkpoint.State["plan_steps"] = len(plan.Steps)
	checkpoint.State["plan_description"] = plan.Description

	m.checkpoints[checkpoint.ID] = checkpoint
	m.saveCheckpoint(checkpoint)

	return checkpoint
}

// UpdateCheckpoint updates the current checkpoint state
func (m *Manager) UpdateCheckpoint(checkpoint *Checkpoint, stepID int, stepName string) {
	checkpoint.StepID = stepID
	checkpoint.StepName = stepName
	checkpoint.UpdatedAt = time.Now()
	m.saveCheckpoint(checkpoint)
}

// UpdateTurnCount updates the turn count in the checkpoint
func (m *Manager) UpdateTurnCount(checkpoint *Checkpoint, turns int) {
	checkpoint.TurnCount = turns
	checkpoint.UpdatedAt = time.Now()
	m.saveCheckpoint(checkpoint)
}

// StoreToolResult stores a tool execution result
func (m *Manager) StoreToolResult(checkpoint *Checkpoint, toolName string, result interface{}) {
	checkpoint.ToolResults[toolName] = result
	checkpoint.UpdatedAt = time.Now()
	m.saveCheckpoint(checkpoint)

	// Also store in results history
	resultRecord := &ExecutionResult{
		ToolName:  toolName,
		Success:   true,
		Data:      result,
		Timestamp: time.Now(),
	}
	m.results[checkpoint.TaskID] = append(m.results[checkpoint.TaskID], resultRecord)
}

// StoreError stores an execution error
func (m *Manager) StoreError(checkpoint *Checkpoint, toolName string, err error) {
	checkpoint.State["last_error"] = err.Error()
	checkpoint.State["failed_tool"] = toolName
	checkpoint.UpdatedAt = time.Now()
	m.saveCheckpoint(checkpoint)

	resultRecord := &ExecutionResult{
		ToolName:  toolName,
		Success:   false,
		Error:     err.Error(),
		Timestamp: time.Now(),
	}
	m.results[checkpoint.TaskID] = append(m.results[checkpoint.TaskID], resultRecord)
}

// CompleteCheckpoint marks a checkpoint as successfully completed
func (m *Manager) CompleteCheckpoint(checkpoint *Checkpoint) {
	checkpoint.IsFinal = true
	checkpoint.State["completed_at"] = time.Now().Format(time.RFC3339)
	checkpoint.UpdatedAt = time.Now()
	m.saveCheckpoint(checkpoint)
}

// FindCheckpoint finds a checkpoint to resume from
// Returns nil if no resumable checkpoint exists
func (m *Manager) FindCheckpoint(taskDescription string) *Checkpoint {
	// Generate potential task IDs
	taskID := m.generateTaskID(taskDescription)

	// Look for exact task match first
	checkpoints := m.listCheckpoints()
	for _, cp := range checkpoints {
		if cp.TaskID == taskID && !cp.IsFinal {
			return cp
		}
	}

	// Look for similar tasks (fuzzy match could be added here)
	for _, cp := range checkpoints {
		if !cp.IsFinal {
			if desc, ok := cp.State["plan_description"].(string); ok {
				if strings.Contains(desc, taskDescription) || strings.Contains(taskDescription, desc) {
					return cp
				}
			}
		}
	}

	return nil
}

// GetProgress returns human-readable progress information
func (m *Manager) GetProgress(checkpoint *Checkpoint) *Progress {
	totalSteps, _ := checkpoint.State["plan_steps"].(int)
	if totalSteps == 0 {
		totalSteps = 5 // Default estimate
	}

	percent := float64(checkpoint.StepID) / float64(totalSteps) * 100
	if percent > 100 {
		percent = 100
	}

	return &Progress{
		TaskID:          checkpoint.TaskID,
		CurrentStep:     checkpoint.StepID,
		TotalSteps:      totalSteps,
		PercentComplete: percent,
		CurrentAction:   checkpoint.StepName,
		Message:         fmt.Sprintf("Step %d/%d: %s", checkpoint.StepID, totalSteps, checkpoint.StepName),
	}
}

// ValidateResult validates an execution result against expectations
func (m *Manager) ValidateResult(checkpoint *Checkpoint, toolName string, result interface{}) *ValidationResult {
	if m.validationLevel == ValidationNone {
		return &ValidationResult{Passed: true, Confidence: 1.0}
	}

	validation := &ValidationResult{
		Passed:     true,
		Confidence: 0.8,
	}

	// Basic validation: check for common error patterns
	if str, ok := result.(string); ok {
		errorPatterns := []string{
			"error", "Error", "ERROR",
			"failed", "Failed", "FAILED",
			"permission denied", "not found", "no such",
			"connection refused", "timeout",
		}
		for _, pattern := range errorPatterns {
			if strings.Contains(str, pattern) {
				validation.Passed = false
				validation.Issues = append(validation.Issues, "Detected error pattern: "+pattern)
				validation.Confidence = 0.3
			}
		}
	}

	// Strict validation: could add LLM-based assessment here
	if m.validationLevel == ValidationStrict || m.validationLevel == ValidationFull {
		// This would call the LLM to assess if the result matches expectations
		// For now, just mark that more detailed validation is possible
	}

	return validation
}

// SuggestRecoveryAction suggests what to do after a failure
func (m *Manager) SuggestRecoveryAction(checkpoint *Checkpoint, err error) RecoveryAction {
	// Count failures for this step
	failKey := fmt.Sprintf("failures_step_%d", checkpoint.StepID)
	failCount := 0
	if count, ok := checkpoint.State[failKey].(int); ok {
		failCount = count
	}
	checkpoint.State[failKey] = failCount + 1

	// Strategy: retry 2 times, then try alternative, then ask user
	switch failCount {
	case 0, 1:
		return RecoveryRetry
	case 2:
		return RecoveryAlternative
	default:
		return RecoveryAskUser
	}
}

// ListRecentCheckpoints returns recently active checkpoints
func (m *Manager) ListRecentCheckpoints(limit int) []*Checkpoint {
	checkpoints := m.listCheckpoints()

	// Sort by updated time (newest first)
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].UpdatedAt.After(checkpoints[j].UpdatedAt)
	})

	if len(checkpoints) > limit {
		checkpoints = checkpoints[:limit]
	}

	return checkpoints
}

// DeleteCheckpoint removes a checkpoint
func (m *Manager) DeleteCheckpoint(checkpointID string) error {
	delete(m.checkpoints, checkpointID)
	return os.Remove(filepath.Join(m.baseDir, checkpointID+".json"))
}

// CleanupCompleted removes completed checkpoints older than the threshold
func (m *Manager) CleanupCompleted(olderThan time.Duration) int {
	deleted := 0
	cutoff := time.Now().Add(-olderThan)

	for _, cp := range m.listCheckpoints() {
		if cp.IsFinal && cp.UpdatedAt.Before(cutoff) {
			m.DeleteCheckpoint(cp.ID)
			deleted++
		}
	}

	return deleted
}

// Internal: save checkpoint to disk
func (m *Manager) saveCheckpoint(checkpoint *Checkpoint) error {
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.baseDir, checkpoint.ID+".json"), data, 0644)
}

// Internal: list all checkpoints from disk
func (m *Manager) listCheckpoints() []*Checkpoint {
	var result []*Checkpoint

	files, _ := filepath.Glob(filepath.Join(m.baseDir, "*.json"))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err == nil {
			result = append(result, &cp)
		}
	}

	return result
}

// Internal: generate stable task ID from description
func (m *Manager) generateTaskID(description string) string {
	hash := md5.Sum([]byte(description))
	return fmt.Sprintf("task-%x", hash[:6])
}

// SetValidationLevel sets the validation level
func (m *Manager) SetValidationLevel(level ValidationLevel) {
	m.validationLevel = level
}

// SetAutoResume enables or disables auto-resume
func (m *Manager) SetAutoResume(auto bool) {
	m.autoResume = auto
}
