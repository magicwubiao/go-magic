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
	// tombstones records IDs removed by auto-cleanup
	// (cleanupSessionIfAllDoneLocked). Operations on tombstoned IDs are
	// answered idempotently instead of failing with "todo not found", so a
	// late update/complete/delete after cleanup never aborts an agent run.
	// Persisted to tombstoneFile: without disk persistence a process restart
	// (deploy/upgrade) wipes them and late ops on cleaned IDs error again.
	tombstones map[string]tombstoneInfo
	// tombstoneFile persists tombstones across restarts; empty = memory-only
	// (tests).
	tombstoneFile string
}

// tombstoneInfo keeps just enough context to answer gracefully.
type tombstoneInfo struct {
	SessionID string
	Title     string
	Status    string // status the item had when it was cleaned up (usually completed/cancelled)
	CleanedAt time.Time
}

// maxTombstones caps tombstone.json growth: each entry is tiny but cleanup
// fires forever, so prune oldest by CleanedAt beyond this.
const maxTombstones = 512

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
			todos:         make(map[string]*TodoItem),
			dataFile:      filepath.Join(dataDir, "todos.json"),
			tombstones:    make(map[string]tombstoneInfo),
			tombstoneFile: filepath.Join(dataDir, "tombstones.json"),
		}
		todoTool.load()
		todoTool.loadTombstones()
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
	return renameWithLockRetry(tmp, t.dataFile)
}

// loadTombstones restores auto-cleanup tombstones from disk. Missing or
// corrupt file just starts with an empty set (same tolerance as load()).
func (t *TodoTool) loadTombstones() {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := os.ReadFile(t.tombstoneFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[TODO] failed to read %s: %v (starting with empty tombstones)", t.tombstoneFile, err)
		}
		return
	}
	var tombs map[string]tombstoneInfo
	if err := json.Unmarshal(data, &tombs); err != nil {
		log.Printf("[TODO] failed to parse %s: %v (starting with empty tombstones)", t.tombstoneFile, err)
		return
	}
	for id, info := range tombs {
		t.tombstones[id] = info
	}
}

