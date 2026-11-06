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
	// cancelFn 取消该子代理的执行 ctx，由 executeTask 在创建后注入。
	// 只设 cancelled 标志拦不住正在运行的 runner（它检查的是 ctx），
	// cancel_task 必须真正中断执行。
	cancelFn  context.CancelFunc
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

// Stop stops the subagent manager.
// 不 close channel：任务队列/结果只经 map 权威读取（WaitForResult/GetResult），
// GC 回收即可；close 与 SubmitTask/publish 的发送存在竞态 panic 风险。
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
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
		// 入队失败不留幽灵任务：从 map 摘除，否则会以 pending 状态永远挂着。
		delete(m.tasks, task.ID)
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

// taskTimeout 返回单个任务的实际超时：优先用 delegate_task 传入的
// timeout_seconds（JSON 反序列化后是 float64，Go 内部路径可能是 int），
// 缺省回落到 Manager 配置。此前 executeTask 硬用 config.Timeout（默认 120s），
// 而工具声明 default 300s——子任务一跑过 2 分钟就被误判为"系统取消"。
func taskTimeout(task *Task, fallback time.Duration) time.Duration {
	if task == nil || task.Context == nil {
		return fallback
	}
	switch v := task.Context["timeout_seconds"].(type) {
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	}
	return fallback
}

// publishResult 已删除：resultChan 无消费者后整体移除，结果统一经
// storeResult 落 map，WaitForResult / GetResult 从 map 读取。

// executeTask executes a task with a subagent
func (m *Manager) executeTask(task *Task) {
	startTime := time.Now()

	// Create context with timeout（per-task 超时优先，见 taskTimeout）
	ctx, cancel := context.WithTimeout(m.ctx, taskTimeout(task, m.config.Timeout))
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
		return
	}

	subAgent, err := m.createSubAgent(task, cancel)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		task.SetStatus(TaskStatusFailed)
		m.storeResult(result)
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
			// 保留已产出的进度（如触达回合上限时 runner 随错误返回的内容），
			// 主 agent 能从 poll_task 里看到 partial_output 而不是白跑一场。
			result.Output = output
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

		// 给 runner 一点时间响应 ctx 取消，尽量回收部分输出；
		// provider 不响应取消时最多多等 5s，避免占住并发槽。
		select {
		case <-done:
			result.Output = output
		case <-time.After(5 * time.Second):
		}
	}

	result.Duration = time.Since(startTime)
	m.storeResult(result)

	// Clean up subagent
	m.removeSubAgent(subAgent.id)
}

// createSubAgent creates a new subagent for a task.
// cancelFn 是该任务执行 ctx 的取消函数，注入 SubAgent 供 cancel_task 中断执行。
func (m *Manager) createSubAgent(task *Task, cancelFn context.CancelFunc) (*SubAgent, error) {
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
		cancelFn: cancelFn,
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
		// 保留已产出的部分结果（如触达回合上限时 runner 随错误返回的
		// lastContent），调用方在 err != nil 时也应检查 output。
		return output, err
	}

	s.mu.Lock()
	s.completed = true
	s.mu.Unlock()

	return output, nil
}

// cancel marks the subagent as cancelled and aborts its execution ctx.
// cancelFn 可能为 nil（尚未开始执行），重复调用幂等。
func (s *SubAgent) cancel() {
	s.mu.Lock()
	s.cancelled = true
	cancelFn := s.cancelFn
	s.mu.Unlock()
	if cancelFn != nil {
		cancelFn()
	}
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

// WaitForResult waits for a task result with timeout.
// 只轮询 results map，不消费共享 resultChan：此前从 channel 读取时，
// 若先到的是别的任务的结果会被永久丢弃（并发委托时结果丢失）。
func (m *Manager) WaitForResult(taskID string, timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		m.mu.RLock()
		result, exists := m.results[taskID]
		m.mu.RUnlock()

		if exists {
			return result, nil
		}

		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("timeout waiting for result")
		}

		select {
		case <-m.ctx.Done():
			return nil, m.ctx.Err()
		case <-ticker.C:
			// Check again
		}
	}
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

	// Wait for all results：逐个按 taskID 等待（结果在 storeResult 里已落到
	// results map），与到达顺序无关。
	var results []Result
	for _, id := range taskIDs {
		result, err := m.WaitForResult(id, m.config.Timeout)
		if err != nil {
			return results, err
		}
		results = append(results, *result)
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
