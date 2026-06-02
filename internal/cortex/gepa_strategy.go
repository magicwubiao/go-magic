package cortex

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// ============================================================================
// StrategyGenerator - Generates optimization strategies using LLM
// ============================================================================
// Uses LLM to analyze effectiveness scores and generate prompt optimization
// strategies. Strategies are evolved through mutation and crossover.
// ============================================================================

// StrategyGenerator generates optimization strategies
type StrategyGenerator struct {
	provider provider.Provider
	rng      *rand.Rand
}

// NewStrategyGenerator creates a new strategy generator
func NewStrategyGenerator(prov provider.Provider) *StrategyGenerator {
	return &StrategyGenerator{
		provider: prov,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateStrategies generates optimization strategies based on effectiveness scores
func (g *StrategyGenerator) GenerateStrategies(
	ctx context.Context,
	scores []EffectivenessScore,
	populationSize int,
) ([]OptimizationStrategy, error) {
	if len(scores) == 0 {
		return nil, fmt.Errorf("no scores to analyze")
	}

	// Analyze patterns in low-performing trajectories
	analysis := g.analyzePatterns(scores)

	// Generate initial strategies using LLM
	strategies, err := g.generateInitialStrategies(ctx, analysis, populationSize/2)
	if err != nil {
		return nil, err
	}

	// Evolve strategies through mutation
	mutated := g.mutateStrategies(strategies, populationSize/4)
	strategies = append(strategies, mutated...)

	// Evolve through crossover
	crossover := g.crossoverStrategies(strategies, populationSize/4)
	strategies = append(strategies, crossover...)

	// Ensure we have exactly populationSize strategies
	if len(strategies) > populationSize {
		strategies = strategies[:populationSize]
	}

	return strategies, nil
}

// analyzePatterns analyzes patterns in effectiveness scores
func (g *StrategyGenerator) analyzePatterns(scores []EffectivenessScore) map[string]interface{} {
	analysis := map[string]interface{}{
		"total_trajectories": len(scores),
		"success_rate":       0.0,
		"avg_effectiveness":  0.0,
		"low_performers":     []EffectivenessScore{},
		"high_performers":    []EffectivenessScore{},
	}

	if len(scores) == 0 {
		return analysis
	}

	// Calculate metrics
	successes := 0
	totalScore := 0.0
	for _, score := range scores {
		if score.Success {
			successes++
		}
		totalScore += score.OverallScore

		// Categorize
		if score.OverallScore < 0.5 {
			analysis["low_performers"] = append(
				analysis["low_performers"].([]EffectivenessScore),
				score,
			)
		} else if score.OverallScore > 0.8 {
			analysis["high_performers"] = append(
				analysis["high_performers"].([]EffectivenessScore),
				score,
			)
		}
	}

	analysis["success_rate"] = float64(successes) / float64(len(scores))
	analysis["avg_effectiveness"] = totalScore / float64(len(scores))

	return analysis
}

// generateInitialStrategies generates initial strategies using LLM
func (g *StrategyGenerator) generateInitialStrategies(
	ctx context.Context,
	analysis map[string]interface{},
	count int,
) ([]OptimizationStrategy, error) {
	// Build prompt for LLM
	prompt := g.buildStrategyPrompt(analysis, count)

	// Call LLM
	type openAIlike interface {
		Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
	}

	var resp *provider.ChatResponse
	var err error

	if oa, ok := g.provider.(openAIlike); ok {
		resp, err = oa.Chat(ctx, []provider.Message{
			{Role: "user", Content: prompt},
		})
	} else {
		return nil, fmt.Errorf("provider does not support chat")
	}

	if err != nil {
		return nil, err
	}

	// Parse strategies from response
	return g.parseStrategies(resp.Content)
}

// buildStrategyPrompt builds the prompt for strategy generation
func (g *StrategyGenerator) buildStrategyPrompt(
	analysis map[string]interface{},
	count int,
) string {
	return fmt.Sprintf(`You are an expert prompt engineer. Analyze the following execution data and generate %d optimization strategies.

Performance Analysis:
- Total trajectories: %d
- Success rate: %.2f%%
- Average effectiveness: %.2f

Generate optimization strategies in JSON format:
[
  {
    "name": "Strategy name",
    "description": "What this strategy does",
    "target": "soul|system|skills",
    "changes": [
      {
        "type": "add|remove|modify",
        "section": "section name",
        "new_content": "new content",
        "reason": "why this change helps"
      }
    ]
  }
]

Guidelines:
1. Focus on common failure patterns
2. Be specific about changes
3. Target the most impactful areas
4. Ensure changes are actionable`,
		count,
		analysis["total_trajectories"],
		analysis["success_rate"].(float64)*100,
		analysis["avg_effectiveness"],
	)
}

// parseStrategies parses strategies from LLM response
func (g *StrategyGenerator) parseStrategies(content string) ([]OptimizationStrategy, error) {
	// Clean up markdown
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var strategies []OptimizationStrategy
	if err := json.Unmarshal([]byte(content), &strategies); err != nil {
		// Try to extract JSON from text
		start := strings.Index(content, "[")
		end := strings.LastIndex(content, "]")
		if start >= 0 && end > start {
			if err := json.Unmarshal([]byte(content[start:end+1]), &strategies); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Assign IDs
	for i := range strategies {
		strategies[i].ID = fmt.Sprintf("strategy_%d_%d", time.Now().Unix(), i)
	}

	return strategies, nil
}

// mutateStrategies creates variations of existing strategies
func (g *StrategyGenerator) mutateStrategies(
	strategies []OptimizationStrategy,
	count int,
) []OptimizationStrategy {
	mutated := make([]OptimizationStrategy, 0, count)

	for i := 0; i < count && len(strategies) > 0; i++ {
		// Select random parent
		parent := strategies[g.rng.Intn(len(strategies))]

		// Create mutation
		mutation := g.mutateStrategy(&parent)
		mutated = append(mutated, mutation)
	}

	return mutated
}

// mutateStrategy creates a single mutation
func (g *StrategyGenerator) mutateStrategy(parent *OptimizationStrategy) OptimizationStrategy {
	mutation := OptimizationStrategy{
		ID:          fmt.Sprintf("mut_%d_%d", time.Now().Unix(), g.rng.Intn(1000)),
		Name:        parent.Name + " (Mutated)",
		Description: parent.Description,
		Target:      parent.Target,
		Changes:     make([]PromptChange, len(parent.Changes)),
	}

	copy(mutation.Changes, parent.Changes)

	// Apply random mutation
	if len(mutation.Changes) > 0 {
		idx := g.rng.Intn(len(mutation.Changes))
		change := &mutation.Changes[idx]

		mutations := []func(*PromptChange){
			func(c *PromptChange) { c.Type = "add" },
			func(c *PromptChange) { c.Type = "modify" },
			func(c *PromptChange) { c.NewContent += " (enhanced)" },
			func(c *PromptChange) { c.Reason += " [mutated]" },
		}

		if len(mutations) > 0 {
			mutations[g.rng.Intn(len(mutations))](change)
		}
	}

	return mutation
}

// crossoverStrategies combines two strategies
func (g *StrategyGenerator) crossoverStrategies(
	strategies []OptimizationStrategy,
	count int,
) []OptimizationStrategy {
	crossover := make([]OptimizationStrategy, 0, count)

	for i := 0; i < count && len(strategies) >= 2; i++ {
		// Select two random parents
		parent1 := strategies[g.rng.Intn(len(strategies))]
		parent2 := strategies[g.rng.Intn(len(strategies))]

		// Create crossover
		child := g.crossover(&parent1, &parent2)
		crossover = append(crossover, child)
	}

	return crossover
}

// crossover combines two strategies
func (g *StrategyGenerator) crossover(parent1, parent2 *OptimizationStrategy) OptimizationStrategy {
	child := OptimizationStrategy{
		ID:          fmt.Sprintf("cross_%d_%d", time.Now().Unix(), g.rng.Intn(1000)),
		Name:        parent1.Name + " + " + parent2.Name,
		Description: "Combined strategy from " + parent1.Name + " and " + parent2.Name,
		Target:      parent1.Target,
		Changes:     []PromptChange{},
	}

	// Combine changes
	allChanges := append(parent1.Changes, parent2.Changes...)

	// Select random subset
	if len(allChanges) > 0 {
		numChanges := g.rng.Intn(len(allChanges)) + 1
		for i := 0; i < numChanges && i < len(allChanges); i++ {
			child.Changes = append(child.Changes, allChanges[i])
		}
	}

	return child
}

// RefineStrategy refines a strategy based on feedback
func (g *StrategyGenerator) RefineStrategy(
	ctx context.Context,
	strategy *OptimizationStrategy,
	feedback string,
) (*OptimizationStrategy, error) {
	prompt := fmt.Sprintf(`Refine the following optimization strategy based on feedback.

Original Strategy:
Name: %s
Description: %s
Target: %s
Changes:
%s

Feedback: %s

Provide refined strategy in the same JSON format.`,
		strategy.Name,
		strategy.Description,
		strategy.Target,
		g.formatChanges(strategy.Changes),
		feedback,
	)

	type openAIlike interface {
		Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
	}

	var resp *provider.ChatResponse
	var err error

	if oa, ok := g.provider.(openAIlike); ok {
		resp, err = oa.Chat(ctx, []provider.Message{
			{Role: "user", Content: prompt},
		})
	} else {
		return nil, fmt.Errorf("provider does not support chat")
	}

	if err != nil {
		return nil, err
	}

	// Parse refined strategy
	strategies, err := g.parseStrategies(resp.Content)
	if err != nil {
		return nil, err
	}

	if len(strategies) == 0 {
		return nil, fmt.Errorf("no strategy parsed")
	}

	return &strategies[0], nil
}

// formatChanges formats changes for display
func (g *StrategyGenerator) formatChanges(changes []PromptChange) string {
	var parts []string
	for _, c := range changes {
		parts = append(parts, fmt.Sprintf("- %s %s: %s", c.Type, c.Section, c.Reason))
	}
	return strings.Join(parts, "\n")
}
