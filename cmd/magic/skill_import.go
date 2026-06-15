package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/importer"
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
  magic skill import ./skills --list

  # Dry run to see what would be imported
  magic skill import ./skills --dry-run`,
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

	// Local path import only (GitHub import removed)
	runLocalImport(path)
}

func runLocalImport(path string) {
	// Resolve path
	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("Error: invalid path: %v\n", err)
		os.Exit(1)
	}

	// Check if path exists
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		fmt.Printf("Error: path not found: %s\n", absPath)
		os.Exit(1)
	}

	// Create manager for duplicate checking
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Warning: could not load skill manager: %v\n", err)
		mgr = nil
	}

	imp := importer.NewImporter(mgr)

	// Handle list-only mode
	if importListOnly {
		listSkills(imp, absPath, info.IsDir())
		return
	}

	// Handle recursive import
	if info.IsDir() && importRecursive {
		importRecursiveSkills(imp, absPath)
		return
	}

	// Handle single skill import
	if info.IsDir() {
		importSingleSkill(imp, absPath)
		return
	}

	// File path - need to determine if it's a skill directory or file
	fmt.Printf("Error: %s is a file, not a directory\n", absPath)
	fmt.Println("Provide a directory path containing SKILL.md")
	os.Exit(1)
}

func importSingleSkill(imp *importer.Importer, skillDir string) {
	result := imp.Import(skillDir, importForce)

	if result.Success {
		fmt.Printf("✓ Successfully imported: %s\n", result.Name)
		fmt.Printf("  Location: %s\n", result.Path)

		if len(result.Warnings) > 0 {
			fmt.Println("\nWarnings:")
			for _, w := range result.Warnings {
				fmt.Printf("  • %s\n", w)
			}
		}
	} else {
		fmt.Printf("✗ Failed to import: %v\n", result.Error)
		os.Exit(1)
	}
}

func importRecursiveSkills(imp *importer.Importer, skillsDir string) {
	results := imp.ImportRecursive(skillsDir, importForce)

	successCount := 0
	failCount := 0

	fmt.Printf("Importing skills from: %s\n\n", skillsDir)

	for _, result := range results {
		if result.Success {
			successCount++
			fmt.Printf("✓ %s\n", result.Name)
		} else {
			failCount++
			fmt.Printf("✗ %s: %v\n", filepath.Base(result.Path), result.Error)
		}
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Failed:  %d\n", failCount)

	if failCount > 0 {
		os.Exit(1)
	}
}

func listSkills(imp *importer.Importer, path string, isDir bool) {
	var skills []*importer.AvailableSkill
	var err error

	if isDir {
		skills, err = imp.ListAvailableSkills(path)
	} else {
		fmt.Printf("Error: --list requires a directory path\n")
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(skills) == 0 {
		fmt.Println("No skills found in directory")
		return
	}

	fmt.Printf("Found %d skills:\n\n", len(skills))

	for _, s := range skills {
		format := strings.ToUpper(s.Format)
		if s.Format == "openclaw" || s.Format == "hermes" {
			format = s.Format
		}
		fmt.Printf("  • %s [%s]\n", s.Name, format)
		if s.Description != "" {
			desc := s.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("    %s\n", desc)
		}
		fmt.Printf("    Path: %s\n\n", s.Path)
	}

	fmt.Println("\nUse 'magic skill import <path>' to import")
}

func previewSkill(imp *importer.Importer, skillDir string) {
	format, _ := importer.DetectFormat(skillDir)

	fmt.Printf("  Format: %s\n", format)

	// Try to read and display basic info
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillMdPath)
	if err == nil {
		frontmatter, content, _ := importer.ParseYAMLFrontmatter(string(data))
		if frontmatter != nil {
			if name, ok := frontmatter["name"].(string); ok {
				fmt.Printf("  Name: %s\n", name)
			}
			if desc, ok := frontmatter["description"].(string); ok {
				fmt.Printf("  Description: %s\n", desc)
			}
			if version, ok := frontmatter["version"].(string); ok {
				fmt.Printf("  Version: %s\n", version)
			}
		}
		_ = content // suppress unused warning
	}
}
