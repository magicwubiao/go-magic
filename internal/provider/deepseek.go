package provider

import (
	"context"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// DeepSeekProvider implements the DeepSeek API (OpenAI-compatible)
type DeepSeekProvider struct {
	*OpenAICompatibleProvider
}

// NewDeepSeekProvider creates a new DeepSeek provider
// If userModels is provided (non-nil and non-empty), it will be used; otherwise defaults are loaded
func NewDeepSeekProvider(apiKey, model string, userModels []ModelInfo) *DeepSeekProvider {
	if model == "" {
		model = "deepseek-chat"
	}
	return &DeepSeekProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("deepseek", apiKey, "https://api.deepseek.com", model, userModels),
	}
}

func (p *DeepSeekProvider) Name() string {
	return "deepseek"
}

// GetCapabilities returns the capabilities of DeepSeek
func (p *DeepSeekProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     false,
		Vision:         false,
	}
}

// Stream implements the Streamer interface (inherited from OpenAICompatibleProvider)
func (p *DeepSeekProvider) Stream(ctx context.Context, messages []types.Message, handler StreamHandler) error {
	return p.OpenAICompatibleProvider.Stream(ctx, messages, handler)
}

// StreamWithTools implements the StreamingToolCaller interface
func (p *DeepSeekProvider) StreamWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}, handler StreamHandler) error {
	return p.OpenAICompatibleProvider.StreamWithTools(ctx, messages, tools, handler)
}

// BaseStreamProvider provides common streaming functionality for non-OpenAI-compatible providers
type BaseStreamProvider struct {
	*OpenAICompatibleProvider
	StreamEndpoint string
}

// Stream implements the Streamer interface for base provider
func (bp *BaseStreamProvider) Stream(ctx context.Context, messages []types.Message, handler StreamHandler) error {
	reqBody := map[string]interface{}{
		"model":    bp.GetModel(),
		"messages": ConvertMessagesForProvider(messages, &bp.BaseProvider),
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}

	headers := map[string]string{}
	if bp.APIKey != "" {
		headers["Authorization"] = "Bearer " + bp.APIKey
	}

	url := bp.BaseURL + "/chat/completions"
	if bp.StreamEndpoint != "" {
		url = bp.StreamEndpoint
	}

	resp, err := bp.DoStreamRequest(ctx, url, reqBody, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return ParseStreamResponse(resp.Body, handler)
}

// StreamWithTools implements the StreamingToolCaller interface for base provider
func (bp *BaseStreamProvider) StreamWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}, handler StreamHandler) error {
	reqBody := map[string]interface{}{
		"model":    bp.GetModel(),
		"messages": ConvertMessagesForProvider(messages, &bp.BaseProvider),
		"tools":    tools,
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}

	headers := map[string]string{}
	if bp.APIKey != "" {
		headers["Authorization"] = "Bearer " + bp.APIKey
	}

	url := bp.BaseURL + "/chat/completions"
	if bp.StreamEndpoint != "" {
		url = bp.StreamEndpoint
	}

	resp, err := bp.DoStreamRequest(ctx, url, reqBody, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return ParseStreamResponseWithTools(resp.Body, handler)
}

// NewBaseStreamProvider creates a new provider with streaming support
func NewBaseStreamProvider(name, apiKey, baseURL, model string) *BaseStreamProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &BaseStreamProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults(name, apiKey, baseURL, model),
	}
}
