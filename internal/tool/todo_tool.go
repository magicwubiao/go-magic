package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TodoItem represents a single todo item
type TodoItem struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`             // pending, in_progress, completed, cancelled
	Priority    string     `json:"priority,omitempty"` // low, medium, high
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// TodoTool manages todo items
type TodoTool struct {
	mu       sync.RWMutex
	todos    map[string]*TodoItem
	dataFile string
}

var (
	todoOnce sync.Once
	todoTool *TodoTool
)

// GetTodoTool returns the singleton todo tool
func GetTodoTool() *TodoTool {
	todoOnce.Do(func() {
		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".magic", "todos")
		os.MkdirAll(dataDir, 0755)

		todoTool = &TodoTool{
			todos:    make(map[string]*TodoItem),
			dataFile: filepath.Join(dataDir, "todos.json"),
		}
		todoTool.load()
	})
	return todoTool
}

func (t *TodoTool) load() {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := os.ReadFile(t.dataFile)
	if err != nil {
		return
	}

	var todos []*TodoItem
	if err := json.Unmarshal(data, &todos); err != nil {
		return
	}

	for _, todo := range todos {
		t.todos[todo.ID] = todo
	}
}

func (t *TodoTool) save() error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	todos := make([]*TodoItem, 0, len(t.todos))
	for _, todo := range t.todos {
		todos = append(todos, todo)
	}

	data, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(t.dataFile, data, 0644)
}

// Name returns the tool name
func (t *TodoTool) Name() string {
	return "todo"
}

// Description returns the tool description
func (t *TodoTool) Description() string {
	return "Manage todo items. Use this to create, list, update, or delete todo items. Supports priorities and status tracking."
}

// Parameters returns the tool parameters schema
func (t *TodoTool) Schema() map[string]interface{} { return t.Parameters() }

func (t *TodoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: create, list, update, delete, complete",
				"enum":        []string{"create", "list", "update", "delete", "complete"},
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Todo item ID (required for update, delete, complete)",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Title of the todo item (required for create)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Detailed description of the todo item",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Priority level: low, medium, high",
				"enum":        []string{"low", "medium", "high"},
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "Status: pending, in_progress, completed, cancelled",
				"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
			},
		},
		"required": []string{"action"},
	}
}

// Execute performs the todo action
func (t *TodoTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "create":
		return t.createTodo(args)
	case "list":
		return t.listTodos(args)
	case "update":
		return t.updateTodo(args)
	case "delete":
		return t.deleteTodo(args)
	case "complete":
		return t.completeTodo(args)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (t *TodoTool) createTodo(args map[string]interface{}) (interface{}, error) {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required for create")
	}

	now := time.Now()
	todo := &TodoItem{
		ID:        fmt.Sprintf("todo_%d", now.UnixNano()),
		Title:     title,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if desc, ok := args["description"].(string); ok {
		todo.Description = desc
	}

	if priority, ok := args["priority"].(string); ok {
		todo.Priority = priority
	}

	t.mu.Lock()
	t.todos[todo.ID] = todo
	t.mu.Unlock()

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	return map[string]interface{}{
		"id":      todo.ID,
		"title":   todo.Title,
		"status":  todo.Status,
		"message": "Todo created successfully",
	}, nil
}

func (t *TodoTool) listTodos(args map[string]interface{}) (interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	todos := make([]map[string]interface{}, 0, len(t.todos))
	for _, todo := range t.todos {
		todos = append(todos, map[string]interface{}{
			"id":         todo.ID,
			"title":      todo.Title,
			"status":     todo.Status,
			"priority":   todo.Priority,
			"created_at": todo.CreatedAt.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"total": len(todos),
		"todos": todos,
	}, nil
}

func (t *TodoTool) updateTodo(args map[string]interface{}) (interface{}, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required for update")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	todo, exists := t.todos[id]
	if !exists {
		return nil, fmt.Errorf("todo not found: %s", id)
	}

	if title, ok := args["title"].(string); ok && title != "" {
		todo.Title = title
	}

	if desc, ok := args["description"].(string); ok {
		todo.Description = desc
	}

	if status, ok := args["status"].(string); ok && status != "" {
		todo.Status = status
	}

	if priority, ok := args["priority"].(string); ok {
		todo.Priority = priority
	}

	todo.UpdatedAt = time.Now()

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	return map[string]interface{}{
		"id":      todo.ID,
		"title":   todo.Title,
		"status":  todo.Status,
		"message": "Todo updated successfully",
	}, nil
}

func (t *TodoTool) deleteTodo(args map[string]interface{}) (interface{}, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required for delete")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.todos[id]; !exists {
		return nil, fmt.Errorf("todo not found: %s", id)
	}

	delete(t.todos, id)

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	return map[string]interface{}{
		"id":      id,
		"message": "Todo deleted successfully",
	}, nil
}

func (t *TodoTool) completeTodo(args map[string]interface{}) (interface{}, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required for complete")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	todo, exists := t.todos[id]
	if !exists {
		return nil, fmt.Errorf("todo not found: %s", id)
	}

	now := time.Now()
	todo.Status = "completed"
	todo.CompletedAt = &now
	todo.UpdatedAt = now

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	return map[string]interface{}{
		"id":      todo.ID,
		"title":   todo.Title,
		"status":  todo.Status,
		"message": "Todo completed successfully",
	}, nil
}

