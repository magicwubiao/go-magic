package provider

// MetaProvider implements the Meta Model API (Muse Spark) using OpenAI-compatible format.
// API docs: https://ai.developer.meta.com/docs/overview
// Endpoint: https://api.meta.ai/v1/chat/completions (Bearer auth, MODEL_API_KEY)
// Models: muse-spark-1.3, muse-spark-1.2, muse-spark-1.1 (1M context; multimodal reasoning,
// parallel tool calling, streamed tool-call arguments, reasoning carried across turns).
type MetaProvider struct {
	*OpenAICompatibleProvider
}

// NewMetaProvider creates a new Meta Model API provider
func NewMetaProvider(apiKey, baseURL, model string) *MetaProvider {
	if model == "" {
		model = "muse-spark-1.3" // Default model (latest, released 2026-09-02)
	}
	if baseURL == "" {
		baseURL = "https://api.meta.ai/v1"
	}
	return &MetaProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("meta", apiKey, baseURL, model),
	}
}

func (p *MetaProvider) Name() string {
	return "meta"
}

// GetCapabilities returns the capabilities of the Meta Model API.
// Muse Spark accepts text, image, video and PDF understanding; streaming and
// (parallel) tool calling are supported through the OpenAI-compatible surface.
func (p *MetaProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
