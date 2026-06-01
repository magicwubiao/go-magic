package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// Manager provides high-level kanban operations
type Manager struct {
	db         *KanbanDB
	dispatcher *Dispatcher
	homeDir    string
}

// NewManager creates a new kanban manager
func NewManager(homeDir string) (*Manager, error) {
	if homeDir == "" {
		homeDir = "~/.magic"
	}
	homeDir = os.ExpandEnv(homeDir)

	dbPath := filepath.Join(homeDir, "kanban.db")
	db, err := NewKanbanDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create kanban DB: %w", err)
	}

	m := &Manager{
		db:      db,
		homeDir: homeDir,
	}

	return m, nil
}

// Init initializes the kanban system
func (m *Manager) Init() error {
	if err := m.db.Init(); err != nil {
		return fmt.Errorf("failed to init kanban DB: %w", err)
	}

	// Start dispatcher
	m.dispatcher = NewDispatcher(m.db)
	m.dispatcher.Start()

	log.Infof("[Kanban] Manager initialized at %s", filepath.Join(m.homeDir, "kanban.db"))
	return nil
}

// Close closes the kanban system
func (m *Manager) Close() error {
	if m.dispatcher != nil {
		m.dispatcher.Stop()
	}
	return m.db.Close()
}

// CreateTask creates a new task
func (m *Manager) CreateTask(title, body, assignee string, opts ...TaskOption) (*Task, error) {
	task := &Task{
		ID:       generateID("task"),
		Title:    title,
		Body:     body,
		Assignee: assignee,
		Status:   StatusTriage,
	}

	// Apply options
	for _, opt := range opts {
		opt(task)
	}

	if err := m.db.CreateTask(task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// Handle parent linking if specified via option
	// (In practice, use AddLink separately)

	// Add created event
	event := &Event{
		ID:        generateID("evt"),
		TaskID:    task.ID,
		EventType: EventCreated,
		Payload:   fmt.Sprintf(`{"title":"%s","assignee":"%s"}`, title, assignee),
	}
	m.db.AddEvent(event)

	return task, nil
}

// GetTask retrieves a task by ID with metadata
func (m *Manager) GetTask(id string) (*Task, error) {
	return m.db.GetTaskWithMeta(id)
}

// ListTasks lists tasks with filters
func (m *Manager) ListTasks(filter TaskFilter) ([]*Task, error) {
	return m.db.ListTasks(filter)
}

// UpdateTask updates a task
func (m *Manager) UpdateTask(id string, updates map[string]interface{}) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if title, ok := updates["title"].(string); ok {
		task.Title = title
	}
	if body, ok := updates["body"].(string); ok {
		task.Body = body
	}
	if assignee, ok := updates["assignee"].(string); ok {
		task.Assignee = assignee
	}
	if status, ok := updates["status"].(string); ok {
		task.Status = TaskStatus(status)
	}
	if priority, ok := updates["priority"].(float64); ok {
		task.Priority = int(priority)
	}
	if tenant, ok := updates["tenant"].(string); ok {
		task.Tenant = tenant
	}
	if workspace, ok := updates["workspace"].(string); ok {
		task.Workspace = workspace
	}
	if skills, ok := updates["skills"].([]interface{}); ok {
		task.Skills = make([]string, len(skills))
		for i, s := range skills {
			task.Skills[i] = fmt.Sprintf("%v", s)
		}
	}

	if err := m.db.UpdateTask(task); err != nil {
		return nil, err
	}

	return m.db.GetTaskWithMeta(id)
}

// DeleteTask deletes a task
func (m *Manager) DeleteTask(id string) error {
	return m.db.DeleteTask(id)
}

// StartTask moves a task from triage/todo to ready
func (m *Manager) StartTask(id string) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusTriage && task.Status != StatusTodo {
		return nil, fmt.Errorf("cannot start task in status %s (must be triage or todo)", task.Status)
	}

	if err := m.db.UpdateTaskStatus(id, string(StatusReady), "Task started"); err != nil {
		return nil, err
	}

	return m.db.GetTaskWithMeta(id)
}

// ClaimTask atomically claims a ready task for running
func (m *Manager) ClaimTask(id, assignee string) (*Task, error) {
	runID := generateID("run")

	claimed, err := m.db.ClaimTask(id, assignee, runID)
	if err != nil {
		return nil, err
	}

	if !claimed {
		return nil, fmt.Errorf("task %s is not in ready state (already claimed?)", id)
	}

	return m.db.GetTaskWithMeta(id)
}

