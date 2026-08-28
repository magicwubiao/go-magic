package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// ProgressStatus represents the current progress assessment
type ProgressStatus string

const (
	ProgressOnTrack   ProgressStatus = "on_track"
	ProgressSlow      ProgressStatus = "slow"
	ProgressStuck     ProgressStatus = "stuck"
	ProgressOffTrack  ProgressStatus = "off_track"
	ProgressCompleted ProgressStatus = "completed"
)

// ReflectionResult holds the result of a self-reflection
type ReflectionResult struct {
	Status         ProgressStatus     `json:"status"`
	Progress       float64            `json:"progress"`       // 0.0 - 1.0
	GoalAlignment  float64            `json:"goal_alignment"` // 0.0 - 1.0
	Summary        string             `json:"summary"`
	Blockers       []string           `json:"blockers"`
	Achievements   []string           `json:"achievements"`
	NextSteps      []string           `json:"next_steps"`
	StrategyAdjust string             `json:"strategy_adjustment"`
	NeedReplan     bool               `json:"need_replan"`
	ToolEfficiency map[string]float64 `json:"tool_efficiency"`
	WarningFlags   []string           `json:"warning_flags"`
}

// ReflectionConfig configures the self-reflection mechanism
type ReflectionConfig struct {
	Enabled            bool
	Interval           int     // Reflect every N turns
	MaxReflections     int     // Max reflections per session
	StuckThreshold     float64 // Progress increase below this = stuck
	StuckConsecutive   int     // Consecutive low-progress rounds to trigger stuck
	EnableToolAnalysis bool
}

// DefaultReflectionConfig returns default reflection settings
func DefaultReflectionConfig() ReflectionConfig {
	return ReflectionConfig{
		Enabled:            true,
		Interval:           5,
		MaxReflections:     10,
		StuckThreshold:     0.05,
		StuckConsecutive:   2,
		EnableToolAnalysis: true,
	}
}

// Reflector manages self-reflection for an agent
type Reflector struct {
	mu              sync.Mutex
	config          ReflectionConfig
	provider        provider.Provider
	originalGoal    string
	reflectionCount int
	lastProgress    float64
	consecutiveLow  int
	toolCallRecords []toolCallRecord
	achievementLog  []string
	blockerLog      []string
	lastResult      *ReflectionResult
}

type toolCallRecord struct {
	Name      string
	Success   bool
	Timestamp time.Time
	Duration  time.Duration
}

// NewReflector creates a new self-reflection manager
func NewReflector(cfg ReflectionConfig, prov provider.Provider, goal string) *Reflector {
	return &Reflector{
		config:          cfg,
		provider:        prov,
		originalGoal:    goal,
		reflectionCount: 0,
		lastProgress:    0.0,
		consecutiveLow:  0,
		toolCallRecords: make([]toolCallRecord, 0),
		achievementLog:  make([]string, 0),
		blockerLog:      make([]string, 0),
	}
}

// ShouldReflect checks if it's time to reflect based on turn count
func (r *Reflector) ShouldReflect(turn int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.config.Enabled {
		return false
	}
	if r.reflectionCount >= r.config.MaxReflections {
		return false
	}
	return turn > 0 && turn%r.config.Interval == 0
}

// RecordToolCall records a tool call for later analysis
func (r *Reflector) RecordToolCall(name string, success bool, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolCallRecords = append(r.toolCallRecords, toolCallRecord{
		Name:      name,
		Success:   success,
		Timestamp: time.Now(),
		Duration:  duration,
	})
}

// RecordAchievement records something that was accomplished
func (r *Reflector) RecordAchievement(achievement string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.achievementLog = append(r.achievementLog, achievement)
}

// RecordBlocker records an obstacle encountered
func (r *Reflector) RecordBlocker(blocker string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blockerLog = append(r.blockerLog, blocker)
}

