package tool

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/magicwubiao/go-magic/internal/kanban"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// KanbanManager is the global kanban manager instance
var KanbanManager *kanban.Manager

// KanbanShowTool shows the current task details
type KanbanShowTool struct {
	*BaseTool
}

// NewKanbanShowTool creates a new kanban show tool
func NewKanbanShowTool() *KanbanShowTool {
	return &KanbanShowTool{
		BaseTool: NewBaseTool(
			"kanban_show",
			"Show the current task details. Use this to see the full context of your assigned task including description, comments, and dependencies.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to show. If not provided, uses KANBAN_TASK environment variable.",
					},
				},
			},
		),
	}
}

// Execute shows the task details
func (t *KanbanShowTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID := os.Getenv("KANBAN_TASK")
	if tid, ok := params["task_id"].(string); ok && tid != "" {
		taskID = tid
	}

	if taskID == "" {
		return nil, fmt.Errorf("no task ID provided (use task_id param or set KANBAN_TASK env var)")
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	task, err := KanbanManager.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	// Get comments
	comments, _ := KanbanManager.ListComments(taskID)

	// Get parents
	parents, _ := KanbanManager.GetParents(taskID)

	// Get children
	children, _ := KanbanManager.GetChildren(taskID)

	result := map[string]interface{}{
		"task":     task,
		"comments": comments,
		"parents":  parents,
		"children": children,
	}

	return result, nil
}

// KanbanCompleteTool completes the current task
type KanbanCompleteTool struct {
	*BaseTool
}

// NewKanbanCompleteTool creates a new kanban complete tool
func NewKanbanCompleteTool() *KanbanCompleteTool {
	return &KanbanCompleteTool{
		BaseTool: NewBaseTool(
			"kanban_complete",
			"Mark the current task as completed. Provide a summary of what was accomplished.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to complete. If not provided, uses KANBAN_TASK environment variable.",
					},
					"summary": map[string]interface{}{
						"type":        "string",
						"description": "Summary of what was accomplished",
					},
					"result": map[string]interface{}{
						"type":        "string",
						"description": "Optional detailed result or output",
					},
				},
				"required": []string{"summary"},
			},
		),
	}
}

// Execute completes a task
func (t *KanbanCompleteTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID := os.Getenv("KANBAN_TASK")
	if tid, ok := params["task_id"].(string); ok && tid != "" {
		taskID = tid
	}

	if taskID == "" {
		return nil, fmt.Errorf("no task ID provided")
	}

	summary, _ := params["summary"].(string)
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	task, err := KanbanManager.CompleteTask(taskID, summary)
	if err != nil {
		return nil, fmt.Errorf("failed to complete task: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"task":    task,
		"message": fmt.Sprintf("Task %s completed: %s", taskID, summary),
	}, nil
}

// KanbanBlockTool blocks the current task
type KanbanBlockTool struct {
	*BaseTool
}

// NewKanbanBlockTool creates a new kanban block tool
func NewKanbanBlockTool() *KanbanBlockTool {
	return &KanbanBlockTool{
		BaseTool: NewBaseTool(
			"kanban_block",
			"Mark the current task as blocked. Use when you cannot proceed due to external dependencies or missing information.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to block. If not provided, uses KANBAN_TASK environment variable.",
					},
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Reason for blocking the task",
					},
				},
				"required": []string{"reason"},
			},
		),
	}
}

// Execute blocks a task
func (t *KanbanBlockTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID := os.Getenv("KANBAN_TASK")
	if tid, ok := params["task_id"].(string); ok && tid != "" {
		taskID = tid
	}

	if taskID == "" {
		return nil, fmt.Errorf("no task ID provided")
	}

	reason, _ := params["reason"].(string)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	task, err := KanbanManager.BlockTask(taskID, reason)
	if err != nil {
		return nil, fmt.Errorf("failed to block task: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"task":    task,
		"message": fmt.Sprintf("Task %s blocked: %s", taskID, reason),
	}, nil
}

// KanbanHeartbeatTool reports task heartbeat
type KanbanHeartbeatTool struct {
	*BaseTool
}

// NewKanbanHeartbeatTool creates a new kanban heartbeat tool
func NewKanbanHeartbeatTool() *KanbanHeartbeatTool {
	return &KanbanHeartbeatTool{
		BaseTool: NewBaseTool(
			"kanban_heartbeat",
			"Report a heartbeat for the current task. Use periodically to indicate the task is still being worked on.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID for heartbeat. If not provided, uses KANBAN_TASK environment variable.",
					},
				},
			},
		),
	}
}

// Execute reports a heartbeat
func (t *KanbanHeartbeatTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID := os.Getenv("KANBAN_TASK")
	if tid, ok := params["task_id"].(string); ok && tid != "" {
		taskID = tid
	}

	if taskID == "" {
		return nil, fmt.Errorf("no task ID provided")
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	if err := KanbanManager.Heartbeat(taskID); err != nil {
		return nil, fmt.Errorf("failed to report heartbeat: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Heartbeat recorded for task %s", taskID),
	}, nil
}

// KanbanCommentTool adds a comment to a task
type KanbanCommentTool struct {
	*BaseTool
}

// NewKanbanCommentTool creates a new kanban comment tool
func NewKanbanCommentTool() *KanbanCommentTool {
	return &KanbanCommentTool{
		BaseTool: NewBaseTool(
			"kanban_comment",
			"Add a comment to a task. Use to communicate with other agents, leave notes, or document progress.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to comment on",
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Author name (defaults to current agent profile)",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Comment text",
					},
				},
				"required": []string{"task_id", "body"},
			},
		),
	}
}

