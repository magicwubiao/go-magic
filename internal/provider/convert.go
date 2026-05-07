package provider

import (
	"encoding/json"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// ConvertMessages converts internal Message format to OpenAI API message format
func ConvertMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))
	
	for msgIdx, msg := range messages {
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
				// Ensure funcArgs is at least "{}" for API compatibility
				if funcArgs == "" {
					funcArgs = "{}"
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
				toolCallID := findToolCallID(messages, msgIdx)
				if toolCallID != "" {
					openAIMsg["tool_call_id"] = toolCallID
				} else {
					// Last resort - generate a synthetic ID
					syntheticID := "call_unknown"
					openAIMsg["tool_call_id"] = syntheticID
				}
			}
		}
		
		result = append(result, openAIMsg)
	}
	
	return result
}

// findToolCallID searches backwards through messages to find the tool_call_id
// that corresponds to a tool result message at the given index
func findToolCallID(messages []types.Message, toolMsgIdx int) string {
	// Count how many tool messages come before this index
	toolCount := 0
	for i := 0; i < toolMsgIdx; i++ {
		if messages[i].Role == "tool" {
			toolCount++
		}
	}
	
	// Search backwards for the last assistant message with tool_calls before this tool message
	for i := toolMsgIdx - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Use the tool message count to find the matching tool call
			tcIdx := toolCount
			if tcIdx < len(messages[i].ToolCalls) && messages[i].ToolCalls[tcIdx].ID != "" {
				return messages[i].ToolCalls[tcIdx].ID
			}
			// Fallback: return first non-empty ID
			for _, tc := range messages[i].ToolCalls {
				if tc.ID != "" {
					return tc.ID
				}
			}
			return ""
		}
	}
	return ""
}
