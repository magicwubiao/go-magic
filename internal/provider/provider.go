package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/magicwubiao/go-magic/pkg/catalog"
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
	// Vision declares whether this model accepts image_url parts. For IDs
	// present in the curated registry this flag is authoritative: vision
	// detection (ModelSupportsVision) checks it BEFORE falling back to
	// name-pattern heuristics, which lag new releases and occasionally
	// misfire (e.g. "o3" matching the text-only o3-mini).
	Vision bool `json:"vision"`
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

// GetDefaultModels returns the default models for a provider. Data derives
// from the single catalog source (pkg/catalog) — do NOT add model entries
// here; edit pkg/catalog/catalog.go instead. Unknown providers return nil.
func GetDefaultModels(providerName string) []ModelInfo {
	models := catalog.Models(providerName)
	if len(models) == 0 {
		return nil
	}
	out := make([]ModelInfo, len(models))
	for i, m := range models {
		out[i] = ModelInfo{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			ContextLen:  m.ContextLen,
			// Vision 三态收敛为 bool：nil（未知）按 false 透出。权威判定
			// 由 modelRegistryVision 直接查 catalog 的 *bool，不经过这里。
			Vision: m.Vision != nil && *m.Vision,
		}
	}
	return out
}
