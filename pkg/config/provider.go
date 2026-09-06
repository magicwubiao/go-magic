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
	case "huoshan", "doubao": // doubao 为旧配置兼容别名（豆包经火山引擎 Ark 提供）
		return provider.NewHuoshanProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "wenxin":
		// Wenxin requires both apiKey and secretKey; use BaseURL field for secretKey
		return provider.NewWenxinProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "moonshot", "kimi": // kimi 为旧配置兼容别名（Kimi 即 Moonshot 月之暗面）
		return provider.NewMoonshotProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "mimo":
		return provider.NewMiMoProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "hunyuan":
		return provider.NewHunyuanProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "longcat":
		return provider.NewLongCatProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
	case "meta":
		return provider.NewMetaProvider(provCfg.APIKey, provCfg.BaseURL, model), nil
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
			Description:  "DeepSeek V4 - 高性价比",
			Models:       []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "openai",
			DisplayName:  "OpenAI",
			Description:  "GPT-5.6 系列",
			Models:       []string{"gpt-5.6", "gpt-5.6-terra", "gpt-5.6-luna"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "anthropic",
			DisplayName:  "Anthropic",
			Description:  "Claude 5 系列 - 强推理能力",
			Models:       []string{"claude-fable-5-1", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5"},
			NeedsAPIKey:  true,
			NeedsBaseURL: false,
		},
		{
			Name:         "dashscope",
			DisplayName:  "DashScope (通义千问)",
			Description:  "阿里云通义千问大模型",
			Models:       []string{"qwen3.8-max", "qwen3.7-plus", "qwen3.7-flash"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "minimax",
			DisplayName:  "MiniMax",
			Description:  "MiniMax 大模型",
			Models:       []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.5"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "ollama",
			DisplayName:  "Ollama",
			Description:  "本地部署的开源大模型",
			Models:       []string{"qwen3.8", "gpt-oss", "deepseek-r1", "gemma4"},
			NeedsAPIKey:  false,
			NeedsBaseURL: true,
		},
		{
			Name:         "openrouter",
			DisplayName:  "OpenRouter",
			Description:  "统一 API 网关，支持多种模型",
			Models:       []string{"openai/gpt-5.6", "anthropic/claude-sonnet-5"},
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
			Models:       []string{"glm-5.3", "glm-5.3-flash", "glm-5.2"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "gemini",
			DisplayName:  "Google Gemini",
			Description:  "Google Gemini 系列模型",
			Models:       []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.1-pro"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "groq",
			DisplayName:  "Groq",
			Description:  "超高速 LLM 推理平台",
			Models:       []string{"llama-3.3-70b-versatile", "openai/gpt-oss-120b", "llama-3.1-8b-instant"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "together",
			DisplayName:  "Together AI",
			Description:  "开源模型托管平台",
			Models:       []string{"deepseek-ai/DeepSeek-V4-Pro", "meta-llama/Llama-4-Maverick-17B-128E-Instruct", "Qwen/Qwen3.8-2.4T-A95B"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "mistral",
			DisplayName:  "Mistral AI",
			Description:  "Mistral 系列开源模型",
			Models:       []string{"mistral-large-latest", "mistral-medium-3-5", "mistral-small-2603"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "cohere",
			DisplayName:  "Cohere",
			Description:  "Cohere Command 系列模型",
			Models:       []string{"command-a-plus-05-2026", "command-a-reasoning-08-2025", "command-a-03-2025"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "perplexity",
			DisplayName:  "Perplexity",
			Description:  "Perplexity 在线搜索增强模型",
			Models:       []string{"sonar-pro", "sonar-reasoning-pro", "sonar-deep-research", "sonar"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "huoshan",
			DisplayName:  "Volcengine (Doubao)",
			Description:  "ByteDance Doubao models via Volcengine Ark",
			Models:       []string{"doubao-seed-2.1-pro", "doubao-seed-2.1-turbo", "doubao-seed-2.0-lite"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:         "wenxin",
			DisplayName:  "文心一言 (Wenxin)",
			Description:  "百度 ERNIE 系列大模型",
			Models:       []string{"ernie-5.1", "ernie-5.0", "ernie-4.5-turbo-128k"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true, // BaseURL field is used for secretKey
		},
		{
			Name:         "moonshot",
			DisplayName:  "Moonshot (Kimi)",
			Description:  "月之暗面 Kimi 大模型",
			Models:       []string{"kimi-k3", "kimi-k2.6", "kimi-k2.7-code"},
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
			Models:       []string{"hy3", "hy-2.0-think", "hunyuan-turbos"},
			NeedsAPIKey:  true,
			NeedsBaseURL: true,
		},
		{
			Name:        "longcat",
			DisplayName: "LongCat (美团龙猫)",
			Description: "美团 LongCat 大模型",
			Models:      []string{"LongCat-2.0-Preview"},
			NeedsAPIKey: true,
		},
		{
			Name:        "meta",
			DisplayName: "Meta Model API (Muse Spark)",
			Description: "Meta 超级智能实验室 Muse Spark 多模态推理模型",
			Models:      []string{"muse-spark-1.3", "muse-spark-1.2", "muse-spark-1.1"},
			NeedsAPIKey: true,
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
