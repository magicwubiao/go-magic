package kanban

import (
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	StatusTriage   TaskStatus = "triage"
	StatusTodo     TaskStatus = "todo"
	StatusReady    TaskStatus = "ready"
	StatusRunning  TaskStatus = "running"
	StatusBlocked  TaskStatus = "blocked"
	StatusDone     TaskStatus = "done"
	StatusArchived TaskStatus = "archived"
)

// ValidStatuses contains all valid task statuses
var ValidStatuses = []TaskStatus{
	StatusTriage, StatusTodo, StatusReady, StatusRunning,
	StatusBlocked, StatusDone, StatusArchived,
}

// IsValidStatus checks if a status is valid
func IsValidStatus(s TaskStatus) bool {
	for _, valid := range ValidStatuses {
		if s == valid {
			return true
		}
	}
	return false
}

// Task represents a kanban task
type Task struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	Assignee          string     `json:"assignee"`
	Status            TaskStatus `json:"status"`
	Priority          int        `json:"priority"` // 0=low, 1=medium, 2=high, 3=critical
	Tenant            string     `json:"tenant"`
	Workspace         string     `json:"workspace"`
	Skills            []string   `json:"skills"`
	MaxRuntimeSeconds int       `json:"max_runtime_seconds"`
	IdempotencyKey    string     `json:"idempotency_key"`
	CurrentRunID      string     `json:"current_run_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// Virtual fields (not stored in DB)
	ParentCount    int `json:"parent_count,omitempty"`
	CommentCount   int `json:"comment_count,omitempty"`
	ChildDoneCount int `json:"child_done_count,omitempty"`
	ChildCount     int `json:"child_count,omitempty"`
}

// Comment represents a task comment
type Comment struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Run represents a task execution run
type Run struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	Status     string     `json:"status"` // running/completed/failed/crashed/timed_out
	PID        int        `json:"pid"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Summary    string     `json:"summary"`
	Result     string     `json:"result"`
}

// Event represents a task event for notifications
type Event struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	EventType string    `json:"event_type"` // created/status_changed/commented/completed/blocked/unblocked
	Payload   string    `json:"payload"`    // JSON
	CreatedAt time.Time `json:"created_at"`
}

// TaskFilter represents filter criteria for listing tasks
type TaskFilter struct {
	Status   []TaskStatus `json:"status,omitempty"`
	Assignee string       `json:"assignee,omitempty"`
	Tenant   string       `json:"tenant,omitempty"`
	Priority *int         `json:"priority,omitempty"`
	Search   string       `json:"search,omitempty"`
	Limit    int          `json:"limit,omitempty"`
	Offset   int          `json:"offset,omitempty"`
}

// TaskOption is a functional option for creating tasks
type TaskOption func(*Task)

// WithPriority sets the task priority
func WithPriority(p int) TaskOption {
	return func(t *Task) {
		t.Priority = p
	}
}

// WithTenant sets the task tenant
func WithTenant(t string) TaskOption {
	return func(task *Task) {
		task.Tenant = t
	}
}

// WithWorkspace sets the task workspace
func WithWorkspace(w string) TaskOption {
	return func(task *Task) {
		task.Workspace = w
	}
}

// WithSkills sets the task skills
func WithSkills(s []string) TaskOption {
	return func(task *Task) {
		task.Skills = s
	}
}

// WithMaxRuntime sets the task max runtime in seconds
func WithMaxRuntime(sec int) TaskOption {
	return func(task *Task) {
		task.MaxRuntimeSeconds = sec
	}
}

// WithIdempotencyKey sets the task idempotency key
func WithIdempotencyKey(k string) TaskOption {
	return func(task *Task) {
		task.IdempotencyKey = k
	}
}

// WithAssignee sets the task assignee
func WithAssignee(a string) TaskOption {
	return func(task *Task) {
		task.Assignee = a
	}
}

// WithBody sets the task body
func WithBody(b string) TaskOption {
	return func(task *Task) {
		task.Body = b
	}
}

// EventType constants
const (
	EventCreated       = "created"
	EventStatusChange  = "status_changed"
	EventCommented     = "commented"
	EventCompleted     = "completed"
	EventBlocked       = "blocked"
	EventUnblocked     = "unblocked"
	EventClaimed       = "claimed"
	EventHeartbeat     = "heartbeat"
	EventTimeout       = "timeout"
	EventCrash         = "crash"
)

// RunStatus constants
const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusCrashed   = "crashed"
	RunStatusTimedOut  = "timed_out"
)
