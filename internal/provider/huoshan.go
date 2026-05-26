package provider

// HuoshanProvider 火山方舟 (字节跳动，兼容OpenAI格式)
type HuoshanProvider struct {
	*OpenAICompatibleProvider
}

// NewHuoshanProvider creates a new Huoshan provider
func NewHuoshanProvider(apiKey, baseURL, model string) *HuoshanProvider {
	if model == "" {
		model = "ep-xxxxx" // 火山方舟的endpoint ID
	}
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}
	return &HuoshanProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("huoshan", apiKey, baseURL, model),
	}
}

func (p *HuoshanProvider) Name() string {
	return "huoshan"
}

// GetCapabilities returns the capabilities of Huoshan
func (p *HuoshanProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