// CompleteTask marks a running task as done
func (m *Manager) CompleteTask(id, summary string) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusRunning {
		return nil, fmt.Errorf("cannot complete task in status %s (must be running)", task.Status)
	}

	// Update run
	run, err := m.db.GetCurrentRun(id)
	if err == nil && run != nil {
		finishedAt := time.Now()
		run.Status = RunStatusCompleted
		run.FinishedAt = &finishedAt
		run.Summary = summary
		m.db.UpdateRun(run)
	}

	if err := m.db.UpdateTaskStatus(id, string(StatusDone), summary); err != nil {
		return nil, err
	}

	// Add completed event
	event := &Event{
		ID:        generateID("evt"),
		TaskID:    id,
		EventType: EventCompleted,
		Payload:   fmt.Sprintf(`{"summary":"%s"}`, summary),
	}
	m.db.AddEvent(event)

	return m.db.GetTaskWithMeta(id)
}

// BlockTask marks a running task as blocked
func (m *Manager) BlockTask(id, reason string) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusRunning {
		return nil, fmt.Errorf("cannot block task in status %s (must be running)", task.Status)
	}

	if err := m.db.UpdateTaskStatus(id, string(StatusBlocked), reason); err != nil {
		return nil, err
	}

	// Add blocked event
	event := &Event{
		ID:        generateID("evt"),
		TaskID:    id,
		EventType: EventBlocked,
		Payload:   fmt.Sprintf(`{"reason":"%s"}`, reason),
	}
	m.db.AddEvent(event)

	return m.db.GetTaskWithMeta(id)
}

// UnblockTask moves a blocked task back to ready
func (m *Manager) UnblockTask(id string) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusBlocked {
		return nil, fmt.Errorf("cannot unblock task in status %s (must be blocked)", task.Status)
	}

	if err := m.db.UpdateTaskStatus(id, string(StatusReady), "Task unblocked"); err != nil {
		return nil, err
	}

	// Add unblocked event
	event := &Event{
		ID:        generateID("evt"),
		TaskID:    id,
		EventType: EventUnblocked,
		Payload:   "{}",
	}
	m.db.AddEvent(event)

	return m.db.GetTaskWithMeta(id)
}

// ArchiveTask moves a done task to archived
func (m *Manager) ArchiveTask(id string) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusDone {
		return nil, fmt.Errorf("cannot archive task in status %s (must be done)", task.Status)
	}

	if err := m.db.UpdateTaskStatus(id, string(StatusArchived), "Task archived"); err != nil {
		return nil, err
	}

	return m.db.GetTaskWithMeta(id)
}

// AddLink adds a parent-child dependency
func (m *Manager) AddLink(parentID, childID string) error {
	// Verify both tasks exist
	if _, err := m.db.GetTask(parentID); err != nil {
		return fmt.Errorf("parent task not found: %s", parentID)
	}
	if _, err := m.db.GetTask(childID); err != nil {
		return fmt.Errorf("child task not found: %s", childID)
	}

	return m.db.AddLink(parentID, childID)
}

// RemoveLink removes a parent-child dependency
func (m *Manager) RemoveLink(parentID, childID string) error {
	return m.db.RemoveLink(parentID, childID)
}

// GetParents gets all parent tasks
func (m *Manager) GetParents(taskID string) ([]*Task, error) {
	return m.db.GetParents(taskID)
}

// GetChildren gets all child tasks
func (m *Manager) GetChildren(taskID string) ([]*Task, error) {
	return m.db.GetChildren(taskID)
}

// AddComment adds a comment to a task
func (m *Manager) AddComment(taskID, author, body string) (*Comment, error) {
	comment := &Comment{
		ID:     generateID("cmt"),
		TaskID: taskID,
		Author: author,
		Body:   body,
	}

	if err := m.db.AddComment(comment); err != nil {
		return nil, err
	}

	return comment, nil
}

// ListComments lists comments for a task
func (m *Manager) ListComments(taskID string) ([]*Comment, error) {
	return m.db.ListComments(taskID)
}

// ListRuns lists execution runs for a task
func (m *Manager) ListRuns(taskID string) ([]*Run, error) {
	return m.db.ListRuns(taskID)
}

