package config

import (
	"fmt"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/catalog"
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
	prov, err := createProviderForName(name, provCfg)
	if err != nil {
		return nil, err
	}
	// Wire user-configured transparent request params (e.g. reasoning
	// flags on gateways that hide thinking behind a request key).
	if len(provCfg.ExtraParams) > 0 {
		if sp, ok := prov.(interface {
			SetExtraParam(key string, value interface{})
		}); ok {
			for k, v := range provCfg.ExtraParams {
				sp.SetExtraParam(k, v)
			}
		}
	}
	return prov, nil
}

func createProviderForName(name string, provCfg ProviderConfig) (provider.Provider, error) {
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
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Models      []string `json:"models"`
	// BaseURL is the provider's official API endpoint (matching each
	// provider constructor's built-in fallback). It seeds the Web UI's
	// add-provider form; users can override it per config. Empty for
	// custom providers where the endpoint is user-supplied.
	BaseURL     string `json:"base_url,omitempty"`
	NeedsAPIKey bool   `json:"needs_api_key"`
	// NeedsBaseURL reports whether the form should surface a base URL
	// field for this provider (wenxin reuses it for secretKey).
	NeedsBaseURL bool `json:"needs_base_url"`
}

// ListProviders returns all supported providers with their metadata.
// Data derives from the single catalog source (pkg/catalog) — do NOT add
// provider/model entries here; edit pkg/catalog/catalog.go instead.
func ListProviders() []ProviderInfo {
	all := catalog.All()
	out := make([]ProviderInfo, 0, len(all))
	for _, p := range all {
		out = append(out, ProviderInfo{
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			Description:  p.Description,
			Models:       catalog.ModelIDs(p.Name),
			BaseURL:      p.BaseURL,
			NeedsAPIKey:  p.NeedsAPIKey,
			NeedsBaseURL: p.NeedsBaseURL,
		})
	}
	return out
}
