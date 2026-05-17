package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SubAgent represents a delegated sub-agent task
type SubAgent struct {
	ID       string                 `json:"id"`
	Task     string                 `json:"task"`
	Status   string                 `json:"status"` // "pending", "running", "completed", "failed"
	Result   string                 `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Created  time.Time              `json:"created"`
	Finished *time.Time             `json:"finished,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SubAgentManager manages sub-agent tasks
type SubAgentManager struct {
	mu      sync.RWMutex
	agents  map[string]*SubAgent
	results map[string]chan string // channels for receiving results
}

var globalSubAgentManager = &SubAgentManager{
	agents:  make(map[string]*SubAgent),
	results: make(map[string]chan string),
}

// DelegateTaskTool allows spawning isolated sub-agents for complex subtasks
type DelegateTaskTool struct {
	baseTool *BaseTool
}

var _ Tool = (*DelegateTaskTool)(nil)

// NewDelegateTaskTool creates a new delegate task tool
func NewDelegateTaskTool() *DelegateTaskTool {
	return &DelegateTaskTool{
		baseTool: NewBaseTool(
			"delegate_task",
			`Spawn an isolated sub-agent with its own context to handle complex subtasks in parallel.
Use this when a task can be broken into independent parts, or when you need a focused agent to handle a specific job without cluttering the main conversation.

Parameters:
- task: The detailed task description for the sub-agent
- context: Optional additional context (files, data, instructions)
- model: Optional model override (defaults to main agent's model)
- timeout: Optional max runtime in seconds (default: 300, max: 3600)

Returns a task ID that can be used to poll for results.`,
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Detailed task description for the sub-agent",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional additional context (files, data, instructions)",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model override (e.g., 'gpt-4', 'claude-3-opus')",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Max runtime in seconds (default: 300, max: 3600)",
						"default":     300,
					},
				},
				"required": []string{"task"},
			},
		),
	}
}

func (t *DelegateTaskTool) Name() string {
	return t.baseTool.Name()
}

func (t *DelegateTaskTool) Description() string {
	return t.baseTool.Description()
}

func (t *DelegateTaskTool) Schema() map[string]interface{} {
	return t.baseTool.Schema()
}

// Execute spawns a sub-agent task
func (t *DelegateTaskTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	task, ok := params["task"].(string)
	if !ok || task == "" {
		return nil, fmt.Errorf("task parameter is required")
	}

	// Optional parameters
	var contextStr string
	if ctx, ok := params["context"].(string); ok {
		contextStr = ctx
	}

	var model string
	if m, ok := params["model"].(string); ok {
		model = m
	}

	timeout := 300
	if to, ok := params["timeout"].(float64); ok {
		timeout = int(to)
		if timeout < 30 {
			timeout = 30
		}
		if timeout > 3600 {
			timeout = 3600
		}
	}

	// Create sub-agent
	agent := &SubAgent{
		ID:       generateTaskID(),
		Task:     task,
		Status:   "pending",
		Created:  time.Now(),
		Metadata: map[string]interface{}{},
	}

	if contextStr != "" {
		agent.Metadata["context"] = contextStr
	}
	if model != "" {
		agent.Metadata["model"] = model
	}
	agent.Metadata["timeout"] = timeout

	// Store agent and result channel
	globalSubAgentManager.mu.Lock()
	globalSubAgentManager.agents[agent.ID] = agent
	resultChan := make(chan string, 1)
	globalSubAgentManager.results[agent.ID] = resultChan
	globalSubAgentManager.mu.Unlock()

	// Start task execution in background
	go t.runSubAgent(agent.ID, task, contextStr, model, timeout)

	return map[string]interface{}{
		"task_id":  agent.ID,
		"status":   "pending",
		"message":  fmt.Sprintf("Sub-agent task created. Poll for results using task_id '%s'", agent.ID),
		"polling": map[string]interface{}{
			"poll_task": fmt.Sprintf("Use task_id '%s' to check status and get results", agent.ID),
		},
	}, nil
}

