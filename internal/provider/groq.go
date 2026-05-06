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

// GroqProvider implements the Groq API (fast inference)
// Groq uses OpenAI-compatible format but with different base URL and specific models
type GroqProvider struct {
	*BaseProvider
	apiKey   string
	model    string
	baseURL  string
	metrics  *RequestMetrics
}

// NewGroqProvider creates a new Groq provider
func NewGroqProvider(apiKey, model string) *GroqProvider {
	if model == "" {
		model = "mixtral-8x7b-32768" // Default to fast model
	}
	
	return &GroqProvider{
		BaseProvider: NewBaseProvider("https://api.groq.com/openai/v1"),
		apiKey:      apiKey,
		model:       model,
		baseURL:     "https://api.groq.com/openai/v1",
		metrics:     &RequestMetrics{},
	}
}

func (p *GroqProvider) Name() string {
	return "groq"
}

// GetCapabilities returns the capabilities of Groq
func (p *GroqProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:    false,
		Vision:         false,
	}
}

// Chat implements the Provider interface
func (p *GroqProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, nil, false)
	
	start := time.Now()
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		p.metrics.AddFailure()
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.Client.Do(req)
	if err != nil {
		p.metrics.AddFailure()
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.metrics.AddFailure()
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		p.metrics.AddFailure()
		return nil, p.parseError(body, resp.StatusCode)
	}

	p.metrics.AddRequest(time.Since(start), nil)
	return p.parseResponse(body)
}

// ChatWithTools implements the ToolCaller interface
func (p *GroqProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, tools, false)
	
	start := time.Now()
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		p.metrics.AddFailure()
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.Client.Do(req)
	if err != nil {
		p.metrics.AddFailure()
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.metrics.AddFailure()
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		p.metrics.AddFailure()
		return nil, p.parseError(body, resp.StatusCode)
	}

	p.metrics.AddRequest(time.Since(start), nil)
	return p.parseResponse(body)
}

// Stream implements the Streamer interface
func (p *GroqProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, nil, true)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.Client.Do(req)
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

// StreamWithTools implements streaming with tool calls
func (p *GroqProvider) StreamWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, tools, true)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.Client.Do(req)
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

// buildRequest builds the OpenAI-compatible request
func (p *GroqProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) map[string]interface{} {
	req := map[string]interface{}{
		"model":    p.model,
		"messages": p.convertMessages(messages),
		"stream":   stream,
	}

	if len(tools) > 0 {
		req["tools"] = tools
	}

	return req
}

// convertMessages converts messages to OpenAI format
func (p *GroqProvider) convertMessages(messages []Message) []map[string]interface{} {
	var converted []map[string]interface{}

	for _, msg := range messages {
		m := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}

		if msg.Role == "tool" {
			m["tool_call_id"] = msg.ToolCallID
			m["tool_role"] = "assistant"
		}

		if len(msg.ToolCalls) > 0 {
			var toolCalls []map[string]interface{}
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": marshalArgs(tc.Arguments),
					},
				})
			}
			m["tool_calls"] = toolCalls
		}

		converted = append(converted, m)
	}

	return converted
}

// marshalArgs converts arguments to JSON string
func marshalArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	data, _ := json.Marshal(args)
	return string(data)
}

// parseResponse parses the OpenAI-compatible response
func (p *GroqProvider) parseResponse(body []byte) (*ChatResponse, error) {
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
			ID:   tc.ID,
			Type: "function",
			Function: types.Function{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
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
func (p *GroqProvider) parseStreamResponse(body io.Reader, handler StreamHandler) error {
	scanner := bufio.NewScanner(body)
	var accumulatedContent strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		data = strings.TrimSpace(data)
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
				accumulatedContent.WriteString(choice.Delta.Content)
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
		Content: accumulatedContent.String(),
		Done:    true,
	})

	return scanner.Err()
}

// parseError parses error responses
func (p *GroqProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return fmt.Errorf("groq error [%s]: %s", errResp.Error.Type, errResp.Error.Message)
	}

	return fmt.Errorf("groq error (%d): %s", statusCode, string(body))
}

// GetMetrics returns provider metrics
func (p *GroqProvider) GetMetrics() (total, failed int64, avgLatency time.Duration, usage Usage) {
	return p.metrics.GetStats()
}

// HealthCheck checks if the provider is reachable
func (p *GroqProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}
