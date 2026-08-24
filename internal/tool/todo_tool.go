package tool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// TodoChangeNotifier 是可选的回调，TodoTool 发生写操作（创建/更新/删除/完成）后调用。
// 默认用 DefaultTodoChangeNotifier（nil，静默）。
// 在 server 启动时会把它替换为发送 SSE 事件的实现，让前端侧边栏实时刷新。
type TodoChangeNotifier interface {
	NotifyTodoChanged(changedID string, action string)
}

var (
	todoChangeNotifierMu sync.RWMutex
	defaultTodoNotifier  TodoChangeNotifier = nil
)

// SetDefaultTodoChangeNotifier 设置进程内全局的 todo 变更通知器。
func SetDefaultTodoChangeNotifier(n TodoChangeNotifier) {
	todoChangeNotifierMu.Lock()
	defer todoChangeNotifierMu.Unlock()
	defaultTodoNotifier = n
}

func getTodoChangeNotifier() TodoChangeNotifier {
	todoChangeNotifierMu.RLock()
	defer todoChangeNotifierMu.RUnlock()
	return defaultTodoNotifier
}

// GlobalBusOrDefaultTodoNotifier 是基于 bus.EventBus 的通知器实现（供 server 注册使用）。
type GlobalBusOrDefaultTodoNotifier struct {
	Bus *bus.EventBus
}

func (n *GlobalBusOrDefaultTodoNotifier) NotifyTodoChanged(changedID string, action string) {
	if n == nil || n.Bus == nil {
		return
	}
	n.Bus.Emit(bus.Event{
		Kind: bus.EventKindTodoUpdate,
		Time: time.Now(),
		Data: map[string]interface{}{
			"id":     changedID,
			"action": action,
		},
	})
}

// broadcastTodoChanged 是 todoTool 内部统一的广播封装：
// 优先走进程内 notifier；如果没注册 notifier，但 bus 存在全局实例（未来扩展），也可兜底；
// 这里保持低耦合，失败不影响原有写操作返回值。
func broadcastTodoChanged(changedID, action string) {
	defer func() { _ = recover() }()
	if n := getTodoChangeNotifier(); n != nil {
		n.NotifyTodoChanged(changedID, action)
	}
}

