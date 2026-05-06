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
	importForce       bool
	importRecursive   bool
	importDryRun      bool
	importListOnly    bool
)

var skillImportCmd = &cobra.Command{
	Use:   "import <path|url>",
	Short: "Import skills from local path or remote URL",
	Long: `Import skills from OpenClaw or Hermes format to go-magic.

Supported sources:
  - Local path: ./path/to/skill
  - GitHub repo: https://github.com/user/repo/tree/main/skills/my-skill
  - GitHub raw: https://raw.githubusercontent.com/user/repo/main/skills/my-skill/SKILL.md
  - GitHub Gist: https://gist.github.com/user/hash
  - HTTP file: https://example.com/skills/my-skill.zip
  - GitHub archive: https://github.com/user/repo/archive/main.zip

Supported formats:
  - OpenClaw: Skills with trigger_conditions in SKILL.md
  - Hermes: Skills with hermes metadata in SKILL.md
  - Generic: Standard SKILL.md with YAML frontmatter

Examples:
  # Import from local path
  magic skill import ./path/to/skill

  # Import from GitHub repository (single skill)
  magic skill import https://github.com/user/repo/tree/main/skills/excel-processor

  # Import from GitHub repository (all skills in directory)
  magic skill import https://github.com/user/repo/tree/main/skills --recursive

  # Import from GitHub Gist
  magic skill import https://gist.github.com/user/abc123

  # Import from HTTP URL (ZIP archive)
  magic skill import https://example.com/skills/my-skill.zip

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
	skillImportCmd.Flags().BoolVarP(&importRecursive, "recursive", "r", false, "Import all skills from directory (local or URL)")
	skillImportCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Show what would be imported without importing")
	skillImportCmd.Flags().BoolVar(&importListOnly, "list", false, "List available skills without importing")

	skillCmd.AddCommand(skillImportCmd)
}

func runSkillImport(cmd *cobra.Command, args []string) {
	path := args[0]

	// Check if it's a URL
	if importer.IsURL(path) {
		runURLImport(path)
		return
	}

	// Local path import
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

func runURLImport(url string) {
	// Detect URL type for display
	downloader, err := importer.NewDownloader()
	if err != nil {
		fmt.Printf("Error: failed to create downloader: %v\n", err)
		os.Exit(1)
	}
	urlType := downloader.DetectURLType(url)
	fmt.Printf("Detected URL type: %s\n\n", urlType)

	// Create manager for duplicate checking
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Warning: could not load skill manager: %v\n", err)
		mgr = nil
	}

	fmt.Println("=== URL Import Mode ===")
	fmt.Printf("Source: %s\n\n", url)

	// Dry run mode
	if importDryRun {
		fmt.Println("[DRY RUN] Would import skill from URL")
		previewURLSkill(url)
		return
	}

	// Handle recursive URL import
	if importRecursive {
		importURLRecursive(mgr, url)
		return
	}

	// Single URL import
	importURL(mgr, url)
}

func importURL(mgr *skills.Manager, url string) {
	result, err := importer.ImportFromURLWithManager(mgr, url, importForce)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if result.Success {
		fmt.Printf("\n✓ Successfully imported: %s\n", result.Name)
		fmt.Printf("  Location: %s\n", result.Path)

		if len(result.Warnings) > 0 {
			fmt.Println("\nWarnings:")
			for _, w := range result.Warnings {
				fmt.Printf("  • %s\n", w)
			}
		}
	} else {
		fmt.Printf("\n✗ Failed to import: %v\n", result.Error)
		os.Exit(1)
	}
}

func importURLRecursive(mgr *skills.Manager, url string) {
	results, err := importer.ImportFromURLRecursiveWithManager(mgr, url, importForce)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	successCount := 0
	failCount := 0

	fmt.Printf("Importing skills from URL...\n\n")

	for _, result := range results {
		if result.Success {
			successCount++
			fmt.Printf("✓ %s\n", result.Name)
		} else {
			failCount++
			name := "unknown"
			if result.Name != "" {
				name = result.Name
			} else {
				name = filepath.Base(result.Path)
			}
			fmt.Printf("✗ %s: %v\n", name, result.Error)
		}
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Failed:  %d\n", failCount)

	if failCount > 0 {
		os.Exit(1)
	}
}

func previewURLSkill(url string) {
	fmt.Println("Source URL:", url)

	// Try to show basic info about the URL
	downloader, err := importer.NewDownloader()
	if err != nil {
		fmt.Printf("  (Could not analyze URL: %v)\n", err)
		return
	}

	urlType := downloader.DetectURLType(url)
	fmt.Printf("  Type: %s\n", urlType)

	// For GitHub URLs, try to show branch/path info
	if strings.Contains(url, "github.com") {
		fmt.Println("  Format: GitHub repository")
	}
}

func importSingleSkill(imp *importer.Importer, skillDir string) {
	// Dry run mode
	if importDryRun {
		fmt.Printf("[DRY RUN] Would import skill from: %s\n", skillDir)
		previewSkill(imp, skillDir)
		return
	}

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
