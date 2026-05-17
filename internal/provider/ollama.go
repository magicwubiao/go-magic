package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/magicwubiao/go-magic/pkg/types"
)

type OllamaProvider struct {
	baseURL string
	model   string
}

func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		baseURL: baseURL,
		model:   model,
	}
}

func (p *OllamaProvider) Name() string {
	return "ollama"
}

func (p *OllamaProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	// Convert messages to Ollama format
	type OllamaMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	ollamaMessages := make([]OllamaMessage, 0)
	for _, msg := range messages {
		if msg.Role == "system" {
			// Ollama handles system messages differently
			continue
		}
		ollamaMessages = append(ollamaMessages, OllamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Build request
	type OllamaRequest struct {
		Model    string          `json:"model"`
		Messages []OllamaMessage `json:"messages"`
		Stream   bool            `json:"stream"`
	}

	reqBody := OllamaRequest{
		Model:    p.model,
		Messages: ollamaMessages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/chat", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(bytes.NewReader(jsonData))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	type OllamaResponse struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}

	var ollamaResp OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &ChatResponse{
		Content: ollamaResp.Message.Content,
	}, nil
}

// ChatWithTools implements the ToolCaller interface for Ollama
func (p *OllamaProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	// Convert messages to Ollama format
	type OllamaMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	ollamaMessages := make([]OllamaMessage, 0)
	for _, msg := range messages {
		if msg.Role == "system" {
			// Ollama handles system messages differently
			continue
		}
		ollamaMessages = append(ollamaMessages, OllamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Convert tools to Ollama format
	type OllamaTool struct {
		Type     string `json:"type"`
		Function struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		} `json:"function"`
	}

	ollamaTools := make([]OllamaTool, 0)
	for _, t := range tools {
		if funcData, ok := t["function"].(map[string]interface{}); ok {
			ollamaTools = append(ollamaTools, OllamaTool{
				Type: "function",
				Function: struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description"`
					Parameters  map[string]interface{} `json:"parameters"`
				}{
					Name:        getStringFromMap(funcData, "name"),
					Description: getStringFromMap(funcData, "description"),
					Parameters:  getMapFromMap(funcData, "parameters"),
				},
			})
		}
	}

	// Build request
	type OllamaRequest struct {
		Model    string          `json:"model"`
		Messages []OllamaMessage `json:"messages"`
		Tools    []OllamaTool    `json:"tools"`
		Stream   bool            `json:"stream"`
	}

	reqBody := OllamaRequest{
		Model:    p.model,
		Messages: ollamaMessages,
		Tools:    ollamaTools,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/chat", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(bytes.NewReader(jsonData))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Ollama response format with tool_calls
	type OllamaToolCall struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}

	type OllamaResponse struct {
		Message struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	}

	var ollamaResp OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	response := &ChatResponse{
		Content: ollamaResp.Message.Content,
	}

	// Parse tool calls
	for i, tc := range ollamaResp.Message.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		response.ToolCalls = append(response.ToolCalls, types.ToolCall{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return response, nil
}

// Helper functions for Ollama tools conversion
func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMapFromMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}
