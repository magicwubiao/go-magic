package skills

import (
	"fmt"

	"github.com/magicwubiao/go-magic/internal/plugin"
)

// PluginAdapter adapts a Skill to the Plugin interface
type PluginAdapter struct {
	skill Skill
}

// NewPluginAdapter creates a new skill plugin adapter
func NewPluginAdapter(skill *Skill) *PluginAdapter {
	return &PluginAdapter{
		skill: *skill,
	}
}

// Manifest returns the plugin manifest
func (a *PluginAdapter) Manifest() *plugin.PluginManifest {
	return &plugin.PluginManifest{
		ID:            sanitizeID(a.skill.Name),
		Name:          a.skill.Name,
		Version:       a.skill.Version,
		Description:   a.skill.Description,
		Author:        a.skill.Author,
		License:       a.skill.License,
		APIVersion:    "1.0",
		Type:          plugin.TypeScript, // Skills are script-based
		Category:      a.skill.Category,
		Tags:          a.skill.Tags,
		Permissions:   []string{"filesystem"},
		Hooks:         []string{"on_load"},
	}
}

// Initialize initializes the skill plugin
func (a *PluginAdapter) Initialize(ctx *plugin.Context) error {
	// Skills don't require special initialization
	// The context is stored for potential later use
	return nil
}

// Execute executes a skill command
func (a *PluginAdapter) Execute(cmd string, args []string) (interface{}, error) {
	// For skills, "execute" returns the skill content
	// This allows the agent to use the skill
	return map[string]interface{}{
		"name":    a.skill.Name,
		"content": a.skill.Content,
		"tools":   a.skill.GetTools(),
		"tags":    a.skill.GetTags(),
	}, nil
}

// Shutdown shuts down the skill plugin
func (a *PluginAdapter) Shutdown() error {
	return nil
}

// PluginAdapterWithTools adapts a Skill with tool definitions
type PluginAdapterWithTools struct {
	*PluginAdapter
	tools []ToolSpec
}

// ToolSpec defines a tool provided by a skill plugin
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

// NewPluginAdapterWithTools creates an adapter with tool definitions
func NewPluginAdapterWithTools(skill *Skill, tools []ToolSpec) *PluginAdapterWithTools {
	return &PluginAdapterWithTools{
		PluginAdapter: NewPluginAdapter(skill),
		tools:        tools,
	}
}

