package cortex

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// TrajectoryStepEnhanced extends TrajectoryStep with richer data
type TrajectoryStepEnhanced struct {
	TrajectoryStep

	Reasoning     string   `json:"reasoning,omitempty"`
	ExpectedOut   string   `json:"expected_outcome,omitempty"`
	ActualOut     string   `json:"actual_outcome,omitempty"`
	Deviation     string   `json:"deviation,omitempty"`
	Lessons       []string `json:"lessons_learned,omitempty"`
	TurnNumber    int      `json:"turn_number"`
	StepType      string   `json:"step_type"` // plan, execute, reflect, decide
	Confidence    float64  `json:"confidence,omitempty"`
	ContextBefore string   `json:"context_before,omitempty"`
}

// TrajectoryEnhanced extends Trajectory with richer data for learning
type TrajectoryEnhanced struct {
	Trajectory

	StepsEnhanced   []TrajectoryStepEnhanced `json:"steps_enhanced,omitempty"`
	PlanSnapshot    string                   `json:"plan_snapshot,omitempty"`
	FinalReflection string                   `json:"final_reflection,omitempty"`
	UserFeedback    string                   `json:"user_feedback,omitempty"`
	StrategyType    string                   `json:"strategy_type,omitempty"`
	ErrorPatterns   []string                 `json:"error_patterns,omitempty"`
	KeyDecisions    []string                 `json:"key_decisions,omitempty"`
	QualityScore    float64                  `json:"quality_score,omitempty"`
}

// TrajectoryInjector handles real-time injection of trajectory-based insights
type TrajectoryInjector struct {
	store    *TrajectoryStore
	provider provider.Provider
	enabled  bool

	// Configuration
	maxExamples        int
	maxSimilarity      float64
	useFailureExamples bool
}

// TrajectoryInjectorConfig configures the injector
type TrajectoryInjectorConfig struct {
	Enabled            bool
	MaxExamples        int
	UseFailureExamples bool
}

// DefaultTrajectoryInjectorConfig returns default config
func DefaultTrajectoryInjectorConfig() TrajectoryInjectorConfig {
	return TrajectoryInjectorConfig{
		Enabled:            true,
		MaxExamples:        2,
		UseFailureExamples: true,
	}
}

// NewTrajectoryInjector creates a new trajectory injector
func NewTrajectoryInjector(store *TrajectoryStore, prov provider.Provider, cfg TrajectoryInjectorConfig) *TrajectoryInjector {
	return &TrajectoryInjector{
		store:              store,
		provider:           prov,
		enabled:            cfg.Enabled,
		maxExamples:        cfg.MaxExamples,
		useFailureExamples: cfg.UseFailureExamples,
	}
}

// GetRelevantTrajectories finds trajectories relevant to the current task
func (ti *TrajectoryInjector) GetRelevantTrajectories(task string, limit int) []Trajectory {
	if !ti.enabled || ti.store == nil {
		return nil
	}

	allTrajectories := ti.store.GetTrajectories(0)
	if len(allTrajectories) == 0 {
		return nil
	}

	// Score each trajectory by relevance
	type scored struct {
		traj  Trajectory
		score float64
	}

	scoredList := make([]scored, 0, len(allTrajectories))
	taskKeywords := extractKeywords(task)

	for _, t := range allTrajectories {
		score := calculateRelevanceScore(task, taskKeywords, t)
		scoredList = append(scoredList, scored{traj: t, score: score})
	}

	// Sort by score descending
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	// Return top N
	if limit > len(scoredList) {
		limit = len(scoredList)
	}

	result := make([]Trajectory, 0, limit)
	minScore := 0.1 // Minimum relevance threshold
	for i := 0; i < limit; i++ {
		if scoredList[i].score >= minScore {
			result = append(result, scoredList[i].traj)
		}
	}

	return result
}

