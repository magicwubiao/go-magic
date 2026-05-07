package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// ToolConverter converts tool definitions between different provider formats
type ToolConverter struct{}

// NewToolConverter creates a new tool converter
func NewToolConverter() *ToolConverter {
	return &ToolConverter{}
}

// ConvertToOpenAI converts tools to OpenAI function format
func (tc *ToolConverter) ConvertToOpenAI(tools []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tools))
	
	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			result = append(result, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        getString(fn, "name"),
					"description": getString(fn, "description"),
					"parameters":  fn["parameters"],
				},
			})
		} else {
			// Assume it's already in OpenAI format
			result = append(result, tool)
		}
	}
	
	return result
}

// ConvertToAnthropic converts tools to Anthropic tool use format
func (tc *ToolConverter) ConvertToAnthropic(tools []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tools))
	
	for _, tool := range tools {
		var name, description string
		var parameters map[string]interface{}
		
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			name = getString(fn, "name")
			description = getString(fn, "description")
			if p, ok := fn["parameters"].(map[string]interface{}); ok {
				parameters = p
			}
		} else {
			name = getString(tool, "name")
			description = getString(tool, "description")
			if p, ok := tool["parameters"].(map[string]interface{}); ok {
				parameters = p
			}
		}
		
		if parameters == nil {
			parameters = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		
		result = append(result, map[string]interface{}{
			"name":        name,
			"description": description,
			"input_schema": parameters,
		})
	}
	
	return result
}

// ConvertToGemini converts tools to Gemini function declaration format
func (tc *ToolConverter) ConvertToGemini(tools []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tools))
	
	for _, tool := range tools {
		var name, description string
		var parameters map[string]interface{}
		
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			name = getString(fn, "name")
			description = getString(fn, "description")
			if p, ok := fn["parameters"].(map[string]interface{}); ok {
				parameters = p
			}
		} else {
			name = getString(tool, "name")
			description = getString(tool, "description")
			if p, ok := tool["parameters"].(map[string]interface{}); ok {
				parameters = p
			}
		}
		
		// Convert to Gemini schema format
		schema := tc.convertToGeminiSchema(parameters)
		
		result = append(result, map[string]interface{}{
			"functionDeclarations": []map[string]interface{}{
				{
					"name":        name,
					"description": description,
					"parameters":  schema,
				},
			},
		})
	}
	
	return result
}

// convertToGeminiSchema converts parameters to Gemini schema format
func (tc *ToolConverter) convertToGeminiSchema(params interface{}) map[string]interface{} {
	if params == nil {
		return map[string]interface{}{
			"type": "object",
		}
	}
	
	if m, ok := params.(map[string]interface{}); ok {
		result := map[string]interface{}{}
		
		// Copy type
		if t, ok := m["type"].(string); ok {
			result["type"] = t
		}
		
		// Convert properties
		if props, ok := m["properties"].(map[string]interface{}); ok {
			result["properties"] = props
		}
		
		// Copy required
		if req, ok := m["required"].([]interface{}); ok {
			result["required"] = req
		}
		
		return result
	}
	
	return map[string]interface{}{"type": "object"}
}

// ParseToolCall parses a tool call from various provider formats
func (tc *ToolConverter) ParseToolCall(data interface{}) (*types.ToolCall, error) {
	// Try OpenAI format first
	if m, ok := data.(map[string]interface{}); ok {
		// OpenAI format with function sub-object
		if fn, ok := m["function"].(map[string]interface{}); ok {
			var args map[string]interface{}
			if argsStr, ok := fn["arguments"].(string); ok {
				json.Unmarshal([]byte(argsStr), &args)
			} else if argsMap, ok := fn["arguments"].(map[string]interface{}); ok {
				args = argsMap
			}
			
			return &types.ToolCall{
				ID:        getString(m, "id"),
				Type:      "function",
				Function: types.Function{
					Name:      getString(fn, "name"),
					Arguments: marshalJSON(args),
				},
			}, nil
		}
		
		// Simple format
		var args map[string]interface{}
		if argsStr, ok := m["arguments"].(string); ok {
			json.Unmarshal([]byte(argsStr), &args)
		} else if argsMap, ok := m["arguments"].(map[string]interface{}); ok {
			args = argsMap
		}
		
		return &types.ToolCall{
			ID:        getString(m, "id"),
			Type:      "function",
			Function: types.Function{
				Name:      getString(m, "name"),
				Arguments: marshalJSON(args),
			},
		}, nil
	}
	
	return nil, fmt.Errorf("cannot parse tool call from %T", data)
}

// ParseToolCalls parses multiple tool calls
func (tc *ToolConverter) ParseToolCalls(data interface{}) []types.ToolCall {
	var result []types.ToolCall
	
	switch v := data.(type) {
	case []interface{}:
		for _, item := range v {
			if tc, err := tc.ParseToolCall(item); err == nil {
				result = append(result, *tc)
			}
		}
	case []map[string]interface{}:
		for _, item := range v {
			if tc, err := tc.ParseToolCall(item); err == nil {
				result = append(result, *tc)
			}
		}
	}
	
	return result
}

// NormalizeToolCall normalizes a tool call to internal format
func (tc *ToolConverter) NormalizeToolCall(toolCall *types.ToolCall) error {
	// Ensure ID is set
	if toolCall.ID == "" {
		toolCall.ID = fmt.Sprintf("call_%d", time.Now().UnixNano())
	}
	
	// Ensure type is set
	if toolCall.Type == "" {
		toolCall.Type = "function"
	}
	
	// Parse arguments if they're a string
	if toolCall.Function.Arguments != "" && toolCall.Function.Arguments[0] == '{' {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
			toolCall.Arguments = args
		}
	}
	
	return nil
}

// ConvertToolResult converts a tool execution result for different providers
func (tc *ToolConverter) ConvertToolResult(provider string, result interface{}, toolCallID string) (interface{}, error) {
	// Parse result to JSON string
	resultStr, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	
	switch strings.ToLower(provider) {
	case "anthropic":
		return map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": toolCallID,
			"content":     resultStr,
		}, nil
	
	case "openai", "openrouter", "groq", "deepseek", "kimi":
		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": toolCallID,
			"content":      resultStr,
		}, nil
	
	case "gemini":
		return map[string]interface{}{
			"functionResponse": map[string]interface{}{
				"name": toolCallID,
				"response": map[string]interface{}{
					"content": resultStr,
				},
			},
		}, nil
	
	default:
		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": toolCallID,
			"content":      resultStr,
		}, nil
	}
}

// ProviderToolFormat specifies the expected tool format for a provider
type ProviderToolFormat struct {
	Format     string // "openai", "anthropic", "gemini"
	UsesTools  bool   // Uses tools array in request
	UsesChoice bool   // Uses choice.tool_call format
}

// GetProviderToolFormat returns the expected tool format for a provider
func GetProviderToolFormat(provider string) ProviderToolFormat {
	switch strings.ToLower(provider) {
	case "anthropic":
		return ProviderToolFormat{
			Format:     "anthropic",
			UsesTools:  true,
			UsesChoice: false,
		}
	case "gemini":
		return ProviderToolFormat{
			Format:     "gemini",
			UsesTools:  true,
			UsesChoice: false,
		}
	default:
		return ProviderToolFormat{
			Format:     "openai",
			UsesTools:  true,
			UsesChoice: true,
		}
	}
}

// Helper functions

func marshalJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func marshalResult(v interface{}) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
