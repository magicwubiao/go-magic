package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// AutoCreatorConfig holds configuration for auto skill creation
type AutoCreatorConfig struct {
	// Minimum tool calls to trigger skill creation
	MinToolCalls int
	// Directory to save auto-created skills
	AutoDir string
	// Whether to auto-create skills
	Enabled bool
	// LLM provider for generating skill content
	Provider provider.Provider
}

// DefaultAutoCreatorConfig returns default configuration
func DefaultAutoCreatorConfig() *AutoCreatorConfig {
	home, _ := os.UserHomeDir()
	return &AutoCreatorConfig{
		MinToolCalls: 5,
		AutoDir:      filepath.Join(home, ".magic", "skills", "auto"),
		Enabled:      true,
	}
}

// AutoCreator automatically creates skills from complex tasks
type AutoCreator struct {
	config        *AutoCreatorConfig
	manager       *Manager
	toolCallCount int
	currentTask   *TaskContext
}

// TaskContext holds context for the current task being analyzed
type TaskContext struct {
	UserInput     string
	ToolCalls     []ToolCallInfo
	ToolResults   []string
	FinalResponse string
	StartedAt     time.Time
}

// ToolCallInfo holds information about a tool call
type ToolCallInfo struct {
	Name      string
	Arguments map[string]interface{}
	Result    string
	Success   bool
}

// NewAutoCreator creates a new auto skill creator
func NewAutoCreator(mgr *Manager, cfg *AutoCreatorConfig) *AutoCreator {
	if cfg == nil {
		cfg = DefaultAutoCreatorConfig()
	}

	// Ensure auto directory exists
	os.MkdirAll(cfg.AutoDir, 0755)

	return &AutoCreator{
		config:        cfg,
		manager:       mgr,
		toolCallCount: 0,
		currentTask:   nil,
	}
}

// StartTask marks the start of a new task
func (ac *AutoCreator) StartTask(userInput string) {
	ac.currentTask = &TaskContext{
		UserInput:   userInput,
		ToolCalls:   make([]ToolCallInfo, 0),
		ToolResults: make([]string, 0),
		StartedAt:   time.Now(),
	}
	ac.toolCallCount = 0
}

// RecordToolCall records a tool call
func (ac *AutoCreator) RecordToolCall(name string, args map[string]interface{}, result string, success bool) {
	if ac.currentTask == nil {
		return
	}
	ac.toolCallCount++
	ac.currentTask.ToolCalls = append(ac.currentTask.ToolCalls, ToolCallInfo{
		Name:      name,
		Arguments: args,
		Result:    result,
		Success:   success,
	})
	ac.currentTask.ToolResults = append(ac.currentTask.ToolResults, result)
}

// ShouldCreateSkill returns true if a skill should be created
func (ac *AutoCreator) ShouldCreateSkill() bool {
	if !ac.config.Enabled {
		return false
	}
	return ac.toolCallCount >= ac.config.MinToolCalls && ac.currentTask != nil
}

// CreateSkill generates a skill from the current task context
func (ac *AutoCreator) CreateSkill() (*Skill, error) {
	if ac.currentTask == nil {
		return nil, fmt.Errorf("no active task")
	}

	// Generate skill name from task
	skillName := ac.generateSkillName(ac.currentTask.UserInput)

	// Generate skill description
	description := ac.generateDescription()

	// Generate skill content using template
	content := ac.generateSkillContent(skillName, description)

	// Extract tools used
	tools := ac.extractTools()

	skill := &Skill{
		SkillMeta: SkillMeta{
			Name:        skillName,
			Description: description,
			Version:     "1.0.0",
			Author:      "magic Auto-Creator",
			Tags:        ac.generateTags(),
		},
		Tools:       tools,
		Content:     content,
		Metadata: map[string]interface{}{
			"auto_created": true,
			"tool_count":   ac.toolCallCount,
			"created_at":   time.Now().Format(time.RFC3339),
		},
	}

	// Save the skill
	if err := ac.saveSkill(skill); err != nil {
		return nil, fmt.Errorf("failed to save skill: %w", err)
	}

	// Register with manager
	ac.manager.skills[skill.Name] = skill

	return skill, nil
}

// generateSkillName generates a skill name from task context
func (ac *AutoCreator) generateSkillName(userInput string) string {
	// Extract key concepts from user input
	words := strings.Fields(strings.ToLower(userInput))
	keywords := make([]string, 0)
	for _, word := range words {
		// Filter out common words
		word = strings.Trim(word, ".,!?;:\"'()[]{}")
		if len(word) > 3 && !isCommonWord(word) {
			keywords = append(keywords, word)
		}
	}

	// Build name from keywords
	var name strings.Builder
	if len(keywords) >= 2 {
		name.WriteString(strings.Title(keywords[0]))
		name.WriteString(strings.Title(keywords[1]))
	} else if len(keywords) == 1 {
		name.WriteString(strings.Title(keywords[0]))
	} else {
		name.WriteString("CustomSkill")
	}

	// Add timestamp suffix to ensure uniqueness
	name.WriteString(fmt.Sprintf("%d", time.Now().Unix()%10000))

	return name.String()
}

