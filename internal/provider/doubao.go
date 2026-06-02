package provider

// DoubaoProvider implements the Doubao (Volcengine) API using OpenAI-compatible format.
type DoubaoProvider struct {
	*OpenAICompatibleProvider
	endpointID string // Optional endpoint ID for Volcengine
}

// NewDoubaoProvider creates a new Doubao provider
func NewDoubaoProvider(apiKey, baseURL, model string) *DoubaoProvider {
	if model == "" {
		model = "doubao-pro-32k" // Default model
	}
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}
	return &DoubaoProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("doubao", apiKey, baseURL, model),
	}
}

// NewDoubaoProviderWithEndpoint creates a Doubao provider with custom endpoint
func NewDoubaoProviderWithEndpoint(apiKey, endpointID string) *DoubaoProvider {
	baseURL := "https://ark.cn-beijing.volces.com/api/v3"
	return &DoubaoProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("doubao", apiKey, baseURL, endpointID),
		endpointID:               endpointID,
	}
}

func (p *DoubaoProvider) Name() string {
	return "doubao"
}

// GetCapabilities returns the capabilities of Doubao
func (p *DoubaoProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}
