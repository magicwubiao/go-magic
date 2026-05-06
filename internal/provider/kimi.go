package provider

import (
	"context"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// KimiProvider 月之暗面 Kimi (兼容OpenAI格式)
type KimiProvider struct {
	*OpenAICompatibleProvider
}

// NewKimiProvider creates a new Kimi provider
func NewKimiProvider(apiKey, model string) *KimiProvider {
	if model == "" {
		model = "moonshot-v1-8k"
	}
	return &KimiProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("kimi", apiKey, "https://api.moonshot.cn/v1", model),
	}
}

func (p *KimiProvider) Name() string {
	return "kimi"
}

// GetCapabilities returns the capabilities of Kimi
func (p *KimiProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}
