package provider

import "github.com/magicwubiao/go-magic/pkg/catalog"

// OpenAIProvider implements the OpenAI API
type OpenAIProvider struct {
	*OpenAICompatibleProvider
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = catalog.BaseURL("openai")
	}
	if model == "" {
		model = catalog.DefaultModel("openai")
	}
	return &OpenAIProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("openai", apiKey, baseURL, model),
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

// GetCapabilities returns the capabilities of OpenAI
func (p *OpenAIProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
