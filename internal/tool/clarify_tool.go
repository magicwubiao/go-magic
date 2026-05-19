package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// ClarifyTool asks the user for clarification with native button support on messaging platforms
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
	return "Ask the user for clarification when a request is ambiguous or missing information. On Telegram and Discord, options are shown as native interactive buttons. On CLI, options are shown as numbered choices."
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
				"type":        "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional list of options for the user to choose from. These will be rendered as native buttons on Telegram/Discord.",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional context about why clarification is needed",
			},
			"multi_select": map[string]interface{}{
				"type":        "boolean",
				"description": "Allow the user to select multiple options (default: false)",
			},
			"header": map[string]interface{}{
				"type":        "string",
				"description": "Short label for the button group (max 12 chars, e.g., 'Auth method', 'Format')",
			},
		},
		"required": []string{"question"},
	}
}

// ClarifyResult is the structured result returned by the clarify tool
type ClarifyResult struct {
	Status      string   `json:"status"`       // "clarification_needed"
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	Context     string   `json:"context,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
	Header      string   `json:"header,omitempty"`
	// Platform-specific rendering hints
	RenderAsButtons bool `json:"render_as_buttons"` // always true when options are provided
}

// Execute asks the user for clarification
func (t *ClarifyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return nil, fmt.Errorf("question is required")
	}

	result := &ClarifyResult{
		Status:          "clarification_needed",
		Question:        question,
		RenderAsButtons: false,
	}

	if options, ok := args["options"].([]interface{}); ok && len(options) > 0 {
		opts := make([]string, len(options))
		for i, opt := range options {
			opts[i] = fmt.Sprintf("%v", opt)
		}
		result.Options = opts
		result.RenderAsButtons = true // Enable native button rendering on supported platforms
	}

	if context, ok := args["context"].(string); ok && context != "" {
		result.Context = context
	}

	if multiSelect, ok := args["multi_select"].(bool); ok {
		result.MultiSelect = multiSelect
	}

	if header, ok := args["header"].(string); ok {
		result.Header = header
	}

	return result, nil
}

// SerializeForGateway serializes the clarify result for gateway platforms
// This is used by Telegram/Discord handlers to render native buttons
func SerializeForGateway(result *ClarifyResult) ([]byte, error) {
	return json.Marshal(result)
}

// BuildTelegramKeyboard builds an InlineKeyboardMarkup for Telegram
func BuildTelegramKeyboard(result *ClarifyResult) string {
	if !result.RenderAsButtons || len(result.Options) == 0 {
		return ""
	}

	// Return structured data that the Telegram handler can parse
	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{},
	}

	var rows [][]map[string]interface{}
	row := make([]map[string]interface{}, 0)
	for i, opt := range result.Options {
		btn := map[string]interface{}{
			"text":          opt,
			"callback_data": fmt.Sprintf("clarify:%d", i),
		}
		row = append(row, btn)
		// Max 3 buttons per row
		if len(row) == 3 || i == len(result.Options)-1 {
			rows = append(rows, row)
			row = make([]map[string]interface{}, 0)
		}
	}
	keyboard["inline_keyboard"] = rows

	data, _ := json.Marshal(keyboard)
	return string(data)
}

// BuildDiscordComponents builds ActionRow components for Discord
func BuildDiscordComponents(result *ClarifyResult) string {
	if !result.RenderAsButtons || len(result.Options) == 0 {
		return ""
	}

	components := []map[string]interface{}{
		{
			"type":       1, // ACTION_ROW
			"components": []map[string]interface{}{},
		},
	}

	buttons := make([]map[string]interface{}, 0)
	for i, opt := range result.Options {
		customID := fmt.Sprintf("clarify_%d", i)
		if len(customID) > 100 {
			customID = fmt.Sprintf("cl_%d", i)
		}
		buttons = append(buttons, map[string]interface{}{
			"type":        2, // BUTTON
			"label":       opt,
			"custom_id":   customID,
			"style":       1, // PRIMARY
		})
	}
	components[0]["components"] = buttons

	data, _ := json.Marshal(components)
	return string(data)
}