// Execute adds a comment
func (t *KanbanCommentTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	body, _ := params["body"].(string)
	if body == "" {
		return nil, fmt.Errorf("body is required")
	}

	author := "agent"
	if a, ok := params["author"].(string); ok && a != "" {
		author = a
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	comment, err := KanbanManager.AddComment(taskID, author, body)
	if err != nil {
		return nil, fmt.Errorf("failed to add comment: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"comment": comment,
		"message": fmt.Sprintf("Comment added to task %s", taskID),
	}, nil
}

// KanbanCreateTool creates a new task
type KanbanCreateTool struct {
	*BaseTool
}

// NewKanbanCreateTool creates a new kanban create tool
func NewKanbanCreateTool() *KanbanCreateTool {
	return &KanbanCreateTool{
		BaseTool: NewBaseTool(
			"kanban_create",
			"Create a new task. Use to break down work into smaller tasks or create sub-tasks.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Task title",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Task description",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Task assignee (agent profile name)",
					},
					"parent_id": map[string]interface{}{
						"type":        "string",
						"description": "Parent task ID for dependency",
					},
					"priority": map[string]interface{}{
						"type":        "integer",
						"description": "Priority: 0=low, 1=medium, 2=high, 3=critical",
					},
				},
				"required": []string{"title"},
			},
		),
	}
}

// Execute creates a task
func (t *KanbanCreateTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	title, _ := params["title"].(string)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	body, _ := params["body"].(string)
	assignee, _ := params["assignee"].(string)
	parentID, _ := params["parent_id"].(string)

	var opts []kanban.TaskOption
	if priority, ok := params["priority"].(float64); ok {
		opts = append(opts, kanban.WithPriority(int(priority)))
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	var task *kanban.Task
	var err error

	if parentID != "" {
		task, err = KanbanManager.CreateTaskWithParent(title, body, assignee, parentID, opts...)
	} else {
		task, err = KanbanManager.CreateTask(title, body, assignee, opts...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"task":    task,
		"message": fmt.Sprintf("Task created: %s", task.ID),
	}, nil
}

// KanbanLinkTool links two tasks as parent-child
type KanbanLinkTool struct {
	*BaseTool
}

// NewKanbanLinkTool creates a new kanban link tool
func NewKanbanLinkTool() *KanbanLinkTool {
	return &KanbanLinkTool{
		BaseTool: NewBaseTool(
			"kanban_link",
			"Link two tasks as parent-child dependency. The child task cannot start until the parent is done.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"parent_id": map[string]interface{}{
						"type":        "string",
						"description": "Parent task ID (must be done before child)",
					},
					"child_id": map[string]interface{}{
						"type":        "string",
						"description": "Child task ID (depends on parent)",
					},
				},
				"required": []string{"parent_id", "child_id"},
			},
		),
	}
}

// Execute links two tasks
func (t *KanbanLinkTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	parentID, _ := params["parent_id"].(string)
	childID, _ := params["child_id"].(string)

	if parentID == "" || childID == "" {
		return nil, fmt.Errorf("both parent_id and child_id are required")
	}

	if KanbanManager == nil {
		return nil, fmt.Errorf("kanban manager not initialized")
	}

	if err := KanbanManager.AddLink(parentID, childID); err != nil {
		return nil, fmt.Errorf("failed to link tasks: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"parent":  parentID,
		"child":   childID,
		"message": fmt.Sprintf("Linked %s -> %s", parentID, childID),
	}, nil
}

// RegisterKanbanTools registers kanban tools to the registry
// Tools are only registered when KANBAN_TASK environment variable is set
func RegisterKanbanTools(registry *Registry) {
	// Only register if KANBAN_TASK is set
	if os.Getenv("KANBAN_TASK") == "" {
		log.Debugf("[Kanban] KANBAN_TASK not set, skipping kanban tool registration")
		return
	}

	log.Infof("[Kanban] Registering kanban tools (task: %s)", os.Getenv("KANBAN_TASK"))

	registry.Register(NewKanbanShowTool())
	registry.Register(NewKanbanCompleteTool())
	registry.Register(NewKanbanBlockTool())
	registry.Register(NewKanbanHeartbeatTool())
	registry.Register(NewKanbanCommentTool())
	registry.Register(NewKanbanCreateTool())
	registry.Register(NewKanbanLinkTool())
}

// InitKanbanManager initializes the global kanban manager
func InitKanbanManager(homeDir string) error {
	if KanbanManager != nil {
		return nil // Already initialized
	}

	manager, err := kanban.NewManager(homeDir)
	if err != nil {
		return fmt.Errorf("failed to create kanban manager: %w", err)
	}

	if err := manager.Init(); err != nil {
		return fmt.Errorf("failed to init kanban manager: %w", err)
	}

	KanbanManager = manager
	log.Infof("[Kanban] Manager initialized")
	return nil
}

// CloseKanbanManager closes the global kanban manager
func CloseKanbanManager() error {
	if KanbanManager == nil {
		return nil
	}

	err := KanbanManager.Close()
	KanbanManager = nil
	return err
}

// ParseTaskStatus parses a string into TaskStatus
func ParseTaskStatus(s string) kanban.TaskStatus {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "triage":
		return kanban.StatusTriage
	case "todo":
		return kanban.StatusTodo
	case "ready":
		return kanban.StatusReady
	case "running":
		return kanban.StatusRunning
	case "blocked":
		return kanban.StatusBlocked
	case "done":
		return kanban.StatusDone
	case "archived":
		return kanban.StatusArchived
	default:
		return kanban.StatusTriage
	}
}
