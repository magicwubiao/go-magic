package learn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Experience represents a learned experience
type Experience struct {
	ID          string                 `json:"id"`
	Timestamp   int64                  `json:"timestamp"`
	Task        string                 `json:"task"`          // What was requested
	Action      string                 `json:"action"`        // What was done
	Success     bool                   `json:"success"`       // Whether it worked
	ToolsUsed   []string               `json:"tools_used"`    // Tools used
	Skills      []string               `json:"skills"`        // Skills invoked
	Duration    int64                  `json:"duration"`     // Time taken (ms)
	Outcome     string                 `json:"outcome"`       // Result summary
	Patterns    []string               `json:"patterns"`      // Identified patterns
	Improvements []string              `json:"improvements"`  // Suggested improvements
	Metadata    map[string]interface{} `json:"metadata"`      // Additional data
}

// LearnedSkill represents an auto-generated skill
type LearnedSkill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Triggers    []string  `json:"triggers"`    // What activates this skill
	Examples    []string  `json:"examples"`     // Usage examples
	SuccessRate float64   `json:"success_rate"` // Historical success rate
	UseCount    int       `json:"use_count"`
	CreatedAt   int64     `json:"created_at"`
	UpdatedAt   int64     `json:"updated_at"`
	Version     int       `json:"version"`      // Iteration number
	Tags        []string  `json:"tags"`
}

// Manager handles the learning loop
type Manager struct {
	dataDir          string
	experiences      []Experience
	skills           map[string]*LearnedSkill
	patterns         map[string]*Pattern
	autoLearnEnabled bool
	minConfidence    float64
	maxExperiences   int
}

// Pattern represents a learned pattern
type Pattern struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Regex       string   `json:"regex"`
	Confidence  float64  `json:"confidence"`  // 0.0-1.0
	Occurrences int      `json:"occurrences"`  // Times seen
	Actions     []string `json:"actions"`      // Recommended actions
	CreatedAt   int64    `json:"created_at"`
	LastSeen    int64    `json:"last_seen"`
}

// Config holds learning configuration
type Config struct {
	Enabled         bool     `yaml:"enabled"`
	AutoLearn       bool     `yaml:"auto_learn"`       // Automatically create skills
	MinConfidence   float64  `yaml:"min_confidence"`    // Min confidence to suggest
	MaxExperiences  int      `yaml:"max_experiences"`  // Max experience entries
	SkillThreshold  int      `yaml:"skill_threshold"`   // Experiences before creating skill
	PatternMinOccur int      `yaml:"pattern_min_occur"` // Occurrences before pattern
	LearnFromErrors bool     `yaml:"learn_from_errors"` // Learn from failures too
}

// DefaultConfig returns default learning config
func DefaultConfig() *Config {
	return &Config{
		Enabled:         true,
		AutoLearn:       true,
		MinConfidence:   0.7,
		MaxExperiences:  1000,
		SkillThreshold:  5,
		PatternMinOccur: 3,
		LearnFromErrors: true,
	}
}

// NewManager creates a new learning manager
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir:          dataDir,
		experiences:      make([]Experience, 0),
		skills:           make(map[string]*LearnedSkill),
		patterns:         make(map[string]*Pattern),
		autoLearnEnabled: true,
		minConfidence:    0.7,
		maxExperiences:   1000,
	}
}

// RecordExperience records a new experience
func (m *Manager) RecordExperience(exp Experience) error {
	exp.ID = fmt.Sprintf("exp_%d_%s", time.Now().UnixNano(), randomID(8))
	exp.Timestamp = time.Now().Unix()
	
	m.experiences = append(m.experiences, exp)
	
	// Trim old experiences
	if len(m.experiences) > m.maxExperiences {
		m.experiences = m.experiences[len(m.experiences)-m.maxExperiences:]
	}
	
	// Extract patterns
	if exp.Success {
		m.extractPatterns(exp)
	}
	
	// Check if we should create a skill
	if m.autoLearnEnabled && len(m.experiences) >= m.skillThreshold {
		m.checkForNewSkill()
	}
	
	return m.Save()
}

// extractPatterns extracts patterns from successful experiences
func (m *Manager) extractPatterns(exp Experience) {
	// Extract task patterns
	taskPatterns := extractTaskPatterns(exp.Task)
	
	for _, pattern := range taskPatterns {
		if p, ok := m.patterns[pattern]; ok {
			p.Occurrences++
			p.LastSeen = time.Now().Unix()
			p.Confidence = float64(p.Occurrences) / float64(p.Occurrences+10)
		} else {
			m.patterns[pattern] = &Pattern{
				ID:          fmt.Sprintf("pat_%d", len(m.patterns)+1),
				Name:        pattern,
				Description: fmt.Sprintf("Pattern observed in: %s", exp.Task),
				Regex:       pattern,
				Confidence:  0.1,
				Occurrences: 1,
				Actions:     []string{exp.Action},
				CreatedAt:   time.Now().Unix(),
				LastSeen:    time.Now().Unix(),
			}
		}
	}
}

