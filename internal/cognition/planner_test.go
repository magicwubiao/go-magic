package cognition

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/perception"
)

// TestCreatePlan tests task plan creation based on perception results
func TestCreatePlan(t *testing.T) {
	planner := NewPlanner()

	tests := []struct {
		name           string
		input          string
		perception     *perception.PerceptionResult
		wantMaxTurns   int
		wantEnableSubs bool
		wantPlan       bool
	}{
		{
			name:   "simple_task",
			input:  "run ls command",
			perception: &perception.PerceptionResult{
				Intent: perception.IntentClassification{
					Type:       perception.IntentTask,
					Complexity: perception.ComplexitySimple,
				},
				Noise: perception.NoiseDetection{HasNoise: false},
			},
			wantMaxTurns:   8,
			wantEnableSubs: false,
			wantPlan:       true,
		},
		{
			name:   "medium_task",
			input:  "create a Python script to parse CSV",
			perception: &perception.PerceptionResult{
				Intent: perception.IntentClassification{
					Type:       perception.IntentTask,
					Complexity: perception.ComplexityMedium,
				},
				Noise: perception.NoiseDetection{HasNoise: false},
			},
			wantMaxTurns:   15,
			wantEnableSubs: false,
			wantPlan:       true,
		},
		{
			name:   "advanced_task",
			input:  "build a full system and deploy to production",
			perception: &perception.PerceptionResult{
				Intent: perception.IntentClassification{
					Type:       perception.IntentTask,
					Complexity: perception.ComplexityAdvanced,
				},
				Noise: perception.NoiseDetection{HasNoise: false},
			},
			wantMaxTurns:   25,
			wantEnableSubs: true,
			wantPlan:       true,
		},
		{
			name:   "task_with_noise",
			input:  "write a script",
			perception: &perception.PerceptionResult{
				Intent: perception.IntentClassification{
					Type:       perception.IntentTask,
					Complexity: perception.ComplexitySimple,
				},
				Noise: perception.NoiseDetection{
					HasNoise:    true,
					Suggestions: []string{"Please provide more details"},
				},
			},
			wantMaxTurns:   8,
			wantEnableSubs: false,
			wantPlan:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := planner.CreatePlan(tt.input, tt.perception)

			if decision.MaxTurns != tt.wantMaxTurns {
				t.Errorf("MaxTurns: expected %d, got %d", tt.wantMaxTurns, decision.MaxTurns)
			}

			if decision.EnableSubAgents != tt.wantEnableSubs {
				t.Errorf("EnableSubAgents: expected %v, got %v", tt.wantEnableSubs, decision.EnableSubAgents)
			}

			if tt.wantPlan && decision.Plan == nil {
				t.Error("expected plan to be created, got nil")
			}

			if tt.perception.Noise.HasNoise && !decision.ClarificationNeeded {
				t.Error("expected ClarificationNeeded to be true when noise detected")
			}
		})
	}
}

// TestDynamicPlanning tests dynamic plan adjustment
func TestDynamicPlanning(t *testing.T) {
	planner := NewPlanner()

	// Create a plan with multiple steps
	plan := &ExecutionPlan{
		Description: "Test task",
		Steps: []Step{
			{ID: 1, Description: "Step 1", Status: StepComplete},
			{ID: 2, Description: "Step 2", Status: StepPending},
			{ID: 3, Description: "Step 3", Status: StepPending},
		},
	}

	state := &ExecutionState{
		CurrentStepID:  2,
		StepsCompleted: []int{1},
		StepsFailed:    []int{},
	}

	// Adjust plan due to failure
	state.StepsFailed = append(state.StepsFailed, 2)
	adjustment := planner.AdjustPlan(plan, state, "Step 2 failed")

	if len(adjustment.StepAdded) == 0 {
		t.Error("expected retry step to be added")
	}

	if len(plan.Steps) != 4 {
		t.Errorf("expected 4 steps after adjustment, got %d", len(plan.Steps))
	}

	// Test GetNextStep - should return step 3 (dependencies met)
	nextStep := planner.GetNextStep(plan, state)
	if nextStep == nil {
		t.Fatal("expected next step, got nil")
	}
	if nextStep.ID != 3 {
		t.Errorf("expected step 3, got %d", nextStep.ID)
	}
}

// TestSubAgentDecision tests sub-agent enablement logic
func TestSubAgentDecision(t *testing.T) {
	planner := NewPlanner()

	tests := []struct {
		name           string
		complexity     perception.TaskComplexity
		wantSubAgents  bool
	}{
		{"simple", perception.ComplexitySimple, false},
		{"medium", perception.ComplexityMedium, false},
		{"advanced", perception.ComplexityAdvanced, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perception := &perception.PerceptionResult{
				Intent: perception.IntentClassification{
					Type:       perception.IntentTask,
					Complexity: tt.complexity,
				},
			}

			decision := planner.CreatePlan("test task", perception)
			if decision.EnableSubAgents != tt.wantSubAgents {
				t.Errorf("EnableSubAgents: expected %v, got %v", tt.wantSubAgents, decision.EnableSubAgents)
			}
		})
	}
}

