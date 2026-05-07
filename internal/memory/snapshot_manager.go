package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Character limits (not tokens) because char counts are model-independent
// This simulates human memory - you don't remember every word, just the conclusions
const (
	MemoryLimitChars = 2200 // MEMORY.md max chars
	UserLimitChars   = 1375 // USER.md max chars
)

// SnapshotManager implements the "frozen snapshot" pattern from Cortex Agent
// Memory updates are written to disk immediately but
// the current turn uses a frozen snapshot to protect prefix cache
// This is crucial for cost optimization with Anthropic's prefix caching
type SnapshotManager struct {
	mu             sync.RWMutex
	memoryPath     string
	userPath       string
	frozenMemory   string // Snapshot for current turn
	frozenUser     string
	latestMemory   string // Latest version (on disk)
	latestUser     string
	version        int
	compressor     *MemoryCompressor
}

// MemoryCompressor handles memory summarization when limits are reached
type MemoryCompressor struct {
	// Can be enhanced with LLM-based summarization
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(baseDir string) *SnapshotManager {
	return &SnapshotManager{
		memoryPath: filepath.Join(baseDir, "MEMORY.md"),
		userPath:   filepath.Join(baseDir, "USER.md"),
		compressor: &MemoryCompressor{},
		version:    1,
	}
}

// Load loads the latest memory from disk
func (sm *SnapshotManager) Load() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Load MEMORY.md
	if content, err := os.ReadFile(sm.memoryPath); err == nil {
		sm.latestMemory = string(content)
		sm.frozenMemory = sm.latestMemory
	}

	// Load USER.md
	if content, err := os.ReadFile(sm.userPath); err == nil {
		sm.latestUser = string(content)
		sm.frozenUser = sm.latestUser
	}

	return nil
}

// OnTurnStart is called at the beginning of each turn
// Uses the frozen snapshot for this turn, does NOT refresh
func (sm *SnapshotManager) OnTurnStart() {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	// frozenMemory remains as-is for the entire turn
	// This protects prefix cache from being invalidated mid-conversation
}

// RefreshSnapshot is called at session end or start of a new conversation
// Refreshes the frozen snapshot with latest memory
func (sm *SnapshotManager) RefreshSnapshot() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.frozenMemory = sm.latestMemory
	sm.frozenUser = sm.latestUser
	sm.version++
}

// GetMemoryForPrompt returns the memory content to include in system prompt
// Uses the frozen snapshot, NOT the latest
func (sm *SnapshotManager) GetMemoryForPrompt() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.frozenMemory
}

// GetUserForPrompt returns the user profile to include in system prompt
// Uses the frozen snapshot, NOT the latest
func (sm *SnapshotManager) GetUserForPrompt() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.frozenUser
}

// UpdateMemory updates memory, writes to disk immediately
// but does NOT refresh the frozen snapshot
func (sm *SnapshotManager) UpdateMemory(content string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Apply character limit
	if len(content) > MemoryLimitChars {
		content = sm.compressor.compressMemory(content, MemoryLimitChars)
	}

	sm.latestMemory = content
	return os.WriteFile(sm.memoryPath, []byte(content), 0644)
}

// UpdateUser updates user profile, writes to disk immediately
// but does NOT refresh the frozen snapshot
func (sm *SnapshotManager) UpdateUser(content string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Apply character limit
	if len(content) > UserLimitChars {
		content = sm.compressor.compressUser(content, UserLimitChars)
	}

	sm.latestUser = content
	return os.WriteFile(sm.userPath, []byte(content), 0644)
}

// AppendToMemory appends a line to memory
func (sm *SnapshotManager) AppendToMemory(line string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	newContent := sm.latestMemory
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += line + "\n"

	// Check limit and compress if needed
	if len(newContent) > MemoryLimitChars {
		newContent = sm.compressor.compressMemory(newContent, MemoryLimitChars)
	}

	sm.latestMemory = newContent
	return os.WriteFile(sm.memoryPath, []byte(newContent), 0644)
}

// AppendToUser appends a line to user profile
func (sm *SnapshotManager) AppendToUser(line string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	newContent := sm.latestUser
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += line + "\n"

	// Check limit and compress if needed
	if len(newContent) > UserLimitChars {
		newContent = sm.compressor.compressUser(newContent, UserLimitChars)
	}

	sm.latestUser = newContent
	return os.WriteFile(sm.userPath, []byte(newContent), 0644)
}

// GetLatestMemory returns the latest memory (not frozen)
func (sm *SnapshotManager) GetLatestMemory() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.latestMemory
}

// GetLatestUser returns the latest user profile (not frozen)
func (sm *SnapshotManager) GetLatestUser() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.latestUser
}

// GetVersion returns the current memory version
func (sm *SnapshotManager) GetVersion() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.version
}

// compressMemory compresses memory content to fit within limit
func (mc *MemoryCompressor) compressMemory(content string, limit int) string {
	// Simple compression: keep first lines and last lines,
	// with a summary note in between
	// In production, this would use LLM summarization

	lines := strings.Split(content, "\n")
	if len(lines) <= 10 {
		return truncateString(content, limit)
	}

	// Keep header + first 5 lines
	result := strings.Join(lines[:5], "\n") + "\n"
	result += "\n[... compressed memory ...]\n\n"
	// Keep last 5 lines
	result += strings.Join(lines[len(lines)-5:], "\n")

	return truncateString(result, limit)
}

// compressUser compresses user profile to fit within limit
func (mc *MemoryCompressor) compressUser(content string, limit int) string {
	return truncateString(content, limit)
}

func truncateString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}
