package cortex

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
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
	mu       sync.Mutex // 保护 rng 的并发访问，rand.Rand 非并发安全
	rng      *rand.Rand
}

// NewStrategyGenerator creates a new strategy generator
func NewStrategyGenerator(prov provider.Provider) *StrategyGenerator {
	return &StrategyGenerator{
		provider: prov,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// randIntn 返回 [0,n) 的随机整数，串行化对 rng 的访问以保证并发安全
func (g *StrategyGenerator) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rng.Intn(n)
}

// shuffle 串行化 rng.Shuffle 以保证并发安全
func (g *StrategyGenerator) shuffle(n int, swap func(i, j int)) {
	if n <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rng.Shuffle(n, swap)
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
		parent := strategies[g.randIntn(len(strategies))]

		// Create mutation
		mutation := g.mutateStrategy(&parent)
		mutated = append(mutated, mutation)
	}

	return mutated
}

// mutateStrategy creates a single mutation。
// 对 changes 做实质性变异（裁剪/重排/合并/增强），而非仅修改名字后缀。
func (g *StrategyGenerator) mutateStrategy(parent *OptimizationStrategy) OptimizationStrategy {
	mutation := OptimizationStrategy{
		ID:          fmt.Sprintf("mut_%d_%d", time.Now().Unix(), g.randIntn(1000)),
		Name:        parent.Name + " (变异)",
		Description: parent.Description,
		Target:      parent.Target,
		Changes:     make([]PromptChange, len(parent.Changes)),
	}
	copy(mutation.Changes, parent.Changes)

	if len(mutation.Changes) == 0 {
		return mutation
	}

	// 随机选择一种实质性变异算子
	switch g.randIntn(4) {
	case 0:
		// 裁剪：删除一个随机 change，简化策略
		idx := g.randIntn(len(mutation.Changes))
		mutation.Changes = append(mutation.Changes[:idx], mutation.Changes[idx+1:]...)
	case 1:
		// 重排序：打乱 changes 顺序
		g.shuffle(len(mutation.Changes), func(i, j int) {
			mutation.Changes[i], mutation.Changes[j] = mutation.Changes[j], mutation.Changes[i]
		})
	case 2:
		// 合并：将两个相邻 changes 合并为一个
		if len(mutation.Changes) >= 2 {
			idx := g.randIntn(len(mutation.Changes) - 1)
			merged := PromptChange{
				Type:       "modify",
				Section:    mutation.Changes[idx].Section,
				OldContent: mutation.Changes[idx].OldContent,
				NewContent: mutation.Changes[idx].NewContent + " " + mutation.Changes[idx+1].NewContent,
				Reason:     mutation.Changes[idx].Reason + "; " + mutation.Changes[idx+1].Reason,
			}
			mutation.Changes = append(mutation.Changes[:idx], append([]PromptChange{merged}, mutation.Changes[idx+2:]...)...)
		}
	case 3:
		// 增强内容：标注放入 Reason（不污染 NewContent，避免 applyAdd 幂等检查失效）
		idx := g.randIntn(len(mutation.Changes))
		change := &mutation.Changes[idx]
		augmentations := []string{
			"增加示例以提升清晰度",
			"明确边界条件",
			"补充错误处理指引",
			"增加中文说明",
		}
		change.Reason = change.Reason + " [变异增强: " + augmentations[g.randIntn(len(augmentations))] + "]"
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
		parent1 := strategies[g.randIntn(len(strategies))]
		parent2 := strategies[g.randIntn(len(strategies))]

		// Create crossover
		child := g.crossover(&parent1, &parent2)
		crossover = append(crossover, child)
	}

	return crossover
}

// crossover combines two strategies。
// 交替从两个父代选取 changes 并去重，实现真正的基因重组而非仅截断前缀。
func (g *StrategyGenerator) crossover(parent1, parent2 *OptimizationStrategy) OptimizationStrategy {
	child := OptimizationStrategy{
		ID:          fmt.Sprintf("cross_%d_%d", time.Now().Unix(), g.randIntn(1000)),
		Name:        parent1.Name + " + " + parent2.Name,
		Description: "组合策略：" + parent1.Name + " 与 " + parent2.Name,
		Target:      parent1.Target,
		Changes:     []PromptChange{},
	}

	// 交叉：交替从两个父代选取 changes，每侧最多取若干条以保证规模可控
	const maxFromEach = 3
	for i := 0; i < len(parent1.Changes) && i < maxFromEach; i++ {
		child.Changes = append(child.Changes, parent1.Changes[i])
	}
	for i := 0; i < len(parent2.Changes) && i < maxFromEach; i++ {
		child.Changes = append(child.Changes, parent2.Changes[i])
	}

	// 去重：避免完全相同的 change 重复出现
	child.Changes = dedupChanges(child.Changes)

	// 限制总 changes 数量，避免过度膨胀
	if len(child.Changes) > 6 {
		child.Changes = child.Changes[:6]
	}

	return child
}

// dedupChanges 去除 Section 与 NewContent 完全相同的 change
func dedupChanges(changes []PromptChange) []PromptChange {
	seen := make(map[string]bool, len(changes))
	result := make([]PromptChange, 0, len(changes))
	for _, c := range changes {
		key := c.Section + "|" + c.NewContent
		if !seen[key] {
			seen[key] = true
			result = append(result, c)
		}
	}
	return result
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
