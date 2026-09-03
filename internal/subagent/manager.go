package subagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// ToolRegistry is an interface for tool registry to avoid circular imports
type ToolRegistry interface {
	List() []string
	Get(name string) (Tool, error)
}

// Tool is an interface for tools
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]interface{}
}

// AgentRunner is an interface for running agent conversations
type AgentRunner interface {
	RunConversation(ctx context.Context, input string) (string, error)
}

// AgentFactory is a function type for creating agents
type AgentFactory func(provider provider.Provider, registry ToolRegistry, toolsSchema []map[string]interface{}, systemPrompt string) AgentRunner

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Config holds subagent configuration
type Config struct {
	MaxConcurrent int           `json:"max_concurrent"` // Max parallel subagents
	MaxDepth      int           `json:"max_depth"`      // Max recursion depth
	Timeout       time.Duration `json:"timeout"`        // Task timeout
	EnableNested  bool          `json:"enable_nested"`  // Enable nested subagents
}

// DefaultConfig returns default subagent configuration
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrent: 3,
		MaxDepth:      2,
		Timeout:       120 * time.Second,
		EnableNested:  true,
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
	Status      TaskStatus             `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Cancelled   bool                   `json:"cancelled"`
	mu          sync.RWMutex           `json:"-"`
}

// SetStatus sets the task status thread-safely
func (t *Task) SetStatus(status TaskStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Status = status
	now := time.Now()
	switch status {
	case TaskStatusRunning:
		t.StartedAt = &now
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		t.CompletedAt = &now
	}
}

// GetStatus gets the task status thread-safely
func (t *Task) GetStatus() TaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// IsCancelled checks if the task is cancelled
func (t *Task) IsCancelled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Cancelled
}

// Cancel marks the task as cancelled
func (t *Task) Cancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Cancelled = true
}

// Result represents the result of a subagent task
type Result struct {
	TaskID     string        `json:"task_id"`
	Success    bool          `json:"success"`
	Output     string        `json:"output"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
	SubResults []Result      `json:"sub_results,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
}

// SubAgent represents a subagent that can execute tasks
type SubAgent struct {
	id        string
	name      string
	provider  provider.Provider
	registry  ToolRegistry
	runner    AgentRunner
	depth     int
	maxDepth  int
	tools     []string
	mu        sync.RWMutex
	completed bool
	cancelled bool
	parentID  string
	taskID    string
}

// Manager manages all subagents
type Manager struct {
	config       *Config
	provider     provider.Provider
	registry     ToolRegistry
	agentFactory AgentFactory
	mu           sync.RWMutex
	subagents    map[string]*SubAgent
	tasks        map[string]*Task
	results      map[string]*Result
	taskQueue    chan *Task
	resultChan   chan *Result
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	semaphore    chan struct{}
}

// NewManager creates a new subagent manager
func NewManager(cfg *Config, prov provider.Provider, registry ToolRegistry, factory AgentFactory) *Manager {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:       cfg,
		provider:     prov,
		registry:     registry,
		agentFactory: factory,
		subagents:    make(map[string]*SubAgent),
		tasks:        make(map[string]*Task),
		results:      make(map[string]*Result),
		taskQueue:    make(chan *Task, cfg.MaxConcurrent*10),
		resultChan:   make(chan *Result, 100),
		ctx:          ctx,
		cancel:       cancel,
		semaphore:    make(chan struct{}, cfg.MaxConcurrent),
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
		Status:      TaskStatusPending,
		CreatedAt:   time.Now(),
	}

	return task.ID, m.SubmitTask(task)
}

// SpawnNestedTask spawns a nested subagent task from a parent task
func (m *Manager) SpawnNestedTask(parentTaskID, description, input string, tools []string, ctx map[string]interface{}) (string, error) {
	m.mu.RLock()
	parentTask, exists := m.tasks[parentTaskID]
	m.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("parent task '%s' not found", parentTaskID)
	}

	if !m.config.EnableNested {
		return "", fmt.Errorf("nested subagents are disabled")
	}

	if parentTask.Depth >= m.config.MaxDepth {
		return "", fmt.Errorf("maximum subagent depth reached (%d)", m.config.MaxDepth)
	}

	task := &Task{
		ID:          uuid.New().String(),
		Description: description,
		Input:       input,
		Tools:       tools,
		Context:     ctx,
		ParentID:    parentTaskID,
		Depth:       parentTask.Depth + 1,
		Status:      TaskStatusPending,
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

// CancelTask cancels a task by ID
func (m *Manager) CancelTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[id]
	if !exists {
		return fmt.Errorf("task '%s' not found", id)
	}

	status := task.GetStatus()
	if status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled {
		return fmt.Errorf("cannot cancel task with status '%s'", status)
	}

	task.Cancel()

	// Also cancel the subagent if it exists
	for _, sub := range m.subagents {
		if sub.taskID == id {
			sub.cancel()
			break
		}
	}

	return nil
}

// ListTasks returns all tasks with optional status filter
func (m *Manager) ListTasks(statusFilter TaskStatus) []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]*Task, 0)
	for _, task := range m.tasks {
		if statusFilter == "" || task.GetStatus() == statusFilter {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// processTasks processes tasks from the queue
func (m *Manager) processTasks() {
	defer m.wg.Done()

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
			case m.semaphore <- struct{}{}:
				m.wg.Add(1)
				go func(t *Task) {
					defer func() {
						<-m.semaphore
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

	// Create context with timeout
	ctx, cancel := context.WithTimeout(m.ctx, m.config.Timeout)
	defer cancel()

	// Update task status
	task.SetStatus(TaskStatusRunning)

	result := &Result{
		TaskID:    task.ID,
		Duration:  0,
		CreatedAt: time.Now(),
	}

	// Check if task was cancelled before starting
	if task.IsCancelled() {
		result.Success = false
		result.Error = "Task was cancelled before execution"
		task.SetStatus(TaskStatusCancelled)
		result.Duration = time.Since(startTime)
		m.storeResult(result)
		m.resultChan <- result
		return
	}

	subAgent, err := m.createSubAgent(task)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		task.SetStatus(TaskStatusFailed)
		m.storeResult(result)
		m.resultChan <- result
		return
	}

	// Execute with cancellation support
	done := make(chan struct{})
	var output string
	var execErr error

	go func() {
		defer close(done)
		output, execErr = subAgent.Run(ctx, task.Input)
	}()

	select {
	case <-done:
		if execErr != nil {
			result.Success = false
			result.Error = execErr.Error()
			task.SetStatus(TaskStatusFailed)
		} else {
			result.Success = true
			result.Output = output
			task.SetStatus(TaskStatusCompleted)
		}
	case <-ctx.Done():
		result.Success = false
		result.Error = "Task timeout or cancelled"
		task.SetStatus(TaskStatusCancelled)
		subAgent.cancel()
	}

	result.Duration = time.Since(startTime)
	m.storeResult(result)
	m.resultChan <- result

	// Clean up subagent
	m.removeSubAgent(subAgent.id)
}

// createSubAgent creates a new subagent for a task
func (m *Manager) createSubAgent(task *Task) (*SubAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task.Depth >= m.config.MaxDepth {
		return nil, fmt.Errorf("maximum subagent depth reached")
	}

	if m.agentFactory == nil {
		return nil, fmt.Errorf("agent factory not set")
	}

	id := uuid.New().String()
	name := fmt.Sprintf("subagent-%s", id[:8])

	// Create system prompt
	systemPrompt := fmt.Sprintf(`You are %s, a subagent executing a specific task.
