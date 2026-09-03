// Package budget provides iteration budget management for long-running tasks.
// Inspired by Hermes Agent's IterationBudget, this package prevents infinite loops
// and provides fine-grained control over agent execution iterations.
package budget

import (
	"sync"
	"time"
)

// Budget tracks iteration consumption for an agent or subagent.
// Each agent gets its own Budget instance to prevent resource exhaustion.
type Budget struct {
	maxTotal   int       // Maximum allowed iterations
	used       int       // Currently consumed iterations
	refunded   int       // Total refunded iterations
	createdAt  time.Time // Budget creation time
	lastUsedAt time.Time // Last consumption time
	mu         sync.RWMutex
}

// New creates a new Budget with the specified maximum iterations.
// Parent agents typically use maxIterations (default 90),
// subagents use delegation.maxIterations (default 50).
func New(maxTotal int) *Budget {
	now := time.Now()
	return &Budget{
		maxTotal:   maxTotal,
		used:       0,
		refunded:   0,
		createdAt:  now,
		lastUsedAt: now,
	}
}

// Consume attempts to consume one iteration.
// Returns true if the iteration was allowed, false if budget is exhausted.
func (b *Budget) Consume() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.used >= b.maxTotal {
		return false
	}
	b.used++
	b.lastUsedAt = time.Now()
	return true
}

// ConsumeN attempts to consume N iterations atomically.
// Returns the number of iterations actually consumed (may be less than N if budget runs out).
func (b *Budget) ConsumeN(n int) int {
	if n <= 0 {
		return 0
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.maxTotal - b.used
	if remaining <= 0 {
		return 0
	}

	if n > remaining {
		n = remaining
	}
	b.used += n
	b.lastUsedAt = time.Now()
	return n
}

// Refund returns one iteration to the budget.
// Useful for execute_code iterations that shouldn't count against the budget.
func (b *Budget) Refund() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.used > 0 {
		b.used--
		b.refunded++
	}
}

// RefundN returns N iterations to the budget.
func (b *Budget) RefundN(n int) {
	if n <= 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if n > b.used {
		n = b.used
	}
	b.used -= n
	b.refunded += n
}

// Used returns the number of consumed iterations.
func (b *Budget) Used() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.used
}

// Remaining returns the number of iterations remaining.
func (b *Budget) Remaining() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	remaining := b.maxTotal - b.used
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MaxTotal returns the maximum allowed iterations.
func (b *Budget) MaxTotal() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.maxTotal
}

// IsExhausted returns true if the budget is exhausted.
func (b *Budget) IsExhausted() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.used >= b.maxTotal
}

// UsagePercent returns the percentage of budget used (0.0 - 100.0).
func (b *Budget) UsagePercent() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.maxTotal == 0 {
		return 0
	}
	return float64(b.used) / float64(b.maxTotal) * 100
}

// Stats returns comprehensive budget statistics.
type Stats struct {
	MaxTotal     int
	Used         int
	Remaining    int
	Refunded     int
	UsagePercent float64
	IsExhausted  bool
	CreatedAt    time.Time
	LastUsedAt   time.Time
	Duration     time.Duration // Time since creation
}

// GetStats returns comprehensive budget statistics.
func (b *Budget) GetStats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	remaining := b.maxTotal - b.used
	if remaining < 0 {
		remaining = 0
	}

	usagePercent := float64(0)
	if b.maxTotal > 0 {
		usagePercent = float64(b.used) / float64(b.maxTotal) * 100
	}

	return Stats{
		MaxTotal:     b.maxTotal,
		Used:         b.used,
		Remaining:    remaining,
		Refunded:     b.refunded,
		UsagePercent: usagePercent,
		IsExhausted:  b.used >= b.maxTotal,
		CreatedAt:    b.createdAt,
		LastUsedAt:   b.lastUsedAt,
		Duration:     time.Since(b.createdAt),
	}
}

// Manager manages budgets for multiple agents/tasks.
type Manager struct {
	budgets map[string]*Budget
	mu      sync.RWMutex
}

// NewManager creates a new budget manager.
func NewManager() *Manager {
	return &Manager{
		budgets: make(map[string]*Budget),
	}
}

// Create creates a new budget for a task/agent.
func (m *Manager) Create(id string, maxTotal int) *Budget {
	m.mu.Lock()
	defer m.mu.Unlock()

	budget := New(maxTotal)
	m.budgets[id] = budget
	return budget
}

// Get retrieves a budget by ID.
func (m *Manager) Get(id string) (*Budget, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	budget, ok := m.budgets[id]
	return budget, ok
}

// Delete removes a budget.
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.budgets, id)
}

// List returns all budget IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.budgets))
	for id := range m.budgets {
		ids = append(ids, id)
	}
	return ids
}

// GetGlobalStats returns aggregated statistics across all budgets.
func (m *Manager) GetGlobalStats() (total, used, remaining int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, b := range m.budgets {
		total += b.maxTotal
		used += b.used
		remaining += b.Remaining()
	}
	return
}

// Default budget configurations
const (
	// DefaultMaxIterations for parent agents
	DefaultMaxIterations = 90
	// DefaultSubagentMaxIterations for subagents
	DefaultSubagentMaxIterations = 50
	// DefaultSimpleTaskMaxIterations for simple tasks
	DefaultSimpleTaskMaxIterations = 30
	// DefaultComplexTaskMaxIterations for complex tasks
	DefaultComplexTaskMaxIterations = 150
)

// Preset creates a budget with preset configurations.
func Preset(presetType string) *Budget {
	switch presetType {
	case "parent":
		return New(DefaultMaxIterations)
	case "subagent":
		return New(DefaultSubagentMaxIterations)
	case "simple":
		return New(DefaultSimpleTaskMaxIterations)
	case "complex":
		return New(DefaultComplexTaskMaxIterations)
	default:
		return New(DefaultMaxIterations)
	}
}
