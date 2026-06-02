package provider

// MistralProvider implements the Mistral AI API using OpenAI-compatible format.
type MistralProvider struct {
	*OpenAICompatibleProvider
}

// NewMistralProvider creates a new Mistral AI provider
func NewMistralProvider(apiKey, baseURL, model string) *MistralProvider {
	if model == "" {
		model = "mistral-small-latest" // Default model
	}
	if baseURL == "" {
		baseURL = "https://api.mistral.ai/v1"
	}
	return &MistralProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("mistral", apiKey, baseURL, model),
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