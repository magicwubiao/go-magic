package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillMeta defines metadata for a skill
type SkillMeta struct {
	Name        string
	Description string
	Version     string
	Author      string
	License     string
	Tags        []string
	Category    string
	Source      string
}

// Skill represents a unified skill type
type Skill struct {
	SkillMeta SkillMeta
	Tools     []string
	Content   string
	Metadata  map[string]interface{}
}

const SkillSourceBuiltin = "builtin"

// HermesSkill represents a Hermes format skill
type HermesSkill struct {
	Name             string
	Description      string
	Version          string
	Author           string
	License          string
	Tags             []string
	Tools            []string
	Category         string
	TriggerConditions []string
	Steps            []string
	Content          string
	CodeFiles        map[string]string // filename -> content
	CodeLanguage     string
	SourcePath       string
	Metadata         map[string]interface{}
}

// HermesParser parses Hermes format skills
type HermesParser struct{}

// NewHermesParser creates a new Hermes parser
func NewHermesParser() *HermesParser {
	return &HermesParser{}
}

// Parse parses a Hermes format skill from a directory
func (p *HermesParser) Parse(skillDir string) (*HermesSkill, error) {
	skillMdPath := filepath.Join(skillDir, "SKILL.md")

	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	frontmatter, content, err := ParseYAMLFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skill := &HermesSkill{
		SourcePath: skillDir,
		Content:    content,
		CodeFiles:  make(map[string]string),
		Metadata:   make(map[string]interface{}),
	}

	// Parse direct frontmatter fields
	if name, ok := frontmatter["name"].(string); ok {
		skill.Name = name
	}
	if desc, ok := frontmatter["description"].(string); ok {
		skill.Description = desc
	}
	if version, ok := frontmatter["version"].(string); ok {
		skill.Version = version
	}
	if author, ok := frontmatter["author"].(string); ok {
		skill.Author = author
	}
	if license, ok := frontmatter["license"].(string); ok {
		skill.License = license
	}

	// Parse tags
	if tags, ok := frontmatter["tags"].([]string); ok {
		skill.Tags = tags
	} else if tagsStr, ok := frontmatter["tags"].(string); ok {
		skill.Tags = parseInlineArray(strings.Trim(tagsStr, "[]"))
	}

	// Parse tools
	if tools, ok := frontmatter["tools"].([]string); ok {
		skill.Tools = tools
	} else if toolsStr, ok := frontmatter["tools"].(string); ok {
		skill.Tools = parseInlineArray(strings.Trim(toolsStr, "[]"))
	}

	// Parse hermes-specific fields
	if hermes, ok := frontmatter["hermes"].(map[string]interface{}); ok {
		skill.Metadata["hermes"] = hermes

		// Parse cortex.tags
		if tags, ok := hermes["tags"].([]string); ok {
			if len(skill.Tags) == 0 {
				skill.Tags = tags
			}
		}

		// Parse cortex.category
		if category, ok := hermes["category"].(string); ok {
			skill.Category = category
		}

		// Parse cortex.tools
		if tools, ok := hermes["tools"].([]string); ok {
			if len(skill.Tools) == 0 {
				skill.Tools = tools
			}
		}
	}

	// Parse trigger_conditions (Hermes also supports this)
	if triggers, ok := frontmatter["trigger_conditions"].([]string); ok {
		skill.TriggerConditions = triggers
	} else if triggersStr, ok := frontmatter["trigger_conditions"].(string); ok {
		skill.TriggerConditions = parseInlineArray(strings.Trim(triggersStr, "[]"))
	}

	// Parse steps
	if steps, ok := frontmatter["steps"].([]string); ok {
		skill.Steps = steps
	} else if stepsStr, ok := frontmatter["steps"].(string); ok {
		skill.Steps = parseInlineArray(strings.Trim(stepsStr, "[]"))
	}

	// Parse metadata (non-hermes)
	for key, value := range frontmatter {
		if key != "hermes" && key != "name" && key != "description" &&
			key != "version" && key != "author" && key != "license" &&
			key != "tags" && key != "tools" && key != "trigger_conditions" &&
			key != "steps" {
			skill.Metadata[key] = value
		}
	}

	// Read code files
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill directory: %w", err)
	}

	var primaryCodeFile string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if IsCodeFile(ext) {
			path := filepath.Join(skillDir, entry.Name())
			codeData, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			skill.CodeFiles[entry.Name()] = string(codeData)

			if primaryCodeFile == "" {
				primaryCodeFile = entry.Name()
				skill.CodeLanguage = GetCodeLanguage(entry.Name())
			}
		}
	}

	return skill, nil
}

