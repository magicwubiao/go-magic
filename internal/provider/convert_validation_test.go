package provider

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestConvertMessages_ValidateToolCallSequence validates that the converted
// messages have correct tool_call / tool message pairing
func TestConvertMessages_ValidateToolCallSequence(t *testing.T) {
	// Simulate a real multi-turn conversation history
	messages := []types.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Search for news"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.ToolCall{
				{ID: "call_abc", Type: "function", Function: types.Function{Name: "web_search", Arguments: `{"query":"news"}`}},
			},
		},
		{Role: "tool", Content: "Found 10 articles...", ToolCallID: "call_abc"},
		{
			Role:    "assistant",
			Content: "Here are the news...",
		},
		{Role: "user", Content: "Thanks"},
		{Role: "assistant", Content: "You're welcome!"},
	}

	result := ConvertMessages(messages)

	// Validate: for every assistant message with tool_calls,
	// the next messages must be tool messages with matching IDs
	for i, msg := range result {
		role := msg["role"].(string)
		if role == "assistant" && msg["tool_calls"] != nil {
			toolCalls := msg["tool_calls"].([]map[string]interface{})
			t.Logf("Assistant message %d has %d tool_calls", i, len(toolCalls))

			// Check next messages are tool messages
			for j, tc := range toolCalls {
				tcID := tc["id"].(string)
				t.Logf("  tool_call[%d]: id=%s", j, tcID)

				expectedToolIdx := i + 1 + j
				if expectedToolIdx >= len(result) {
					t.Errorf("Missing tool message for tool_call id=%s", tcID)
					continue
				}

				toolMsg := result[expectedToolIdx]
				if toolMsg["role"] != "tool" {
					t.Errorf("Expected tool message at %d, got %s", expectedToolIdx, toolMsg["role"])
					continue
				}

				toolCallID := toolMsg["tool_call_id"].(string)
				if toolCallID != tcID {
					t.Errorf("ID mismatch at %d: tool_call=%s, tool_msg=%s", expectedToolIdx, tcID, toolCallID)
				} else {
					t.Logf("  ✓ tool message at %d matches: %s", expectedToolIdx, toolCallID)
				}
			}
		}
	}

	// Also validate by serializing to JSON and checking structure
	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	t.Logf("Converted messages:\n%s", string(jsonBytes))
}

// TestConvertMessages_StrictOpenAIValidation simulates what OpenAI API checks
func TestConvertMessages_StrictOpenAIValidation(t *testing.T) {
	testCases := []struct {
		name     string
		messages []types.Message
		valid    bool
	}{
		{
			name: "normal conversation",
			messages: []types.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			valid: true,
		},
		{
			name: "tool call with matching tool response",
			messages: []types.Message{
				{Role: "user", Content: "search"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []types.ToolCall{
						{ID: "call_1", Type: "function", Function: types.Function{Name: "search", Arguments: `{}`}},
					},
				},
				{Role: "tool", Content: "result", ToolCallID: "call_1"},
				{Role: "assistant", Content: "here is the result"},
			},
			valid: true,
		},
		{
			name: "tool call with missing tool response - sanitized by ConvertMessages",
			messages: []types.Message{
				{Role: "user", Content: "search"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []types.ToolCall{
						{ID: "call_1", Type: "function", Function: types.Function{Name: "search", Arguments: `{}`}},
					},
				},
				// Missing tool message!
				{Role: "user", Content: "where is the result?"},
			},
			valid: true, // sanitizeToolCallSequence removes the incomplete sequence
		},
		{
			name: "multiple tool calls with matching responses",
			messages: []types.Message{
				{Role: "user", Content: "search"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []types.ToolCall{
						{ID: "call_1", Type: "function", Function: types.Function{Name: "search", Arguments: `{"q":"a"}`}},
						{ID: "call_2", Type: "function", Function: types.Function{Name: "search", Arguments: `{"q":"b"}`}},
					},
				},
				{Role: "tool", Content: "result a", ToolCallID: "call_1"},
				{Role: "tool", Content: "result b", ToolCallID: "call_2"},
				{Role: "assistant", Content: "results"},
			},
			valid: true,
		},
		{
			name: "multiple tool calls with ID mismatch",
			messages: []types.Message{
				{Role: "user", Content: "search"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []types.ToolCall{
						{ID: "call_1", Type: "function", Function: types.Function{Name: "search", Arguments: `{}`}},
						{ID: "call_2", Type: "function", Function: types.Function{Name: "search", Arguments: `{}`}},
					},
				},
				{Role: "tool", Content: "result", ToolCallID: "call_WRONG"},
				{Role: "tool", Content: "result", ToolCallID: "call_WRONG"},
			},
			valid: false, // IDs don't match
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ConvertMessages(tc.messages)
			err := validateOpenAIMessageSequence(result)
			if tc.valid && err != nil {
				t.Errorf("Expected valid, got error: %v", err)
			}
			if !tc.valid && err == nil {
				t.Errorf("Expected invalid, but validation passed")
			}
			if err != nil {
				t.Logf("Validation error (expected): %v", err)
			}
		})
	}
}

// validateOpenAIMessageSequence checks if messages conform to OpenAI's requirements
func validateOpenAIMessageSequence(messages []map[string]interface{}) error {
	for i, msg := range messages {
		role := msg["role"].(string)

		if role == "assistant" && msg["tool_calls"] != nil {
			toolCalls := msg["tool_calls"].([]map[string]interface{})
			if len(toolCalls) == 0 {
				continue
			}

			// Check that the next N messages are tool messages
			for j, tc := range toolCalls {
				tcID := tc["id"].(string)
				if tcID == "" {
					return fmt.Errorf("assistant message %d: tool_call[%d] has empty id", i, j)
				}

				expectedIdx := i + 1 + j
				if expectedIdx >= len(messages) {
					return fmt.Errorf("assistant message %d: missing tool message for tool_call id=%s", i, tcID)
				}

				nextMsg := messages[expectedIdx]
				if nextMsg["role"] != "tool" {
					return fmt.Errorf("assistant message %d: expected tool message at %d, got %s", i, expectedIdx, nextMsg["role"])
				}

				toolCallID, ok := nextMsg["tool_call_id"].(string)
				if !ok || toolCallID == "" {
					return fmt.Errorf("assistant message %d: tool message at %d has empty tool_call_id", i, expectedIdx)
				}

				if toolCallID != tcID {
					return fmt.Errorf("assistant message %d: tool_call id=%s does not match tool message tool_call_id=%s at %d", i, tcID, toolCallID, expectedIdx)
				}
			}
		}
	}
	return nil
}
