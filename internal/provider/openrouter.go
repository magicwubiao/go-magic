package provider

import (
	
)

// OpenRouterProvider OpenRouter (兼容OpenAI格式)
type OpenRouterProvider struct {
	*OpenAICompatibleProvider
}

// NewOpenRouterProvider creates a new OpenRouter provider
func NewOpenRouterProvider(apiKey, model string) *OpenRouterProvider {
	if model == "" {
		model = "openai/gpt-4"
	}
	return &OpenRouterProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("openrouter", apiKey, "https://openrouter.ai/api/v1", model),
	}
}

func (p *OpenRouterProvider) Name() string {
	return "openrouter"
}

// GetCapabilities returns the capabilities of OpenRouter
func (p *OpenRouterProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}
