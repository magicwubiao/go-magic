package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// OpenAICompatibleProvider provides OpenAI-compatible API functionality
type OpenAICompatibleProvider struct {
	*BaseProvider
	name  string
	Model string
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider
func NewOpenAICompatibleProvider(name, apiKey, baseURL, model string) *OpenAICompatibleProvider {
	bp := NewBaseProvider(baseURL)
	bp.APIKey = apiKey
	
	return &OpenAICompatibleProvider{
		BaseProvider: bp,
		name:         name,
		Model:        model,
	}
}

// Name returns the provider name
func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

// Chat implements the Provider interface
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []types.Message) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    p.Model,
		"messages": ConvertMessages(messages),
	}

	url := p.BaseURL + "/chat/completions"
	
	headers := map[string]string{}
	respBody, statusCode, err := p.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}

	if statusCode != 200 {
		return nil, p.ParseAPIError(respBody, statusCode)
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Role      string `json:"role"`
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

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &ChatResponse{Content: ""}, nil
	}

	choice := response.Choices[0]
	
	chatResp := &ChatResponse{
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		chatResp.ToolCalls = make([]types.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			chatResp.ToolCalls = append(chatResp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: types.Function{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return chatResp, nil
}

// ChatWithTools implements the ToolCaller interface
func (p *OpenAICompatibleProvider) ChatWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    p.Model,
		"messages": ConvertMessages(messages),
		"tools":    tools,
	}

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	respBody, statusCode, err := p.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, fmt.Errorf("chat with tools request failed: %w", err)
	}

	if statusCode != 200 {
		return nil, p.ParseAPIError(respBody, statusCode)
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Role      string `json:"role"`
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
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &ChatResponse{Content: ""}, nil
	}

	choice := response.Choices[0]
	
	chatResp := &ChatResponse{
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		chatResp.ToolCalls = make([]types.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			chatResp.ToolCalls = append(chatResp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: types.Function{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return chatResp, nil
}

// Stream implements the Streamer interface
func (p *OpenAICompatibleProvider) Stream(ctx context.Context, messages []types.Message, handler StreamHandler) error {
	return p.streamWithContext(ctx, messages, nil, false, handler)
}

// StreamWithTools implements the StreamingToolCaller interface
func (p *OpenAICompatibleProvider) StreamWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}, handler StreamHandler) error {
	return p.streamWithContext(ctx, messages, tools, true, handler)
}

// streamWithContext is the internal streaming implementation
func (p *OpenAICompatibleProvider) streamWithContext(ctx context.Context, messages []types.Message, tools []map[string]interface{}, withTools bool, handler StreamHandler) error {
	reqBody := map[string]interface{}{
		"model":    p.Model,
		"messages": ConvertMessages(messages),
		"stream":   true,
	}

	if withTools && tools != nil {
		reqBody["tools"] = tools
	}

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	resp, err := p.DoStreamRequest(ctx, url, reqBody, headers)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stream API returned status %d: %s", resp.StatusCode, string(body))
	}

	return ParseStreamResponse(resp.Body, handler)
}
