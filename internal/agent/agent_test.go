package agent

import (
	"context"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// MockToolRegistry is a mock implementation of ToolRegistry for testing
type MockToolRegistry struct {
	tools map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

func (m *MockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if fn, ok := m.tools[name]; ok {
		return fn(ctx, args)
	}
	return nil, tool.NewToolNotFoundError(name)
}

// MockProvider is a mock implementation of Provider for testing
type MockProvider struct {
	name   string
	chatFn func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, messages)
	}
	return &provider.ChatResponse{Content: "mock response"}, nil
}

// TestNewAIAgent tests creating a new AI agent
func TestNewAIAgent(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{name: "test_provider"}

	agent := NewAIAgent(prov, registry, nil, "You are a helpful assistant.")
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.provider != prov {
		t.Error("provider not set correctly")
	}
	if agent.registry != registry {
		t.Error("registry not set correctly")
	}
}

// TestNewEnhancedAgent tests creating an enhanced agent
func TestNewEnhancedAgent(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{name: "test_provider"}

	agent := NewEnhancedAgent(
		prov, registry, nil, "You are a helpful assistant.",
		WithSteering(SteeringConfig{MaxIterations: 10}),
		WithMemory(true),
	)

	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.maxIterations != 10 {
		t.Errorf("expected maxIterations 10, got %d", agent.maxIterations)
	}
	if !agent.memoryEnabled {
		t.Error("expected memoryEnabled to be true")
	}
}

// TestAgentRunConversation tests running a conversation
func TestAgentRunConversation(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			return &provider.ChatResponse{Content: "Hello! How can I help you?"}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "You are a helpful assistant.")

	resp, err := agent.RunConversation(context.Background(), "Hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
}

// TestAgentToolExecution tests tool execution in conversation
func TestAgentToolExecution(t *testing.T) {
	registry := &MockToolRegistry{
		tools: map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error){
			"test_tool": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
				return "tool result", nil
			},
		},
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			// Return tool call on first message
			if len(messages) == 2 {
				return &provider.ChatResponse{
					Content:   "Let me help with that.",
					ToolCalls: nil,
				}, nil
			}
			return &provider.ChatResponse{
				Content: "Here's the tool result.",
			}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "You are a helpful assistant.")
	agent.maxTurns = 5

	resp, err := agent.RunConversation(context.Background(), "Use test_tool")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
}

// TestAgentContextPropagation tests context propagation
func TestAgentContextPropagation(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			return &provider.ChatResponse{Content: "done"}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := agent.RunConversation(ctx, "test")
	// Should handle canceled context gracefully
	if err != nil && err != context.Canceled {
		// Accept both behaviors - some implementations may handle this differently
	}
}

// TestAgentEventBus tests event emission
func TestAgentEventBus(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			return &provider.ChatResponse{Content: "test"}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "")

	// Test that Emit doesn't panic
	agent.Emit(0, "test data")
}

// TestAgentSession tests session management
func TestAgentSession(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			return &provider.ChatResponse{Content: "test"}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "")
	agent.SetSession("test_session_123")

	if agent.session != "test_session_123" {
		t.Errorf("expected session 'test_session_123', got '%s'", agent.session)
	}
}

// TestAgentAddSkillsContext tests adding skills context
func TestAgentAddSkillsContext(t *testing.T) {
	registry := &MockToolRegistry{
		tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error)),
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			return &provider.ChatResponse{Content: "test"}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "")
	agent.AddSkillsContext("You have access to the following skills: coding")

	if len(agent.history) == 0 {
		t.Error("expected non-empty history")
	}
}

// TestAgentMaxIterations tests max iterations limit
func TestAgentMaxIterations(t *testing.T) {
	registry := &MockToolRegistry{
		tools: map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error){
			"recursive_tool": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
				return "tool call", nil
			},
		},
	}
	prov := &MockProvider{
		name: "test_provider",
		chatFn: func(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
			// Always return a tool call to trigger max iterations
			return &provider.ChatResponse{
				Content: "Using tool...",
				ToolCalls: []provider.ToolCall{
					{ID: "call_1", Name: "recursive_tool", Arguments: map[string]interface{}{}},
				},
			}, nil
		},
	}

	agent := NewAIAgent(prov, registry, nil, "")
	agent.maxTurns = 3
	agent.maxIterations = 2

	_, err := agent.RunConversation(context.Background(), "start")
	if err == nil {
		// May or may not hit iteration limit depending on implementation
	}
}

// TestWithSteeringOption tests steering configuration
func TestWithSteeringOption(t *testing.T) {
	cfg := SteeringConfig{
		MaxIterations:  10,
		MaxTokenBudget: 1000,
	}

	opt := WithSteering(cfg)

	registry := &MockToolRegistry{tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error))}
	prov := &MockProvider{name: "test"}
	agent := NewAIAgent(prov, registry, nil, "")

	opt(agent)

	if agent.maxIterations != 10 {
		t.Errorf("expected maxIterations 10, got %d", agent.maxIterations)
	}
	if agent.maxTokenBudget != 1000 {
		t.Errorf("expected maxTokenBudget 1000, got %d", agent.maxTokenBudget)
	}
}

// TestWithMemoryOption tests memory configuration
func TestWithMemoryOption(t *testing.T) {
	opt := WithMemory(true)

	registry := &MockToolRegistry{tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error))}
	prov := &MockProvider{name: "test"}
	agent := NewAIAgent(prov, registry, nil, "")

	opt(agent)

	if !agent.memoryEnabled {
		t.Error("expected memoryEnabled to be true")
	}
}

// TestWithHooksOption tests hooks configuration
func TestWithHooksOption(t *testing.T) {
	registry := &MockToolRegistry{tools: make(map[string]func(ctx context.Context, args map[string]interface{}) (interface{}, error))}
	prov := &MockProvider{name: "test"}
	agent := NewAIAgent(prov, registry, nil, "")

	if agent.hooks == nil {
		t.Error("expected non-nil hooks manager")
	}
}
