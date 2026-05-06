package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSkillGetTools tests the GetTools method
func TestSkillGetTools(t *testing.T) {
	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Tools:       []string{"tool1", "tool2"},
	}

	tools := skill.GetTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	if tools[0] != "tool1" || tools[1] != "tool2" {
		t.Error("tools mismatch")
	}
}

// TestSkillGetToolsFromMetadata tests getting tools from metadata
func TestSkillGetToolsFromMetadata(t *testing.T) {
	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Metadata: map[string]interface{}{
			"tools": []string{"meta_tool1", "meta_tool2"},
		},
	}

	tools := skill.GetTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools from metadata, got %d", len(tools))
	}
}

// TestSkillGetToolsFromCortexFormat tests getting tools from Cortex format metadata
func TestSkillGetToolsFromCortexFormat(t *testing.T) {
	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Metadata: map[string]interface{}{
			"cortex": map[string]interface{}{
				"tools": []string{"Cortex_tool1", "Cortex_tool2"},
			},
		},
	}

	tools := skill.GetTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools from Cortex format, got %d", len(tools))
	}
	if tools[0] != "Cortex_tool1" {
		t.Error("Cortex tools mismatch")
	}
}

// TestSkillGetTags tests the GetTags method
func TestSkillGetTags(t *testing.T) {
	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Tags:        []string{"tag1", "tag2"},
	}

	tags := skill.GetTags()
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}
}

// TestSkillGetTagsFromMetadata tests getting tags from metadata
func TestSkillGetTagsFromMetadata(t *testing.T) {
	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Metadata: map[string]interface{}{
			"tags": []string{"meta_tag1", "meta_tag2"},
		},
	}

	tags := skill.GetTags()
	if len(tags) != 2 {
		t.Errorf("expected 2 tags from metadata, got %d", len(tags))
	}
}

// TestNewManager tests creating a new manager
func TestNewManager(t *testing.T) {
	// Create a temporary skills directory
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
}

// TestManagerAddSkill tests adding a skill
func TestManagerAddSkill(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Content:     "# Test Skill\n\nThis is a test skill.",
	}

	err = manager.Add(skill)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	skills := manager.List()
	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}

	retrieved, err := manager.Get("test_skill")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if retrieved.Name != "test_skill" {
		t.Errorf("expected skill name 'test_skill', got '%s'", retrieved.Name)
	}
}

// TestManagerRemoveSkill tests removing a skill
func TestManagerRemoveSkill(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
	}

	manager.Add(skill)
	err = manager.Remove("test_skill")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	skills := manager.List()
	if len(skills) != 0 {
		t.Errorf("expected 0 skills after remove, got %d", len(skills))
	}
}

// TestManagerLoadSkillFromFile tests loading a skill from a markdown file
func TestManagerLoadSkillFromFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a skill file
	skillDir := filepath.Join(tmpDir, "test_skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	skillContent := `---
name: test_skill
description: A test skill loaded from file
version: 1.0.0
author: Test Author
tags: [test, unit]
---

# Test Skill

This skill was loaded from a file.
`

	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	skills := manager.List()
	if len(skills) == 0 {
		t.Error("expected at least one skill to be loaded")
	}
}

// TestManagerListByTags tests listing skills by tags
func TestManagerListByTags(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Add test skills
	manager.Add(&Skill{
		Name:        "python_coder",
		Description: "Python coding assistant",
		Tags:        []string{"coding", "python"},
	})
	manager.Add(&Skill{
		Name:        "go_coder",
		Description: "Go programming expert",
		Tags:        []string{"coding", "go"},
	})
	manager.Add(&Skill{
		Name:        "writer",
		Description: "Content writing assistant",
		Tags:        []string{"writing"},
	})

	// List by tags
	codingSkills := manager.ListByTags([]string{"coding"})
	if len(codingSkills) != 2 {
		t.Errorf("expected 2 coding skills, got %d", len(codingSkills))
	}

	writeSkills := manager.ListByTags([]string{"writing"})
	if len(writeSkills) != 1 {
		t.Errorf("expected 1 writing skill, got %d", len(writeSkills))
	}
}

// TestManagerSearch tests searching skills
func TestManagerSearch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Add test skills
	manager.Add(&Skill{
		Name:        "python_coder",
		Description: "Python coding assistant",
		Tags:        []string{"coding", "python"},
	})
	manager.Add(&Skill{
		Name:        "data_analyst",
		Description: "Data analysis expert",
		Tags:        []string{"data", "analysis"},
	})

	// Search by keyword
	results := manager.Search("python")
	if len(results) != 1 {
		t.Errorf("expected 1 python result, got %d", len(results))
	}

	results = manager.Search("analysis")
	if len(results) != 1 {
		t.Errorf("expected 1 analysis result, got %d", len(results))
	}
}

// TestManagerMatchSkillsByInput tests skill matching
func TestManagerMatchSkillsByInput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	manager.Add(&Skill{
		Name:        "data_analyst",
		Description: "Data analysis and visualization expert",
		Tags:        []string{"data", "analysis", "visualization"},
		Tools:       []string{"python_execute", "web_search"},
	})

	// Test matching
	skills := manager.MatchSkillsByInput("analyze sales data and create charts")
	if len(skills) == 0 {
		t.Error("expected at least one matched skill")
	}
}

// TestManagerGetSkillsContext tests getting skills context string
func TestManagerGetSkillsContext(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	manager.Add(&Skill{
		Name:        "python_expert",
		Description: "Python programming expert",
		Content:     "You are an expert in Python programming.",
	})

	ctx := manager.GetSkillsContext()
	if ctx == "" {
		t.Error("expected non-empty skills context")
	}
}

// TestManagerCount tests counting skills
func TestManagerCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	initialCount := manager.Count()

	manager.Add(&Skill{Name: "skill1", Description: "desc1"})
	manager.Add(&Skill{Name: "skill2", Description: "desc2"})

	if manager.Count() != initialCount+2 {
		t.Errorf("expected count %d, got %d", initialCount+2, manager.Count())
	}
}

// TestManagerReload tests reloading skills
func TestManagerReload(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Reload should not error even with empty directory
	err = manager.Reload()
	if err != nil {
		t.Errorf("unexpected error on reload: %v", err)
	}
}

// TestSkillSource tests skill source tracking
func TestSkillSource(t *testing.T) {
	skill := &Skill{
		Name:   "test_skill",
		Source: "local",
	}

	if skill.Source != "local" {
		t.Errorf("expected source 'local', got '%s'", skill.Source)
	}
}

// TestManagerGetSkillInfo tests getting skill information
func TestManagerGetSkillInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	skill := &Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Content:     "Skill content here",
		Tools:       []string{"tool1", "tool2"},
	}
	manager.Add(skill)

	desc, tools, content, err := manager.GetSkillInfo("test_skill")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if desc != "A test skill" {
		t.Errorf("unexpected description: %s", desc)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	if content != "Skill content here" {
		t.Errorf("unexpected content: %s", content)
	}
}

// TestManagerListSkills tests listing skill names
func TestManagerListSkills(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ManagerConfig{
		SearchDirs: []string{tmpDir},
	}

	manager, err := NewManagerWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	manager.Add(&Skill{Name: "skill1", Description: "desc1"})
	manager.Add(&Skill{Name: "skill2", Description: "desc2"})

	names := manager.ListSkills()
	if len(names) != 2 {
		t.Errorf("expected 2 skills, got %d", len(names))
	}
}
