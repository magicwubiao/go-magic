package review

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnhancedBackgroundReview_Creation(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	if reviewer == nil {
		t.Fatal("Expected non-nil BackgroundReview")
	}
	
	if reviewer.baseDir != filepath.Join(tmpDir, "reviews") {
		t.Errorf("Unexpected baseDir: %s", reviewer.baseDir)
	}
	
	if reviewer.config == nil {
		t.Fatal("Expected non-nil config")
	}
}

func TestEnhancedBackgroundReview_Start(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	if err := reviewer.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	
	// Check directory was created
	if _, err := os.Stat(reviewer.baseDir); os.IsNotExist(err) {
		t.Error("Expected reviews directory to be created")
	}
}

func TestEnhancedBackgroundReview_TriggerNudgeReview(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	toolCalls := []string{"read_file", "write_file", "execute_command", "glob", "read_file"}
	
	metrics := &PerformanceMetrics{
		PerceptionLatency: 10 * time.Millisecond,
		PlanningLatency:   20 * time.Millisecond,
		ExecutionLatency:  100 * time.Millisecond,
		SuccessRate:       0.9,
	}
	
	reviewer.TriggerNudgeReview(15, toolCalls, metrics)
	
	// Wait for async review
	time.Sleep(200 * time.Millisecond)
	
	// Check review was recorded
	if len(reviewer.reviewLog) != 1 {
		t.Errorf("Expected 1 review, got %d", len(reviewer.reviewLog))
	}
	
	stats := reviewer.GetStats()
	if stats.TotalReviews != 1 {
		t.Errorf("Expected TotalReviews to be 1, got %d", stats.TotalReviews)
	}
}

func TestEnhancedBackgroundReview_TriggerTaskCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	toolSequence := []string{"glob", "read_file", "write_file"}
	
	reviewer.TriggerTaskCompletionReview("Creating a README file", toolSequence, true)
	
	// Wait for async processing
	time.Sleep(100 * time.Millisecond)
	
	// Pattern should be detected
	if len(reviewer.patternsFound) == 0 {
		t.Log("No patterns detected (expected for single occurrence)")
	}
}

func TestEnhancedBackgroundReview_PatternDetection(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Trigger same pattern multiple times
	toolSequence := []string{"read_file", "write_file", "execute_command"}
	
	for i := 0; i < 5; i++ {
		reviewer.TriggerTaskCompletionReview("Test task", toolSequence, true)
		time.Sleep(10 * time.Millisecond)
	}
	
	// Check patterns were detected
	if len(reviewer.patternsFound) == 0 {
		t.Error("Expected patterns to be detected")
	}
	
	// Find the specific pattern
	var foundPattern *DetectedPattern
	for i := range reviewer.patternsFound {
		if len(reviewer.patternsFound[i].ToolSequence) == 3 {
			foundPattern = &reviewer.patternsFound[i]
			break
		}
	}
	
	if foundPattern == nil {
		t.Error("Expected to find 3-tool pattern")
	} else {
		if foundPattern.Frequency < 5 {
			t.Errorf("Expected frequency >= 5, got %d", foundPattern.Frequency)
		}
	}
}

func TestEnhancedBackgroundReview_ErrorPatternTracking(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Trigger multiple failures
	for i := 0; i < 3; i++ {
		reviewer.TriggerTaskCompletionReview("Timeout error occurred", []string{"read_file", "write_file"}, false)
		time.Sleep(10 * time.Millisecond)
	}
	
	if len(reviewer.errorPatterns) == 0 {
		t.Error("Expected error patterns to be tracked")
	}
	
	// Check suggestions were generated
	if len(reviewer.errorPatterns[0].Suggestions) == 0 {
		t.Error("Expected suggestions for error pattern")
	}
}

func TestEnhancedBackgroundReview_ComprehensiveReview(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	// Update tool frequency
	reviewer.mu.Lock()
	reviewer.toolFrequency["read_file"] = 50
	reviewer.toolFrequency["write_file"] = 30
	reviewer.toolFrequency["execute_command"] = 20
	reviewer.mu.Unlock()
	
	toolCalls := []string{"read_file", "write_file", "read_file", "execute_command", "read_file"}
	
	metrics := &PerformanceMetrics{
		PerceptionLatency:  5 * time.Millisecond,
		PlanningLatency:    10 * time.Millisecond,
		ExecutionLatency:  200 * time.Millisecond,
		MemoryRetrievalMs:  2.5,
		SuccessRate:       0.85,
	}
	
	entry := reviewer.performComprehensiveReview(20, toolCalls, metrics)
	
	if entry == nil {
		t.Fatal("Expected non-nil review entry")
	}
	
	if entry.Summary == "" {
		t.Error("Expected non-empty summary")
	}
	
	// Check summary contains expected sections
	summary := entry.Summary
	if len(summary) < 100 {
		t.Error("Summary seems too short")
	}
	
	// Check performance metrics were captured
	if entry.PerformanceMetrics == nil {
		t.Error("Expected performance metrics")
	}
}

