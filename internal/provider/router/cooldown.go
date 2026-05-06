package router

import (
	"sync"
	"time"
)

// CooldownTracker tracks provider cooldown state for failover
type CooldownTracker struct {
	mu       sync.RWMutex
	failures map[string]failureRecord
	success  map[string]time.Time
}

// failureRecord tracks failure history for a provider
type failureRecord struct {
	Count     int
	Reason    FailoverReason
	FirstFail time.Time
	LastFail  time.Time
}

// DefaultCooldownDuration is the default cooldown period after failure
const DefaultCooldownDuration = 30 * time.Second

// MaxCooldownDuration is the maximum cooldown period
const MaxCooldownDuration = 5 * time.Minute

// NewCooldownTracker creates a new cooldown tracker
func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		failures: make(map[string]failureRecord),
		success:  make(map[string]time.Time),
	}
}

// IsAvailable checks if a provider is available (not in cooldown)
func (c *CooldownTracker) IsAvailable(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	record, ok := c.failures[key]
	if !ok {
		return true
	}

	// Check if cooldown has expired
	cooldown := c.calculateCooldown(record)
	elapsed := time.Since(record.LastFail)
	return elapsed >= cooldown
}

// Remaining returns the remaining cooldown time
func (c *CooldownTracker) Remaining(key string) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	record, ok := c.failures[key]
	if !ok {
		return 0
	}

	cooldown := c.calculateCooldown(record)
	elapsed := time.Since(record.LastFail)
	remaining := cooldown - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CooldownPeriod returns the configured cooldown period
func (c *CooldownTracker) CooldownPeriod(key string) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	record, ok := c.failures[key]
	if !ok {
		return DefaultCooldownDuration
	}
	return c.calculateCooldown(record)
}

// MarkFailure records a failure for a provider
func (c *CooldownTracker) MarkFailure(key string, reason FailoverReason) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	record, ok := c.failures[key]
	if !ok {
		record = failureRecord{
			FirstFail: now,
		}
	}

	record.Count++
	record.Reason = reason
	record.LastFail = now
	c.failures[key] = record
}

// MarkSuccess records a success and resets failure count
func (c *CooldownTracker) MarkSuccess(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.failures, key)
	c.success[key] = time.Now()
}

// GetFailureCount returns the number of consecutive failures
func (c *CooldownTracker) GetFailureCount(key string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	record, ok := c.failures[key]
	if !ok {
		return 0
	}
	return record.Count
}

// calculateCooldown calculates cooldown duration based on failure count
func (c *CooldownTracker) calculateCooldown(record failureRecord) time.Duration {
	// Exponential backoff: 30s, 60s, 120s, 240s, 300s max
	base := DefaultCooldownDuration
	cooldown := base * time.Duration(1<<min(record.Count-1, 4))
	if cooldown > MaxCooldownDuration {
		cooldown = MaxCooldownDuration
	}
	return cooldown
}

// Clear resets all cooldown state
func (c *CooldownTracker) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failures = make(map[string]failureRecord)
	c.success = make(map[string]time.Time)
}

// Stats returns cooldown statistics
type CooldownStats struct {
	FailedProviders  int
	ActiveCooldowns  map[string]time.Duration
	SuccessProviders int
}

func (c *CooldownTracker) Stats() CooldownStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CooldownStats{
		ActiveCooldowns: make(map[string]time.Duration),
	}
	stats.FailedProviders = len(c.failures)
	stats.SuccessProviders = len(c.success)

	now := time.Now()
	for key, record := range c.failures {
		cooldown := c.calculateCooldown(record)
		elapsed := now.Sub(record.LastFail)
		remaining := cooldown - elapsed
		if remaining > 0 {
			stats.ActiveCooldowns[key] = remaining
		}
	}

	return stats
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