// Reflect performs a self-reflection using LLM
func (r *Reflector) Reflect(ctx context.Context, history []provider.Message, currentTurn int) (*ReflectionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.provider == nil {
		return r.fallbackReflect(currentTurn), nil
	}

	r.reflectionCount++

	// Build reflection prompt
	prompt := r.buildReflectionPrompt(history, currentTurn)

	messages := []provider.Message{
		{Role: "system", Content: reflectionSystemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := r.provider.Chat(ctx, messages)
	if err != nil {
		log.Warnf("[Reflection] LLM reflection failed, using fallback: %v", err)
		return r.fallbackReflect(currentTurn), nil
	}

	result, err := r.parseReflectionResponse(resp.Content)
	if err != nil {
		log.Warnf("[Reflection] Failed to parse reflection response: %v", err)
		return r.fallbackReflect(currentTurn), nil
	}

	// Update progress tracking
	progressDelta := result.Progress - r.lastProgress
	if progressDelta < r.config.StuckThreshold {
		r.consecutiveLow++
		if r.consecutiveLow >= r.config.StuckConsecutive {
			result.Status = ProgressStuck
			result.WarningFlags = append(result.WarningFlags,
				fmt.Sprintf("Progress stalled for %d consecutive rounds", r.consecutiveLow))
		}
	} else {
		r.consecutiveLow = 0
	}
	r.lastProgress = result.Progress
	r.lastResult = result

	log.Infof("[Reflection] Turn %d: status=%s, progress=%.0f%%, alignment=%.0f%%",
		currentTurn, result.Status, result.Progress*100, result.GoalAlignment*100)

	return result, nil
}

// reflectionSystemPrompt is the system prompt for the reflection LLM
const reflectionSystemPrompt = `You are an AI agent self-reflection analyzer. Your job is to objectively assess the agent's progress toward its goal.

Analyze the conversation history and provide:
1. Progress estimate (0-100%)
2. Goal alignment score (0-100%)
3. List of achievements so far
4. List of blockers/obstacles
5. Recommended next steps
6. Whether a strategy adjustment is needed
7. Warning flags for potential issues

Be honest and constructive. If the agent is going in circles, say so. If it's making good progress, acknowledge that.

Respond ONLY with valid JSON in this exact format:
{
  "status": "on_track|slow|stuck|off_track|completed",
  "progress": 0.0-1.0,
  "goal_alignment": 0.0-1.0,
  "summary": "brief summary of current state",
  "blockers": ["blocker1", "blocker2"],
  "achievements": ["achievement1", "achievement2"],
  "next_steps": ["step1", "step2"],
  "strategy_adjustment": "suggested adjustment or empty string if none needed",
  "need_replan": true/false,
  "tool_efficiency": {"tool_name": 0.0-1.0},
  "warning_flags": ["warning1"]
}`

func (r *Reflector) buildReflectionPrompt(history []provider.Message, turn int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Self-Reflection (Turn %d) ===\n\n", turn))
	sb.WriteString(fmt.Sprintf("Original Goal: %s\n\n", r.originalGoal))

	// Recent achievements and blockers
	if len(r.achievementLog) > 0 {
		sb.WriteString("Recent Achievements:\n")
		for i, a := range r.achievementLog {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(r.achievementLog)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", a))
		}
		sb.WriteString("\n")
	}

	if len(r.blockerLog) > 0 {
		sb.WriteString("Recent Blockers:\n")
		for i, b := range r.blockerLog {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(r.blockerLog)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", b))
		}
		sb.WriteString("\n")
	}

	// Tool call statistics
	if r.config.EnableToolAnalysis && len(r.toolCallRecords) > 0 {
		sb.WriteString("Tool Call Statistics:\n")
		toolStats := r.analyzeToolUsage()
		for tool, stats := range toolStats {
			sb.WriteString(fmt.Sprintf("  %s: %d calls, %.0f%% success rate\n",
				tool, stats.total, stats.successRate*100))
		}
		sb.WriteString("\n")
	}

	// Conversation history summary (last N messages)
	sb.WriteString("=== Recent Conversation ===\n\n")
	recentMsgs := r.getRecentMessages(history, 20)
	for _, msg := range recentMsgs {
		role := msg.Role
		// Strip <think> reasoning trails: raw deliberation text pollutes the
		// reflection analysis and can bias the reflector toward "the agent is
		// stuck in repetitive thinking" false positives.
		content := stripThinkContent(msg.Content)
		if len(content) > 500 {
			content = content[:500] + "... [truncated]"
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
	}

	sb.WriteString("\nPlease provide your reflection analysis as JSON.")

	return sb.String()
}

func (r *Reflector) getRecentMessages(history []provider.Message, count int) []provider.Message {
	if len(history) <= count {
		return history
	}
	return history[len(history)-count:]
}

type toolStats struct {
	total       int
	successes   int
	successRate float64
	totalTime   time.Duration
}

func (r *Reflector) analyzeToolUsage() map[string]*toolStats {
	stats := make(map[string]*toolStats)
	for _, tc := range r.toolCallRecords {
		if stats[tc.Name] == nil {
			stats[tc.Name] = &toolStats{}
		}
		s := stats[tc.Name]
		s.total++
		if tc.Success {
			s.successes++
		}
		s.totalTime += tc.Duration
	}
	for _, s := range stats {
		if s.total > 0 {
			s.successRate = float64(s.successes) / float64(s.total)
		}
	}
	return stats
}

func (r *Reflector) parseReflectionResponse(content string) (*ReflectionResult, error) {
	// Clean up markdown code blocks
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	// Try to find JSON in the response
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		content = content[jsonStart : jsonEnd+1]
	}

	var result ReflectionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse reflection JSON: %w", err)
	}

	// Validate and set defaults
	if result.ToolEfficiency == nil {
		result.ToolEfficiency = make(map[string]float64)
	}
	if result.Blockers == nil {
		result.Blockers = []string{}
	}
	if result.Achievements == nil {
		result.Achievements = []string{}
	}
	if result.NextSteps == nil {
		result.NextSteps = []string{}
	}
	if result.WarningFlags == nil {
		result.WarningFlags = []string{}
	}

	return &result, nil
}

// fallbackReflect provides a simple rule-based reflection when LLM is unavailable
func (r *Reflector) fallbackReflect(turn int) *ReflectionResult {
	progress := r.estimateProgressRuleBased(turn)

	status := ProgressOnTrack
	var warnings []string

	if turn > 10 && progress < 0.3 {
		status = ProgressSlow
		warnings = append(warnings, "Progress is slow relative to turns used")
	}
	if r.consecutiveLow >= r.config.StuckConsecutive {
		status = ProgressStuck
		warnings = append(warnings, "Progress appears stalled")
	}
	if progress >= 0.95 {
		status = ProgressCompleted
	}

	return &ReflectionResult{
		Status:         status,
		Progress:       progress,
		GoalAlignment:  0.8,
		Summary:        fmt.Sprintf("Rule-based reflection at turn %d", turn),
		Blockers:       []string{},
		Achievements:   r.achievementLog,
		NextSteps:      []string{"Continue current approach"},
		StrategyAdjust: "",
		NeedReplan:     status == ProgressStuck || status == ProgressOffTrack,
		ToolEfficiency: make(map[string]float64),
		WarningFlags:   warnings,
	}
}

func (r *Reflector) estimateProgressRuleBased(turn int) float64 {
	// Simple heuristic: progress based on achievements vs estimated total work
	if len(r.achievementLog) == 0 {
		return 0.0
	}
	// Assume each achievement advances progress, with diminishing returns
	estimate := 1.0 - 1.0/float64(len(r.achievementLog)+1)
	// Adjust for blockers
	if len(r.blockerLog) > 0 {
		estimate -= float64(len(r.blockerLog)) * 0.05
	}
	if estimate < 0 {
		estimate = 0
	}
	if estimate > 0.95 {
		estimate = 0.95
	}
	return estimate
}

// GetLastResult returns the most recent reflection result
func (r *Reflector) GetLastResult() *ReflectionResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastResult
}