// runSubAgent executes the sub-agent task
func (t *DelegateTaskTool) runSubAgent(id, task, contextStr, model string, timeoutSeconds int) {
	defer func() {
		if r := recover(); r != nil {
			globalSubAgentManager.mu.Lock()
			if agent, ok := globalSubAgentManager.agents[id]; ok {
				agent.Status = "failed"
				agent.Error = fmt.Sprintf("panic: %v", r)
				now := time.Now()
				agent.Finished = &now
			}
			globalSubAgentManager.mu.Unlock()
		}
	}()

	// Update status to running
	globalSubAgentManager.mu.Lock()
	if agent, ok := globalSubAgentManager.agents[id]; ok {
		agent.Status = "running"
	}
	globalSubAgentManager.mu.Unlock()

	// TODO: Integrate with agent execution
	// For now, this is a placeholder that simulates sub-agent execution
	// In a full implementation, this would:
	// 1. Create a new agent context
	// 2. Run the agent with the given task
	// 3. Return the result

	// Simulate execution
	time.Sleep(time.Duration(timeoutSeconds/2) * time.Second)

	// For demo purposes, mark as completed with placeholder result
	// In production, this would call the actual agent
	result := fmt.Sprintf("[Sub-agent] Completed task: %s\n\nNote: This is a placeholder result. Full sub-agent execution requires integration with the agent runtime.", task)

	globalSubAgentManager.mu.Lock()
	if agent, ok := globalSubAgentManager.agents[id]; ok {
		agent.Status = "completed"
		agent.Result = result
		now := time.Now()
		agent.Finished = &now
	}
	if ch, ok := globalSubAgentManager.results[id]; ok {
		ch <- result
		close(ch)
	}
	globalSubAgentManager.mu.Unlock()
}

// PollTaskTool polls for sub-agent task results
type PollTaskTool struct {
	baseTool *BaseTool
}

var _ Tool = (*PollTaskTool)(nil)

// NewPollTaskTool creates a new poll task tool
func NewPollTaskTool() *PollTaskTool {
	return &PollTaskTool{
		baseTool: NewBaseTool(
			"poll_task",
			`Poll for the status and result of a delegated sub-agent task.
Use the task_id returned by delegate_task to check progress and retrieve results.`,
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID returned by delegate_task",
					},
					"wait": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, wait for completion (max 60 seconds)",
						"default":     false,
					},
				},
				"required": []string{"task_id"},
			},
		),
	}
}

func (t *PollTaskTool) Name() string {
	return t.baseTool.Name()
}

func (t *PollTaskTool) Description() string {
	return t.baseTool.Description()
}

func (t *PollTaskTool) Schema() map[string]interface{} {
	return t.baseTool.Schema()
}

// Execute polls for task status
func (t *PollTaskTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID, ok := params["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id parameter is required")
	}

	wait := false
	if w, ok := params["wait"].(bool); ok {
		wait = w
	}

	globalSubAgentManager.mu.RLock()
	agent, exists := globalSubAgentManager.agents[taskID]
	if !exists {
		globalSubAgentManager.mu.RUnlock()
		return nil, fmt.Errorf("task_id '%s' not found", taskID)
	}

	// If waiting and not completed, wait for result
	if wait && agent.Status != "completed" && agent.Status != "failed" {
		resultChan := globalSubAgentManager.results[taskID]
		globalSubAgentManager.mu.RUnlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(60 * time.Second):
			globalSubAgentManager.mu.RLock()
			agent = globalSubAgentManager.agents[taskID]
			globalSubAgentManager.mu.RUnlock()
		case result := <-resultChan:
			globalSubAgentManager.mu.RLock()
			agent = globalSubAgentManager.agents[taskID]
			agent.Result = result
			globalSubAgentManager.mu.RUnlock()
		}
	} else {
		globalSubAgentManager.mu.RUnlock()
	}

	// Build response
	resp := map[string]interface{}{
		"task_id":  agent.ID,
		"status":   agent.Status,
		"created":  agent.Created.Format(time.RFC3339),
		"duration": time.Since(agent.Created).String(),
	}

	if agent.Finished != nil {
		resp["finished"] = agent.Finished.Format(time.RFC3339)
	}

	switch agent.Status {
	case "completed":
		resp["result"] = agent.Result
	case "failed":
		resp["error"] = agent.Error
	case "running":
		resp["message"] = "Task is still running..."
	case "pending":
		resp["message"] = "Task is queued..."
	}

	return resp, nil
}

// ListTasksTool lists all sub-agent tasks
type ListTasksTool struct {
	baseTool *BaseTool
}

var _ Tool = (*ListTasksTool)(nil)

