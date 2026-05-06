package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Provider is a type alias for the provider.Provider interface
type Provider = provider.Provider

// UnifiedProvider wraps multiple providers with unified interface
type UnifiedProvider struct {
	primary   Provider
	fallbacks []Provider
	name      string
}

// NewUnifiedProvider creates a provider that supports fallback
func NewUnifiedProvider(primary Provider, fallbacks ...Provider) *UnifiedProvider {
	name := primary.Name()
	if len(fallbacks) > 0 {
		name = fmt.Sprintf("%s+fallback(%d)", name, len(fallbacks))
	}
	return &UnifiedProvider{
		primary:   primary,
		fallbacks: fallbacks,
		name:      name,
	}
}

func (p *UnifiedProvider) Name() string {
	return p.name
}

func (p *UnifiedProvider) Chat(ctx context.Context, messages []types.Message) (*types.ChatResponse, error) {
	candidates := p.buildCandidates()

	run := func(ctx context.Context, provName, model string) (*LLMResponse, error) {
		prov := p.findProvider(provName)
		if prov == nil {
			return nil, fmt.Errorf("provider not found: %s", provName)
		}

		resp, err := prov.Chat(ctx, messages)
		if err != nil {
			return nil, err
		}

		return &LLMResponse{
			Content:     resp.Content,
			RawResponse: resp,
		}, nil
	}

	result, err := fallbackExecute(ctx, candidates, run)
	if err != nil {
		return nil, err
	}

	// Safe type assertion with error handling
	if chatResp, ok := result.Response.RawResponse.(*provider.ChatResponse); ok {
		return &types.ChatResponse{
			Content:   chatResp.Content,
			ToolCalls: chatResp.ToolCalls,
		}, nil
	}
	return nil, fmt.Errorf("unexpected response type: %T", result.Response.RawResponse)
}

func (p *UnifiedProvider) findProvider(name string) Provider {
	if p.primary.Name() == name {
		return p.primary
	}
	for _, fb := range p.fallbacks {
		if fb.Name() == name {
			return fb
		}
	}
	return p.primary
}

func (p *UnifiedProvider) buildCandidates() []FallbackCandidate {
	var candidates []FallbackCandidate

	// Primary first
	candidates = append(candidates, FallbackCandidate{
		Provider: p.primary.Name(),
		Model:    "",
	})

	// Then fallbacks
	for _, fb := range p.fallbacks {
		candidates = append(candidates, FallbackCandidate{
			Provider: fb.Name(),
			Model:    "",
		})
	}

	return candidates
}

// ExtractProviderModel extracts provider and model from "provider/model" format
func ExtractProviderModel(modelRef string) (provider, model string) {
	modelRef = strings.TrimSpace(modelRef)
	parts := strings.SplitN(modelRef, "/", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "openai", modelRef
}
