package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// Migrator handles migration from OpenClaw to Hermes Agent format
type Migrator struct {
	parser       *parser.Parser
	generator    *HermesGenerator
	report       *MigrationReport
	overwrite    bool
}

// MigrationReport contains the result of a migration operation
type MigrationReport struct {
	TotalCount     int
	SuccessCount   int
	FailedCount    int
	MigratedSkills []MigratedSkill
	Warnings       []string
	Errors         []MigrationError
}

// MigratedSkill represents a successfully migrated skill
type MigratedSkill struct {
	SourceName    string
	SourcePath    string
	TargetName    string
	TargetPath    string
	Warnings      []string
	Format        parser.SkillFormat
}

// MigrationError represents a migration failure
type MigrationError struct {
	SourcePath string
	Error      error
	Warning    string
}

// MigrateOptions contains options for migration
type MigrateOptions struct {
	InputPath   string  // Source path (file or directory)
	OutputPath  string  // Output directory
	Format      string  // "openclaw" or "hermes" (auto-detect if empty)
	Batch       bool    // Batch migrate all skills in directory
	Overwrite   bool    // Overwrite existing skills
	DryRun      bool    // Preview without creating files
	Recursive   bool    // Recursively process subdirectories
}

// NewMigrator creates a new migrator
func NewMigrator() *Migrator {
	return &Migrator{
		parser:    parser.NewParser(),
		generator: NewHermesGenerator(),
		report: &MigrationReport{
			MigratedSkills: []MigratedSkill{},
			Warnings:       []string{},
			Errors:         []MigrationError{},
		},
	}
}

// DetectFormat detects the skill format from a path
func DetectFormat(path string) (parser.SkillFormat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return parser.FormatUnknown, fmt.Errorf("failed to stat path: %w", err)
	}

	if info.IsDir() {
		return parser.DetectFormat(path)
	}

	// Check file extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return parser.DetectFormat(filepath.Dir(path))
	case ".json":
		// Check JSON structure
		data, err := os.ReadFile(path)
		if err != nil {
			return parser.FormatUnknown, err
		}
		return DetectJSONFormat(string(data))
	}

	return parser.FormatUnknown, fmt.Errorf("unknown format for file: %s", path)
}

// DetectJSONFormat detects format from JSON content
func DetectJSONFormat(content string) (parser.SkillFormat, error) {
	content = strings.TrimSpace(content)

	if strings.Contains(content, "trigger_conditions") {
		return parser.FormatOpenClaw, nil
	}

	if strings.Contains(content, "hermes") {
		return parser.FormatHermes, nil
	}

	return parser.FormatMagic, nil
}

