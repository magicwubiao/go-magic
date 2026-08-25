package config

import (
	"fmt"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// toModelInfo converts a slice of model IDs to ModelInfo slice
func toModelInfo(modelIDs []string) []provider.ModelInfo {
	if len(modelIDs) == 0 {
		return nil
	}
	models := make([]provider.ModelInfo, len(modelIDs))
	for i, id := range modelIDs {
		models[i] = provider.ModelInfo{
			ID:          id,
			Name:        id,
			Description: "User configured model",
		}
	}
	return models
}

// CreateProvider creates a provider.Provider from the given Config.
// It uses cfg.Provider as the provider name and looks up cfg.Providers[cfg.Provider]
// for API key, base URL, and model overrides.
// Returns an error if the provider is unknown or not configured.
func CreateProvider(cfg *Config) (provider.Provider, error) {
	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not configured", cfg.Provider)
	}
	return CreateProviderFor(cfg.Provider, provCfg)
}

// CreateProviderFor creates a provider from an explicit name + config pair.
// Used by Bot Mode where each bot can pin its own provider/model.
func CreateProviderFor(name string, provCfg ProviderConfig) (provider.Provider, error) {
	// Get current model: Models[0] > Model field
	model := provCfg.GetCurrentModel()
	if model == "" {
		return nil, fmt.Errorf("no model configured for provider %s", name)
	}

	// Convert user-configured models to ModelInfo
	userModels := toModelInfo(provCfg.Models)

	switch name {
	case "openai":
		return provider.NewOpenAIProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "anthropic":
		return provider.NewAnthropicProvider(provCfg.APIKey, model), nil
	case "deepseek":
		return provider.NewDeepSeekProvider(provCfg.APIKey, provCfg.BaseURL, model, userModels), nil
	case "dashscope":
		return provider.NewDashScopeProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "kimi":
		return provider.NewKimiProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "minimax":
		return provider.NewMiniMaxProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "ollama":
		return provider.NewOllamaProvider(provCfg.BaseURL, model), nil
	case "openrouter":
		return provider.NewOpenRouterProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "vllm":
		return provider.NewVLLMProvider(provCfg.BaseURL, model), nil
	case "zhipu":
		return provider.NewZhipuProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "gemini":
		return provider.NewGeminiProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "groq":
		return provider.NewGroqProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "together":
		return provider.NewTogetherProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "mistral":
		return provider.NewMistralProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "cohere":
		return provider.NewCohereProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "perplexity":
		return provider.NewPerplexityProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "doubao":
		return provider.NewDoubaoProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "wenxin":
		// Wenxin requires both apiKey and secretKey; use BaseURL field for secretKey
		return provider.NewWenxinProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "moonshot":
		return provider.NewMoonshotProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "mimo":
		return provider.NewMiMoProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "hunyuan":
		return provider.NewHunyuanProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "custom":
		return provider.NewOpenAICompatibleProvider("custom", provCfg.APIKey, provCfg.BaseURL, model, userModels), nil
	default:
		// For unknown providers, try to use OpenAI-compatible provider with user models
		return provider.NewOpenAICompatibleProvider(name, provCfg.APIKey, provCfg.BaseURL, model, userModels), nil
	}
}

// ProviderInfo contains metadata about a supported provider.
type ProviderInfo struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	Models       []string `json:"models"`
	NeedsAPIKey  bool     `json:"needs_api_key"`
	NeedsBaseURL bool     `json:"needs_base_url"`
}