Task: %s
Depth: %d/%d
Task ID: %s
%s`,
		name,
		task.Description,
		task.Depth,
		m.config.MaxDepth,
		task.ID,
		generateContextPrompt(task.Context),
	)

	// Generate tools schema from registry based on task.Tools filter
	toolsSchema := m.generateToolsSchema(task.Tools)
	runner := m.agentFactory(m.provider, m.registry, toolsSchema, systemPrompt)

	subAgent := &SubAgent{
		id:       id,
		name:     name,
		provider: m.provider,
		registry: m.registry,
		runner:   runner,
		depth:    task.Depth,
		maxDepth: m.config.MaxDepth,
		tools:    task.Tools,
		parentID: task.ParentID,
		taskID:   task.ID,
	}

	m.subagents[id] = subAgent
	return subAgent, nil
}

// removeSubAgent removes a subagent from the manager
func (m *Manager) removeSubAgent(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subagents, id)
}

// generateToolsSchema generates tools schema from registry, optionally filtered by tool names
func (m *Manager) generateToolsSchema(filterTools []string) []map[string]interface{} {
	tools := []map[string]interface{}{}
	for _, tName := range m.registry.List() {
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

		t, err := m.registry.Get(tName)
		if err != nil {
			continue
		}

		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Schema(),
			},
		})
	}
	return tools
}

// Run executes the subagent
func (s *SubAgent) Run(ctx context.Context, input string) (string, error) {
	s.mu.Lock()
	if s.completed {
		s.mu.Unlock()
		return "", fmt.Errorf("subagent already completed")
	}
	if s.cancelled {
		s.mu.Unlock()
		return "", fmt.Errorf("subagent was cancelled")
	}
	s.mu.Unlock()

	if s.runner == nil {
		return "", fmt.Errorf("agent runner not set")
	}

	output, err := s.runner.RunConversation(ctx, input)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.completed = true
	s.mu.Unlock()

	return output, nil
}

// cancel marks the subagent as cancelled
func (s *SubAgent) cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
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
			"cancelled": sub.cancelled,
			"parent_id": sub.parentID,
			"task_id":   sub.taskID,
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
	sub.cancelled = true
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

	statusCounts := map[TaskStatus]int{
		TaskStatusPending:   0,
		TaskStatusRunning:   0,
		TaskStatusCompleted: 0,
		TaskStatusFailed:    0,
		TaskStatusCancelled: 0,
	}

	for _, task := range m.tasks {
		statusCounts[task.GetStatus()]++
	}

	return map[string]interface{}{
		"active_subagents": len(m.subagents),
		"pending_tasks":    statusCounts[TaskStatusPending],
		"running_tasks":    statusCounts[TaskStatusRunning],
		"completed_tasks":  statusCounts[TaskStatusCompleted],
		"failed_tasks":     statusCounts[TaskStatusFailed],
		"cancelled_tasks":  statusCounts[TaskStatusCancelled],
		"total_tasks":      len(m.tasks),
		"config":           m.config,
	}
}

func generateContextPrompt(ctx map[string]interface{}) string {
	if ctx == nil || len(ctx) == 0 {
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
