package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

func TestHermesGenerator_Generate(t *testing.T) {
	gen := NewHermesGenerator()

	tests := []struct {
		name        string
		format      parser.SkillFormat
		frontmatter map[string]interface{}
		content     string
		wantErr     bool
	}{
		{
			name:   "OpenClaw format",
			format: parser.FormatOpenClaw,
			frontmatter: map[string]interface{}{
				"name":               "test-skill",
				"description":        "A test skill",
				"version":            "1.0.0",
				"author":             "Test Author",
				"tags":               []string{"test", "example"},
				"tools":              []string{"bash", "read_file"},
				"trigger_conditions": []string{"when user says hello"},
				"steps":              []string{"step1", "step2"},
			},
			content: "# Test Skill\n\nThis is a test skill.",
			wantErr: false,
		},
		{
			name:   "Hermes format",
			format: parser.FormatHermes,
			frontmatter: map[string]interface{}{
				"name":        "hermes-skill",
				"description": "A Hermes skill",
				"version":     "2.0.0",
				"tags":        []string{"hermes"},
				"hermes": map[string]interface{}{
					"category": "productivity",
					"tags":     []string{"advanced"},
				},
			},
			content: "# Hermes Skill\n\nHermes formatted skill.",
			wantErr: false,
		},
		{
			name:   "Minimal format",
			format: parser.FormatMagic,
			frontmatter: map[string]interface{}{
				"name":        "minimal",
				"description": "Minimal skill",
			},
			content: "# Minimal\n\nMinimal content.",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, warnings, err := gen.Generate(tt.format, tt.frontmatter, tt.content, tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if result == "" {
				t.Error("Generate() returned empty result")
			}

			// Check frontmatter was generated
			if !contains(result, "---") {
				t.Error("Generate() missing frontmatter separator")
			}

			// Check name is present
			if !contains(result, "name:") {
				t.Error("Generate() missing name field")
			}

			// For OpenClaw, check migration metadata
			if tt.format == parser.FormatOpenClaw {
				if !contains(result, "migrated_from:") {
					t.Error("OpenClaw migration missing migrated_from field")
				}
				if len(warnings) == 0 {
					t.Log("OpenClaw conversion warnings:", warnings)
				}
			}
		})
	}
}

