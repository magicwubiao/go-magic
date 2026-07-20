package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SubGoal represents an additional success criterion layered onto an active goal
type SubGoal struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// AddSubGoal adds a new sub-goal to the current active goal
// Returns the sub-goal text that should be appended to the goal prompt
func (gm *GoalManager) AddSubGoal(text string) (*SubGoal, error) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.current == nil {
		return nil, fmt.Errorf("no active goal to add sub-goal to")
	}
	if gm.current.State != GoalActive {
		return nil, fmt.Errorf("current goal is not active (state: %s)", gm.current.State)
	}

	subGoal := &SubGoal{
		ID:        uuid.New().String(),
		Text:      text,
		CreatedAt: time.Now(),
	}

	gm.subGoals = append(gm.subGoals, subGoal)
	return subGoal, nil
}

// GetSubGoals returns all sub-goals for the current goal
func (gm *GoalManager) GetSubGoals() []SubGoal {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	if gm.subGoals == nil {
		return nil
	}
	result := make([]SubGoal, len(gm.subGoals))
	for i, sg := range gm.subGoals {
		result[i] = *sg
	}
	return result
}

// BuildSubGoalPrompt builds the prompt text including all sub-goals
// This is appended to the judge prompt so the LLM considers sub-goals
func (gm *GoalManager) BuildSubGoalPrompt() string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	if len(gm.subGoals) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\nAdditional success criteria (sub-goals):\n")
	for i, sg := range gm.subGoals {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, sg.Text))
	}
	sb.WriteString("The goal is achieved ONLY when ALL criteria (including sub-goals) are met.\n")

	return sb.String()
}

// ClearSubGoals removes all sub-goals
func (gm *GoalManager) ClearSubGoals() {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	gm.subGoals = nil
}

// RemoveSubGoal removes a sub-goal by ID
func (gm *GoalManager) RemoveSubGoal(id string) bool {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	for i, sg := range gm.subGoals {
		if sg.ID == id {
			gm.subGoals = append(gm.subGoals[:i], gm.subGoals[i+1:]...)
			return true
		}
	}
	return false
}
