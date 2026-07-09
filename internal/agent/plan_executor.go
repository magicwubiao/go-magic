package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// PlanExecutor manages plan-guided execution of agent tasks
type PlanExecutor struct {
	mu sync.RWMutex

	provider    provider.Provider
	plan        *cognition.ExecutionPlan
	state       *cognition.ExecutionState
	currentStep *cognition.Step
	llmPlanner  *cognition.LLMPlanner

	// Configuration
	useLLMPlanning bool
	autoReplan     bool
	maxReplans     int
	replanCount    int

	// Callbacks
	onStepComplete func(step *cognition.Step)
	onPlanChange   func(plan *cognition.ExecutionPlan)
}

// PlanExecutorConfig configures the plan executor
type PlanExecutorConfig struct {
	UseLLMPlanning bool
	AutoReplan     bool
	MaxReplans     int
}

// DefaultPlanExecutorConfig returns default config
func DefaultPlanExecutorConfig() PlanExecutorConfig {
	return PlanExecutorConfig{
		UseLLMPlanning: true,
		AutoReplan:     true,
		MaxReplans:     3,
	}
}

// NewPlanExecutor creates a new plan executor
func NewPlanExecutor(prov provider.Provider, cfg PlanExecutorConfig) *PlanExecutor {
	pe := &PlanExecutor{
		provider:       prov,
		useLLMPlanning: cfg.UseLLMPlanning,
		autoReplan:     cfg.AutoReplan,
		maxReplans:     cfg.MaxReplans,
		state: &cognition.ExecutionState{
			StepsCompleted: []int{},
			StepsFailed:    []int{},
			Adjustments:    []cognition.PlanAdjustment{},
		},
	}

	if cfg.UseLLMPlanning && prov != nil {
		pe.llmPlanner = cognition.NewLLMPlanner(prov)
	}

	return pe
}

// CreatePlan creates an execution plan for a task
func (pe *PlanExecutor) CreatePlan(ctx context.Context, task string) (*cognition.ExecutionPlan, error) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	var plan *cognition.ExecutionPlan

	if pe.llmPlanner != nil {
		decision, err := pe.llmPlanner.CreatePlan(ctx, task)
		if err != nil {
			log.Warnf("[PlanExecutor] LLM planning failed, falling back to rule-based: %v", err)
			plan = pe.createRuleBasedPlan(task)
		} else {
			plan = decision.Plan
		}
	} else {
		plan = pe.createRuleBasedPlan(task)
	}

	pe.plan = plan
	pe.state = &cognition.ExecutionState{
		StepsCompleted: []int{},
		StepsFailed:    []int{},
		Adjustments:    []cognition.PlanAdjustment{},
	}
	pe.replanCount = 0

	log.Infof("[PlanExecutor] Plan created: %d steps, estimated %d turns",
		len(plan.Steps), plan.TotalEstimatedTurns)

	return plan, nil
}

// createRuleBasedPlan creates a simple rule-based plan
func (pe *PlanExecutor) createRuleBasedPlan(task string) *cognition.ExecutionPlan {
	planner := cognition.NewPlanner()

	// Use a default perception result for rule-based planning
	decision := planner.CreatePlan(task, nil)
	return decision.Plan
}

// GetCurrentStep returns the step that should be executed next
func (pe *PlanExecutor) GetCurrentStep() *cognition.Step {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if pe.plan == nil {
		return nil
	}

	for i := range pe.plan.Steps {
		step := &pe.plan.Steps[i]
		if step.Status != cognition.StepPending {
			continue
		}

		allDepsComplete := true
		for _, depID := range step.Dependencies {
			depComplete := false
			for _, completedID := range pe.state.StepsCompleted {
				if depID == completedID {
					depComplete = true
					break
				}
			}
			if !depComplete {
				allDepsComplete = false
				break
			}
		}

		if allDepsComplete {
			return step
		}
	}

	return nil
}

