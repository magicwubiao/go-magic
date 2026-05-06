package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// EnhancedConfig holds enhanced subagent configuration
type EnhancedConfig struct {
	MaxConcurrent int           `json:"max_concurrent"` // Max parallel subagents
	MaxDepth      int           `json:"max_depth"`      // Max recursion depth
	Timeout       time.Duration `json:"timeout"`        // Task timeout
	EnableRPC     bool          `json:"enable_rpc"`     // Enable RPC communication
	AutoCleanup   bool          `json:"auto_cleanup"`   // Auto cleanup completed agents
}

// DefaultEnhancedConfig returns default enhanced configuration
func DefaultEnhancedConfig() *EnhancedConfig {
	return &EnhancedConfig{
		MaxConcurrent: 3,
		MaxDepth:      2,
		Timeout:       120 * time.Second,
		EnableRPC:     true,
		AutoCleanup:   true,
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
	Priority    int                    `json:"priority"` // Higher priority tasks first
}

// Result represents the result of a subagent task
type Result struct {
	TaskID     string                 `json:"task_id"`
	Success    bool                   `json:"success"`
	Output     string                 `json:"output"`
	Error      string                 `json:"error,omitempty"`
	Duration   time.Duration          `json:"duration"`
	SubResults []Result               `json:"sub_results,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
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
	sessionID string
	bus       *bus.EventBus
}

// EnhancedManager manages all subagents with enhanced features
type EnhancedManager struct {
	config     *EnhancedConfig
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

	// RPC communication
	rpcHandlers map[string]RPCHandler
	eventBus    *bus.EventBus
}

// RPCHandler handles RPC calls
type RPCHandler func(ctx context.Context, args interface{}) (interface{}, error)

// RPCMessage represents an RPC message
type RPCMessage struct {
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Args    interface{} `json:"args"`
	ReplyTo string      `json:"reply_to,omitempty"`
}

// NewEnhancedManager creates a new enhanced subagent manager
func NewEnhancedManager(cfg *EnhancedConfig, prov provider.Provider, registry agent.ToolRegistry) *EnhancedManager {
	if cfg == nil {
		cfg = DefaultEnhancedConfig()
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	if cfg.Timeout == 0 {
		ctx = context.Background()
		cancel = func() {}
	}

	m := &EnhancedManager{
		config:      cfg,
		provider:    prov,
		registry:    registry,
		subagents:   make(map[string]*SubAgent),
		tasks:       make(map[string]*Task),
		results:     make(map[string]*Result),
		taskQueue:   make(chan *Task, cfg.MaxConcurrent*2),
		resultChan:  make(chan *Result, 100),
		ctx:         ctx,
		cancel:      cancel,
		rpcHandlers: make(map[string]RPCHandler),
		eventBus:    bus.NewEventBus(),
	}

	// Register default RPC handlers
	m.registerRPCHandlers()

	return m
}

// registerRPCHandlers registers default RPC handlers
func (m *EnhancedManager) registerRPCHandlers() {
	m.rpcHandlers["execute_tool"] = m.rpcExecuteTool
	m.rpcHandlers["spawn_subagent"] = m.rpcSpawnSubagent
	m.rpcHandlers["get_result"] = m.rpcGetResult
	m.rpcHandlers["emit_event"] = m.rpcEmitEvent
}

// Start starts the subagent manager
func (m *EnhancedManager) Start() {
	m.wg.Add(1)
	go m.processTasks()
}

// Stop stops the subagent manager
func (m *EnhancedManager) Stop() {
	m.cancel()
	m.wg.Wait()
	close(m.taskQueue)
	close(m.resultChan)
}

// SpawnTask spawns a new subagent task
func (m *EnhancedManager) SpawnTask(description, input string, tools []string) (string, error) {
	return m.SpawnTaskWithContext(description, input, tools, nil)
}

// SpawnTaskWithContext spawns a task with additional context
func (m *EnhancedManager) SpawnTaskWithContext(description, input string, tools []string, ctx map[string]interface{}) (string, error) {
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

// SpawnSubtask spawns a subtask with parent context
func (m *EnhancedManager) SpawnSubtask(parentID string, description, input string, tools []string) (string, error) {
	m.mu.RLock()
	parent, exists := m.tasks[parentID]
	m.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("parent task not found: %s", parentID)
	}

	if parent.Depth >= m.config.MaxDepth {
		return "", fmt.Errorf("maximum depth exceeded")
	}

	task := &Task{
		ID:          uuid.New().String(),
		Description: description,
		Input:       input,
		Tools:       tools,
		Context:     parent.Context,
		ParentID:    parentID,
		Depth:       parent.Depth + 1,
		CreatedAt:   time.Now(),
	}

	return task.ID, m.SubmitTask(task)
}

// SubmitTask submits a task to the queue
func (m *EnhancedManager) SubmitTask(task *Task) error {
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
func (m *EnhancedManager) GetTask(id string) *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[id]
}

// GetResult returns a result by task ID
func (m *EnhancedManager) GetResult(id string) *Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results[id]
}

// GetAllResults returns all results
func (m *EnhancedManager) GetAllResults() []*Result {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]*Result, 0, len(m.results))
	for _, r := range m.results {
		results = append(results, r)
	}
	return results
}

// GetResultsChannel returns the results channel
func (m *EnhancedManager) GetResultsChannel() <-chan *Result {
	return m.resultChan
}

// processTasks processes tasks from the queue
func (m *EnhancedManager) processTasks() {
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

// executeTask executes a task
func (m *EnhancedManager) executeTask(task *Task) {
	start := time.Now()

	// Create subagent
	subAgent := m.createSubAgent(task)

	result := &Result{
		TaskID:   task.ID,
		Duration: time.Since(start),
		Metadata: make(map[string]interface{}),
	}

	// Execute the task
	output, err := subAgent.agent.RunConversation(m.ctx, task.Input)

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = output
	} else {
		result.Success = true
		result.Output = output
	}

	result.Duration = time.Since(start)

	// Store result
	m.mu.Lock()
	m.results[task.ID] = result
	m.mu.Unlock()

	// Send to result channel
	select {
	case m.resultChan <- result:
	default:
		// Channel full, skip
	}

	// Cleanup if configured
	if m.config.AutoCleanup {
		m.mu.Lock()
		delete(m.subagents, subAgent.id)
		delete(m.tasks, task.ID)
		m.mu.Unlock()
	}
}

// createSubAgent creates a subagent for a task
func (m *EnhancedManager) createSubAgent(task *Task) *SubAgent {
	sa := &SubAgent{
		id:        uuid.New().String(),
		name:      fmt.Sprintf("subagent-%s", task.ID[:8]),
		provider:  m.provider,
		registry:  m.registry,
		depth:     task.Depth,
		maxDepth:  m.config.MaxDepth,
		tools:     task.Tools,
		sessionID: task.ID,
		bus:       m.eventBus,
	}

	// Create agent
	systemPrompt := m.buildSystemPrompt(task)
	toolsSchema := m.getToolsSchema(task.Tools)
	sa.agent = agent.NewAIAgent(m.provider, m.registry, toolsSchema, systemPrompt)
	sa.agent.SetSession(sa.sessionID)

	m.mu.Lock()
	m.subagents[sa.id] = sa
	m.mu.Unlock()

	return sa
}

// buildSystemPrompt builds a system prompt for the subagent
func (m *EnhancedManager) buildSystemPrompt(task *Task) string {
	prompt := fmt.Sprintf("You are %s, a specialized subagent.\n\n", task.Description)

	if task.Context != nil {
		prompt += "Context:\n"
		for k, v := range task.Context {
			prompt += fmt.Sprintf("- %s: %v\n", k, v)
		}
	}

	prompt += "\nFocus on the specific task assigned to you and provide clear, concise results."

	return prompt
}

// getToolsSchema returns tools schema for the subagent
func (m *EnhancedManager) getToolsSchema(tools []string) []map[string]interface{} {
	// Return full schema for now
	// In production, this would filter based on task.Tools
	return nil
}

// RPC Methods

// HandleRPC handles an RPC message
func (m *EnhancedManager) HandleRPC(ctx context.Context, msg *RPCMessage) (*RPCMessage, error) {
	m.mu.RLock()
	handler, exists := m.rpcHandlers[msg.Method]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown RPC method: %s", msg.Method)
	}

	result, err := handler(ctx, msg.Args)
	if err != nil {
		return &RPCMessage{
			ID:     msg.ID,
			Method: "error",
			Args:   err.Error(),
		}, nil
	}

	return &RPCMessage{
		ID:     msg.ID,
		Method: msg.Method + "_result",
		Args:   result,
	}, nil
}

// rpcExecuteTool handles tool execution via RPC
func (m *EnhancedManager) rpcExecuteTool(ctx context.Context, args interface{}) (interface{}, error) {
	var req struct {
		ToolName string                 `json:"tool_name"`
		Args     map[string]interface{} `json:"args"`
	}

	if err := unmarshalArgs(args, &req); err != nil {
		return nil, err
	}

	result, err := m.registry.Execute(ctx, req.ToolName, req.Args)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  result,
	}, nil
}

// rpcSpawnSubagent handles subagent spawning via RPC
func (m *EnhancedManager) rpcSpawnSubagent(ctx context.Context, args interface{}) (interface{}, error) {
	var req struct {
		Description string   `json:"description"`
		Input       string   `json:"input"`
		Tools       []string `json:"tools,omitempty"`
	}

	if err := unmarshalArgs(args, &req); err != nil {
		return nil, err
	}

	taskID, err := m.SpawnTask(req.Description, req.Input, req.Tools)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"task_id": taskID,
	}, nil
}

// rpcGetResult handles result retrieval via RPC
func (m *EnhancedManager) rpcGetResult(ctx context.Context, args interface{}) (interface{}, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}

	if err := unmarshalArgs(args, &req); err != nil {
		return nil, err
	}

	result := m.GetResult(req.TaskID)
	if result == nil {
		return nil, fmt.Errorf("result not found: %s", req.TaskID)
	}

	return result, nil
}

// rpcEmitEvent handles event emission via RPC
func (m *EnhancedManager) rpcEmitEvent(ctx context.Context, args interface{}) (interface{}, error) {
	var req struct {
		Kind string      `json:"kind"`
		Data interface{} `json:"data"`
	}

	if err := unmarshalArgs(args, &req); err != nil {
		return nil, err
	}

	m.eventBus.Emit(bus.Event{
		Kind: bus.EventKind(req.Kind),
		Data: req.Data,
	})

	return map[string]interface{}{
		"success": true,
	}, nil
}

// unmarshalArgs unmarshals RPC arguments
func unmarshalArgs(args interface{}, target interface{}) error {
	var data []byte
	var err error

	switch v := args.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	case map[string]interface{}:
		data, err = json.Marshal(v)
	default:
		return fmt.Errorf("unsupported args type: %T", args)
	}

	if err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

// RegisterRPCHandler registers a custom RPC handler
func (m *EnhancedManager) RegisterRPCHandler(method string, handler RPCHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rpcHandlers[method] = handler
}

// GetActiveAgents returns the number of active subagents
func (m *EnhancedManager) GetActiveAgents() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subagents)
}

// GetPendingTasks returns the number of pending tasks
func (m *EnhancedManager) GetPendingTasks() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tasks)
}

// WaitForResult waits for a specific task result with timeout
func (m *EnhancedManager) WaitForResult(taskID string, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for result")
		case result := <-m.resultChan:
			if result.TaskID == taskID {
				return result, nil
			}
		case <-ticker.C:
			m.mu.RLock()
			result := m.results[taskID]
			m.mu.RUnlock()
			if result != nil {
				return result, nil
			}
		}
	}
}

// AggregateResults aggregates results from multiple subtasks
func (m *EnhancedManager) AggregateResults(parentTaskID string) *Result {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parent, exists := m.tasks[parentTaskID]
	if !exists {
		return nil
	}

	var subResults []Result
	for _, task := range m.tasks {
		if task.ParentID == parentTaskID {
			if r, ok := m.results[task.ID]; ok {
				subResults = append(subResults, *r)
			}
		}
	}

	return &Result{
		TaskID:     parentTaskID,
		Success:    len(subResults) > 0,
		Output:     fmt.Sprintf("Aggregated %d subtasks", len(subResults)),
		SubResults: subResults,
	}
}

// ToolExecutor implements agent.ToolRegistry for use as a SubAgent tool
type ToolExecutor struct {
	manager *EnhancedManager
}

// Execute executes a tool by spawning a subagent task
func (t *ToolExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	// Build input from tool name and arguments
	var input strings.Builder
	input.WriteString(fmt.Sprintf("Execute tool: %s\n", name))
	input.WriteString("Arguments:\n")
	for k, v := range args {
		input.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
	}

	// Spawn a subagent task
	taskID, err := t.manager.SpawnTask(
		fmt.Sprintf("Tool execution: %s", name),
		input.String(),
		[]string{name}, // Only allow the requested tool
	)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn task: %w", err)
	}

	// Wait for result with timeout
	taskCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	for {
		select {
		case <-taskCtx.Done():
			return nil, fmt.Errorf("task timeout: %w", taskCtx.Err())
		case result := <-t.manager.GetResultsChannel():
			if result.TaskID == taskID {
				if result.Success {
					return result.Output, nil
				}
				return nil, fmt.Errorf("task failed: %s", result.Error)
			}
		case <-time.After(100 * time.Millisecond):
			// Check if result is available
			if r := t.manager.GetResult(taskID); r != nil {
				if r.Success {
					return r.Output, nil
				}
				return nil, fmt.Errorf("task failed: %s", r.Error)
			}
		}
	}
}

// Helper to satisfy agent.ToolRegistry interface - wraps ToolExecutor
type SubAgentToolExecutor struct {
	manager *EnhancedManager
}

// NewSubAgentToolExecutor creates a new tool executor backed by EnhancedManager
func NewSubAgentToolExecutor(mgr *EnhancedManager) *SubAgentToolExecutor {
	return &SubAgentToolExecutor{manager: mgr}
}

// Execute executes a tool by spawning a subagent task
func (r *SubAgentToolExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	return r.manager.ExecuteTool(ctx, name, args)
}

// ExecuteTool executes a tool via the subagent manager
func (m *EnhancedManager) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	// Build input from tool name and arguments
	var input strings.Builder
	input.WriteString(fmt.Sprintf("Execute the following tool: %s\n\n", name))
	input.WriteString("Task:\n")
	input.WriteString("Please execute this tool with the provided arguments and return the result.\n\n")
	input.WriteString("Arguments:\n")
	for k, v := range args {
		input.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
	}

	// Create a direct task for this tool execution
	task := &Task{
		ID:          uuid.New().String(),
		Description: fmt.Sprintf("Tool execution: %s", name),
		Input:       input.String(),
		Tools:       []string{name},
		Depth:       0,
		CreatedAt:   time.Now(),
	}

	// Submit and execute synchronously
	if err := m.SubmitTask(task); err != nil {
		return nil, fmt.Errorf("failed to submit task: %w", err)
	}

	// Wait for result with timeout
	taskCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-taskCtx.Done():
			return nil, fmt.Errorf("tool execution timeout")
		case result := <-m.resultChan:
			if result.TaskID == task.ID {
				if result.Success {
					return result.Output, nil
				}
				return nil, fmt.Errorf("tool execution failed: %s", result.Error)
			}
		case <-ticker.C:
			if r := m.GetResult(task.ID); r != nil {
				if r.Success {
					return r.Output, nil
				}
				return nil, fmt.Errorf("tool execution failed: %s", r.Error)
			}
		}
	}
}
