package cortex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// ============================================================================
// GEPA Engine - Generative Evolutionary Prompt Alignment
// ============================================================================
// GEPA is the core self-evolution engine inspired by Hermes Agent.
// It learns from execution trajectories and evolves system prompts through
// an evolutionary optimization process.
//
// Algorithm:
// 1. Collect trajectories (task executions)
// 2. Evaluate effectiveness (success rate, efficiency)
// 3. Generate optimization strategies
// 4. Apply strategies to prompts
// 5. Iterate and converge (100-500 iterations)
// ============================================================================

// GEPAEngine is the main GEPA orchestrator
type GEPAEngine struct {
	mu sync.RWMutex

	// Configuration
	baseDir              string
	provider             provider.Provider
	convergenceThreshold float64 // Stop when improvement < threshold
	maxIterations        int
	populationSize       int

	// Components
	evaluator       *EffectivenessEvaluator
	generator       *StrategyGenerator
	optimizer       *PromptOptimizer
	trajectoryStore *TrajectoryStore

	// Evolution state
	generations  []Generation
	currentGen   int
	bestStrategy *OptimizationStrategy
	isConverged  bool

	// Metrics
	totalTrajectories int
	successRate       float64
	avgEffectiveness  float64
}

// Generation represents one evolution iteration
type Generation struct {
	ID           int                    `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	Strategies   []OptimizationStrategy `json:"strategies"`
	BestStrategy *OptimizationStrategy  `json:"best_strategy"`
	AvgFitness   float64                `json:"avg_fitness"`
	Improvement  float64                `json:"improvement"`
	Trajectories []string               `json:"trajectory_ids"`
}

// OptimizationStrategy represents a prompt optimization strategy
type OptimizationStrategy struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Target      string          `json:"target"` // "soul", "system", "skills"
	Changes     []PromptChange  `json:"changes"`
	Fitness     float64         `json:"fitness"`
	Applied     bool            `json:"applied"`
	AppliedAt   *time.Time      `json:"applied_at,omitempty"`
	Results     *StrategyResult `json:"results,omitempty"`
}

// PromptChange represents a single prompt modification
type PromptChange struct {
	Type       string `json:"type"`    // "add", "remove", "modify", "reorder"
	Section    string `json:"section"` // Section name
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
	Reason     string `json:"reason"`
}

// StrategyResult tracks the outcome of applying a strategy
type StrategyResult struct {
	BeforeSuccessRate float64   `json:"before_success_rate"`
	AfterSuccessRate  float64   `json:"after_success_rate"`
	Improvement       float64   `json:"improvement"`
	SampleSize        int       `json:"sample_size"`
	MeasuredAt        time.Time `json:"measured_at"`
}

// EffectivenessScore represents a trajectory's effectiveness
type EffectivenessScore struct {
	TrajectoryID string  `json:"trajectory_id"`
	Success      bool    `json:"success"`
	Efficiency   float64 `json:"efficiency"`    // 0-1, based on turns vs optimal
	Quality      float64 `json:"quality"`       // 0-1, based on output quality
	ToolAccuracy float64 `json:"tool_accuracy"` // 0-1, correct tool usage
	OverallScore float64 `json:"overall_score"` // Weighted combination
}

// NewGEPAEngine creates a new GEPA engine
func NewGEPAEngine(baseDir string, prov provider.Provider, trajectoryStore *TrajectoryStore) *GEPAEngine {
	return &GEPAEngine{
		baseDir:              baseDir,
		provider:             prov,
		convergenceThreshold: 0.01, // 1% improvement threshold
		maxIterations:        500,
		populationSize:       10,
		evaluator:            NewEffectivenessEvaluator(),
		generator:            NewStrategyGenerator(prov),
		optimizer:            NewPromptOptimizer(prov),
		trajectoryStore:      trajectoryStore,
		generations:          make([]Generation, 0),
	}
}

// Start starts the GEPA evolution process
func (g *GEPAEngine) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Load previous generations
	g.loadGenerations()

	// Start evolution loop in background
	go g.evolutionLoop(ctx)

	return nil
}

// evolutionLoop runs the main GEPA evolution algorithm
func (g *GEPAEngine) evolutionLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute) // Evaluate every 5 minutes
	defer ticker.Stop()

	// Use background context if nil
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if g.isConverged {
				continue
			}

			if err := g.evolve(ctx); err != nil {
				// Log error but continue
				continue
			}
		}
	}
}

// evolve performs one evolution iteration
func (g *GEPAEngine) evolve(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 1. Collect recent trajectories
	trajectories := g.trajectoryStore.GetTrajectories(100)
	if len(trajectories) < 10 {
		return fmt.Errorf("insufficient trajectories for evolution")
	}

	// 2. Evaluate effectiveness
	scores := g.evaluator.EvaluateBatch(trajectories)

	// 3. Generate optimization strategies
	strategies, err := g.generator.GenerateStrategies(ctx, scores, g.populationSize)
	if err != nil {
		return err
	}

	// 4. Evaluate strategies
	for i := range strategies {
		strategies[i].Fitness = g.evaluateStrategyFitness(&strategies[i], scores)
	}

	// 5. Select best strategy
	best := g.selectBestStrategy(strategies)

	// 6. Calculate improvement
	improvement := 0.0
	if len(g.generations) > 0 {
		prevBest := g.generations[len(g.generations)-1].BestStrategy
		if prevBest != nil {
			improvement = best.Fitness - prevBest.Fitness
		}
	}

	// 7. Create new generation
	gen := Generation{
		ID:           g.currentGen + 1,
		Timestamp:    time.Now(),
		Strategies:   strategies,
		BestStrategy: best,
		AvgFitness:   g.calculateAvgFitness(strategies),
		Improvement:  improvement,
	}

	// 8. Check convergence
	if improvement < g.convergenceThreshold && len(g.generations) > 50 {
		g.isConverged = true
	}

	// 9. Save generation
	g.generations = append(g.generations, gen)
	g.currentGen = gen.ID
	g.bestStrategy = best
	g.saveGeneration(&gen)

	// 10. Update metrics
	g.updateMetrics(scores)

	return nil
}

// ApplyBestStrategy applies the best strategy to the system
func (g *GEPAEngine) ApplyBestStrategy(soul *SoulManager, systemPrompt string) (string, error) {
	g.mu.RLock()
	best := g.bestStrategy
	g.mu.RUnlock()

	if best == nil {
		return systemPrompt, nil
	}

	// Apply strategy using optimizer
	optimized, err := g.optimizer.Optimize(systemPrompt, best.Changes)
	if err != nil {
		return systemPrompt, err
	}

	// Mark as applied
	now := time.Now()
	best.Applied = true
	best.AppliedAt = &now

	return optimized, nil
}

// evaluateStrategyFitness evaluates a strategy's potential fitness
func (g *GEPAEngine) evaluateStrategyFitness(strategy *OptimizationStrategy, scores []EffectivenessScore) float64 {
	// Base fitness on average effectiveness
	avgScore := 0.0
	for _, s := range scores {
		avgScore += s.OverallScore
	}
	avgScore /= float64(len(scores))

	// Adjust based on strategy type
	multiplier := 1.0
	switch strategy.Target {
	case "soul":
		multiplier = 1.2 // Personality changes have high impact
	case "system":
		multiplier = 1.1
	case "skills":
		multiplier = 1.0
	}

	// Adjust based on number of changes
	if len(strategy.Changes) > 5 {
		multiplier *= 0.9 // Penalize overly complex strategies
	}

	return avgScore * multiplier
}

// selectBestStrategy selects the best strategy from a population
func (g *GEPAEngine) selectBestStrategy(strategies []OptimizationStrategy) *OptimizationStrategy {
	if len(strategies) == 0 {
		return nil
	}

	// Sort by fitness
	sort.Slice(strategies, func(i, j int) bool {
		return strategies[i].Fitness > strategies[j].Fitness
	})

	best := strategies[0]
	return &best
}

// calculateAvgFitness calculates average fitness of strategies
func (g *GEPAEngine) calculateAvgFitness(strategies []OptimizationStrategy) float64 {
	if len(strategies) == 0 {
		return 0
	}

	total := 0.0
	for _, s := range strategies {
		total += s.Fitness
	}
	return total / float64(len(strategies))
}

// updateMetrics updates engine metrics
func (g *GEPAEngine) updateMetrics(scores []EffectivenessScore) {
	g.totalTrajectories = len(scores)

	successes := 0
	totalEffectiveness := 0.0
	for _, s := range scores {
		if s.Success {
			successes++
		}
		totalEffectiveness += s.OverallScore
	}

	if len(scores) > 0 {
		g.successRate = float64(successes) / float64(len(scores))
		g.avgEffectiveness = totalEffectiveness / float64(len(scores))
	}
}

// GetStats returns GEPA engine statistics
func (g *GEPAEngine) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return map[string]interface{}{
		"current_generation":    g.currentGen,
		"is_converged":          g.isConverged,
		"total_trajectories":    g.totalTrajectories,
		"success_rate":          g.successRate,
		"avg_effectiveness":     g.avgEffectiveness,
		"best_strategy_id":      g.bestStrategy,
		"convergence_threshold": g.convergenceThreshold,
	}
}

// GetGenerations returns all generations
func (g *GEPAEngine) GetGenerations() []Generation {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.generations
}

// saveGeneration saves a generation to disk
func (g *GEPAEngine) saveGeneration(gen *Generation) {
	path := filepath.Join(g.baseDir, "gepa", "generations", fmt.Sprintf("gen_%d.json", gen.ID))
	os.MkdirAll(filepath.Dir(path), 0755)

	data, _ := json.MarshalIndent(gen, "", "  ")
	os.WriteFile(path, data, 0644)
}

// loadGenerations loads previous generations from disk
func (g *GEPAEngine) loadGenerations() {
	genDir := filepath.Join(g.baseDir, "gepa", "generations")
	entries, err := os.ReadDir(genDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "gen_") {
			continue
		}

		path := filepath.Join(genDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var gen Generation
		if err := json.Unmarshal(data, &gen); err != nil {
			continue
		}

		g.generations = append(g.generations, gen)
		if gen.ID > g.currentGen {
			g.currentGen = gen.ID
			g.bestStrategy = gen.BestStrategy
		}
	}
}

// Reset resets the GEPA engine
func (g *GEPAEngine) Reset() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.generations = make([]Generation, 0)
	g.currentGen = 0
	g.bestStrategy = nil
	g.isConverged = false
	g.totalTrajectories = 0
	g.successRate = 0
	g.avgEffectiveness = 0

	// Clear saved generations
	genDir := filepath.Join(g.baseDir, "gepa", "generations")
	os.RemoveAll(genDir)

	return nil
}