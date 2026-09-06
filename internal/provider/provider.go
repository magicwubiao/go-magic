package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// Message is an alias for types.Message
type Message = types.Message

// ChatResponse represents a chat response with optional usage info
type ChatResponse struct {
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
	Usage            *Usage           `json:"usage,omitempty"`
}

// ModelInfo represents information about a supported model
type ModelInfo struct {
	ID          string `json:"id"`          // Model ID used in API calls
	Name        string `json:"name"`        // Human-readable name
	Description string `json:"description"` // Model description
	ContextLen  int    `json:"context_len"` // Context window size (0 = unknown)
}

// Modeler is an optional interface for providers that support multiple models.
// Providers implementing this interface allow dynamic model switching.
type Modeler interface {
	// SetModel sets the current model. Returns error if model is not supported.
	SetModel(model string) error
	// GetModel returns the current model ID.
	GetModel() string
	// ListModels returns the list of supported models.
	ListModels() []ModelInfo
}

// Provider is the interface for LLM providers.
type Provider interface {
	Chat(ctx context.Context, messages []Message) (*ChatResponse, error)
	Name() string
}

// ToolCaller is an optional interface for providers that support tool calling.
type ToolCaller interface {
	ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error)
}

// Streamer is an optional interface for providers that support streaming.
type Streamer interface {
	Stream(ctx context.Context, messages []Message, handler StreamHandler) error
}

// StreamingToolCaller is for providers that support both streaming and tool calling
type StreamingToolCaller interface {
	StreamWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error
}

// CapableProvider is an optional interface for providers that declare their capabilities
type CapableProvider interface {
	GetCapabilities() *Capabilities
}

// ConvertConfigProvider is an optional interface for providers that support file conversion config
type ConvertConfigProvider interface {
	SetConvertConfig(cfg *ConvertConfig)
}

// Registry manages provider instances.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register registers a provider in the registry
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns a provider by name
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

// List returns all registered provider names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// GetCapabilities returns the capabilities of a provider, or default if not specified
func GetCapabilities(p Provider) *Capabilities {
	if cp, ok := p.(CapableProvider); ok {
		return cp.GetCapabilities()
	}
	return DefaultCapabilities()
}

// GetModeler returns the Modeler interface if supported by the provider
func GetModeler(p Provider) (Modeler, bool) {
	if m, ok := p.(Modeler); ok {
		return m, true
	}
	return nil, false
}

// IsModelSupported checks if a model is supported by the provider
func IsModelSupported(p Provider, model string) bool {
	m, ok := GetModeler(p)
	if !ok {
		return false
	}
	for _, mi := range m.ListModels() {
		if mi.ID == model {
			return true
		}
	}
	return false
}

// GetDefaultModels returns the default models for known providers
func GetDefaultModels(providerName string) []ModelInfo {
	defaults := map[string][]ModelInfo{
		"openai": {
			{ID: "gpt-5.6", Name: "GPT-5.6 Sol", Description: "最新旗舰模型", ContextLen: 1050000},
			{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Description: "均衡模型", ContextLen: 1050000},
			{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Description: "最快最便宜", ContextLen: 1050000},
		},
		"deepseek": {
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Description: "高性价比主力模型", ContextLen: 1000000},
			{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Description: "旗舰推理模型", ContextLen: 1000000},
		},
		"anthropic": {
			{ID: "claude-fable-5-1", Name: "Claude Fable 5.1", Description: "最强推理旗舰", ContextLen: 1000000},
			{ID: "claude-opus-5", Name: "Claude Opus 5", Description: "企业级智能体编码", ContextLen: 1000000},
			{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Description: "速度与智能均衡", ContextLen: 1000000},
			{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", Description: "最快模型", ContextLen: 200000},
		},
		"gemini": {
			{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Description: "最新主力模型", ContextLen: 1000000},
			{ID: "gemini-3.7-flash", Name: "Gemini 3.7 Flash", Description: "高性价比", ContextLen: 1000000},
			{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", Description: "最强推理", ContextLen: 1000000},
		},
		"ollama": {
			{ID: "qwen3.8", Name: "Qwen 3.8", Description: "阿里开源模型", ContextLen: 131072},
			{ID: "gpt-oss", Name: "GPT-OSS", Description: "OpenAI 开源模型", ContextLen: 131072},
			{ID: "deepseek-r1", Name: "DeepSeek R1", Description: "推理模型", ContextLen: 131072},
		},
		"longcat": {
			{ID: "LongCat-2.0-Preview", Name: "LongCat 2.0 Preview", Description: "旗舰推理与 Agent 模型", ContextLen: 1000000},
		},
		"meta": {
			{ID: "muse-spark-1.3", Name: "Muse Spark 1.3", Description: "旗舰多模态推理模型", ContextLen: 1048576},
			{ID: "muse-spark-1.2", Name: "Muse Spark 1.2", Description: "多模态推理模型", ContextLen: 1048576},
			{ID: "muse-spark-1.1", Name: "Muse Spark 1.1", Description: "首发推理模型", ContextLen: 1048576},
		},
	}

	if models, ok := defaults[providerName]; ok {
		return models
	}
	return nil
}
