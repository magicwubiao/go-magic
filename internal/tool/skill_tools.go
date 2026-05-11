package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// SkillListTool lists all available skills
type SkillListTool struct {
	*BaseTool
	manager interface {
		List() map[string]*Skill
		GetCategories() []string
	}
}

// NewSkillListTool creates a new skill list tool
func NewSkillListTool() *SkillListTool {
	return &SkillListTool{
		BaseTool: NewBaseTool(
			"skill_list",
			"List all available skills. Returns skill names, descriptions, categories, and sources.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter skills by category",
					},
					"source": map[string]interface{}{
						"type":        "string",
						"description": "Filter by source: local, builtin, registry, auto",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query to filter skills by name or description",
					},
				},
			},
		),
	}
}

// SetManager sets the skills manager
func (t *SkillListTool) SetManager(mgr interface {
	List() map[string]*Skill
	GetCategories() []string
}) {
	t.manager = mgr
}

// Execute lists all skills
func (t *SkillListTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("skills manager not initialized")
	}

	category := ""
	if c, ok := args["category"].(string); ok {
		category = c
	}

	source := ""
	if s, ok := args["source"].(string); ok {
		source = s
	}

	query := ""
	if q, ok := args["query"].(string); ok {
		query = q
	}

	skills := t.manager.List()
	categories := t.manager.GetCategories()

	// Filter skills
	var filtered []*Skill
	for _, skill := range skills {
		// Filter by category
		if category != "" && skill.Category != category {
			continue
		}

		// Filter by source
		if source != "" && string(skill.Source) != source {
			continue
		}

		// Filter by query
		if query != "" {
			found := false
			lowerQuery := toLower(query)
			if contains(toLower(skill.Name), lowerQuery) {
				found = true
			} else if contains(toLower(skill.Description), lowerQuery) {
				found = true
			}
			if !found {
				continue
			}
		}

		filtered = append(filtered, skill)
	}

	// Convert to output format
	type SkillSummary struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Category    string   `json:"category,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		Source      string   `json:"source"`
		Version     string   `json:"version,omitempty"`
	}

	summaries := make([]SkillSummary, 0, len(filtered))
	for _, s := range filtered {
		summaries = append(summaries, SkillSummary{
			Name:        s.Name,
			Description: s.Description,
			Category:    s.Category,
			Tags:        s.Tags,
			Source:      string(s.Source),
			Version:     s.Version,
		})
	}

	return map[string]interface{}{
		"count":      len(summaries),
		"categories": categories,
		"skills":     summaries,
	}, nil
}

// toLower is a helper
func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// SkillInfoTool shows detailed information about a skill
type SkillInfoTool struct {
	*BaseTool
	manager interface {
		Get(name string) (*Skill, error)
		GetSkillDir(name string) (string, error)
	}
}

// NewSkillInfoTool creates a new skill info tool
func NewSkillInfoTool() *SkillInfoTool {
	return &SkillInfoTool{
		BaseTool: NewBaseTool(
			"skill_info",
			"Get detailed information about a specific skill including its full content, metadata, and supporting files.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "The name of the skill to get info about",
					},
					"include_content": map[string]interface{}{
						"type":        "boolean",
						"description": "Include the full skill content (default: true)",
					},
				},
				"required": []string{"name"},
			},
		),
	}
}

// SetManager sets the skills manager
func (t *SkillInfoTool) SetManager(mgr interface {
	Get(name string) (*Skill, error)
	GetSkillDir(name string) (string, error)
}) {
	t.manager = mgr
}

// Execute returns skill information
func (t *SkillInfoTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("skills manager not initialized")
	}

	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("skill name is required")
	}

	skill, err := t.manager.Get(name)
	if err != nil {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	includeContent := true
	if ic, ok := args["include_content"].(bool); ok {
		includeContent = ic
	}

	skillDir, _ := t.manager.GetSkillDir(name)

	result := map[string]interface{}{
		"name":        skill.Name,
		"description": skill.Description,
		"version":     skill.Version,
		"author":      skill.Author,
		"category":    skill.Category,
		"tags":        skill.Tags,
		"source":      string(skill.Source),
		"directory":   skillDir,
	}

	if includeContent {
		result["content"] = skill.Content
	}

	// Add supporting files info
	if len(skill.Scripts) > 0 {
		result["scripts"] = skill.Scripts
	}

	return result, nil
}

// SkillManageTool allows creating, updating, and deleting skills
type SkillManageTool struct {
	*BaseTool
	manager interface {
		Create(name, description, content, category string, tags []string) (*Skill, error)
		Update(name, content string) error
		Delete(name string) error
	}
}

// NewSkillManageTool creates a new skill manage tool
func NewSkillManageTool() *SkillManageTool {
	return &SkillManageTool{
		BaseTool: NewBaseTool(
			"skill_manage",
			"Create, update, or delete skills. Use this to save complex workflows as reusable skills.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform: create, update, delete",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Skill name",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Skill description (for create)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Full skill content in SKILL.md format (for create/update)",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Skill category (for create)",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Skill tags (for create)",
					},
				},
				"required": []string{"action", "name"},
			},
		),
	}
}

// SetManager sets the skills manager
func (t *SkillManageTool) SetManager(mgr interface {
	Create(name, description, content, category string, tags []string) (*Skill, error)
	Update(name, content string) error
	Delete(name string) error
}) {
	t.manager = mgr
}

// Execute performs the skill management action
func (t *SkillManageTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("skills manager not initialized")
	}

	action, ok := args["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action is required (create, update, delete)")
	}

	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required")
	}

	switch action {
	case "create":
		description := ""
		if d, ok := args["description"].(string); ok {
			description = d
		}
		content := ""
		if c, ok := args["content"].(string); ok {
			content = c
		}
		category := ""
		if cat, ok := args["category"].(string); ok {
			category = cat
		}
		var tags []string
		if t, ok := args["tags"].([]interface{}); ok {
			for _, tt := range t {
				if ts, ok := tt.(string); ok {
					tags = append(tags, ts)
				}
			}
		}

		skill, err := t.manager.Create(name, description, content, category, tags)
		if err != nil {
			return nil, fmt.Errorf("failed to create skill: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Skill '%s' created successfully", name),
			"skill":   skill.Name,
		}, nil

	case "update":
		content, ok := args["content"].(string)
		if !ok || content == "" {
			return nil, fmt.Errorf("content is required for update")
		}

		if err := t.manager.Update(name, content); err != nil {
			return nil, fmt.Errorf("failed to update skill: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Skill '%s' updated successfully", name),
		}, nil

	case "delete":
		if err := t.manager.Delete(name); err != nil {
			return nil, fmt.Errorf("failed to delete skill: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Skill '%s' deleted successfully", name),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (use: create, update, delete)", action)
	}
}

// MarshalJSON helper for skill source
func (t *SkillManageTool) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Name())
}
