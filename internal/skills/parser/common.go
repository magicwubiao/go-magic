package parser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillFormat represents the format type of a skill
type SkillFormat string

const (
	FormatOpenClaw   SkillFormat = "openclaw"
	FormatHermes     SkillFormat = "hermes"
	FormatMagic      SkillFormat = "magic"
	FormatUnknown    SkillFormat = "unknown"
)

// ParseResult holds the result of parsing a skill
type ParseResult struct {
	Format   SkillFormat
	Name     string
	Data     map[string]interface{}
	Content  string
	CodeFiles map[string]string // filename -> content
	RawFrontmatter string
}

// ParseYAMLFrontmatter extracts YAML frontmatter from markdown content
// Returns the frontmatter map and the remaining content
func ParseYAMLFrontmatter(content string) (map[string]interface{}, string, error) {
	content = strings.TrimPrefix(content, "\xef\xbb\xbf") // Remove BOM if present

	if !strings.HasPrefix(content, "---") {
		return nil, content, nil
	}

	// Find the closing ---
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return nil, content, nil
	}

	var frontmatterLines []string
	contentLines := []string{}
	inFrontmatter := false

	for i, line := range lines {
		if i == 0 {
			continue // Skip first "---"
		}

		if strings.HasPrefix(line, "---") && !inFrontmatter {
			inFrontmatter = true
			continue
		}

		if strings.HasPrefix(line, "---") && inFrontmatter {
			// End of frontmatter
			contentLines = lines[i+1:]
			break
		}

		if inFrontmatter {
			frontmatterLines = append(frontmatterLines, line)
		} else {
			contentLines = append(contentLines, line)
		}
	}

	if len(frontmatterLines) == 0 {
		return nil, strings.TrimSpace(strings.Join(lines[1:], "\n")), nil
	}

	frontmatter := parseSimpleYAML(strings.Join(frontmatterLines, "\n"))
	remainder := strings.TrimSpace(strings.Join(contentLines, "\n"))

	return frontmatter, remainder, nil
}

// parseSimpleYAML parses simple YAML key-value pairs
// Supports: strings, arrays, nested objects (limited)
func parseSimpleYAML(content string) map[string]interface{} {
	result := make(map[string]interface{})
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentMap map[string]interface{}
	var currentKey string

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Calculate indentation
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		content := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(content, "#") {
			continue
		}

		// Determine if it's a key-value pair or array item
		if strings.Contains(content, ":") && !strings.HasPrefix(content, "-") {
			// Key-value pair
			parts := strings.SplitN(content, ":", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			if indent == 0 {
				currentMap = result
			}

			// Handle different value types
			if value == "" || value == "|" || value == ">" {
				// This might be a nested object or multi-line value
				currentKey = key
				if indent == 0 {
					currentMap = result
				}
			} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
				// Inline array
				arrayContent := strings.Trim(value, "[]")
				if arrayContent == "" {
					currentMap[key] = []string{}
				} else {
					items := parseInlineArray(arrayContent)
					currentMap[key] = items
				}
			} else if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
				// Inline object - store as string for now
				currentMap[key] = value
			} else {
				// String value - handle quoted strings
				currentMap[key] = strings.Trim(value, "\"")
			}
		} else if strings.HasPrefix(content, "-") {
			// Array item
			item := strings.TrimPrefix(content, "-")
			item = strings.TrimSpace(item)

			// Get the array key
			arrKey := currentKey
			if arr, ok := currentMap[arrKey].([]string); ok {
				currentMap[arrKey] = append(arr, item)
			} else {
				currentMap[arrKey] = []string{item}
			}
		}
	}

	return result
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

// DetectFormat detects the skill format from a directory
func DetectFormat(skillDir string) (SkillFormat, error) {
	skillMdPath := filepath.Join(skillDir, "SKILL.md")

	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return FormatUnknown, fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	frontmatter, _, err := ParseYAMLFrontmatter(string(data))
	if err != nil {
		return FormatUnknown, err
	}

	if frontmatter == nil {
		// No frontmatter, check for hermes markers
		if strings.Contains(string(data), "hermes") {
			return FormatHermes, nil
		}
		return FormatMagic, nil
	}

	// Detect based on frontmatter fields
	if _, ok := frontmatter["trigger_conditions"]; ok {
		return FormatOpenClaw, nil
	}

	if _, ok := frontmatter["hermes"]; ok {
		return FormatHermes, nil
	}

	// Check metadata.hermes
	if meta, ok := frontmatter["metadata"].(map[string]interface{}); ok {
		if _, ok := meta["hermes"]; ok {
			return FormatHermes, nil
		}
	}

	// Check for OpenClaw-style fields
	if _, ok := frontmatter["steps"]; ok {
		return FormatOpenClaw, nil
	}

	return FormatMagic, nil
}

// ReadSkillFiles reads all relevant files from a skill directory
func ReadSkillFiles(skillDir string) (map[string]string, error) {
	files := make(map[string]string)

	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		name := entry.Name()

		// Include SKILL.md and code files
		if name == "SKILL.md" || isCodeFile(ext) {
			path := filepath.Join(skillDir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %w", name, err)
			}
			files[name] = string(data)
		}
	}

	return files, nil
}

// isCodeFile checks if a file extension is a code file
func isCodeFile(ext string) bool {
	codeExts := []string{
		".go", ".py", ".js", ".ts", ".tsx", ".jsx",
		".sh", ".bash", ".zsh",
		".rb", ".java", ".kt", ".swift",
		".rs", ".c", ".cpp", ".h", ".hpp",
		".cs", ".php", ".lua", ".pl",
	}
	ext = strings.ToLower(ext)
	for _, codeExt := range codeExts {
		if ext == codeExt {
			return true
		}
	}
	return false
}

// GetCodeLanguage determines the programming language from file extension
func GetCodeLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".py":
		return "python"
	case ".js", ".ts", ".tsx", ".jsx":
		return "typescript"
	case ".go":
		return "go"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".kt":
		return "kotlin"
	case ".swift":
		return "swift"
	case ".rs":
		return "rust"
	case ".c", ".cpp", ".h", ".hpp":
		return "c"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".lua":
		return "lua"
	case ".pl":
		return "perl"
	default:
		return "unknown"
	}
}
