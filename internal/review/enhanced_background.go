package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// EnhancedBackgroundReview is System 3 of Cortex Agent:
// "Background Review - an independent daemon that runs asynchronously,
// specifically designed to summarize experiences without interrupting the user conversation."
type EnhancedBackgroundReview struct {
	mu              sync.RWMutex
	baseDir         string
	reviewLog       []ReviewEntry
	patternsFound   []DetectedPattern
	isReviewing     bool
	stats           *ReviewStats
	scheduledTask   *time.Timer
	config          *ReviewConfig
	
	// Pattern tracking
	toolFrequency   map[string]int
	sessionHistory  []SessionSnapshot
	errorPatterns   []ErrorPattern
	
	// Performance metrics
	latencyTracker  []time.Duration
}

// ReviewConfig holds configuration for the review system
type ReviewConfig struct {
	ReviewInterval   time.Duration
	MinPatternFreq   int
	MaxPatterns      int
	AutoSaveEnabled  bool
	SnapshotInterval int
}

// ReviewStats holds aggregated statistics
type ReviewStats struct {
	TotalReviews      int
	TotalPatterns     int
	TotalSkillsSuggested int
	AvgReviewDuration time.Duration
	LastReviewTime    time.Time
	TopPatterns       []PatternRanking
}

// PatternRanking represents a pattern with its frequency
type PatternRanking struct {
	Pattern   string `json:"pattern"`
	Frequency int    `json:"frequency"`
}

// SessionSnapshot captures a point in time for session analysis
type SessionSnapshot struct {
	Timestamp   time.Time
	TurnCount   int
	ToolCount   int
	Errors      int
	Duration    time.Duration
}

