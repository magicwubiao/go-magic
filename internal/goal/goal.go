package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status represents goal status
type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusAbandoned Status = "abandoned"
)

// Goal represents a user goal
type Goal struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	Progress    int        `json:"progress"` // 0-100
	SessionIDs  []string   `json:"session_ids"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Manager manages goals
type Manager struct {
	mu    sync.RWMutex
	goals map[string]*Goal
	dir   string
}

// NewManager creates a new goal manager
func NewManager(dir string) (*Manager, error) {
	m := &Manager{
		goals: make(map[string]*Goal),
		dir:   dir,
	}

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Load existing goals
	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

// Create creates a new goal
func (m *Manager) Create(ctx context.Context, title, description string) (*Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	goal := &Goal{
		ID:          generateID(),
		Title:       title,
		Description: description,
		Status:      StatusActive,
		Progress:    0,
		SessionIDs:  []string{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.goals[goal.ID] = goal
	if err := m.save(); err != nil {
		delete(m.goals, goal.ID)
		return nil, err
	}

	return goal, nil
}

// Get gets a goal by ID
func (m *Manager) Get(ctx context.Context, id string) (*Goal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	goal, ok := m.goals[id]
	if !ok {
		return nil, fmt.Errorf("goal not found: %s", id)
	}

	return goal, nil
}

// List lists all goals
func (m *Manager) List(ctx context.Context, status Status) ([]*Goal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Goal
	for _, g := range m.goals {
		if status == "" || g.Status == status {
			result = append(result, g)
		}
	}

	// Sort by updated_at desc
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].UpdatedAt.After(result[i].UpdatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// Update updates a goal
func (m *Manager) Update(ctx context.Context, id string, updates map[string]interface{}) (*Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	goal, ok := m.goals[id]
	if !ok {
		return nil, fmt.Errorf("goal not found: %s", id)
	}

	if title, ok := updates["title"].(string); ok {
		goal.Title = title
	}
	if desc, ok := updates["description"].(string); ok {
		goal.Description = desc
	}
	if status, ok := updates["status"].(string); ok {
		goal.Status = Status(status)
		if goal.Status == StatusCompleted {
			now := time.Now()
			goal.CompletedAt = &now
			goal.Progress = 100
		}
	}
	if progress, ok := updates["progress"].(float64); ok {
		goal.Progress = int(progress)
		if goal.Progress > 100 {
			goal.Progress = 100
		}
		if goal.Progress < 0 {
			goal.Progress = 0
		}
	}

	goal.UpdatedAt = time.Now()

	if err := m.save(); err != nil {
		return nil, err
	}

	return goal, nil
}

// Delete deletes a goal
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.goals[id]; !ok {
		return fmt.Errorf("goal not found: %s", id)
	}

	delete(m.goals, id)
	return m.save()
}

// LinkSession links a session to a goal
func (m *Manager) LinkSession(ctx context.Context, goalID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	goal, ok := m.goals[goalID]
	if !ok {
		return fmt.Errorf("goal not found: %s", goalID)
	}

	// Check if already linked
	for _, id := range goal.SessionIDs {
		if id == sessionID {
			return nil
		}
	}

	goal.SessionIDs = append(goal.SessionIDs, sessionID)
	goal.UpdatedAt = time.Now()

	return m.save()
}

// UnlinkSession unlinks a session from a goal
func (m *Manager) UnlinkSession(ctx context.Context, goalID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	goal, ok := m.goals[goalID]
	if !ok {
		return fmt.Errorf("goal not found: %s", goalID)
	}

	var newIDs []string
	for _, id := range goal.SessionIDs {
		if id != sessionID {
			newIDs = append(newIDs, id)
		}
	}
	goal.SessionIDs = newIDs
	goal.UpdatedAt = time.Now()

	return m.save()
}

// GetGoalsBySession gets all goals linked to a session
func (m *Manager) GetGoalsBySession(ctx context.Context, sessionID string) ([]*Goal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Goal
	for _, g := range m.goals {
		for _, id := range g.SessionIDs {
			if id == sessionID {
				result = append(result, g)
				break
			}
		}
	}
	return result, nil
}

// save saves goals to disk
func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.goals, "", "  ")
	if err != nil {
		return err
	}

	// Ensure goals subdirectory exists
	goalsDir := filepath.Join(m.dir, "goals")
	if err := os.MkdirAll(goalsDir, 0755); err != nil {
		return err
	}

	file := filepath.Join(goalsDir, "goals.json")
	return os.WriteFile(file, data, 0644)
}

// load loads goals from disk
func (m *Manager) load() error {
	// Try new location first (goals/goals.json)
	goalsDir := filepath.Join(m.dir, "goals")
	file := filepath.Join(goalsDir, "goals.json")
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			// Try legacy location (goals.json in root dir) for migration
			legacyFile := filepath.Join(m.dir, "goals.json")
			legacyData, legacyErr := os.ReadFile(legacyFile)
			if legacyErr != nil {
				if os.IsNotExist(legacyErr) {
					return nil
				}
				return legacyErr
			}
			// Migrate to new location
			if err := os.MkdirAll(goalsDir, 0755); err != nil {
				return err
			}
			if err := os.WriteFile(file, legacyData, 0644); err != nil {
				return err
			}
			// Remove legacy file
			_ = os.Remove(legacyFile)
			data = legacyData
		} else {
			return err
		}
	}

	return json.Unmarshal(data, &m.goals)
}

func generateID() string {
	return fmt.Sprintf("goal_%d", time.Now().UnixNano())
}
