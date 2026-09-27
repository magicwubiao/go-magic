package provider

import "github.com/magicwubiao/go-magic/pkg/catalog"

// MiniMaxProvider MiniMax (兼容OpenAI格式)
type MiniMaxProvider struct {
	*OpenAICompatibleProvider
}

// NewMiniMaxProvider creates a new MiniMax provider
func NewMiniMaxProvider(apiKey, baseURL, model string) *MiniMaxProvider {
	if model == "" {
		model = catalog.DefaultModel("minimax")
	}
	if baseURL == "" {
		baseURL = catalog.BaseURL("minimax")
	}
	return &MiniMaxProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("minimax", apiKey, baseURL, model),
	}
}

func (p *MiniMaxProvider) Name() string {
	return "minimax"
}

// GetCapabilities returns the capabilities of MiniMax
func (p *MiniMaxProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     false,
		Vision:         false,
	}
}