// TodoItem represents a single todo item
type TodoItem struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`             // pending, in_progress, completed, cancelled
	Priority    string     `json:"priority,omitempty"` // low, medium, high
	SessionID   string     `json:"session_id,omitempty"`
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
	// priorityRank sorts "high" first when doing priority-ordered list output.
	priorityRank = map[string]int{"high": 3, "medium": 2, "low": 1, "": 0}
)

// GetTodoTool returns the singleton todo tool
func GetTodoTool() *TodoTool {
	todoOnce.Do(func() {
		dataDir := filepath.Join(config.GetMagicHome(), "todos")
		_ = os.MkdirAll(dataDir, defaultFileSecurity().DefaultDirMode)

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
	if err := os.WriteFile(tmp, data, defaultFileSecurity().DefaultFileMode); err != nil {
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
				"description": "Title of the todo item (required for create; optional for update). Pass empty string on update to clear the existing field explicitly.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Detailed description of the todo item. Pass empty string on update to clear the description explicitly.",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Priority level: low, medium, high. Pass empty string on update to clear priority (no priority).",
				"enum":        []string{"low", "medium", "high"},
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "Status: pending, in_progress, completed, cancelled. Setting status=completed via update behaves exactly like action=complete (also sets completed_at timestamp).",
				"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
			},
			"filter_status": map[string]interface{}{
				"type":        "string",
				"description": "Only for action=list. Filter by status: pending/in_progress/completed/cancelled. Optional.",
				"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
			},
			"filter_priority": map[string]interface{}{
				"type":        "string",
				"description": "Only for action=list. Filter by priority: low/medium/high. Optional.",
				"enum":        []string{"low", "medium", "high"},
			},
			"sort": map[string]interface{}{
				"type":        "string",
				"description": "Only for action=list. Sort order: created_asc (default), created_desc, priority_desc, updated_desc.",
				"enum":        []string{"created_asc", "created_desc", "priority_desc", "updated_desc"},
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

	// 合并会话上下文：如果 ctx 里有 session_id，但 args 未显式指定，则注入。
	// 这样 LLM 在某个会话里调用 todo 工具时，写操作会自动归属该会话、读操作自动过滤。
	if _, has := args["session_id"]; !has {
		if sid := SessionIDFromContext(ctx); sid != "" {
			args["session_id"] = sid
		}
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

// newTodoID generates a collision-resistant ID.
// We previously used UnixNano in base36 only -- on Windows (time resolution ~15ms)
// and even on Linux inside tight loops this could return duplicates, causing later
// createTodo calls to silently overwrite the earlier entry (total data loss).
// We now append 4 random bytes (8 hex chars) as a collision guard:
// "todo_{base36(nanos)}_{rand8}" -> effectively unique even at 1M creates/sec.
func newTodoID(now time.Time) string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback (extremely rare: crypto/rand broken): encode nanos twice with salt
		return fmt.Sprintf("todo_%x%x", now.UnixNano(), now.Nanosecond()^0x9e3779b9)
	}
	return fmt.Sprintf("todo_%s_%s",
		toBase36(now.UnixNano()),
		hex.EncodeToString(buf[:]))
}

// toBase36 mirrors the old strconv.FormatInt(i,36) but avoids pulling in
// the now-removed strconv import; equivalent semantics to the prior ID prefix.
func toBase36(i int64) string {
	if i == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	neg := i < 0
	if neg {
		i = -i
	}
	var b [64]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%36]
		i /= 36
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func (t *TodoTool) createTodo(args map[string]interface{}) (interface{}, error) {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required for create")
	}

	now := time.Now()
	todo := &TodoItem{
		// Collision-resistant ID (see newTodoID doc)
		ID:        newTodoID(now),
		Title:     title,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if sid, ok := args["session_id"].(string); ok && sid != "" {
		todo.SessionID = sid
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

	// 同 session_id 范围下按标题去重：如果已经有同标题且状态仍为 pending/in_progress 的项，
	// 不创建新条目，直接返回现有条目（附带 deduplicated:true 标记）。
	// 这样 LLM 在持续对话中反复调用 action=create 同一个步骤标题时，不会产生重复待办。
	// 已 completed/cancelled 的同名项不阻挡，允许重新开启新任务。
	for _, existing := range t.todos {
		if existing.SessionID != todo.SessionID {
			continue
		}
		if strings.EqualFold(existing.Title, todo.Title) &&
			(existing.Status == "pending" || existing.Status == "in_progress") {
			// 触发一次 update 广播，让前端侧边栏刷新（理论上内容没变化，但确保 UI 同步）。
			broadcastTodoChanged(existing.ID, "update")
			resp := map[string]interface{}{
				"id":           existing.ID,
				"title":        existing.Title,
				"status":       existing.Status,
				"deduplicated": true,
				"message":      "Todo with same title already active; returning existing entry",
			}
			if existing.Priority != "" {
				resp["priority"] = existing.Priority
			}
			if existing.SessionID != "" {
				resp["session_id"] = existing.SessionID
			}
			return resp, nil
		}
	}

	// Final collision guard: just in case clock+rand somehow repeat for two tasks,
	// append a counter suffix until the slot is free.
	baseID := todo.ID
	suffix := 1
	for _, exists := t.todos[todo.ID]; exists; _, exists = t.todos[todo.ID] {
		todo.ID = fmt.Sprintf("%s_%d", baseID, suffix)
		suffix++
	}

	t.todos[todo.ID] = todo

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}
	broadcastTodoChanged(todo.ID, "create")

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

	// Optional server-side filters so the LLM doesn't have to pull the entire
	// list just to find "pending high-priority items", which is the #1 query.
	filterStatus, _ := args["filter_status"].(string)
	filterPriority, _ := args["filter_priority"].(string)
	filterSession, _ := args["session_id"].(string)
	// "filter_session" 是给 HTTP API 用的显式参数名，和 Execute 注入的 "session_id" 同义
	if v, ok := args["filter_session"].(string); ok && v != "" {
		filterSession = v
	}
	sortMode, _ := args["sort"].(string)
	if sortMode == "" {
		sortMode = "created_asc"
	}

	pendingCount := 0
	inProgressCount := 0
	completedCount := 0
	totalCount := 0

	// 会话过滤分两种语义（由 filterSessionExplicitRequested 区分）：
	//   - 显式传了 session_id / filter_session（哪怕是空串 ""）：严格按传入值过滤。
	//     空串意味着"只看全局 / 未归属任何会话的 todo"，避免首页漏会话时把所有会话混在一起展示。
	//   - 完全没传会话参数（Execute 内也没有 ctx 注入）：返回全部（兜底语义，仅当外部调用者完全未感知 session 时启用）。
	hasSessionArg := false
	if _, ok := args["session_id"]; ok {
		hasSessionArg = true
	}
	if _, ok := args["filter_session"]; ok {
		hasSessionArg = true
	}

	items := make([]*TodoItem, 0, len(t.todos))
	for _, todo := range t.todos {
		if hasSessionArg {
			if todo.SessionID != filterSession {
				continue
			}
		}
		if filterStatus != "" && todo.Status != filterStatus {
			continue
		}
		if filterPriority != "" && todo.Priority != filterPriority {
			continue
		}
		switch todo.Status {
		case "pending":
			pendingCount++
		case "in_progress":
			inProgressCount++
		case "completed":
			completedCount++
		}
		totalCount++
		items = append(items, todo)
	}

	switch sortMode {
	case "created_desc":
		sort.Slice(items, func(i, j int) bool { return items[j].CreatedAt.Before(items[i].CreatedAt) })
	case "priority_desc":
		// Sort by priority (high first), tie-break by creation time.
		sort.Slice(items, func(i, j int) bool {
			ri, rj := priorityRank[items[i].Priority], priorityRank[items[j].Priority]
			if ri != rj {
				return ri > rj
			}
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		})
	case "updated_desc":
		sort.Slice(items, func(i, j int) bool { return items[j].UpdatedAt.Before(items[i].UpdatedAt) })
	default: // created_asc
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	}

	todos := make([]map[string]interface{}, 0, len(items))
	for _, todo := range items {
		row := map[string]interface{}{
			"id":         todo.ID,
			"title":      todo.Title,
			"status":     todo.Status,
			"priority":   todo.Priority,
			"created_at": todo.CreatedAt.Format(time.RFC3339),
			"updated_at": todo.UpdatedAt.Format(time.RFC3339),
		}
		if todo.SessionID != "" {
			row["session_id"] = todo.SessionID
		}
		if todo.Description != "" {
			row["description"] = todo.Description
		}
		if todo.CompletedAt != nil {
			row["completed_at"] = todo.CompletedAt.Format(time.RFC3339)
		}
		todos = append(todos, row)
	}

	return map[string]interface{}{
		"total":             totalCount,
		"todos":             todos,
		"pending_count":     pendingCount,
		"in_progress_count": inProgressCount,
		"completed_count":   completedCount,
		"filter_status":     filterStatus,
		"filter_priority":   filterPriority,
		"filter_session_id": filterSession,
		"sort":              sortMode,
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

	// 会话边界保护：如果调用方提供了 session_id（来自 ctx 或显式参数），
	// 且 todo 本身已归属其他会话，则拒绝修改，避免串会话改数据。
	if callerSession, _ := args["session_id"].(string); callerSession != "" && todo.SessionID != "" && todo.SessionID != callerSession {
		return nil, fmt.Errorf("todo not found: %s", id)
	}
	sessionID := todo.SessionID

	// CONSISTENT "key present in args" semantics for every mutable field.
	// Previous behavior was asymmetric: title/priority required "!= empty"
	// (so you could never clear them) while description accepted "" as clear.
	// New rule: if the key is in args AT ALL (checked via comma-ok on the map),
	// the provided value is applied verbatim -- including empty string, which
	// is interpreted as the LLM explicitly requesting to clear that field.
	// If the key is NOT in args, we leave the existing value untouched.
	if _, hasTitle := args["title"]; hasTitle {
		if v, ok := args["title"].(string); ok {
			todo.Title = v
		}
	}
	if _, hasDesc := args["description"]; hasDesc {
		if v, ok := args["description"].(string); ok {
			todo.Description = v
		}
	}

	changedToCompleted := false
	changedToTerminal := false
	if _, hasStatus := args["status"]; hasStatus {
		if status, ok := args["status"].(string); ok && status != "" {
			if !validStatuses[status] {
				return nil, fmt.Errorf("invalid status: %s (allowed: pending, in_progress, completed, cancelled)", status)
			}
			prev := todo.Status
			todo.Status = status
			if status == "completed" && prev != "completed" {
				changedToCompleted = true
				changedToTerminal = true
			}
			if status == "cancelled" && prev != "cancelled" {
				changedToTerminal = true
			}
			if status != "completed" {
				// Moving OUT of completed: clear the completed_at timestamp so
				// data stays consistent with action=complete's invariants.
				todo.CompletedAt = nil
			}
		}
	}

	if _, hasPriority := args["priority"]; hasPriority {
		if priority, ok := args["priority"].(string); ok {
			if priority != "" && !validPriorities[priority] {
				return nil, fmt.Errorf("invalid priority: %s (allowed: low, medium, high)", priority)
			}
			todo.Priority = priority
		}
	}

	now := time.Now()
	todo.UpdatedAt = now
	if changedToCompleted {
		todo.CompletedAt = &now
	}

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}
	broadcastTodoChanged(todo.ID, "update")
	if changedToTerminal {
		t.cleanupSessionIfAllDoneLocked(sessionID)
	}

	resp := map[string]interface{}{
		"id":         todo.ID,
		"title":      todo.Title,
		"status":     todo.Status,
		"updated_at": todo.UpdatedAt.Format(time.RFC3339),
		"message":    "Todo updated successfully",
	}
	if todo.CompletedAt != nil {
		resp["completed_at"] = todo.CompletedAt.Format(time.RFC3339)
	}
	return resp, nil
}

func (t *TodoTool) deleteTodo(args map[string]interface{}) (interface{}, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required for delete")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	todo, exists := t.todos[id]
	if !exists {
		return nil, fmt.Errorf("todo not found: %s", id)
	}

	// 会话边界保护（同 updateTodo）
	if callerSession, _ := args["session_id"].(string); callerSession != "" && todo.SessionID != "" && todo.SessionID != callerSession {
		return nil, fmt.Errorf("todo not found: %s", id)
	}
	sessionID := todo.SessionID

	delete(t.todos, id)

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}
	broadcastTodoChanged(id, "delete")
	t.cleanupSessionIfAllDoneLocked(sessionID)

	return map[string]interface{}{
		"id":      id,
		"message": "Todo deleted successfully",
	}, nil
}

func (t *TodoTool) cleanupSessionIfAllDoneLocked(sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	bucket := make([]*TodoItem, 0, 8)
	for _, todo := range t.todos {
		if todo.SessionID == sessionID {
			bucket = append(bucket, todo)
		}
	}
	if len(bucket) == 0 {
		return nil
	}
	for _, todo := range bucket {
		if todo.Status == "pending" || todo.Status == "in_progress" {
			return nil
		}
	}

	removedIDs := make([]string, 0, len(bucket))
	for _, todo := range bucket {
		delete(t.todos, todo.ID)
		removedIDs = append(removedIDs, todo.ID)
	}
	if len(removedIDs) > 0 {
		if err := t.save(); err != nil {
			log.Printf("[todo] cleanup bucket(%s) save failed: %v", sessionID, err)
			return removedIDs
		}
		for _, id := range removedIDs {
			broadcastTodoChanged(id, "delete")
		}
		log.Printf("[todo] cleanup bucket(%s): %d todos removed", sessionID, len(removedIDs))
	}
	return removedIDs
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
	// 会话边界保护（同 updateTodo）
	if callerSession, _ := args["session_id"].(string); callerSession != "" && todo.SessionID != "" && todo.SessionID != callerSession {
		return nil, fmt.Errorf("todo not found: %s", id)
	}
	sessionID := todo.SessionID
	// 已 completed → 短路：不重打 completed_at、不 save、不 broadcast，避免无谓 IO
	if todo.Status == "completed" {
		t.cleanupSessionIfAllDoneLocked(sessionID)
		return map[string]interface{}{
			"id":      todo.ID,
			"title":   todo.Title,
			"status":  todo.Status,
			"message": "Todo already completed",
		}, nil
	}

	now := time.Now()
	todo.Status = "completed"
	todo.CompletedAt = &now
	todo.UpdatedAt = now

	if err := t.save(); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}
	broadcastTodoChanged(todo.ID, "complete")
	t.cleanupSessionIfAllDoneLocked(sessionID)

	return map[string]interface{}{
		"id":      todo.ID,
		"title":   todo.Title,
		"status":  todo.Status,
		"message": "Todo completed successfully",
	}, nil
}