// NewListTasksTool creates a new list tasks tool
func NewListTasksTool() *ListTasksTool {
	return &ListTasksTool{
		baseTool: NewBaseTool(
			"list_tasks",
			`List all delegated sub-agent tasks.
Returns a summary of all tasks including their status and IDs.`,
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type": "string",
						"enum": []string{"all", "pending", "running", "completed", "failed"},
						"description": "Filter by task status",
						"default": "all",
					},
				},
			},
		),
	}
}

func (t *ListTasksTool) Name() string {
	return t.baseTool.Name()
}

func (t *ListTasksTool) Description() string {
	return t.baseTool.Description()
}

func (t *ListTasksTool) Schema() map[string]interface{} {
	return t.baseTool.Schema()
}

// Execute lists tasks
func (t *ListTasksTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	statusFilter := "all"
	if s, ok := params["status"].(string); ok {
		statusFilter = s
	}

	globalSubAgentManager.mu.RLock()
	defer globalSubAgentManager.mu.RUnlock()

	var tasks []map[string]interface{}
	for _, agent := range globalSubAgentManager.agents {
		if statusFilter != "all" && agent.Status != statusFilter {
			continue
		}

		task := map[string]interface{}{
			"task_id":  agent.ID,
			"status":   agent.Status,
			"task":     truncate(agent.Task, 100),
			"created":  agent.Created.Format(time.RFC3339),
			"duration": time.Since(agent.Created).String(),
		}

		if agent.Finished != nil {
			task["finished"] = agent.Finished.Format(time.RFC3339)
		}

		tasks = append(tasks, task)
	}

	// Count by status
	statusCounts := map[string]int{
		"pending":   0,
		"running":   0,
		"completed": 0,
		"failed":    0,
	}
	for _, agent := range globalSubAgentManager.agents {
		statusCounts[agent.Status]++
	}

	return map[string]interface{}{
		"total":        len(tasks),
		"status_counts": statusCounts,
		"tasks":         tasks,
	}, nil
}

// CancelTaskTool cancels a running sub-agent task
type CancelTaskTool struct {
	baseTool *BaseTool
}

var _ Tool = (*CancelTaskTool)(nil)

// NewCancelTaskTool creates a new cancel task tool
func NewCancelTaskTool() *CancelTaskTool {
	return &CancelTaskTool{
		baseTool: NewBaseTool(
			"cancel_task",
			`Cancel a running delegated sub-agent task.
Only tasks with status 'pending' or 'running' can be cancelled.`,
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to cancel",
					},
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Optional reason for cancellation",
					},
				},
				"required": []string{"task_id"},
			},
		),
	}
}

func (t *CancelTaskTool) Name() string {
	return t.baseTool.Name()
}

func (t *CancelTaskTool) Description() string {
	return t.baseTool.Description()
}

func (t *CancelTaskTool) Schema() map[string]interface{} {
	return t.baseTool.Schema()
}

// Execute cancels a task
func (t *CancelTaskTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID, ok := params["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id parameter is required")
	}

	reason := ""
	if r, ok := params["reason"].(string); ok {
		reason = r
	}

	globalSubAgentManager.mu.Lock()
	defer globalSubAgentManager.mu.Unlock()

	agent, exists := globalSubAgentManager.agents[taskID]
	if !exists {
		return nil, fmt.Errorf("task_id '%s' not found", taskID)
	}

	if agent.Status != "pending" && agent.Status != "running" {
		return nil, fmt.Errorf("cannot cancel task with status '%s'", agent.Status)
	}

	agent.Status = "failed"
	agent.Error = fmt.Sprintf("Cancelled by user: %s", reason)
	now := time.Now()
	agent.Finished = &now

	return map[string]interface{}{
		"task_id":  taskID,
		"status":   "cancelled",
		"message":  "Task has been cancelled",
		"cancelled": time.Now().Format(time.RFC3339),
	}, nil
}

// Helper functions

func generateTaskID() string {
	return fmt.Sprintf("task_%d_%d", time.Now().UnixNano()/1000000, time.Now().Nanosecond()%1000)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// MarshalResult marshals a result to JSON string
func MarshalResult(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to marshal result: %v"}`, err)
	}
	return string(data)
}

// GetSubAgentManager returns the global sub-agent manager
func GetSubAgentManager() *SubAgentManager {
	return globalSubAgentManager
}
