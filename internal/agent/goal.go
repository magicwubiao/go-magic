package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// GoalState represents the state of a goal
type GoalState string

const (
	GoalActive    GoalState = "active"
	GoalPaused    GoalState = "paused"
	GoalAchieved  GoalState = "achieved"
	GoalExhausted GoalState = "exhausted"
	GoalCleared   GoalState = "cleared"
)

// Goal represents a persistent cross-turn goal
type Goal struct {
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	State       GoalState  `json:"state"`
	TurnCount   int        `json:"turn_count"`
	MaxTurns    int        `json:"max_turns"`
	CreatedAt   time.Time  `json:"created_at"`
	LastJudgeAt  *time.Time `json:"last_judge_at"`
	JudgeResult string     `json:"judge_result"`
}

// GoalManager manages the lifecycle of goals
type GoalManager struct {
	mu       sync.RWMutex
	current  *Goal
	provider provider.Provider
	maxTurns int
	dataDir  string
}

// NewGoalManager creates a new GoalManager
func NewGoalManager(prov provider.Provider, dataDir string) *GoalManager {
	return &GoalManager{
		provider: prov,
		maxTurns: 20,
		dataDir:  dataDir,
	}
}

// SetMaxTurns sets the maximum turns for new goals
func (gm *GoalManager) SetMaxTurns(max int) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if max > 0 {
		gm.maxTurns = max
	}
}

// SetGoal creates a new goal
func (gm *GoalManager) SetGoal(text string) *Goal {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	goal := &Goal{
		ID:        uuid.New().String(),
		Text:      text,
		State:     GoalActive,
		TurnCount: 0,
		MaxTurns:  gm.maxTurns,
		CreatedAt: time.Now(),
	}
	gm.current = goal
	return goal
}

// GetStatus returns the current goal status
func (gm *GoalManager) GetStatus() *Goal {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	if gm.current == nil {
		return nil
	}
	// Return a copy to avoid race conditions
	goalCopy := *gm.current
	return &goalCopy
}

// SetState sets the goal state
func (gm *GoalManager) SetState(state GoalState) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if gm.current != nil {
		gm.current.State = state
	}
}

// Pause pauses the current goal
func (gm *GoalManager) Pause() *Goal {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if gm.current != nil && gm.current.State == GoalActive {
		gm.current.State = GoalPaused
		return gm.current
	}
	return nil
}

// Resume resumes a paused goal
func (gm *GoalManager) Resume() *Goal {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if gm.current != nil && gm.current.State == GoalPaused {
		gm.current.State = GoalActive
		gm.current.TurnCount = 0 // Reset turn counter
		return gm.current
	}
	return nil
}

// Clear clears the current goal
func (gm *GoalManager) Clear() {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	gm.current = nil
}

// IncrementTurn increments the turn count
func (gm *GoalManager) IncrementTurn() {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if gm.current != nil {
		gm.current.TurnCount++
	}
}

// IsExhausted checks if the goal has exhausted its turn budget
func (gm *GoalManager) IsExhausted() bool {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	if gm.current == nil {
		return false
	}
	return gm.current.TurnCount >= gm.current.MaxTurns
}

// JudgeGoal judges if the goal has been achieved based on the last assistant response
func (gm *GoalManager) JudgeGoal(ctx context.Context, lastResponse string) (achieved bool, reason string, err error) {
	gm.mu.RLock()
	goal := gm.current
	gm.mu.RUnlock()

	if goal == nil || goal.State != GoalActive {
		return false, "", nil
	}

	// Build judge prompt
	judgePrompt := fmt.Sprintf(`You are a goal achievement judge. Your job is to determine if a stated goal has been satisfied by the assistant's work.

Goal: %s

Assistant's last response:
%s

Reply with EXACTLY one of:
- ACHIEVED: <brief reason why the goal is satisfied>
- NOT_ACHIEVED: <brief reason why more work is needed>

Be strict: the goal is only ACHIEVED if the assistant has actually completed the task, not just described how to do it.`, goal.Text, lastResponse)

	messages := []provider.Message{
		{Role: "user", Content: judgePrompt},
	}

	resp, err := gm.provider.Chat(ctx, messages)
	if err != nil {
		log.Warnf("[Goal] Judge failed: %v", err)
		return false, "", err
	}

	result := strings.TrimSpace(resp.Content)
	gm.mu.Lock()
	if gm.current != nil {
		gm.current.JudgeResult = result
		now := time.Now()
		gm.current.LastJudgeAt = &now
	}
	gm.mu.Unlock()

	// Parse result
	if strings.HasPrefix(result, "ACHIEVED:") {
		reason = strings.TrimPrefix(result, "ACHIEVED:")
		reason = strings.TrimSpace(reason)
		return true, reason, nil
	}

	// Extract reason from NOT_ACHIEVED
	if strings.HasPrefix(result, "NOT_ACHIEVED:") {
		reason = strings.TrimPrefix(result, "NOT_ACHIEVED:")
		reason = strings.TrimSpace(reason)
		return false, reason, nil
	}

	// Fallback: if the response doesn't match expected format, assume not achieved
	return false, "Could not determine goal status", nil
}

// GetContinuationPrompt generates a continuation prompt
func (gm *GoalManager) GetContinuationPrompt() string {
	gm.mu.RLock()
	goal := gm.current
	gm.mu.RUnlock()

	if goal == nil {
		return ""
	}

	prompt := fmt.Sprintf("Continue working toward the goal: %s. So far %d turns have been used.",
		goal.Text, goal.TurnCount)

	if goal.JudgeResult != "" {
		prompt += fmt.Sprintf(" Last assessment: %s", goal.JudgeResult)
	}

	prompt += " Keep going."
	return prompt
}

// Save persists the goal to disk
func (gm *GoalManager) Save() error {
	gm.mu.RLock()
	goal := gm.current
	sessionID := ""
	gm.mu.RUnlock()

	if goal == nil || sessionID == "" {
		return nil
	}

	// Ensure data directory exists
	if err := os.MkdirAll(gm.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create goals directory: %w", err)
	}

	filePath := filepath.Join(gm.dataDir, sessionID+".json")
	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal goal: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to save goal: %w", err)
	}

	log.Debugf("[Goal] Saved to %s", filePath)
	return nil
}

// SaveWithSessionID persists the goal with a specific session ID
func (gm *GoalManager) SaveWithSessionID(sessionID string) error {
	gm.mu.RLock()
	goal := gm.current
	gm.mu.RUnlock()

	if goal == nil || sessionID == "" {
		return nil
	}

	// Ensure data directory exists
	if err := os.MkdirAll(gm.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create goals directory: %w", err)
	}

	filePath := filepath.Join(gm.dataDir, sessionID+".json")
	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal goal: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to save goal: %w", err)
	}

	log.Debugf("[Goal] Saved to %s", filePath)
	return nil
}

// Load loads a goal from disk
func (gm *GoalManager) Load(sessionID string) error {
	if sessionID == "" {
		return nil
	}

	filePath := filepath.Join(gm.dataDir, sessionID+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read goal file: %w", err)
	}

	var goal Goal
	if err := json.Unmarshal(data, &goal); err != nil {
		return fmt.Errorf("failed to unmarshal goal: %w", err)
	}

	gm.mu.Lock()
	gm.current = &goal
	gm.mu.Unlock()

	log.Infof("[Goal] Loaded goal: %s (state: %s)", goal.Text, goal.State)
	return nil
}

// GetSessionID extracts session ID from the goal's ID field (used as session identifier)
func (gm *GoalManager) GetSessionID() string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	if gm.current != nil {
		return gm.current.ID
	}
	return ""
}
