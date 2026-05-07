package provider

import (
	
)

// ZhipuProvider 智谱AI (兼容OpenAI格式)
type ZhipuProvider struct {
	*OpenAICompatibleProvider
}

// NewZhipuProvider creates a new Zhipu provider
func NewZhipuProvider(apiKey, model string) *ZhipuProvider {
	if model == "" {
		model = "glm-4"
	}
	return &ZhipuProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("zhipu", apiKey, "https://open.bigmodel.cn/api/paas/v4", model),
	}
}

func (p *ZhipuProvider) Name() string {
	return "zhipu"
}

// GetCapabilities returns the capabilities of Zhipu
func (p *ZhipuProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}
