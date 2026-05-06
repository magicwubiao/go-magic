package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// ParseYAMLFrontmatter extracts YAML frontmatter from markdown content
// Re-exported from parser for CLI use
func ParseYAMLFrontmatter(content string) (map[string]interface{}, string, error) {
	return parser.ParseYAMLFrontmatter(content)
}

// DetectFormat detects the skill format from a directory
func DetectFormat(skillDir string) (SkillFormat, error) {
	return parser.DetectFormat(skillDir)
}

// SkillFormat represents the format type of a skill
type SkillFormat = parser.SkillFormat

const (
	FormatOpenClaw SkillFormat = parser.FormatOpenClaw
	FormatHermes   SkillFormat = parser.FormatHermes
	FormatMagic    SkillFormat = parser.FormatMagic
	FormatUnknown  SkillFormat = parser.FormatUnknown
)

// Importer handles skill import operations
type Importer struct {
	parser    *parser.Parser
	converter *Converter
	validator *Validator
	manager   *skills.Manager
}

// Parser is a wrapper for detecting and parsing different formats
type Parser struct {
	openclawParser *parser.OpenClawParser
	CortexParser   *parser.HermesParser
}

// NewParser creates a new multi-format parser
func NewParser() *Parser {
	return &Parser{
		openclawParser: parser.NewOpenClawParser(),
		CortexParser:   parser.NewHermesParser(),
	}
}

// NewImporter creates a new importer
func NewImporter(manager *skills.Manager) *Importer {
	return &Importer{
		parser:    NewParser(),
		converter: NewConverter(),
		validator: NewValidator(),
		manager:   manager,
	}
}

// ImportResult holds the result of an import operation
type ImportResult struct {
	Success bool
	Name    string
	Path    string
	Error   error
	Warnings []string
}

// Import imports a skill from a directory
func (i *Importer) Import(skillDir string, force bool) *ImportResult {
	result := &ImportResult{
		Path:     skillDir,
		Warnings: []string{},
	}

	// Validate directory
	validation := i.validator.ValidateSkillDir(skillDir)
	if !validation.Valid {
		result.Success = false
		result.Error = fmt.Errorf("validation failed: %v", validation.Errors)
		return result
	}
	result.Warnings = append(result.Warnings, validation.Warnings...)

	// Detect format
	format, err := parser.DetectFormat(skillDir)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to detect format: %w", err)
		return result
	}

	// Parse based on format
	var skill *skills.Skill
	switch format {
	case parser.FormatOpenClaw:
		openclaw, err := i.parser.openclawParser.Parse(skillDir)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to parse OpenClaw skill: %w", err)
			return result
		}
		skill, err = i.converter.ConvertOpenClaw(openclaw)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to convert OpenClaw skill: %w", err)
			return result
		}

	case parser.FormatHermes:
		hermes, err := i.parser.CortexParser.Parse(skillDir)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to parse Cortex skill: %w", err)
			return result
		}
		skill, err = i.converter.ConvertHermes(hermes)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to convert Cortex skill: %w", err)
			return result
		}

	default:
		// Try to parse as generic format
		files, err := parser.ReadSkillFiles(skillDir)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to read skill files: %w", err)
			return result
		}

		skill, err = i.parseGenericFormat(files)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to parse skill: %w", err)
			return result
		}
	}

	result.Name = skill.Name

	// Check for duplicates
	if !force && i.manager != nil {
		exists, _ := i.validator.CheckDuplicate(skill.Name, i.manager)
		if exists {
			result.Success = false
			result.Error = fmt.Errorf("skill '%s' already exists (use --force to overwrite)", skill.Name)
			return result
		}
	}

	// Determine destination path
	home, err := os.UserHomeDir()
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to get home directory: %w", err)
		return result
	}

	destDir := filepath.Join(home, ".magic", "skills", skill.Name)

	// Check if destination exists
	if !force {
		if _, err := os.Stat(destDir); err == nil {
			result.Success = false
			result.Error = fmt.Errorf("skill '%s' already exists at %s (use --force to overwrite)", skill.Name, destDir)
			return result
		}
	}

	// Copy skill files
	if err := i.copySkillDir(skillDir, destDir); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to copy skill: %w", err)
		return result
	}

	result.Path = destDir
	result.Success = true
	return result
}

