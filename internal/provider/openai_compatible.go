package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		case "kimi", "moonshot":
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
		case "doubao", "huoshan":
			baseURL = "https://ark.cn-beijing.volces.com/api/v3"
		case "perplexity":
			baseURL = "https://api.perplexity.ai"
		case "hunyuan":
			baseURL = "https://api.hunyuan.cloud.tencent.com/v1"
		case "mimo":
			baseURL = "https://api.xiaomimimo.com/v1"
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
		case "kimi", "moonshot":
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
		case "doubao", "huoshan":
			model = "doubao-1.5-pro-32k"
		case "perplexity":
			model = "sonar-pro"
		case "hunyuan":
			model = "hunyuan-turbos-latest"
		case "mimo":
			model = "mimo-v2-flash"
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

// applyExtraParams 将透传参数合并进请求体（已设置的键不覆盖既有核心字段）。
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
}

// Chat implements the Provider interface
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []types.Message) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": ConvertMessagesForProvider(messages, p.BaseProvider),
	}
	p.applyExtraParams(reqBody)

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	respBody, statusCode, err := p.DoRequest(ctx, "POST", url, reqBody, headers)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}

	if statusCode != 200 {
		return nil, p.ParseAPIError(respBody, statusCode)
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Role      string `json:"role"`
				ToolCalls []struct {
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

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &ChatResponse{Content: ""}, nil
	}

	choice := response.Choices[0]

	chatResp := &ChatResponse{
		Content: choice.Message.Content,
		// 回填 usage：之前的实现丢弃了该信息，导致缓存命中
		// 与真实 token 消耗不可见，成本统计失真。
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
			// Ensure type is always "function"
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

// ChatWithTools implements the ToolCaller interface
func (p *OpenAICompatibleProvider) ChatWithTools(ctx context.Context, messages []types.Message, tools []map[string]interface{}) (*ChatResponse, error) {
	convertedMessages := ConvertMessagesForProvider(messages, p.BaseProvider)
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": convertedMessages,
		"tools":    tools,
	}
	p.applyExtraParams(reqBody)

	// tool_choice strategy:
	// - Standard providers (OpenAI, Groq, Together, Perplexity) use "auto"
	// - Chinese providers (zhipu, kimi, minimax, doubao, hunyuan) also use "auto"
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

	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Role      string `json:"role"`
				ToolCalls []struct {
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

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &ChatResponse{Content: ""}, nil
	}

	choice := response.Choices[0]

	chatResp := &ChatResponse{
		Content: choice.Message.Content,
		// ChatWithTools 路径同样回填 usage + 缓存命中
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
			// Ensure type is always "function"
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
		"messages": ConvertMessagesForProvider(messages, p.BaseProvider),
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}
	p.applyExtraParams(reqBody)

	if withTools && tools != nil {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
	}

	url := p.BaseURL + "/chat/completions"

	headers := map[string]string{}
	resp, err := p.DoStreamRequest(ctx, url, reqBody, headers)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stream API returned status %d: %s", resp.StatusCode, string(body))
	}

	if withTools {
		return ParseStreamResponseWithTools(ctx, resp.Body, handler)
	}
	return ParseStreamResponse(ctx, resp.Body, handler)
}
