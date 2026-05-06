package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SkillVersion represents a version of a skill
type SkillVersion struct {
	Version     string    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	Changes     string    `json:"changes"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Performance *PerformanceMetrics    `json:"performance,omitempty"`
}

// PerformanceMetrics tracks skill performance
type PerformanceMetrics struct {
	TotalUses     int         `json:"total_uses"`
	SuccessCount  int         `json:"success_count"`
	FailureCount  int         `json:"failure_count"`
	AvgDuration   time.Duration `json:"avg_duration"`
	LastUsed      time.Time   `json:"last_used"`
	SuccessRate   float64     `json:"success_rate"`
	AvgUserRating float64     `json:"avg_user_rating"`
}

// SkillHealth represents the health status of a skill
type SkillHealth struct {
	Status       string  `json:"status"` // "healthy", "degraded", "deprecated"
	Score        float64 `json:"score"` // 0-100
	Issues       []string `json:"issues"`
	Recommendations []string `json:"recommendations"`
}

// EnhancedSkillRecord extends the base skill with version tracking
type EnhancedSkillRecord struct {
	SkillID       string          `json:"skill_id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Author        string          `json:"author"`
	Level         string          `json:"level"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Versions      []SkillVersion  `json:"versions"`
	CurrentVersion string         `json:"current_version"`
	Performance   *PerformanceMetrics `json:"performance"`
	Health        *SkillHealth    `json:"health"`
	Tags          []string        `json:"tags"`
	Dependents    []string        `json:"dependents"` // Skills that depend on this
	Deprecated    bool            `json:"deprecated"`
	DeprecationReason string      `json:"deprecation_reason,omitempty"`
}

// EnhancedAutoCreator provides advanced skill self-evolution
type EnhancedAutoCreator struct {
	baseDir      string
	patterns     []Pattern
	skillCount   int
	minFrequency int
	mu           sync.RWMutex
	
	// Enhanced tracking
	skillRegistry map[string]*EnhancedSkillRecord
	paramHistory  map[string][]ParamSnapshot
	mergeCandidates []MergeCandidate
}

// ParamSnapshot captures parameter usage at a point in time
type ParamSnapshot struct {
	Timestamp   time.Time
	SkillID     string
	Parameters  map[string]interface{}
	Success     bool
	Duration    time.Duration
}

// MergeCandidate represents a potential skill merge
type MergeCandidate struct {
	Skill1      string   `json:"skill_1"`
	Skill2      string   `json:"skill_2"`
	Similarity  float64  `json:"similarity"`
	CommonTools []string `json:"common_tools"`
	Reasons     []string `json:"reasons"`
}

// NewEnhancedAutoCreator creates an enhanced auto-creator with self-evolution
func NewEnhancedAutoCreator(baseDir string) *EnhancedAutoCreator {
	skillsDir := filepath.Join(baseDir, "auto_skills")
	os.MkdirAll(skillsDir, 0755)

	creator := &EnhancedAutoCreator{
		baseDir:       skillsDir,
		patterns:      make([]Pattern, 0),
		minFrequency:  2,
		skillRegistry: make(map[string]*EnhancedSkillRecord),
		paramHistory:  make(map[string][]ParamSnapshot),
	}
	
	// Load existing skills
	creator.loadSkillRegistry()
	
	return creator
}

// loadSkillRegistry loads all existing skills into the registry
func (e *EnhancedAutoCreator) loadSkillRegistry() {
	entries, err := os.ReadDir(e.baseDir)
	if err != nil {
		return
	}
	
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		
		skillDir := filepath.Join(e.baseDir, entry.Name())
		metaPath := filepath.Join(skillDir, "meta.json")
		
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		
		var record EnhancedSkillRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		
		e.skillRegistry[record.SkillID] = &record
	}
}

// RecordSkillUsage records usage metrics for a skill
func (e *EnhancedAutoCreator) RecordSkillUsage(skillID string, params map[string]interface{}, success bool, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	record, exists := e.skillRegistry[skillID]
	if !exists {
		return
	}
	
	// Update performance metrics
	if record.Performance == nil {
		record.Performance = &PerformanceMetrics{}
	}
	
	record.Performance.TotalUses++
	record.Performance.LastUsed = time.Now()
	
	if success {
		record.Performance.SuccessCount++
	} else {
		record.Performance.FailureCount++
	}
	
	// Calculate success rate
	total := record.Performance.SuccessCount + record.Performance.FailureCount
	if total > 0 {
		record.Performance.SuccessRate = float64(record.Performance.SuccessCount) / float64(total)
	}
	
	// Update average duration
	if record.Performance.TotalUses == 1 {
		record.Performance.AvgDuration = duration
	} else {
		record.Performance.AvgDuration = (record.Performance.AvgDuration*(time.Duration(record.Performance.TotalUses-1)) + duration) / time.Duration(record.Performance.TotalUses)
	}
	
	// Record parameter history for optimization
	e.recordParamSnapshot(skillID, params, success, duration)
	
	// Update health score
	record.Health = e.calculateSkillHealth(record)
	
	// Save updated record
	e.saveSkillRecord(record)
}

// recordParamSnapshot records parameter usage for optimization
func (e *EnhancedAutoCreator) recordParamSnapshot(skillID string, params map[string]interface{}, success bool, duration time.Duration) {
	snapshot := ParamSnapshot{
		Timestamp:  time.Now(),
		SkillID:    skillID,
		Parameters: params,
		Success:    success,
		Duration:   duration,
	}
	
	e.paramHistory[skillID] = append(e.paramHistory[skillID], snapshot)
	
	// Keep only recent history
	if len(e.paramHistory[skillID]) > 100 {
		e.paramHistory[skillID] = e.paramHistory[skillID][len(e.paramHistory[skillID])-100:]
	}
}

// calculateSkillHealth calculates the health score for a skill
func (e *EnhancedAutoCreator) calculateSkillHealth(record *EnhancedSkillRecord) *SkillHealth {
	health := &SkillHealth{
		Issues:          make([]string, 0),
		Recommendations: make([]string, 0),
	}
	
	score := 100.0
	
	// Check usage frequency
	if record.Performance != nil {
		if record.Performance.TotalUses == 0 {
			health.Issues = append(health.Issues, "Skill has never been used")
			score -= 30
		} else if time.Since(record.Performance.LastUsed) > 30*24*time.Hour {
			health.Issues = append(health.Issues, "Skill not used in over 30 days")
			score -= 20
		}
		
		// Check success rate
		if record.Performance.SuccessRate < 0.5 {
			health.Issues = append(health.Issues, fmt.Sprintf("Low success rate: %.0f%%", record.Performance.SuccessRate*100))
			score -= 30
			health.Recommendations = append(health.Recommendations, "Review error patterns and improve skill logic")
		} else if record.Performance.SuccessRate < 0.8 {
			health.Issues = append(health.Issues, fmt.Sprintf("Success rate could be improved: %.0f%%", record.Performance.SuccessRate*100))
			score -= 10
		}
	}
	
	// Check deprecation status
	if record.Deprecated {
		health.Issues = append(health.Issues, "Skill is deprecated")
		score -= 40
		health.Recommendations = append(health.Recommendations, "Consider migrating to a newer skill")
	}
	
	// Check for missing versions
	if len(record.Versions) == 0 {
		health.Issues = append(health.Issues, "No version history")
		score -= 5
	}
	
	// Determine status
	health.Score = score
	if score >= 80 {
		health.Status = "healthy"
	} else if score >= 50 {
		health.Status = "degraded"
		health.Recommendations = append(health.Recommendations, "Skill needs attention")
	} else {
		health.Status = "deprecated"
	}
	
	return health
}

// OptimizeParameters analyzes parameter history and suggests optimizations
func (e *EnhancedAutoCreator) OptimizeParameters(skillID string) map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	history, exists := e.paramHistory[skillID]
	if !exists || len(history) < 5 {
		return nil
	}
	
	// Analyze successful vs failed parameter combinations
	successParams := make([]map[string]interface{}, 0)
	failedParams := make([]map[string]interface{}, 0)
	
	for _, snapshot := range history {
		if snapshot.Success {
			successParams = append(successParams, snapshot.Parameters)
		} else {
			failedParams = append(failedParams, snapshot.Parameters)
		}
	}
	
	// Find common successful patterns
	optimized := make(map[string]interface{})
	
	// Simple analysis: find parameters that correlate with success
	for key := range successParams[0] {
		successVals := make([]interface{}, 0)
		for _, params := range successParams {
			if val, ok := params[key]; ok {
				successVals = append(successVals, val)
			}
		}
		
		if len(successVals) > 0 {
			// Most common value
			optimized[key] = findMostCommon(successVals)
		}
	}
	
	return optimized
}

// findMostCommon finds the most common value in a slice
func findMostCommon(values []interface{}) interface{} {
	if len(values) == 0 {
		return nil
	}
	
	count := make(map[interface{}]int)
	for _, v := range values {
		count[v]++
	}
	
	var mostCommon interface{}
	maxCount := 0
	for v, c := range count {
		if c > maxCount {
			maxCount = c
			mostCommon = v
		}
	}
	
	return mostCommon
}

// CreateNewVersion creates a new version of an existing skill
func (e *EnhancedAutoCreator) CreateNewVersion(skillID string, changes string, newParams map[string]interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	record, exists := e.skillRegistry[skillID]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	
	// Generate new version number
	newVersion := fmt.Sprintf("v%d.%d", 
		len(record.Versions)+1, 0)
	
	version := SkillVersion{
		Version:    newVersion,
		CreatedAt:  time.Now(),
		Changes:    changes,
		Parameters: newParams,
	}
	
	if record.Performance != nil {
		version.Performance = &PerformanceMetrics{
			TotalUses:    record.Performance.TotalUses,
			SuccessCount: record.Performance.SuccessCount,
			SuccessRate:  record.Performance.SuccessRate,
		}
	}
	
	record.Versions = append(record.Versions, version)
	record.CurrentVersion = newVersion
	record.UpdatedAt = time.Now()
	
	// Update skill file with new version
	e.saveSkillRecord(record)
	e.updateSkillFile(record)
	
	return nil
}

// updateSkillFile updates the SKILL.md file with version info
func (e *EnhancedAutoCreator) updateSkillFile(record *EnhancedSkillRecord) {
	skillDir := filepath.Join(e.baseDir, record.SkillID)
	skillPath := filepath.Join(skillDir, "SKILL.md")
	
	// Read existing content
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return
	}
	
	content := string(data)
	
	// Add version info section
	versionSection := fmt.Sprintf(`
---

## Version History (%s)

### Current Version: %s

`, time.Now().Format("2006-01-02"), record.CurrentVersion)
	
	// Add recent changes
	for i := len(record.Versions) - 1; i >= 0 && i > len(record.Versions)-4; i-- {
		v := record.Versions[i]
		versionSection += fmt.Sprintf("#### %s (%s)\n- %s\n", v.Version, v.CreatedAt.Format("Jan 2, 2006"), v.Changes)
	}
	
	// Insert before footer
	if idx := strings.Index(content, "*This skill was auto-generated"); idx > 0 {
		content = content[:idx] + versionSection + content[idx:]
	}
	
	os.WriteFile(skillPath, []byte(content), 0644)
}

// saveSkillRecord saves skill metadata to JSON
func (e *EnhancedAutoCreator) saveSkillRecord(record *EnhancedSkillRecord) {
	skillDir := filepath.Join(e.baseDir, record.SkillID)
	os.MkdirAll(skillDir, 0755)
	
	metaPath := filepath.Join(skillDir, "meta.json")
	data, _ := json.MarshalIndent(record, "", "  ")
	os.WriteFile(metaPath, data, 0644)
}

// RollbackVersion rolls back a skill to a previous version
func (e *EnhancedAutoCreator) RollbackVersion(skillID string, targetVersion string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	record, exists := e.skillRegistry[skillID]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	
	// Find target version
	var version SkillVersion
	found := false
	for _, v := range record.Versions {
		if v.Version == targetVersion {
			version = v
			found = true
			break
		}
	}
	
	if !found {
		return fmt.Errorf("version not found: %s", targetVersion)
	}
	
	// Create rollback version
	rollbackVersion := fmt.Sprintf("rollback-%s", targetVersion)
	record.Versions = append(record.Versions, SkillVersion{
		Version:   rollbackVersion,
		CreatedAt: time.Now(),
		Changes:   fmt.Sprintf("Rolled back to %s", targetVersion),
	})
	record.CurrentVersion = rollbackVersion
	record.UpdatedAt = time.Now()
	
	// Note: In production, would also restore the skill file content from version storage
	e.saveSkillRecord(record)
	
	return nil
}

// DeprecateSkill marks a skill as deprecated
func (e *EnhancedAutoCreator) DeprecateSkill(skillID string, reason string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	record, exists := e.skillRegistry[skillID]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	
	record.Deprecated = true
	record.DeprecationReason = reason
	record.UpdatedAt = time.Now()
	
	e.saveSkillRecord(record)
	
	return nil
}

// FindMergeCandidates finds skills that could be merged
func (e *EnhancedAutoCreator) FindMergeCandidates() []MergeCandidate {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	candidates := make([]MergeCandidate, 0)
	skills := make([]*EnhancedSkillRecord, 0)
	
	for _, record := range e.skillRegistry {
		skills = append(skills, record)
	}
	
	// Compare all pairs
	for i := 0; i < len(skills); i++ {
		for j := i + 1; j < len(skills); j++ {
			candidate := e.evaluateMergeCandidate(skills[i], skills[j])
			if candidate != nil {
				candidates = append(candidates, *candidate)
			}
		}
	}
	
	// Sort by similarity
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Similarity > candidates[j].Similarity
	})
	
	e.mergeCandidates = candidates
	return candidates
}

// evaluateMergeCandidate evaluates if two skills should be merged
func (e *EnhancedAutoCreator) evaluateMergeCandidate(skill1, skill2 *EnhancedSkillRecord) *MergeCandidate {
	// Find common tools
	commonTools := findCommonElements(skill1.Pattern.Tools, skill2.Pattern.Tools)
	
	if len(commonTools) == 0 {
		return nil
	}
	
	// Calculate similarity
	similarity := float64(len(commonTools)) / float64(max(len(skill1.Pattern.Tools), len(skill2.Pattern.Tools)))
	
	if similarity < 0.5 {
		return nil
	}
	
	// Generate merge reasons
	reasons := make([]string, 0)
	if len(commonTools) >= 3 {
		reasons = append(reasons, fmt.Sprintf("Share %d common tools", len(commonTools)))
	}
	if skill1.Health != nil && skill1.Health.Status == "degraded" {
		reasons = append(reasons, fmt.Sprintf("%s is degraded", skill1.Name))
	}
	if skill2.Health != nil && skill2.Health.Status == "degraded" {
		reasons = append(reasons, fmt.Sprintf("%s is degraded", skill2.Name))
	}
	if similarity > 0.8 {
		reasons = append(reasons, "High overlap in functionality")
	}
	
	return &MergeCandidate{
		Skill1:      skill1.SkillID,
		Skill2:      skill2.SkillID,
		Similarity:  similarity,
		CommonTools: commonTools,
		Reasons:     reasons,
	}
}

// findCommonElements finds common elements between two string slices
func findCommonElements(a, b []string) []string {
	set := make(map[string]bool)
	for _, v := range a {
		set[v] = true
	}
	
	common := make([]string, 0)
	for _, v := range b {
		if set[v] {
			common = append(common, v)
		}
	}
	
	return common
}

// MergeSkills merges two skills into one
func (e *EnhancedAutoCreator) MergeSkills(candidate MergeCandidate, mergedName string) (*EnhancedSkillRecord, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	skill1, ok1 := e.skillRegistry[candidate.Skill1]
	skill2, ok2 := e.skillRegistry[candidate.Skill2]
	
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("one or both skills not found")
	}
	
	// Create merged skill
	merged := &EnhancedSkillRecord{
		SkillID:        fmt.Sprintf("merged-%s", time.Now().Format("20060102150405")),
		Name:           mergedName,
		Description:    fmt.Sprintf("Merged from %s and %s", skill1.Name, skill2.Name),
		Author:         "cortex-auto",
		Level:          "Level 2",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CurrentVersion: "v1.0",
		Performance: &PerformanceMetrics{
			TotalUses: skill1.Performance.TotalUses + skill2.Performance.TotalUses,
		},
		Tags:       append(skill1.Tags, skill2.Tags...),
		Dependents: append(skill1.Dependents, skill2.Dependents...),
	}
	
	// Generate merged skill content
	e.generateMergedSkillContent(skill1, skill2, merged, candidate.CommonTools)
	
	// Save merged skill
	e.skillRegistry[merged.SkillID] = merged
	e.saveSkillRecord(merged)
	
	// Mark originals as deprecated
	skill1.Deprecated = true
	skill1.DeprecationReason = fmt.Sprintf("Merged into %s", merged.SkillID)
	skill2.Deprecated = true
	skill2.DeprecationReason = fmt.Sprintf("Merged into %s", merged.SkillID)
	
	e.saveSkillRecord(skill1)
	e.saveSkillRecord(skill2)
	
	return merged, nil
}

// generateMergedSkillContent generates the skill content for merged skill
func (e *EnhancedAutoCreator) generateMergedSkillContent(skill1, skill2 *EnhancedSkillRecord, merged *EnhancedSkillRecord, commonTools []string) {
	skillDir := filepath.Join(e.baseDir, merged.SkillID)
	os.MkdirAll(skillDir, 0755)
	
	var content strings.Builder
	
	content.WriteString(fmt.Sprintf("# %s\n\n", merged.Name))
	content.WriteString(fmt.Sprintf("**Auto-generated by Cortex Agent** | Merged Skill\n\n"))
	content.WriteString(fmt.Sprintf("## What this skill does\n\n%s\n\n", merged.Description))
	
	content.WriteString("---\n\n## Merged From\n\n")
	content.WriteString(fmt.Sprintf("- %s\n", skill1.Name))
	content.WriteString(fmt.Sprintf("- %s\n\n", skill2.Name))
	
	content.WriteString("---\n\n## Common Tool Sequence\n\n")
	for i, tool := range commonTools {
		content.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, tool))
	}
	
	content.WriteString("\n### Original Patterns\n\n")
	content.WriteString("#### From " + skill1.Name + "\n")
	content.WriteString(fmt.Sprintf("Tools: %s\n\n", strings.Join(skill1.Pattern.Tools, " → ")))
	
	content.WriteString("#### From " + skill2.Name + "\n")
	content.WriteString(fmt.Sprintf("Tools: %s\n\n", strings.Join(skill2.Pattern.Tools, " → ")))
	
	content.WriteString("---\n\n")
	content.WriteString("*This skill was auto-generated by merging similar patterns.*\n")
	
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content.String()), 0644)
}

// GetSkillHealth returns the health status of all skills
func (e *EnhancedAutoCreator) GetSkillHealth() map[string]*SkillHealth {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	healthMap := make(map[string]*SkillHealth)
	for id, record := range e.skillRegistry {
		if record.Health == nil {
			record.Health = e.calculateSkillHealth(record)
		}
		healthMap[id] = record.Health
	}
	
	return healthMap
}

// GetStaleSkills returns skills that haven't been used recently
func (e *EnhancedAutoCreator) GetStaleSkills(daysSinceUse int) []*EnhancedSkillRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	stale := make([]*EnhancedSkillRecord, 0)
	cutoff := time.Now().AddDate(0, 0, -daysSinceUse)
	
	for _, record := range e.skillRegistry {
		if record.Performance != nil && record.Performance.LastUsed.Before(cutoff) {
			stale = append(stale, record)
		}
	}
	
	return stale
}

// GenerateOptimizationReport generates a report of optimization opportunities
func (e *EnhancedAutoCreator) GenerateOptimizationReport() string {
	var report strings.Builder
	
	report.WriteString("# Skill Optimization Report\n\n")
	report.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))
	
	// Health summary
	healthMap := e.GetSkillHealth()
	healthy, degraded, deprecated := 0, 0, 0
	
	for _, health := range healthMap {
		switch health.Status {
		case "healthy":
			healthy++
		case "degraded":
			degraded++
		case "deprecated":
			deprecated++
		}
	}
	
	report.WriteString("## Health Summary\n\n")
	report.WriteString(fmt.Sprintf("- Healthy: %d\n", healthy))
	report.WriteString(fmt.Sprintf("- Degraded: %d\n", degraded))
	report.WriteString(fmt.Sprintf("- Deprecated: %d\n\n", deprecated))
	
	// Merge candidates
	candidates := e.FindMergeCandidates()
	if len(candidates) > 0 {
		report.WriteString("## Merge Opportunities\n\n")
		for _, c := range candidates {
			report.WriteString(fmt.Sprintf("- **%s** and **%s**: %.0f%% overlap\n", c.Skill1, c.Skill2, c.Similarity*100))
			for _, reason := range c.Reasons {
				report.WriteString(fmt.Sprintf("  - %s\n", reason))
			}
		}
		report.WriteString("\n")
	}
	
	// Stale skills
	stale := e.GetStaleSkills(30)
	if len(stale) > 0 {
		report.WriteString("## Stale Skills (unused 30+ days)\n\n")
		for _, s := range stale {
			report.WriteString(fmt.Sprintf("- %s (%s)\n", s.Name, s.SkillID))
		}
		report.WriteString("\n")
	}
	
	// Parameter optimization suggestions
	report.WriteString("## Parameter Optimization Suggestions\n\n")
	for skillID := range e.paramHistory {
		opts := e.OptimizeParameters(skillID)
		if opts != nil && len(opts) > 0 {
			report.WriteString(fmt.Sprintf("### %s\n", skillID))
			for k, v := range opts {
				report.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
			}
		}
	}
	
	return report.String()
}

// AnalyzeToolSequence analyzes a sequence of tool calls for patterns
func (e *EnhancedAutoCreator) AnalyzeToolSequence(task string, tools []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if len(tools) < 3 {
		return
	}

	// Look for subsequences that repeat across tasks
	for i := 0; i <= len(tools)-3; i++ {
		subsequence := tools[i : i+3]
		patternKey := strings.Join(subsequence, " → ")
		found := false

		for pIdx := range e.patterns {
			existingKey := strings.Join(e.patterns[pIdx].ToolSequence, " → ")
			if existingKey == patternKey {
				e.patterns[pIdx].Frequency++
				e.patterns[pIdx].ExampleTasks = append(e.patterns[pIdx].ExampleTasks, task)
				
				// Update confidence based on frequency
				if e.patterns[pIdx].Frequency >= e.minFrequency {
					e.patterns[pIdx].Confidence = min(0.9, e.patterns[pIdx].Confidence+0.1)
				}
				found = true
				break
			}
		}

		if !found {
			e.patterns = append(e.patterns, Pattern{
				Name:         fmt.Sprintf("Pattern-%d", len(e.patterns)+1),
				Description:  fmt.Sprintf("Detected sequence: %s", patternKey),
				Tools:        subsequence,
				ToolSequence: subsequence,
				Frequency:    1,
				Confidence:   0.5,
				ExampleTasks: []string{task},
			})
		}
	}
}

// GenerateSkillFromPattern generates a skill file from a detected pattern
func (e *EnhancedAutoCreator) GenerateSkillFromPattern(pattern Pattern) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	skillID := fmt.Sprintf("auto-%s-%d", pattern.Name, time.Now().Unix())
	skillDir := filepath.Join(e.baseDir, skillID)
	os.MkdirAll(skillDir, 0755)

	// Generate skill metadata with enhanced tracking
	record := &EnhancedSkillRecord{
		SkillID:        skillID,
		Name:           fmt.Sprintf("Automated %s", pattern.Name),
		Description:    pattern.Description,
		Author:         "cortex-auto",
		Level:          "Level 1",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CurrentVersion: "v1.0",
		Performance: &PerformanceMetrics{},
		Tags:          []string{"auto-generated"},
		Pattern:       &pattern,
	}

	metaJSON, _ := json.MarshalIndent(record, "", "  ")
	os.WriteFile(filepath.Join(skillDir, "meta.json"), metaJSON, 0644)

	// Generate SKILL.md
	skillMD := e.generateSkillMarkdown(pattern, record)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644)

	e.skillCount++
	e.skillRegistry[skillID] = record

	return nil
}

// generateSkillMarkdown generates progressive disclosure markdown
func (e *EnhancedAutoCreator) generateSkillMarkdown(pattern Pattern, record *EnhancedSkillRecord) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", record.Name))
	sb.WriteString(fmt.Sprintf("**Auto-generated by Cortex Agent** | Pattern seen %d times\n\n", pattern.Frequency))
	sb.WriteString(fmt.Sprintf("## What this skill does\n\n%s\n\n", record.Description))

	sb.WriteString("---\n\n## Level 1: Complete Usage Guide\n\n")
	sb.WriteString("### Tool Sequence\n\n")
	sb.WriteString("This skill uses the following tool sequence:\n\n")
	for i, tool := range pattern.ToolSequence {
		sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, tool))
	}

	sb.WriteString("\n### How to Use\n\n")
	sb.WriteString("When you need to perform this task:\n")
	sb.WriteString("1. Call the first tool with appropriate parameters\n")
	sb.WriteString("2. Use results from previous step as input to next tool\n")
	sb.WriteString("3. Continue until the sequence is complete\n\n")

	sb.WriteString("### Example Tasks\n\n")
	for _, example := range pattern.ExampleTasks {
		sb.WriteString(fmt.Sprintf("- %s\n", example))
	}

	sb.WriteString("\n---\n\n## Level 2: Reference and Tips\n\n")
	sb.WriteString("### Common Pitfalls\n\n")
	sb.WriteString("- Make sure each tool call completes successfully before proceeding\n")
	sb.WriteString("- Verify intermediate results before moving to complex operations\n")
	sb.WriteString("- If a step fails, consider alternative approaches or ask for clarification\n\n")

	sb.WriteString("### Optimization Tips\n\n")
	sb.WriteString("- Consider parallel execution for independent sub-tasks\n")
	sb.WriteString("- Cache intermediate results when appropriate\n")
	sb.WriteString("- Document any deviations from the standard pattern\n\n")

	sb.WriteString("\n---\n\n")
	sb.WriteString("*This skill was auto-generated by Cortex Agent's pattern recognition system.*\n")
	sb.WriteString("*It will be improved and refined as the pattern is used more frequently.*\n")

	return sb.String()
}

// GetPatterns returns all detected patterns
func (e *EnhancedAutoCreator) GetPatterns() []Pattern {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.patterns
}

// GetGeneratedSkills returns all auto-generated skills
func (e *EnhancedAutoCreator) GetGeneratedSkills() []*EnhancedSkillRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	skills := make([]*EnhancedSkillRecord, 0, len(e.skillRegistry))
	for _, record := range e.skillRegistry {
		skills = append(skills, record)
	}
	
	return skills
}

// GetSkillRecord returns a specific skill record
func (e *EnhancedAutoCreator) GetSkillRecord(skillID string) (*EnhancedSkillRecord, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	record, exists := e.skillRegistry[skillID]
	return record, exists
}

// ClearPatterns clears all detected patterns
func (e *EnhancedAutoCreator) ClearPatterns() {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	e.patterns = make([]Pattern, 0)
}

// ExportSkillAnalytics exports analytics data for external analysis
func (e *EnhancedAutoCreator) ExportSkillAnalytics() ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	analytics := map[string]interface{}{
		"exported_at":        time.Now(),
		"total_skills":        len(e.skillRegistry),
		"total_patterns":       len(e.patterns),
		"skills":              e.skillRegistry,
		"merge_candidates":    e.mergeCandidates,
		"health_summary":      e.GetSkillHealth(),
	}
	
	return json.MarshalIndent(analytics, "", "  ")
}