// DetectedPattern is an enhanced pattern with richer metadata
type DetectedPattern struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ToolSequence []string  `json:"tool_sequence"`
	Frequency    int       `json:"frequency"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	SuccessRate  float64   `json:"success_rate"`
	AvgDuration  time.Duration `json:"avg_duration"`
	Contexts     []string  `json:"contexts"`
	Score        float64   `json:"score"` // Computed importance score
}

// ErrorPattern represents a recurring error pattern
type ErrorPattern struct {
	ErrorType    string    `json:"error_type"`
	ToolSequence []string  `json:"tool_sequence"`
	Frequency    int       `json:"frequency"`
	LastSeen     time.Time `json:"last_seen"`
	Suggestions  []string  `json:"suggestions"`
}

// ReviewEntry represents a single review session (enhanced)
type ReviewEntry struct {
	ID            string            `json:"id"`
	Timestamp     time.Time         `json:"timestamp"`
	TurnCount     int               `json:"turn_count"`
	Summary       string            `json:"summary"`
	Patterns      []DetectedPattern `json:"patterns"`
	SkillsSuggested []string        `json:"skills_suggested"`
	Duration      time.Duration     `json:"duration"`
	ErrorSummary  string            `json:"error_summary,omitempty"`
	PerformanceMetrics *PerformanceMetrics `json:"performance_metrics,omitempty"`
}

// PerformanceMetrics captures detailed performance data
type PerformanceMetrics struct {
	PerceptionLatency  time.Duration `json:"perception_latency_ms"`
	PlanningLatency    time.Duration `json:"planning_latency_ms"`
	ExecutionLatency   time.Duration `json:"execution_latency_ms"`
	MemoryRetrievalMs  float64       `json:"memory_retrieval_ms"`
	TotalToolsUsed     int           `json:"total_tools_used"`
	UniqueToolsUsed    int           `json:"unique_tools_used"`
	SuccessRate        float64       `json:"success_rate"`
}

// NewEnhancedBackgroundReview creates an enhanced background review manager
func NewEnhancedBackgroundReview(baseDir string) *EnhancedBackgroundReview {
	return &EnhancedBackgroundReview{
		baseDir:          filepath.Join(baseDir, "reviews"),
		reviewLog:        make([]ReviewEntry, 0),
		patternsFound:    make([]DetectedPattern, 0),
		stats:            &ReviewStats{},
		toolFrequency:    make(map[string]int),
		sessionHistory:   make([]SessionSnapshot, 0),
		errorPatterns:     make([]ErrorPattern, 0),
		latencyTracker:    make([]time.Duration, 0),
		config: &ReviewConfig{
			ReviewInterval:   15 * time.Minute,
			MinPatternFreq:   2,
			MaxPatterns:      100,
			AutoSaveEnabled:  true,
			SnapshotInterval: 5,
		},
	}
}

// Start initializes the background review system
func (br *EnhancedBackgroundReview) Start() error {
	if err := os.MkdirAll(br.baseDir, 0755); err != nil {
		return err
	}
	
	// Load existing patterns and stats
	br.loadPatterns()
	br.loadStats()
	
	// Start scheduled review
	br.startScheduledReview()
	
	return nil
}

// Stop gracefully stops the review system
func (br *EnhancedBackgroundReview) Stop() {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	if br.scheduledTask != nil {
		br.scheduledTask.Stop()
	}
	
	// Save current state
	br.savePatterns()
	br.saveStats()
}

// TriggerNudgeReview triggers a review on nudge (every N turns)
// This runs asynchronously and does NOT block
func (br *EnhancedBackgroundReview) TriggerNudgeReview(turnCount int, toolCalls []string, metrics *PerformanceMetrics) {
	br.mu.Lock()
	if br.isReviewing {
		br.mu.Unlock()
		return
	}
	br.isReviewing = true
	br.mu.Unlock()

	go func() {
		start := time.Now()
		
		// Update tool frequency tracking
		br.updateToolFrequency(toolCalls)
		
		// Run comprehensive review
		entry := br.performComprehensiveReview(turnCount, toolCalls, metrics)
		entry.Duration = time.Since(start)
		
		br.mu.Lock()
		br.reviewLog = append(br.reviewLog, *entry)
		br.stats.TotalReviews++
		br.stats.LastReviewTime = time.Now()
		
		// Update latency tracking
		br.latencyTracker = append(br.latencyTracker, entry.Duration)
		if len(br.latencyTracker) > 100 {
			br.latencyTracker = br.latencyTracker[1:]
		}
		
		// Calculate average duration
		var total time.Duration
		for _, d := range br.latencyTracker {
			total += d
		}
		br.stats.AvgReviewDuration = total / time.Duration(len(br.latencyTracker))
		br.isReviewing = false
		br.mu.Unlock()
		
		// Save review entry
		if br.config.AutoSaveEnabled {
			br.saveReview(*entry)
			br.saveStats()
		}
	}()
}

// TriggerTaskCompletionReview triggers a review when a task completes
func (br *EnhancedBackgroundReview) TriggerTaskCompletionReview(task string, toolSequence []string, success bool) {
	go func() {
		// Update pattern tracking
		br.updatePatternTracking(task, toolSequence, success)
		
		// Analyze for new patterns
		patterns := br.detectPatterns(toolSequence, task)
		
		// Analyze for error patterns
		if !success {
			br.trackErrorPattern(toolSequence, task)
		}
		
		// Update session history
		br.addSessionSnapshot(toolSequence, success)
		
		// If we found a high-confidence pattern, suggest a skill
		for _, pattern := range patterns {
			if pattern.Frequency >= br.config.MinPatternFreq && pattern.Score >= 0.7 {
				skillID := br.suggestSkill(task, pattern)
				if skillID != "" {
					br.mu.Lock()
					br.stats.TotalSkillsSuggested++
					br.mu.Unlock()
				}
			}
		}
	}()
}

// TriggerPeriodicReview triggers a scheduled periodic review
func (br *EnhancedBackgroundReview) TriggerPeriodicReview() {
	br.mu.Lock()
	sessionData := br.getAggregatedSessionData()
	br.mu.Unlock()
	
	br.performPeriodicAnalysis(sessionData)
}

// performComprehensiveReview performs a full review analysis
func (br *EnhancedBackgroundReview) performComprehensiveReview(turnCount int, toolCalls []string, metrics *PerformanceMetrics) *ReviewEntry {
	entry := &ReviewEntry{
		ID:                fmt.Sprintf("review-%d", time.Now().Unix()),
		Timestamp:         time.Now(),
		TurnCount:         turnCount,
		PerformanceMetrics: metrics,
	}
	
	var summary strings.Builder
	
	// Tool Usage Analysis
	summary.WriteString("## Tool Usage Analysis\n\n")
	freq := br.getToolFrequency()
	var sortedTools []string
	for tool := range freq {
		sortedTools = append(sortedTools, tool)
	}
	sort.Slice(sortedTools, func(i, j int) bool {
		return freq[sortedTools[i]] > freq[sortedTools[j]]
	})
	
	summary.WriteString("### Most Used Tools\n")
	for i, tool := range sortedTools {
		if i >= 10 {
			break
		}
		summary.WriteString(fmt.Sprintf("- **%s**: %d times (%.1f%%)\n", tool, freq[tool], float64(freq[tool])/float64(len(toolCalls))*100))
	}
	
	// Pattern Detection
	summary.WriteString("\n## Pattern Detection\n")
	patterns := br.detectPatterns(toolCalls, "")
	entry.Patterns = patterns
	
	if len(patterns) > 0 {
		summary.WriteString("\nDetected patterns:\n")
		for _, p := range patterns {
			summary.WriteString(fmt.Sprintf("- `%s`: seen %d times (confidence: %.0f%%)\n", 
				strings.Join(p.ToolSequence, " → "), p.Frequency, p.Score*100))
		}
	} else {
		summary.WriteString("\nNo new patterns detected in this session.\n")
	}
	
	// Error Analysis
	summary.WriteString("\n## Error Analysis\n")
	errorSummary := br.getErrorSummary()
	entry.ErrorSummary = errorSummary
	summary.WriteString(errorSummary)
	
	// Performance Metrics
	if metrics != nil {
		summary.WriteString("\n## Performance Metrics\n")
		summary.WriteString(fmt.Sprintf("- Perception latency: %s\n", metrics.PerceptionLatency))
		summary.WriteString(fmt.Sprintf("- Planning latency: %s\n", metrics.PlanningLatency))
		summary.WriteString(fmt.Sprintf("- Execution latency: %s\n", metrics.ExecutionLatency))
		summary.WriteString(fmt.Sprintf("- Memory retrieval: %.2fms\n", metrics.MemoryRetrievalMs))
		summary.WriteString(fmt.Sprintf("- Success rate: %.1f%%\n", metrics.SuccessRate*100))
	}
	
	// Action Items
	summary.WriteString("\n## Action Items\n")
	actionItems := br.generateActionItems(patterns, freq)
	for _, item := range actionItems {
		summary.WriteString(fmt.Sprintf("- [ ] %s\n", item))
	}
	
	entry.Summary = summary.String()
	return entry
}

// detectPatterns looks for recurring tool usage patterns with enhanced analysis
func (br *EnhancedBackgroundReview) detectPatterns(toolSequence []string, context string) []DetectedPattern {
	patterns := make([]DetectedPattern, 0)
	
	if len(toolSequence) < 3 {
		return patterns
	}
	
	// Look for common sequences of length 3-5
	for length := 3; length <= 5 && length <= len(toolSequence); length++ {
		for i := 0; i <= len(toolSequence)-length; i++ {
			seq := toolSequence[i : i+length]
			seqKey := strings.Join(seq, " → ")
			
			// Check if we've seen this pattern before
			found := false
			for idx := range br.patternsFound {
				existingKey := strings.Join(br.patternsFound[idx].ToolSequence, " → ")
				if existingKey == seqKey {
					br.patternsFound[idx].Frequency++
					br.patternsFound[idx].LastSeen = time.Now()
					if context != "" {
						br.patternsFound[idx].Contexts = append(br.patternsFound[idx].Contexts, context)
					}
					// Recalculate score
					br.patternsFound[idx].Score = br.calculatePatternScore(&br.patternsFound[idx])
					found = true
					break
				}
			}
			
			if !found {
				// New pattern detected
				newPattern := DetectedPattern{
					ID:            fmt.Sprintf("pattern-%d", len(br.patternsFound)+1),
					Name:          fmt.Sprintf("Pattern %d", len(br.patternsFound)+1),
					ToolSequence: seq,
					Frequency:     1,
					FirstSeen:     time.Now(),
					LastSeen:      time.Now(),
					SuccessRate:   0.5, // Default
					Score:         0.5,
				}
				if context != "" {
					newPattern.Contexts = []string{context}
				}
				newPattern.Score = br.calculatePatternScore(&newPattern)
				br.patternsFound = append(br.patternsFound, newPattern)
			}
		}
	}
	
	// Return high-confidence patterns
	for _, p := range br.patternsFound {
		if p.Score >= 0.6 {
			patterns = append(patterns, p)
		}
	}
	
	// Limit patterns list
	if len(patterns) > br.config.MaxPatterns {
		patterns = patterns[:br.config.MaxPatterns]
	}
	
	br.stats.TotalPatterns = len(br.patternsFound)
	
	return patterns
}

// calculatePatternScore computes importance score for a pattern
func (br *EnhancedBackgroundReview) calculatePatternScore(pattern *DetectedPattern) float64 {
	// Score based on frequency
	freqScore := float64(pattern.Frequency) / 10.0
	if freqScore > 1.0 {
		freqScore = 1.0
	}
	
	// Score based on recency
	daysSinceLastSeen := time.Since(pattern.LastSeen).Hours() / 24
	recencyScore := 1.0 - (daysSinceLastSeen / 30.0)
	if recencyScore < 0 {
		recencyScore = 0
	}
	
	// Score based on success rate
	successScore := pattern.SuccessRate
	
	// Score based on context diversity
	contextScore := float64(len(pattern.Contexts)) / 5.0
	if contextScore > 1.0 {
		contextScore = 1.0
	}
	
	// Weighted average
	return (freqScore*0.4 + recencyScore*0.2 + successScore*0.3 + contextScore*0.1)
}

// updatePatternTracking updates pattern metadata
func (br *EnhancedBackgroundReview) updatePatternTracking(task string, toolSequence []string, success bool) {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	br.detectPatterns(toolSequence, task)
	
	// Update success rate for patterns
	for i := range br.patternsFound {
		for j := 0; j <= len(toolSequence)-len(br.patternsFound[i].ToolSequence); j++ {
			match := true
			for k := range br.patternsFound[i].ToolSequence {
				if j+k >= len(toolSequence) || br.patternsFound[i].ToolSequence[k] != toolSequence[j+k] {
					match = false
					break
				}
			}
			if match {
				// Update success rate with exponential moving average
				currentRate := br.patternsFound[i].SuccessRate
				if success {
					br.patternsFound[i].SuccessRate = currentRate*0.7 + 0.3
				} else {
					br.patternsFound[i].SuccessRate = currentRate * 0.7
				}
				br.patternsFound[i].Score = br.calculatePatternScore(&br.patternsFound[i])
				break
			}
		}
	}
}

// trackErrorPattern records error patterns
func (br *EnhancedBackgroundReview) trackErrorPattern(toolSequence []string, task string) {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	// Extract error type from task description
	errorType := br.extractErrorType(task)
	
	seqKey := strings.Join(toolSequence, " → ")
	
	for i := range br.errorPatterns {
		if strings.Join(br.errorPatterns[i].ToolSequence, " → ") == seqKey {
			br.errorPatterns[i].Frequency++
			br.errorPatterns[i].LastSeen = time.Now()
			return
		}
	}
	
	// New error pattern
	suggestions := br.generateErrorSuggestions(errorType, toolSequence)
	br.errorPatterns = append(br.errorPatterns, ErrorPattern{
		ErrorType:    errorType,
		ToolSequence: toolSequence,
		Frequency:    1,
		LastSeen:     time.Now(),
		Suggestions:  suggestions,
	})
}

// extractErrorType attempts to categorize an error
func (br *EnhancedBackgroundReview) extractErrorType(task string) string {
	task = strings.ToLower(task)
	
	errorPatterns := map[string]string{
		"timeout":    "Timeout Error",
		"permission": "Permission Error",
		"not found":  "Resource Not Found",
		"invalid":   "Invalid Input",
		"network":   "Network Error",
		"memory":    "Memory Error",
		"parse":     "Parse Error",
		"exec":      "Execution Error",
	}
	
	for pattern, errorType := range errorPatterns {
		if strings.Contains(task, pattern) {
			return errorType
		}
	}
	
	return "Unknown Error"
}

// generateErrorSuggestions provides recovery suggestions for errors
func (br *EnhancedBackgroundReview) generateErrorSuggestions(errorType string, toolSequence []string) []string {
	suggestions := map[string][]string{
		"Timeout Error": {
			"Consider increasing timeout for this operation",
			"Break down the task into smaller steps",
			"Check if external service is responding slowly",
		},
		"Permission Error": {
			"Verify file/directory permissions",
			"Check if running with correct user context",
			"Ensure required access rights are granted",
		},
		"Resource Not Found": {
			"Verify the resource path is correct",
			"Check if the resource was moved or deleted",
			"Ensure dependencies are properly installed",
		},
		"Invalid Input": {
			"Review input format and encoding",
			"Validate input parameters before use",
			"Check for special characters that may cause issues",
		},
		"Network Error": {
			"Check network connectivity",
			"Verify firewall/proxy settings",
			"Retry with exponential backoff",
		},
		"Memory Error": {
			"Process large data in chunks",
			"Clear cached data periodically",
			"Consider streaming for large files",
		},
	}
	
	if s, ok := suggestions[errorType]; ok {
		return s
	}
	
	return []string{
		"Review error logs for more details",
		"Break down the operation into smaller steps",
		"Consult documentation for this operation",
	}
}

// addSessionSnapshot records session state
func (br *EnhancedBackgroundReview) addSessionSnapshot(toolSequence []string, success bool) {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	snapshot := SessionSnapshot{
		Timestamp: time.Now(),
		TurnCount: len(toolSequence),
		ToolCount: len(toolSequence),
		Errors:    0,
	}
	if !success {
		snapshot.Errors = 1
	}
	
	br.sessionHistory = append(br.sessionHistory, snapshot)
	
	// Keep only recent history
	if len(br.sessionHistory) > 1000 {
		br.sessionHistory = br.sessionHistory[len(br.sessionHistory)-1000:]
	}
}

// getAggregatedSessionData aggregates session history for analysis
func (br *EnhancedBackgroundReview) getAggregatedSessionData() map[string]interface{} {
	data := make(map[string]interface{})
	
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	// Calculate aggregates
	var totalTurns, totalTools, totalErrors int
	var totalDuration time.Duration
	
	for _, snap := range br.sessionHistory {
		totalTurns += snap.TurnCount
		totalTools += snap.ToolCount
		totalErrors += snap.Errors
		totalDuration += snap.Duration
	}
	
	data["total_sessions"] = len(br.sessionHistory)
	data["total_turns"] = totalTurns
	data["total_tools"] = totalTools
	data["total_errors"] = totalErrors
	data["avg_turns_per_session"] = float64(totalTurns) / float64(len(br.sessionHistory))
	data["error_rate"] = float64(totalErrors) / float64(len(br.sessionHistory))
	
	return data
}

// updateToolFrequency updates the tool usage frequency map
func (br *EnhancedBackgroundReview) updateToolFrequency(toolCalls []string) {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	for _, tool := range toolCalls {
		br.toolFrequency[tool]++
	}
}

// getToolFrequency returns the current tool frequency map
func (br *EnhancedBackgroundReview) getToolFrequency() map[string]int {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	result := make(map[string]int)
	for k, v := range br.toolFrequency {
		result[k] = v
	}
	return result
}

// getErrorSummary generates a summary of recent errors
func (br *EnhancedBackgroundReview) getErrorSummary() string {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	var summary strings.Builder
	
	if len(br.errorPatterns) == 0 {
		return "No errors detected in recent sessions.\n"
	}
	
	summary.WriteString("### Recent Error Patterns\n")
	
	// Sort by frequency
	sorted := make([]ErrorPattern, len(br.errorPatterns))
	copy(sorted, br.errorPatterns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Frequency > sorted[j].Frequency
	})
	
	for i, pattern := range sorted {
		if i >= 5 {
			break
		}
		summary.WriteString(fmt.Sprintf("- **%s** (%dx): `%s`\n", 
			pattern.ErrorType, pattern.Frequency, strings.Join(pattern.ToolSequence, " → ")))
		if len(pattern.Suggestions) > 0 {
			summary.WriteString(fmt.Sprintf("  - Suggestion: %s\n", pattern.Suggestions[0]))
		}
	}
	
	return summary.String()
}

// generateActionItems generates suggested actions based on review
func (br *EnhancedBackgroundReview) generateActionItems(patterns []DetectedPattern, freq map[string]int) []string {
	items := make([]string, 0)
	
	// Suggest skill creation for high-frequency patterns
	if len(patterns) >= 2 {
		items = append(items, fmt.Sprintf("Consider creating a skill for pattern: %s", 
			strings.Join(patterns[0].ToolSequence, " → ")))
	}
	
	// Suggest MEMORY.md update
	items = append(items, "Update MEMORY.md with key insights from this session")
	
	// Suggest parameter optimization
	for tool, count := range freq {
		if count > 20 {
			items = append(items, fmt.Sprintf("Optimize %s usage - called %d times", tool, count))
		}
	}
	
	// Error-based suggestions
	if len(br.errorPatterns) > 0 {
		items = append(items, "Review error patterns and implement fixes")
	}
	
	return items
}

// suggestSkill suggests creating a skill based on detected patterns
func (br *EnhancedBackgroundReview) suggestSkill(task string, pattern DetectedPattern) string {
	// Generate skill suggestion
	skillID := fmt.Sprintf("auto-skill-%s-%d", pattern.ID, time.Now().Unix())
	
	// In production, this would call the skill auto-creator
	fmt.Printf("[BackgroundReview] Suggested skill creation:\n")
	fmt.Printf("  Task: %s\n", task)
	fmt.Printf("  Pattern: %s\n", strings.Join(pattern.ToolSequence, " → "))
	fmt.Printf("  Frequency: %d\n", pattern.Frequency)
	fmt.Printf("  Confidence: %.0f%%\n", pattern.Score*100)
	
	return skillID
}

// startScheduledReview starts periodic background reviews
func (br *EnhancedBackgroundReview) startScheduledReview() {
	br.mu.Lock()
	if br.scheduledTask != nil {
		br.scheduledTask.Stop()
	}
	br.scheduledTask = time.NewTimer(br.config.ReviewInterval)
	br.mu.Unlock()
	
	go func() {
		for range br.scheduledTask.C {
			br.TriggerPeriodicReview()
			
			// Reset timer
			br.mu.Lock()
			br.scheduledTask = time.NewTimer(br.config.ReviewInterval)
			br.mu.Unlock()
		}
	}()
}

// performPeriodicAnalysis performs scheduled analysis
func (br *EnhancedBackgroundReview) performPeriodicAnalysis(sessionData map[string]interface{}) {
	// Analyze trends over time
	fmt.Printf("[BackgroundReview] Performing periodic analysis...\n")
	fmt.Printf("  Total sessions analyzed: %v\n", sessionData["total_sessions"])
	fmt.Printf("  Average turns per session: %.1f\n", sessionData["avg_turns_per_session"])
	fmt.Printf("  Error rate: %.2f%%\n", sessionData["error_rate"].(float64)*100)
	
	// Save patterns periodically
	br.savePatterns()
	br.saveStats()
}

// saveReview writes the review entry to disk
func (br *EnhancedBackgroundReview) saveReview(entry ReviewEntry) {
	filename := filepath.Join(br.baseDir, fmt.Sprintf("review-%s.json", 
		entry.Timestamp.Format("2006-01-02-15-04-05")))
	
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}
	
	os.WriteFile(filename, data, 0644)
}

// savePatterns persists detected patterns
func (br *EnhancedBackgroundReview) savePatterns() {
	filename := filepath.Join(br.baseDir, "patterns.json")
	
	data, err := json.MarshalIndent(br.patternsFound, "", "  ")
	if err != nil {
		return
	}
	
	os.WriteFile(filename, data, 0644)
}

// loadPatterns loads saved patterns
func (br *EnhancedBackgroundReview) loadPatterns() {
	filename := filepath.Join(br.baseDir, "patterns.json")
	
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	
	json.Unmarshal(data, &br.patternsFound)
}

// saveStats persists review statistics
func (br *EnhancedBackgroundReview) saveStats() {
	filename := filepath.Join(br.baseDir, "stats.json")
	
	data, err := json.MarshalIndent(br.stats, "", "  ")
	if err != nil {
		return
	}
	
	os.WriteFile(filename, data, 0644)
}

// loadStats loads saved statistics
func (br *EnhancedBackgroundReview) loadStats() {
	filename := filepath.Join(br.baseDir, "stats.json")
	
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	
	json.Unmarshal(data, &br.stats)
}

// GetRecentReviews returns recent review entries
func (br *EnhancedBackgroundReview) GetRecentReviews(limit int) []ReviewEntry {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	if len(br.reviewLog) <= limit {
		return br.reviewLog
	}
	return br.reviewLog[len(br.reviewLog)-limit:]
}

// GetStats returns current review statistics
func (br *EnhancedBackgroundReview) GetStats() *ReviewStats {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	// Update top patterns
	stats := *br.stats
	sorted := make([]DetectedPattern, len(br.patternsFound))
	copy(sorted, br.patternsFound)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	
	for i := range sorted {
		if i >= 10 {
			break
		}
		stats.TopPatterns = append(stats.TopPatterns, PatternRanking{
			Pattern:   strings.Join(sorted[i].ToolSequence, " → "),
			Frequency: sorted[i].Frequency,
		})
	}
	
	return &stats
}

// GetTopPatterns returns the most significant patterns
func (br *EnhancedBackgroundReview) GetTopPatterns(limit int) []DetectedPattern {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	sorted := make([]DetectedPattern, len(br.patternsFound))
	copy(sorted, br.patternsFound)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	
	if len(sorted) <= limit {
		return sorted
	}
	return sorted[:limit]
}

// GetErrorPatterns returns detected error patterns
func (br *EnhancedBackgroundReview) GetErrorPatterns() []ErrorPattern {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	return br.errorPatterns
}

// SetConfig updates review configuration
func (br *EnhancedBackgroundReview) SetConfig(config *ReviewConfig) {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	br.config = config
	
	// Restart scheduled review with new interval
	if br.scheduledTask != nil {
		br.scheduledTask.Stop()
		br.scheduledTask = time.NewTimer(config.ReviewInterval)
	}
}

// ExportReviewReport generates a comprehensive review report
func (br *EnhancedBackgroundReview) ExportReviewReport(format string) (string, error) {
	br.mu.RLock()
	defer br.mu.RUnlock()
	
	switch format {
	case "json":
		data, err := json.MarshalIndent(map[string]interface{}{
			"stats":    br.stats,
			"patterns": br.patternsFound,
			"errors":   br.errorPatterns,
			"reviews":  br.reviewLog,
		}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil
		
	case "markdown":
		return br.generateMarkdownReport()
		
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

// generateMarkdownReport creates a markdown formatted report
func (br *EnhancedBackgroundReview) generateMarkdownReport() (string, error) {
	var sb strings.Builder
	
	sb.WriteString("# Cortex Agent Review Report\n\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))
	
	// Summary Statistics
	sb.WriteString("## Summary Statistics\n\n")
	sb.WriteString(fmt.Sprintf("- Total Reviews: %d\n", br.stats.TotalReviews))
	sb.WriteString(fmt.Sprintf("- Total Patterns: %d\n", br.stats.TotalPatterns))
	sb.WriteString(fmt.Sprintf("- Skills Suggested: %d\n", br.stats.TotalSkillsSuggested))
	sb.WriteString(fmt.Sprintf("- Avg Review Duration: %s\n", br.stats.AvgReviewDuration))
	sb.WriteString(fmt.Sprintf("- Last Review: %s\n\n", br.stats.LastReviewTime.Format(time.RFC3339)))
	
	// Top Patterns
	sb.WriteString("## Top Patterns\n\n")
	for _, p := range br.stats.TopPatterns {
		sb.WriteString(fmt.Sprintf("- `%s` (%d occurrences)\n", p.Pattern, p.Frequency))
	}
	sb.WriteString("\n")
	
	// Error Patterns
	sb.WriteString("## Error Patterns\n\n")
	for _, e := range br.errorPatterns {
		sb.WriteString(fmt.Sprintf("### %s (%dx)\n", e.ErrorType, e.Frequency))
		sb.WriteString(fmt.Sprintf("Tool sequence: `%s`\n", strings.Join(e.ToolSequence, " → ")))
		for _, s := range e.Suggestions {
			sb.WriteString(fmt.Sprintf("- %s\n", s))
		}
		sb.WriteString("\n")
	}
	
	return sb.String(), nil
}

// AddCustomMetric allows adding custom metrics to review
func (br *EnhancedBackgroundReview) AddCustomMetric(key string, value interface{}) {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	// This would be used to track custom KPIs
	fmt.Printf("[BackgroundReview] Custom metric: %s = %v\n", key, value)
}

// Reset clears all review data
func (br *EnhancedBackgroundReview) Reset() {
	br.mu.Lock()
	defer br.mu.Unlock()
	
	br.reviewLog = make([]ReviewEntry, 0)
	br.patternsFound = make([]DetectedPattern, 0)
	br.toolFrequency = make(map[string]int)
	br.sessionHistory = make([]SessionSnapshot, 0)
	br.errorPatterns = make([]ErrorPattern, 0)
	br.stats = &ReviewStats{}
}

// IsReviewing returns whether a review is currently in progress
func (br *EnhancedBackgroundReview) IsReviewing() bool {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.isReviewing
}
