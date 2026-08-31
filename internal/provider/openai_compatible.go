package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// OpenAICompatibleProvider provides OpenAI-compatible API functionality
type OpenAICompatibleProvider struct {
	*BaseProvider
	name        string
	model       string
	models      []ModelInfo
	mu          sync.RWMutex
	extraParams map[string]interface{} // 请求体透传参数（缓存开关等 provider 特有字段）
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider
// If userModels is provided (non-nil), it will be used; otherwise defaults are loaded
func NewOpenAICompatibleProvider(name, apiKey, baseURL, model string, userModels []ModelInfo) *OpenAICompatibleProvider {
	// Defensive: apply default base URL per provider if not provided
	if baseURL == "" {
		switch name {
		case "openai", "custom":
			baseURL = "https://api.openai.com/v1"
		case "anthropic":
			baseURL = "https://api.anthropic.com/v1"
		case "deepseek":
			baseURL = "https://api.deepseek.com"
		case "kimi", "moonshot": // kimi 为 moonshot 兼容别名
			baseURL = "https://api.moonshot.cn/v1"
		case "zhipu":
			baseURL = "https://open.bigmodel.cn/api/paas/v4"
		case "minimax":
			baseURL = "https://api.minimax.chat/v1"
		case "groq":
			baseURL = "https://api.groq.com/openai/v1"
		case "openrouter":
			baseURL = "https://openrouter.ai/api/v1"
		case "mistral":
			baseURL = "https://api.mistral.ai/v1"
		case "dashscope":
			baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		case "doubao", "huoshan": // doubao 为 huoshan 兼容别名
			baseURL = "https://ark.cn-beijing.volces.com/api/v3"
		case "perplexity":
			baseURL = "https://api.perplexity.ai"
		case "hunyuan":
			baseURL = "https://api.hunyuan.cloud.tencent.com/v1"
		case "mimo":
			baseURL = "https://api.xiaomimimo.com/v1"
		case "longcat":
			baseURL = "https://api.longcat.chat/openai/v1"
		case "ollama":
			baseURL = "http://localhost:11434"
		case "vllm":
			baseURL = "http://localhost:8000/v1"
		}
	}

	if model == "" {
		switch name {
		case "openai":
			model = "gpt-5.6"
		case "custom":
			model = "gpt-4o-mini"
		case "anthropic":
			model = "claude-sonnet-5"
		case "deepseek":
			model = "deepseek-chat"
		case "kimi", "moonshot": // kimi 为 moonshot 兼容别名
			model = "kimi-k2-0905-preview"
		case "zhipu":
			model = "glm-4.6"
		case "minimax":
			model = "MiniMax-M2.5"
		case "groq":
			model = "llama-3.3-70b-versatile"
		case "openrouter":
			model = "openai/gpt-5.6"
		case "mistral":
			model = "mistral-large-latest"
		case "dashscope":
			model = "qwen-plus"
		case "doubao", "huoshan": // doubao 为 huoshan 兼容别名
			model = "doubao-1.5-pro-32k"
		case "perplexity":
			model = "sonar-pro"
		case "hunyuan":
			model = "hunyuan-turbos-latest"
		case "mimo":
			model = "mimo-v2-flash"
		case "longcat":
			model = "LongCat-2.0-Preview"
		case "ollama":
			model = "llama3.3"
		case "vllm":
			model = "default-model"
		}
	}

	var modelList []ModelInfo

	// Use user-provided models if available
	if len(userModels) > 0 {
		modelList = userModels
	} else {
		// Get default models for this provider if available
		modelList = GetDefaultModels(name)
		if modelList == nil {
			modelList = []ModelInfo{{ID: model, Name: model, Description: "Default model"}}
		}
	}

	return &OpenAICompatibleProvider{
		BaseProvider: NewBaseProvider(baseURL).WithAPIKey(apiKey),
		name:         name,
		model:        model,
		models:       modelList,
	}
}

// NewOpenAICompatibleProviderWithDefaults creates a new OpenAI-compatible provider with default models
// This is a convenience function for providers that don't need custom model lists
func NewOpenAICompatibleProviderWithDefaults(name, apiKey, baseURL, model string) *OpenAICompatibleProvider {
	return NewOpenAICompatibleProvider(name, apiKey, baseURL, model, nil)
}

// Name returns the provider name
func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

// SetModel sets the current model. If model is not in the list, it will be added.
func (p *OpenAICompatibleProvider) SetModel(model string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if model is already in the list
	for _, m := range p.models {
		if m.ID == model {
			p.model = model
			return nil
		}
	}

	// Model not in list, add it dynamically
	p.models = append(p.models, ModelInfo{
		ID:          model,
		Name:        model,
		Description: "User configured model",
	})
	p.model = model
	return nil
}

// GetModel returns the current model ID.
func (p *OpenAICompatibleProvider) GetModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// ListModels returns the list of supported models.
func (p *OpenAICompatibleProvider) ListModels() []ModelInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]ModelInfo, len(p.models))
	copy(result, p.models)
	return result
}

