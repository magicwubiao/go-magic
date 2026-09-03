// Package complexity provides task complexity analysis functionality.
// This package is independent to avoid circular imports between agent and perception packages.
package complexity

import (
	"strings"
)

// Analyzer analyzes task complexity
type Analyzer struct {
	threshold int
}

// NewAnalyzer creates a new complexity analyzer
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		threshold: 50, // Tasks with score > 50 should be decomposed
	}
}

// Score represents the complexity of a task
type Score struct {
	Score       int      `json:"score"`
	Factors     []string `json:"factors"`
	ShouldSplit bool     `json:"should_split"`
}

// Analyze analyzes the complexity of a task description
func (a *Analyzer) Analyze(taskDescription string) *Score {
	score := 0
	factors := []string{}

	// Factor 1: Length of description
	if len(taskDescription) > 200 {
		score += 20
		factors = append(factors, "long_description")
	}

	// Factor 2: Number of steps/actions implied
	actionKeywords := []string{"and then", "after that", "next", "finally", "subsequently"}
	for _, keyword := range actionKeywords {
		if strings.Contains(strings.ToLower(taskDescription), keyword) {
			score += 10
			factors = append(factors, "multiple_steps")
			break
		}
	}

	// Factor 3: Multiple domains mentioned
	domains := []string{"file", "database", "api", "web", "server", "client", "frontend", "backend"}
	domainCount := 0
	for _, domain := range domains {
		if strings.Contains(strings.ToLower(taskDescription), domain) {
			domainCount++
		}
	}
	if domainCount > 2 {
		score += 15
		factors = append(factors, "multiple_domains")
	}

	// Factor 4: Uncertainty indicators
	uncertaintyKeywords := []string{"research", "investigate", "explore", "analyze", "find out"}
	for _, keyword := range uncertaintyKeywords {
		if strings.Contains(strings.ToLower(taskDescription), keyword) {
			score += 10
			factors = append(factors, "research_needed")
			break
		}
	}

	return &Score{
		Score:       score,
		Factors:     factors,
		ShouldSplit: score > a.threshold,
	}
}

// ShouldDecompose returns true if the task should be decomposed
func (a *Analyzer) ShouldDecompose(taskDescription string) bool {
	score := a.Analyze(taskDescription)
	return score.ShouldSplit
}
