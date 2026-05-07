package provider

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// ConvertMessages converts internal Message format to OpenAI API message format
func ConvertMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))
	
	for _, msg := range messages {
		openAIMsg := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		
		// Handle tool calls for assistant messages
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				// Use Function.Name/Arguments (OpenAI format), fallback to Name field
				funcName := tc.Function.Name
				if funcName == "" {
					funcName = tc.Name
				}
				funcArgs := tc.Function.Arguments
				if funcArgs == "" && tc.Arguments != nil {
					// Arguments field is map[string]interface{}, convert to JSON string
					argsBytes, _ := json.Marshal(tc.Arguments)
					funcArgs = string(argsBytes)
				}
				toolCall := map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      funcName,
						"arguments": funcArgs,
					},
				}
				toolCalls = append(toolCalls, toolCall)
			}
			openAIMsg["tool_calls"] = toolCalls
		}
		
		// Handle tool_call_id for tool messages - ALWAYS include it for tool role
		if msg.Role == "tool" {
			if msg.ToolCallID != "" {
				openAIMsg["tool_call_id"] = msg.ToolCallID
			} else {
				// tool_call_id is required by API - search backwards for matching assistant tool_call
				toolCallID := findToolCallID(messages, msg)
				if toolCallID != "" {
					openAIMsg["tool_call_id"] = toolCallID
					fmt.Fprintf(os.Stderr, "[WARN] Missing tool_call_id for tool message, found matching ID: %s\n", toolCallID)
				} else {
					// Last resort - this will likely fail but at least we tried
					fmt.Fprintf(os.Stderr, "[ERROR] Missing tool_call_id for tool message and could not find matching ID\n")
				}
			}
		}
		
		result = append(result, openAIMsg)
	}
	
	return result
}

// findToolCallID searches backwards through messages to find the tool_call_id
// that corresponds to a tool result message
func findToolCallID(messages []types.Message, toolMsg types.Message) string {
	// Search backwards for the last assistant message with tool_calls
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Return the first tool call ID (simple matching)
			return messages[i].ToolCalls[0].ID
		}
	}
	return ""
}