// ImportFromFiles imports a skill from a map of files
func (i *Importer) ImportFromFiles(name string, files map[string]string, force bool) *ImportResult {
	result := &ImportResult{
		Name:     name,
		Warnings: []string{},
	}

	// Determine format from files
	var skill *skills.Skill
	var err error

	if openclawFiles, ok := files["SKILL.md"]; ok {
		frontmatter, _, _ := parser.ParseYAMLFrontmatter(openclawFiles)
		if frontmatter != nil {
			if _, ok := frontmatter["trigger_conditions"]; ok {
				openclaw, parseErr := i.parser.openclawParser.ParseFromFiles(files)
				if parseErr == nil {
					skill, err = i.converter.ConvertOpenClaw(openclaw)
				}
			} else if _, ok := frontmatter["hermes"]; ok {
				hermes, parseErr := i.parser.CortexParser.ParseFromFiles(files)
				if parseErr == nil {
					skill, err = i.converter.ConvertHermes(hermes)
				}
			}
		}
	}

	if skill == nil {
		// Generic format
		skill = &skills.Skill{
			Name:    name,
			Content: files["SKILL.md"],
		}
	}

	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to parse skill: %w", err)
		return result
	}

	result.Name = skill.Name

	// Save to skills directory
	home, err := os.UserHomeDir()
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to get home directory: %w", err)
		return result
	}

	destDir := filepath.Join(home, ".magic", "skills", skill.Name)

	// Create directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to create skill directory: %w", err)
		return result
	}

	// Write files
	for filename, content := range files {
		path := filepath.Join(destDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to write %s: %w", filename, err)
			return result
		}
	}

	result.Path = destDir
	result.Success = true
	return result
}

// ImportRecursive imports all skills from a directory
func (i *Importer) ImportRecursive(skillsDir string, force bool) []*ImportResult {
	results := []*ImportResult{}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return []*ImportResult{{
			Success: false,
			Error:   fmt.Errorf("failed to read directory: %w", err),
		}}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(skillsDir, entry.Name())
		result := i.Import(skillDir, force)
		results = append(results, result)
	}

	return results
}

// parseGenericFormat parses a skill without known format
func (i *Importer) parseGenericFormat(files map[string]string) (*skills.Skill, error) {
	skillMd, ok := files["SKILL.md"]
	if !ok {
		return nil, fmt.Errorf("SKILL.md not found")
	}

	frontmatter, content, err := parser.ParseYAMLFrontmatter(skillMd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skill := &skills.Skill{
		Content: content,
		Source:  "imported",
	}

	if frontmatter != nil {
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
		if tags, ok := frontmatter["tags"].([]string); ok {
			skill.Tags = tags
		}
		if tools, ok := frontmatter["tools"].([]string); ok {
			skill.Tools = tools
		}
	}

	// Add code files to content
	var codeBuilder strings.Builder
	for filename, code := range files {
		if filename == "SKILL.md" {
			continue
		}
		ext := filepath.Ext(filename)
		codeBuilder.WriteString(fmt.Sprintf("\n## %s\n\n```%s\n%s\n```\n", filename, ext, code))
	}
	skill.Content += codeBuilder.String()

	return skill, nil
}

// copySkillDir copies a skill directory to destination
func (i *Importer) copySkillDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}

// ListAvailableSkills lists skills available in a directory (for preview before import)
func (i *Importer) ListAvailableSkills(skillsDir string) ([]*AvailableSkill, error) {
	var available []*AvailableSkill

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(skillsDir, entry.Name())
		skillMdPath := filepath.Join(skillDir, "SKILL.md")

		if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
			continue // Skip directories without SKILL.md
		}

		format, _ := parser.DetectFormat(skillDir)

		// Try to get skill name
		data, _ := os.ReadFile(skillMdPath)
		frontmatter, _, _ := parser.ParseYAMLFrontmatter(string(data))

		name := entry.Name()
		description := ""
		if frontmatter != nil {
			if n, ok := frontmatter["name"].(string); ok {
				name = n
			}
			if d, ok := frontmatter["description"].(string); ok {
				description = d
			}
		}

		available = append(available, &AvailableSkill{
			Name:        name,
			Path:        skillDir,
			Format:      string(format),
			Description: description,
		})
	}

	return available, nil
}

// AvailableSkill represents a skill available for import
type AvailableSkill struct {
	Name        string
	Path        string
	Format      string
	Description string
}

// ImportFromURL imports a skill from a URL
func (i *Importer) ImportFromURL(url string) (*ImportResult, error) {
	return ImportFromURLWithManager(i.manager, url, false)
}

// ImportFromURLWithManager imports a skill from a URL with explicit manager
func ImportFromURLWithManager(manager *skills.Manager, url string, force bool) (*ImportResult, error) {
	result := DownloadAndImport(manager, url, force)
	if result == nil {
		return nil, errors.New("import returned nil result")
	}
	return result, nil
}

// ImportFromURLRecursive imports all skills from a URL directory
func (i *Importer) ImportFromURLRecursive(url string) ([]*ImportResult, error) {
	return ImportFromURLRecursiveWithManager(i.manager, url, false)
}

// ImportFromURLRecursiveWithManager imports all skills from a URL directory with explicit manager
func ImportFromURLRecursiveWithManager(manager *skills.Manager, url string, force bool) ([]*ImportResult, error) {
	results := DownloadAndImportRecursive(manager, url, force)
	if results == nil {
		return nil, errors.New("import returned nil results")
	}
	return results, nil
}
