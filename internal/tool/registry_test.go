package tool

import (
	"context"
	"testing"
	"time"
)

// MockTool is a simple tool for testing
type MockTool struct {
	name    string
	execute func(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

func (m *MockTool) Name() string {
	return m.name
}

func (m *MockTool) Description() string {
	return "Mock tool for testing"
}

func (m *MockTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{
				"type": "string",
			},
		},
	}
}

func (m *MockTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return m.execute(ctx, params)
}

// TestNewRegistry tests creating a new registry
func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if registry.tools == nil {
		t.Error("expected tools map to be initialized")
	}
	if registry.timeouts == nil {
		t.Error("expected timeouts map to be initialized")
	}
}

// TestRegistryRegister tests registering a tool
func TestRegistryRegister(t *testing.T) {
	registry := NewRegistry()
	tool := &MockTool{name: "test_tool", execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return "result", nil
	}}

	registry.Register(tool)

	names := registry.List()
	if len(names) != 1 {
		t.Errorf("expected 1 tool, got %d", len(names))
	}
	if names[0] != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got '%s'", names[0])
	}
}

// TestRegistryUnregister tests unregistering a tool
func TestRegistryUnregister(t *testing.T) {
	registry := NewRegistry()
	tool := &MockTool{name: "test_tool", execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return "result", nil
	}}

	registry.Register(tool)
	registry.Unregister("test_tool")

	names := registry.List()
	if len(names) != 0 {
		t.Errorf("expected 0 tools after unregister, got %d", len(names))
	}

	_, err := registry.Get("test_tool")
	if err == nil {
		t.Error("expected error when getting unregistered tool")
	}
}

// TestRegistryGet tests getting a tool by name
func TestRegistryGet(t *testing.T) {
	registry := NewRegistry()
	tool := &MockTool{name: "test_tool", execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return "result", nil
	}}

	registry.Register(tool)

	got, err := registry.Get("test_tool")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got.Name() != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got '%s'", got.Name())
	}

	_, err = registry.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

// TestRegistryList tests listing all registered tools
func TestRegistryList(t *testing.T) {
	registry := NewRegistry()

	names := registry.List()
	if len(names) != 0 {
		t.Errorf("expected 0 tools initially, got %d", len(names))
	}

	registry.Register(&MockTool{name: "tool1", execute: nil})
	registry.Register(&MockTool{name: "tool2", execute: nil})

	names = registry.List()
	if len(names) != 2 {
		t.Errorf("expected 2 tools, got %d", len(names))
	}
}

// TestRegistrySetTimeout tests setting and getting tool timeouts
func TestRegistrySetTimeout(t *testing.T) {
	registry := NewRegistry()

	registry.SetTimeout("test_tool", 30*time.Second)
	timeout := registry.GetTimeout("test_tool")

	if timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", timeout)
	}

	// Test default timeout (zero)
	timeout = registry.GetTimeout("nonexistent")
	if timeout != 0 {
		t.Errorf("expected 0 timeout for nonexistent tool, got %v", timeout)
	}
}

// TestRegistryRegisterAll tests registering all built-in tools
func TestRegistryRegisterAll(t *testing.T) {
	registry := NewRegistry()
	registry.RegisterAll()

	names := registry.List()
	if len(names) == 0 {
		t.Error("expected at least one tool to be registered")
	}

	// Check that critical tools are registered
	criticalTools := []string{"read_file", "write_file", "web_search"}
	for _, name := range criticalTools {
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool '%s' to be registered", name)
		}
	}
}

// TestGetAllTools tests GetAllTools function
func TestGetAllTools(t *testing.T) {
	tools := GetAllTools()
	if len(tools) == 0 {
		t.Error("expected at least one built-in tool")
	}

	// Check that all returned objects implement Tool interface
	for _, tool := range tools {
		if tool.Name() == "" {
			t.Error("tool has empty name")
		}
		if tool.Description() == "" {
			t.Error("tool has empty description")
		}
	}
}

// TestToolExecutor tests the default tool executor
func TestToolExecutor(t *testing.T) {
	executor := NewDefaultToolExecutor()
	if executor == nil {
		t.Fatal("expected non-nil executor")
	}

	tool := &MockTool{
		name: "exec_test",
		execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "executed", nil
		},
	}

	result, err := executor.Execute(context.Background(), tool, map[string]interface{}{"input": "test"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "executed" {
		t.Errorf("expected 'executed', got '%v'", result)
	}
}

// TestToolExecutorWithTimeout tests tool execution with timeout
func TestToolExecutorWithTimeout(t *testing.T) {
	executor := NewDefaultToolExecutor()
	executor.SetTimeout(100 * time.Millisecond)

	slowTool := &MockTool{
		name: "slow_tool",
		execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			time.Sleep(200 * time.Millisecond)
			return "done", nil
		},
	}

	_, err := executor.Execute(context.Background(), slowTool, map[string]interface{}{})
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestConcurrentAccess tests thread safety of registry
func TestConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	// Concurrently register and unregister tools
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				name := "tool"
				registry.Register(&MockTool{
					name: name,
					execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
						return nil, nil
					},
				})
				registry.Get(name)
				registry.List()
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestToolValidation tests parameter validation
func TestToolValidation(t *testing.T) {
	validated := &MockTool{
		name: "validated_tool",
		execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if input, ok := params["input"].(string); !ok {
				return nil, ErrInvalidParameters
			}
			return input, nil
		},
	}

	params := map[string]interface{}{"input": "test value"}
	result, err := validated.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "test value" {
		t.Errorf("expected 'test value', got '%v'", result)
	}
}
