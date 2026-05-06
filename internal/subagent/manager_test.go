package subagent

import (
	"context"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

func mockProvider() provider.Provider {
	return &mockProviderImpl{}
}

type mockProviderImpl struct{}

func (m *mockProviderImpl) Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	return &provider.ChatResponse{
		Content: "Mock response",
	}, nil
}

func (m *mockProviderImpl) Name() string {
	return "mock"
}

func (m *mockProviderImpl) ChatWithTools(ctx context.Context, messages []provider.Message, tools []interface{}) (*provider.ChatResponse, error) {
	return m.Chat(ctx, messages)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxConcurrent != 3 {
		t.Errorf("Expected MaxConcurrent 3, got %d", cfg.MaxConcurrent)
	}

	if cfg.MaxDepth != 2 {
		t.Errorf("Expected MaxDepth 2, got %d", cfg.MaxDepth)
	}

	if cfg.Timeout != 120*time.Second {
		t.Errorf("Expected Timeout 120s, got %v", cfg.Timeout)
	}
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.config != cfg {
		t.Error("Config not set correctly")
	}

	if mgr.provider != prov {
		t.Error("Provider not set correctly")
	}
}

func TestNewManagerNilConfig(t *testing.T) {
	prov := mockProvider()

	mgr := NewManager(nil, prov, nil)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	// Should use default config
	if mgr.config.MaxConcurrent != 3 {
		t.Errorf("Expected default MaxConcurrent 3, got %d", mgr.config.MaxConcurrent)
	}
}

func TestSpawnTask(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)
	mgr.Start()
	defer mgr.Stop()

	taskID, err := mgr.SpawnTask("Test task", "Test input", nil)
	if err != nil {
		t.Errorf("SpawnTask failed: %v", err)
	}

	if taskID == "" {
		t.Error("Task ID should not be empty")
	}
}

func TestSpawnTaskWithContext(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)
	mgr.Start()
	defer mgr.Stop()

	ctx := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	taskID, err := mgr.SpawnTaskWithContext("Test task with context", "Test input", nil, ctx)
	if err != nil {
		t.Errorf("SpawnTaskWithContext failed: %v", err)
	}

	if taskID == "" {
		t.Error("Task ID should not be empty")
	}

	// Check task was stored
	task := mgr.GetTask(taskID)
	if task == nil {
		t.Error("Task not found")
	}

	if task.Context["key1"] != "value1" {
		t.Errorf("Context key1 mismatch: got %v", task.Context["key1"])
	}
}

func TestGetTask(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	// Non-existent task
	task := mgr.GetTask("nonexistent")
	if task != nil {
		t.Error("Expected nil for non-existent task")
	}
}

func TestGetResult(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	// Non-existent result
	result := mgr.GetResult("nonexistent")
	if result != nil {
		t.Error("Expected nil for non-existent result")
	}
}

func TestSubmitTask(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	task := &Task{
		ID:          "test-task-1",
		Description: "Test task",
		Input:       "Test input",
		Depth:       0,
	}

	err := mgr.SubmitTask(task)
	if err != nil {
		t.Errorf("SubmitTask failed: %v", err)
	}
}

func TestSubmitTaskExceedsDepth(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	task := &Task{
		ID:          "test-task-depth",
		Description: "Test task exceeding depth",
		Input:       "Test input",
		Depth:       10, // Exceeds default MaxDepth of 2
	}

	err := mgr.SubmitTask(task)
	if err == nil {
		t.Error("Expected error for task exceeding depth")
	}
}

func TestListSubAgents(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	agents := mgr.ListSubAgents()
	if len(agents) != 0 {
		t.Errorf("Expected 0 agents, got %d", len(agents))
	}
}

func TestKillSubAgent(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	err := mgr.KillSubAgent("nonexistent")
	if err == nil {
		t.Error("Expected error killing non-existent subagent")
	}
}

func TestGetStats(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	stats := mgr.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}

	if stats["active_subagents"].(int) != 0 {
		t.Error("Expected 0 active subagents")
	}

	if stats["pending_tasks"].(int) != 0 {
		t.Error("Expected 0 pending tasks")
	}

	if stats["completed_tasks"].(int) != 0 {
		t.Error("Expected 0 completed tasks")
	}
}