// GetPlanPrompt returns a prompt fragment describing the current plan state
func (pe *PlanExecutor) GetPlanPrompt() string {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if pe.plan == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n=== Execution Plan ===\n")
	sb.WriteString(fmt.Sprintf("Task: %s\n", pe.plan.Description))
	sb.WriteString(fmt.Sprintf("Total steps: %d | Completed: %d | Failed: %d\n\n",
		len(pe.plan.Steps), len(pe.state.StepsCompleted), len(pe.state.StepsFailed)))

	for _, step := range pe.plan.Steps {
		var icon string
		switch step.Status {
		case cognition.StepComplete:
			icon = "✅"
		case cognition.StepFailed:
			icon = "❌"
		case cognition.StepRunning:
			icon = "🔄"
		default:
			icon = "⏳"
		}

		sb.WriteString(fmt.Sprintf("%s Step %d: %s", icon, step.ID, step.Description))
		if len(step.Tools) > 0 {
			sb.WriteString(fmt.Sprintf(" [tools: %s]", strings.Join(step.Tools, ", ")))
		}
		sb.WriteString("\n")
	}

	currentStep := pe.GetCurrentStep()
	if currentStep != nil {
		sb.WriteString(fmt.Sprintf("\n📍 Current focus: Step %d - %s\n",
			currentStep.ID, currentStep.Description))
		if len(currentStep.Tools) > 0 {
			sb.WriteString(fmt.Sprintf("   Recommended tools: %s\n",
				strings.Join(currentStep.Tools, ", ")))
		}
	}

	sb.WriteString("\nFollow the plan. Complete steps in order.\n")

	return sb.String()
}

// MarkStepComplete marks a step as completed
func (pe *PlanExecutor) MarkStepComplete(stepID int) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if pe.plan == nil {
		return
	}

	for i := range pe.plan.Steps {
		if pe.plan.Steps[i].ID == stepID {
			pe.plan.Steps[i].Status = cognition.StepComplete
			break
		}
	}

	pe.state.StepsCompleted = append(pe.state.StepsCompleted, stepID)

	log.Infof("[PlanExecutor] Step %d completed (%d/%d done)",
		stepID, len(pe.state.StepsCompleted), len(pe.plan.Steps))

	if pe.onStepComplete != nil {
		for i := range pe.plan.Steps {
			if pe.plan.Steps[i].ID == stepID {
				pe.onStepComplete(&pe.plan.Steps[i])
				break
			}
		}
	}
}

// MarkStepFailed marks a step as failed
func (pe *PlanExecutor) MarkStepFailed(stepID int, reason string) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if pe.plan == nil {
		return
	}

	for i := range pe.plan.Steps {
		if pe.plan.Steps[i].ID == stepID {
			pe.plan.Steps[i].Status = cognition.StepFailed
			break
		}
	}

	pe.state.StepsFailed = append(pe.state.StepsFailed, stepID)

	log.Warnf("[PlanExecutor] Step %d failed: %s", stepID, reason)
}

// IsPlanComplete returns whether all steps are completed
func (pe *PlanExecutor) IsPlanComplete() bool {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if pe.plan == nil {
		return false
	}

	return len(pe.state.StepsCompleted) >= len(pe.plan.Steps)
}

// GetProgress returns the plan progress (0.0 - 1.0)
func (pe *PlanExecutor) GetProgress() float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if pe.plan == nil || len(pe.plan.Steps) == 0 {
		return 0.0
	}

	return float64(len(pe.state.StepsCompleted)) / float64(len(pe.plan.Steps))
}

// ShouldReplan checks if the plan needs to be adjusted
func (pe *PlanExecutor) ShouldReplan(failStreak int) bool {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if !pe.autoReplan {
		return false
	}
	if pe.replanCount >= pe.maxReplans {
		return false
	}
	if len(pe.state.StepsFailed) > 0 && failStreak >= 2 {
		return true
	}
	return false
}

