package provider

// MoonshotProvider implements the Moonshot (Kimi) AI API using OpenAI-compatible format.
// Note: Moonshot and Kimi use the same API endpoint (https://api.moonshot.cn/v1).
// This provider is kept for backward compatibility with existing configurations.
type MoonshotProvider struct {
	*OpenAICompatibleProvider
}

// NewMoonshotProvider creates a new Moonshot provider
func NewMoonshotProvider(apiKey, baseURL, model string) *MoonshotProvider {
	if model == "" {
		model = "kimi-k2-0905-preview" // Default model
	}
	if baseURL == "" {
		baseURL = "https://api.moonshot.cn/v1"
	}
	return &MoonshotProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("moonshot", apiKey, baseURL, model),
	}
}

func (p *MoonshotProvider) Name() string {
	return "moonshot"
}

// GetCapabilities returns the capabilities of Moonshot
func (p *MoonshotProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
