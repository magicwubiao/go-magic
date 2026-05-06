package subagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Config holds subagent configuration
type Config struct {
	MaxConcurrent int           `json:"max_concurrent"` // Max parallel subagents
	MaxDepth      int           `json:"max_depth"`      // Max recursion depth
	Timeout       time.Duration `json:"timeout"`        // Task timeout
}

// DefaultConfig returns default subagent configuration
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrent: 3,
		MaxDepth:      2,
		Timeout:       120 * time.Second,
	}
}

// Task represents a task to be executed by a subagent
type Task struct {
	ID          string                 `json:"id"`
	Description string                 `json:"description"`
	Input       string                 `json:"input"`
	Tools       []string               `json:"tools,omitempty"` // Tools to enable for this task
	Context     map[string]interface{} `json:"context,omitempty"`
	ParentID    string                 `json:"parent_id,omitempty"`
	Depth       int                    `json:"depth"`
	CreatedAt   time.Time              `json:"created_at"`
}

// Result represents the result of a subagent task
type Result struct {
	TaskID     string        `json:"task_id"`
	Success    bool          `json:"success"`
	Output     string        `json:"output"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
	SubResults []Result      `json:"sub_results,omitempty"`
}

// SubAgent represents a subagent that can execute tasks
type SubAgent struct {
	id        string
	name      string
	provider  provider.Provider
	registry  agent.ToolRegistry
	agent     *agent.Agent
	depth     int
	maxDepth  int
	tools     []string
	mu        sync.RWMutex
	completed bool
}

// Manager manages all subagents
type Manager struct {
	config     *Config
	provider   provider.Provider
	registry   agent.ToolRegistry
	mu         sync.RWMutex
	subagents  map[string]*SubAgent
	tasks      map[string]*Task
	results    map[string]*Result
	taskQueue  chan *Task
	resultChan chan *Result
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewManager creates a new subagent manager
func NewManager(cfg *Config, prov provider.Provider, registry agent.ToolRegistry) *Manager {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	if cfg.Timeout == 0 {
		ctx = context.Background()
		cancel = func() {}
	}

	return &Manager{
		config:     cfg,
		provider:   prov,
		registry:   registry,
		subagents:  make(map[string]*SubAgent),
		tasks:      make(map[string]*Task),
		results:    make(map[string]*Result),
		taskQueue:  make(chan *Task, cfg.MaxConcurrent*2),
		resultChan: make(chan *Result, 100),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start starts the subagent manager
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.processTasks()
}

// Stop stops the subagent manager
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	close(m.taskQueue)
	close(m.resultChan)
}

// SpawnTask spawns a new subagent task
func (m *Manager) SpawnTask(description, input string, tools []string) (string, error) {
	return m.SpawnTaskWithContext(description, input, tools, nil)
}

// SpawnTaskWithContext spawns a task with additional context
func (m *Manager) SpawnTaskWithContext(description, input string, tools []string, ctx map[string]interface{}) (string, error) {
	task := &Task{
		ID:          uuid.New().String(),
		Description: description,
		Input:       input,
		Tools:       tools,
		Context:     ctx,
		CreatedAt:   time.Now(),
	}

	return task.ID, m.SubmitTask(task)
}

// SubmitTask submits a task to the queue
func (m *Manager) SubmitTask(task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task.Depth > m.config.MaxDepth {
		return fmt.Errorf("task depth %d exceeds maximum %d", task.Depth, m.config.MaxDepth)
	}

	m.tasks[task.ID] = task

	select {
	case m.taskQueue <- task:
		return nil
	default:
		return fmt.Errorf("task queue full")
	}
}

// GetTask returns a task by ID
func (m *Manager) GetTask(id string) *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[id]
}

// GetResult returns a result by task ID
func (m *Manager) GetResult(id string) *Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results[id]
}

// processTasks processes tasks from the queue
func (m *Manager) processTasks() {
	defer m.wg.Done()

	semaphore := make(chan struct{}, m.config.MaxConcurrent)

	for {
		select {
		case <-m.ctx.Done():
			return
		case task, ok := <-m.taskQueue:
			if !ok {
				return
			}

			// Acquire semaphore slot
			select {
			case semaphore <- struct{}{}:
				m.wg.Add(1)
				go func(t *Task) {
					defer func() {
						<-semaphore
						m.wg.Done()
					}()
					m.executeTask(t)
				}(task)
			case <-m.ctx.Done():
				return
			}
		}
	}
}

// executeTask executes a task with a subagent
func (m *Manager) executeTask(task *Task) {
	startTime := time.Now()
	result := &Result{
		TaskID:   task.ID,
		Duration: time.Since(startTime),
	}

	subAgent, err := m.createSubAgent(task)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		m.storeResult(result)
		m.resultChan <- result
		return
	}

	output, err := subAgent.Run(m.ctx, task.Input)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
	} else {
		result.Success = true
		result.Output = output
		result.Duration = time.Since(startTime)
	}

	m.storeResult(result)
	m.resultChan <- result
}

// createSubAgent creates a new subagent for a task
func (m *Manager) createSubAgent(task *Task) (*SubAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task.Depth >= m.config.MaxDepth {
		return nil, fmt.Errorf("maximum subagent depth reached")
	}

	id := uuid.New().String()
	name := fmt.Sprintf("subagent-%s", id[:8])

	// Create system prompt
	systemPrompt := fmt.Sprintf(`You are %s, a subagent executing a specific task.
Task: %s
Depth: %d/%d
%s`,
		name,
		task.Description,
		task.Depth,
		m.config.MaxDepth,
		generateContextPrompt(task.Context),
	)

	// Generate tools schema from registry based on task.Tools filter
	toolsSchema := m.generateToolsSchema(task.Tools)
	aiAgent := agent.NewAIAgent(m.provider, m.registry, toolsSchema, systemPrompt)

	subAgent := &SubAgent{
		id:       id,
		name:     name,
		provider: m.provider,
		registry: m.registry,
		agent:    aiAgent,
		depth:    task.Depth,
		maxDepth: m.config.MaxDepth,
		tools:    task.Tools,
	}

	m.subagents[id] = subAgent
	return subAgent, nil
}

// generateToolsSchema generates tools schema from registry, optionally filtered by tool names
func (m *Manager) generateToolsSchema(filterTools []string) []map[string]interface{} {
	// Get tool registry interface
	registry, ok := m.registry.(*tool.Registry)
	if !ok {
		// If registry is not the standard type, return empty schema
		return []map[string]interface{}{}
	}

	tools := []map[string]interface{}{}
	for _, tName := range registry.List() {
		// Apply filter if specified
		if len(filterTools) > 0 {
			found := false
			for _, f := range filterTools {
				if f == tName {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		t, err := registry.Get(tName)
		if err != nil {
			continue
		}

		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return tools
}

// Run executes the subagent
func (s *SubAgent) Run(ctx context.Context, input string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.completed {
		return "", fmt.Errorf("subagent already completed")
	}

	output, err := s.agent.RunConversation(ctx, input)
	if err != nil {
		return "", err
	}

	s.completed = true
	return output, nil
}

// storeResult stores a result
func (m *Manager) storeResult(result *Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.TaskID] = result
}

// ListSubAgents returns all active subagents
func (m *Manager) ListSubAgents() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]map[string]interface{}, 0, len(m.subagents))
	for id, sub := range m.subagents {
		sub.mu.RLock()
		agents = append(agents, map[string]interface{}{
			"id":        id,
			"name":      sub.name,
			"depth":     sub.depth,
			"completed": sub.completed,
		})
		sub.mu.RUnlock()
	}
	return agents
}

// KillSubAgent terminates a subagent
func (m *Manager) KillSubAgent(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, exists := m.subagents[id]
	if !exists {
		return fmt.Errorf("subagent '%s' not found", id)
	}

	sub.mu.Lock()
	sub.completed = true
	sub.mu.Unlock()

	delete(m.subagents, id)
	return nil
}

// WaitForResult waits for a task result with timeout
func (m *Manager) WaitForResult(taskID string, timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		m.mu.RLock()
		result, exists := m.results[taskID]
		m.mu.RUnlock()

		if exists {
			return result, nil
		}

		select {
		case result, ok := <-m.resultChan:
			if !ok {
				return nil, fmt.Errorf("result channel closed")
			}
			if result.TaskID == taskID {
				return result, nil
			}
		case <-m.ctx.Done():
			return nil, m.ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Check again
		}
	}

	return nil, fmt.Errorf("timeout waiting for result")
}

// SpawnMultiple spawns multiple tasks and waits for all results
func (m *Manager) SpawnMultiple(tasks []struct {
	Description string
	Input       string
	Tools       []string
}) ([]Result, error) {
	var taskIDs []string
	var mu sync.Mutex

	for _, task := range tasks {
		id, err := m.SpawnTask(task.Description, task.Input, task.Tools)
		if err != nil {
			return nil, fmt.Errorf("failed to spawn task: %w", err)
		}
		mu.Lock()
		taskIDs = append(taskIDs, id)
		mu.Unlock()
	}

	// Wait for all results
	var results []Result
	for range taskIDs {
		select {
		case result := <-m.resultChan:
			results = append(results, *result)
		case <-m.ctx.Done():
			return results, m.ctx.Err()
		}
	}

	return results, nil
}

// GetStats returns manager statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"active_subagents": len(m.subagents),
		"pending_tasks":    len(m.tasks),
		"completed_tasks":  len(m.results),
		"config":           m.config,
	}
}

func generateContextPrompt(ctx map[string]interface{}) string {
	if ctx == nil {
		return ""
	}

	var prompt string
	for key, value := range ctx {
		prompt += fmt.Sprintf("\n%s: %v", key, value)
	}
	return prompt
}

// ToJSONRPCMessage converts a provider message to JSON-RPC format
func ToJSONRPCMessage(msg provider.Message) types.Message {
	return types.Message{
		Role:      msg.Role,
		Content:   msg.Content,
		ToolCalls: nil,
	}
}
