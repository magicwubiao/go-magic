package provider

// LongCatProvider implements the Meituan LongCat API using OpenAI-compatible format.
// API docs: https://longcat.chat/platform/docs/zh/APIDocs.html
// Endpoint: https://api.longcat.chat/openai/v1/chat/completions (Bearer auth)
// Models: LongCat-Flash-Chat, LongCat-Flash-Thinking, LongCat-Flash-Thinking-2601,
//
//	LongCat-Flash-Lite, LongCat-2.0-Preview (text input only).
type LongCatProvider struct {
	*OpenAICompatibleProvider
}

// NewLongCatProvider creates a new LongCat provider
func NewLongCatProvider(apiKey, baseURL, model string) *LongCatProvider {
	if model == "" {
		model = "LongCat-Flash-Chat" // Default model
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
// The documented chat completions API accepts text-only input; streaming is supported.
func (p *LongCatProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     false,
		Vision:         false,
	}
}
