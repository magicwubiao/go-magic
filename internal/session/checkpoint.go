package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Checkpoint represents a session checkpoint for recovery
type Checkpoint struct {
	SessionID   string          `json:"session_id"`
	Platform    string          `json:"platform"`
	ChannelID   string          `json:"channel_id"`
	UserID      string          `json:"user_id"`
	Messages    []types.Message `json:"messages"`
	AgentState  json.RawMessage `json:"agent_state,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Interrupted bool            `json:"interrupted"` // true if gateway was shutdown while session was active
}

// CheckpointManager manages session checkpoints
type CheckpointManager struct {
	dir string // ~/.magic/checkpoints/
	mu  sync.RWMutex
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager() (*CheckpointManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".magic", "checkpoints")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	return &CheckpointManager{dir: dir}, nil
}

// checkpointPath returns the file path for a session checkpoint
func (cm *CheckpointManager) checkpointPath(sessionID string) string {
	// Sanitize sessionID for filesystem safety
	safeID := strings.ReplaceAll(sessionID, "/", "_")
	return filepath.Join(cm.dir, safeID+".json")
}

// Save saves a checkpoint to disk
func (cm *CheckpointManager) Save(cp *Checkpoint) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cp.UpdatedAt = time.Now()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = cp.UpdatedAt
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}

	path := cm.checkpointPath(cp.SessionID)
	return os.WriteFile(path, data, 0644)
}

// Load loads a checkpoint from disk
func (cm *CheckpointManager) Load(sessionID string) (*Checkpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	path := cm.checkpointPath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}

	return &cp, nil
}

// Delete removes a checkpoint from disk
func (cm *CheckpointManager) Delete(sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	path := cm.checkpointPath(sessionID)
	return os.Remove(path)
}

// ListInterrupted returns all checkpoints marked as interrupted
func (cm *CheckpointManager) ListInterrupted() ([]*Checkpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	entries, err := os.ReadDir(cm.dir)
	if err != nil {
		return nil, err
	}

	var checkpoints []*Checkpoint
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(cm.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("[Checkpoint] Failed to read %s: %v", path, err)
			continue
		}

		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			log.Warnf("[Checkpoint] Failed to parse %s: %v", path, err)
			continue
		}

		if cp.Interrupted {
			checkpoints = append(checkpoints, &cp)
		}
	}

	return checkpoints, nil
}

// ListAll returns all checkpoints
func (cm *CheckpointManager) ListAll() ([]*Checkpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	entries, err := os.ReadDir(cm.dir)
	if err != nil {
		return nil, err
	}

	var checkpoints []*Checkpoint
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(cm.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("[Checkpoint] Failed to read %s: %v", path, err)
			continue
		}

		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			log.Warnf("[Checkpoint] Failed to parse %s: %v", path, err)
			continue
		}

		checkpoints = append(checkpoints, &cp)
	}

	return checkpoints, nil
}

// Prune removes checkpoints older than maxAge (default 7 days)
func (cm *CheckpointManager) Prune(maxAge time.Duration) error {
	if maxAge == 0 {
		maxAge = 7 * 24 * time.Hour // Default 7 days
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	entries, err := os.ReadDir(cm.dir)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(cm.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				log.Warnf("[Checkpoint] Failed to prune %s: %v", path, err)
			} else {
				log.Infof("[Checkpoint] Pruned old checkpoint: %s", entry.Name())
			}
		}
	}

	return nil
}

// MarkInterrupted marks a session as interrupted (gateway shutdown)
func (cm *CheckpointManager) MarkInterrupted(sessionID string) error {
	cp, err := cm.Load(sessionID)
	if err != nil {
		// No checkpoint exists, nothing to mark
		return nil
	}

	cp.Interrupted = true
	return cm.Save(cp)
}

// ClearInterrupted clears the interrupted flag after successful recovery
func (cm *CheckpointManager) ClearInterrupted(sessionID string) error {
	cp, err := cm.Load(sessionID)
	if err != nil {
		return err
	}

	cp.Interrupted = false
	return cm.Save(cp)
}
