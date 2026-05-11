package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// SkillData represents skill data as a map to avoid circular imports
type SkillData map[string]interface{}

// SkillsManager interface for skills manager
type SkillsManager interface {
	List() map[string]SkillData
	Get(name string) (SkillData, error)
	GetCategories() []string
	GetSkillDir(name string) (string, error)
	Create(name, description, content, category string, tags []string) (SkillData, error)
	Update(name, content string) error
	Delete(name string) error
}

// SkillListTool lists all available skills
type SkillListTool struct {
	*BaseTool
	manager SkillsManager
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
func (t *SkillListTool) SetManager(mgr SkillsManager) {
	t.manager = mgr
}

// Execute lists all skills
func (t *SkillListTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("skills manager not initialized")
	}

	category := getString(args, "category")
	source := getString(args, "source")
	query := getString(args, "query")

	skills := t.manager.List()
	categories := t.manager.GetCategories()

	// Filter skills
	var filtered []SkillData
	for _, skill := range skills {
		// Filter by category
		if category != "" {
			if cat := getString(skill, "category"); cat != category {
				continue
			}
		}

		// Filter by source
		if source != "" {
			if src := getString(skill, "source"); src != source {
				continue
			}
		}

		// Filter by query
		if query != "" {
			name := getString(skill, "name")
			desc := getString(skill, "description")
			if !containsIgnoreCase(name, query) && !containsIgnoreCase(desc, query) {
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
			Name:        getString(s, "name"),
			Description: getString(s, "description"),
			Category:    getString(s, "category"),
			Tags:        getStringSlice(s, "tags"),
			Source:      getString(s, "source"),
			Version:     getString(s, "version"),
		})
	}

	return map[string]interface{}{
		"count":      len(summaries),
		"categories": categories,
		"skills":     summaries,
	}, nil
}

// SkillViewTool shows detailed information about a skill
type SkillViewTool struct {
	*BaseTool
	manager SkillsManager
}

// NewSkillViewTool creates a new skill view tool
func NewSkillViewTool() *SkillViewTool {
	return &SkillViewTool{
		BaseTool: NewBaseTool(
			"skill_view",
			"Get detailed information about a specific skill including its full content, metadata, and supporting files.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "The name of the skill to get info about",
					},
					"level": map[string]interface{}{
						"type":        "integer",
						"description": "Progressive disclosure level: 0=summary, 1=full content, 2=with references (default: 0)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Specific reference file to view (for level 2)",
					},
				},
				"required": []string{"name"},
			},
		),
	}
}

// SetManager sets the skills manager
func (t *SkillViewTool) SetManager(mgr SkillsManager) {
	t.manager = mgr
}

// Execute returns skill information
func (t *SkillViewTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("skills manager not initialized")
	}

	name := getString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}

	skill, err := t.manager.Get(name)
	if err != nil {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	level := getInt(args, "level")
	path := getString(args, "path")

	skillDir, _ := t.manager.GetSkillDir(name)

	result := map[string]interface{}{
		"name":      getString(skill, "name"),
		"directory": skillDir,
	}

	// Level 0: Summary
	if level >= 0 {
		result["description"] = getString(skill, "description")
		result["version"] = getString(skill, "version")
		result["category"] = getString(skill, "category")
		result["tags"] = getStringSlice(skill, "tags")
		result["source"] = getString(skill, "source")
	}

	// Level 1: Full content
	if level >= 1 {
		result["content"] = getString(skill, "content")
		result["author"] = getString(skill, "author")
		result["scripts"] = getStringSlice(skill, "scripts")
	}

	// Level 2: Specific reference
	if level >= 2 && path != "" {
		// For now, include references in content
		refs := getStringMap(skill, "references")
		if refs != nil {
			if content, ok := refs[path]; ok {
				result["reference_content"] = content
			}
		}
	}

	return result, nil
}

// SkillManageTool allows creating, updating, and deleting skills
type SkillManageTool struct {
	*BaseTool
	manager SkillsManager
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
						"description": "Action to perform: create, update, delete, patch, edit",
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
					"old_string": map[string]interface{}{
						"type":        "string",
						"description": "Text to replace (for patch action)",
					},
					"new_string": map[string]interface{}{
						"type":        "string",
						"description": "Replacement text (for patch action)",
					},
				},
				"required": []string{"action", "name"},
			},
		),
	}
}

// SetManager sets the skills manager
func (t *SkillManageTool) SetManager(mgr SkillsManager) {
	t.manager = mgr
}

// Execute manages skills
func (t *SkillManageTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("skills manager not initialized")
	}

	action := getString(args, "action")
	name := getString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}

	switch action {
	case "create":
		description := getString(args, "description")
		content := getString(args, "content")
		category := getString(args, "category")
		tags := getStringSlice(args, "tags")

		skill, err := t.manager.Create(name, description, content, category, tags)
		if err != nil {
			return nil, fmt.Errorf("failed to create skill: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"action":  "created",
			"skill":   skill,
		}, nil

	case "update", "edit":
		content := getString(args, "content")
		if content == "" {
			return nil, fmt.Errorf("content is required for update")
		}

		err := t.manager.Update(name, content)
		if err != nil {
			return nil, fmt.Errorf("failed to update skill: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"action":  "updated",
			"name":    name,
		}, nil

	case "delete":
		err := t.manager.Delete(name)
		if err != nil {
			return nil, fmt.Errorf("failed to delete skill: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"action":  "deleted",
			"name":    name,
		}, nil

	case "patch":
		// Patch is not directly supported, use update instead
		return nil, fmt.Errorf("patch action not directly supported, use update with full content")

	default:
		return nil, fmt.Errorf("unknown action: %s, supported: create, update, delete", action)
	}
}

// Helper functions
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func getStringMap(m map[string]interface{}, key string) map[string]string {
	if v, ok := m[key].(map[string]interface{}); ok {
		result := make(map[string]string)
		for k, val := range v {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		return result
	}
	return nil
}

func containsIgnoreCase(s, substr string) bool {
	s = strToLower(s)
	substr = strToLower(substr)
	return strContains(s, substr)
}

func strToLower(s string) string {
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

func strContains(s, substr string) bool {
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

// MarshalJSON for SkillListTool
func (t *SkillListTool) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Name())
}

// MarshalJSON for SkillViewTool
func (t *SkillViewTool) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Name())
}

// MarshalJSON for SkillManageTool
func (t *SkillManageTool) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Name())
}
