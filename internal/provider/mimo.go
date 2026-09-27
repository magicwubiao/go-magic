package provider

import "github.com/magicwubiao/go-magic/pkg/catalog"

// MiMoProvider implements the Xiaomi MiMo API using OpenAI-compatible format.
type MiMoProvider struct {
	*OpenAICompatibleProvider
}

// NewMiMoProvider creates a new MiMo provider
func NewMiMoProvider(apiKey, baseURL, model string) *MiMoProvider {
	if model == "" {
		model = catalog.DefaultModel("mimo")
	}
	if baseURL == "" {
		baseURL = catalog.BaseURL("mimo")
	}
	return &MiMoProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("mimo", apiKey, baseURL, model),
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
