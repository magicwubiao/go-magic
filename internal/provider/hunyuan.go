package provider

// HunyuanProvider implements the Tencent Hunyuan API using OpenAI-compatible format.
type HunyuanProvider struct {
	*OpenAICompatibleProvider
}

// NewHunyuanProvider creates a new Hunyuan provider
func NewHunyuanProvider(apiKey, baseURL, model string) *HunyuanProvider {
	if model == "" {
		model = "hunyuan-turbo" // Default model
	}
	if baseURL == "" {
		baseURL = "https://api.hunyuan.cloud.tencent.com/v1"
	}
	return &HunyuanProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("hunyuan", apiKey, baseURL, model),
	}
}

func (p *HunyuanProvider) Name() string {
	return "hunyuan"
}

// GetCapabilities returns the capabilities of Hunyuan
func (p *HunyuanProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
