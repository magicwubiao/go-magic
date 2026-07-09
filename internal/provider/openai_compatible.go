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
	name   string
	model  string
	models []ModelInfo
	mu     sync.RWMutex
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
		case "openai", "custom":
			model = "gpt-4o-mini"
		case "anthropic":
			model = "claude-3-5-sonnet-20241022"
		case "deepseek":
			model = "deepseek-chat"
		case "kimi", "moonshot":
			model = "moonshot-v1-8k"
		case "zhipu":
			model = "glm-4"
		case "minimax":
			model = "abab6.5s-chat"
		case "groq":
			model = "llama3-70b-8192"
		case "openrouter":
			model = "gpt-4o-mini"
		case "mistral":
			model = "mistral-small-latest"
		case "dashscope":
			model = "qwen-plus"
		case "doubao", "huoshan":
			model = "doubao-pro-32k"
		case "perplexity":
			model = "llama-3.1-sonar-small-128k-online"
		case "hunyuan":
			model = "hunyuan-lite"
		case "mimo":
			model = "moa-v1"
		case "ollama":
			model = "llama3.2"
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

// Chat implements the Provider interface
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []types.Message) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":    p.GetModel(),
		"messages": ConvertMessagesForProvider(messages, p.BaseProvider),
	}

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
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
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
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
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
