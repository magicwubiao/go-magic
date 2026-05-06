package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// CohereProvider implements the Cohere API
type CohereProvider struct {
	apiKey   string
	model    string
	baseURL  string
	client   *http.Client
}

// NewCohereProvider creates a new Cohere provider
func NewCohereProvider(apiKey, model string) *CohereProvider {
	if model == "" {
		model = "command-r-plus" // Default model
	}
	return &CohereProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.cohere.ai/v1",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *CohereProvider) Name() string {
	return "cohere"
}

// GetCapabilities returns the capabilities of Cohere
func (p *CohereProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:     true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      false,
		Vision:          false,
	}
}

// cohereMessage represents a message in Cohere format
type cohereMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}

// cohereRequest represents Cohere API request
type cohereRequest struct {
	Model         string            `json:"model"`
	Message       string            `json:"message"`
	ChatHistory   []cohereMessage   `json:"chat_history,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	MaxTokens     *int             `json:"max_tokens,omitempty"`
	Tools         []cohereTool      `json:"tools,omitempty"`
	ToolResults   []cohereToolResult `json:"tool_results,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
}

// cohereTool represents a tool definition for Cohere
type cohereTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	ParameterDefinitions map[string]interface{} `json:"parameter_definitions"`
}

// cohereToolResult represents tool execution result
type cohereToolResult struct {
	ToolOutputs []struct {
		ToolUseID string `json:"tool_use_id"`
		Output    string `json:"output"`
	} `json:"tool_outputs"`
}

// cohereResponse represents Cohere API response
type cohereResponse struct {
	Text       string `json:"text"`
	FinishReason string `json:"finish_reason,omitempty"`
	ToolCalls  []struct {
		ToolUseID string `json:"tool_use_id"`
		Name      string `json:"name"`
		Parameters map[string]interface{} `json:"parameters"`
	} `json:"tool_calls,omitempty"`
	Tokens struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"tokens,omitempty"`
}

// cohereStreamChunk represents streaming response chunk
type cohereStreamChunk struct {
	EventType string `json:"event_type"`
	Text      string `json:"text,omitempty"`
	ToolCalls []struct {
		ToolUseID string `json:"tool_use_id"`
		Name      string `json:"name"`
		Parameters map[string]interface{} `json:"parameters"`
	} `json:"tool_calls,omitempty"`
	GenerationID   string `json:"generation_id,omitempty"`
	FinishReason  string `json:"finish_reason,omitempty"`
}

// Chat implements the Provider interface
func (p *CohereProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, nil, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Cohere-Version", "2024-06-06")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(body, resp.StatusCode)
	}

	return p.parseResponse(body)
}

// ChatWithTools implements the ToolCaller interface
func (p *CohereProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, tools, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Cohere-Version", "2024-06-06")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(body, resp.StatusCode)
	}

	return p.parseResponse(body)
}

// Stream implements the Streamer interface
func (p *CohereProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, nil, true)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Cohere-Version", "2024-06-06")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return p.parseError(body, resp.StatusCode)
	}

	return p.parseStreamResponse(resp.Body, handler)
}

// buildRequest builds the Cohere request from messages
func (p *CohereProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) *cohereRequest {
	req := &cohereRequest{
		Model:  p.model,
		Stream: stream,
	}

	var history []cohereMessage
	var currentMessage string

	for _, msg := range messages {
		if msg.Role == "system" {
			// Cohere handles system prompt differently
			history = append(history, cohereMessage{
				Role:    "SYSTEM",
				Content: msg.Content,
			})
			continue
		}

		if msg.Role == "tool" {
			// Tool results - add to history
			history = append(history, cohereMessage{
				Role:    "tool",
				Content: msg.Content,
			})
			continue
		}

		if msg.Role == "assistant" {
			history = append(history, cohereMessage{
				Role:    "assistant",
				Content: msg.Content,
			})
			continue
		}

		// Last user message becomes the current message
		if msg.Role == "user" {
			currentMessage = msg.Content
		}
	}

	req.ChatHistory = history
	req.Message = currentMessage

	if len(tools) > 0 {
		req.Tools = p.convertTools(tools)
	}

	return req
}

// convertTools converts OpenAI-style tools to Cohere format
func (p *CohereProvider) convertTools(tools []map[string]interface{}) []cohereTool {
	var cohereTools []cohereTool

	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			ct := cohereTool{
				Name:                  getString(fn, "name"),
				Description:           getString(fn, "description"),
				ParameterDefinitions:  make(map[string]interface{}),
			}

			if params, ok := fn["parameters"].(map[string]interface{}); ok {
				if props, ok := params["properties"].(map[string]interface{}); ok {
					for k, v := range props {
						ct.ParameterDefinitions[k] = v
					}
				}
				if req, ok := params["required"].([]interface{}); ok {
					reqList := make([]string, 0)
					for _, r := range req {
						if s, ok := r.(string); ok {
							reqList = append(reqList, s)
						}
					}
					if len(reqList) > 0 {
						ct.ParameterDefinitions["required"] = reqList
					}
				}
			}

			cohereTools = append(cohereTools, ct)
		}
	}

	return cohereTools
}

// parseResponse parses the Cohere response
func (p *CohereProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var resp cohereResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var content string
	var toolCalls []types.ToolCall

	if resp.Text != "" {
		content = resp.Text
	}

	for _, tc := range resp.ToolCalls {
		toolCalls = append(toolCalls, types.ToolCall{
			ID:       tc.ToolUseID,
			Type:     "function",
			Function: types.Function{Name: tc.Name, Arguments: tc.Parameters},
		})
	}

	return &ChatResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: &Usage{
			PromptTokens:     resp.Tokens.InputTokens,
			CompletionTokens: resp.Tokens.OutputTokens,
			TotalTokens:      resp.Tokens.InputTokens + resp.Tokens.OutputTokens,
		},
	}, nil
}

// parseStreamResponse parses streaming response
func (p *CohereProvider) parseStreamResponse(body io.Reader, handler StreamHandler) error {
	scanner := bufio.NewScanner(body)
	var accumulatedContent string

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data:")
		if strings.TrimSpace(data) == "" {
			continue
		}

		var chunk cohereStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		switch chunk.EventType {
		case "text-generation":
			if chunk.Text != "" {
				accumulatedContent += chunk.Text
				handler(&StreamResponse{
					Content: chunk.Text,
					Done:    false,
				})
			}

		case "tool-calls":
			for _, tc := range chunk.ToolCalls {
				handler(&StreamResponse{
					ToolCall: &types.ToolCall{
						ID:   tc.ToolUseID,
						Type: "function",
						Function: types.Function{
							Name:      tc.Name,
							Arguments: tc.Parameters,
						},
					},
					Done: false,
				})
			}

		case "stream-end":
			handler(&StreamResponse{
				Content: "",
				Done:    true,
			})
			return nil
		}
	}

	handler(&StreamResponse{
		Content: accumulatedContent,
		Done:    true,
	})

	return scanner.Err()
}

// parseError parses error responses
func (p *CohereProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		return fmt.Errorf("cohere error [%s]: %s", errResp.Type, errResp.Message)
	}

	return fmt.Errorf("cohere error (%d): %s", statusCode, string(body))
}