// TestMemoryRetrievalHints tests retrieval hint generation
func TestMemoryRetrievalHints(t *testing.T) {
	planner := NewPlanner()

	perception := &perception.PerceptionResult{
		Intent: perception.IntentClassification{
			Type: perception.IntentTask,
			Entities: []perception.Entity{
				{Type: "language", Value: "python"},
				{Type: "file", Value: "data.csv"},
			},
		},
		ContextHints: []string{"previous Python work", "CSV processing"},
	}

	decision := planner.CreatePlan("analyze the Python data", perception)

	// Check entity-based hints
	if len(decision.RetrievalHints) < 2 {
		t.Errorf("expected at least 2 retrieval hints, got %d", len(decision.RetrievalHints))
	}

	// Check context hints
	if len(decision.ContextHints) < 2 {
		t.Errorf("expected at least 2 context hints, got %d", len(decision.ContextHints))
	}

	// Check hint relevance scores
	for _, hint := range decision.RetrievalHints {
		if hint.Relevance <= 0 || hint.Relevance > 1 {
			t.Errorf("invalid relevance score: %f", hint.Relevance)
		}
	}
}

// TestPlanCreationWithDifferentTasks tests plan creation for various task types
func TestPlanCreationWithDifferentTasks(t *testing.T) {
	planner := NewPlanner()

	tests := []struct {
		name           string
		input          string
		wantSteps      int
		wantTotalTurns int
	}{
		{
			name:           "file_creation",
			input:          "write a new Python script",
			wantSteps:      4, // analysis + create + verify + summary
			wantTotalTurns: 5, // 1 + 2 + 1 + 1
		},
		{
			name:           "execution_task",
			input:          "run the tests",
			wantSteps:      4,
			wantTotalTurns: 5,
		},
		{
			name:           "analysis_task",
			input:          "analyze the logs",
			wantSteps:      4,
			wantTotalTurns: 5,
		},
		{
			name:           "deployment_task",
			input:          "deploy to production",
			wantSteps:      5, // includes setup step
			wantTotalTurns: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perception := &perception.PerceptionResult{
				Intent: perception.IntentClassification{
					Type:       perception.IntentTask,
					Complexity: perception.ComplexityMedium,
				},
			}

			decision := planner.CreatePlan(tt.input, perception)

			if decision.Plan == nil {
				t.Fatal("expected plan, got nil")
			}

			if len(decision.Plan.Steps) < tt.wantSteps {
				t.Errorf("expected at least %d steps, got %d", tt.wantSteps, len(decision.Plan.Steps))
			}

			if decision.Plan.TotalEstimatedTurns < tt.wantTotalTurns {
				t.Errorf("expected at least %d total turns, got %d", tt.wantTotalTurns, decision.Plan.TotalEstimatedTurns)
			}
		})
	}
}

// TestStepStatusManagement tests step status transitions
func TestStepStatusManagement(t *testing.T) {
	planner := NewPlanner()
	state := &ExecutionState{
		StepsCompleted: []int{},
		StepsFailed:    []int{},
	}

	// Mark steps
	planner.MarkStepComplete(state, 1)
	if len(state.StepsCompleted) != 1 || state.StepsCompleted[0] != 1 {
		t.Error("Step 1 should be marked complete")
	}

	planner.MarkStepComplete(state, 2)
	if len(state.StepsCompleted) != 2 {
		t.Error("Step 2 should be marked complete")
	}

	planner.MarkStepFailed(state, 3)
	if len(state.StepsFailed) != 1 || state.StepsFailed[0] != 3 {
		t.Error("Step 3 should be marked failed")
	}
}

// TestDependencyChecking tests step dependency validation
func TestDependencyChecking(t *testing.T) {
	planner := NewPlanner()

	plan := &ExecutionPlan{
		Steps: []Step{
			{ID: 1, Description: "Step 1", Dependencies: []int{}, Status: StepComplete},
			{ID: 2, Description: "Step 2", Dependencies: []int{1}, Status: StepComplete},
			{ID: 3, Description: "Step 3", Dependencies: []int{1, 2}, Status: StepPending},
			{ID: 4, Description: "Step 4", Dependencies: []int{3}, Status: StepPending},
		},
	}

	state := &ExecutionState{
		StepsCompleted: []int{1, 2},
		StepsFailed:    []int{},
	}

	// Step 3 should be available (dependencies 1 and 2 complete)
	nextStep := planner.GetNextStep(plan, state)
	if nextStep == nil || nextStep.ID != 3 {
		t.Errorf("expected step 3, got %v", nextStep)
	}

	// Mark step 3 complete
	planner.MarkStepComplete(state, 3)

	// Step 4 should now be available
	nextStep = planner.GetNextStep(plan, state)
	if nextStep == nil || nextStep.ID != 4 {
		t.Errorf("expected step 4, got %v", nextStep)
	}
}

