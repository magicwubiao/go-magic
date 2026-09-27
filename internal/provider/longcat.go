package provider

import "github.com/magicwubiao/go-magic/pkg/catalog"

// LongCatProvider implements the Meituan LongCat API using OpenAI-compatible format.
// API docs: https://longcat.chat/platform/docs/zh/APIDocs.html
// Endpoint: https://api.longcat.chat/openai/v1/chat/completions (Bearer auth)
// Models: LongCat-2.0-Preview (Flash 系列已于 2026-05-29 停止服务).
type LongCatProvider struct {
	*OpenAICompatibleProvider
}

// NewLongCatProvider creates a new LongCat provider
func NewLongCatProvider(apiKey, baseURL, model string) *LongCatProvider {
	if model == "" {
		model = catalog.DefaultModel("longcat")
	}
	if baseURL == "" {
		baseURL = catalog.BaseURL("longcat")
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
