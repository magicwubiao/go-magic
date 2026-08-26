package cortex

import (
	"math"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// EffectivenessEvaluator - Evaluates trajectory effectiveness
// ============================================================================
// Evaluates execution trajectories based on multiple dimensions:
// - Success: Did the task complete successfully?
// - Efficiency: How many turns relative to optimal?
// - Quality: Output quality metrics
// - Tool Accuracy: Correct tool selection and usage
// ============================================================================

// EffectivenessEvaluator evaluates trajectory effectiveness
type EffectivenessEvaluator struct {
	// Weights for different scoring dimensions
	weights ScoringWeights
}

// ScoringWeights defines the weight of each scoring dimension
type ScoringWeights struct {
	SuccessWeight      float64 // Weight of success/failure
	EfficiencyWeight   float64 // Weight of turn efficiency
	QualityWeight      float64 // Weight of output quality
	ToolAccuracyWeight float64 // Weight of correct tool usage
}

// DefaultScoringWeights returns default scoring weights
func DefaultScoringWeights() ScoringWeights {
	return ScoringWeights{
		SuccessWeight:      0.4, // 40% - Most important
		EfficiencyWeight:   0.2, // 20%
		QualityWeight:      0.2, // 20%
		ToolAccuracyWeight: 0.2, // 20%
	}
}

// NewEffectivenessEvaluator creates a new evaluator
func NewEffectivenessEvaluator() *EffectivenessEvaluator {
	return &EffectivenessEvaluator{
		weights: DefaultScoringWeights(),
	}
}

// Evaluate evaluates a single trajectory
func (e *EffectivenessEvaluator) Evaluate(trajectory *Trajectory) EffectivenessScore {
	score := EffectivenessScore{
		TrajectoryID: trajectory.ID,
		Success:      trajectory.Success,
	}

	// Calculate individual dimensions
	successScore := e.evaluateSuccess(trajectory)
	efficiencyScore := e.evaluateEfficiency(trajectory)
	qualityScore := e.evaluateQuality(trajectory)
	toolAccuracyScore := e.evaluateToolAccuracy(trajectory)

	// Store individual scores
	score.Success = successScore > 0.5
	score.Efficiency = efficiencyScore
	score.Quality = qualityScore
	score.ToolAccuracy = toolAccuracyScore

	// Calculate weighted overall score
	score.OverallScore = e.calculateOverallScore(
		successScore,
		efficiencyScore,
		qualityScore,
		toolAccuracyScore,
	)

	return score
}

// EvaluateBatch evaluates multiple trajectories
func (e *EffectivenessEvaluator) EvaluateBatch(trajectories []Trajectory) []EffectivenessScore {
	scores := make([]EffectivenessScore, 0, len(trajectories))
	for i := range trajectories {
		scores = append(scores, e.Evaluate(&trajectories[i]))
	}
	return scores
}

// evaluateSuccess evaluates task success
func (e *EffectivenessEvaluator) evaluateSuccess(trajectory *Trajectory) float64 {
	if trajectory.Success {
		return 1.0
	}
	// Partial credit if some steps succeeded
	successfulSteps := 0
	for _, step := range trajectory.Steps {
		if step.Success {
			successfulSteps++
		}
	}
	if len(trajectory.Steps) > 0 {
		return float64(successfulSteps) / float64(len(trajectory.Steps)) * 0.5
	}
	return 0.0
}

// evaluateEfficiency evaluates turn efficiency
func (e *EffectivenessEvaluator) evaluateEfficiency(trajectory *Trajectory) float64 {
	actualTurns := len(trajectory.Steps)
	if actualTurns == 0 {
		return 0.0
	}

	// Estimate optimal turns based on task complexity
	optimalTurns := e.estimateOptimalTurns(trajectory.Task)

	// Calculate efficiency ratio
	ratio := float64(optimalTurns) / float64(actualTurns)
	if ratio > 1.0 {
		ratio = 1.0 // Cap at 1.0 for being better than optimal
	}

	// Penalize for being too slow
	if actualTurns > optimalTurns*3 {
		ratio *= 0.5 // Heavy penalty for very inefficient
	}

	return ratio
}

// estimateOptimalTurns estimates the optimal number of turns for a task
func (e *EffectivenessEvaluator) estimateOptimalTurns(task string) int {
	// Simple heuristic based on task complexity indicators
	task = strings.ToLower(task)

	// Count complexity indicators（中英文词表，避免只识别英文连接词）
	complexity := 1
	indicators := []string{
		"and", "then", "also", "additionally",
		"analyze", "compare", "evaluate",
		"multiple", "several", "various",
		// 中文复杂度词表（连接词与顺序词，避免中文任务恒得 2 轮最优）
		"然后", "接着", "并且", "之后", "以及",
		"此外", "另外", "同时",
		"首先", "其次", "最后",
		"分析", "比较", "评估",
		"多个", "若干", "各种",
	}

	for _, indicator := range indicators {
		if strings.Contains(task, indicator) {
			complexity++
		}
	}

	// Base optimal turns on complexity
	return 2 + complexity
}

// evaluateQuality evaluates output quality
func (e *EffectivenessEvaluator) evaluateQuality(trajectory *Trajectory) float64 {
	if trajectory.Result == "" {
		return 0.0
	}

	quality := 0.5 // Base quality

	result := trajectory.Result

	// Check for quality indicators
	indicators := map[string]float64{
		"success":   0.1,
		"completed": 0.1,
		"error":     -0.2,
		"failed":    -0.2,
		"warning":   -0.1,
		"成功":        0.1,
		"完成":        0.1,
		"错误":        -0.2,
		"失败":        -0.2,
		"警告":        -0.1,
	}

	lowerResult := strings.ToLower(result)
	for indicator, delta := range indicators {
		if strings.Contains(lowerResult, indicator) {
			quality += delta
		}
	}

	// Length factor (reasonable length is good)
	length := len(result)
	if length > 100 && length < 5000 {
		quality += 0.1
	} else if length > 10000 {
		quality -= 0.1 // Too verbose
	}

	// Cap between 0 and 1
	return math.Max(0.0, math.Min(1.0, quality))
}

// evaluateToolAccuracy evaluates correct tool usage
func (e *EffectivenessEvaluator) evaluateToolAccuracy(trajectory *Trajectory) float64 {
	if len(trajectory.Steps) == 0 {
		return 0.0
	}

	correctUsage := 0
	for _, step := range trajectory.Steps {
		if step.Success && isAppropriateTool(step.ToolName, step.ToolInput) {
			correctUsage++
		}
	}

	return float64(correctUsage) / float64(len(trajectory.Steps))
}

// isAppropriateTool checks if a tool was used appropriately
func isAppropriateTool(toolName, input string) bool {
	// Simple heuristics for tool appropriateness
	toolName = strings.ToLower(toolName)
	input = strings.ToLower(input)

	// 工具-输入关键词映射，与 internal/tool/registry.go 注册的实际工具名对齐
	toolChecks := map[string][]string{
		"read_file":        {"file", "path", ".go", ".md", ".txt", ".json", ".yaml", ".yml"},
		"write_file":       {"file", "path", "content"},
		"file_edit":        {"file", "path", "old", "new"},
		"list_files":       {"dir", "directory", "path", "folder"},
		"search_in_files":  {"search", "pattern", "find", "grep"},
		"directory_tree":   {"dir", "directory", "path", "tree"},
		"web_search":       {"search", "query", "find"},
		"web_fetch":        {"url", "http", "fetch"},
		"web_select":       {"url", "select", "css", "selector"},
		"execute_command":  {"command", "run", "execute", "cmd"},
		"execute_code":     {"code", "language", "python", "javascript"},
		"memory_store":     {"key", "value", "store", "memory"},
		"memory_recall":    {"key", "query", "recall", "memory"},
		"browser_navigate": {"url", "http", "navigate"},
		"browser_click":    {"click", "selector", "element"},
		"browser_type":     {"type", "input", "text", "selector"},
		"browser_snapshot": {"snapshot", "page", "screenshot"},
	}

	checks, exists := toolChecks[toolName]
	if !exists {
		// 未知工具：若有输入则假定合理（宽容处理，避免新工具被误判为 0），
		// 无输入则判定为不合适。
		return input != ""
	}

	// Check if input contains relevant keywords
	for _, keyword := range checks {
		if strings.Contains(input, keyword) {
			return true
		}
	}

	return false
}

// calculateOverallScore calculates the weighted overall score
func (e *EffectivenessEvaluator) calculateOverallScore(
	success, efficiency, quality, toolAccuracy float64,
) float64 {
	overall := success*e.weights.SuccessWeight +
		efficiency*e.weights.EfficiencyWeight +
		quality*e.weights.QualityWeight +
		toolAccuracy*e.weights.ToolAccuracyWeight

	return math.Max(0.0, math.Min(1.0, overall))
}

// GetAverageScore calculates the average score across multiple trajectories
func (e *EffectivenessEvaluator) GetAverageScore(scores []EffectivenessScore) float64 {
	if len(scores) == 0 {
		return 0.0
	}

	total := 0.0
	for _, score := range scores {
		total += score.OverallScore
	}

	return total / float64(len(scores))
}

// GetSuccessRate calculates the success rate
func (e *EffectivenessEvaluator) GetSuccessRate(scores []EffectivenessScore) float64 {
	if len(scores) == 0 {
		return 0.0
	}

	successes := 0
	for _, score := range scores {
		if score.Success {
			successes++
		}
	}

	return float64(successes) / float64(len(scores))
}

// GetTopPerformers returns the top N performing trajectories
func (e *EffectivenessEvaluator) GetTopPerformers(
	scores []EffectivenessScore,
	n int,
) []EffectivenessScore {
	// Sort by overall score (descending) using stdlib sort.Slice instead of O(n^2) bubble sort
	sorted := make([]EffectivenessScore, len(scores))
	copy(sorted, scores)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].OverallScore > sorted[j].OverallScore
	})

	if n > len(sorted) {
		n = len(sorted)
	}

	return sorted[:n]
}

