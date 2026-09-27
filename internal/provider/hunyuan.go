package provider

import "github.com/magicwubiao/go-magic/pkg/catalog"

// HunyuanProvider implements the Tencent Hunyuan API using OpenAI-compatible format.
type HunyuanProvider struct {
	*OpenAICompatibleProvider
}

// NewHunyuanProvider creates a new Hunyuan provider
func NewHunyuanProvider(apiKey, baseURL, model string) *HunyuanProvider {
	if model == "" {
		model = catalog.DefaultModel("hunyuan")
	}
	if baseURL == "" {
		baseURL = catalog.BaseURL("hunyuan")
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