// checkForNewSkill checks if a new skill should be created
func (m *Manager) checkForNewSkill() {
	// Group recent experiences by pattern
	patternGroups := make(map[string][]Experience)
	
	for i := len(m.experiences) - 1; i >= 0 && i >= len(m.experiences)-m.skillThreshold; i-- {
		exp := m.experiences[i]
		if !exp.Success {
			continue
		}
		
		for _, pattern := range m.extractTaskPatterns(exp.Task) {
			patternGroups[pattern] = append(patternGroups[pattern], exp)
		}
	}
	
	// Check each pattern group
	for pattern, exps := range patternGroups {
		if len(exps) >= m.skillThreshold {
			// Check if skill already exists
			skillName := normalizeSkillName(pattern)
			if _, exists := m.skills[skillName]; !exists {
				m.createSkillFromExperiences(skillName, pattern, exps)
			}
		}
	}
}

// extractTaskPatterns extracts patterns from a task description
func extractTaskPatterns(task string) []string {
	var patterns []string
	
	// Extract key phrases
	words := strings.Fields(strings.ToLower(task))
	
	// Common action patterns
	actionWords := []string{"create", "build", "fix", "update", "delete", "get", "list", "search", "generate", "process"}
	
	for _, word := range words {
		for _, action := range actionWords {
			if strings.HasPrefix(word, action) {
				patterns = append(patterns, word)
			}
		}
	}
	
	// Extract tool mentions
	toolRe := regexp.MustCompile(`(\w+)_(\w+)`)
	for _, match := range toolRe.FindAllStringSubmatch(task, -1) {
		if len(match) > 1 {
			patterns = append(patterns, match[1])
		}
	}
	
	return patterns
}

// normalizeSkillName normalizes a skill name
func normalizeSkillName(pattern string) string {
	name := strings.ToLower(pattern)
	name = strings.ReplaceAll(name, " ", "_")
	name = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(name, "")
	return name
}

// createSkillFromExperiences creates a new skill from experiences
func (m *Manager) createSkillFromExperiences(name, description string, exps []Experience) error {
	skill := &LearnedSkill{
		ID:          fmt.Sprintf("learned_%d", time.Now().UnixNano()),
		Name:        name,
		Description: fmt.Sprintf("Auto-generated skill for: %s", description),
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
		UseCount:    0,
		Version:     1,
		Tags:        []string{"auto-generated", "learned"},
	}
	
	// Build skill content from experiences
	skill.Content = m.buildSkillContent(name, description, exps)
	
	// Extract examples
	for _, exp := range exps[:min(3, len(exps))] {
		skill.Examples = append(skill.Examples, exp.Task)
	}
	
	// Calculate success rate
	successCount := 0
	for _, exp := range exps {
		if exp.Success {
			successCount++
		}
	}
	skill.SuccessRate = float64(successCount) / float64(len(exps))
	
	// Set triggers
	for _, exp := range exps {
		skill.Triggers = append(skill.Triggers, m.extractTaskPatterns(exp.Task)...)
	}
	
	m.skills[name] = skill
	return m.Save()
}

// buildSkillContent builds skill content from experiences
func (m *Manager) buildSkillContent(name, description string, exps []Experience) string {
	var builder strings.Builder
	
	builder.WriteString(fmt.Sprintf("# %s\n\n", name))
	builder.WriteString(fmt.Sprintf("> Auto-generated skill based on %d successful experiences\n\n", len(exps)))
	builder.WriteString(fmt.Sprintf("## Purpose\n%s\n\n", description))
	
	builder.WriteString("## When to Use\n")
	builder.WriteString("- This skill is triggered when:\n")
	seen := make(map[string]bool)
	for _, exp := range exps {
		for _, trigger := range m.extractTaskPatterns(exp.Task) {
			if !seen[trigger] {
				builder.WriteString(fmt.Sprintf("  - Task involves: %s\n", trigger))
				seen[trigger] = true
			}
		}
	}
	builder.WriteString("\n")
	
	builder.WriteString("## How It Works\n")
	builder.WriteString("Based on learned patterns:\n")
	for _, exp := range exps[:min(3, len(exps))] {
		builder.WriteString(fmt.Sprintf("1. %s\n", exp.Action))
	}
	builder.WriteString("\n")
	
	builder.WriteString("## Best Practices\n")
	builder.WriteString("- Always verify results after execution\n")
	builder.WriteString("- Use appropriate tools based on the task\n")
	builder.WriteString("- Handle errors gracefully\n")
	
	return builder.String()
}

