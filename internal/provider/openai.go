package provider

import (
	
)

// OpenAIProvider implements the OpenAI API
type OpenAIProvider struct {
	*OpenAICompatibleProvider
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4"
	}
	return &OpenAIProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("openai", apiKey, baseURL, model),
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

// GetCapabilities returns the capabilities of OpenAI
func (p *OpenAIProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}
