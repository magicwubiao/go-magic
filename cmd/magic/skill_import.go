package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var (
	importForce     bool
	importRecursive bool
	importListOnly  bool
)

var skillImportCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Import skills from local path",
	Long: `Import skills from local path to go-magic.

Supported sources:
  - Local path: ./path/to/skill

Supported formats:
  - OpenClaw: Skills with trigger_conditions in SKILL.md
  - Hermes: Skills with hermes metadata in SKILL.md
  - Generic: Standard SKILL.md with YAML frontmatter

Examples:
  # Import from local path
  magic skill import ./path/to/skill

  # Import with overwrite
  magic skill import ./path/to/skill --force

  # List available skills without importing
  magic skill import ./skills --list`,
	Args: cobra.ExactArgs(1),
	Run:  runSkillImport,
}

func init() {
	skillImportCmd.Flags().BoolVarP(&importForce, "force", "f", false, "Overwrite existing skills")
	skillImportCmd.Flags().BoolVarP(&importRecursive, "recursive", "r", false, "Import all skills from directory")
	skillImportCmd.Flags().BoolVar(&importListOnly, "list", false, "List available skills without importing")

	skillsCmd.AddCommand(skillImportCmd)
}

func runSkillImport(cmd *cobra.Command, args []string) {
	path := args[0]
	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("Error: invalid path: %v\n", err)
		os.Exit(1)
	}

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		fmt.Printf("Error: path not found: %s\n", absPath)
		os.Exit(1)
	}

	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Warning: could not load skill manager: %v\n", err)
		mgr = nil
	}

	if importListOnly {
		listLocalSkills(absPath, info.IsDir())
		return
	}

	if info.IsDir() && importRecursive {
		importRecursiveSkills(mgr, absPath)
		return
	}

	if info.IsDir() {
		importSingleSkill(mgr, absPath)
		return
	}

	fmt.Printf("Error: %s is a file, not a directory\n", absPath)
	fmt.Println("Provide a directory path containing SKILL.md")
	os.Exit(1)
}

func importSingleSkill(mgr *skills.Manager, skillDir string) {
	skillName := filepath.Base(skillDir)
	skillMdPath := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
		fmt.Printf("Error: %s does not contain SKILL.md\n", skillDir)
		os.Exit(1)
	}

	destDir := filepath.Join(getGlobalSkillsDir(), skillName)
	if _, err := os.Stat(destDir); err == nil && !importForce {
		fmt.Printf("Error: skill '%s' already exists. Use --force to overwrite\n", skillName)
		os.Exit(1)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Printf("Error: failed to create skill directory: %v\n", err)
		os.Exit(1)
	}

	files, _ := os.ReadDir(skillDir)
	for _, f := range files {
		src := filepath.Join(skillDir, f.Name())
		dst := filepath.Join(destDir, f.Name())
		data, _ := os.ReadFile(src)
		os.WriteFile(dst, data, 0644)
	}

	fmt.Printf("Successfully imported: %s\n", skillName)
	fmt.Printf("  Location: %s\n", destDir)
}

func importRecursiveSkills(mgr *skills.Manager, skillsDir string) {
	files, err := os.ReadDir(skillsDir)
	if err != nil {
		fmt.Printf("Error: failed to read directory: %v\n", err)
		os.Exit(1)
	}

	successCount := 0
	failCount := 0

	fmt.Printf("Importing skills from: %s\n\n", skillsDir)

	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		skillDir := filepath.Join(skillsDir, f.Name())
		skillMdPath := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
			continue
		}

		destDir := filepath.Join(getGlobalSkillsDir(), f.Name())
		if _, err := os.Stat(destDir); err == nil && !importForce {
			fmt.Printf("Skipped %s: already exists (use --force to overwrite)\n", f.Name())
			failCount++
			continue
		}

		if err := os.MkdirAll(destDir, 0755); err != nil {
			fmt.Printf("Failed %s: %v\n", f.Name(), err)
			failCount++
			continue
		}

		subFiles, _ := os.ReadDir(skillDir)
		for _, sf := range subFiles {
			src := filepath.Join(skillDir, sf.Name())
			dst := filepath.Join(destDir, sf.Name())
			data, _ := os.ReadFile(src)
			os.WriteFile(dst, data, 0644)
		}

		fmt.Printf("Imported: %s\n", f.Name())
		successCount++
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Skipped: %d\n", failCount)
}

func listLocalSkills(path string, isDir bool) {
	if !isDir {
		fmt.Printf("Error: --list requires a directory path\n")
		os.Exit(1)
	}

	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var skillDirs []string
	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		skillMdPath := filepath.Join(path, f.Name(), "SKILL.md")
		if _, err := os.Stat(skillMdPath); err == nil {
			skillDirs = append(skillDirs, f.Name())
		}
	}

	if len(skillDirs) == 0 {
		fmt.Println("No skills found in directory")
		return
	}

	fmt.Printf("Found %d skills:\n\n", len(skillDirs))
	for _, name := range skillDirs {
		skillMdPath := filepath.Join(path, name, "SKILL.md")
		data, _ := os.ReadFile(skillMdPath)
		format := detectSkillFormat(string(data))

		fmt.Printf("  - %s [%s]\n", name, format)
		desc := extractDescription(string(data))
		if desc != "" {
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("    %s\n", desc)
		}
		fmt.Printf("    Path: %s\n\n", filepath.Join(path, name))
	}

	fmt.Println("\nUse 'magic skill import <path>' to import")
}

func detectSkillFormat(content string) string {
	if strings.Contains(content, "trigger_conditions:") || strings.Contains(content, "trigger_condition:") {
		return "openclaw"
	}
	if strings.Contains(content, "hermes_version:") || strings.Contains(content, "hermes:") {
		return "hermes"
	}
	return "generic"
}

func extractDescription(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimPrefix(line, "description:")
			desc = strings.TrimSpace(desc)
			desc = strings.Trim(desc, "\"")
			return desc
		}
	}
	return ""
}

func getGlobalSkillsDir() string {
	return filepath.Join(config.GetMagicHome(), "skills")
}
