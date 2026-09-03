package cortex

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrajectoryStep represents a single step in an execution trajectory
type TrajectoryStep struct {
	ToolName   string                 `json:"tool_name"`
	ToolInput  string                 `json:"tool_input"`
	ToolOutput string                 `json:"tool_output"`
	Success    bool                   `json:"success"`
	Duration   time.Duration          `json:"duration"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Trajectory represents a complete task execution trajectory
type Trajectory struct {
	ID          string           `json:"id"`
	Task        string           `json:"task"`
	Description string           `json:"description"`
	Steps       []TrajectoryStep `json:"steps"`
	Result      string           `json:"result"`
	Success     bool             `json:"success"`
	Duration    time.Duration    `json:"duration"`
	StartTime   time.Time        `json:"start_time"`
	EndTime     time.Time        `json:"end_time"`
	Model       string           `json:"model,omitempty"`
	Provider    string           `json:"provider,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	Score       float64          `json:"score,omitempty"`
}

// TrajectoryPattern represents a learned pattern from trajectories
type TrajectoryPattern struct {
	ID           string    `json:"id"`
	Pattern      string    `json:"pattern"`
	ToolSequence []string  `json:"tool_sequence"`
	SuccessRate  float64   `json:"success_rate"`
	Occurrences  int       `json:"occurrences"`
	LastSeen     time.Time `json:"last_seen"`
	SkillName    string    `json:"skill_name,omitempty"`
}

// TrajectoryStore manages trajectory storage and learning
type TrajectoryStore struct {
	mu            sync.RWMutex
	baseDir       string
	trajectoryDir string
	patternDir    string
	trajectories  []Trajectory
	patterns      []TrajectoryPattern
}

// NewTrajectoryStore creates a new trajectory store
func NewTrajectoryStore(baseDir string) (*TrajectoryStore, error) {
	ts := &TrajectoryStore{
		baseDir:       baseDir,
		trajectoryDir: filepath.Join(baseDir, "trajectories"),
		patternDir:    filepath.Join(baseDir, "patterns"),
		trajectories:  make([]Trajectory, 0),
		patterns:      make([]TrajectoryPattern, 0),
	}

	// Create directories
	if err := os.MkdirAll(ts.trajectoryDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(ts.patternDir, 0755); err != nil {
		return nil, err
	}

	// Load existing trajectories
	ts.loadTrajectories()
	ts.loadPatterns()

	return ts, nil
}

// RecordTrajectory records a completed trajectory
func (ts *TrajectoryStore) RecordTrajectory(trajectory *Trajectory) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Set timestamps
	if trajectory.StartTime.IsZero() {
		trajectory.StartTime = time.Now()
	}
	trajectory.EndTime = time.Now()
	trajectory.Duration = trajectory.EndTime.Sub(trajectory.StartTime)

	// Generate ID if not set
	if trajectory.ID == "" {
		trajectory.ID = generateTrajectoryID(trajectory)
	}

	// Add to memory
	ts.trajectories = append(ts.trajectories, *trajectory)

	// Save to disk
	if err := ts.saveTrajectory(trajectory); err != nil {
		return err
	}

	// Learn patterns from this trajectory
	ts.learnPattern(trajectory)

	// Evict old trajectories if needed
	ts.evictOldTrajectories()

	return nil
}

