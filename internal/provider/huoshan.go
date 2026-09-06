package provider

// HuoshanProvider 火山方舟（Volcengine Ark）—— 豆包大模型的官方提供渠道。
// 豆包（Doubao）由字节跳动通过火山引擎提供，本类型是该服务的唯一实现，
// "doubao" 仅作为兼容别名保留（配置/命令中两者等价）。
type HuoshanProvider struct {
	*OpenAICompatibleProvider
	endpointID string // Optional endpoint ID for Volcengine
}

// NewHuoshanProvider creates a new Huoshan (Volcengine Ark) provider
func NewHuoshanProvider(apiKey, baseURL, model string) *HuoshanProvider {
	if model == "" {
		model = "doubao-seed-2.1-pro"
	}
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}
	return &HuoshanProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("huoshan", apiKey, baseURL, model),
	}
}

// NewHuoshanProviderWithEndpoint creates a Huoshan provider with custom endpoint ID
func NewHuoshanProviderWithEndpoint(apiKey, endpointID string) *HuoshanProvider {
	baseURL := "https://ark.cn-beijing.volces.com/api/v3"
	return &HuoshanProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("huoshan", apiKey, baseURL, endpointID),
		endpointID:               endpointID,
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
