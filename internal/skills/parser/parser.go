package parser

import (
	"fmt"
	"os"
	"path/filepath"
)

// Parser is a unified parser that can detect and parse different skill formats
type Parser struct {
	HermesParser   *HermesParser
	OpenClawParser *OpenClawParser
}

// NewParser creates a new unified parser
func NewParser() *Parser {
	return &Parser{
		HermesParser:   NewHermesParser(),
		OpenClawParser: NewOpenClawParser(),
	}
}

// Parse parses a skill from a directory, auto-detecting the format
func (p *Parser) Parse(skillDir string) (*ParseResult, error) {
	// Detect format
	format, err := DetectFormat(skillDir)
	if err != nil {
		return nil, fmt.Errorf("failed to detect format: %w", err)
	}

	// Get the skill.md path
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	
	_, err = os.ReadFile(skillMdPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	result := &ParseResult{
		Format: format,
		Data:   make(map[string]interface{}),
	}

	switch format {
	case FormatHermes:
		skill, err := p.HermesParser.Parse(skillDir)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Hermes skill: %w", err)
		}
		result.Name = skill.Name
		result.Content = skill.Content
		result.CodeFiles = skill.CodeFiles
		result.Data["description"] = skill.Description
		result.Data["version"] = skill.Version
		result.Data["author"] = skill.Author
		result.Data["tags"] = skill.Tags

	case FormatOpenClaw:
		skill, err := p.OpenClawParser.Parse(skillDir)
		if err != nil {
			return nil, fmt.Errorf("failed to parse OpenClaw skill: %w", err)
		}
		result.Name = skill.Name
		result.Content = skill.Content
		result.CodeFiles = skill.CodeFiles

	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	return result, nil
}
