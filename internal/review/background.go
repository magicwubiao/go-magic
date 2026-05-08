package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackgroundReview handles the third system from Cortex Agent:
// "Background Review - an independent daemon that runs asynchronously,
// specifically designed to summarize experiences without interrupting the user conversation."
type BackgroundReview struct {
	mu            sync.Mutex
	baseDir       string
	reviewLog     []ReviewEntry
	patternsFound []DetectedPattern
	isReviewing   bool
}

// NewBackgroundReview creates a new background review manager
func NewBackgroundReview(baseDir string) *BackgroundReview {
	return &BackgroundReview{
		baseDir:       filepath.Join(baseDir, "reviews"),
		reviewLog:     make([]ReviewEntry, 0),
		patternsFound: make([]DetectedPattern, 0),
	}
}

// Start initializes the background review system
func (br *BackgroundReview) Start() error {
	return os.MkdirAll(br.baseDir, 0755)
}

// TriggerNudgeReview triggers a review on nudge (every N turns)
// This runs asynchronously and does NOT block
func (br *BackgroundReview) TriggerNudgeReview(turnCount int, toolCalls []string) {
	br.mu.Lock()
	if br.isReviewing {
		br.mu.Unlock()
		return
	}
	br.isReviewing = true
	br.mu.Unlock()

	go func() {
		start := time.Now()

		// Run the review
		summary := br.performReview(toolCalls)

		// Save the result
		entry := ReviewEntry{
			Timestamp: time.Now(),
			TurnCount: turnCount,
			Summary:   summary,
			Duration:  time.Since(start),
		}

		br.mu.Lock()
		br.reviewLog = append(br.reviewLog, entry)
		br.isReviewing = false
		br.mu.Unlock()

		// Write to disk
		br.saveReview(entry)
	}()
}

// TriggerTaskCompletionReview triggers a review when a task completes
func (br *BackgroundReview) TriggerTaskCompletionReview(task string, toolSequence []string) {
	go func() {
		// Analyze for pattern detection using enhanced method
		patterns := br.detectPatterns(toolSequence, "")

		// If we found a recurring pattern, suggest a skill
		if len(patterns) > 0 {
			br.suggestSkill(task, patterns)
		}
	}()
}

// saveReview writes the review entry to disk
func (br *BackgroundReview) saveReview(entry ReviewEntry) {
	filename := filepath.Join(br.baseDir, fmt.Sprintf("review-%s.md", time.Now().Format("2006-01-02-15-04-05")))

	content := fmt.Sprintf(`# Background Review

Timestamp: %s
Turn Count: %d
Duration: %s

## Summary

%s

## Patterns Found

%v
`,
		entry.Timestamp.Format(time.RFC3339),
		entry.TurnCount,
		entry.Duration,
		entry.Summary,
		entry.Patterns,
	)

	os.WriteFile(filename, []byte(content), 0644)
}

// GetRecentReviews returns recent review entries
func (br *BackgroundReview) GetRecentReviews(limit int) []ReviewEntry {
	br.mu.Lock()
	defer br.mu.Unlock()

	if len(br.reviewLog) <= limit {
		return br.reviewLog
	}
	return br.reviewLog[len(br.reviewLog)-limit:]
}

// performReview performs a review of the tool calls
func (br *BackgroundReview) performReview(toolCalls []string) string {
	if len(toolCalls) == 0 {
		return "No tool calls to review"
	}

	// Count tool usage
	toolCounts := make(map[string]int)
	for _, tc := range toolCalls {
		toolCounts[tc]++
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Reviewed %d tool calls across %d tools:\n", len(toolCalls), len(toolCounts)))
	for tool, count := range toolCounts {
		summary.WriteString(fmt.Sprintf("- %s: %d calls\n", tool, count))
		if count >= 3 {
			summary.WriteString("  ⚠️ High usage - may indicate a loop or inefficient approach\n")
		}
	}

	return summary.String()
}

// detectPatterns detects patterns in tool sequences
func (br *BackgroundReview) detectPatterns(toolSequence []string, _ string) []DetectedPattern {
	if len(toolSequence) < 4 {
		return nil
	}

	var patterns []DetectedPattern

	// Detect repeating subsequences of length 2 (e.g., A→B→A→B)
	pairCounts := make(map[string]int)
	for i := 0; i < len(toolSequence)-1; i++ {
		pair := toolSequence[i] + "->" + toolSequence[i+1]
		pairCounts[pair]++
	}

	for pair, count := range pairCounts {
		if count >= 3 {
			parts := strings.Split(pair, "->")
			patterns = append(patterns, DetectedPattern{
				Name:         "repeating_pair",
				ToolSequence: parts,
				Frequency:    count,
			})
		}
	}

	return patterns
}

// suggestSkill suggests a skill based on detected patterns
// Currently logs the suggestion; full implementation would create/export skill definitions
func (br *BackgroundReview) suggestSkill(task string, patterns []DetectedPattern) {
	br.mu.Lock()
	defer br.mu.Unlock()

	if len(patterns) == 0 {
		return
	}

	// Build suggestion summary
	var toolSequence []string
	for _, p := range patterns {
		if len(p.ToolSequence) > len(toolSequence) {
			toolSequence = p.ToolSequence
		}
	}

	// Log the suggestion for manual review
	suggestion := map[string]interface{}{
		"timestamp":      time.Now().Format(time.RFC3339),
		"task":           task,
		"pattern_count":  len(patterns),
		"suggested_tool": toolSequence,
	}

	// Store suggestion in patterns found
	br.patternsFound = append(br.patternsFound, patterns...)

	// In a full implementation, this would:
	// 1. Generate a skill definition based on patterns
	// 2. Create a SKILL.md file
	// 3. Register the skill with the skills manager
	// For now, we log it for manual review
	_ = suggestion // suggestion stored in patternsFound for later retrieval
}
