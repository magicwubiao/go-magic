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

// HunyuanProvider implements the Tencent Hunyuan API
type HunyuanProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewHunyuanProvider creates a new Hunyuan provider
func NewHunyuanProvider(apiKey, model string) *HunyuanProvider {
	if model == "" {
		model = "hunyuan-turbo" // Default model
	}
	return &HunyuanProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.hunyuan.cloud.tencent.com/v1",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *HunyuanProvider) Name() string {
	return "hunyuan"
}

// GetCapabilities returns the capabilities of Hunyuan
func (p *HunyuanProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      false,
		Vision:          false,
	}
}

// Chat implements the Provider interface
func (p *HunyuanProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, nil, false)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"
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
func (p *HunyuanProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, tools, false)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"
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
func (p *HunyuanProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, nil, true)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "text/event-stream")

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

// buildRequest builds the Hunyuan request from messages
func (p *HunyuanProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) map[string]interface{} {
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

// convertMessages converts messages to Hunyuan format
func (p *HunyuanProvider) convertMessages(messages []Message) []map[string]interface{} {
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

// parseResponse parses the Hunyuan response
func (p *HunyuanProvider) parseResponse(body []byte) (*ChatResponse, error) {
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
func (p *HunyuanProvider) parseStreamResponse(body io.Reader, handler StreamHandler) error {
	scanner := bufio.NewScanner(body)
	var accumulatedContent string

	for scanner.Scan() {
		line := scanner.Text()

		// Hunyuan uses custom SSE format: data: {"Note":"...","Choices":[...]}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		var chunk struct {
			ID      string `json:"Id"`
			Note    string `json:"Note"`
			Choices []struct {
				Delta struct {
					Role    string `json:"Role"`
					Content string `json:"Content"`
					ToolCalls []struct {
						ID       string `json:"ID"`
						Type     string `json:"Type"`
						Function struct {
							Name      string `json:"Name"`
							Arguments string `json:"Arguments"`
						} `json:"Function"`
					} `json:"ToolCalls"`
				} `json:"Delta"`
				FinishReason string `json:"FinishReason"`
			} `json:"Choices"`
			Usage struct {
				PromptTokens     int `json:"PromptTokens"`
				CompletionTokens int `json:"CompletionTokens"`
				TotalTokens      int `json:"TotalTokens"`
			} `json:"Usage"`
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

			if choice.FinishReason == "stop" || choice.FinishReason != "" {
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
func (p *HunyuanProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return fmt.Errorf("hunyuan error [%s]: %s", errResp.Error.Code, errResp.Error.Message)
	}

	// Try to parse Hunyuan's error format
	var hunyuanErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &hunyuanErr); err == nil && hunyuanErr.Message != "" {
		return fmt.Errorf("hunyuan error [%d]: %s", hunyuanErr.Code, hunyuanErr.Message)
	}

	return fmt.Errorf("hunyuan error (%d): %s", statusCode, string(body))
}
