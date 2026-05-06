package skills

import (
	"time"
)

// SkillSource indicates where the skill comes from
type SkillSource string

const (
	SkillSourceLocal    SkillSource = "local"
	SkillSourceGlobal   SkillSource = "global"
	SkillSourceBuiltin  SkillSource = "builtin"
	SkillSourceRegistry SkillSource = "registry"
)

// SkillMeta contains metadata about a skill
type SkillMeta struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Version     string     `json:"version,omitempty"`
	Author      string     `json:"author,omitempty"`
	License     string     `json:"license,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Category    string     `json:"category,omitempty"`
	Source      SkillSource `json:"source,omitempty"`
	InstalledAt time.Time  `json:"installed_at,omitempty"`
}

// Skill represents a unified skill with metadata and content
// This is the canonical type for all skills in the system
type Skill struct {
	SkillMeta
	Tools    []string               `json:"tools,omitempty"` // Tools required by this skill
	Content  string                 `json:"content"`         // Main skill content/prompt
	Metadata map[string]interface{} `json:"metadata,omitempty"` // Additional metadata
}

// GetTools returns the list of tool names required by this skill
func (s *Skill) GetTools() []string {
	// First check explicit tools list
	if len(s.Tools) > 0 {
		return s.Tools
	}

	// Then check metadata
	if s.Metadata != nil {
		if tools, ok := s.Metadata["tools"].([]string); ok {
			return tools
		}
		// Check hermes format
		if hermes, ok := s.Metadata["hermes"].(map[string]interface{}); ok {
			if tools, ok := hermes["tools"].([]string); ok {
				return tools
			}
		}
	}

	return nil
}

// GetTags returns skill tags
func (s *Skill) GetTags() []string {
	if len(s.Tags) > 0 {
		return s.Tags
	}
	if s.Metadata != nil {
		if tags, ok := s.Metadata["tags"].([]string); ok {
			return tags
		}
		if hermes, ok := s.Metadata["hermes"].(map[string]interface{}); ok {
			if tags, ok := hermes["tags"].([]string); ok {
				return tags
			}
		}
	}
	return nil
}

// ToSkillMeta converts Skill to SkillMeta
func (s *Skill) ToSkillMeta() *SkillMeta {
	return &SkillMeta{
		Name:        s.Name,
		Description: s.Description,
		Version:     s.Version,
		Author:      s.Author,
		License:     s.License,
		Tags:        s.Tags,
		Category:    s.Category,
		Source:      s.Source,
		InstalledAt: s.InstalledAt,
	}
}

// NewSkill creates a new Skill with the given metadata
func NewSkill(name, description string) *Skill {
	return &Skill{
		SkillMeta: SkillMeta{
			Name:        name,
			Description: description,
			InstalledAt: time.Now(),
		},
		Metadata: make(map[string]interface{}),
	}
}
