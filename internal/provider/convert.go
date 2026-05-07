package provider

import (
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
				toolCall := map[string]interface{}{
					"id": tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				}
				toolCalls = append(toolCalls, toolCall)
			}
			openAIMsg["tool_calls"] = toolCalls
		}
		
		// Handle tool_call_id for tool messages
		if msg.ToolCallID != "" {
			openAIMsg["tool_call_id"] = msg.ToolCallID
		}
		
		result = append(result, openAIMsg)
	}
	
	return result
}
