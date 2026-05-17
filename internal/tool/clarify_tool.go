package tool

import (
	"context"
	"fmt"
)

// ClarifyTool asks the user for clarification
type ClarifyTool struct{}

// NewClarifyTool creates a new clarify tool
func NewClarifyTool() *ClarifyTool {
	return &ClarifyTool{}
}

// Name returns the tool name
func (t *ClarifyTool) Name() string {
	return "clarify"
}

// Description returns the tool description
func (t *ClarifyTool) Description() string {
	return "Ask the user for clarification when a request is ambiguous or missing information. Use this instead of guessing."
}

// Parameters returns the tool parameters schema
func (t *ClarifyTool) Schema() map[string]interface{} { return t.Parameters() }

func (t *ClarifyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "The clarification question to ask the user",
			},
			"options": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional list of options for the user to choose from",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional context about why clarification is needed",
			},
		},
		"required": []string{"question"},
	}
}

// Execute asks the user for clarification
func (t *ClarifyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return nil, fmt.Errorf("question is required")
	}

	result := map[string]interface{}{
		"status":   "clarification_needed",
		"question": question,
	}

	if options, ok := args["options"].([]interface{}); ok && len(options) > 0 {
		opts := make([]string, len(options))
		for i, opt := range options {
			opts[i] = fmt.Sprintf("%v", opt)
		}
		result["options"] = opts
	}

	if context, ok := args["context"].(string); ok && context != "" {
		result["context"] = context
	}

	return result, nil
}
