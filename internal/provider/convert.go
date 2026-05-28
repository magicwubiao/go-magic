package provider

import (
	"fmt"

	"github.com/magicwubiao/go-magic/pkg/log"
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
			for j, tc := range msg.ToolCalls {
				// Always use "function" as the type - some providers require this
				toolCallType := tc.Type
				if toolCallType == "" {
					toolCallType = "function"
				}

				// Ensure tool call ID is not empty
				tcID := tc.ID
				if tcID == "" {
					tcID = fmt.Sprintf("call_%d_%d", i, j)
				}

				toolCall := map[string]interface{}{
					"id":   tcID,
					"type": toolCallType,
					"function": map[string]interface{}{
						"name":      tc.GetToolName(),
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

			// Use ToolCallID directly if available (this is the primary source)
			if msg.ToolCallID != "" {
				openAIMsg["tool_call_id"] = msg.ToolCallID
			} else {
				// Fallback: try to find the matching tool_call ID from assistant messages
				toolCallID := findToolCallID(messages, i)
				if toolCallID != "" {
					openAIMsg["tool_call_id"] = toolCallID
				} else {
					// Last resort - generate a synthetic ID
					openAIMsg["tool_call_id"] = fmt.Sprintf("call_unknown_%d", i)
				}
			}
		}

		result = append(result, openAIMsg)
	}

	// Sanitize: remove incomplete tool_call sequences that would cause API errors
	result = sanitizeToolCallSequence(result)

	return result
}

// sanitizeToolCallSequence removes incomplete tool_call sequences from the message list.
// OpenAI requires that every assistant message with tool_calls must be immediately
// followed by tool messages with matching tool_call_ids.
func sanitizeToolCallSequence(messages []map[string]interface{}) []map[string]interface{} {
	cleaned := make([]map[string]interface{}, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		role, _ := msg["role"].(string)

		if role == "assistant" && msg["tool_calls"] != nil {
			toolCalls, ok := msg["tool_calls"].([]map[string]interface{})
			if !ok || len(toolCalls) == 0 {
				cleaned = append(cleaned, msg)
				continue
			}

			// Check if all tool_calls have corresponding tool messages following
			allMatched := true
			for j := range toolCalls {
				expectedIdx := i + 1 + j
				if expectedIdx >= len(messages) {
					allMatched = false
					break
				}
				nextMsg := messages[expectedIdx]
				nextRole, _ := nextMsg["role"].(string)
				if nextRole != "tool" {
					allMatched = false
					break
				}
			}

			if !allMatched {
				// Incomplete sequence - remove the assistant message with tool_calls
				// and any orphaned tool messages that follow
				log.Warnf("[ConvertMessages] Removing incomplete tool_call sequence at message %d (%d tool_calls)", i, len(toolCalls))
				// Skip this assistant message
				i++
				// Skip any tool messages that belong to this sequence
				for i < len(messages) {
					if r, ok := messages[i]["role"].(string); ok && r == "tool" {
						i++
					} else {
						break
					}
				}
				i-- // adjust for loop increment
				continue
			}
		}

		cleaned = append(cleaned, msg)
	}

	return cleaned
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