// AnalyzeTrend analyzes score trends over time
func (e *EffectivenessEvaluator) AnalyzeTrend(
	scores []EffectivenessScore,
	trajectories []Trajectory,
) map[string]interface{} {
	if len(scores) < 2 {
		return map[string]interface{}{
			"trend": "insufficient_data",
		}
	}

	// Sort by timestamp
	type scoredTrajectory struct {
		score     EffectivenessScore
		timestamp time.Time
	}

	scored := make([]scoredTrajectory, len(scores))
	for i, score := range scores {
		scored[i] = scoredTrajectory{
			score:     score,
			timestamp: findTrajectoryTimestamp(trajectories, score.TrajectoryID),
		}
	}

	// Simple trend analysis
	recentScores := scored[len(scored)-minInt(len(scored), 10):]
	olderScores := scored[:minInt(len(scored)/2, 10)]

	recentAvg := 0.0
	for _, s := range recentScores {
		recentAvg += s.score.OverallScore
	}
	recentAvg /= float64(len(recentScores))

	olderAvg := 0.0
	for _, s := range olderScores {
		olderAvg += s.score.OverallScore
	}
	olderAvg /= float64(len(olderScores))

	trend := "stable"
	improvement := recentAvg - olderAvg
	if improvement > 0.1 {
		trend = "improving"
	} else if improvement < -0.1 {
		trend = "declining"
	}

	return map[string]interface{}{
		"trend":       trend,
		"improvement": improvement,
		"recent_avg":  recentAvg,
		"older_avg":   olderAvg,
	}
}

// findTrajectoryTimestamp finds a trajectory's timestamp by ID
func findTrajectoryTimestamp(trajectories []Trajectory, id string) time.Time {
	for _, t := range trajectories {
		if t.ID == id {
			return t.EndTime
		}
	}
	return time.Time{}
}

// minInt returns the minimum of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
