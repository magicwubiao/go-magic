package provider

import (
	"context"
	"fmt"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// Message is an alias for types.Message
type Message = types.Message

// ChatResponse represents a chat response with optional usage info
type ChatResponse struct {
	Content   string           `json:"content"`
	ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
	Usage     *Usage           `json:"usage,omitempty"`
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

// Registry manages provider instances.
type Registry struct {
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
	r.providers[p.Name()] = p
}

// Get returns a provider by name
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

// List returns all registered provider names
func (r *Registry) List() []string {
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