// TestClarificationNeeded tests when clarification is required
func TestClarificationNeeded(t *testing.T) {
	planner := NewPlanner()

	tests := []struct {
		name    string
		noise   perception.NoiseDetection
		wantCla bool
	}{
		{
			name:    "no_noise",
			noise:   perception.NoiseDetection{HasNoise: false},
			wantCla: false,
		},
		{
			name: "has_noise_no_suggestion",
			noise: perception.NoiseDetection{
				HasNoise:    true,
				Suggestions: []string{},
			},
			wantCla: true,
		},
		{
			name: "has_noise_with_suggestion",
			noise: perception.NoiseDetection{
				HasNoise:    true,
				Suggestions: []string{"Please clarify"},
			},
			wantCla: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perception := &perception.PerceptionResult{
				Intent: perception.IntentClassification{Type: perception.IntentTask},
				Noise:  tt.noise,
			}

			decision := planner.CreatePlan("test", perception)
			if decision.ClarificationNeeded != tt.wantCla {
				t.Errorf("ClarificationNeeded: expected %v, got %v", tt.wantCla, decision.ClarificationNeeded)
			}
		})
	}
}

// TestPlanMetadata tests plan metadata fields
func TestPlanMetadata(t *testing.T) {
	planner := NewPlanner()

	perception := &perception.PerceptionResult{
		Intent: perception.IntentClassification{
			Type:       perception.IntentTask,
			Complexity: perception.ComplexityMedium,
		},
	}

	decision := planner.CreatePlan("test task description", perception)

	if decision.Plan == nil {
		t.Fatal("expected plan")
	}

	// Check metadata
	if decision.Plan.Description != "test task description" {
		t.Errorf("Description mismatch")
	}
	if decision.Plan.OriginalInput != "test task description" {
		t.Errorf("OriginalInput mismatch")
	}
	if decision.Plan.Complexity != perception.ComplexityMedium {
		t.Errorf("Complexity mismatch")
	}
	if decision.Plan.CreatedAt == 0 {
		t.Error("CreatedAt should be set")
	}
	if decision.Plan.ModifiedAt == 0 {
		t.Error("ModifiedAt should be set")
	}
	if !decision.Plan.IsDynamic {
		t.Error("IsDynamic should be true")
	}
}

// BenchmarkCreatePlan benchmarks plan creation
func BenchmarkCreatePlan(b *testing.B) {
	planner := NewPlanner()
	perception := &perception.PerceptionResult{
		Intent: perception.IntentClassification{
			Type:       perception.IntentTask,
			Complexity: perception.ComplexityMedium,
			Entities: []perception.Entity{
				{Type: "language", Value: "python"},
				{Type: "file", Value: "data.csv"},
			},
		},
		ContextHints: []string{"previous work", "data processing"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		planner.CreatePlan("analyze the Python CSV data", perception)
	}
}

// BenchmarkAdjustPlan benchmarks plan adjustment
func BenchmarkAdjustPlan(b *testing.B) {
	planner := NewPlanner()
	plan := &ExecutionPlan{
		Description: "Test task",
		Steps: []Step{
			{ID: 1, Description: "Step 1", Status: StepComplete},
			{ID: 2, Description: "Step 2", Status: StepFailed},
			{ID: 3, Description: "Step 3", Status: StepPending},
		},
	}
	state := &ExecutionState{
		StepsCompleted: []int{1},
		StepsFailed:    []int{2},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a fresh plan for each iteration
		p := *plan
		s := *state
		planner.AdjustPlan(&p, &s, "test failure")
	}
}

// BenchmarkGetNextStep benchmarks next step retrieval
func BenchmarkGetNextStep(b *testing.B) {
	planner := NewPlanner()
	plan := &ExecutionPlan{
		Steps: []Step{
			{ID: 1, Description: "Step 1", Status: StepComplete},
			{ID: 2, Description: "Step 2", Status: StepPending, Dependencies: []int{1}},
			{ID: 3, Description: "Step 3", Status: StepPending, Dependencies: []int{2}},
			{ID: 4, Description: "Step 4", Status: StepPending, Dependencies: []int{3}},
		},
	}
	state := &ExecutionState{
		StepsCompleted: []int{1, 2, 3},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		planner.GetNextStep(plan, state)
	}
}
