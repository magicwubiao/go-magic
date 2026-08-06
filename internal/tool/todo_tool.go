package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
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

// 合法状态/优先级（与 Parameters() 的 enum 保持一致，Execute 时校验防脏数据）
var (
	validStatuses   = map[string]bool{"pending": true, "in_progress": true, "completed": true, "cancelled": true}
	validPriorities = map[string]bool{"low": true, "medium": true, "high": true}
)

// GetTodoTool returns the singleton todo tool
func GetTodoTool() *TodoTool {
	todoOnce.Do(func() {
		dataDir := filepath.Join(config.GetMagicHome(), "todos")
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
		if !os.IsNotExist(err) {
			// 文件存在但读取失败：记录警告，以空列表启动，但不主动覆盖原文件
			log.Printf("[TODO] failed to read %s: %v (starting with empty list, file preserved)", t.dataFile, err)
		}
		return
	}

	var todos []*TodoItem
	if err := json.Unmarshal(data, &todos); err != nil {
		// 解析失败：保留磁盘原文件不动，内存为空，避免下次 save 把空数据覆盖回去造成二次丢失
		log.Printf("[TODO] failed to parse %s: %v (starting with empty list, file preserved)", t.dataFile, err)
		return
	}

	for _, todo := range todos {
		t.todos[todo.ID] = todo
	}
}

// save 持久化 todos 到磁盘。调用方必须已持有 t.mu 锁（读或写）。
// 注意：不能在此处再加 RLock——update/delete/complete 在写锁内调用 save，
// sync.RWMutex 不可重入，写锁持有期间获取读锁会永久阻塞（曾导致 todo 工具 60s 超时）。
// 采用「写临时文件 + rename」原子写，避免写入中途崩溃导致 todos.json 损坏。
func (t *TodoTool) save() error {
	todos := make([]*TodoItem, 0, len(t.todos))
	for _, todo := range t.todos {
		todos = append(todos, todo)
	}

	data, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return err
	}

	tmp := t.dataFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, t.dataFile)
}

// Name returns the tool name
func (t *TodoTool) Name() string {
	return "todo"
}

// Description returns the tool description
func (t *TodoTool) Description() string {
	return "Task planning and tracking tool. Use this to break down complex tasks into manageable steps. " +
		"WHEN TO USE: When the user asks for something requiring 3+ steps; Before starting a multi-step workflow (coding, research, analysis); To track progress on long-running tasks. " +
		"HOW TO USE: 1) First call action=create to add each step, 2) Call action=list to show progress, 3) Call action=complete when done, 4) Call action=update if plans change. " +
		"EXAMPLE: User says 'Build a login page' -> Create todos for: Design form, Add validation, Connect API, Test -> Complete each as you finish."
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
		// 用 base36 编码时间戳，避免纯数字 ID 被 PII 脱敏器误判为手机号
		// （如 todo_1784515845... 中的子串匹配 1[3-9]\d{9} 被替换为 [PHONE]）
		ID:        fmt.Sprintf("todo_%s", strconv.FormatInt(now.UnixNano(), 36)),
		Title:     title,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if desc, ok := args["description"].(string); ok {
		todo.Description = desc
	}

	if priority, ok := args["priority"].(string); ok && priority != "" {
		if !validPriorities[priority] {
			return nil, fmt.Errorf("invalid priority: %s (allowed: low, medium, high)", priority)
		}
		todo.Priority = priority
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.todos[todo.ID] = todo

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

	// 收集后按创建时间稳定排序，避免 map 遍历随机顺序影响可读性
	items := make([]*TodoItem, 0, len(t.todos))
	for _, todo := range t.todos {
		items = append(items, todo)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})

	todos := make([]map[string]interface{}, 0, len(items))
	for _, todo := range items {
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
		if !validStatuses[status] {
			return nil, fmt.Errorf("invalid status: %s (allowed: pending, in_progress, completed, cancelled)", status)
		}
		todo.Status = status
	}

	if priority, ok := args["priority"].(string); ok && priority != "" {
		if !validPriorities[priority] {
			return nil, fmt.Errorf("invalid priority: %s (allowed: low, medium, high)", priority)
		}
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
