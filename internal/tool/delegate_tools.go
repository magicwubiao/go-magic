package tool

import (
	"context"
	"fmt"

	"github.com/magicwubiao/go-magic/internal/subagent"
)

// DelegateTaskTool allows spawning isolated sub-agents for complex subtasks
type DelegateTaskTool struct {
	baseTool *BaseTool
	manager  *subagent.Manager
}

var _ Tool = (*DelegateTaskTool)(nil)

// NewDelegateTaskTool creates a new delegate task tool
func NewDelegateTaskTool(manager *subagent.Manager) *DelegateTaskTool {
	return &DelegateTaskTool{
		baseTool: NewBaseTool(
			"delegate_task",
			`Spawn an isolated sub-agent with its own context to handle complex subtasks in parallel.
Use this when a task can be broken into independent parts, or when you need a focused agent to handle a specific job without cluttering the main conversation.

Parameters:
- task: The detailed task description for the sub-agent
- input: The actual input/prompt for the sub-agent to process
- tools: Optional list of tool names to enable for this sub-agent (if empty, all tools are available)
- context: Optional additional context (key-value pairs)
- timeout: Optional max runtime in seconds (default: 300, max: 3600)

Returns a task ID that can be used to poll for results.`,
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Detailed task description for the sub-agent",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "The actual input/prompt for the sub-agent to process",
					},
					"tools": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Optional list of tool names to enable",
					},
					"context": map[string]interface{}{
						"type":        "object",
						"description": "Optional additional context (key-value pairs)",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Max runtime in seconds (default: 300, max: 3600)",
						"default":     300,
					},
				},
				"required": []string{"task", "input"},
			},
		),
		manager: manager,
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
	if t.manager == nil {
		return nil, fmt.Errorf("subagent manager not initialized")
	}

	taskDesc, ok := params["task"].(string)
	if !ok || taskDesc == "" {
		return nil, fmt.Errorf("task parameter is required")
	}

	input, ok := params["input"].(string)
	if !ok || input == "" {
		return nil, fmt.Errorf("input parameter is required")
	}

	// Optional parameters
	var tools []string
	if t, ok := params["tools"].([]interface{}); ok {
		for _, tool := range t {
			if s, ok := tool.(string); ok {
				tools = append(tools, s)
			}
		}
	}

	var contextMap map[string]interface{}
	if ctx, ok := params["context"].(map[string]interface{}); ok {
		contextMap = ctx
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

	// Add timeout to context
	if contextMap == nil {
		contextMap = make(map[string]interface{})
	}
	contextMap["timeout_seconds"] = timeout

	// Create task
	taskID, err := t.manager.SpawnTaskWithContext(taskDesc, input, tools, contextMap)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn sub-agent task: %w", err)
	}

	return map[string]interface{}{
		"task_id": taskID,
		"status":  "pending",
		"message": fmt.Sprintf("Sub-agent task created with ID '%s'. Use poll_task to check status and get results.", taskID),
		"polling": map[string]interface{}{
			"tool":    "poll_task",
			"task_id": taskID,
		},
	}, nil
}

// PollTaskTool polls for sub-agent task results
type PollTaskTool struct {
	baseTool *BaseTool
	manager  *subagent.Manager
}

var _ Tool = (*PollTaskTool)(nil)

// NewPollTaskTool creates a new poll task tool
func NewPollTaskTool(manager *subagent.Manager) *PollTaskTool {
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
		manager: manager,
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
	if t.manager == nil {
		return nil, fmt.Errorf("subagent manager not initialized")
	}

	taskID, ok := params["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id parameter is required")
	}

	wait := false
	if w, ok := params["wait"].(bool); ok {
		wait = w
	}

	// Get task
	task := t.manager.GetTask(taskID)
	if task == nil {
		return nil, fmt.Errorf("task '%s' not found", taskID)
	}

	// If waiting and not completed, wait for result
	if wait {
		result, err := t.manager.WaitForResult(taskID, 60)
		if err != nil {
			// Return current status if timeout
			return t.buildTaskResponse(task, nil), nil
		}
		return t.buildTaskResponse(task, result), nil
	}

	// Get result if available
	result := t.manager.GetResult(taskID)
	return t.buildTaskResponse(task, result), nil
}

