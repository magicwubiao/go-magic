package provider

// LongCatProvider implements the Meituan LongCat API using OpenAI-compatible format.
// API docs: https://longcat.chat/platform/docs
// Base URL: https://api.longcat.chat/openai/v1
type LongCatProvider struct {
	*OpenAICompatibleProvider
}

// NewLongCatProvider creates a new LongCat provider.
func NewLongCatProvider(apiKey, baseURL, model string) *LongCatProvider {
	if model == "" {
		model = "LongCat-2.0-Preview"
	}
	if baseURL == "" {
		baseURL = "https://api.longcat.chat/openai/v1"
	}
	return &LongCatProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("longcat", apiKey, baseURL, model),
	}
}

func (p *LongCatProvider) Name() string {
	return "longcat"
}

// GetCapabilities returns the capabilities of LongCat.
func (p *LongCatProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     false,
		Vision:         false,
	}
}
