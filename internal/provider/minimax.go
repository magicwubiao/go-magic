package provider

import (
	
)

// MiniMaxProvider MiniMax (兼容OpenAI格式)
type MiniMaxProvider struct {
	*OpenAICompatibleProvider
}

// NewMiniMaxProvider creates a new MiniMax provider
func NewMiniMaxProvider(apiKey, model string) *MiniMaxProvider {
	if model == "" {
		model = "abab6-chat"
	}
	return &MiniMaxProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider("minimax", apiKey, "https://api.minimax.chat/v1", model),
	}
}

func (p *MiniMaxProvider) Name() string {
	return "minimax"
}

// GetCapabilities returns the capabilities of MiniMax
func (p *MiniMaxProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      false,
		Vision:          false,
	}
}
