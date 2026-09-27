package provider

import "github.com/magicwubiao/go-magic/pkg/catalog"

// MistralProvider implements the Mistral AI API using OpenAI-compatible format.
type MistralProvider struct {
	*OpenAICompatibleProvider
}

// NewMistralProvider creates a new Mistral AI provider
func NewMistralProvider(apiKey, baseURL, model string) *MistralProvider {
	if model == "" {
		model = catalog.DefaultModel("mistral")
	}
	if baseURL == "" {
		baseURL = catalog.BaseURL("mistral")
	}
	return &MistralProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("mistral", apiKey, baseURL, model),
	}
}

func (p *MistralProvider) Name() string {
	return "mistral"
}

// GetCapabilities returns the capabilities of Mistral AI
func (p *MistralProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
