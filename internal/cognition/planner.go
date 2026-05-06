package cognition

import (
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/perception"
)

// Planner handles task decomposition and execution planning
type Planner struct {
	// Can be enhanced with LLM-based planning
}

// NewPlanner creates a new cognition planner
func NewPlanner() *Planner {
	return &Planner{}
}

// CreatePlan creates an execution plan based on perception result
func (p *Planner) CreatePlan(input string, result *perception.PerceptionResult) *Decision {
	decision := &Decision{
		RetrievalHints:  make([]RetrievalHint, 0),
		ContextHints:    make([]string, 0),
		MaxTurns:        10,
		EnableSubAgents: false,
	}

	// Base max turns on complexity
	switch result.Intent.Complexity {
	case perception.ComplexityAdvanced:
		decision.MaxTurns = 25
		decision.EnableSubAgents = true
	case perception.ComplexityMedium:
		decision.MaxTurns = 15
	default:
		decision.MaxTurns = 8
	}

	// If noise detected, may need clarification
	if result.Noise.HasNoise {
		decision.ClarificationNeeded = true
		if len(result.Noise.Suggestions) > 0 {
			decision.ClarificationQuestion = strings.Join(result.Noise.Suggestions, " ")
		}
	}

	// Create execution plan for task intents
	if result.Intent.Type == perception.IntentTask {
		decision.Plan = p.createTaskPlan(input, result)
	}

	// Add retrieval hints based on context hints
	for _, hint := range result.ContextHints {
		decision.ContextHints = append(decision.ContextHints, hint)
		decision.RetrievalHints = append(decision.RetrievalHints, RetrievalHint{
			Type:      "history",
			Query:     hint,
			Relevance: 0.8,
		})
	}

	// Add entity-based retrieval hints
	for _, entity := range result.Intent.Entities {
		decision.RetrievalHints = append(decision.RetrievalHints, RetrievalHint{
			Type:      string(entity.Type),
			Query:     entity.Value,
			Relevance: 0.9,
		})
	}

	return decision
}

// createTaskPlan creates a step-by-step plan for task execution
func (p *Planner) createTaskPlan(input string, result *perception.PerceptionResult) *ExecutionPlan {
	lower := strings.ToLower(input)
	steps := make([]Step, 0)
	stepID := 1

	// Step 1: Analysis and context gathering
	steps = append(steps, Step{
		ID:          stepID,
		Description: "Analyze requirements and gather context",
		Tools:       []string{"search_files", "memory"},
		Priority:    1,
		EstimatedTurns: 1,
		Status:      StepPending,
	})
	stepID++

	// Step 2: Core implementation based on task type
	// File creation/editing tasks
	if strings.Contains(lower, "write") || strings.Contains(lower, "create") ||
		strings.Contains(lower, "generate") || strings.Contains(lower, "build") {
		steps = append(steps, Step{
			ID:          stepID,
			Description: "Create/modify files according to requirements",
			Tools:       []string{"write_file", "file_edit"},
			Priority:    1,
			EstimatedTurns: 2,
			Dependencies: []int{stepID - 1},
			Status:      StepPending,
		})
		stepID++
	}

	// Code execution/testing tasks
	if strings.Contains(lower, "run") || strings.Contains(lower, "execute") ||
		strings.Contains(lower, "test") || strings.Contains(lower, "debug") {
		steps = append(steps, Step{
			ID:          stepID,
			Description: "Execute code and verify results",
			Tools:       []string{"execute_command"},
			Priority:    1,
			EstimatedTurns: 2,
			Dependencies: []int{stepID - 1},
			Status:      StepPending,
		})
		stepID++
	}

	// Analysis tasks
	if strings.Contains(lower, "analyze") || strings.Contains(lower, "process") ||
		strings.Contains(lower, "summarize") || strings.Contains(lower, "parse") {
		steps = append(steps, Step{
			ID:          stepID,
			Description: "Process data and generate analysis",
			Tools:       []string{"execute_command", "read_file"},
			Priority:    1,
			EstimatedTurns: 2,
			Dependencies: []int{stepID - 1},
			Status:      StepPending,
		})
		stepID++
	}

	// Deployment/setup tasks
	if strings.Contains(lower, "deploy") || strings.Contains(lower, "setup") ||
		strings.Contains(lower, "install") || strings.Contains(lower, "configure") {
		steps = append(steps, Step{
			ID:          stepID,
			Description: "Set up environment and deploy",
			Tools:       []string{"execute_command", "write_file"},
			Priority:    2,
			EstimatedTurns: 2,
			Dependencies: []int{stepID - 1},
			Status:      StepPending,
		})
		stepID++
	}

	// Final step: verification and summary
	steps = append(steps, Step{
		ID:          stepID,
		Description: "Verify results and summarize work done",
		Tools:       []string{"read_file", "execute_command"},
		Priority:    1,
		EstimatedTurns: 1,
		Dependencies: []int{stepID - 1},
		Status:      StepPending,
	})

	// Calculate total estimated turns
	totalTurns := 0
	for _, step := range steps {
		totalTurns += step.EstimatedTurns
	}

	return &ExecutionPlan{
		Description:         input,
		OriginalInput:       input,
		Complexity:          result.Intent.Complexity,
		Steps:               steps,
		TotalEstimatedTurns: totalTurns,
		CreatedAt:           time.Now().Unix(),
		ModifiedAt:          time.Now().Unix(),
		IsDynamic:           true,
	}
}

// AdjustPlan adjusts the execution plan based on execution feedback
func (p *Planner) AdjustPlan(plan *ExecutionPlan, state *ExecutionState, reason string) *PlanAdjustment {
	adjustment := &PlanAdjustment{
		Reason:     reason,
		AdjustedAt: time.Now().Unix(),
	}

	// For failed steps, add a retry step
	for _, failedStepID := range state.StepsFailed {
		for _, step := range plan.Steps {
			if step.ID == failedStepID && step.Status == StepFailed {
				// Add retry step
				newStep := Step{
					ID:          len(plan.Steps) + 1,
					Description: "Retry: " + step.Description,
					Tools:       step.Tools,
					Priority:    step.Priority,
					EstimatedTurns: step.EstimatedTurns,
					Dependencies: step.Dependencies,
					Status:      StepPending,
				}
				plan.Steps = append(plan.Steps, newStep)
				adjustment.StepAdded = append(adjustment.StepAdded, newStep)
			}
		}
	}

	plan.ModifiedAt = time.Now().Unix()
	state.Adjustments = append(state.Adjustments, *adjustment)

	return adjustment
}

// GetNextStep returns the next step that should be executed
func (p *Planner) GetNextStep(plan *ExecutionPlan, state *ExecutionState) *Step {
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.Status != StepPending {
			continue
		}

		// Check if all dependencies are completed
		allDepsComplete := true
		for _, depID := range step.Dependencies {
			depComplete := false
			for _, completedID := range state.StepsCompleted {
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

// MarkStepComplete marks a step as completed
func (p *Planner) MarkStepComplete(state *ExecutionState, stepID int) {
	state.StepsCompleted = append(state.StepsCompleted, stepID)
}

// MarkStepFailed marks a step as failed
func (p *Planner) MarkStepFailed(state *ExecutionState, stepID int) {
	state.StepsFailed = append(state.StepsFailed, stepID)
}

