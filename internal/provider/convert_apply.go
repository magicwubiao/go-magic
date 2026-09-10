package provider

// ApplyConvertConfig installs cfg on a provider instance so the request path
// (prepMessagesWithConfig for OpenAI-compatible providers, p.ConvertConfig for
// anthropic/gemini/cohere/...) picks it up on the very next request.
//
// Providers embed the base implementation differently, hence the type switch;
// the SetConvertConfig interface fallback covers the rest (anthropic, gemini,
// cohere, ...). A provider that stores its config on the shared *BaseProvider
// is updated in place, so already-built agents — which hold a pointer to that
// same provider instance — see the new value without being rebuilt.
//
// This lives in the provider package (not internal/agent) because the server
// refreshes the config when the user edits "image input (vision)" in the model
// settings page, and that path must not depend on agent creation: the web UI
// reuses the cached per-session agent, so an agent-only write meant the change
// stayed invisible until the next process restart.
//
// Returns false when the provider has nowhere to store the config (nil
// provider or unknown implementation).
func ApplyConvertConfig(p Provider, cfg *ConvertConfig) bool {
	if p == nil {
		return false
	}
	switch v := p.(type) {
	case *OpenAICompatibleProvider:
		if v.BaseProvider != nil {
			v.BaseProvider.WithConvertConfig(cfg)
			return true
		}
	case *DeepSeekProvider:
		if v.OpenAICompatibleProvider != nil && v.OpenAICompatibleProvider.BaseProvider != nil {
			v.OpenAICompatibleProvider.BaseProvider.WithConvertConfig(cfg)
			return true
		}
	case *DashScopeProvider:
		if v.BaseProvider != nil {
			v.BaseProvider.WithConvertConfig(cfg)
			return true
		}
	}
	if ccp, ok := p.(interface{ SetConvertConfig(*ConvertConfig) }); ok {
		ccp.SetConvertConfig(cfg)
		return true
	}
	return false
}

// GetConvertConfig returns the conversion config currently installed on a
// provider, if it exposes one. Read-only counterpart of ApplyConvertConfig
// (used by tests and by callers that need to know the effective vision policy).
func GetConvertConfig(p Provider) *ConvertConfig {
	if p == nil {
		return nil
	}
	switch v := p.(type) {
	case *OpenAICompatibleProvider:
		if v.BaseProvider != nil {
			return v.BaseProvider.ConvertCfg
		}
	case *DeepSeekProvider:
		if v.OpenAICompatibleProvider != nil && v.OpenAICompatibleProvider.BaseProvider != nil {
			return v.OpenAICompatibleProvider.BaseProvider.ConvertCfg
		}
	case *DashScopeProvider:
		if v.BaseProvider != nil {
			return v.BaseProvider.ConvertCfg
		}
	}
	if g, ok := p.(interface{ GetConvertConfig() *ConvertConfig }); ok {
		return g.GetConvertConfig()
	}
	return nil
}
