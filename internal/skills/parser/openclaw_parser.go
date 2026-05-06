package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenClawSkill represents an OpenClaw format skill
type OpenClawSkill struct {
	Name             string
	Description      string
	Version          string
	Author           string
	License          string
	Tags             []string
	Tools            []string
	TriggerConditions []string
	Steps            []string
	Content          string
	CodeFiles        map[string]string // filename -> content
	CodeLanguage     string
	SourcePath       string
}

// OpenClawParser parses OpenClaw format skills
type OpenClawParser struct{}

// NewOpenClawParser creates a new OpenClaw parser
func NewOpenClawParser() *OpenClawParser {
	return &OpenClawParser{}
}

// Parse parses an OpenClaw format skill from a directory
func (p *OpenClawParser) Parse(skillDir string) (*OpenClawSkill, error) {
	skillMdPath := filepath.Join(skillDir, "SKILL.md")

	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	frontmatter, content, err := ParseYAMLFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skill := &OpenClawSkill{
		SourcePath: skillDir,
		Content:    content,
		CodeFiles:  make(map[string]string),
	}

	// Parse frontmatter fields
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

	// Parse trigger_conditions (OpenClaw specific)
	if triggers, ok := frontmatter["trigger_conditions"].([]string); ok {
		skill.TriggerConditions = triggers
	} else if triggersStr, ok := frontmatter["trigger_conditions"].(string); ok {
		skill.TriggerConditions = parseInlineArray(strings.Trim(triggersStr, "[]"))
	}

	// Parse steps (OpenClaw specific)
	if steps, ok := frontmatter["steps"].([]string); ok {
		skill.Steps = steps
	} else if stepsStr, ok := frontmatter["steps"].(string); ok {
		skill.Steps = parseInlineArray(strings.Trim(stepsStr, "[]"))
	}

	// Read code files
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill directory: %w", err)
	}

	var primaryCodeFile string
	var primaryLang string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if isCodeFile(ext) {
			path := filepath.Join(skillDir, entry.Name())
			codeData, err := os.ReadFile(path)
			if err != nil {
				continue // Skip files that can't be read
			}

			skill.CodeFiles[entry.Name()] = string(codeData)

			// Track primary code file (first one found)
			if primaryCodeFile == "" {
				primaryCodeFile = entry.Name()
				primaryLang = GetCodeLanguage(entry.Name())
			}
		}
	}

	skill.CodeLanguage = primaryLang
	_ = primaryCodeFile // Will be used when we expose code

	return skill, nil
}

// ParseFromFiles parses skill data from a map of files
func (p *OpenClawParser) ParseFromFiles(files map[string]string) (*OpenClawSkill, error) {
	skillMd, ok := files["SKILL.md"]
	if !ok {
		return nil, fmt.Errorf("SKILL.md not found in files")
	}

	frontmatter, content, err := ParseYAMLFrontmatter(skillMd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skill := &OpenClawSkill{
		Content:    content,
		CodeFiles:  make(map[string]string),
	}

	// Parse frontmatter fields
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

	// Parse trigger_conditions
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

	// Collect code files
	var primaryCodeFile string
	for filename, content := range files {
		if filename == "SKILL.md" {
			continue
		}
		ext := filepath.Ext(filename)
		if isCodeFile(ext) {
			skill.CodeFiles[filename] = content
			if primaryCodeFile == "" {
				primaryCodeFile = filename
				skill.CodeLanguage = GetCodeLanguage(filename)
			}
		}
	}

	_ = primaryCodeFile

	return skill, nil
}

// Validate checks if the skill has all required fields
func (s *OpenClawSkill) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if s.Description == "" {
		return fmt.Errorf("skill description is required")
	}
	return nil
}

// GetPrimaryCode returns the primary code file content
func (s *OpenClawSkill) GetPrimaryCode() (string, string) {
	for filename, content := range s.CodeFiles {
		return filename, content
	}
	return "", ""
}
