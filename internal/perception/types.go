package perception

// IntentType represents the type of user intent
type IntentType string

const (
	// IntentTask is for task-oriented requests ("Write a Python script")
	IntentTask IntentType = "task"

	// IntentQuestion is for information requests
	IntentQuestion IntentType = "question"

	// IntentClarification is when user clarifies previous message
	IntentClarification IntentType = "clarification"

	// IntentCorrection is when user corrects the assistant
	IntentCorrection IntentType = "correction"

	// IntentFeedback is feedback, positive or negative
	IntentFeedback IntentType = "feedback"

	// IntentChitChat is casual conversation
	IntentChitChat IntentType = "chitchat"

	// IntentUnknown is when intent cannot be classified
	IntentUnknown IntentType = "unknown"
)

// TaskComplexity indicates how complex a task is
type TaskComplexity string

const (
	ComplexitySimple   TaskComplexity = "simple"   // Single tool call
	ComplexityMedium   TaskComplexity = "medium"   // 2-5 tool calls
	ComplexityAdvanced TaskComplexity = "advanced" // 5+ tool calls, may need sub-agents
)

// Entity represents extracted named entities from user input
type Entity struct {
	Type  string `json:"type"`  // e.g., "language", "file", "tool"
	Value string `json:"value"` // e.g., "Python", "data.csv"
}

// IntentClassification represents the result of intent parsing
type IntentClassification struct {
	Type        IntentType     `json:"type"`
	Confidence  float64        `json:"confidence"` // 0.0 - 1.0
	Complexity  TaskComplexity `json:"complexity,omitempty"`
	Entities    []Entity       `json:"entities,omitempty"`
	Keywords    []string       `json:"keywords,omitempty"`
	Description string         `json:"description,omitempty"`
}

// NoiseType represents types of input noise
type NoiseType string

const (
	NoiseTypo        NoiseType = "typo"
	NoiseIncomplete  NoiseType = "incomplete"
	NoiseAmbiguous   NoiseType = "ambiguous"
	NoiseContradicts NoiseType = "contradicts"
	NoiseIrrelevant  NoiseType = "irrelevant"
)

// NoiseDetection represents detected noise in user input
type NoiseDetection struct {
	HasNoise    bool        `json:"has_noise"`
	NoiseTypes  []NoiseType `json:"noise_types,omitempty"`
	Suggestions []string    `json:"suggestions,omitempty"`
}

// PerceptionResult is the complete output of the perception layer
type PerceptionResult struct {
	Input        string              `json:"input"`
	Intent       IntentClassification `json:"intent"`
	Noise        NoiseDetection      `json:"noise"`
	ContextHints []string            `json:"context_hints,omitempty"`
	Priority     int                 `json:"priority"` // 1 (highest) - 5 (lowest)
}
