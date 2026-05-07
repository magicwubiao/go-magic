package importer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// Converter converts external skill formats to go-magic Skill format
type Converter struct{}

// NewConverter creates a new converter
func NewConverter() *Converter {
	return &Converter{}
}

// ConvertOpenClaw converts an OpenClaw skill to go-magic Skill
func (c *Converter) ConvertOpenClaw(openclaw *parser.OpenClawSkill) (*skills.Skill, error) {
	if err := openclaw.Validate(); err != nil {
		return nil, fmt.Errorf("invalid OpenClaw skill: %w", err)
	}

	// Build metadata
	metadata := make(map[string]interface{})
	metadata["source"] = "openclaw"
	metadata["source_path"] = openclaw.SourcePath
	metadata["source_format"] = string(parser.FormatOpenClaw)

	// Add trigger conditions if present
	if len(openclaw.TriggerConditions) > 0 {
		metadata["trigger_conditions"] = openclaw.TriggerConditions
	}

	// Add steps if present
	if len(openclaw.Steps) > 0 {
		metadata["steps"] = openclaw.Steps
	}

	// Add code files
	if len(openclaw.CodeFiles) > 0 {
		metadata["code_files"] = openclaw.CodeFiles
		metadata["code_language"] = openclaw.CodeLanguage
	}

	// Build combined content
	content := c.buildContent(openclaw.Name, openclaw.Description, openclaw.Content, openclaw.CodeFiles)

	return &skills.Skill{
		SkillMeta: skills.SkillMeta{
			Name:        openclaw.Name,
			Description: openclaw.Description,
			Version:     openclaw.Version,
			Author:      openclaw.Author,
			License:     openclaw.License,
			Tags:        openclaw.Tags,
			Source:      "imported",
			InstalledAt: time.Now(),
		},
		Tools:    openclaw.Tools,
		Content:  content,
		Metadata: metadata,
	}, nil
}

// ConvertHermes converts a Hermes skill to go-magic Skill
func (c *Converter) ConvertHermes(hermes *parser.HermesSkill) (*skills.Skill, error) {
	if err := hermes.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Hermes skill: %w", err)
	}

	// Build metadata
	metadata := make(map[string]interface{})
	metadata["source"] = "hermes"
	metadata["source_path"] = hermes.SourcePath
	metadata["source_format"] = string(parser.FormatHermes)

	// Preserve hermes-specific metadata
	if hermes.Category != "" {
		metadata["category"] = hermes.Category
	}

	// Add trigger conditions if present
	if len(hermes.TriggerConditions) > 0 {
		metadata["trigger_conditions"] = hermes.TriggerConditions
	}

	// Add steps if present
	if len(hermes.Steps) > 0 {
		metadata["steps"] = hermes.Steps
	}

	// Add code files
	if len(hermes.CodeFiles) > 0 {
		metadata["code_files"] = hermes.CodeFiles
		metadata["code_language"] = hermes.CodeLanguage
	}

	// Add original metadata
	for key, value := range hermes.Metadata {
		if key != "hermes" {
			metadata[key] = value
		}
	}

	// Build combined content
	content := c.buildContent(hermes.Name, hermes.Description, hermes.Content, hermes.CodeFiles)

	return &skills.Skill{
		SkillMeta: skills.SkillMeta{
			Name:        hermes.Name,
			Description: hermes.Description,
			Version:     hermes.Version,
			Author:      hermes.Author,
			License:     hermes.License,
			Tags:        hermes.Tags,
			Source:      "imported",
			InstalledAt: time.Now(),
		},
		Tools:    hermes.Tools,
		Content:  content,
		Metadata: metadata,
	}, nil
}

// ConvertFromParseResult converts a generic ParseResult to go-magic Skill
func (c *Converter) ConvertFromParseResult(result *parser.ParseResult) (*skills.Skill, error) {
	if result == nil {
		return nil, fmt.Errorf("parse result is nil")
	}

	switch result.Format {
	case parser.FormatOpenClaw:
		openclaw := &parser.OpenClawSkill{
			Name:              result.Name,
			Description:       result.Data["description"].(string),
			Version:           getString(result.Data, "version"),
			Author:            getString(result.Data, "author"),
			Tags:              getStringSlice(result.Data, "tags"),
			Tools:             getStringSlice(result.Data, "tools"),
			TriggerConditions: getStringSlice(result.Data, "trigger_conditions"),
			Steps:             getStringSlice(result.Data, "steps"),
			Content:           result.Content,
			CodeFiles:         result.CodeFiles,
		}
		return c.ConvertOpenClaw(openclaw)

	case parser.FormatHermes:
		hermes := &parser.HermesSkill{
			Name:              result.Name,
			Description:       result.Data["description"].(string),
			Version:           getString(result.Data, "version"),
			Author:            getString(result.Data, "author"),
			Tags:              getStringSlice(result.Data, "tags"),
			Tools:             getStringSlice(result.Data, "tools"),
			TriggerConditions: getStringSlice(result.Data, "trigger_conditions"),
			Steps:             getStringSlice(result.Data, "steps"),
			Content:           result.Content,
			CodeFiles:         result.CodeFiles,
			Metadata:          result.Data,
		}
		return c.ConvertHermes(hermes)

	default:
		// Generic magic format
		skill := &skills.Skill{
			SkillMeta: skills.SkillMeta{
				Name:        result.Name,
				Description: getString(result.Data, "description"),
				Version:     getString(result.Data, "version"),
				Author:      getString(result.Data, "author"),
				Tags:        getStringSlice(result.Data, "tags"),
				Source:      "imported",
				InstalledAt: time.Now(),
			},
			Tools:   getStringSlice(result.Data, "tools"),
			Content: result.Content,
		}

		if result.Data != nil {
			skill.Metadata = result.Data
		}

		return skill, nil
	}
}

// buildContent combines markdown content with code files
func (c *Converter) buildContent(name, description, markdown string, codeFiles map[string]string) string {
	var builder strings.Builder

	// Add header
	builder.WriteString(fmt.Sprintf("# %s\n\n", name))
	builder.WriteString(fmt.Sprintf("%s\n\n", description))

	// Add markdown content if present
	if markdown != "" {
		builder.WriteString("## Documentation\n\n")
		builder.WriteString(markdown)
		builder.WriteString("\n\n")
	}

	// Add code files section
	if len(codeFiles) > 0 {
		builder.WriteString("## Code Files\n\n")
		for filename, content := range codeFiles {
			ext := strings.TrimPrefix(filepath.Ext(filename), ".")
			builder.WriteString(fmt.Sprintf("### %s (.%s)\n\n", filename, ext))
			builder.WriteString("```" + ext + "\n")
			builder.WriteString(content)
			builder.WriteString("\n```\n\n")
		}
	}

	return builder.String()
}

// ToJSON converts a skill to JSON for storage
func (c *Converter) ToJSON(skill *skills.Skill) ([]byte, error) {
	return json.MarshalIndent(skill, "", "  ")
}

// Helper functions for type-safe access to map data
func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func getStringSlice(data map[string]interface{}, key string) []string {
	if v, ok := data[key].([]string); ok {
		return v
	}
	return nil
}