// Manifest returns the plugin manifest with tools
func (a *PluginAdapterWithTools) Manifest() *plugin.PluginManifest {
	manifest := a.PluginAdapter.Manifest()

	// Add commands from tools
	for _, tool := range a.tools {
		manifest.Commands = append(manifest.Commands, plugin.CommandSpec{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}

	return manifest
}

// Execute handles tool execution
func (a *PluginAdapterWithTools) Execute(cmd string, args []string) (interface{}, error) {
	// Check if executing a specific tool
	if cmd != "" {
		for _, tool := range a.tools {
			if tool.Name == cmd {
				// Tool execution would be handled by the tool registry
				return fmt.Sprintf("Tool '%s' would be executed here", tool.Name), nil
			}
		}
		return nil, fmt.Errorf("unknown tool: %s", cmd)
	}

	// Default: return skill content
	return a.PluginAdapter.Execute(cmd, args)
}

// BuiltinSkillPlugin wraps a built-in skill as a plugin
type BuiltinSkillPlugin struct {
	name        string
	description string
	content     string
	category    string
	tags        []string
	author      string
	version     string
}

// NewBuiltinSkillPlugin creates a new built-in skill plugin
func NewBuiltinSkillPlugin(name, description, content, category string, tags []string) *BuiltinSkillPlugin {
	return &BuiltinSkillPlugin{
		name:        name,
		description: description,
		content:     content,
		category:    category,
		tags:        tags,
		author:      "go-magic",
		version:     "1.0.0",
	}
}

// WithAuthor sets the author
func (p *BuiltinSkillPlugin) WithAuthor(author string) *BuiltinSkillPlugin {
	p.author = author
	return p
}

// WithVersion sets the version
func (p *BuiltinSkillPlugin) WithVersion(version string) *BuiltinSkillPlugin {
	p.version = version
	return p
}

// Manifest returns the plugin manifest
func (p *BuiltinSkillPlugin) Manifest() *plugin.PluginManifest {
	return &plugin.PluginManifest{
		ID:          sanitizeID(p.name),
		Name:        p.name,
		Version:     p.version,
		Description: p.description,
		LongDesc:    p.description,
		Author:      p.author,
		License:     "MIT",
		APIVersion:  "1.0",
		Type:        plugin.TypeScript,
		Category:    p.category,
		Tags:        p.tags,
		Permissions: []string{"filesystem"},
		Hooks:       []string{"on_load"},
		Events:      []string{},
		Commands: []plugin.CommandSpec{
			{Name: "use", Description: "Use this skill"},
		},
	}
}

// Initialize initializes the plugin
func (p *BuiltinSkillPlugin) Initialize(ctx *plugin.Context) error {
	return nil
}

// Execute executes the skill
func (p *BuiltinSkillPlugin) Execute(cmd string, args []string) (interface{}, error) {
	return map[string]interface{}{
		"name":        p.name,
		"description": p.description,
		"content":     p.content,
		"category":    p.category,
		"tags":        p.tags,
		"author":      p.author,
	}, nil
}

// Shutdown shuts down the plugin
func (p *BuiltinSkillPlugin) Shutdown() error {
	return nil
}

// RegisterBuiltinSkills registers all built-in skills as plugins
func RegisterBuiltinSkills(registry *plugin.Registry) error {
	builtins := GetBuiltinSkills()

	for _, skill := range builtins {
		plugin := NewBuiltinSkillPlugin(
			skill.Name,
			skill.Description,
			skill.Content,
			skill.Category,
			skill.Tags,
		).WithAuthor(skill.Author).WithVersion(skill.Version)

		if err := registry.Register(plugin); err != nil {
			// Log but continue
			fmt.Printf("Warning: failed to register built-in skill %s: %v\n", skill.Name, err)
		}
	}

	return nil
}

// BuiltinSkill represents a built-in skill definition
type BuiltinSkill struct {
	Name        string
	Description string
	Content     string
	Category    string
	Tags        []string
	Author      string
	Version     string
}

// GetBuiltinSkills returns all built-in skill definitions
func GetBuiltinSkills() []BuiltinSkill {
	return []BuiltinSkill{
		// Code Review Skill
		{
			Name:        "code-review",
			Description: "Analyzes code for bugs, style issues, and potential improvements",
			Category:    "development",
			Tags:        []string{"code", "review", "analysis"},
			Author:      "go-magic",
			Version:     "1.0.0",
			Content: `## Code Review Skill

This skill helps you review code by:
1. Identifying potential bugs and security issues
2. Checking code style and best practices
3. Suggesting performance improvements
4. Reviewing error handling

### How to use
When reviewing code, provide the code content and specify:
- The programming language
- Any specific concerns or areas to focus on

### Output format
- Issues found (severity: critical/high/medium/low)
- Suggestions for improvement
- Overall code quality assessment
`,
		},

		// Daily Report Skill
		{
			Name:        "daily-report",
			Description: "Generates daily status reports from task updates",
			Category:    "productivity",
			Tags:        []string{"report", "daily", "summary"},
			Author:      "go-magic",
			Version:     "1.0.0",
			Content: `## Daily Report Skill

This skill helps you create daily status reports by:
1. Collecting task updates and progress
2. Identifying blockers and risks
3. Summarizing accomplishments
4. Planning next day's work

### How to use
Provide your task updates in natural language, and the skill will format them into a structured report.

### Output format
- Summary section
- Completed tasks
- In-progress tasks
- Blockers and risks
- Tomorrow's plan
`,
		},

		// File Organizer Skill
		{
			Name:        "file-organizer",
			Description: "Organizes and categorizes files based on type and content",
			Category:    "utilities",
			Tags:        []string{"file", "organization", "management"},
			Author:      "go-magic",
			Version:     "1.0.0",
			Content: `## File Organizer Skill

This skill helps organize files by:
1. Detecting file types and extensions
2. Suggesting appropriate directories
3. Creating naming conventions
4. Batch organizing files

### How to use
Provide the directory path and optional criteria for organization.
`,
		},

		// Summarization Skill
		{
			Name:        "summarization",
			Description: "Creates concise summaries of long documents or conversations",
			Category:    "productivity",
			Tags:        []string{"summary", "text", "condensation"},
			Author:      "go-magic",
			Version:     "1.0.0",
			Content: `## Summarization Skill

This skill creates effective summaries by:
1. Identifying key points and main ideas
2. Removing redundant information
3. Preserving important details
4. Maintaining logical flow

### How to use
Provide the text to summarize and indicate:
- Desired summary length (brief/detailed)
- Focus areas if any
- Format preference
`,
		},

		// Translation Skill
		{
			Name:        "translation",
			Description: "Translates text between languages with context awareness",
			Category:    "language",
			Tags:        []string{"translation", "language", "localization"},
			Author:      "go-magic",
			Version:     "1.0.0",
			Content: `## Translation Skill

This skill translates text while:
1. Preserving meaning and nuance
2. Adapting cultural references
3. Maintaining formatting
4. Handling specialized terminology

### How to use
Provide the text and target language. Optionally specify:
- Tone (formal/informal)
- Domain (technical/general)
- Special terminology to use
`,
		},

		// Web Search Skill
		{
			Name:        "web-search",
			Description: "Searches the web for information and synthesizes results",
			Category:    "research",
			Tags:        []string{"search", "web", "research", "information"},
			Author:      "go-magic",
			Version:     "1.0.0",
			Content: `## Web Search Skill

This skill helps find and synthesize web information by:
1. Formulating effective search queries
2. Filtering relevant results
3. Extracting key information
4. Citing sources properly

### How to use
Provide your research question or topic. The skill will:
- Break down complex queries
- Search multiple sources
- Synthesize findings
- Provide citations
`,
		},
	}
}

// sanitizeID converts a name to a valid plugin ID
func sanitizeID(name string) string {
	// Convert to lowercase
	result := ""
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result += string(c)
		} else if c >= 'A' && c <= 'Z' {
			result += string(c + 32) // to lowercase
		} else if c == ' ' || c == '-' || c == '_' {
			result += "-"
		}
	}
	// Remove leading/trailing hyphens
	for len(result) > 0 && result[0] == '-' {
		result = result[1:]
	}
	for len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	if result == "" {
		result = "unnamed"
	}
	return result
}
