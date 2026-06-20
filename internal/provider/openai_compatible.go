package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// OpenAICompatibleProvider provides OpenAI-compatible API functionality
type OpenAICompatibleProvider struct {
	*BaseProvider
	name   string
	model  string
	models []ModelInfo
	mu     sync.RWMutex
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider
// If userModels is provided (non-nil), it will be used; otherwise defaults are loaded
func NewOpenAICompatibleProvider(name, apiKey, baseURL, model string, userModels []ModelInfo) *OpenAICompatibleProvider {
	var modelList []ModelInfo

	// Use user-provided models if available
	if len(userModels) > 0 {
		modelList = userModels
	} else {
		// Get default models for this provider if available
		modelList = GetDefaultModels(name)
		if modelList == nil {
			modelList = []ModelInfo{{ID: model, Name: model, Description: "Default model"}}
		}
	}

	return &OpenAICompatibleProvider{
		BaseProvider: NewBaseProvider(baseURL).WithAPIKey(apiKey),
		name:         name,
		model:        model,
		models:       modelList,
	}
}

// NewOpenAICompatibleProviderWithDefaults creates a new OpenAI-compatible provider with default models
// This is a convenience function for providers that don't need custom model lists
func NewOpenAICompatibleProviderWithDefaults(name, apiKey, baseURL, model string) *OpenAICompatibleProvider {
	return NewOpenAICompatibleProvider(name, apiKey, baseURL, model, nil)
}

// Name returns the provider name
func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

// SetModel sets the current model. If model is not in the list, it will be added.
func (p *OpenAICompatibleProvider) SetModel(model string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if model is already in the list
	for _, m := range p.models {
		if m.ID == model {
			p.model = model
			return nil
		}
	}

	// Model not in list, add it dynamically
	p.models = append(p.models, ModelInfo{
		ID:          model,
		Name:        model,
		Description: "User configured model",
	})
	p.model = model
	return nil
}

// GetModel returns the current model ID.
func (p *OpenAICompatibleProvider) GetModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// ListModels returns the list of supported models.
func (p *OpenAICompatibleProvider) ListModels() []ModelInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]ModelInfo, len(p.models))
	copy(result, p.models)
	return result
}

// SetModels sets the list of supported models.
func (p *OpenAICompatibleProvider) SetModels(models []ModelInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.models = models
}

// SetConvertConfig implements ConvertConfigProvider
func (p *OpenAICompatibleProvider) SetConvertConfig(cfg *ConvertConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ConvertCfg = cfg
}

// Chat implements the Provider interface
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []types.Message) (*types.ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": ConvertMessagesForProvider(messages, p.BaseProvider),
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
		return &types.ChatResponse{Content: ""}, nil
	}

	choice := response.Choices[0]

	chatResp := &types.ChatResponse{
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		chatResp.ToolCalls = make([]types.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			// Ensure type is always "function"
			tcType := tc.Type
			if tcType == "" {
				tcType = "function"
			}
			chatResp.ToolCalls = append(chatResp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: tcType,
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
func (p *OpenAICompatibleProvider) ChatWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}) (*types.ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":       p.GetModel(),
		"messages":    ConvertMessagesForProvider(messages, p.BaseProvider),
		"tools":       tools,
		"tool_choice": "auto",
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
		return &types.ChatResponse{Content: ""}, nil
	}

	choice := response.Choices[0]

	chatResp := &types.ChatResponse{
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		chatResp.ToolCalls = make([]types.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			// Ensure type is always "function"
			tcType := tc.Type
			if tcType == "" {
				tcType = "function"
			}
			chatResp.ToolCalls = append(chatResp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: tcType,
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
		"model":    p.GetModel(),
		"messages": ConvertMessagesForProvider(messages, p.BaseProvider),
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}

	if withTools && tools != nil {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
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

	if withTools {
		return ParseStreamResponseWithTools(resp.Body, handler)
	}
	return ParseStreamResponse(resp.Body, handler)
}