// ImproveSkill improves a skill based on usage
func (m *Manager) ImproveSkill(skillID string, feedback Feedback) error {
	skill, ok := m.skills[skillID]
	if !ok {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	
	skill.Version++
	skill.UpdatedAt = time.Now().Unix()
	
	if feedback.Success {
		skill.SuccessRate = (skill.SuccessRate*float64(skill.UseCount) + 1.0) / float64(skill.UseCount+1)
		skill.UseCount++
		
		// Add positive improvements
		if feedback.Improvements != "" {
			skill.Content += fmt.Sprintf("\n\n## Improvement v%d\n%s\n", skill.Version, feedback.Improvements)
		}
	} else {
		// Learn from failure
		skill.Content += fmt.Sprintf("\n\n## Fix v%d\n%s\n", skill.Version, feedback.Error)
		skill.SuccessRate = skill.SuccessRate * 0.9 // Decrease success rate
	}
	
	return m.Save()
}

// Feedback represents skill usage feedback
type Feedback struct {
	Success      bool
	Error        string
	Improvements string
}

// GetSkill returns a skill by name
func (m *Manager) GetSkill(name string) *LearnedSkill {
	return m.skills[name]
}

// ListSkills returns all learned skills
func (m *Manager) ListSkills() []*LearnedSkill {
	skills := make([]*LearnedSkill, 0, len(m.skills))
	for _, s := range m.skills {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].UseCount > j.UseCount
	})
	return skills
}

// GetPatterns returns patterns with confidence above threshold
func (m *Manager) GetPatterns(minConfidence float64) []*Pattern {
	var result []*Pattern
	for _, p := range m.patterns {
		if p.Confidence >= minConfidence {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Confidence > j.Confidence
	})
	return result
}

// GetRecommendations returns skill recommendations based on task
func (m *Manager) GetRecommendations(task string) []string {
	var recommendations []string
	
	taskPatterns := extractTaskPatterns(task)
	
	for _, pattern := range taskPatterns {
		// Check patterns first
		if p, ok := m.patterns[pattern]; ok && p.Confidence >= m.minConfidence {
			recommendations = append(recommendations, p.Actions...)
		}
		
		// Check skills
		for _, skill := range m.skills {
			for _, trigger := range skill.Triggers {
				if strings.Contains(pattern, trigger) {
					recommendations = append(recommendations, skill.Name)
				}
			}
		}
	}
	
	return recommendations
}

// Save saves the learning state
func (m *Manager) Save() error {
	// Save experiences
	expPath := filepath.Join(m.dataDir, "experiences.json")
	data, err := json.MarshalIndent(m.experiences, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(expPath, data, 0644); err != nil {
		return err
	}
	
	// Save skills
	skillPath := filepath.Join(m.dataDir, "learned_skills.json")
	data, err = json.MarshalIndent(m.skills, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(skillPath, data, 0644)
}

// Load loads the learning state
func (m *Manager) Load() error {
	// Load experiences
	expPath := filepath.Join(m.dataDir, "experiences.json")
	if data, err := os.ReadFile(expPath); err == nil {
		json.Unmarshal(data, &m.experiences)
	}
	
	// Load skills
	skillPath := filepath.Join(m.dataDir, "learned_skills.json")
	if data, err := os.ReadFile(skillPath); err == nil {
		json.Unmarshal(data, &m.skills)
	}
	
	return nil
}

// Stats returns learning statistics
type Stats struct {
	TotalExperiences int            `json:"total_experiences"`
	SuccessfulExp    int            `json:"successful_experiences"`
	LearnedSkills    int            `json:"learned_skills"`
	PatternsFound    int            `json:"patterns_found"`
	TopPatterns      []*Pattern     `json:"top_patterns"`
	TopSkills        []*LearnedSkill `json:"top_skills"`
}

// GetStats returns learning statistics
func (m *Manager) GetStats() *Stats {
	stats := &Stats{
		TotalExperiences: len(m.experiences),
		LearnedSkills:    len(m.skills),
		PatternsFound:    len(m.patterns),
	}
	
	for _, exp := range m.experiences {
		if exp.Success {
			stats.SuccessfulExp++
		}
	}
	
	stats.TopPatterns = m.GetPatterns(0.5)[:min(5, len(m.GetPatterns(0.5)))]
	stats.TopSkills = m.ListSkills()[:min(5, len(m.ListSkills()))]
	
	return stats
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func randomID(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[time.Now().UnixNano()%int64(len(chars))]
	}
	return string(result)
}
