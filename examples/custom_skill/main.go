// Package main provides an example of creating a custom skill
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	fmt.Println("=== Custom Skill Creation Example ===")
	
	// Example skill structure
	createExampleSkill()
	
	fmt.Println("\nCustom skill created successfully!")
}

func createExampleSkill() {
	// Create skill directory
	skillDir := "./skills/custom-readme-generator"
	os.MkdirAll(skillDir, 0755)
	
	// Create meta.json
	meta := `{
  "id": "custom-readme-generator",
  "name": "README Generator",
  "description": "Automatically generates README.md files for projects",
  "author": "custom",
  "created_at": "` + timestamp() + `",
  "level": "Level 2",
  "tags": ["documentation", "automation"],
  "tools": ["read_file", "glob", "write_file"]
}`
	
	os.WriteFile(filepath.Join(skillDir, "meta.json"), []byte(meta), 0644)
	
	// Create SKILL.md
	skill := `# README Generator

**Custom Skill** | Automated README generation

## What this skill does

This skill automatically generates comprehensive README.md files for projects by analyzing the project structure and generating appropriate documentation sections.

## Level 1: Complete Usage Guide

### Tool Sequence

This skill uses the following tool sequence:

1. ` + "`glob`" + ` - Find all source files in the project
2. ` + "`read_file`" + ` - Read key files (package.json, setup.py, etc.)
3. ` + "`write_file`" + ` - Generate the README.md file

### How to Use

When you need to create documentation for a project:

1. First, call glob to discover the project structure
2. Read key configuration files to understand the project
3. Generate README sections based on findings
4. Write the final README.md

### Example Tasks

- Generate README for a new Python project
- Update README with new features
- Add installation instructions to existing README

---

## Level 2: Reference and Tips

### Common Sections to Include

- Project title and description
- Installation instructions
- Usage examples
- Configuration options
- Contributing guidelines
- License information

### Optimization Tips

- Parse existing config files for accurate version info
- Detect test framework for test instructions
- Identify package manager for install commands

---

*This skill was created by analyzing recurring documentation patterns.*
`
	
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0644)
}

func timestamp() string {
	return strings.Replace("2024-01-01T00:00:00Z", " ", "T", 1)
}
