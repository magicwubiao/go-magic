package provider

import (
	"fmt"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// ConvertMessages converts internal Message type to OpenAI-compatible format
func ConvertMessages(messages []types.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for i, msg := range messages {
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
				// Always use "function" as the type - some providers require this
				toolCallType := tc.Type
				if toolCallType == "" {
					toolCallType = "function"
				}

				toolCall := map[string]interface{}{
					"id":   tc.ID,
					"type": toolCallType,
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

			// Find the matching tool_call ID from the most recent assistant message with tool_calls
			toolCallID := findToolCallID(messages, i)
			if toolCallID != "" {
				openAIMsg["tool_call_id"] = toolCallID
			} else if msg.ToolCallID != "" {
				openAIMsg["tool_call_id"] = msg.ToolCallID
			} else {
				// Last resort - generate a synthetic ID
				openAIMsg["tool_call_id"] = fmt.Sprintf("call_unknown_%d", i)
			}
		}

		result = append(result, openAIMsg)
	}

	return result
}

// findToolCallID searches backwards to find the matching tool_call ID
func findToolCallID(messages []types.Message, currentIdx int) string {
	// Count only the consecutive tool messages immediately before this one
	consecutiveToolCount := 0
	for i := currentIdx - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			consecutiveToolCount++
		} else if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			// Found the assistant message that initiated these tool calls
			tcIdx := consecutiveToolCount
			if tcIdx < len(messages[i].ToolCalls) {
				return messages[i].ToolCalls[tcIdx].ID
			}
			return ""
		} else {
			// Not a tool or assistant-with-tool-calls message, stop searching
			break
		}
	}
	return ""
}