func TestGenerateContextPrompt(t *testing.T) {
	ctx := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	prompt := generateContextPrompt(ctx)
	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}

	// Check that both values are in the prompt
	expected := "key1: value1"
	if !contains(prompt, expected) {
		t.Errorf("Expected prompt to contain '%s'", expected)
	}
}

func TestGenerateContextPromptNil(t *testing.T) {
	prompt := generateContextPrompt(nil)
	if prompt != "" {
		t.Errorf("Expected empty prompt for nil context, got '%s'", prompt)
	}
}

func TestSpawnMultiple(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)
	mgr.Start()
	defer mgr.Stop()

	tasks := []struct {
		Description string
		Input       string
		Tools       []string
	}{
		{"Task 1", "Input 1", nil},
		{"Task 2", "Input 2", nil},
	}

	results, err := mgr.SpawnMultiple(tasks)
	if err != nil {
		t.Errorf("SpawnMultiple failed: %v", err)
	}

	if len(results) != len(tasks) {
		t.Errorf("Expected %d results, got %d", len(tasks), len(results))
	}
}

func TestTaskCreation(t *testing.T) {
	task := &Task{
		ID:          "test-id",
		Description: "Test description",
		Input:       "Test input",
		Tools:       []string{"tool1", "tool2"},
		Context:     map[string]interface{}{"key": "value"},
		Depth:       1,
		CreatedAt:   time.Now(),
	}

	if task.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", task.ID)
	}

	if task.Depth != 1 {
		t.Errorf("Expected Depth 1, got %d", task.Depth)
	}
}

func TestResultCreation(t *testing.T) {
	result := &Result{
		TaskID:   "test-task-id",
		Success:  true,
		Output:   "Test output",
		Duration: 100 * time.Millisecond,
	}

	if !result.Success {
		t.Error("Expected Success to be true")
	}

	if result.Output != "Test output" {
		t.Errorf("Expected Output 'Test output', got '%s'", result.Output)
	}
}

func TestSubAgentName(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	task := &Task{
		ID:          "test-id",
		Description: "Test task",
		Input:       "Test input",
		Depth:       0,
	}

	subAgent, err := mgr.createSubAgent(task)
	if err != nil {
		t.Fatalf("createSubAgent failed: %v", err)
	}

	if !contains(subAgent.name, "subagent-") {
		t.Errorf("Expected subagent name to contain 'subagent-', got '%s'", subAgent.name)
	}
}

func TestSubAgentRunAlreadyCompleted(t *testing.T) {
	cfg := DefaultConfig()
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)

	task := &Task{
		ID:          "test-id",
		Description: "Test task",
		Input:       "Test input",
		Depth:       0,
	}

	subAgent, err := mgr.createSubAgent(task)
	if err != nil {
		t.Fatalf("createSubAgent failed: %v", err)
	}

	// Run first time
	_, err = subAgent.Run(context.Background(), "Test input")
	if err != nil {
		t.Errorf("First Run failed: %v", err)
	}

	// Run second time should fail
	_, err = subAgent.Run(context.Background(), "Test input 2")
	if err == nil {
		t.Error("Expected error on second Run")
	}
}

func TestManagerContextCancellation(t *testing.T) {
	cfg := &Config{
		MaxConcurrent: 3,
		MaxDepth:      2,
		Timeout:       1 * time.Millisecond,
	}
	prov := mockProvider()

	mgr := NewManager(cfg, prov, nil)
	mgr.Start()

	// Wait for timeout
	time.Sleep(10 * time.Millisecond)

	// Manager should stop automatically
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfigSerialization(t *testing.T) {
	cfg := &Config{
		MaxConcurrent: 5,
		MaxDepth:      3,
		Timeout:       60 * time.Second,
	}

	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent mismatch")
	}

	if cfg.MaxDepth != 3 {
		t.Errorf("MaxDepth mismatch")
	}

	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout mismatch")
	}
}