// isCommonWord checks if a word is a common word
func isCommonWord(word string) bool {
	common := map[string]bool{
		"help": true, "how": true, "what": true, "can": true, "you": true,
		"the": true, "and": true, "for": true, "with": true, "please": true,
		"need": true, "want": true, "have": true, "this": true, "that": true,
	}
	return common[word]
}

// generateDescription generates a skill description
func (ac *AutoCreator) generateDescription() string {
	var desc strings.Builder
	desc.WriteString("Auto-generated skill for: ")

	if ac.currentTask != nil {
		// Summarize the task
		input := ac.currentTask.UserInput
		if len(input) > 100 {
			input = input[:100] + "..."
		}
		desc.WriteString(input)
	}

	return desc.String()
}

// generateTags generates tags for the skill
func (ac *AutoCreator) generateTags() []string {
	tags := []string{"auto-created"}

	if ac.currentTask == nil {
		return tags
	}

	// Add tags based on tools used
	toolSet := make(map[string]bool)
	for _, tc := range ac.currentTask.ToolCalls {
		toolSet[tc.Name] = true
	}

	// Map tools to tags
	toolTags := map[string]string{
		"read_file":       "file-ops",
		"write_file":      "file-ops",
		"execute_command": "shell",
		"web_search":      "web",
		"python":          "code",
	}

	for tool, tag := range toolTags {
		if toolSet[tool] {
			tags = append(tags, tag)
		}
	}

	return tags
}

// extractTools extracts unique tool names used
func (ac *AutoCreator) extractTools() []string {
	if ac.currentTask == nil {
		return nil
	}

	seen := make(map[string]bool)
	var tools []string
	for _, tc := range ac.currentTask.ToolCalls {
		if !seen[tc.Name] {
			seen[tc.Name] = true
			tools = append(tools, tc.Name)
		}
	}
	return tools
}

// generateSkillContent generates the SKILL.md content
func (ac *AutoCreator) generateSkillContent(name, description string) string {
	var sb strings.Builder

	sb.WriteString("# " + name + "\n\n")
	sb.WriteString("## Description\n\n")
	sb.WriteString(description + "\n\n")

	sb.WriteString("## When to Use\n\n")
	sb.WriteString("Use this skill when:\n")

	// Generate usage scenarios from tool calls
	if ac.currentTask != nil {
		for i, tc := range ac.currentTask.ToolCalls[:min(3, len(ac.currentTask.ToolCalls))] {
			sb.WriteString(fmt.Sprintf("- Task involves %s operation\n", tc.Name))
			_ = i // suppress unused variable warning
		}
	}

	sb.WriteString("\n## Steps\n\n")
	sb.WriteString("1. Analyze the user's request\n")
	sb.WriteString("2. Identify required operations\n")
	sb.WriteString("3. Execute operations in appropriate order\n")
	sb.WriteString("4. Return results to user\n")

	sb.WriteString("\n## Tools Used\n\n")
	if tools := ac.extractTools(); len(tools) > 0 {
		for _, tool := range tools {
			sb.WriteString(fmt.Sprintf("- `%s`\n", tool))
		}
	} else {
		sb.WriteString("- (No tools used)\n")
	}

	sb.WriteString("\n## Notes\n\n")
	sb.WriteString("- Auto-generated skill\n")
	sb.WriteString(fmt.Sprintf("- Created at: %s\n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- Based on %d tool calls\n", ac.toolCallCount))

	return sb.String()
}

// saveSkill saves a skill to disk
func (ac *AutoCreator) saveSkill(skill *Skill) error {
	// Create skill directory
	skillDir := filepath.Join(ac.config.AutoDir, skill.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}

	// Save SKILL.md
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(skill.Content), 0644); err != nil {
		return err
	}

	// Save metadata JSON
	metadataPath := filepath.Join(skillDir, "metadata.json")
	metadata := map[string]interface{}{
		"name":        skill.Name,
		"description": skill.Description,
		"version":     skill.Version,
		"author":      skill.Author,
		"tags":        skill.Tags,
		"tools":       skill.Tools,
		"source":      skill.Source,
		"created_at":  time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(metadataPath, data, 0644)
}

// Reset clears the current task context
func (ac *AutoCreator) Reset() {
	ac.currentTask = nil
	ac.toolCallCount = 0
}

// GetToolCallCount returns the number of tool calls in current task
func (ac *AutoCreator) GetToolCallCount() int {
	return ac.toolCallCount
}

// IsActive returns true if there's an active task
func (ac *AutoCreator) IsActive() bool {
	return ac.currentTask != nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