// saveTombstonesLocked persists tombstones atomically; prunes oldest entries
// beyond maxTombstones. Caller must hold t.mu (write). Failures are logged,
// never fatal — tombstones are a graceful-degradation mechanism, losing some
// only means a late op may error "todo not found" as before.
func (t *TodoTool) saveTombstonesLocked() {
	if t.tombstoneFile == "" {
		return
	}
	if len(t.tombstones) > maxTombstones {
		type entry struct {
			id string
			at time.Time
		}
		entries := make([]entry, 0, len(t.tombstones))
		for id, info := range t.tombstones {
			entries = append(entries, entry{id, info.CleanedAt})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
		for _, e := range entries[:len(entries)-maxTombstones] {
			delete(t.tombstones, e.id)
		}
	}
	data, err := json.MarshalIndent(t.tombstones, "", "  ")
	if err != nil {
		log.Printf("[TODO] marshal tombstones failed: %v", err)
		return
	}
	tmp := t.tombstoneFile + ".tmp"
	if err := os.WriteFile(tmp, data, defaultFileSecurity().DefaultFileMode); err != nil {
		log.Printf("[TODO] tombstone write failed: %v", err)
		return
	}
	if err := renameWithLockRetry(tmp, t.tombstoneFile); err != nil {
		log.Printf("[TODO] tombstone rename failed: %v", err)
	}
}

// Name returns the tool name
func (t *TodoTool) Name() string {
	return "todo"
}

// Description returns the tool description
func (t *TodoTool) Description() string {
	return "Task planning and tracking tool. Use this to break down complex tasks into manageable steps. " +
		"WHEN TO USE: When the user asks for something requiring 3+ steps; Before starting a multi-step workflow (coding, research, analysis); To track progress on long-running tasks. " +
		"PLAN OF RECORD: create the list BEFORE executing the first step (not after), and keep statuses truthful — never mark an item completed unless it actually is. Update the list when the plan changes instead of silently deviating. " +
		"MARK AS YOU GO: the moment ONE step truly lands, immediately issue action=complete (or action=update with status=completed) for THAT item — one completion per finished step, in the same response as the work that finished it. " +
		"NEVER BATCH COMPLETIONS: do not hold the completions back and fire them all at once when the whole task is over — not even as a tidy final sweep. A status that lags behind reality is the most common way this list goes wrong: an item left at pending after its work is done is indistinguishable from unfinished work for the user, and it blocks the list from being cleaned up. " +
		"NOT A DELIVERABLE: creating the list does not finish the task. After the create calls, keep working through the steps in the same turn — never end your turn with just the plan. " +
		"HOW TO USE: 1) Call action=create to add steps, 2) Call action=list to show progress, 3) Call action=complete right after each step lands (one per step, as you go), 4) Call action=update if plans change. " +
		"BATCHING APPLIES TO CREATE ONLY: when creating multiple todos, emit ALL create calls together in a single response as parallel tool calls — never one create per turn, since each extra turn costs a full LLM round-trip. Completions are the exact opposite of batched: they must follow reality step by step. " +
		"EXAMPLE: User says 'Build a login page' -> In ONE response emit parallel create calls for: Design form, Add validation, Connect API, Test -> then complete EACH one individually right after it actually works (verify it first), never all four in one final batch."
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
		return nil, fmt.Errorf("unknown action: %s (valid actions: create, list, update, delete, complete; to mark a todo in progress use action=update with status=in_progress)", action)
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
	if !ok || strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title is required for create")
	}
	// 规范化：去掉首尾空白，避免出现"看似有标题、实际只有空格"的空白待办，
	// 否则前端侧边栏会渲染出没有文字的空白项。
	title = strings.TrimSpace(title)

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

	// 返回值也带一句收尾提醒：模型每回合都会重读工具结果，"刚建完计划"正是
	// 最该把"完成一个就标一个"钉进上下文的那一刻（比只写在 Description 里更靠近
	// 决策点）。线上残留的主因就是模型把 complete 攒到最后——甚至根本不发。
	// 文案里的 "one completion per finished step" 是 5 处提示词共用的标记短语，
	// 由 todo_tool_guidance_test.go 守卫。
	return map[string]interface{}{
		"id":      todo.ID,
		"title":   todo.Title,
		"status":  todo.Status,
		"message": "Todo created successfully. As soon as this step actually lands, mark it complete right away (action=complete) — one completion per finished step; do not hold the completions back for a single batch at the end.",
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
		if resp, handled := t.resolveTombstoneLocked(id, "update"); handled {
			return resp, nil
		}
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
			// 拒绝把标题清空/改成纯空白：否则前端侧边栏会渲染出没有文字的空白项。
			// 允许调用方删除 title 键来保留原值，但显式传空串或全空格会报错提示。
			if strings.TrimSpace(v) == "" {
				return nil, fmt.Errorf("title cannot be empty for update")
			}
			todo.Title = strings.TrimSpace(v)
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
		if resp, handled := t.resolveTombstoneLocked(id, "delete"); handled {
			return resp, nil
		}
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
		// Record a tombstone so later operations on this ID degrade
		// gracefully instead of erroring with "todo not found".
		t.tombstones[todo.ID] = tombstoneInfo{
			SessionID: todo.SessionID,
			Title:     todo.Title,
			Status:    todo.Status,
			CleanedAt: time.Now(),
		}
		removedIDs = append(removedIDs, todo.ID)
	}
	if len(removedIDs) > 0 {
		if err := t.save(); err != nil {
			log.Printf("[todo] cleanup bucket(%s) save failed: %v", sessionID, err)
			return removedIDs
		}
		// 墓碑持久化：进程重启（部署/升级）后旧 ID 的迟到操作仍能优雅降级。
		t.saveTombstonesLocked()
		for _, id := range removedIDs {
			broadcastTodoChanged(id, "delete")
		}
		log.Printf("[todo] cleanup bucket(%s): %d todos removed", sessionID, len(removedIDs))
	}
	return removedIDs
}

// PendingTitlesForSession returns the titles of the given bucket's unfinished
// items (pending / in_progress), ordered by creation time. Callers embed them
// in LLM prompts and are responsible for their own length budgeting (the agent's
// reconcile nudge caps the list itself).
//
// 供 Agent 的回合末"待办对账提醒"使用（nudgeTodoReconciliation）：模型给出
// 最终回答前，若本回合动过 todo 且会话仍有未完成项，把它们的名字递回去。
func (t *TodoTool) PendingTitlesForSession(sessionID string) []string {
	t.mu.RLock()
	items := make([]*TodoItem, 0, 8)
	for _, todo := range t.todos {
		if todo.SessionID != sessionID {
			continue
		}
		if todo.Status == "pending" || todo.Status == "in_progress" {
			items = append(items, todo)
		}
	}
	t.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	titles := make([]string, 0, len(items))
	for _, todo := range items {
		titles = append(titles, todo.Title)
	}
	return titles
}