func TestToolConverter_ConvertTools(t *testing.T) {
	conv := NewToolConverter()

	tests := []struct {
		name         string
		openclawTools []string
		wantTools    []string
		wantWarnLen  int
	}{
		{
			name:          "Known tools",
			openclawTools: []string{"bash", "read_file", "write_file"},
			wantTools:     []string{"bash", "read_file", "write_file"},
			wantWarnLen:   0,
		},
		{
			name:          "Unknown tools preserved",
			openclawTools: []string{"custom_tool"},
			wantTools:     []string{"custom_tool"},
			wantWarnLen:   1,
		},
		{
			name:          "Mixed tools",
			openclawTools: []string{"bash", "unknown_tool"},
			wantTools:     []string{"bash", "unknown_tool"},
			wantWarnLen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, warnings := conv.ConvertTools(tt.openclawTools)

			if len(tools) != len(tt.wantTools) {
				t.Errorf("ConvertTools() got %d tools, want %d", len(tools), len(tt.wantTools))
			}

			for i, tool := range tools {
				if i < len(tt.wantTools) && tool != tt.wantTools[i] {
					t.Errorf("ConvertTools()[%d] = %v, want %v", i, tool, tt.wantTools[i])
				}
			}

			if len(warnings) != tt.wantWarnLen {
				t.Errorf("ConvertTools() got %d warnings, want %d", len(warnings), tt.wantWarnLen)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	gen := NewHermesGenerator()

	tests := []struct {
		input string
		want  string
	}{
		{"My Skill", "my-skill"},
		{"Test_Skill-123", "test_skill-123"},
		{"UPPERCASE", "uppercase"},
		{"multiple   spaces", "multiple---spaces"},
		{"special!@#chars", "specialchars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectFormat(t *testing.T) {
	// Create temp directory with test files
	tmpDir, err := os.MkdirTemp("", "migrate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		content  string
		wantFmt  parser.SkillFormat
	}{
		{
			name:    "OpenClaw with trigger_conditions",
			content: "---\nname: test\ntrigger_conditions:\n  - hello\n---\n# Test",
			wantFmt: parser.FormatOpenClaw,
		},
		{
			name:    "Hermes with hermes block",
			content: "---\nname: test\nmetadata:\n  hermes:\n    tags: []\n---\n# Test",
			wantFmt: parser.FormatHermes,
		},
		{
			name:    "Magic simple",
			content: "---\nname: test\n---\n# Test",
			wantFmt: parser.FormatMagic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skillFile := filepath.Join(tmpDir, "test-skill", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(skillFile), 0755); err != nil {
				t.Fatalf("Failed to create skill dir: %v", err)
			}
			if err := os.WriteFile(skillFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			format, err := DetectFormat(filepath.Join(tmpDir, "test-skill"))
			if err != nil {
				t.Fatalf("DetectFormat() error = %v", err)
			}

			if format != tt.wantFmt {
				t.Errorf("DetectFormat() = %v, want %v", format, tt.wantFmt)
			}
		})
	}
}

func TestMigrationReport_GenerateReport(t *testing.T) {
	report := &MigrationReport{
		TotalCount:   3,
		SuccessCount: 2,
		FailedCount:  1,
		MigratedSkills: []MigratedSkill{
			{
				SourceName: "skill1",
				SourcePath: "/path/to/skill1",
				TargetName: "skill1",
				TargetPath: "/output/skill1",
				Format:     parser.FormatOpenClaw,
			},
			{
				SourceName: "skill2",
				SourcePath: "/path/to/skill2",
				TargetName: "skill2",
				TargetPath: "/output/skill2",
				Format:     parser.FormatHermes,
				Warnings:   []string{"Warning 1"},
			},
		},
		Warnings: []string{"Global warning"},
		Errors: []MigrationError{
			{
				SourcePath: "/path/to/skill3",
				Error:     nil,
				Warning:   "Failed to migrate",
			},
		},
	}

	output := report.GenerateReport()

	// Check key sections
	if !contains(output, "Total: 3") {
		t.Error("Report missing total count")
	}
	if !contains(output, "Success: 2") {
		t.Error("Report missing success count")
	}
	if !contains(output, "Failed: 1") {
		t.Error("Report missing failed count")
	}
	if !contains(output, "skill1") {
		t.Error("Report missing skill1")
	}
	if !contains(output, "skill2") {
		t.Error("Report missing skill2")
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{}, []string{}},
		{[]string{"only"}, []string{"only"}},
	}

	for _, tt := range tests {
		got := uniqueStrings(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("uniqueStrings(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestConvertToSkill(t *testing.T) {
	frontmatter := map[string]interface{}{
		"name":        "test-skill",
		"description": "A test skill",
		"version":     "1.0.0",
		"author":      "Test Author",
		"tags":        []string{"test", "example"},
		"tools":       []string{"bash"},
		"hermes": map[string]interface{}{
			"category": "test",
			"tags":     []string{"advanced"},
		},
	}

	skill := ConvertToSkill(parser.FormatOpenClaw, frontmatter, "# Test")

	if skill.Name != "test-skill" {
		t.Errorf("ConvertToSkill() Name = %v, want test-skill", skill.Name)
	}
	if skill.Description != "A test skill" {
		t.Errorf("ConvertToSkill() Description = %v, want A test skill", skill.Description)
	}
	if skill.Version != "1.0.0" {
		t.Errorf("ConvertToSkill() Version = %v, want 1.0.0", skill.Version)
	}
	if skill.Author != "Test Author" {
		t.Errorf("ConvertToSkill() Author = %v, want Test Author", skill.Author)
	}
	if len(skill.Tags) != 2 {
		t.Errorf("ConvertToSkill() Tags len = %v, want 2", len(skill.Tags))
	}
	if len(skill.Tools) != 1 {
		t.Errorf("ConvertToSkill() Tools len = %v, want 1", len(skill.Tools))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