// GetBoard returns the kanban board view
func (m *Manager) GetBoard(tenant string) (map[TaskStatus][]*Task, error) {
	return m.db.GetBoard(tenant)
}

// GetStats returns task statistics
func (m *Manager) GetStats(tenant string) (map[TaskStatus]int, error) {
	return m.db.GetStats(tenant)
}

// TriageTask uses LLM to refine a triage task into detailed requirements
func (m *Manager) TriageTask(ctx context.Context, id string, prov provider.Provider) (*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusTriage {
		return nil, fmt.Errorf("task %s is not in triage status (current: %s)", id, task.Status)
	}

	// Create a prompt for the LLM to refine the task
	prompt := fmt.Sprintf(`You are a task refinement assistant. A task needs to be refined from a high-level description into detailed requirements.

Original task:
Title: %s
Description: %s

Please refine this task into:
1. A clear, specific title
2. A detailed description with:
   - Objective: What needs to be accomplished
   - Acceptance criteria: How success will be measured
   - Technical notes: Any relevant implementation details
   - Dependencies: What this task depends on

Respond in JSON format:
{
  "title": "refined title",
  "body": "detailed description with all sections",
  "priority": 0-3 (0=low, 1=medium, 2=high, 3=critical)
}

Only respond with the JSON, no additional text.`, task.Title, task.Body)

	messages := []provider.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := prov.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse the LLM response
	var refined struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Priority int    `json:"priority"`
	}

	if err := json.Unmarshal([]byte(resp.Content), &refined); err != nil {
		// Try to extract JSON from the response
		content := resp.Content
		startIdx := 0
		for i := 0; i < len(content); i++ {
			if content[i] == '{' {
				startIdx = i
				break
			}
		}
		endIdx := len(content)
		for i := len(content) - 1; i >= 0; i-- {
			if content[i] == '}' {
				endIdx = i + 1
				break
			}
		}
		if endIdx > startIdx {
			jsonStr := content[startIdx:endIdx]
			json.Unmarshal([]byte(jsonStr), &refined)
		}

		// If still failed, use the content as-is
		if refined.Title == "" {
			refined.Title = task.Title
			refined.Body = resp.Content
		}
	}

	// Update the task with refined values
	updates := map[string]interface{}{
		"title":    refined.Title,
		"body":     refined.Body,
		"priority": refined.Priority,
	}

	// Update status to todo (refinement complete)
	updates["status"] = string(StatusTodo)

	updatedTask, err := m.UpdateTask(id, updates)
	if err != nil {
		return nil, err
	}

	log.Infof("[Kanban] Task %s triaged and refined to: %s", id, refined.Title)

	return updatedTask, nil
}

// SplitTask uses LLM to split a task into subtasks
func (m *Manager) SplitTask(ctx context.Context, id string, prov provider.Provider) ([]*Task, error) {
	task, err := m.db.GetTask(id)
	if err != nil {
		return nil, err
	}

	prompt := fmt.Sprintf(`You are a task planning assistant. Please analyze the following task and break it down into 3-5 smaller, actionable subtasks.

Parent Task:
Title: %s
Description: %s

For each subtask, provide:
1. A clear, specific title
2. A brief description of what needs to be done
3. Estimated hours (realistic estimate)
4. Priority (0=low, 1=medium, 2=high, 3=critical)

Respond in JSON format:
{
  "subtasks": [
    {
      "title": "subtask title",
      "description": "subtask description",
      "estimated_hours": 2.5,
      "priority": 1
    }
  ]
}

Only respond with the JSON, no additional text.`, task.Title, task.Body)

	messages := []provider.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := prov.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse the LLM response
	var result struct {
		Subtasks []struct {
			Title          string  `json:"title"`
			Description    string  `json:"description"`
			EstimatedHours float64 `json:"estimated_hours"`
			Priority       int     `json:"priority"`
		} `json:"subtasks"`
	}

	content := resp.Content
	// Try to extract JSON
	startIdx := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '{' {
			startIdx = i
			break
		}
	}
	endIdx := len(content)
	for i := len(content) - 1; i >= 0; i-- {
		if content[i] == '}' {
			endIdx = i + 1
			break
		}
	}

	if endIdx > startIdx {
		jsonStr := content[startIdx:endIdx]
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			return nil, fmt.Errorf("failed to parse LLM response: %w", err)
		}
	}

	// Create subtasks
	var subtasks []*Task
	for _, st := range result.Subtasks {
		subtask, err := m.CreateTask(
			st.Title,
			st.Description,
			task.Assignee,
			WithPriority(st.Priority),
			WithTenant(task.Tenant),
			WithWorkspace(task.Workspace),
		)
		if err != nil {
			log.Warnf("[Kanban] Failed to create subtask: %v", err)
			continue
		}

		// Update estimated hours
		m.UpdateTask(subtask.ID, map[string]interface{}{
			"estimated_hours": st.EstimatedHours,
			"goal_id":         task.GoalID, // Inherit goal association
		})

		// Link to parent
		if err := m.AddLink(id, subtask.ID); err != nil {
			log.Warnf("[Kanban] Failed to link subtask: %v", err)
		}

		subtasks = append(subtasks, subtask)
	}

	log.Infof("[Kanban] Task %s split into %d subtasks", id, len(subtasks))
	return subtasks, nil
}