// Migrate performs migration from source to target
func (m *Migrator) Migrate(opts *MigrateOptions) (*MigrationReport, error) {
	m.overwrite = opts.Overwrite
	m.report = &MigrationReport{
		MigratedSkills: []MigratedSkill{},
		Warnings:       []string{},
		Errors:         []MigrationError{},
	}

	info, err := os.Stat(opts.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access input path: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(opts.OutputPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	if info.IsDir() {
		if opts.Batch {
			return m.migrateBatch(opts)
		}
		return m.migrateDirectory(opts.InputPath, opts.OutputPath, opts.Format, opts.DryRun)
	}

	return m.migrateSingleFile(opts.InputPath, opts.OutputPath, opts.Format, opts.DryRun)
}

// migrateBatch migrates all skills in a directory
func (m *Migrator) migrateBatch(opts *MigrateOptions) (*MigrationReport, error) {
	entries, err := os.ReadDir(opts.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var skillDirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(opts.InputPath, entry.Name())
		skillMdPath := filepath.Join(skillPath, "SKILL.md")

		if _, err := os.Stat(skillMdPath); err == nil {
			skillDirs = append(skillDirs, skillPath)
		}
	}

	m.report.TotalCount = len(skillDirs)

	for _, skillDir := range skillDirs {
		skillName := filepath.Base(skillDir)
		outputDir := filepath.Join(opts.OutputPath, skillName)

		skillOpts := &MigrateOptions{
			InputPath:  skillDir,
			OutputPath: outputDir,
			Format:     opts.Format,
			Overwrite:  opts.Overwrite,
			DryRun:     opts.DryRun,
		}

		_, err := m.migrateDirectory(skillOpts.InputPath, skillOpts.OutputPath, skillOpts.Format, skillOpts.DryRun)
		if err != nil {
			m.report.FailedCount++
		}
	}

	return m.report, nil
}

// migrateDirectory migrates a single skill directory
func (m *Migrator) migrateDirectory(inputPath, outputPath, formatHint string, dryRun bool) (*MigrationReport, error) {
	// Detect format
	format := parser.SkillFormat(formatHint)
	if format == "" || format == parser.FormatUnknown {
		var err error
		format, err = DetectFormat(inputPath)
		if err != nil {
			return nil, err
		}
	}

	// Check if SKILL.md exists
	skillMdPath := filepath.Join(inputPath, "SKILL.md")
	if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
		// Try to find any markdown file
		entries, err := os.ReadDir(inputPath)
		if err != nil {
			return nil, fmt.Errorf("no skill file found in: %s", inputPath)
		}

		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".md") || strings.HasSuffix(entry.Name(), ".markdown")) {
				skillMdPath = filepath.Join(inputPath, entry.Name())
				break
			}
		}
	}

	// Read skill content
	skillContent, err := os.ReadFile(skillMdPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file: %w", err)
	}

	// Parse frontmatter
	frontmatter, content, err := parser.ParseYAMLFrontmatter(string(skillContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Get skill name
	skillName := filepath.Base(inputPath)
	if name, ok := frontmatter["name"].(string); ok && name != "" {
		skillName = name
	}

	m.report.TotalCount++

	// Check if output already exists
	if !dryRun && !m.overwrite {
		if _, err := os.Stat(outputPath); err == nil {
			m.report.Warnings = append(m.report.Warnings, fmt.Sprintf("Skipping %s: output directory already exists (use --overwrite to replace)", skillName))
			return m.report, nil
		}
	}

	// Generate Hermes format
	hermesContent, warnings, err := m.generator.Generate(format, frontmatter, content, skillName)
	if err != nil {
		m.report.FailedCount++
		m.report.Errors = append(m.report.Errors, MigrationError{
			SourcePath: inputPath,
			Error:      err,
		})
		return m.report, err
	}

	// Write output
	if !dryRun {
		if err := os.MkdirAll(outputPath, 0755); err != nil {
			m.report.FailedCount++
			m.report.Errors = append(m.report.Errors, MigrationError{
				SourcePath: inputPath,
				Error:      err,
			})
			return m.report, err
		}

		outputFile := filepath.Join(outputPath, "SKILL.md")
		if err := os.WriteFile(outputFile, []byte(hermesContent), 0644); err != nil {
			m.report.FailedCount++
			m.report.Errors = append(m.report.Errors, MigrationError{
				SourcePath: inputPath,
				Error:      err,
			})
			return m.report, err
		}

		// Copy additional files
		if err := m.copyAdditionalFiles(inputPath, outputPath); err != nil {
			m.report.Warnings = append(m.report.Warnings, fmt.Sprintf("Warning copying additional files for %s: %v", skillName, err))
		}
	}

	m.report.SuccessCount++
	m.report.MigratedSkills = append(m.report.MigratedSkills, MigratedSkill{
		SourceName: skillName,
		SourcePath: inputPath,
		TargetName: skillName,
		TargetPath: outputPath,
		Warnings:   warnings,
		Format:     format,
	})

	return m.report, nil
}

// migrateSingleFile migrates a single skill file
func (m *Migrator) migrateSingleFile(inputPath, outputPath, formatHint string, dryRun bool) (*MigrationReport, error) {
	format := parser.SkillFormat(formatHint)
	if format == "" || format == parser.FormatUnknown {
		var err error
		format, err = DetectFormat(inputPath)
		if err != nil {
			return nil, err
		}
	}

	content, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	frontmatter, body, err := parser.ParseYAMLFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skillName := filepath.Base(inputPath)
	if name, ok := frontmatter["name"].(string); ok && name != "" {
		skillName = name
	}

	m.report.TotalCount++

	if dryRun {
		fmt.Printf("[DRY RUN] Would migrate %s (%s) to %s\n", skillName, format, outputPath)
		m.report.SuccessCount++
		return m.report, nil
	}

	hermesContent, warnings, err := m.generator.Generate(format, frontmatter, body, skillName)
	if err != nil {
		m.report.FailedCount++
		m.report.Errors = append(m.report.Errors, MigrationError{
			SourcePath: inputPath,
			Error:      err,
		})
		return m.report, err
	}

	// Determine output file path
	if filepath.Ext(outputPath) == "" {
		// It's a directory, use the skill name
		outputPath = filepath.Join(outputPath, skillName, "SKILL.md")
	} else if filepath.Base(outputPath) == skillName || filepath.Ext(filepath.Base(outputPath)) == "" {
		// Use the path as-is
	} else {
		outputPath = filepath.Join(filepath.Dir(outputPath), skillName+".md")
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		m.report.FailedCount++
		return m.report, err
	}

	if err := os.WriteFile(outputPath, []byte(hermesContent), 0644); err != nil {
		m.report.FailedCount++
		return m.report, err
	}

	m.report.SuccessCount++
	m.report.MigratedSkills = append(m.report.MigratedSkills, MigratedSkill{
		SourceName: skillName,
		SourcePath: inputPath,
		TargetName: skillName,
		TargetPath: filepath.Dir(outputPath),
		Warnings:   warnings,
		Format:     format,
	})

	return m.report, nil
}

// copyAdditionalFiles copies non-markdown files from source to destination
func (m *Migrator) copyAdditionalFiles(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") {
			continue
		}

		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(dstDir, name)

		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}

		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return err
		}
	}

	return nil
}

