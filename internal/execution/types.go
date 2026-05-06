package execution

import "time"

// Checkpoint represents a saved execution state
// This is the core of the resumable execution pattern
type Checkpoint struct {
	ID            string                 `json:"id"`             // Unique checkpoint ID
	TaskID        string                 `json:"task_id"`        // Task this checkpoint belongs to
	StepID        int                    `json:"step_id"`        // Current step being executed
	StepName      string                 `json:"step_name"`      // Human-readable step name
	TurnCount     int                    `json:"turn_count"`     // Turns used so far
	State         map[string]interface{} `json:"state"`          // Arbitrary state data
	ToolResults   map[string]interface{} `json:"tool_results"`   // Results from tools
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	IsFinal       bool                   `json:"is_final"`       // True if task completed
}

// ExecutionResult represents the result of a single tool execution
type ExecutionResult struct {
	ToolName    string      `json:"tool_name"`
	Success     bool        `json:"success"`
	Data        interface{} `json:"data,omitempty"`
	Error       string      `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
	Timestamp   time.Time   `json:"timestamp"`
}

// Progress represents task progress information for the user
type Progress struct {
	TaskID          string  `json:"task_id"`
	CurrentStep     int     `json:"current_step"`
	TotalSteps      int     `json:"total_steps"`
	PercentComplete float64 `json:"percent_complete"`
	CurrentAction   string  `json:"current_action"`
	Message         string  `json:"message,omitempty"`
}

// ValidationLevel defines how strictly to validate execution results
type ValidationLevel string

const (
	ValidationNone     ValidationLevel = "none"
	ValidationBasic    ValidationLevel = "basic"     // Check for obvious errors
	ValidationStrict   ValidationLevel = "strict"    // Verify expected outcomes
	ValidationFull     ValidationLevel = "full"      // LLM-based quality assessment
)

// ValidationResult contains the outcome of result validation
type ValidationResult struct {
	Passed      bool     `json:"passed"`
	Issues      []string `json:"issues,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
	Confidence  float64  `json:"confidence"` // 0.0 - 1.0
}

// ResumeStrategy defines how to resume from a checkpoint
type ResumeStrategy string

const (
	ResumeAuto       ResumeStrategy = "auto"       // Resume automatically
	ResumeAskUser    ResumeStrategy = "ask_user"   // Ask user before resuming
	ResumeRestart    ResumeStrategy = "restart"    // Start over from beginning
)

// RecoveryAction represents possible actions when execution fails
type RecoveryAction string

const (
	RecoveryRetry       RecoveryAction = "retry"        // Retry the same step
	RecoveryAlternative RecoveryAction = "alternative"  // Try a different approach
	RecoveryAskUser     RecoveryAction = "ask_user"     // Ask user what to do
	RecoveryAbort       RecoveryAction = "abort"        // Give up
)