// InjectReflectionPrompt injects reflection insights into the next LLM call
func (r *Reflector) InjectReflectionPrompt(result *ReflectionResult) string {
	if result == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n=== Self-Reflection Insights ===\n")
	sb.WriteString(fmt.Sprintf("Progress: %.0f%% | Status: %s\n", result.Progress*100, result.Status))
	sb.WriteString(fmt.Sprintf("Goal Alignment: %.0f%%\n", result.GoalAlignment*100))

	if len(result.Achievements) > 0 {
		sb.WriteString("\nAchievements so far:\n")
		for _, a := range result.Achievements {
			sb.WriteString(fmt.Sprintf("  ✓ %s\n", a))
		}
	}

	if len(result.Blockers) > 0 {
		sb.WriteString("\nCurrent blockers:\n")
		for _, b := range result.Blockers {
			sb.WriteString(fmt.Sprintf("  ⚠ %s\n", b))
		}
	}

	if result.StrategyAdjust != "" {
		sb.WriteString(fmt.Sprintf("\nStrategy Adjustment: %s\n", result.StrategyAdjust))
	}

	if len(result.NextSteps) > 0 {
		sb.WriteString("\nSuggested next steps:\n")
		for i, s := range result.NextSteps {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, s))
		}
	}

	if len(result.WarningFlags) > 0 {
		sb.WriteString("\n⚠ Warnings:\n")
		for _, w := range result.WarningFlags {
			sb.WriteString(fmt.Sprintf("  - %s\n", w))
		}
	}

	sb.WriteString("\nUse these insights to guide your next actions.\n")

	return sb.String()
}