// TodoPlanEntry 是一条注入给模型的"在案计划"条目。
//
// 与 PendingTitlesForSession 的关键区别是**带 ID**：模型要调 action=complete /
// update 必须给出精确 ID，而 ID 只出现在 create 的返回值里。历史一旦被压缩，
// 那份返回值就没了（见 ActivePlanForSession 的说明）。
type TodoPlanEntry struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// ActivePlanForSession 返回该会话**未完成**（pending / in_progress）的待办快照，
// 供请求组装时当作权威"在案计划"注入。
//
// 为什么必须存在：待办列表原本只活在**对话历史**里（create 的工具结果携带 ID），
// 而对话历史正是系统唯一会主动删除的东西 —— 长回合必然触发历史压缩
// （agent.maybeCompressContext，每次 LLM 调用前查阈值），压缩把中段消息整体换成
// 一条摘要，而摘要既不保留待办 ID、注入的 SummaryPrefix 还明说"早先的事已经处理
// 完了、只回应最新用户消息"。2026-10-09 线上事故即由此而来：模型建了 4 条待办、
// 只标了 1 条（压缩之前那条），压缩之后既没有 ID、也不知道列表还在案，此后再没
// 发过 complete，残留永久留在面板上。
//
// 因此"在案计划"必须从**持久层**（本工具的 todos 内存/磁盘态）每轮重新渲染进请求，
// 而不是寄存在会被删掉的历史里。provider 每轮重算 ⇒ 压缩永远删不掉它。
//
// 排序：in_progress 在前（正在做的），其余按创建时间；上限由调用方（注入点）裁剪。
func (t *TodoTool) ActivePlanForSession(sessionID string) []TodoPlanEntry {
	if sessionID == "" {
		// 无会话上下文时不动全局桶，避免把别人的计划注入本次请求。
		return nil
	}
	t.mu.RLock()
	items := make([]*TodoItem, 0, 8)
	for _, todo := range t.todos {
		if todo.SessionID != sessionID {
			continue
		}
		if todo.Status == "pending" || todo.Status == "in_progress" {
			items = append(items, todo)
		}
	}
	t.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		if (items[i].Status == "in_progress") != (items[j].Status == "in_progress") {
			return items[i].Status == "in_progress"
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	entries := make([]TodoPlanEntry, 0, len(items))
	for _, todo := range items {
		entries = append(entries, TodoPlanEntry{ID: todo.ID, Title: todo.Title, Status: todo.Status})
	}
	return entries
}

// SweepSessionTerminal removes every already-finished todo (completed / cancelled)
// of the given bucket and returns how many were removed. Unfinished items
// (pending / in_progress) are deliberately left alone — they represent real
// outstanding work.
//
// Why this exists: cleanupSessionIfAllDoneLocked only clears a bucket when *every*
// item in it is terminal. A real conversation almost always leaves at least one
// step unfinished (or the model simply never marks the last one), so the bucket
// never reaches the "all done" state and finished items pile up in the sidebar
// forever. Observed in the wild: 47 completed items still on disk across 10
// sessions. The agent calls this once per turn (Agent.sweepFinishedTodos) so
// finished work disappears when the conversation stops.
//
// sessionID == "" targets the global (unowned) bucket, mirroring listTodos.
func (t *TodoTool) SweepSessionTerminal(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	removed := make([]*TodoItem, 0, 8)
	for _, todo := range t.todos {
		if todo.SessionID != sessionID {
			continue
		}
		if todo.Status == "completed" || todo.Status == "cancelled" {
			removed = append(removed, todo)
		}
	}
	if len(removed) == 0 {
		return 0
	}

	for _, todo := range removed {
		delete(t.todos, todo.ID)
	}
	if err := t.save(); err != nil {
		// Roll back so memory and disk stay consistent: committing the in-memory
		// deletion while the file still holds the items makes them reappear after a
		// restart (the panel would look "haunted").
		for _, todo := range removed {
			t.todos[todo.ID] = todo
		}
		log.Printf("[todo] sweep bucket(%s) save failed: %v (rolled back)", sessionID, err)
		return 0
	}

	for _, todo := range removed {
		t.tombstones[todo.ID] = tombstoneInfo{
			SessionID: todo.SessionID,
			Title:     todo.Title,
			Status:    todo.Status,
			CleanedAt: time.Now(),
		}
	}
	// 墓碑持久化：进程重启（部署/升级）后旧 ID 的迟到操作仍能优雅降级。
	t.saveTombstonesLocked()
	for _, todo := range removed {
		broadcastTodoChanged(todo.ID, "delete")
	}
	log.Printf("[todo] sweep bucket(%s): %d finished todos removed", sessionID, len(removed))
	return len(removed)
}

// resolveTombstoneLocked answers an operation on an auto-cleaned todo
// gracefully. Caller must hold t.mu (write). Returns handled=false when the
// ID has no tombstone (i.e. a genuinely unknown ID).
func (t *TodoTool) resolveTombstoneLocked(id, action string) (map[string]interface{}, bool) {
	info, ok := t.tombstones[id]
	if !ok {
		return nil, false
	}
	resp := map[string]interface{}{
		"id":         id,
		"status":     info.Status,
		"tombstoned": true,
		"message":    fmt.Sprintf("todo was auto-removed after its session finished; treating %s as no-op", action),
	}
	if info.Title != "" {
		resp["title"] = info.Title
	}
	return resp, true
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
		if resp, handled := t.resolveTombstoneLocked(id, "complete"); handled {
			return resp, nil
		}
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
