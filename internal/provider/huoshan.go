package provider

import (
	
)

// HuoshanProvider 火山方舟 (字节跳动，兼容OpenAI格式)
type HuoshanProvider struct {
	*OpenAICompatibleProvider
}

// NewHuoshanProvider creates a new Huoshan provider
func NewHuoshanProvider(apiKey, model string) *HuoshanProvider {
	if model == "" {
		model = "ep-xxxxx" // 火山方舟的endpoint ID
	}
	return &HuoshanProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("huoshan", apiKey, "https://ark.cn-beijing.volces.com/api/v3", model),
	}
}

func (p *HuoshanProvider) Name() string {
	return "huoshan"
}

// GetCapabilities returns the capabilities of Huoshan
func (p *HuoshanProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}
