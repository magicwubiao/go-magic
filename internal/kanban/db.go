package kanban

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// KanbanDB manages the kanban SQLite database
type KanbanDB struct {
	db   *sql.DB
	path string
}

// NewKanbanDB creates a new KanbanDB instance
func NewKanbanDB(path string) (*KanbanDB, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return &KanbanDB{
		db:   db,
		path: path,
	}, nil
}

// Init initializes the database schema
func (kdb *KanbanDB) Init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		assignee TEXT DEFAULT '',
		status TEXT DEFAULT 'triage',
		priority INTEGER DEFAULT 0,
		tenant TEXT DEFAULT '',
		workspace TEXT DEFAULT '',
		skills TEXT DEFAULT '[]',
		max_runtime_seconds INTEGER DEFAULT 0,
		idempotency_key TEXT DEFAULT '',
		current_run_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS task_links (
		parent_id TEXT NOT NULL,
		child_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (parent_id, child_id),
		FOREIGN KEY (parent_id) REFERENCES tasks(id) ON DELETE CASCADE,
		FOREIGN KEY (child_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS task_comments (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		author TEXT DEFAULT '',
		body TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS task_runs (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		status TEXT DEFAULT 'running',
		pid INTEGER DEFAULT 0,
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		finished_at DATETIME,
		summary TEXT DEFAULT '',
		result TEXT DEFAULT '',
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS task_events (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
	CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee);
	CREATE INDEX IF NOT EXISTS idx_tasks_tenant ON tasks(tenant);
	CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
	CREATE INDEX IF NOT EXISTS idx_task_links_parent ON task_links(parent_id);
	CREATE INDEX IF NOT EXISTS idx_task_links_child ON task_links(child_id);
	CREATE INDEX IF NOT EXISTS idx_task_comments_task ON task_comments(task_id);
	CREATE INDEX IF NOT EXISTS idx_task_runs_task ON task_runs(task_id);
	CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id);
	`

	_, err := kdb.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Infof("[KanbanDB] Initialized database at %s", kdb.path)
	return nil
}

// Close closes the database connection
func (kdb *KanbanDB) Close() error {
	return kdb.db.Close()
}

// generateID generates a unique task ID
func generateID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// CreateTask creates a new task
func (kdb *KanbanDB) CreateTask(task *Task) error {
	if task.ID == "" {
		task.ID = generateID("task")
	}

	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	if task.Status == "" {
		task.Status = StatusTriage
	}

	skillsJSON, _ := json.Marshal(task.Skills)

	query := `
	INSERT INTO tasks (id, title, body, assignee, status, priority, tenant, workspace, skills, 
		max_runtime_seconds, idempotency_key, current_run_id, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := kdb.db.Exec(query,
		task.ID, task.Title, task.Body, task.Assignee, string(task.Status), task.Priority,
		task.Tenant, task.Workspace, string(skillsJSON), task.MaxRuntimeSeconds,
		task.IdempotencyKey, task.CurrentRunID, task.CreatedAt, task.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	return nil
}

// GetTask retrieves a task by ID
func (kdb *KanbanDB) GetTask(id string) (*Task, error) {
	query := `
	SELECT id, title, body, assignee, status, priority, tenant, workspace, skills, 
		max_runtime_seconds, idempotency_key, current_run_id, created_at, updated_at
	FROM tasks WHERE id = ?
	`

	var task Task
	var skillsJSON string
	err := kdb.db.QueryRow(query, id).Scan(
		&task.ID, &task.Title, &task.Body, &task.Assignee, &task.Status, &task.Priority,
		&task.Tenant, &task.Workspace, &skillsJSON, &task.MaxRuntimeSeconds,
		&task.IdempotencyKey, &task.CurrentRunID, &task.CreatedAt, &task.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	json.Unmarshal([]byte(skillsJSON), &task.Skills)
	if task.Skills == nil {
		task.Skills = []string{}
	}

	return &task, nil
}

// UpdateTask updates a task
func (kdb *KanbanDB) UpdateTask(task *Task) error {
	task.UpdatedAt = time.Now()

	skillsJSON, _ := json.Marshal(task.Skills)

	query := `
	UPDATE tasks SET title = ?, body = ?, assignee = ?, status = ?, priority = ?, 
		tenant = ?, workspace = ?, skills = ?, max_runtime_seconds = ?, 
		idempotency_key = ?, current_run_id = ?, updated_at = ?
	WHERE id = ?
	`

	result, err := kdb.db.Exec(query,
		task.Title, task.Body, task.Assignee, string(task.Status), task.Priority,
		task.Tenant, task.Workspace, string(skillsJSON), task.MaxRuntimeSeconds,
		task.IdempotencyKey, task.CurrentRunID, task.UpdatedAt, task.ID)

	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found: %s", task.ID)
	}

	return nil
}

// ListTasks lists tasks with optional filters
func (kdb *KanbanDB) ListTasks(filter TaskFilter) ([]*Task, error) {
	query := `
	SELECT id, title, body, assignee, status, priority, tenant, workspace, skills,
		max_runtime_seconds, idempotency_key, current_run_id, created_at, updated_at
	FROM tasks WHERE 1=1
	`
	args := []interface{}{}

	if len(filter.Status) > 0 {
		placeholders := make([]string, len(filter.Status))
		for i, s := range filter.Status {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		query += fmt.Sprintf(" AND status IN (%s)", strings.Join(placeholders, ","))
	}

	if filter.Assignee != "" {
		query += " AND assignee = ?"
		args = append(args, filter.Assignee)
	}

	if filter.Tenant != "" {
		query += " AND tenant = ?"
		args = append(args, filter.Tenant)
	}

	if filter.Priority != nil {
		query += " AND priority = ?"
		args = append(args, *filter.Priority)
	}

	if filter.Search != "" {
		query += " AND (title LIKE ? OR body LIKE ?)"
		search := "%" + filter.Search + "%"
		args = append(args, search, search)
	}

	query += " ORDER BY priority DESC, created_at ASC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := kdb.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var task Task
		var skillsJSON string
		if err := rows.Scan(
			&task.ID, &task.Title, &task.Body, &task.Assignee, &task.Status, &task.Priority,
			&task.Tenant, &task.Workspace, &skillsJSON, &task.MaxRuntimeSeconds,
			&task.IdempotencyKey, &task.CurrentRunID, &task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			continue
		}

		json.Unmarshal([]byte(skillsJSON), &task.Skills)
		if task.Skills == nil {
			task.Skills = []string{}
		}

		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// DeleteTask deletes a task by ID
func (kdb *KanbanDB) DeleteTask(id string) error {
	result, err := kdb.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found: %s", id)
	}

	return nil
}

// UpdateTaskStatus updates a task's status
func (kdb *KanbanDB) UpdateTaskStatus(id, newStatus, summary string) error {
	now := time.Now()

	query := "UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?"
	result, err := kdb.db.Exec(query, newStatus, now, id)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found: %s", id)
	}

	// Add status change event
	event := &Event{
		ID:        generateID("evt"),
		TaskID:    id,
		EventType: EventStatusChange,
		Payload:   fmt.Sprintf(`{"new_status":"%s","summary":"%s"}`, newStatus, summary),
	}
	kdb.AddEvent(event)

	return nil
}

// AddLink adds a parent-child link
func (kdb *KanbanDB) AddLink(parentID, childID string) error {
	query := "INSERT OR IGNORE INTO task_links (parent_id, child_id) VALUES (?, ?)"
	_, err := kdb.db.Exec(query, parentID, childID)
	if err != nil {
		return fmt.Errorf("failed to add link: %w", err)
	}
	return nil
}

// RemoveLink removes a parent-child link
func (kdb *KanbanDB) RemoveLink(parentID, childID string) error {
	result, err := kdb.db.Exec("DELETE FROM task_links WHERE parent_id = ? AND child_id = ?", parentID, childID)
	if err != nil {
		return fmt.Errorf("failed to remove link: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("link not found")
	}

	return nil
}

// GetParents gets all parent tasks of a task
func (kdb *KanbanDB) GetParents(taskID string) ([]*Task, error) {
	query := `
	SELECT t.id, t.title, t.body, t.assignee, t.status, t.priority, t.tenant, t.workspace, t.skills,
		t.max_runtime_seconds, t.idempotency_key, t.current_run_id, t.created_at, t.updated_at
	FROM tasks t
	INNER JOIN task_links l ON t.id = l.parent_id
	WHERE l.child_id = ?
	`

	return kdb.queryTasks(query, taskID)
}

// GetChildren gets all child tasks of a task
func (kdb *KanbanDB) GetChildren(taskID string) ([]*Task, error) {
	query := `
	SELECT t.id, t.title, t.body, t.assignee, t.status, t.priority, t.tenant, t.workspace, t.skills,
		t.max_runtime_seconds, t.idempotency_key, t.current_run_id, t.created_at, t.updated_at
	FROM tasks t
	INNER JOIN task_links l ON t.id = l.child_id
	WHERE l.parent_id = ?
	`

	return kdb.queryTasks(query, taskID)
}

// AreAllParentsDone checks if all parent tasks are done
func (kdb *KanbanDB) AreAllParentsDone(taskID string) (bool, error) {
	query := `
	SELECT COUNT(*) FROM tasks t
	INNER JOIN task_links l ON t.id = l.parent_id
	WHERE l.child_id = ? AND t.status != 'done' AND t.status != 'archived'
	`

	var count int
	if err := kdb.db.QueryRow(query, taskID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check parents: %w", err)
	}

	return count == 0, nil
}

func (kdb *KanbanDB) queryTasks(query string, args ...interface{}) ([]*Task, error) {
	rows, err := kdb.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var task Task
		var skillsJSON string
		if err := rows.Scan(
			&task.ID, &task.Title, &task.Body, &task.Assignee, &task.Status, &task.Priority,
			&task.Tenant, &task.Workspace, &skillsJSON, &task.MaxRuntimeSeconds,
			&task.IdempotencyKey, &task.CurrentRunID, &task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			continue
		}

		json.Unmarshal([]byte(skillsJSON), &task.Skills)
		if task.Skills == nil {
			task.Skills = []string{}
		}

		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// AddComment adds a comment to a task
func (kdb *KanbanDB) AddComment(comment *Comment) error {
	if comment.ID == "" {
		comment.ID = generateID("cmt")
	}
	if comment.CreatedAt.IsZero() {
		comment.CreatedAt = time.Now()
	}

	query := "INSERT INTO task_comments (id, task_id, author, body, created_at) VALUES (?, ?, ?, ?, ?)"
	_, err := kdb.db.Exec(query, comment.ID, comment.TaskID, comment.Author, comment.Body, comment.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	// Add comment event
	event := &Event{
		ID:        generateID("evt"),
		TaskID:    comment.TaskID,
		EventType: EventCommented,
		Payload:   fmt.Sprintf(`{"author":"%s","comment_id":"%s"}`, comment.Author, comment.ID),
	}
	kdb.AddEvent(event)

	return nil
}

// ListComments lists comments for a task
func (kdb *KanbanDB) ListComments(taskID string) ([]*Comment, error) {
	query := "SELECT id, task_id, author, body, created_at FROM task_comments WHERE task_id = ? ORDER BY created_at ASC"

	rows, err := kdb.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list comments: %w", err)
	}
	defer rows.Close()

	var comments []*Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.CreatedAt); err != nil {
			continue
		}
		comments = append(comments, &c)
	}

	return comments, nil
}

// CreateRun creates a new task run
func (kdb *KanbanDB) CreateRun(run *Run) error {
	if run.ID == "" {
		run.ID = generateID("run")
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}

	query := `
	INSERT INTO task_runs (id, task_id, status, pid, started_at, summary, result)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := kdb.db.Exec(query, run.ID, run.TaskID, run.Status, run.PID, run.StartedAt, run.Summary, run.Result)
	if err != nil {
		return fmt.Errorf("failed to create run: %w", err)
	}

	return nil
}

// UpdateRun updates a task run
func (kdb *KanbanDB) UpdateRun(run *Run) error {
	query := `
	UPDATE task_runs SET status = ?, pid = ?, finished_at = ?, summary = ?, result = ?
	WHERE id = ?
	`

	_, err := kdb.db.Exec(query, run.Status, run.PID, run.FinishedAt, run.Summary, run.Result, run.ID)
	if err != nil {
		return fmt.Errorf("failed to update run: %w", err)
	}

	return nil
}

// GetCurrentRun gets the current running task for a task
func (kdb *KanbanDB) GetCurrentRun(taskID string) (*Run, error) {
	query := `
	SELECT id, task_id, status, pid, started_at, finished_at, summary, result
	FROM task_runs WHERE task_id = ? AND status = 'running'
	ORDER BY started_at DESC LIMIT 1
	`

	var run Run
	err := kdb.db.QueryRow(query, taskID).Scan(
		&run.ID, &run.TaskID, &run.Status, &run.PID, &run.StartedAt, &run.FinishedAt, &run.Summary, &run.Result)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get current run: %w", err)
	}

	return &run, nil
}

// ListRuns lists all runs for a task
func (kdb *KanbanDB) ListRuns(taskID string) ([]*Run, error) {
	query := `
	SELECT id, task_id, status, pid, started_at, finished_at, summary, result
	FROM task_runs WHERE task_id = ? ORDER BY started_at DESC
	`

	rows, err := kdb.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}
	defer rows.Close()

	var runs []*Run
	for rows.Next() {
		var run Run
		if err := rows.Scan(
			&run.ID, &run.TaskID, &run.Status, &run.PID, &run.StartedAt, &run.FinishedAt, &run.Summary, &run.Result,
		); err != nil {
			continue
		}
		runs = append(runs, &run)
	}

	return runs, nil
}

// AddEvent adds an event to the event log
func (kdb *KanbanDB) AddEvent(event *Event) error {
	if event.ID == "" {
		event.ID = generateID("evt")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	query := "INSERT INTO task_events (id, task_id, event_type, payload, created_at) VALUES (?, ?, ?, ?, ?)"
	_, err := kdb.db.Exec(query, event.ID, event.TaskID, event.EventType, event.Payload, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to add event: %w", err)
	}

	return nil
}

// ListEvents lists events for a task since a given time
func (kdb *KanbanDB) ListEvents(taskID string, since time.Time) ([]*Event, error) {
	query := `
	SELECT id, task_id, event_type, payload, created_at
	FROM task_events WHERE task_id = ? AND created_at >= ?
	ORDER BY created_at ASC
	`

	rows, err := kdb.db.Query(query, taskID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TaskID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			continue
		}
		events = append(events, &e)
	}

	return events, nil
}

// ClaimTask atomically claims a task (CAS: ready → running)
func (kdb *KanbanDB) ClaimTask(taskID, assignee, runID string) (bool, error) {
	tx, err := kdb.db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Atomic CAS update
	result, err := tx.Exec(
		"UPDATE tasks SET status = ?, assignee = ?, current_run_id = ?, updated_at = ? WHERE id = ? AND status = ?",
		StatusRunning, assignee, runID, time.Now(), taskID, StatusReady)

	if err != nil {
		return false, fmt.Errorf("failed to claim task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return false, nil // Task was not in ready state
	}

	// Create run record
	run := &Run{
		ID:        runID,
		TaskID:    taskID,
		Status:    RunStatusRunning,
		StartedAt: time.Now(),
	}

	runQuery := "INSERT INTO task_runs (id, task_id, status, pid, started_at) VALUES (?, ?, ?, ?, ?)"
	if _, err := tx.Exec(runQuery, run.ID, run.TaskID, run.Status, run.PID, run.StartedAt); err != nil {
		return false, fmt.Errorf("failed to create run: %w", err)
	}

	// Add claim event
	eventQuery := "INSERT INTO task_events (id, task_id, event_type, payload, created_at) VALUES (?, ?, ?, ?, ?)"
	eventID := generateID("evt")
	payload := fmt.Sprintf(`{"assignee":"%s","run_id":"%s"}`, assignee, runID)
	if _, err := tx.Exec(eventQuery, eventID, taskID, EventClaimed, payload, time.Now()); err != nil {
		return false, fmt.Errorf("failed to add event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit: %w", err)
	}

	return true, nil
}

// GetRunningTasks gets all running tasks (for dispatcher)
func (kdb *KanbanDB) GetRunningTasks() ([]*Task, error) {
	query := `
	SELECT id, title, body, assignee, status, priority, tenant, workspace, skills,
		max_runtime_seconds, idempotency_key, current_run_id, created_at, updated_at
	FROM tasks WHERE status = 'running'
	`

	return kdb.queryTasks(query)
}

// GetReadyTasks gets all ready tasks (for dispatcher)
func (kdb *KanbanDB) GetReadyTasks() ([]*Task, error) {
	query := `
	SELECT id, title, body, assignee, status, priority, tenant, workspace, skills,
		max_runtime_seconds, idempotency_key, current_run_id, created_at, updated_at
	FROM tasks WHERE status = 'ready'
	ORDER BY priority DESC, created_at ASC
	`

	return kdb.queryTasks(query)
}

// GetTodoTasks gets all todo tasks (for dispatcher)
func (kdb *KanbanDB) GetTodoTasks() ([]*Task, error) {
	query := `
	SELECT id, title, body, assignee, status, priority, tenant, workspace, skills,
		max_runtime_seconds, idempotency_key, current_run_id, created_at, updated_at
	FROM tasks WHERE status = 'todo'
	`

	return kdb.queryTasks(query)
}

// GetTaskWithMeta gets a task with metadata (parent count, comment count, etc.)
func (kdb *KanbanDB) GetTaskWithMeta(id string) (*Task, error) {
	task, err := kdb.GetTask(id)
	if err != nil {
		return nil, err
	}

	// Count parents
	var parentCount int
	kdb.db.QueryRow("SELECT COUNT(*) FROM task_links WHERE child_id = ?", id).Scan(&parentCount)
	task.ParentCount = parentCount

	// Count comments
	var commentCount int
	kdb.db.QueryRow("SELECT COUNT(*) FROM task_comments WHERE task_id = ?", id).Scan(&commentCount)
	task.CommentCount = commentCount

	// Count children and done children
	var childCount, childDoneCount int
	kdb.db.QueryRow("SELECT COUNT(*) FROM task_links WHERE parent_id = ?", id).Scan(&childCount)
	task.ChildCount = childCount

	if childCount > 0 {
		kdb.db.QueryRow(`
			SELECT COUNT(*) FROM task_links l
			INNER JOIN tasks t ON l.child_id = t.id
			WHERE l.parent_id = ? AND (t.status = 'done' OR t.status = 'archived')
		`, id).Scan(&childDoneCount)
	}
	task.ChildDoneCount = childDoneCount

	return task, nil
}

// GetBoard returns tasks grouped by status for board view
func (kdb *KanbanDB) GetBoard(tenant string) (map[TaskStatus][]*Task, error) {
	filter := TaskFilter{
		Status: []TaskStatus{StatusTriage, StatusTodo, StatusReady, StatusRunning, StatusBlocked, StatusDone},
	}
	if tenant != "" {
		filter.Tenant = tenant
	}

	tasks, err := kdb.ListTasks(filter)
	if err != nil {
		return nil, err
	}

	board := make(map[TaskStatus][]*Task)
	for _, s := range ValidStatuses {
		if s == StatusArchived {
			continue
		}
		board[s] = []*Task{}
	}

	for _, task := range tasks {
		// Get metadata for each task
		taskWithMeta, err := kdb.GetTaskWithMeta(task.ID)
		if err != nil {
			continue
		}
		board[taskWithMeta.Status] = append(board[taskWithMeta.Status], taskWithMeta)
	}

	return board, nil
}

// GetStats returns task counts by status
func (kdb *KanbanDB) GetStats(tenant string) (map[TaskStatus]int, error) {
	query := "SELECT status, COUNT(*) FROM tasks WHERE 1=1"
	args := []interface{}{}

	if tenant != "" {
		query += " AND tenant = ?"
		args = append(args, tenant)
	}

	query += " GROUP BY status"

	rows, err := kdb.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[TaskStatus]int)
	for _, s := range ValidStatuses {
		stats[s] = 0
	}

	for rows.Next() {
		var status TaskStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			continue
		}
		stats[status] = count
	}

	return stats, nil
}
