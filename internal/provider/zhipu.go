package provider

// ZhipuProvider 智谱AI (兼容OpenAI格式)
type ZhipuProvider struct {
	*OpenAICompatibleProvider
}

// NewZhipuProvider creates a new Zhipu provider
func NewZhipuProvider(apiKey, baseURL, model string) *ZhipuProvider {
	if model == "" {
		model = "glm-4"
	}
	if baseURL == "" {
		baseURL = "https://open.bigmodel.cn/api/paas/v4"
	}
	return &ZhipuProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProviderWithDefaults("zhipu", apiKey, baseURL, model),
	}
}

func (p *ZhipuProvider) Name() string {
	return "zhipu"
}

// GetCapabilities returns the capabilities of Zhipu
// GLM 系列对稳定前缀自动启用隐式上下文缓存（无需请求参数），
// 命中量通过 usage.prompt_tokens_details.cached_tokens 上报，
// 已由 OpenAICompatibleProvider 统一解析至 Usage.CacheReadTokens。
func (p *ZhipuProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
		PromptCaching:  true,
	}
}
