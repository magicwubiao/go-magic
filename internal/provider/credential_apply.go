package provider

import "strings"

// 本文件负责「把刚保存的新凭据装到正在运行的 provider 实例上」。
//
// 背景（改 key 不生效事故）：web 端每个会话的 agent 是缓存的，并且共享 server 的
// 同一个 provider 实例（agent.NewEnhancedAgent(s.provider, ...)）。用户在「模型
// 设置」页把 API Key 改成正确的值后，config.json 更新了、运行中的实例却仍持有旧
// key，于是聊天继续拿旧 key 发请求，一直 401「无效的 API Key……请重新生成」——
// 而同一页面的「测试连接」按钮却是通的，因为它是用刚保存的配置**新建**一个临时
// provider 发请求（handleProvidersTest）。这与视觉策略（ApplyConvertConfig）是
// 同一类「配置改了但运行实例没跟上」的 bug，修法一致：就地更新实例，不重建 agent、
// 不丢会话状态。
//
// ApplyCredentials 返回 false 表示该 provider 把凭据存在本文件不认识的地方
// （gemini/together/perplexity/wenxin/cohere 等私有 apiKey 字段），调用方必须
// 改为重建 provider 并清掉缓存 agent，否则改了 key 依旧不生效。

// credentialSetter 是「能就地替换凭据」的 provider 需要实现的接口。
// OpenAI 兼容家族（zhipu/deepseek/longcat/openai/moonshot/huoshan/hunyuan/mimo/
// meta/minimax/mistral/groq/openrouter/custom…）都内嵌 *OpenAICompatibleProvider，
// 因此自动获得该方法，无需逐文件实现。空串表示「保持原值」。
type credentialSetter interface {
	SetCredentials(apiKey, baseURL string)
}

// ApplyCredentials installs new credential material on a live provider instance.
// See the file header for why an in-place update (not a provider rebuild) is the
// right move on the web server path.
func ApplyCredentials(p Provider, apiKey, baseURL string) bool {
	if p == nil {
		return false
	}
	if apiKey == "" && baseURL == "" {
		// 没有要改的字段：无需更新，也无需重建。
		return true
	}
	if cs, ok := p.(credentialSetter); ok {
		cs.SetCredentials(apiKey, baseURL)
		return true
	}
	return false
}

// applyToBaseProvider 更新 BaseProvider 上的凭据并清零熔断计数。
//
// 熔断清零是必须的：无效 key 连续失败会把熔断器打到 open（RecordFailure ≥5 次），
// 此时即使换上正确的 key，请求也会被熔断器挡下 —— 用户看到的仍是「还是不行」。
func applyToBaseProvider(bp *BaseProvider, apiKey, baseURL string) {
	if bp == nil {
		return
	}
	if apiKey != "" {
		bp.APIKey = apiKey
	}
	if baseURL != "" {
		bp.BaseURL = strings.TrimRight(baseURL, "/")
	}
	bp.ResetCircuitBreaker()
}

// SetCredentials implements credentialSetter for the OpenAI-compatible family
// (request path reads BaseProvider.APIKey / BaseProvider.BaseURL per request).
func (p *OpenAICompatibleProvider) SetCredentials(apiKey, baseURL string) {
	if p == nil || p.BaseProvider == nil {
		return
	}
	applyToBaseProvider(p.BaseProvider, apiKey, baseURL)
}

// SetCredentials implements credentialSetter for DashScope, which keeps its own
// apiKey/baseURL copies for request building (dashscope.go 请求路径读的是
// p.apiKey / p.baseURL，不是 BaseProvider 上的字段，两边都要写)。
func (p *DashScopeProvider) SetCredentials(apiKey, baseURL string) {
	if p == nil {
		return
	}
	if apiKey != "" {
		p.apiKey = apiKey
	}
	if baseURL != "" {
		p.baseURL = strings.TrimRight(baseURL, "/")
	}
	if p.BaseProvider != nil {
		applyToBaseProvider(p.BaseProvider, apiKey, baseURL)
	}
}

// SetCredentials implements credentialSetter for Anthropic. baseURL is ignored:
// the Anthropic provider has no configurable endpoint (constructor takes only
// apiKey + model) and reads p.apiKey per request.
func (p *AnthropicProvider) SetCredentials(apiKey, _ string) {
	if p == nil || apiKey == "" {
		return
	}
	p.apiKey = apiKey
}
