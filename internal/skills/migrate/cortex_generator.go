package migrate

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// HermesGenerator generates Hermes Agent SKILL.md format
type HermesGenerator struct{}

// NewHermesGenerator creates a new Hermes generator
func NewHermesGenerator() *HermesGenerator {
	return &HermesGenerator{}
}

// Generate generates Hermes SKILL.md content from source format
func (g *HermesGenerator) Generate(format parser.SkillFormat, frontmatter map[string]interface{}, content string, defaultName string) (string, []string, error) {
	var warnings []string

	// Build frontmatter
	frontmatterBuilder := &strings.Builder{}
	frontmatterBuilder.WriteString("---\n")

	// Name
	name := g.getString(frontmatter, "name", defaultName)
	frontmatterBuilder.WriteString(fmt.Sprintf("name: %s\n", sanitizeName(name)))

	// Description
	description := g.getString(frontmatter, "description", "Migrated skill")
	frontmatterBuilder.WriteString(fmt.Sprintf("description: %q\n", description))

	// Version
	version := g.getString(frontmatter, "version", "1.0.0")
	frontmatterBuilder.WriteString(fmt.Sprintf("version: %s\n", version))

	// Author
	if author := g.getString(frontmatter, "author", ""); author != "" {
		frontmatterBuilder.WriteString(fmt.Sprintf("author: %s\n", author))
	}

	// License
	if license := g.getString(frontmatter, "license", ""); license != "" {
		frontmatterBuilder.WriteString(fmt.Sprintf("license: %s\n", license))
	}

	// Tags
	tags := g.getTags(frontmatter)
	if len(tags) > 0 {
		frontmatterBuilder.WriteString(fmt.Sprintf("tags: [%s]\n", strings.Join(tags, ", ")))
	}

	// Tools
	tools := g.getTools(frontmatter)
	if len(tools) > 0 {
		frontmatterBuilder.WriteString(fmt.Sprintf("tools: [%s]\n", strings.Join(tools, ", ")))
	}

	// Hermes metadata block
	frontmatterBuilder.WriteString("metadata:\n")
	frontmatterBuilder.WriteString("  hermes:\n")
	frontmatterBuilder.WriteString("    tags: []\n")
	frontmatterBuilder.WriteString("    tools: []\n")

	// Migrated info
	frontmatterBuilder.WriteString(fmt.Sprintf("    migrated_from: %s\n", format))
	frontmatterBuilder.WriteString(fmt.Sprintf("    migrated_at: %s\n", time.Now().Format(time.RFC3339)))

	// Add source-specific metadata
	switch format {
	case parser.FormatOpenClaw:
		if triggers := g.getStringArray(frontmatter, "trigger_conditions"); len(triggers) > 0 {
			frontmatterBuilder.WriteString("    original_triggers:\n")
			for _, t := range triggers {
				frontmatterBuilder.WriteString(fmt.Sprintf("      - %q\n", t))
			}
			warnings = append(warnings, "trigger_conditions converted to metadata (Hermes uses different trigger system)")
		}
		if steps := g.getStringArray(frontmatter, "steps"); len(steps) > 0 {
			frontmatterBuilder.WriteString("    original_steps:\n")
			for _, s := range steps {
				frontmatterBuilder.WriteString(fmt.Sprintf("      - %q\n", s))
			}
			warnings = append(warnings, "steps converted to metadata (consider rewriting as guidance sections)")
		}
	}

	frontmatterBuilder.WriteString("---\n\n")

	// Generate improved content
	generatedContent := g.generateContent(format, frontmatter, content, name)
	frontmatterBuilder.WriteString(generatedContent)

	return frontmatterBuilder.String(), warnings, nil
}

// getString extracts a string from frontmatter
func (g *HermesGenerator) getString(fm map[string]interface{}, key, defaultVal string) string {
	if val, ok := fm[key].(string); ok && val != "" {
		return val
	}
	return defaultVal
}

// getTags extracts tags from frontmatter
func (g *HermesGenerator) getTags(fm map[string]interface{}) []string {
	if tags, ok := fm["tags"].([]string); ok {
		return tags
	}
	if tagsStr, ok := fm["tags"].(string); ok {
		return parseInlineTags(tagsStr)
	}
	if hermes, ok := fm["hermes"].(map[string]interface{}); ok {
		if tags, ok := hermes["tags"].([]string); ok {
			return tags
		}
	}
	return nil
}