// ParseFromFiles parses skill data from a map of files
func (p *HermesParser) ParseFromFiles(files map[string]string) (*HermesSkill, error) {
	skillMd, ok := files["SKILL.md"]
	if !ok {
		return nil, fmt.Errorf("SKILL.md not found in files")
	}

	frontmatter, content, err := ParseYAMLFrontmatter(skillMd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skill := &HermesSkill{
		Content:  content,
		CodeFiles: make(map[string]string),
		Metadata: make(map[string]interface{}),
	}

	// Parse direct frontmatter fields
	if name, ok := frontmatter["name"].(string); ok {
		skill.Name = name
	}
	if desc, ok := frontmatter["description"].(string); ok {
		skill.Description = desc
	}
	if version, ok := frontmatter["version"].(string); ok {
		skill.Version = version
	}
	if author, ok := frontmatter["author"].(string); ok {
		skill.Author = author
	}
	if license, ok := frontmatter["license"].(string); ok {
		skill.License = license
	}

	// Parse tags
	if tags, ok := frontmatter["tags"].([]string); ok {
		skill.Tags = tags
	} else if tagsStr, ok := frontmatter["tags"].(string); ok {
		skill.Tags = parseInlineArray(strings.Trim(tagsStr, "[]"))
	}

	// Parse tools
	if tools, ok := frontmatter["tools"].([]string); ok {
		skill.Tools = tools
	} else if toolsStr, ok := frontmatter["tools"].(string); ok {
		skill.Tools = parseInlineArray(strings.Trim(toolsStr, "[]"))
	}

	// Parse hermes-specific fields
	if hermes, ok := frontmatter["hermes"].(map[string]interface{}); ok {
		skill.Metadata["hermes"] = hermes

		if tags, ok := hermes["tags"].([]string); ok {
			if len(skill.Tags) == 0 {
				skill.Tags = tags
			}
		}
		if category, ok := hermes["category"].(string); ok {
			skill.Category = category
		}
		if tools, ok := hermes["tools"].([]string); ok {
			if len(skill.Tools) == 0 {
				skill.Tools = tools
			}
		}
	}

	// Collect code files
	var primaryCodeFile string
	for filename, content := range files {
		if filename == "SKILL.md" {
			continue
		}
		ext := filepath.Ext(filename)
		if IsCodeFile(ext) {
			skill.CodeFiles[filename] = content
			if primaryCodeFile == "" {
				primaryCodeFile = filename
				skill.CodeLanguage = GetCodeLanguage(filename)
			}
		}
	}

	return skill, nil
}

// Validate checks if the skill has all required fields
func (s *HermesSkill) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if s.Description == "" {
		return fmt.Errorf("skill description is required")
	}
	return nil
}

// GetPrimaryCode returns the primary code file content
func (s *HermesSkill) GetPrimaryCode() (string, string) {
	for filename, content := range s.CodeFiles {
		return filename, content
	}
	return "", ""
}

// ParseAgentsSkillsIO parses skills from agentskills.io format
// This is similar to Hermes but with slight field differences
func (p *HermesParser) ParseAgentsSkillsIO(skillDir string) (*HermesSkill, error) {
	skill, err := p.Parse(skillDir)
	if err != nil {
		return nil, err
	}

	// agentskills.io might use different field names
	// Normalize them here if needed

	return skill, nil
}

// ToSkill converts HermesSkill to the unified Skill type
// This allows HermesSkill to be used as an intermediate parsing format
func (s *HermesSkill) ToSkill() *Skill {
	return &Skill{
		SkillMeta: SkillMeta{
			Name:        s.Name,
			Description: s.Description,
			Version:     s.Version,
			Author:      s.Author,
			License:     s.License,
			Tags:        s.Tags,
			Category:    s.Category,
			Source:      SkillSourceBuiltin,
		},
		Tools:    s.Tools,
		Content:  s.Content,
		Metadata: s.Metadata,
	}
}
