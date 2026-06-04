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
	Long: `Manage Magic Agent skills.

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

// Hub management commands
var skillsUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a hub skill",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsUninstall,
}

var skillsDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a skill (won't be shown to agent)",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsDisable,
}

var skillsEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a disabled skill",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsEnable,
}

var skillsHubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Manage hub skills",
	Long: `Manage hub-installed skills.

Examples:
  magic skills hub list
  magic skills hub audit`,
}

var skillsHubListCmd = &cobra.Command{
	Use:   "list",
	Short: "List hub-installed skills",
	Run:   runSkillsHubList,
}

var skillsHubAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Show hub audit log",
	Run:   runSkillsHubAudit,
}

var skillsConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage skill configuration",
	Long: `Manage skill configuration (disabled skills, etc.)

Examples:
  magic skills config list
  magic skills config disabled`,
}

var skillsConfigListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration",
	Run:   runSkillsConfigList,
}

var skillsConfigDisabledCmd = &cobra.Command{
	Use:   "disabled",
	Short: "List disabled skills",
	Run:   runSkillsConfigDisabled,
}

func init() {
	skillsCmd.AddCommand(skillsShowCmd)
	skillsCmd.AddCommand(skillsSearchCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsUninstallCmd)
	skillsCmd.AddCommand(skillsCreateCmd)
	skillsCmd.AddCommand(skillsDeleteCmd)
	skillsCmd.AddCommand(skillsMatchCmd)
	skillsCmd.AddCommand(skillsDisableCmd)
	skillsCmd.AddCommand(skillsEnableCmd)
	skillsCmd.AddCommand(skillsHubCmd)
	skillsCmd.AddCommand(skillsConfigCmd)

	// Hub subcommands
	skillsHubCmd.AddCommand(skillsHubListCmd)
	skillsHubCmd.AddCommand(skillsHubAuditCmd)

	// Config subcommands
	skillsConfigCmd.AddCommand(skillsConfigListCmd)
	skillsConfigCmd.AddCommand(skillsConfigDisabledCmd)

	// migrate command is added in skill_migrate.go
	// list command is added in skill_list.go

	skillsCreateCmd.Flags().BoolVarP(&skillCreateForce, "force", "f", false, "Overwrite if skill exists")
	skillsInstallCmd.Flags().StringVar(&skillInstallFrom, "from", "", "Source path or URL")

	rootCmd.AddCommand(skillsCmd)
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
	fmt.Printf("Source:      %s\n", string(skill.Source))

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

	// Determine source and install
	if from != "" {
		// Install from local path or URL
		installSkillFromPath(name, from)
	} else {
		// Try to install from official Hermes registry
		fmt.Printf("Installing skill '%s' from Hermes official registry...\n", name)

		// 使用 Hub 安装
		err := mgr.InstallFromHub(skills.HubSourceOfficial, name)
		if err != nil {
			fmt.Printf("Failed to install from registry: %v\n", err)
			fmt.Println("\nSupported sources:")
			fmt.Println("  - GitHub: magic skills install my-skill --from github:owner/repo/path")
			fmt.Println("  - URL: magic skills install my-skill --from https://example.com/skill.zip")
			fmt.Println("  - skills.sh: magic skills install my-skill --from skills.sh/name")
			fmt.Println("  - Local: magic skills install my-skill --from /path/to/skill")
			os.Exit(1)
		}

		fmt.Println("\n✓ Skill installed successfully from registry!")
	}
}

func installSkillFromPath(name, path string) {
	// Check if it's a URL (remote source)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "github:") || strings.HasPrefix(path, "skills.sh/") {
		installSkillFromURL(name, path)
		return
	}

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
		if err := copyDirMigrate(path, dest); err != nil {
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

// installSkillFromURL 从远程 URL 安装技能
func installSkillFromURL(name, url string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Installing skill '%s' from remote source...\n", name)
	fmt.Printf("  URL: %s\n", url)

	// 解析 URL 类型并确定 HubSource
	var source skills.HubSource
	switch {
	case strings.HasPrefix(url, "github:"):
		source = skills.HubSourceGitHub
		url = strings.TrimPrefix(url, "github:")
		url = "https://github.com/" + url
	case strings.HasPrefix(url, "skills.sh/"):
		source = skills.HubSourceSkillsSh
	case strings.Contains(url, "well-known"):
		source = skills.HubSourceWellKnown
	default:
		source = skills.HubSourceHub
	}

	err = mgr.InstallFromHub(source, url)
	if err != nil {
		fmt.Printf("Failed to install from URL: %v\n", err)
		os.Exit(1)
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

// =============================================================================
// Hub, Disable/Enable Commands
// =============================================================================

func runSkillsUninstall(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	skillName := args[0]
	err = mgr.UninstallHubSkill(skillName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Uninstalled hub skill '%s'\n", skillName)
}

func runSkillsDisable(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	skillName := args[0]
	err = mgr.DisableSkill(skillName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Disabled skill '%s'\n", skillName)
	fmt.Println("  Use 'magic skills enable " + skillName + "' to re-enable.")
}

func runSkillsEnable(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	skillName := args[0]
	err = mgr.EnableSkill(skillName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Enabled skill '%s'\n", skillName)
}

func runSkillsHubList(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	entries := mgr.GetHubLockEntries()
	if len(entries) == 0 {
		fmt.Println("No hub skills installed.")
		return
	}

	fmt.Printf("Hub-installed skills (%d):\n\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  📦 %s\n", e.SkillName)
		fmt.Printf("     Source: %s\n", e.Source)
		fmt.Printf("     Installed: %s\n", e.InstalledAt.Format("2006-01-02 15:04"))
		if e.SecurityAudit != "" {
			fmt.Printf("     Security: %s\n", e.SecurityAudit)
		}
		fmt.Println()
	}
}

func runSkillsHubAudit(cmd *cobra.Command, args []string) {
	// 读取审计日志
	home, _ := os.UserHomeDir()
	auditLog := filepath.Join(home, ".magic", "skills", ".hub", "audit.log")

	data, err := os.ReadFile(auditLog)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No audit log found.")
		} else {
			fmt.Printf("Error reading audit log: %v\n", err)
		}
		return
	}

	fmt.Print(string(data))
}

func runSkillsConfigList(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	disabled := mgr.GetDisabledSkills()

	fmt.Println("Skill Configuration:")
	fmt.Println()

	fmt.Println("Disabled Skills (Global):")
	if len(disabled.Global) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, name := range disabled.Global {
			fmt.Printf("  • %s\n", name)
		}
	}
	fmt.Println()

	fmt.Println("Disabled Skills (By Platform):")
	if len(disabled.Platform) == 0 {
		fmt.Println("  (none)")
	} else {
		for platform, names := range disabled.Platform {
			fmt.Printf("  %s:\n", platform)
			for _, name := range names {
				fmt.Printf("    • %s\n", name)
			}
		}
	}
}

func runSkillsConfigDisabled(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	disabled := mgr.GetDisabledSkills()

	fmt.Println("Disabled Skills:")
	fmt.Println()

	fmt.Println("Global:")
	if len(disabled.Global) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, name := range disabled.Global {
			fmt.Printf("  • %s\n", name)
		}
	}
	fmt.Println()

	fmt.Println("By Platform:")
	if len(disabled.Platform) == 0 {
		fmt.Println("  (none)")
	} else {
		for platform, names := range disabled.Platform {
			fmt.Printf("  %s:\n", platform)
			for _, name := range names {
				fmt.Printf("    • %s\n", name)
			}
		}
	}
}