// Replan adjusts the plan based on execution feedback
func (pe *PlanExecutor) Replan(ctx context.Context, feedback string) (*cognition.PlanAdjustment, error) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if pe.plan == nil {
		return nil, fmt.Errorf("no plan to adjust")
	}

	pe.replanCount++

	if pe.llmPlanner != nil {
		adjustment, err := pe.llmPlanner.AdjustPlanWithFeedback(ctx, pe.plan, feedback)
		if err != nil {
			log.Warnf("[PlanExecutor] LLM replan failed: %v", err)
			return pe.replanRuleBased(feedback), nil
		}
		return adjustment, nil
	}

	return pe.replanRuleBased(feedback), nil
}

func (pe *PlanExecutor) replanRuleBased(feedback string) *cognition.PlanAdjustment {
	planner := cognition.NewPlanner()
	adjustment := planner.AdjustPlan(pe.plan, pe.state, feedback)

	if pe.onPlanChange != nil {
		pe.onPlanChange(pe.plan)
	}

	return adjustment
}

// GetPlan returns the current plan
func (pe *PlanExecutor) GetPlan() *cognition.ExecutionPlan {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.plan
}

// GetState returns the current execution state
func (pe *PlanExecutor) GetState() *cognition.ExecutionState {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.state
}

// DetectStepCompletion tries to detect if the current step is complete
// based on the conversation history and tool results
func (pe *PlanExecutor) DetectStepCompletion(history []provider.Message, toolResults map[string]interface{}) bool {
	currentStep := pe.GetCurrentStep()
	if currentStep == nil {
		return false
	}

	// Simple heuristic: if we've had successful tool calls and the LLM
	// has indicated progress, consider step complete
	successfulTools := 0
	for _, msg := range history {
		if msg.Role == "tool" && !strings.HasPrefix(msg.Content, "Error:") {
			successfulTools++
		}
	}

	// If we've made enough tool calls with success, assume step done
	return successfulTools >= currentStep.EstimatedTurns
}

// GenerateStepSummary generates a summary of a completed step
func (pe *PlanExecutor) GenerateStepSummary(ctx context.Context, stepID int, history []provider.Message) string {
	pe.mu.RLock()
	step := pe.findStep(stepID)
	pe.mu.RUnlock()

	if step == nil {
		return ""
	}

	if pe.provider == nil {
		return fmt.Sprintf("Step %d completed: %s", stepID, step.Description)
	}

	// Use LLM to generate a concise summary
	recentMsgs := history
	if len(recentMsgs) > 10 {
		recentMsgs = recentMsgs[len(recentMsgs)-10:]
	}

	var contextBuilder strings.Builder
	for _, msg := range recentMsgs {
		content := msg.Content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		contextBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, content))
	}

	prompt := fmt.Sprintf(`Summarize what was accomplished in step %d: "%s"

Context from execution:
%s

Provide a 1-2 sentence summary of what was accomplished.`, stepID, step.Description, contextBuilder.String())

	resp, err := pe.provider.Chat(ctx, []provider.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return fmt.Sprintf("Step %d completed: %s", stepID, step.Description)
	}

	return strings.TrimSpace(resp.Content)
}

func (pe *PlanExecutor) findStep(stepID int) *cognition.Step {
	if pe.plan == nil {
		return nil
	}
	for i := range pe.plan.Steps {
		if pe.plan.Steps[i].ID == stepID {
			return &pe.plan.Steps[i]
		}
	}
	return nil
}

// GetPlanAsJSON returns the plan in JSON format for storage/transport
func (pe *PlanExecutor) GetPlanAsJSON() (string, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if pe.plan == nil {
		return "", fmt.Errorf("no plan")
	}

	data, err := json.Marshal(pe.plan)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// StepsCompletedCount returns the number of completed steps
func (pe *PlanExecutor) StepsCompletedCount() int {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return len(pe.state.StepsCompleted)
}

// TotalSteps returns the total number of steps
func (pe *PlanExecutor) TotalSteps() int {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if pe.plan == nil {
		return 0
	}
	return len(pe.plan.Steps)
}