// BuildFewShotPrompt builds a few-shot prompt from relevant trajectories
func (ti *TrajectoryInjector) BuildFewShotPrompt(task string) string {
	if !ti.enabled {
		return ""
	}

	trajectories := ti.GetRelevantTrajectories(task, ti.maxExamples)
	if len(trajectories) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n=== Relevant Previous Experiences ===\n\n")

	for i, t := range trajectories {
		if t.Success {
			sb.WriteString(fmt.Sprintf("Example %d (Successful):\n", i+1))
		} else {
			sb.WriteString(fmt.Sprintf("Example %d (Failed - learn from this):\n", i+1))
		}
		sb.WriteString(fmt.Sprintf("Task: %s\n", t.Task))
		sb.WriteString(fmt.Sprintf("Result: %s\n", t.Result))

		// Summarize key steps
		if len(t.Steps) > 0 {
			sb.WriteString("Key steps:\n")
			for j, step := range t.Steps {
				if j >= 5 {
					sb.WriteString(fmt.Sprintf("  ... and %d more steps\n", len(t.Steps)-5))
					break
				}
				status := "✓"
				if !step.Success {
					status = "✗"
				}
				sb.WriteString(fmt.Sprintf("  %s %s\n", status, step.ToolName))
			}
		}

		if t.Score > 0 {
			sb.WriteString(fmt.Sprintf("Score: %.1f/100\n", t.Score))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Use these examples as reference. Learn from both successes and failures.\n")

	return sb.String()
}

// BuildFailureAvoidancePrompt builds a prompt about what NOT to do
func (ti *TrajectoryInjector) BuildFailureAvoidancePrompt(task string) string {
	if !ti.enabled || !ti.useFailureExamples {
		return ""
	}

	allTrajectories := ti.store.GetTrajectories(0)
	var failedTrajectories []Trajectory
	for _, t := range allTrajectories {
		if !t.Success && strings.Contains(strings.ToLower(t.Task), strings.ToLower(task)) {
			failedTrajectories = append(failedTrajectories, t)
		}
	}

	if len(failedTrajectories) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n=== Common Pitfalls to Avoid ===\n\n")

	for i, t := range failedTrajectories {
		if i >= 3 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. Task \"%s\" failed. ", i+1, t.Task))
		if t.Result != "" {
			sb.WriteString(fmt.Sprintf("Failure: %s", t.Result))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nAvoid repeating these mistakes.\n")

	return sb.String()
}

// BuildStepGuidance builds guidance for the current step based on similar steps
func (ti *TrajectoryInjector) BuildStepGuidance(currentTask string, currentTool string) string {
	if !ti.enabled || ti.store == nil {
		return ""
	}

	allTrajectories := ti.store.GetTrajectories(0)
	type stepInfo struct {
		task    string
		tool    string
		input   string
		output  string
		success bool
	}

	var similarSteps []stepInfo
	for _, t := range allTrajectories {
		for _, step := range t.Steps {
			if step.ToolName == currentTool {
				if isTaskSimilar(t.Task, currentTask) {
					similarSteps = append(similarSteps, stepInfo{
						task:    t.Task,
						tool:    step.ToolName,
						input:   step.ToolInput,
						output:  step.ToolOutput,
						success: step.Success,
					})
				}
			}
		}
	}

	if len(similarSteps) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n\n=== Hints for %s ===\n\n", currentTool))

	// Show successful examples
	successCount := 0
	for _, s := range similarSteps {
		if s.success && successCount < 2 {
			sb.WriteString(fmt.Sprintf("Successful usage example:\nInput: %s\nOutput preview: %s\n\n",
				truncate(s.input, 200), truncate(s.output, 200)))
			successCount++
		}
	}

	// Show common failures
	failCount := 0
	for _, s := range similarSteps {
		if !s.success && failCount < 1 {
			sb.WriteString(fmt.Sprintf("Common failure:\nInput: %s\nError: %s\n\n",
				truncate(s.input, 200), truncate(s.output, 200)))
			failCount++
		}
	}

	return sb.String()
}

// AnalyzeTrajectoryQuality analyzes a trajectory for quality metrics
func (ti *TrajectoryInjector) AnalyzeTrajectoryQuality(ctx context.Context, trajectory *Trajectory) (float64, error) {
	if ti.provider == nil {
		return 0.5, nil
	}

	// Build summary of trajectory
	var stepsSummary strings.Builder
	for _, step := range trajectory.Steps {
		status := "success"
		if !step.Success {
			status = "failed"
		}
		stepsSummary.WriteString(fmt.Sprintf("- %s (%s)\n", step.ToolName, status))
	}

	prompt := fmt.Sprintf(`Rate the quality of this agent execution trajectory on a scale of 0-100.

Task: %s
Result: %s
Success: %v
Number of steps: %d
Steps:
%s

Consider:
1. Did it achieve the goal efficiently?
2. Were the tool choices optimal?
3. Was there unnecessary backtracking?
4. Was the final result high quality?

Respond with only a number between 0 and 100.`,
		trajectory.Task,
		trajectory.Result,
		trajectory.Success,
		len(trajectory.Steps),
		stepsSummary.String(),
	)

	resp, err := ti.provider.Chat(ctx, []provider.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Warnf("[Trajectory] Quality analysis failed: %v", err)
		return 0.5, nil
	}

	// Parse score from response
	var score float64
	fmt.Sscanf(strings.TrimSpace(resp.Content), "%f", &score)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score / 100.0, nil
}

// Helper functions

func extractKeywords(text string) []string {
	text = strings.ToLower(text)
	words := strings.Fields(text)
	var keywords []string
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"to": true, "for": true, "of": true, "in": true, "on": true,
		"and": true, "or": true, "with": true, "by": true, "from": true,
		"it": true, "this": true, "that": true, "be": true, "do": true,
	}
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:\"'()[]{}")
		if len(w) > 2 && !stopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

func calculateRelevanceScore(task string, keywords []string, trajectory Trajectory) float64 {
	score := 0.0
	taskLower := strings.ToLower(task)
	trajTaskLower := strings.ToLower(trajectory.Task)
	trajDescLower := strings.ToLower(trajectory.Description)

	// Direct substring match
	if strings.Contains(trajTaskLower, taskLower) || strings.Contains(taskLower, trajTaskLower) {
		score += 0.5
	}
	if strings.Contains(trajDescLower, taskLower) {
		score += 0.2
	}

	// Keyword overlap
	matchCount := 0
	for _, kw := range keywords {
		if strings.Contains(trajTaskLower, kw) || strings.Contains(trajDescLower, kw) {
			matchCount++
		}
	}
	if len(keywords) > 0 {
		score += float64(matchCount) / float64(len(keywords)) * 0.4
	}

	// Boost for successful trajectories
	if trajectory.Success {
		score += 0.1
	}

	// Boost for scored trajectories
	if trajectory.Score > 0 {
		score += trajectory.Score * 0.05
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

func isTaskSimilar(task1, task2 string) bool {
	kw1 := extractKeywords(task1)
	kw2 := extractKeywords(task2)
	if len(kw1) == 0 || len(kw2) == 0 {
		return false
	}
	overlap := 0
	for _, k1 := range kw1 {
		for _, k2 := range kw2 {
			if k1 == k2 {
				overlap++
				break
			}
		}
	}
	return float64(overlap)/float64(len(kw1)) >= 0.3
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RecordEnhancedStep records an enhanced trajectory step
func RecordEnhancedStep(traj *TrajectoryEnhanced, step TrajectoryStepEnhanced) {
	traj.StepsEnhanced = append(traj.StepsEnhanced, step)
}

// GenerateFinalReflection generates a final reflection on the trajectory
func (ti *TrajectoryInjector) GenerateFinalReflection(ctx context.Context, trajectory *TrajectoryEnhanced) error {
	if ti.provider == nil {
		return nil
	}

	var stepsSummary strings.Builder
	for _, step := range trajectory.StepsEnhanced {
		stepsSummary.WriteString(fmt.Sprintf("Step %d (%s): %s\n",
			step.TurnNumber, step.ToolName, step.Lessons))
	}

	prompt := fmt.Sprintf(`Reflect on this agent execution and summarize key lessons learned.

Task: %s
Success: %v
Result: %s
Steps:
%s

Provide:
1. What went well
2. What went wrong
3. Key lessons learned
4. What would you do differently next time?`,
		trajectory.Task,
		trajectory.Success,
		trajectory.Result,
		stepsSummary.String(),
	)

	resp, err := ti.provider.Chat(ctx, []provider.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return err
	}

	trajectory.FinalReflection = resp.Content
	return nil
}