// SetModels sets the list of supported models.
func (p *OpenAICompatibleProvider) SetModels(models []ModelInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.models = models
}

// SetConvertConfig implements ConvertConfigProvider
func (p *OpenAICompatibleProvider) SetConvertConfig(cfg *ConvertConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ConvertCfg = cfg
}

// 保留字段：透传参数不允许覆盖这些核心请求字段，防止破坏请求完整性。
var reservedRequestKeys = map[string]bool{
	"model": true, "messages": true, "tools": true, "tool_choice": true,
	"stream": true, "stream_options": true,
}

// SetExtraParam 设置一个随每次请求透传给 API 的额外参数。
// 用途示例：
//   - OpenAI 兼容层的显式缓存/隐私参数
//   - provider 特有开关（如 dashscope 的 enable_thinking）
//
// 与缓存相关：对支持显式缓存的网关（如 one-api/new-api 类代理）可透传
// 其约定的缓存开关；对 zhipu 等隐式缓存提供商无需设置任何参数。
func (p *OpenAICompatibleProvider) SetExtraParam(key string, value interface{}) {
	if reservedRequestKeys[key] {
		log.Warnf("[OpenAICompatible] refusing to override reserved request key %q via SetExtraParam", key)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.extraParams == nil {
		p.extraParams = make(map[string]interface{})
	}
	p.extraParams[key] = value
}

// defaultTemperature is applied to outbound requests when neither the core
// request nor extraParams specifies one. Providers' server-side defaults vary
// and some are more prone to sampling degeneration (repetitive "thinking
// loops"); an explicit, standard temperature makes behaviour predictable.
// Users can still override it per-model via extraParams ("temperature").
const defaultTemperature = 0.7

// skipTemperatureDefault reports whether the configured model is known to
// reject non-default temperature values (OpenAI o-series / gpt-5 style
// reasoning models). For those we omit the default and let the provider
// apply its own fixed value.
func (p *OpenAICompatibleProvider) skipTemperatureDefault() bool {
	model := strings.ToLower(p.GetModel())
	if strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		return true
	}
	return strings.Contains(model, "gpt-5")
}

// isDashScope is true when this provider instance talks to the
// DashScope compatible-mode endpoint. It enables a set of behavioural
// tweaks: turn-count trimming, thinking-mode extra params and
// incremental_output streaming.
func (p *OpenAICompatibleProvider) isDashScope() bool {
	return p.name == "dashscope"
}

// applyDashScopeDefaults adds DashScope-only request keys on top of the
// outbound body: enable_thinking + preserve_thinking (non-streaming &
// streaming bodies); for streaming bodies also turns on
// incremental_output so reasoning_content comes through as SSE deltas.
func (p *OpenAICompatibleProvider) applyDashScopeDefaults(reqBody map[string]interface{}, streaming bool) {
	if !p.isDashScope() {
		return
	}
	// Don't clobber user-set values (they may have used SetExtraParam).
	if _, ok := reqBody["enable_thinking"]; !ok {
		reqBody["enable_thinking"] = true
	}
	if _, ok := reqBody["preserve_thinking"]; !ok {
		reqBody["preserve_thinking"] = true
	}
	if streaming {
		if _, ok := reqBody["incremental_output"]; !ok {
			reqBody["incremental_output"] = true
		}
	}
}

// prepMessages runs provider-specific preprocessing over the message list.
// For DashScope it enforces the 140-assistant-message hard cap (see
// TrimDashScopeTurns). Other providers currently pass through unchanged.
func (p *OpenAICompatibleProvider) prepMessages(messages []types.Message) []map[string]interface{} {
	if p.isDashScope() {
		trimmed := TrimDashScopeTurns(messages)
		if len(trimmed) != len(messages) {
			log.Infof("[OpenAICompat:dashscope] trimmed turns: %d -> %d (limit %d)",
				len(messages), len(trimmed), DashScopeTurnMaxSend)
		}
		return ConvertMessagesForProvider(trimmed, p.BaseProvider)
	}
	return ConvertMessagesForProvider(messages, p.BaseProvider)
}

// applyExtraParams 将透传参数合并进请求体（已设置的键不覆盖既有核心字段），
// 并补上默认采样参数（temperature），见 defaultTemperature。
func (p *OpenAICompatibleProvider) applyExtraParams(reqBody map[string]interface{}) {
	p.mu.RLock()
	params := p.extraParams
	p.mu.RUnlock()
	for k, v := range params {
		if reservedRequestKeys[k] {
			continue // 双重保险：合并时同样跳过保留键
		}
		reqBody[k] = v
	}
	// Anti-degeneration default: explicit temperature unless already set
	// (either by the caller or via extraParams) or the model rejects it.
	if _, ok := reqBody["temperature"]; !ok && !p.skipTemperatureDefault() {
		reqBody["temperature"] = defaultTemperature
	}
}

