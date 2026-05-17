package provider

import (
	"github.com/magicwubiao/go-magic/pkg/types"
)

// ConvertMessages converts internal Message type to OpenAI-compatible format
func ConvertMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for msgIdx, msg := range messages {
		openAIMsg := make(map[string]interface{})

		// Always set role
		openAIMsg["role"] = msg.Role

		// Handle content based on type
		if len(msg.ContentParts) > 0 {
			// Multi-modal content (text + images)
			parts := make([]map[string]interface{}, 0, len(msg.ContentParts))
			for _, part := range msg.ContentParts {
				if part.Type == "text" {
					parts = append(parts, map[string]interface{}{
						"type": "text",
						"text": part.Text,
					})
				} else if part.Type == "image_url" {
					parts = append(parts, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": part.ImageURL.URL,
						},
					})
				}
			}
			openAIMsg["content"] = parts
		} else {
			// Fallback to plain text content
			openAIMsg["content"] = msg.Content
		}

		// Handle tool calls for assistant messages
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				toolCall := map[string]interface{}{
					"id":   tc.ID,
					"type": tc.Type,
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				}
				toolCalls = append(toolCalls, toolCall)
			}
			openAIMsg["tool_calls"] = toolCalls
		}

		// Handle tool_call_id for tool messages - ALWAYS include it for tool role
		if msg.Role == "tool" {
			// OpenAI requires tool messages to have content (null is ok, but not missing)
			// If content is empty, set it to null
			if msg.Content == "" {
				openAIMsg["content"] = nil
			}
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
	// Count only the consecutive tool messages immediately before this one
	// (i.e., since the last assistant message with tool_calls)
	consecutiveToolCount := 0
	for i := toolMsgIdx - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			consecutiveToolCount++
		} else if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Found the assistant message that initiated these tool calls
			tcIdx := consecutiveToolCount
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
		} else {
			// Not a tool or assistant-with-tool-calls message, stop searching
			break
		}
	}
	return ""
}