// getTools extracts tools from frontmatter
func (g *HermesGenerator) getTools(fm map[string]interface{}) []string {
	if tools, ok := fm["tools"].([]string); ok {
		return tools
	}
	if toolsStr, ok := fm["tools"].(string); ok {
		return parseInlineTags(toolsStr)
	}
	if hermes, ok := fm["hermes"].(map[string]interface{}); ok {
		if tools, ok := hermes["tools"].([]string); ok {
			return tools
		}
	}
	return nil
}

// getStringArray extracts a string array from frontmatter
func (g *HermesGenerator) getStringArray(fm map[string]interface{}, key string) []string {
	if arr, ok := fm[key].([]string); ok {
		return arr
	}
	if str, ok := fm[key].(string); ok {
		return parseInlineTags(str)
	}
	return nil
}

// parseInlineTags parses inline array syntax like "[tag1, tag2]"
func parseInlineTags(s string) []string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = s[1 : len(s)-1]
	}

	var tags []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"")
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

// generateContent generates improved markdown content
func (g *HermesGenerator) generateContent(format parser.SkillFormat, frontmatter map[string]interface{}, originalContent, name string) string {
	var sb strings.Builder

	// Add skill title
	sb.WriteString(fmt.Sprintf("# %s\n\n", sanitizeTitle(name)))

	// Add description
	if desc := g.getString(frontmatter, "description", ""); desc != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", desc))
	}

	// Process original content
	if originalContent != "" {
		processed := g.processContent(originalContent)
		sb.WriteString(processed)
	}

	// Add migration notes
	sb.WriteString("\n\n---\n\n")
	sb.WriteString("## Migration Notes\n\n")
	sb.WriteString(fmt.Sprintf("- Migrated from: %s\n", format))
	sb.WriteString(fmt.Sprintf("- Migration date: %s\n", time.Now().Format("2006-01-02")))

	// Add original metadata reference
	if author := g.getString(frontmatter, "author", ""); author != "" {
		sb.WriteString(fmt.Sprintf("- Original author: %s\n", author))
	}

	return sb.String()
}

// processContent processes and improves original markdown content
func (g *HermesGenerator) processContent(content string) string {
	var sb strings.Builder

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Skip duplicate title
		if i == 0 && strings.HasPrefix(line, "#") {
			continue
		}

		// Skip very short lines that might be artifacts
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 2 && trimmed != "" {
			continue
		}

		// Convert OpenClaw trigger sections to Hermes format
		if strings.Contains(strings.ToLower(trimmed), "trigger") {
			// Skip trigger sections, they'll be in metadata
			if strings.Contains(strings.ToLower(trimmed), "##") || strings.Contains(strings.ToLower(trimmed), "#") {
				sb.WriteString("## When to Use\n\n")
				continue
			}
		}

		// Convert steps sections
		if strings.Contains(strings.ToLower(trimmed), "step") && !strings.Contains(trimmed, "#") {
			continue // Skip numbered steps, they'll be in metadata
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String()
}

// sanitizeName ensures the name is a valid identifier
func sanitizeName(name string) string {
	// Replace spaces and special chars with hyphens
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	name = re.ReplaceAllString(name, "-")
	// Convert to lowercase
	name = strings.ToLower(name)
	// Remove leading/trailing hyphens
	name = strings.Trim(name, "-")
	// Limit length
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}

// sanitizeTitle creates a proper title from name
func sanitizeTitle(name string) string {
	// Convert hyphens and underscores to spaces
	title := strings.ReplaceAll(name, "-", " ")
	title = strings.ReplaceAll(title, "_", " ")
	// Capitalize words
	words := strings.Fields(title)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

// GenerateMinimal generates a minimal Hermes SKILL.md
func (g *HermesGenerator) GenerateMinimal(name, description string) string {
	return fmt.Sprintf(`---
name: %s
description: %q
version: 1.0.0
tags: []
tools: []
metadata:
  hermes:
    tags: []
    tools: []
---

# %s

%s

## Usage

Load this skill when needed.

## Examples

Add examples here.
`, sanitizeName(name), description, sanitizeTitle(name), description)
}