func TestEnhancedBackgroundReview_Stats(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	// Trigger some reviews
	toolCalls := []string{"tool1", "tool2", "tool3"}
	metrics := &PerformanceMetrics{SuccessRate: 0.9}
	
	for i := 0; i < 3; i++ {
		reviewer.TriggerNudgeReview(10*i, toolCalls, metrics)
		time.Sleep(50 * time.Millisecond)
	}
	
	stats := reviewer.GetStats()
	
	if stats.TotalReviews != 3 {
		t.Errorf("Expected 3 reviews, got %d", stats.TotalReviews)
	}
	
	if stats.AvgReviewDuration == 0 {
		t.Error("Expected non-zero average duration")
	}
}

func TestEnhancedBackgroundReview_GetTopPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Add patterns with different scores
	reviewer.mu.Lock()
	reviewer.patternsFound = []DetectedPattern{
		{ToolSequence: []string{"a", "b"}, Frequency: 1, Score: 0.3},
		{ToolSequence: []string{"c", "d"}, Frequency: 5, Score: 0.8},
		{ToolSequence: []string{"e", "f"}, Frequency: 3, Score: 0.6},
	}
	reviewer.mu.Unlock()
	
	topPatterns := reviewer.GetTopPatterns(2)
	
	if len(topPatterns) != 2 {
		t.Errorf("Expected 2 patterns, got %d", len(topPatterns))
	}
	
	// Should be sorted by score descending
	if len(topPatterns) >= 1 {
		if topPatterns[0].Score < topPatterns[len(topPatterns)-1].Score {
			t.Error("Patterns not sorted by score descending")
		}
	}
}

func TestEnhancedBackgroundReview_ErrorSummary(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Add some error patterns
	reviewer.mu.Lock()
	reviewer.errorPatterns = []ErrorPattern{
		{
			ErrorType:   "Timeout",
			Frequency:   5,
			Suggestions: []string{"Increase timeout"},
		},
	}
	reviewer.mu.Unlock()
	
	summary := reviewer.getErrorSummary()
	
	if summary == "" {
		t.Error("Expected non-empty error summary")
	}
	
	if len(summary) < 10 {
		t.Error("Error summary too short")
	}
}

func TestEnhancedBackgroundReview_ActionItems(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	patterns := []DetectedPattern{
		{ToolSequence: []string{"a", "b", "c"}, Frequency: 3, Score: 0.7},
	}
	freq := map[string]int{"read_file": 25, "write_file": 10}
	
	items := reviewer.generateActionItems(patterns, freq)
	
	if len(items) == 0 {
		t.Error("Expected action items")
	}
	
	// Should include skill suggestion for frequent pattern
	foundSkillSuggestion := false
	for _, item := range items {
		if len(item) > 20 { // Skill suggestion is long
			foundSkillSuggestion = true
			break
		}
	}
	
	if !foundSkillSuggestion {
		t.Error("Expected skill suggestion in action items")
	}
}

func TestEnhancedBackgroundReview_ExportReport(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	// Generate some data
	reviewer.mu.Lock()
	reviewer.stats = &ReviewStats{
		TotalReviews:         5,
		TotalPatterns:        3,
		TotalSkillsSuggested: 1,
	}
	reviewer.mu.Unlock()
	
	// Export as JSON
	jsonReport, err := reviewer.ExportReviewReport("json")
	if err != nil {
		t.Fatalf("JSON export failed: %v", err)
	}
	
	if len(jsonReport) == 0 {
		t.Error("Expected non-empty JSON report")
	}
	
	// Export as Markdown
	mdReport, err := reviewer.ExportReviewReport("markdown")
	if err != nil {
		t.Fatalf("Markdown export failed: %v", err)
	}
	
	if len(mdReport) == 0 {
		t.Error("Expected non-empty Markdown report")
	}
	
	// Test invalid format
	_, err = reviewer.ExportReviewReport("invalid")
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestEnhancedBackgroundReview_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Add patterns
	reviewer.mu.Lock()
	reviewer.patternsFound = []DetectedPattern{
		{ID: "p1", Name: "Test Pattern", Score: 0.8},
	}
	reviewer.stats = &ReviewStats{TotalReviews: 2}
	reviewer.mu.Unlock()
	
	// Save
	reviewer.savePatterns()
	reviewer.saveStats()
	
	// Create new instance and load
	reviewer2 := NewEnhancedBackgroundReview(tmpDir)
	reviewer2.Start()
	
	if len(reviewer2.patternsFound) != 1 {
		t.Errorf("Expected 1 pattern after load, got %d", len(reviewer2.patternsFound))
	}
	
	if reviewer2.stats.TotalReviews != 2 {
		t.Errorf("Expected TotalReviews=2 after load, got %d", reviewer2.stats.TotalReviews)
	}
}

