package review

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BackgroundReview handles the third system from Cortex Agent:
// "Background Review - an independent daemon that runs asynchronously,
// specifically designed to summarize experiences without interrupting the user conversation."
type BackgroundReview struct {
	mu             sync.Mutex
	baseDir        string
	reviewLog      []ReviewEntry
	patternsFound  []DetectedPattern
	isReviewing    bool
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
	return fmt.Sprintf("Reviewed %d tool calls", len(toolCalls))
}

// detectPatterns detects patterns in tool sequences
func (br *BackgroundReview) detectPatterns(toolSequence []string, _ string) []DetectedPattern {
	return nil
}

// suggestSkill suggests a skill based on detected patterns
func (br *BackgroundReview) suggestSkill(task string, patterns []DetectedPattern) {
	// TODO: implement skill suggestion
}