// saveTrajectory saves a single trajectory to disk
func (ts *TrajectoryStore) saveTrajectory(trajectory *Trajectory) error {
	filename := filepath.Join(ts.trajectoryDir, trajectory.ID+".json")
	data, err := json.MarshalIndent(trajectory, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// loadTrajectories loads all trajectories from disk
func (ts *TrajectoryStore) loadTrajectories() {
	entries, err := os.ReadDir(ts.trajectoryDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(ts.trajectoryDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var trajectory Trajectory
		if err := json.Unmarshal(data, &trajectory); err != nil {
			continue
		}

		ts.trajectories = append(ts.trajectories, trajectory)
	}
}

// learnPattern extracts patterns from successful trajectories
func (ts *TrajectoryStore) learnPattern(trajectory *Trajectory) {
	if !trajectory.Success || len(trajectory.Steps) < 2 {
		return
	}

	// Extract tool sequence
	var toolSeq []string
	for _, step := range trajectory.Steps {
		toolSeq = append(toolSeq, step.ToolName)
	}

	patternKey := joinToolSequence(toolSeq)

	// Check if pattern already exists
	for i, p := range ts.patterns {
		if p.Pattern == patternKey {
			// Update existing pattern
			ts.patterns[i].Occurrences++
			ts.patterns[i].LastSeen = time.Now()
			// 增量更新成功率：新成功率 = (旧成功率×(total-1) + 本次结果) / total
			// 成功本次结果记 1，失败记 0
			total := float64(ts.patterns[i].Occurrences)
			outcome := 0.0
			if trajectory.Success {
				outcome = 1.0
			}
			ts.patterns[i].SuccessRate = (ts.patterns[i].SuccessRate*(total-1) + outcome) / total
			ts.savePattern(&ts.patterns[i])
			return
		}
	}

	// Create new pattern
	newPattern := TrajectoryPattern{
		ID:           generatePatternID(patternKey),
		Pattern:      patternKey,
		ToolSequence: toolSeq,
		SuccessRate:  1.0,
		Occurrences:  1,
		LastSeen:     time.Now(),
	}

	ts.patterns = append(ts.patterns, newPattern)
	ts.savePattern(&newPattern)
}

// savePattern saves a pattern to disk
func (ts *TrajectoryStore) savePattern(pattern *TrajectoryPattern) error {
	filename := filepath.Join(ts.patternDir, pattern.ID+".json")
	data, err := json.MarshalIndent(pattern, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// loadPatterns loads all patterns from disk
func (ts *TrajectoryStore) loadPatterns() {
	entries, err := os.ReadDir(ts.patternDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(ts.patternDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var pattern TrajectoryPattern
		if err := json.Unmarshal(data, &pattern); err != nil {
			continue
		}

		ts.patterns = append(ts.patterns, pattern)
	}
}

// evictOldTrajectories removes old trajectories to save space
func (ts *TrajectoryStore) evictOldTrajectories() {
	maxTrajectories := 1000
	if len(ts.trajectories) <= maxTrajectories {
		return
	}

	// Sort by end time (oldest first) using stdlib instead of O(n^2) selection sort.
	sort.Slice(ts.trajectories, func(i, j int) bool {
		return ts.trajectories[i].EndTime.Before(ts.trajectories[j].EndTime)
	})

	// Remove oldest
	toRemove := len(ts.trajectories) - maxTrajectories
	for i := 0; i < toRemove; i++ {
		trajectory := ts.trajectories[i]
		path := filepath.Join(ts.trajectoryDir, trajectory.ID+".json")
		os.Remove(path)
	}

	ts.trajectories = ts.trajectories[toRemove:]

	// 被删除轨迹衍生的 pattern 仍会残留，这里基于剩余轨迹重新计算 pattern 统计，
	// 并清理无任何剩余轨迹支撑的 pattern。
	ts.recomputePatterns()
}

// recomputePatterns 基于当前剩余轨迹重新计算 pattern 的 Occurrences 与 SuccessRate，
// 并清理无任何剩余轨迹支撑的 pattern 及其磁盘文件。
func (ts *TrajectoryStore) recomputePatterns() {
	type agg struct {
		occurrences int
		successes   int
		lastSeen    time.Time
		toolSeq     []string
	}
	aggMap := make(map[string]*agg)

	for i := range ts.trajectories {
		t := &ts.trajectories[i]
		if len(t.Steps) < 2 {
			continue
		}
		var toolSeq []string
		toolSeq = append(toolSeq, t.Steps[0].ToolName)
		for _, step := range t.Steps[1:] {
			toolSeq = append(toolSeq, step.ToolName)
		}
		key := joinToolSequence(toolSeq)
		a, ok := aggMap[key]
		if !ok {
			a = &agg{toolSeq: toolSeq}
			aggMap[key] = a
		}
		a.occurrences++
		if t.Success {
			a.successes++
		}
		if t.EndTime.After(a.lastSeen) {
			a.lastSeen = t.EndTime
		}
	}

	// 重建 patterns，保留原 pattern 的 SkillName（若有）
	newPatterns := make([]TrajectoryPattern, 0, len(aggMap))
	for key, a := range aggMap {
		var successRate float64
		if a.occurrences > 0 {
			successRate = float64(a.successes) / float64(a.occurrences)
		}
		pattern := TrajectoryPattern{
			ID:           generatePatternID(key),
			Pattern:      key,
			ToolSequence: a.toolSeq,
			SuccessRate:  successRate,
			Occurrences:  a.occurrences,
			LastSeen:     a.lastSeen,
		}
		for _, old := range ts.patterns {
			if old.Pattern == key && old.SkillName != "" {
				pattern.SkillName = old.SkillName
				break
			}
		}
		newPatterns = append(newPatterns, pattern)
		ts.savePattern(&pattern)
	}

	// 删除不再有剩余轨迹支撑的 pattern 磁盘文件
	newKeys := make(map[string]bool, len(newPatterns))
	for _, p := range newPatterns {
		newKeys[p.Pattern] = true
	}
	for _, old := range ts.patterns {
		if !newKeys[old.Pattern] {
			path := filepath.Join(ts.patternDir, old.ID+".json")
			os.Remove(path)
		}
	}

	ts.patterns = newPatterns
}

// GetTrajectories returns all trajectories
func (ts *TrajectoryStore) GetTrajectories(limit int) []Trajectory {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if limit <= 0 || limit > len(ts.trajectories) {
		limit = len(ts.trajectories)
	}

	return ts.trajectories[len(ts.trajectories)-limit:]
}

// GetPatterns returns all learned patterns
func (ts *TrajectoryStore) GetPatterns(minSuccessRate float64) []TrajectoryPattern {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var filtered []TrajectoryPattern
	for _, p := range ts.patterns {
		if p.SuccessRate >= minSuccessRate {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// GetTopPatterns returns the most successful patterns
func (ts *TrajectoryStore) GetTopPatterns(limit int) []TrajectoryPattern {
	patterns := ts.GetPatterns(0) // Get all

	// Sort by success rate * occurrences (descending) using stdlib instead of O(n^2) selection sort.
	sort.Slice(patterns, func(i, j int) bool {
		scoreI := patterns[i].SuccessRate * float64(patterns[i].Occurrences)
		scoreJ := patterns[j].SuccessRate * float64(patterns[j].Occurrences)
		return scoreI > scoreJ
	})

	if limit > len(patterns) {
		limit = len(patterns)
	}
	return patterns[:limit]
}

// SearchTrajectories searches trajectories by task description
func (ts *TrajectoryStore) SearchTrajectories(query string, limit int) []Trajectory {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var results []Trajectory
	for _, t := range ts.trajectories {
		if containsIgnoreCase(t.Task, query) || containsIgnoreCase(t.Description, query) {
			results = append(results, t)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results
}

// GetStats returns trajectory statistics
func (ts *TrajectoryStore) GetStats() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var totalDuration time.Duration
	var successCount, totalSteps int

	for _, t := range ts.trajectories {
		totalDuration += t.Duration
		if t.Success {
			successCount++
		}
		totalSteps += len(t.Steps)
	}

	total := len(ts.trajectories)
	if total == 0 {
		return map[string]interface{}{
			"total_trajectories": 0,
			"total_patterns":     len(ts.patterns),
			"success_rate":       0,
		}
	}

	return map[string]interface{}{
		"total_trajectories": total,
		"total_patterns":     len(ts.patterns),
		"success_rate":       float64(successCount) / float64(total),
		"avg_duration":       totalDuration / time.Duration(total),
		"avg_steps":          float64(totalSteps) / float64(total),
		"total_duration":     totalDuration,
	}
}

// Clear removes all trajectories and patterns
func (ts *TrajectoryStore) Clear() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Clear trajectories
	for _, t := range ts.trajectories {
		path := filepath.Join(ts.trajectoryDir, t.ID+".json")
		os.Remove(path)
	}
	ts.trajectories = make([]Trajectory, 0)

	// Clear patterns
	for _, p := range ts.patterns {
		path := filepath.Join(ts.patternDir, p.ID+".json")
		os.Remove(path)
	}
	ts.patterns = make([]TrajectoryPattern, 0)

	return nil
}

// Helper functions

func generateTrajectoryID(t *Trajectory) string {
	return fmt.Sprintf("traj_%d_%04d", t.StartTime.UnixNano(), globalRandIntn(10000))
}

// globalRng provides a concurrency-safe source of random numbers for
// generatePatternID and other package-level helpers that need randomness
// but don't have access to a StrategyGenerator.rng.
var globalRng struct {
	sync.Mutex
	r *rand.Rand
}

func init() {
	globalRng.r = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func globalRandIntn(n int) int {
	if n <= 0 {
		return 0
	}
	globalRng.Lock()
	defer globalRng.Unlock()
	return globalRng.r.Intn(n)
}

func generatePatternID(pattern string) string {
	// Use FNV-1a hash to avoid collisions from int overflow
	h := fnv.New32a()
	h.Write([]byte(pattern))
	return fmt.Sprintf("pattern_%x", h.Sum32())
}

func joinToolSequence(seq []string) string {
	result := ""
	for i, s := range seq {
		if i > 0 {
			result += " -> "
		}
		result += s
	}
	return result
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
