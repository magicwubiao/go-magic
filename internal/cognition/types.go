package cognition

import "github.com/magicwubiao/go-magic/internal/perception"

// Step represents a single step in the execution plan
type Step struct {
	ID          int      `json:"id"`
	Description string   `json:"description"`
	Tools       []string `json:"tools,omitempty"` // Tools likely needed for this step
	Priority    int      `json:"priority"`         // 1 = highest
	EstimatedTurns int   `json:"estimated_turns"`
	Dependencies []int   `json:"dependencies,omitempty"` // Step IDs that must complete first
	Status      StepStatus `json:"status"`
}

// StepStatus represents the status of a plan step
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepComplete  StepStatus = "complete"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// ExecutionPlan represents the complete plan for a task
type ExecutionPlan struct {
	TaskID       string                 `json:"task_id"`
	Description  string                 `json:"description"`
	OriginalInput string                `json:"original_input"`
	Complexity   perception.TaskComplexity `json:"complexity"`
	Steps        []Step                 `json:"steps"`
	TotalEstimatedTurns int             `json:"total_estimated_turns"`
	CreatedAt    int64                  `json:"created_at"`
	ModifiedAt   int64                  `json:"modified_at"`
	IsDynamic    bool                   `json:"is_dynamic"` // Whether plan can change
}

// PlanAdjustment represents a change to the execution plan
type PlanAdjustment struct {
	Reason     string   `json:"reason"`
	StepAdded  []Step   `json:"steps_added,omitempty"`
	StepRemoved []int   `json:"steps_removed,omitempty"`
	StepModified []int  `json:"steps_modified,omitempty"`
	AdjustedAt int64    `json:"adjusted_at"`
}

// RetrievalHint represents a hint for memory retrieval
type RetrievalHint struct {
	Type        string `json:"type"` // "memory", "file", "history"
	Query       string `json:"query"`
	Relevance   float64 `json:"relevance"` // 0.0 - 1.0
}

// Decision represents a decision made by the cognition layer
type Decision struct {
	Plan              *ExecutionPlan      `json:"plan"`
	RetrievalHints    []RetrievalHint     `json:"retrieval_hints"`
	ContextHints      []string            `json:"context_hints"`
	MaxTurns          int                 `json:"max_turns"`
	EnableSubAgents   bool                `json:"enable_sub_agents"`
	ToolFilter        []string            `json:"tool_filter,omitempty"` // If set, only these tools allowed
	ClarificationNeeded bool              `json:"clarification_needed"`
	ClarificationQuestion string           `json:"clarification_question,omitempty"`
}

// ExecutionState tracks the current state of plan execution
type ExecutionState struct {
	CurrentStepID  int                `json:"current_step_id"`
	StepsCompleted []int              `json:"steps_completed"`
	StepsFailed    []int              `json:"steps_failed"`
	TurnsUsed      int                `json:"turns_used"`
	Adjustments    []PlanAdjustment   `json:"adjustments"`
}