// ListProviders returns all supported providers with their metadata.
func ListProviders() []ProviderInfo {
	return []ProviderInfo{
		{
			Name:         "deepseek",
			DisplayName:  "DeepSeek",
			Description:  "DeepSeek V3/R1 - 高性价比",
			Models:       []string{"deepseek-chat", "deepseek-reasoner"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "openai",
			DisplayName:  "OpenAI",
			Description:  "GPT-4o, GPT-4o-mini",
			Models:       []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "anthropic",
			DisplayName:  "Anthropic",
			Description:  "Claude 4 Sonnet/Opus - 强推理能力",
			Models:       []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-3-5-sonnet-20241022"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "dashscope",
			DisplayName:  "DashScope (通义千问)",
			Description:  "阿里云通义千问大模型",
			Models:       []string{"qwen-turbo", "qwen-plus", "qwen-max"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "kimi",
			DisplayName:  "Kimi (Moonshot AI)",
			Description:  "月之暗面 Kimi 大模型",
			Models:       []string{"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "minimax",
			DisplayName:  "MiniMax",
			Description:  "MiniMax 大模型",
			Models:       []string{"abab6.5s-chat", "abab6.5-chat"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "ollama",
			DisplayName:  "Ollama",
			Description:  "本地部署的开源大模型",
			Models:       []string{"llama3", "codellama", "mistral", "qwen2"},
			NeedsAPIKey:  false,
			NeedsBaseURL: true,
		},
		{
			Name:         "openrouter",
			DisplayName:  "OpenRouter",
			Description:  "统一 API 网关，支持多种模型",
			Models:       []string{"openai/gpt-4o", "anthropic/claude-3.5-sonnet"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "vllm",
			DisplayName:  "vLLM",
			Description:  "本地部署的高性能推理引擎",
			Models:       []string{"default"},
			NeedsAPIKey:  false,
			NeedsBaseURL: true,
		},
		{
			Name:         "zhipu",
			DisplayName:  "智谱 AI (Zhipu)",
			Description:  "智谱 GLM 系列大模型",
			Models:       []string{"glm-4", "glm-4-plus", "glm-4-flash"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "gemini",
			DisplayName:  "Google Gemini",
			Description:  "Google Gemini 系列模型",
			Models:       []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "groq",
			DisplayName:  "Groq",
			Description:  "超高速 LLM 推理平台",
			Models:       []string{"llama-3.3-70b-versatile", "mixtral-8x7b-32768"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "together",
			DisplayName:  "Together AI",
			Description:  "开源模型托管平台",
			Models:       []string{"meta-llama/Llama-3-70b-chat-hf"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "mistral",
			DisplayName:  "Mistral AI",
			Description:  "Mistral 系列开源模型",
			Models:       []string{"mistral-large-latest", "mistral-small-latest", "open-mistral-nemo"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "cohere",
			DisplayName:  "Cohere",
			Description:  "Cohere Command 系列模型",
			Models:       []string{"command-r-plus", "command-r", "command"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "perplexity",
			DisplayName:  "Perplexity",
			Description:  "Perplexity 在线搜索增强模型",
			Models:       []string{"llama-3.1-sonar-large-128k-online", "llama-3.1-sonar-small-128k-online"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "doubao",
			DisplayName:  "豆包 (Doubao)",
			Description:  "字节跳动豆包大模型",
			Models:       []string{"doubao-pro-32k", "doubao-pro-128k"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "wenxin",
			DisplayName:  "文心一言 (Wenxin)",
			Description:  "百度 ERNIE 系列大模型",
			Models:       []string{"ernie-4.0-8k-latest", "ernie-4.0-turbo-8k", "ernie-speed-128k"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true, // BaseURL field is used for secretKey
		},
		{
			Name:         "moonshot",
			DisplayName:  "Moonshot (Kimi)",
			Description:  "月之暗面 Moonshot 大模型",
			Models:       []string{"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "mimo",
			DisplayName:  "MiMo",
			Description:  "MiMo 大模型",
			Models:       []string{"default"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "hunyuan",
			DisplayName:  "混元 (Hunyuan)",
			Description:  "腾讯混元大模型",
			Models:       []string{"hunyuan-pro", "hunyuan-standard"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "custom",
			DisplayName:  "Custom (OpenAI Compatible)",
			Description:  "自定义 OpenAI 兼容 API",
			Models:       []string{"default"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
	}
}
