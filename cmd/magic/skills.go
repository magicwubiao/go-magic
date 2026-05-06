package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills"
)

// Skill metadata for templates
type SkillTemplate struct {
	Name        string
	Description string
	Tags        []string
	Tools       []string
	Content     string
}

var (
	skillCreateForce bool
	skillInstallFrom string
	skillInstallURL  string
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage skills",
	Long: `Manage magic Agent skills.

Skills are loaded from three levels:
  - Built-in skills: bundled with the application
  - Global skills: ~/.magic/skills/
  - Workspace skills: ./skills/ or .magic/skills/

Supports multiple formats:
  - SKILL.md with YAML frontmatter (recommended)
  - JSON (.json) - with name, description, content
  - Markdown (.md, .markdown) - content as skill
  - Text (.txt) - plain text as skill
  - Directory with manifest.json

Examples:
  magic skills list
  magic skills show <name>
  magic skills search <keyword>
  magic skills install <name>
  magic skills create <name>
  magic skills delete <name>
  magic skills match <input>`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all skills",
	Run:   runSkillsList,
}

var skillsShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a skill",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsShow,
}

var skillsSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search skills by keyword",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsSearch,
}

var skillsInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install a skill",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsInstall,
}

var skillsCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new skill from template",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsCreate,
}

var skillsDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a skill",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsDelete,
}

var skillsMatchCmd = &cobra.Command{
	Use:   "match <input>",
	Short: "Find skills matching the input",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsMatch,
}

func init() {
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsShowCmd)
	skillsCmd.AddCommand(skillsSearchCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsCreateCmd)
	skillsCmd.AddCommand(skillsDeleteCmd)
	skillsCmd.AddCommand(skillsMatchCmd)
	// migrate command is added in skill_migrate.go

	skillsCreateCmd.Flags().BoolVarP(&skillCreateForce, "force", "f", false, "Overwrite if skill exists")
	skillsInstallCmd.Flags().StringVar(&skillInstallFrom, "from", "", "Source path or URL")

	rootCmd.AddCommand(skillsCmd)
}

func runSkillsList(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	skillList := mgr.List()
	if len(skillList) == 0 {
		fmt.Println("No skills found.")
		fmt.Println("Skills directories:")
		fmt.Println("  ~/.magic/skills/ (global)")
		fmt.Println("  ./skills/ (workspace)")
		fmt.Println("  .magic/skills/ (workspace)")
		return
	}

	fmt.Printf("Found %d skills:\n\n", len(skillList))

	// Group by source
	bySource := make(map[string][]*skills.Skill)
	for _, s := range skillList {
		source := s.Source
		if source == "" {
			source = "local"
		}
		bySource[source] = append(bySource[source], s)
	}

	for source, list := range bySource {
		fmt.Printf("## %s\n", strings.ToUpper(source))
		for _, s := range list {
			tags := ""
			if len(s.Tags) > 0 {
				tags = fmt.Sprintf(" [%s]", strings.Join(s.Tags, ", "))
			}
			fmt.Printf("  • %s: %s%s\n", s.Name, s.Description, tags)
		}
		fmt.Println()
	}
}

