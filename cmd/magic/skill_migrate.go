package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills/migrate"
)

var (
	migrateInput   string
	migrateOutput  string
	migrateFormat  string
	migrateBatch   bool
	migrateOverwrite bool
	migrateDryRun  bool
	migrateRecursive bool
)

var skillMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate skills from OpenClaw to Hermes Agent format",
	Long: `Migrate skills from OpenClaw format to Hermes Agent SKILL.md format.

This command converts skills from various formats to the Hermes Agent standard:
  - OpenClaw: Skills with trigger_conditions in SKILL.md
  - Hermes: Skills with hermes metadata
  - Magic: Standard skills with YAML frontmatter

The migration includes:
  - Converting frontmatter metadata
  - Transforming tool definitions
  - Adapting trigger conditions
  - Preserving skill content with improvements

Examples:
  # Migrate a single skill directory
  magic skill migrate ./my-skill -o ./output

  # Batch migrate all skills in a directory
  magic skill migrate ./skills -o ./output --batch

  # Migrate with format auto-detection
  magic skill migrate ./openclaw-skill

  # Preview migration without creating files
  magic skill migrate ./my-skill -o ./output --dry-run

  # Force overwrite existing skills
  magic skill migrate ./my-skill -o ./output --overwrite`,
	Args: cobra.RangeArgs(0, 1),
	Run:  runSkillMigrate,
}

func init() {
	skillMigrateCmd.Flags().StringVarP(&migrateInput, "input", "i", "", "Input path (skill directory or file)")
	skillMigrateCmd.Flags().StringVarP(&migrateOutput, "output", "o", "", "Output directory for migrated skills")
	skillMigrateCmd.Flags().StringVar(&migrateFormat, "format", "", "Input format (openclaw, hermes, auto-detect)")
	skillMigrateCmd.Flags().BoolVar(&migrateBatch, "batch", false, "Batch migrate all skills in directory")
	skillMigrateCmd.Flags().BoolVarP(&migrateOverwrite, "overwrite", "w", false, "Overwrite existing skills")
	skillMigrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Preview migration without creating files")
	skillMigrateCmd.Flags().BoolVarP(&migrateRecursive, "recursive", "r", false, "Recursively process subdirectories")

	// Set up input path from argument if provided
	skillMigrateCmd.Args = func(cmd *cobra.Command, args []string) error {
		return nil
	}

	skillCmd.AddCommand(skillMigrateCmd)
}

func runSkillMigrate(cmd *cobra.Command, args []string) {
	// Determine input path
	inputPath := migrateInput
	if len(args) > 0 && inputPath == "" {
		inputPath = args[0]
	}

	if inputPath == "" {
		// Use current directory as default
		wd, err := os.Getwd()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		inputPath = wd
	}

	// Resolve to absolute path
	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		fmt.Printf("Error: invalid input path: %v\n", err)
		os.Exit(1)
	}

	// Check if input exists
	if _, err := os.Stat(absInput); os.IsNotExist(err) {
		fmt.Printf("Error: input path does not exist: %s\n", absInput)
		os.Exit(1)
	}

	// Determine output path
	outputPath := migrateOutput
	if outputPath == "" {
		if !migrateBatch {
			// For single migration, use sibling directory
			outputPath = absInput + "-hermes"
		} else {
			// For batch, use ./migrated in current directory
			wd, _ := os.Getwd()
			outputPath = filepath.Join(wd, "migrated")
		}
	}

	// Resolve output to absolute path
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		fmt.Printf("Error: invalid output path: %v\n", err)
		os.Exit(1)
	}

	// Display configuration
	fmt.Println("OpenClaw to Hermes Agent Migration")
	fmt.Println("==================================")
	fmt.Println()
	fmt.Printf("Input:   %s\n", absInput)
	fmt.Printf("Output:  %s\n", absOutput)

	if migrateFormat != "" {
		fmt.Printf("Format:  %s\n", migrateFormat)
	} else {
		fmt.Println("Format:  auto-detect")
	}

	fmt.Printf("Mode:    ")
	if migrateBatch {
		fmt.Println("batch")
	} else {
		fmt.Println("single")
	}

	if migrateDryRun {
		fmt.Println("Dry run: yes (no files will be created)")
	}

	if migrateOverwrite {
		fmt.Println("Overwrite: yes")
	}

	fmt.Println()

	// Validate format
	if migrateFormat != "" && migrateFormat != "openclaw" && migrateFormat != "hermes" && migrateFormat != "magic" {
		fmt.Printf("Error: invalid format '%s'. Use: openclaw, hermes, magic\n", migrateFormat)
		os.Exit(1)
	}

	// Create migrator
	migrator := migrate.NewMigrator()

	// Prepare options
	opts := &migrate.MigrateOptions{
		InputPath:  absInput,
		OutputPath: absOutput,
		Format:     migrateFormat,
		Batch:      migrateBatch,
		Overwrite:  migrateOverwrite,
		DryRun:     migrateDryRun,
		Recursive: migrateRecursive,
	}

	// Run migration
	report, err := migrator.Migrate(opts)
	if err != nil {
		fmt.Printf("Migration failed: %v\n", err)
		os.Exit(1)
	}

	// Print report
	fmt.Println(report.GenerateReport())

	// Exit with error code if any failures
	if report.FailedCount > 0 {
		os.Exit(1)
	}
}

// detectInputFormat detects the format of input for display purposes
func detectInputFormat(path string) string {
	format, err := migrate.DetectFormat(path)
	if err != nil {
		return "unknown"
	}
	return string(format)
}