// typedChatResponse is the shared non-streaming response payload used by
// both Chat and ChatWithTools.
type typedChatResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			Role             string `json:"role"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// parseTypedChatResponse decodes a non-streaming /chat/completions JSON
// body into a ChatResponse. Unlike a plain struct unmarshal it also probes
// the raw message map for alternate reasoning-content names
// (thinking_content / reasoning / thinking) which some DashScope
// endpoints, proxy wrappers (one-api/new-api) and thinking-model
// variants emit instead of the canonical "reasoning_content".
func parseTypedChatResponse(body []byte) (*ChatResponse, error) {
	var response typedChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	// Raw decode to probe alternate reasoning names on the 0th choice message.
	var raw struct {
		Choices []struct {
			Message map[string]interface{} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(body, &raw)

	if len(response.Choices) == 0 {
		return &ChatResponse{Content: ""}, nil
	}
	choice := response.Choices[0]

	reasoning := choice.Message.ReasoningContent
	if reasoning == "" && len(raw.Choices) > 0 && raw.Choices[0].Message != nil {
		for _, alt := range []string{"thinking_content", "reasoning", "thinking"} {
			if v, ok := raw.Choices[0].Message[alt].(string); ok && v != "" {
				reasoning = v
				break
			}
		}
	}

	chatResp := &ChatResponse{
		Content:          choice.Message.Content,
		ReasoningContent: reasoning,
		Usage: &Usage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			CacheReadTokens:  response.Usage.PromptTokensDetails.CachedTokens,
		},
	}

	if len(choice.Message.ToolCalls) > 0 {
		chatResp.ToolCalls = make([]types.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			tcType := tc.Type
			if tcType == "" {
				tcType = "function"
			}
			chatResp.ToolCalls = append(chatResp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: tcType,
				Function: types.Function{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}
	return chatResp, nil
}

// Chat implements the Provider interface
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []types.Message) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": p.prepMessages(messages),
	}
	p.applyExtraParams(reqBody)
	p.applyDashScopeDefaults(reqBody, false)

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	respBody, statusCode, err := p.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}

	if statusCode != 200 {
		return nil, p.ParseAPIError(respBody, statusCode)
	}
	return parseTypedChatResponse(respBody)
}

// ChatWithTools implements the ToolCaller interface
func (p *OpenAICompatibleProvider) ChatWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}) (*ChatResponse, error) {
	convertedMessages := p.prepMessages(messages)
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": convertedMessages,
		"tools":    tools,
	}
	p.applyExtraParams(reqBody)
	p.applyDashScopeDefaults(reqBody, false)

	// tool_choice strategy:
	// - Standard providers (OpenAI, Groq, Together, Perplexity) use "auto"
	// - Chinese providers (zhipu, moonshot/kimi, minimax, huoshan/doubao, hunyuan) also use "auto"
	// - DashScope and others may reject tool_choice
	switch p.name {
	case "dashscope", "mimo":
		// These providers don't require tool_choice
	default:
		reqBody["tool_choice"] = "auto"
	}

	if strings.TrimSpace(p.GetModel()) == "" {
		log.Warnf("[ChatWithTools] model parameter is empty for provider=%s", p.name)
	}

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	respBody, statusCode, err := p.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, fmt.Errorf("chat with tools request failed: %w", err)
	}

	if statusCode != 200 {
		parsedErr := p.ParseAPIError(respBody, statusCode)
		return nil, fmt.Errorf("chat with tools request failed: status %d, %w", statusCode, parsedErr)
	}
	return parseTypedChatResponse(respBody)
}

// Stream implements the Streamer interface
func (p *OpenAICompatibleProvider) Stream(ctx context.Context, messages []types.Message, handler StreamHandler) error {
	return p.streamWithContext(ctx, messages, nil, false, handler)
}

// StreamWithTools implements the StreamingToolCaller interface
func (p *OpenAICompatibleProvider) StreamWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}, handler StreamHandler) error {
	return p.streamWithContext(ctx, messages, tools, true, handler)
}

// streamWithContext is the internal streaming implementation
func (p *OpenAICompatibleProvider) streamWithContext(ctx context.Context, messages []types.Message, tools []map[string]interface{}, withTools bool, handler StreamHandler) error {
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": p.prepMessages(messages),
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}
	p.applyExtraParams(reqBody)
	p.applyDashScopeDefaults(reqBody, true)

	if withTools && tools != nil {
		reqBody["tools"] = tools
		// DashScope / mimo do not want tool_choice (same rule as
		// ChatWithTools non-streaming path).
		switch p.name {
		case "dashscope", "mimo":
		default:
			reqBody["tool_choice"] = "auto"
		}
	}

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	resp, err := p.DoStreamRequestWithBreaker(ctx, url, reqBody, headers)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if withTools {
		return ParseStreamResponseWithTools(ctx, resp.Body, handler)
	}
	return ParseStreamResponse(ctx, resp.Body, handler)
}
