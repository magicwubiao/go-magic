package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult holds the result of validation
type ValidationResult struct {
	Valid  bool
	Errors []error
	Warnings []string
}

// Validator validates skills before import
type Validator struct {
	allowedTools []string
}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{}
}

// SetAllowedTools sets the list of allowed tool names
func (v *Validator) SetAllowedTools(tools []string) {
	v.allowedTools = tools
}

// ValidateSkillDir validates a skill directory
func (v *Validator) ValidateSkillDir(skillDir string) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []error{},
		Warnings: []string{},
	}

	// Check if directory exists
	info, err := os.Stat(skillDir)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("skill directory does not exist: %w", err))
		return result
	}

	if !info.IsDir() {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("path is not a directory"))
		return result
	}

	// Check for SKILL.md
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("SKILL.md not found"))
		return result
	}

	// Read and parse SKILL.md
	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("failed to read SKILL.md: %w", err))
		return result
	}

	frontmatter, _, err := parser.ParseYAMLFrontmatter(string(data))
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("failed to parse frontmatter: %w", err))
		return result
	}

	// Validate required fields
	if frontmatter == nil {
		result.Warnings = append(result.Warnings, "No YAML frontmatter found")
	} else {
		v.validateFrontmatter(frontmatter, result)
	}

	// Validate code files
	v.validateCodeFiles(skillDir, result)

	return result
}

// validateFrontmatter validates the YAML frontmatter
func (v *Validator) validateFrontmatter(frontmatter map[string]interface{}, result *ValidationResult) {
	// Check name
	if name, ok := frontmatter["name"].(string); !ok || name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, &ValidationError{
			Field:   "name",
			Message: "skill name is required",
		})
	}

	// Check description
	if desc, ok := frontmatter["description"].(string); !ok || desc == "" {
		result.Valid = false
		result.Errors = append(result.Errors, &ValidationError{
			Field:   "description",
			Message: "skill description is required",
		})
	}

	// Validate version format if present
	if version, ok := frontmatter["version"].(string); ok {
		if !isValidVersion(version) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("version '%s' may not follow semantic versioning", version))
		}
	}

	// Validate tools if present
	if tools, ok := frontmatter["tools"].([]string); ok {
		v.validateTools(tools, result)
	} else if toolsStr, ok := frontmatter["tools"].(string); ok {
		tools := parseInlineArray(strings.Trim(toolsStr, "[]"))
		v.validateTools(tools, result)
	}
}

// validateTools checks if all tools are allowed
func (v *Validator) validateTools(tools []string, result *ValidationResult) {
	if len(v.allowedTools) == 0 {
		return // No restrictions
	}

	toolSet := make(map[string]bool)
	for _, t := range v.allowedTools {
		toolSet[t] = true
	}

	for _, tool := range tools {
		if !toolSet[tool] {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("tool '%s' may not be available in this environment", tool))
		}
	}
}

// validateCodeFiles validates code files in the skill directory
func (v *Validator) validateCodeFiles(skillDir string, result *ValidationResult) {
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		result.Warnings = append(result.Warnings, "could not read skill directory")
		return
	}

	codeFileCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if parser.IsCodeFile(ext) {
			codeFileCount++

			// Check for potentially dangerous files
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("hidden file '%s' will be included", name))
			}
		}
	}

	if codeFileCount > 10 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("skill contains %d code files, which may be excessive", codeFileCount))
	}
}

// ValidateParsedSkill validates a parsed skill
func (v *Validator) ValidateParsedSkill(skill interface{}) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []error{},
		Warnings: []string{},
	}

	switch s := skill.(type) {
	case *parser.OpenClawSkill:
		if err := s.Validate(); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err)
		}
		v.validateParsedFields(s.Name, s.Description, result)

	case *parser.HermesSkill:
		if err := s.Validate(); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err)
		}
		v.validateParsedFields(s.Name, s.Description, result)

	default:
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("unknown skill type"))
	}

	return result
}

// validateParsedFields validates common fields
func (v *Validator) validateParsedFields(name, description string, result *ValidationResult) {
	// Name validation
	if name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, &ValidationError{
			Field:   "name",
			Message: "skill name is required",
		})
	} else if len(name) > 100 {
		result.Warnings = append(result.Warnings, "skill name is very long (>100 chars)")
	}

	// Description validation
	if description == "" {
		result.Valid = false
		result.Errors = append(result.Errors, &ValidationError{
			Field:   "description",
			Message: "skill description is required",
		})
	} else if len(description) > 500 {
		result.Warnings = append(result.Warnings, "description is very long (>500 chars)")
	}
}

// CheckDuplicate checks if a skill with the same name already exists
func (v *Validator) CheckDuplicate(name string, manager *skills.Manager) (bool, error) {
	if manager == nil {
		return false, nil
	}

	_, err := manager.Get(name)
	if err != nil {
		// Skill not found, no duplicate
		return false, nil
	}

	return true, nil
}

// isValidVersion checks if version follows semver
func isValidVersion(version string) bool {
	// Simple check for semver-like pattern: major.minor.patch
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}

	for _, part := range parts {
		if len(part) == 0 {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}

	return true
}

// parseInlineArray parses a comma-separated inline array
func parseInlineArray(content string) []string {
	var result []string
	depth := 0
	var current strings.Builder

	for _, ch := range content {
		switch ch {
		case '{', '[':
			depth++
			current.WriteRune(ch)
		case '}', ']':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				item := strings.TrimSpace(current.String())
				item = strings.Trim(item, "\"")
				if item != "" {
					result = append(result, item)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Don't forget the last item
	item := strings.TrimSpace(current.String())
	item = strings.Trim(item, "\"")
	if item != "" {
		result = append(result, item)
	}

	return result
}
