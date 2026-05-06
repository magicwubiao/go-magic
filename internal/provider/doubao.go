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

// DoubaoProvider implements the Doubao (Volcengine) API
type DoubaoProvider struct {
	apiKey      string
	model       string
	baseURL     string
	client      *http.Client
	endpointID  string
}

// NewDoubaoProvider creates a new Doubao provider
func NewDoubaoProvider(apiKey, model string) *DoubaoProvider {
	if model == "" {
		model = "doubao-pro-32k" // Default model
	}
	return &DoubaoProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://ark.cn-beijing.volces.com/api/v3",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// NewDoubaoProviderWithEndpoint creates a Doubao provider with custom endpoint
func NewDoubaoProviderWithEndpoint(apiKey, endpointID string) *DoubaoProvider {
	return &DoubaoProvider{
		apiKey:     apiKey,
		endpointID: endpointID,
		baseURL:    "https://ark.cn-beijing.volces.com/api/v3",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *DoubaoProvider) Name() string {
	return "doubao"
}

// GetCapabilities returns the capabilities of Doubao
func (p *DoubaoProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:     true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      false,
		Vision:          false,
	}
}

// Chat implements the Provider interface
func (p *DoubaoProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, nil, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getURL("/chat/completions")
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
func (p *DoubaoProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, tools, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getURL("/chat/completions")
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
func (p *DoubaoProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, nil, true)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getURL("/chat/completions")
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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

// getURL returns the appropriate URL based on configuration
func (p *DoubaoProvider) getURL(path string) string {
	if p.endpointID != "" {
		return fmt.Sprintf("%s/bots/%s%s", p.baseURL, p.endpointID, path)
	}
	return p.baseURL + path
}

// buildRequest builds the OpenAI-compatible request
func (p *DoubaoProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) map[string]interface{} {
	req := map[string]interface{}{
		"model": p.model,
		"messages": p.convertMessages(messages),
		"stream": stream,
	}

	if len(tools) > 0 {
		req["tools"] = tools
	}

	return req
}

// convertMessages converts messages to OpenAI format
func (p *DoubaoProvider) convertMessages(messages []Message) []map[string]interface{} {
	var converted []map[string]interface{}

	for _, msg := range messages {
		m := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}

		if msg.Role == "tool" {
			m["role"] = "tool"
			m["tool_call_id"] = msg.ToolCallID
		}

		if len(msg.ToolCalls) > 0 {
			var toolCalls []map[string]interface{}
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				})
			}
			m["tool_calls"] = toolCalls
		}

		converted = append(converted, m)
	}

	return converted
}

// parseResponse parses the OpenAI-compatible response
func (p *DoubaoProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var resp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	var content string
	var toolCalls []types.ToolCall

	if choice.Message.Content != "" {
		content = choice.Message.Content
	}

	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, types.ToolCall{
			ID:       tc.ID,
			Type:     "function",
			Function: types.Function{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
		})
	}

	return &ChatResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// parseStreamResponse parses streaming SSE response
func (p *DoubaoProvider) parseStreamResponse(body io.Reader, handler StreamHandler) error {
	scanner := bufio.NewScanner(body)
	var accumulatedContent string

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				accumulatedContent += choice.Delta.Content
				handler(&StreamResponse{
					Content: choice.Delta.Content,
					Done:    false,
				})
			}

			for _, tc := range choice.Delta.ToolCalls {
				handler(&StreamResponse{
					ToolCall: &types.ToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: types.Function{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					},
					Done: false,
				})
			}

			if choice.FinishReason != "" {
				handler(&StreamResponse{
					Content: "",
					Done:    true,
				})
				return nil
			}
		}
	}

	handler(&StreamResponse{
		Content: accumulatedContent,
		Done:    true,
	})

	return scanner.Err()
}

// parseError parses error responses
func (p *DoubaoProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return fmt.Errorf("doubao error [%s]: %s", errResp.Error.Type, errResp.Error.Message)
	}

	return fmt.Errorf("doubao error (%d): %s", statusCode, string(body))
}