func (t *PollTaskTool) buildTaskResponse(task *subagent.Task, result *subagent.Result) map[string]interface{} {
	resp := map[string]interface{}{
		"task_id":    task.ID,
		"status":     task.GetStatus(),
		"created_at": task.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}

	if task.StartedAt != nil {
		resp["started_at"] = task.StartedAt.Format("2006-01-02T15:04:05Z")
	}

	if task.CompletedAt != nil {
		resp["completed_at"] = task.CompletedAt.Format("2006-01-02T15:04:05Z")
	}

	if task.ParentID != "" {
		resp["parent_id"] = task.ParentID
		resp["depth"] = task.Depth
	}

	if result != nil {
		resp["duration"] = result.Duration.String()
		if result.Success {
			resp["result"] = result.Output
		} else {
			resp["error"] = result.Error
		}
	} else {
		switch task.GetStatus() {
		case subagent.TaskStatusPending:
			resp["message"] = "Task is queued and waiting to start..."
		case subagent.TaskStatusRunning:
			if task.StartedAt != nil {
				resp["message"] = fmt.Sprintf("Task is running for %s...", task.CreatedAt.Sub(*task.StartedAt).Round(60))
			} else {
				resp["message"] = "Task is running..."
			}
		}
	}

	return resp
}

// ListTasksTool lists all sub-agent tasks
type ListTasksTool struct {
	baseTool *BaseTool
	manager  *subagent.Manager
}

var _ Tool = (*ListTasksTool)(nil)

// NewListTasksTool creates a new list tasks tool
func NewListTasksTool(manager *subagent.Manager) *ListTasksTool {
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
						"enum": []string{"all", "pending", "running", "completed", "failed", "cancelled"},
						"description": "Filter by task status",
						"default":     "all",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of tasks to return",
						"default":     50,
					},
				},
			},
		),
		manager: manager,
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
	if t.manager == nil {
		return nil, fmt.Errorf("subagent manager not initialized")
	}

	statusFilter := subagent.TaskStatus("")
	if s, ok := params["status"].(string); ok && s != "all" {
		statusFilter = subagent.TaskStatus(s)
	}

	limit := 50
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
		if limit < 1 {
			limit = 1
		}
		if limit > 100 {
			limit = 100
		}
	}

	tasks := t.manager.ListTasks(statusFilter)

	// Count by status
	stats := t.manager.GetStats()

	var taskList []map[string]interface{}
	for i, task := range tasks {
		if i >= limit {
			break
		}

		taskInfo := map[string]interface{}{
			"task_id":     task.ID,
			"status":      task.GetStatus(),
			"description": truncateString(task.Description, 100),
			"created_at":  task.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}

		if task.StartedAt != nil {
			taskInfo["started_at"] = task.StartedAt.Format("2006-01-02T15:04:05Z")
		}

		if task.CompletedAt != nil {
			taskInfo["completed_at"] = task.CompletedAt.Format("2006-01-02T15:04:05Z")
		}

		if task.ParentID != "" {
			taskInfo["parent_id"] = task.ParentID
			taskInfo["depth"] = task.Depth
		}

		taskList = append(taskList, taskInfo)
	}

	return map[string]interface{}{
		"total":         len(tasks),
		"returned":      len(taskList),
		"status_counts": stats["status_counts"],
		"tasks":         taskList,
	}, nil
}

// CancelTaskTool cancels a running sub-agent task
type CancelTaskTool struct {
	baseTool *BaseTool
	manager  *subagent.Manager
}

var _ Tool = (*CancelTaskTool)(nil)

// NewCancelTaskTool creates a new cancel task tool
func NewCancelTaskTool(manager *subagent.Manager) *CancelTaskTool {
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
		manager: manager,
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
	if t.manager == nil {
		return nil, fmt.Errorf("subagent manager not initialized")
	}

	taskID, ok := params["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id parameter is required")
	}

	reason := ""
	if r, ok := params["reason"].(string); ok {
		reason = r
	}

	err := t.manager.CancelTask(taskID)
	if err != nil {
		return nil, err
	}

	resp := map[string]interface{}{
		"task_id":   taskID,
		"status":    "cancelled",
		"message":   "Task has been cancelled",
		"cancelled": "2006-01-02T15:04:05Z",
	}

	if reason != "" {
		resp["reason"] = reason
	}

	return resp, nil
}

// Helper functions

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RegisterSubAgentTools registers all subagent tools with the registry
func RegisterSubAgentTools(registry *Registry, manager *subagent.Manager) {
	if manager == nil {
		return
	}

	registry.Register(NewDelegateTaskTool(manager))
	registry.Register(NewPollTaskTool(manager))
	registry.Register(NewListTasksTool(manager))
	registry.Register(NewCancelTaskTool(manager))
}