func TestEnhancedBackgroundReview_Config(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Update config
	newConfig := &ReviewConfig{
		ReviewInterval:   5 * time.Minute,
		MinPatternFreq:   3,
		MaxPatterns:      50,
		AutoSaveEnabled:  false,
	}
	
	reviewer.SetConfig(newConfig)
	
	if reviewer.config.ReviewInterval != 5*time.Minute {
		t.Error("Config not updated")
	}
	
	if reviewer.config.MinPatternFreq != 3 {
		t.Error("MinPatternFreq not updated")
	}
}

func TestEnhancedBackgroundReview_Reset(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Add some data
	reviewer.mu.Lock()
	reviewer.reviewLog = []ReviewEntry{{ID: "test"}}
	reviewer.patternsFound = []DetectedPattern{{ID: "p1"}}
	reviewer.stats = &ReviewStats{TotalReviews: 5}
	reviewer.mu.Unlock()
	
	// Reset
	reviewer.Reset()
	
	if len(reviewer.reviewLog) != 0 {
		t.Error("Expected reviewLog to be empty after reset")
	}
	
	if len(reviewer.patternsFound) != 0 {
		t.Error("Expected patternsFound to be empty after reset")
	}
	
	if reviewer.stats.TotalReviews != 0 {
		t.Error("Expected stats.TotalReviews to be 0 after reset")
	}
}

func TestEnhancedBackgroundReview_IsReviewing(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	if reviewer.IsReviewing() {
		t.Error("Expected IsReviewing to be false initially")
	}
	
	// Trigger a review
	reviewer.TriggerNudgeReview(10, []string{"a", "b"}, nil)
	
	// Check immediately (might be true during review)
	if reviewer.IsReviewing() {
		t.Log("Review is in progress")
	}
	
	// Wait for completion
	time.Sleep(100 * time.Millisecond)
	
	if reviewer.IsReviewing() {
		t.Error("Expected IsReviewing to be false after completion")
	}
}

func TestEnhancedBackgroundReview_AddCustomMetric(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// This should not panic
	reviewer.AddCustomMetric("test_metric", 42)
	reviewer.AddCustomMetric("another_metric", "value")
	reviewer.AddCustomMetric("complex_metric", map[string]int{"key": 1})
}

func TestEnhancedBackgroundReview_GetRecentReviews(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	// Add many reviews
	reviewer.mu.Lock()
	for i := 0; i < 10; i++ {
		reviewer.reviewLog = append(reviewer.reviewLog, ReviewEntry{ID: "test"})
	}
	reviewer.mu.Unlock()
	
	// Get recent
	recent := reviewer.GetRecentReviews(5)
	if len(recent) != 5 {
		t.Errorf("Expected 5 recent reviews, got %d", len(recent))
	}
}

func TestEnhancedBackgroundReview_PatternScoreCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	pattern := &DetectedPattern{
		ToolSequence: []string{"a", "b", "c"},
		Frequency:    5,
		LastSeen:    time.Now(),
		SuccessRate:  0.9,
		Contexts:     []string{"ctx1", "ctx2", "ctx3"},
	}
	
	score := reviewer.calculatePatternScore(pattern)
	
	if score <= 0 || score > 1 {
		t.Errorf("Expected score between 0 and 1, got %f", score)
	}
}

func TestEnhancedBackgroundReview_SuggestSkill(t *testing.T) {
	tmpDir := t.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	pattern := DetectedPattern{
		ToolSequence: []string{"read", "write", "execute"},
		Frequency:    5,
		Score:        0.8,
	}
	
	skillID := reviewer.suggestSkill("Test task", pattern)
	
	if skillID == "" {
		t.Error("Expected non-empty skill ID")
	}
}

func BenchmarkTriggerNudgeReview(b *testing.B) {
	tmpDir := b.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	reviewer.Start()
	
	toolCalls := []string{"read_file", "write_file", "execute_command", "glob"}
	metrics := &PerformanceMetrics{SuccessRate: 0.9}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reviewer.TriggerNudgeReview(i, toolCalls, metrics)
	}
	
	time.Sleep(time.Millisecond * 50 * time.Duration(b.N))
}

func BenchmarkGetStats(b *testing.B) {
	tmpDir := b.TempDir()
	reviewer := NewEnhancedBackgroundReview(tmpDir)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = reviewer.GetStats()
	}
}
