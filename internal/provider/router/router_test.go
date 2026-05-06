package router

import (
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestNewProviderRouter tests creating a new provider router
func TestNewProviderRouter(t *testing.T) {
	router := NewProviderRouter()
	if router == nil {
		t.Fatal("expected non-nil router")
	}
	if router.providers == nil {
		t.Error("expected providers map to be initialized")
	}
	if router.cooldown == nil {
		t.Error("expected cooldown tracker to be initialized")
	}
}

// TestProviderRouterRegister tests registering a provider creator
func TestProviderRouterRegister(t *testing.T) {
	router := NewProviderRouter()

	creator := func(config ProviderConfig) (provider.Provider, error) {
		return &MockProvider{name: config.ProviderName}, nil
	}

	router.Register("test", creator)

	if len(router.providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(router.providers))
	}
}

// TestExtractProviderModel tests extracting provider and model
func TestExtractProviderModel(t *testing.T) {
	tests := []struct {
		input            string
		expectedProvider string
		expectedModel    string
	}{
		{"openai:gpt-4", "openai", "gpt-4"},
		{"anthropic:claude-3-opus", "anthropic", "claude-3-opus"},
		{"deepseek:chat", "deepseek", "chat"},
		{"gpt-4", "openai", "gpt-4"},
		{"", "openai", ""},
	}

	for _, tt := range tests {
		prov, model := ExtractProviderModel(tt.input)
		if prov != tt.expectedProvider {
			t.Errorf("expected provider '%s', got '%s' for input '%s'", tt.expectedProvider, prov, tt.input)
		}
		if model != tt.expectedModel {
			t.Errorf("expected model '%s', got '%s' for input '%s'", tt.expectedModel, model, tt.input)
		}
	}
}

// TestProviderRouterExecuteFallback tests fallback execution
func TestProviderRouterExecuteFallback(t *testing.T) {
	router := NewProviderRouter()

	candidates := []FallbackCandidate{
		{Provider: "openai", Model: "gpt-4"},
		{Provider: "anthropic", Model: "claude-3"},
	}

	var callCount int
	runFn := func(ctx interface{}, prov, model string) (*LLMResponse, error) {
		callCount++
		return &LLMResponse{Content: "response from " + prov}, nil
	}

	result, err := router.ExecuteFallback(nil, candidates, runFn)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Content != "response from openai" {
		t.Errorf("expected 'response from openai', got '%s'", result.Content)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// TestProviderRouterExecuteFallbackWithFailure tests fallback on failure
func TestProviderRouterExecuteFallbackWithFailure(t *testing.T) {
	router := NewProviderRouter()

	candidates := []FallbackCandidate{
		{Provider: "openai", Model: "gpt-4"},
		{Provider: "anthropic", Model: "claude-3"},
	}

	var callCount int
	runFn := func(ctx interface{}, prov, model string) (*LLMResponse, error) {
		callCount++
		if prov == "openai" {
			return nil, &mockError{msg: "openai failed"}
		}
		return &LLMResponse{Content: "response from " + prov}, nil
	}

	result, err := router.ExecuteFallback(nil, candidates, runFn)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Content != "response from anthropic" {
		t.Errorf("expected 'response from anthropic', got '%s'", result.Content)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(result.Attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", len(result.Attempts))
	}
}

// MockProvider is a mock implementation of Provider for testing
type MockProvider struct {
	name string
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Chat(ctx interface{}, messages interface{}) (interface{}, error) {
	return &provider.ChatResponse{Content: "mock response"}, nil
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// TestCooldownTracker tests cooldown tracking
func TestCooldownTracker(t *testing.T) {
	tracker := NewCooldownTracker()

	// Add provider to cooldown
	tracker.SetCooldown("test_provider", 5*time.Second)

	if !tracker.IsInCooldown("test_provider") {
		t.Error("expected provider to be in cooldown")
	}

	// Remove from cooldown
	tracker.ClearCooldown("test_provider")

	if tracker.IsInCooldown("test_provider") {
		t.Error("expected provider to not be in cooldown")
	}
}

// TestCooldownTrackerMultipleProviders tests cooldown with multiple providers
func TestCooldownTrackerMultipleProviders(t *testing.T) {
	tracker := NewCooldownTracker()

	tracker.SetCooldown("provider1", 1*time.Second)
	tracker.SetCooldown("provider2", 5*time.Second)

	time.Sleep(2 * time.Second)

	if tracker.IsInCooldown("provider1") {
		t.Error("expected provider1 to be out of cooldown")
	}
	if !tracker.IsInCooldown("provider2") {
		t.Error("expected provider2 to still be in cooldown")
	}
}

// TestFallbackCandidate tests FallbackCandidate struct
func TestFallbackCandidate(t *testing.T) {
	candidate := FallbackCandidate{
		Provider: "openai",
		Model:    "gpt-4",
		RPM:      60,
	}

	if candidate.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", candidate.Provider)
	}
	if candidate.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", candidate.Model)
	}
	if candidate.RPM != 60 {
		t.Errorf("expected RPM 60, got %d", candidate.RPM)
	}
}

// TestFallbackAttempt tests FallbackAttempt struct
func TestFallbackAttempt(t *testing.T) {
	attempt := FallbackAttempt{
		Provider: "openai",
		Model:    "gpt-4",
		Error:    nil,
		Reason:   FailoverRateLimit,
		Duration: 100 * time.Millisecond,
		Skipped:  false,
	}

	if attempt.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", attempt.Provider)
	}
	if attempt.Reason != FailoverRateLimit {
		t.Errorf("expected reason FailoverRateLimit, got %s", attempt.Reason)
	}
}

// TestFallbackResult tests FallbackResult struct
func TestFallbackResult(t *testing.T) {
	result := &FallbackResult{
		Response: &LLMResponse{Content: "test response"},
		Provider: "openai",
		Model:    "gpt-4",
		Attempts: []FallbackAttempt{
			{Provider: "openai", Model: "gpt-4"},
		},
	}

	if result.Content != "test response" {
		t.Errorf("expected 'test response', got '%s'", result.Content)
	}
	if result.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", result.Provider)
	}
	if len(result.Attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(result.Attempts))
	}
}
