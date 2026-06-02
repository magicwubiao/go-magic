package provider

// MiMoProvider implements the Xiaomi MiMo API using OpenAI-compatible format.
type MiMoProvider struct {
	*OpenAICompatibleProvider
}

// NewMiMoProvider creates a new MiMo provider
func NewMiMoProvider(apiKey, baseURL, model string) *MiMoProvider {
	if model == "" {
		model = "mimo-v2-flash" // Default model
	}
	if baseURL == "" {
		baseURL = "https://api.xiaomimimo.com/v1"
	}
	return &MiMoProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("mimo", apiKey, baseURL, model),
	}
}

func (p *MiMoProvider) Name() string {
	return "mimo"
}

// GetCapabilities returns the capabilities of MiMo
func (p *MiMoProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     false,
		Vision:         false,
	}
}