// AddParentLink is a helper to add a parent link during task creation
type AddParentLink struct {
	ParentID string
}

// WithParentID creates an option to link to a parent task
func WithParentID(parentID string) TaskOption {
	return func(t *Task) {
		// This will be handled by the caller after CreateTask
	}
}

// GetDispatcher returns the dispatcher (for testing)
func (m *Manager) GetDispatcher() *Dispatcher {
	return m.dispatcher
}

// GetDB returns the database (for testing)
func (m *Manager) GetDB() *KanbanDB {
	return m.db
}

// CreateTaskWithParent creates a task and links it to a parent
func (m *Manager) CreateTaskWithParent(title, body, assignee, parentID string, opts ...TaskOption) (*Task, error) {
	task, err := m.CreateTask(title, body, assignee, opts...)
	if err != nil {
		return nil, err
	}

	if parentID != "" {
		if err := m.AddLink(parentID, task.ID); err != nil {
			log.Warnf("[Kanban] Failed to link task %s to parent %s: %v", task.ID, parentID, err)
		}
	}

	return m.GetTask(task.ID)
}

// Heartbeat updates the task's updated_at timestamp (indicating the agent is alive)
func (m *Manager) Heartbeat(id string) error {
	task, err := m.db.GetTask(id)
	if err != nil {
		return err
	}

	task.UpdatedAt = time.Now()
	return m.db.UpdateTask(task)
}

// UpdateTaskWithPID updates a task's current run with a PID
func (m *Manager) UpdateTaskWithPID(id string, runID string, pid int) error {
	task, err := m.db.GetTask(id)
	if err != nil {
		return err
	}

	task.CurrentRunID = runID
	task.UpdatedAt = time.Now()

	// Update the run with PID
	run, err := m.db.GetCurrentRun(id)
	if err == nil && run != nil {
		run.PID = pid
		m.db.UpdateRun(run)
	}

	return m.db.UpdateTask(task)
}

// GenerateRunID generates a new unique run ID
func GenerateRunID() string {
	return fmt.Sprintf("run_%s", uuid.New().String()[:8])
}

// GetBurndownData returns burndown chart data for a time period
func (m *Manager) GetBurndownData(tenant string, days int) ([]BurndownPoint, error) {
	return m.db.GetBurndownData(tenant, days)
}

// GetThroughputStats returns task throughput statistics
func (m *Manager) GetThroughputStats(tenant string, days int) (*ThroughputStats, error) {
	return m.db.GetThroughputStats(tenant, days)
}

// BurndownPoint represents a point in the burndown chart
type BurndownPoint struct {
	Date      string `json:"date"`
	Total     int    `json:"total"`     // Total tasks at start of day
	Remaining int    `json:"remaining"` // Tasks not done
	Completed int    `json:"completed"` // Tasks completed that day
	Added     int    `json:"added"`     // Tasks added that day
}

// ThroughputStats represents throughput statistics
type ThroughputStats struct {
	TotalCreated     int     `json:"total_created"`           // Total tasks created
	TotalCompleted   int     `json:"total_completed"`         // Total tasks completed
	AverageLeadTime  float64 `json:"average_lead_time_hours"` // Average time from creation to completion
	ThroughputPerDay float64 `json:"throughput_per_day"`      // Average tasks completed per day
}
