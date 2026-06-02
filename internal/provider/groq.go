package provider

// GroqProvider implements the Groq API (fast inference) using OpenAI-compatible format.
// Groq uses OpenAI-compatible format but with different base URL and specific models.
type GroqProvider struct {
	*OpenAICompatibleProvider
}

// NewGroqProvider creates a new Groq provider
func NewGroqProvider(apiKey, baseURL, model string) *GroqProvider {
	if model == "" {
		model = "mixtral-8x7b-32768" // Default to fast model
	}
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}
	return &GroqProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("groq", apiKey, baseURL, model),
	}
}

func (p *GroqProvider) Name() string {
	return "groq"
}

// GetCapabilities returns the capabilities of Groq
func (p *GroqProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     false,
		Vision:         false,
	}
}