func runSkillsShow(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	skill, err := mgr.Get(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Name:        %s\n", skill.Name)
	fmt.Printf("Description: %s\n", skill.Description)
	fmt.Printf("Version:     %s\n", skill.Version)
	fmt.Printf("Author:      %s\n", skill.Author)
	fmt.Printf("Source:      %s\n", skill.Source)

	if len(skill.Tags) > 0 {
		fmt.Printf("Tags:        %s\n", strings.Join(skill.Tags, ", "))
	}
	if len(skill.Tools) > 0 {
		fmt.Printf("Tools:       %s\n", strings.Join(skill.Tools, ", "))
	}

	fmt.Println("\n--- Content ---")
	fmt.Println(skill.Content)
}

func runSkillsSearch(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	keyword := args[0]
	results := mgr.Search(keyword)

	if len(results) == 0 {
		fmt.Printf("No skills found matching '%s'\n", keyword)
		return
	}

	fmt.Printf("Found %d skills matching '%s':\n\n", len(results), keyword)
	for _, s := range results {
		tags := ""
		if len(s.Tags) > 0 {
			tags = fmt.Sprintf(" [%s]", strings.Join(s.Tags, ", "))
		}
		fmt.Printf("  • %s: %s%s\n", s.Name, s.Description, tags)
	}
}

func runSkillsInstall(cmd *cobra.Command, args []string) {
	name := args[0]
	from := skillInstallFrom

	// Check if already installed
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	if _, err := mgr.Get(name); err == nil {
		fmt.Printf("Skill '%s' is already installed.\n", name)
		fmt.Println("Use 'magic skills delete " + name + "' first to reinstall.")
		return
	}

	// Determine source
	if from != "" {
		// Install from local path or URL
		installSkillFromPath(name, from)
	} else {
		// Try to install from registry
		fmt.Printf("Installing skill '%s' from registry...\n", name)
		fmt.Println("Note: Registry installation not yet implemented.")
		fmt.Println("Place skill files in:")
		fmt.Println("  ~/.magic/skills/")
	}
}

func installSkillFromPath(name, path string) {
	// Check if source exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		fmt.Printf("Path not found: %s\n", path)
		os.Exit(1)
	}

	// Create skills directory
	home, _ := os.UserHomeDir()
	skillsDir := filepath.Join(home, ".magic", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		fmt.Printf("Failed to create skills directory: %v\n", err)
		os.Exit(1)
	}

	// Determine destination
	var dest string
	if info.IsDir() {
		dest = filepath.Join(skillsDir, name)
	} else {
		dest = filepath.Join(skillsDir, filepath.Base(path))
	}

	// Check if already exists
	if _, err := os.Stat(dest); err == nil {
		fmt.Printf("Skill '%s' already exists at: %s\n", name, dest)
		fmt.Println("Use --force to overwrite.")
		return
	}

	// Copy file or directory
	fmt.Printf("Installing skill '%s'...\n", name)
	fmt.Printf("  From: %s\n", path)
	fmt.Printf("  To:   %s\n", dest)

	if info.IsDir() {
		if err := copyDir(path, dest); err != nil {
			fmt.Printf("Failed to copy directory: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := copyFile(path, dest); err != nil {
			fmt.Printf("Failed to copy file: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("\n✓ Skill installed successfully!")
}

func runSkillsCreate(cmd *cobra.Command, args []string) {
	name := args[0]

	// Create skills directory
	home, _ := os.UserHomeDir()
	skillsDir := filepath.Join(home, ".magic", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		fmt.Printf("Failed to create skills directory: %v\n", err)
		os.Exit(1)
	}

	// Create skill directory
	skillDir := filepath.Join(skillsDir, name)
	if _, err := os.Stat(skillDir); err == nil {
		if !skillCreateForce {
			fmt.Printf("Skill '%s' already exists at %s\n", name, skillDir)
			fmt.Println("Use --force to overwrite.")
			return
		}
		os.RemoveAll(skillDir)
	}

	os.MkdirAll(skillDir, 0755)

	// Create SKILL.md template
	template := getSkillTemplate(name)
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillMdPath, []byte(template), 0644); err != nil {
		fmt.Printf("Failed to create SKILL.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Created skill '%s' at %s\n", name, skillDir)
	fmt.Printf("  Edit %s to customize\n", skillMdPath)
}

func getSkillTemplate(name string) string {
	return fmt.Sprintf(`---
name: %s
description: "Describe what this skill does"
version: 1.0.0
author: your-name
tags: [tag1, tag2]
tools: []
---

# %s Skill

## When to Use

Load this skill when:
- Scenario 1 where this skill is useful
- Scenario 2 where this skill is useful

## How It Works

Describe the workflow and steps.

## Examples

### Example 1
Describe an example use case.

## Tips

- Tip 1
- Tip 2
`, name, name)
}

func runSkillsDelete(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	err = mgr.Remove(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Skill '%s' deleted.\n", args[0])
}

func runSkillsMatch(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	input := args[0]
	results := mgr.MatchSkillsByInput(input)

	if len(results) == 0 {
		fmt.Printf("No skills found matching '%s'\n", input)
		return
	}

	fmt.Printf("Found %d skills matching '%s':\n\n", len(results), input)
	for _, s := range results {
		fmt.Printf("  • %s: %s\n", s.Name, s.Description)
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func copyDir(src, dst string) error {
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
		return os.WriteFile(destPath, data, 0644)
	})
}