// GenerateReport creates a human-readable migration report
func (m *MigrationReport) GenerateReport() string {
	var sb strings.Builder

	sb.WriteString("Migration Report\n")
	sb.WriteString("=================\n")
	sb.WriteString(fmt.Sprintf("Time: %s\n\n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Total: %d | Success: %d | Failed: %d\n\n", m.TotalCount, m.SuccessCount, m.FailedCount))

	if len(m.MigratedSkills) > 0 {
		sb.WriteString("Migrated Skills:\n")
		for _, s := range m.MigratedSkills {
			sb.WriteString(fmt.Sprintf("  ✓ %s (%s)\n", s.SourceName, s.Format))
			if len(s.Warnings) > 0 {
				for _, w := range s.Warnings {
					sb.WriteString(fmt.Sprintf("    ⚠ %s\n", w))
				}
			}
		}
		sb.WriteString("\n")
	}

	if len(m.Errors) > 0 {
		sb.WriteString("Errors:\n")
		for _, e := range m.Errors {
			sb.WriteString(fmt.Sprintf("  ✗ %s: %v\n", filepath.Base(e.SourcePath), e.Error))
			if e.Warning != "" {
				sb.WriteString(fmt.Sprintf("    %s\n", e.Warning))
			}
		}
		sb.WriteString("\n")
	}

	if len(m.Warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, w := range m.Warnings {
			sb.WriteString(fmt.Sprintf("  ⚠ %s\n", w))
		}
	}

	return sb.String()
}

// ConvertToSkill converts migration result to skills.Skill
func ConvertToSkill(format parser.SkillFormat, frontmatter map[string]interface{}, content string) *skills.Skill {
	skill := &skills.Skill{
		Metadata: make(map[string]interface{}),
	}

	// Extract common fields
	if name, ok := frontmatter["name"].(string); ok {
		skill.Name = name
	}
	if desc, ok := frontmatter["description"].(string); ok {
		skill.Description = desc
	}
	if version, ok := frontmatter["version"].(string); ok {
		skill.Version = version
	} else {
		skill.Version = "1.0.0"
	}
	if author, ok := frontmatter["author"].(string); ok {
		skill.Author = author
	}
	if license, ok := frontmatter["license"].(string); ok {
		skill.License = license
	}

	// Extract tags
	if tags, ok := frontmatter["tags"].([]string); ok {
		skill.Tags = tags
	}

	// Extract tools
	if tools, ok := frontmatter["tools"].([]string); ok {
		skill.Tools = tools
	}

	// For Hermes format, also check hermes block
	if hermes, ok := frontmatter["hermes"].(map[string]interface{}); ok {
		skill.Metadata["hermes"] = hermes

		// Override with hermes-specific values if not set
		if len(skill.Tags) == 0 {
			if tags, ok := hermes["tags"].([]string); ok {
				skill.Tags = tags
			}
		}
		if len(skill.Tools) == 0 {
			if tools, ok := hermes["tools"].([]string); ok {
				skill.Tools = tools
			}
		}
		if category, ok := hermes["category"].(string); ok {
			skill.Metadata["category"] = category
		}
	}

	skill.Content = content

	return skill
}
