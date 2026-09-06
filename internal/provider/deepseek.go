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
// If baseURL is empty, the default DeepSeek API URL will be used
func NewDeepSeekProvider(apiKey, baseURL, model string, userModels []ModelInfo) *DeepSeekProvider {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		model = "deepseek-v4-flash"
	}
	return &DeepSeekProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("deepseek", apiKey, baseURL, model, userModels),
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
	cfg := bp.BaseProvider.ConvertCfg
	if cfg != nil {
		cfg = cfg.WithAutoVision(bp.GetModel())
	}
	reqBody := map[string]interface{}{
		"model":    bp.GetModel(),
		"messages": ConvertMessagesWithConfig(messages, cfg),
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

	resp, err := bp.DoStreamRequestWithBreaker(ctx, url, reqBody, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return ParseStreamResponse(ctx, resp.Body, handler)
}

// StreamWithTools implements the StreamingToolCaller interface for base provider
func (bp *BaseStreamProvider) StreamWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}, handler StreamHandler) error {
	cfg := bp.BaseProvider.ConvertCfg
	if cfg != nil {
		cfg = cfg.WithAutoVision(bp.GetModel())
	}
	reqBody := map[string]interface{}{
		"model":    bp.GetModel(),
		"messages": ConvertMessagesWithConfig(messages, cfg),
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

	resp, err := bp.DoStreamRequestWithBreaker(ctx, url, reqBody, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return ParseStreamResponseWithTools(ctx, resp.Body, handler)
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
