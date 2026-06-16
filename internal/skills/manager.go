package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// Skill is now defined in types.go - this file re-exports it for backwards compatibility
// All new code should use types.Skill

// Manager manages skill loading and registration
type Manager struct {
	mu              sync.RWMutex
	searchDirs      []string
	builtinDir      string
	skills          map[string]*Skill
	toolNames       []string              // Cached tool names from registry
	registryURL     string                // ClawHub or GitHub registry URL
	hubLock         *HubLock              // Hub 安装跟踪 (.hub/lock.json)
	bundledManifest *BundledManifest      // 内置技能跟踪 (.bundled_manifest)
	disabledSkills  *DisabledSkillsConfig // 禁用技能配置
	skillsDir       string                // 技能目录路径 (~/.magic/skills)
	hubDir          string                // Hub 目录路径 (~/.magic/skills/.hub)
	autoSkillsDir   string                // 自动技能根目录 (.../auto_skills)
	// Registry manager for hub search/install
	registryMgr *RegistryManager
}

// ManagerConfig 配置管理器
type ManagerConfig struct {
	SearchDirs    []string // 搜索目录列表
	BuiltinDir    string   // 内置技能目录
	RegistryURL   string   // 技能注册表 URL
	ToolNames     []string // 可用工具名称列表（用于技能验证）
	AutoSkillsDir string   // 自动技能目录路径（三态管理根目录）
}

// NewManager creates a new skill manager with default configuration
func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Get the path to built-in skills directory
	builtinDir := filepath.Join("internal", "skills", "builtin")

	config := &ManagerConfig{
		SearchDirs: []string{
			filepath.Join(home, ".magic", "skills"),
			"skills",
			filepath.Join(".magic", "skills"),
		},
		BuiltinDir: builtinDir,
	}

	return NewManagerWithConfig(config)
}

// NewManagerWithConfig creates a manager with custom configuration
func NewManagerWithConfig(config *ManagerConfig) (*Manager, error) {
	if config == nil {
		config = &ManagerConfig{}
	}

	// Set defaults
	if len(config.SearchDirs) == 0 {
		home, _ := os.UserHomeDir()
		config.SearchDirs = []string{
			filepath.Join(home, ".magic", "skills"),
			"skills",
			filepath.Join(".magic", "skills"),
		}
	}

	skillsDir := config.SearchDirs[0]

	// 自动技能目录：显式指定时用指定路径，否则在第一个 searchDir 下建 auto_skills
	autoDir := config.AutoSkillsDir
	if autoDir == "" {
		autoDir = filepath.Join(skillsDir, "auto_skills")
	}

	m := &Manager{
		searchDirs:     config.SearchDirs,
		builtinDir:     config.BuiltinDir,
		registryURL:    config.RegistryURL,
		toolNames:      config.ToolNames,
		skills:         make(map[string]*Skill),
		skillsDir:      skillsDir,
		hubDir:         filepath.Join(skillsDir, ".hub"),
		autoSkillsDir:  autoDir,
		disabledSkills: &DisabledSkillsConfig{Platform: make(map[string][]string)},
		registryMgr:    NewRegistryManager(),
	}

	// 创建 Hub 目录
	os.MkdirAll(m.hubDir, 0755)
	// 创建三态目录
	os.MkdirAll(filepath.Join(m.autoSkillsDir, "pending"), 0755)
	os.MkdirAll(filepath.Join(m.autoSkillsDir, "approved"), 0755)
	os.MkdirAll(filepath.Join(m.autoSkillsDir, "archived"), 0755)

	// 加载 Hub lock.json
	m.loadHubLock()

	// 加载 Bundled manifest
	m.loadBundledManifest()

	// 加载禁用技能配置
	m.loadDisabledSkills()

	// Load built-in skills (always try embedded FS, fallback to filesystem)
	if err := m.loadBuiltinSkills(); err != nil {
		// Don't fail on error, just log
		fmt.Printf("Warning: failed to load built-in skills: %v\n", err)
	}

	if err := m.loadSkills(); err != nil {
		return nil, err
	}

	// Initialize effectiveness manager with auto-save and cleanup
	if len(m.searchDirs) > 0 {
		if effMgr, err := NewEffectivenessManager(m.searchDirs[0]); err == nil {
			effMgr.StartAutoSave()
			// Start periodic cleanup (every 24 hours, remove records older than 30 days)
			go m.startEffectivenessCleanup(effMgr)
		}
	}

	return m, nil
}

// startEffectivenessCleanup starts a background goroutine to periodically clean old effectiveness records
func (m *Manager) startEffectivenessCleanup(effMgr *EffectivenessManager) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if err := effMgr.ClearOldRecords(30 * 24 * time.Hour); err != nil {
			fmt.Printf("[skills] Failed to clean old effectiveness records: %v\n", err)
		} else {
			fmt.Printf("[skills] Cleaned old effectiveness records (older than 30 days)\n")
		}
	}
}

// loadBuiltinSkills 加载内置技能
func (m *Manager) loadBuiltinSkills() error {
	// First try embedded builtin skills (always available in compiled binary)
	entries, err := BuiltinSkillsFS.ReadDir(BuiltinDirName)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillMdPath := BuiltinDirName + "/" + entry.Name() + "/SKILL.md"
			data, err := BuiltinSkillsFS.ReadFile(skillMdPath)
			if err == nil {
				skill := m.loadSkillFromContent(string(data), entry.Name())
				if skill != nil {
					skill.Source = "builtin"
					m.skills[skill.Name] = skill
				}
			}
		}
		return nil
	}

	// Fallback to filesystem (for development mode)
	if m.builtinDir == "" {
		return nil
	}

	osEntries, err := os.ReadDir(m.builtinDir)
	if err != nil {
		return err
	}

	for _, entry := range osEntries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(m.builtinDir, entry.Name())
		skillMdPath := filepath.Join(path, "SKILL.md")

		if _, err := os.Stat(skillMdPath); err == nil {
			skill := m.loadSkillFromFile(skillMdPath)
			if skill != nil {
				skill.Source = "builtin"
				m.skills[skill.Name] = skill
			}
		}
	}

	return nil
}

func (m *Manager) loadSkills() error {
	for _, dir := range m.searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // Skip directories that don't exist
		}

		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())

			if entry.IsDir() {
				// Check for SKILL.md (Cortex format)
				skillMdPath := filepath.Join(path, "SKILL.md")
				if _, err := os.Stat(skillMdPath); err == nil {
					skill := m.loadSkillFromFile(skillMdPath)
					if skill != nil {
						// Preserve existing source if it's builtin or registry (from hub)
						if existingSkill, exists := m.skills[skill.Name]; exists {
							if existingSkill.Source == "builtin" {
								skill.Source = existingSkill.Source
							} else if skill.Source != SkillSourceRegistry {
								skill.Source = "local"
								if dir == m.searchDirs[0] || strings.Contains(dir, ".magic") {
									skill.Source = "global"
								}
							}
						} else if skill.Source != SkillSourceRegistry {
							skill.Source = "local"
							if dir == m.searchDirs[0] || strings.Contains(dir, ".magic") {
								skill.Source = "global"
							}
						}
						m.skills[skill.Name] = skill
					}
					continue
				}
				// Check for manifest.json (legacy format)
				manifestPath := filepath.Join(path, "manifest.json")
				if _, err := os.Stat(manifestPath); err == nil {
					skill := m.loadSkillFromManifest(manifestPath)
					if skill != nil {
						skill.Source = "local"
						m.skills[skill.Name] = skill
					}
					continue
				}
				continue
			}

			skill := m.loadSkillFromFile(path)
			if skill != nil {
				// Preserve existing source if it's builtin or registry (from hub)
				if existingSkill, exists := m.skills[skill.Name]; exists {
					if existingSkill.Source == "builtin" {
						skill.Source = existingSkill.Source
					} else if skill.Source != SkillSourceRegistry {
						skill.Source = "local"
					}
				} else if skill.Source != SkillSourceRegistry {
					skill.Source = "local"
				}
				m.skills[skill.Name] = skill
			}
		}
	}

	return nil
}

func (m *Manager) loadSkillFromManifest(manifestPath string) *Skill {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}

	var skill Skill
	if err := json.Unmarshal(data, &skill); err != nil {
		return nil
	}

	if skill.Content == "" {
		dir := filepath.Dir(manifestPath)
		for _, name := range []string{"README.md", "skill.md", "content.md"} {
			contentPath := filepath.Join(dir, name)
			if data, err := os.ReadFile(contentPath); err == nil {
				skill.Content = string(data)
				break
			}
		}
	}

	return &skill
}

func (m *Manager) loadSkillFromFile(path string) *Skill {
	ext := filepath.Ext(path)

	// If path is a directory, it's an internal error - return nil
	if ext == "" && path != "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return nil
		}
	}

	var skill *Skill
	switch ext {
	case ".json":
		skill = m.loadJSONSkill(path)
	case ".md", ".markdown":
		skill = m.loadMarkdownSkill(path)
	default:
		skill = m.loadTextSkill(path)
	}

	if skill != nil {
		// Inject absolute skill directory path
		absPath, _ := filepath.Abs(path)
		skillDir := filepath.Dir(absPath)
		skill.Dir = skillDir

		// Load origin metadata to determine source
		if originMeta, err := LoadSkillOriginMeta(skillDir); err == nil && originMeta != nil {
			// Any hub source should be marked as registry source
			switch HubSource(originMeta.Registry) {
			case HubSourceHub, HubSourceGitHub, HubSourceOfficial, HubSourceSkillsSh, HubSourceWellKnown:
				skill.Source = SkillSourceRegistry
			}
		}

		// Scan scripts/ directory
		scriptsDir := filepath.Join(skillDir, "scripts")
		if entries, err := os.ReadDir(scriptsDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					skill.Scripts = append(skill.Scripts, filepath.Join("scripts", e.Name()))
				}
			}
		}
	}

	return skill
}

func (m *Manager) loadJSONSkill(path string) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var skill Skill
	if err := json.Unmarshal(data, &skill); err != nil {
		return nil
	}

	if skill.Name == "" {
		skill.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return &skill
}

func (m *Manager) loadMarkdownSkill(path string) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	content := string(data)

	// Default values
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	description := "Markdown skill"

	// If file is SKILL.md, use parent directory name as skill name
	if name == "SKILL" {
		name = filepath.Base(filepath.Dir(path))
	}

	// Try to use the parser package for full OpenClaw/Hermes parsing
	skillDir := filepath.Dir(path)
	p := parser.NewParser()
	result, parseErr := p.Parse(skillDir)
	if parseErr == nil && result != nil {
		// Successfully parsed with the unified parser
		skill := &Skill{
			SkillMeta: SkillMeta{
				Name:        result.Name,
				Description: extractString(result.Data, "description", description),
				Version:     extractString(result.Data, "version", ""),
				Author:      extractString(result.Data, "author", ""),
				License:     extractString(result.Data, "license", ""),
				Tags:        extractStringSlice(result.Data, "tags"),
			},
			Tools:    extractStringSlice(result.Data, "tools"),
			Content:  content,
			Metadata: make(map[string]interface{}),
		}

		// If name is empty (parser didn't find one), fall back to directory/file name
		if skill.Name == "" {
			skill.Name = name
		}

		// Copy OpenClaw-specific fields into Metadata
		if tc, ok := result.Data["trigger_conditions"]; ok {
			skill.Metadata["trigger_conditions"] = tc
		}
		if steps, ok := result.Data["steps"]; ok {
			skill.Metadata["steps"] = steps
		}
		// Copy any remaining Data fields into Metadata
		for k, v := range result.Data {
			if k != "description" && k != "version" && k != "author" &&
				k != "license" && k != "tags" && k != "tools" &&
				k != "trigger_conditions" && k != "steps" {
				skill.Metadata[k] = v
			}
		}

		return skill
	}

	// Fallback: use the simple frontmatter parser
	tags := []string{}
	tools := []string{}

	if strings.HasPrefix(content, "---") {
		endMarker := strings.Index(content[3:], "---")
		if endMarker != -1 {
			frontmatter := content[3 : endMarker+3]
			name, description, tags, tools = parseFrontmatter(frontmatter, name)
		}
	}

	skill := &Skill{
		SkillMeta: SkillMeta{
			Name:        name,
			Description: description,
			Tags:        tags,
		},
		Tools:    tools,
		Content:  content,
		Metadata: make(map[string]interface{}),
	}

	return skill
}

// loadSkillFromContent loads a skill from markdown content string (for embedded skills)
func (m *Manager) loadSkillFromContent(content, dirName string) *Skill {
	name := dirName
	description := "Built-in skill"

	tags := []string{}
	tools := []string{}

	if strings.HasPrefix(content, "---") {
		endMarker := strings.Index(content[3:], "---")
		if endMarker != -1 {
			frontmatter := content[3 : endMarker+3]
			name, description, tags, tools = parseFrontmatter(frontmatter, name)
		}
	}

	return &Skill{
		SkillMeta: SkillMeta{
			Name:        name,
			Description: description,
			Tags:        tags,
		},
		Tools:    tools,
		Content:  content,
		Metadata: make(map[string]interface{}),
	}
}

// extractString safely extracts a string value from a map
func extractString(data map[string]interface{}, key, defaultVal string) string {
	if data == nil {
		return defaultVal
	}
	if v, ok := data[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

// extractStringSlice safely extracts a []string value from a map
func extractStringSlice(data map[string]interface{}, key string) []string {
	if data == nil {
		return nil
	}
	if v, ok := data[key].([]string); ok {
		return v
	}
	// Also handle []interface{} (from YAML parsing)
	if v, ok := data[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// parseFrontmatter 解析 YAML frontmatter
func parseFrontmatter(frontmatter, defaultName string) (name, description string, tags, tools []string) {
	name = defaultName
	tags = []string{}
	tools = []string{}

	lines := strings.Split(frontmatter, "\n")
	inTags := false
	inTools := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and nested YAML
		if line == "" || strings.HasPrefix(line, "metadata:") || strings.HasPrefix(line, "hermes:") {
			continue
		}

		// Handle tags array
		if strings.HasPrefix(line, "tags:") {
			inTags = true
			inTools = false
			value := strings.TrimSpace(strings.TrimPrefix(line, "tags:"))
			if value != "" && value != "[]" {
				tags = parseArrayLine(value)
			}
			continue
		}

		// Handle tools array
		if strings.HasPrefix(line, "tools:") {
			inTools = true
			inTags = false
			value := strings.TrimSpace(strings.TrimPrefix(line, "tools:"))
			if value != "" && value != "[]" {
				tools = parseArrayLine(value)
			}
			continue
		}

		// If we're in an array block
		if inTags || inTools {
			if strings.HasPrefix(line, "-") {
				item := strings.TrimSpace(strings.TrimPrefix(line, "-"))
				item = strings.Trim(item, "[],")
				if item != "" {
					if inTags {
						tags = append(tags, item)
					} else {
						tools = append(tools, item)
					}
				}
			} else if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				// Exited array block
				inTags = false
				inTools = false
			}
		}

		// Parse key: value
		if idx := strings.Index(line, ":"); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])

			// Remove quotes
			value = strings.Trim(value, "\"'")

			switch key {
			case "name":
				if value != "" {
					name = value
				}
			case "description":
				if value != "" {
					description = value
				}
			}
		}
	}

	return
}

func parseArrayLine(line string) []string {
	// Handle inline array like [tag1, tag2, tag3]
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		line = strings.Trim(line, "[]")
		parts := strings.Split(line, ",")
		result := make([]string, 0)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, "\"'")
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	}
	return []string{}
}

func (m *Manager) loadTextSkill(path string) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	skill := &Skill{
		SkillMeta: SkillMeta{
			Name: name,
		},
		Content: string(data),
	}

	return skill
}

// List returns all loaded skills
func (m *Manager) List() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skills := make([]*Skill, 0, len(m.skills))
	for _, s := range m.skills {
		skills = append(skills, s)
	}
	return skills
}

// Get retrieves a skill by name
func (m *Manager) Get(name string) (*Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skill, ok := m.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill %s not found", name)
	}
	return skill, nil
}

// Add adds a new skill
func (m *Manager) Add(skill *Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.addInternal(skill)
}

// RegisterSkill registers a skill in memory (no save to disk) - used by auto-creator
func (m *Manager) RegisterSkill(skill *Skill) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills[skill.Name] = skill
}

// addInternal is the locked add implementation
func (m *Manager) addInternal(skill *Skill) error {
	data, err := json.MarshalIndent(skill, "", "  ")
	if err != nil {
		return err
	}

	// Save to first search directory
	if len(m.searchDirs) == 0 {
		return fmt.Errorf("no search directories configured")
	}

	// Ensure directory exists
	if err := os.MkdirAll(m.searchDirs[0], 0755); err != nil {
		return err
	}

	path := filepath.Join(m.searchDirs[0], skill.Name+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	m.skills[skill.Name] = skill
	return nil
}

// Remove removes a skill
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try to remove from all search directories
	for _, dir := range m.searchDirs {
		path := filepath.Join(dir, name+".json")
		if err := os.Remove(path); err == nil {
			delete(m.skills, name)
			return nil
		}
		// Also try directory format
		dirPath := filepath.Join(dir, name)
		if err := os.RemoveAll(dirPath); err == nil {
			delete(m.skills, name)
			return nil
		}
	}

	return fmt.Errorf("skill %s not found in any search directory", name)
}

// GetSkillsContext returns all skills formatted for system prompt
// Note: pending/rejected auto-skills are NOT included here - they need manual approval first.
func (m *Manager) GetSkillsContext() string {
	var ctx string
	for _, skill := range m.skills {
		// 跳过未批准和已拒绝的自动生成技能
		if skill.Source == SkillSourceAuto &&
			(skill.Status == SkillStatusPending || skill.Status == SkillStatusRejected) {
			continue
		}
		content := skill.ResolveContent("")
		ctx += fmt.Sprintf("\n--- Skill: %s ---\n[Skill directory: %s]\n", skill.Name, skill.Dir)
		if files := skill.SupportingFiles(); files != "" {
			ctx += files + "\n"
		}
		ctx += content + "\n"
	}
	return ctx
}

// MatchSkillsByInput 根据输入匹配相关技能
func (m *Manager) MatchSkillsByInput(input string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	input = strings.ToLower(input)
	matched := make([]*Skill, 0)
	matchedSet := make(map[string]bool)

	tryAdd := func(skill *Skill) {
		if !matchedSet[skill.Name] {
			matchedSet[skill.Name] = true
			matched = append(matched, skill)
		}
	}

	for _, skill := range m.skills {
		// Check name
		if strings.Contains(strings.ToLower(skill.Name), input) {
			tryAdd(skill)
			continue
		}

		// Check description
		if strings.Contains(strings.ToLower(skill.Description), input) {
			tryAdd(skill)
			continue
		}

		// Check tags
		for _, tag := range skill.GetTags() {
			if strings.Contains(strings.ToLower(tag), input) {
				tryAdd(skill)
				break
			}
		}

		// Check trigger_conditions in Metadata (supports both []string and string)
		if skill.Metadata != nil {
			if conditions, ok := skill.Metadata["trigger_conditions"]; ok {
				matched := false
				switch condVal := conditions.(type) {
				case []string:
					for _, cond := range condVal {
						if strings.Contains(strings.ToLower(cond), input) {
							matched = true
							break
						}
					}
				case string:
					if strings.Contains(strings.ToLower(condVal), input) {
						matched = true
					}
				}
				if matched {
					tryAdd(skill)
				}
			}
		}
	}

	return matched
}

// Reload reloads all skills
func (m *Manager) Reload() error {
	m.mu.Lock()
	m.skills = make(map[string]*Skill)
	m.mu.Unlock()

	// Reload built-in skills first
	if m.builtinDir != "" {
		if err := m.loadBuiltinSkills(); err != nil {
			fmt.Printf("Warning: failed to load built-in skills: %v\n", err)
		}
	}

	return m.loadSkills()
}

// Count returns the number of skills
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.skills)
}

// Search searches for skills by keyword
func (m *Manager) Search(keyword string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keyword = strings.ToLower(keyword)
	matched := make([]*Skill, 0)

	for _, skill := range m.skills {
		if strings.Contains(strings.ToLower(skill.Name), keyword) {
			matched = append(matched, skill)
			continue
		}
		if strings.Contains(strings.ToLower(skill.Description), keyword) {
			matched = append(matched, skill)
			continue
		}
		for _, tag := range skill.GetTags() {
			if strings.Contains(strings.ToLower(tag), keyword) {
				matched = append(matched, skill)
				break
			}
		}
	}

	return matched
}

// InstallFromURL installs a skill directly from a URL
func (m *Manager) InstallFromURL(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Try to parse as JSON first
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var skill Skill
	if err := json.Unmarshal(data, &skill); err == nil && skill.Name != "" {
		return m.Add(&skill)
	}

	// Try as plain markdown/text
	skill = Skill{
		SkillMeta: SkillMeta{
			Name:        "imported-skill",
			Description: "Skill imported from URL",
			Tags:        []string{"imported"},
		},
		Content: string(data),
	}

	return m.Add(&skill)
}

// Implement SkillInfoProvider interface for tool.SkillInvokeTool
// ListSkills returns skill names
func (m *Manager) ListSkills() []string {
	skills := m.List()
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	return names
}

// GetSkillInfo returns skill info by name
func (m *Manager) GetSkillInfo(name string) (description string, tools []string, content string, err error) {
	skill, err := m.Get(name)
	if err != nil {
		return "", nil, "", err
	}
	return skill.Description, skill.GetTools(), skill.Content, nil
}

// GetSkillsList returns a compact list of skill names and descriptions
// for system prompt injection (instead of full skill content which can be 48KB+)
// Note: pending/rejected auto-skills are NOT included - they need manual approval.
func (m *Manager) GetSkillsList() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Available skills (use /skill <name> for details):\n")
	for name, skill := range m.skills {
		if skill.Source == SkillSourceAuto &&
			(skill.Status == SkillStatusPending || skill.Status == SkillStatusRejected) {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, skill.Description))
	}
	return sb.String()
}

// ScanSkillSecurity 扫描技能内容的安全性
// 参考 Hermes Agent 的安全扫描：检测数据泄露、提示注入、破坏性命令
func ScanSkillSecurity(content string) *SecurityScanResult {
	result := &SecurityScanResult{
		ScannedAt: time.Now().Format(time.RFC3339),
		Safe:      true,
	}

	// 检测高危提示注入模式（只检测真正危险的组合）
	injectionPatterns := []string{
		"ignore all previous instructions and",
		"ignore all above instructions and",
		"ignore previous instructions and do",
	}
	for _, pattern := range injectionPatterns {
		if strings.Contains(strings.ToLower(content), pattern) {
			result.Safe = false
			result.Threats = append(result.Threats, "potential prompt injection: "+pattern)
			result.Severity = "high"
		}
	}

	// 检测破坏性命令
	destructivePatterns := []string{
		"rm -rf /",
		"rm -rf /*",
		"mkfs",
		"dd if=",
		":(){ :|:& };:", // fork bomb
		"chmod -R 777 /",
	}
	for _, pattern := range destructivePatterns {
		if strings.Contains(strings.ToLower(content), pattern) {
			result.Safe = false
			result.Threats = append(result.Threats, "destructive command: "+pattern)
			result.Severity = "high"
		}
	}

	return result
}

// GetSkillsContextForPrompt 返回用于系统提示词注入的技能索引（Level 0）
// 参考 Hermes Agent 的 ephemeral 注入：只注入名称和描述，不注入完整内容
// Note: pending/rejected auto-skills are NOT included - they need manual approval.
func (m *Manager) GetSkillsContextForPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Available skills (use skill_view for details):\n")
	for name, skill := range m.skills {
		// 跳过未批准和已拒绝的自动生成技能
		if skill.Source == SkillSourceAuto &&
			(skill.Status == SkillStatusPending || skill.Status == SkillStatusRejected) {
			continue
		}
		desc := skill.Description
		if desc == "" {
			desc = "No description"
		}
		sb.WriteString(fmt.Sprintf("  /%s: %s\n", name, desc))
	}
	return sb.String()
}

// List returns all skills as a map (for tool interface)
func (m *Manager) ListAll() map[string]*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*Skill, len(m.skills))
	for k, v := range m.skills {
		result[k] = v
	}
	return result
}

// GetSkillDir returns the directory path for a skill
func (m *Manager) GetSkillDir(name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skill, ok := m.skills[name]
	if !ok {
		return "", fmt.Errorf("skill %s not found", name)
	}
	return skill.Dir, nil
}

// GetSkillsBySource returns skills filtered by source
func (m *Manager) GetSkillsBySource(source SkillSource) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Skill
	for _, skill := range m.skills {
		if skill.Source == source {
			result = append(result, skill)
		}
	}
	return result
}

// GetSkillSources returns all unique skill sources
func (m *Manager) GetSkillSources() []SkillSource {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sourceSet := make(map[SkillSource]bool)
	for _, skill := range m.skills {
		sourceSet[skill.Source] = true
	}

	sources := make([]SkillSource, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return string(sources[i]) < string(sources[j])
	})
	return sources
}

// GetSkillSourceStats returns statistics about skill sources
func (m *Manager) GetSkillSourceStats() map[SkillSource]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[SkillSource]int)
	for _, skill := range m.skills {
		stats[skill.Source]++
	}
	return stats
}

// Create creates a new skill from scratch
func (m *Manager) Create(name, description, content string, tags []string) (*Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if skill already exists
	if _, ok := m.skills[name]; ok {
		return nil, fmt.Errorf("skill %s already exists", name)
	}

	// Find or create skill directory
	var skillDir string
	for _, dir := range m.searchDirs {
		if strings.HasPrefix(dir, os.Getenv("HOME")) || strings.HasPrefix(dir, "/workspace/projects") {
			skillDir = filepath.Join(dir, name)
			if err := os.MkdirAll(skillDir, 0755); err == nil {
				break
			}
		}
	}

	if skillDir == "" {
		skillDir = filepath.Join(m.searchDirs[0], name)
		os.MkdirAll(skillDir, 0755)
	}

	// Write SKILL.md file
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	skillContent := fmt.Sprintf(`---
name: %s
description: %s
version: 1.0.0
author: magic-agent
tags: [%s]
---

# %s

## When to Use
%s

## Procedure
<!-- 描述具体的操作步骤 -->

## Pitfalls
<!-- 已知的陷阱和注意事项 -->

## Verification
<!-- 如何验证操作成功 -->
`, name, description, strings.Join(tags, ", "), name, content)

	if err := os.WriteFile(skillMdPath, []byte(skillContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write skill file: %w", err)
	}

	// Create and add skill
	skill := &Skill{
		SkillMeta: SkillMeta{
			Name:        name,
			Description: description,
			Version:     "1.0.0",
			Tags:        tags,
			Source:      SkillSourceLocal,
		},
		Content: content,
		Dir:     skillDir,
	}

	m.skills[name] = skill
	return skill, nil
}

// Update updates an existing skill's content
func (m *Manager) Update(name, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	// Update content
	skill.Content = content

	// Write to file
	if skill.Dir == "" {
		return fmt.Errorf("skill %s has no directory", name)
	}

	skillMdPath := filepath.Join(skill.Dir, "SKILL.md")
	return os.WriteFile(skillMdPath, []byte(content), 0644)
}

// UpdateMetadata updates skill metadata (name, description, tags)
func (m *Manager) UpdateMetadata(name string, meta SkillMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	// Update metadata fields
	skill.Name = meta.Name
	skill.Description = meta.Description
	skill.Tags = meta.Tags

	// Write to skill.yaml if directory exists
	if skill.Dir == "" {
		return nil
	}

	skillFile := filepath.Join(skill.Dir, "skill.yaml")
	content := fmt.Sprintf("name: %s\ndescription: %s\ntags: %s\n",
		meta.Name, meta.Description, strings.Join(meta.Tags, ","))
	return os.WriteFile(skillFile, []byte(content), 0644)
}

// Delete removes a skill
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	// Remove directory
	if skill.Dir != "" {
		if err := os.RemoveAll(skill.Dir); err != nil {
			return fmt.Errorf("failed to remove skill directory: %w", err)
		}
	}

	delete(m.skills, name)
	return nil
}

// Patch 对技能内容进行定向修补（参考 Hermes Agent 的 patch 操作）
// 比 update 更节省 token，只替换指定的文本片段
func (m *Manager) Patch(name, oldString, newString string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	if skill.Dir == "" {
		return fmt.Errorf("skill %s has no directory", name)
	}

	// 读取当前 SKILL.md 文件
	skillMdPath := filepath.Join(skill.Dir, "SKILL.md")
	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return fmt.Errorf("failed to read skill file: %w", err)
	}

	content := string(data)

	// 检查 oldString 是否存在
	if !strings.Contains(content, oldString) {
		return fmt.Errorf("old_string not found in skill %s content", name)
	}

	// 执行替换（仅替换第一个匹配）
	newContent := strings.Replace(content, oldString, newString, 1)

	// 安全扫描新内容
	scanResult := ScanSkillSecurity(newString)
	if !scanResult.Safe {
		return fmt.Errorf("patch blocked by security scan: %s", strings.Join(scanResult.Threats, "; "))
	}

	// 写回文件
	if err := os.WriteFile(skillMdPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write patched skill file: %w", err)
	}

	// 更新内存中的内容
	skill.Content = newContent

	return nil
}

// =============================================================================
// Skills Hub Integration
// =============================================================================

// HubConfig contains hub/source configuration
type HubConfig struct {
	Sources  []HubSourceConfig `json:"sources"`
	CacheDir string            `json:"cache_dir"`
	CacheTTL int               `json:"cache_ttl_hours"` // Hours to cache
}

// HubSourceConfig describes a skill source
type HubSourceConfig struct {
	Type HubSource `json:"type"`
	Name string    `json:"name"`
	URL  string    `json:"url,omitempty"`
}

// SearchHub searches all configured hubs for skills using the new registry manager
func (m *Manager) SearchHub(keyword string, sources []HubSource) ([]HubSkill, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Only search ClawHub - simplified to single source
	return m.registryMgr.SearchAll(ctx, keyword, 20)
}

// InstallFromHub installs a skill from a hub source using the registry manager
func (m *Manager) InstallFromHub(source HubSource, sourceID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Only ClawHub is supported for remote installation
	if source != HubSourceHub {
		return fmt.Errorf("only ClawHub source is supported for remote installation")
	}

	// Get ClawHub registry
	reg := m.registryMgr.GetRegistry("clawhub")
	if reg == nil {
		return fmt.Errorf("ClawHub registry not found")
	}

	// Create staging directory
	tmpDir, err := os.MkdirTemp("", "go-magic-skill-staging-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download and install to staging
	if err := reg.DownloadAndInstall(ctx, sourceID, "", tmpDir); err != nil {
		return fmt.Errorf("failed to download skill: %w", err)
	}

	// Security scan
	if err := m.scanAndInstallFromStaging(tmpDir, source, sourceID); err != nil {
		return err
	}

	return nil
}

// scanAndInstallFromStaging scans staged skills and installs them with proper directory structure
func (m *Manager) scanAndInstallFromStaging(stagingDir string, source HubSource, sourceID string) error {
	// Find the main SKILL.md file (only install once per download)
	var skillMDPath string
	var found bool

	err := filepath.WalkDir(stagingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "SKILL.md") {
			skillMDPath = path
			found = true
			return filepath.SkipAll // Stop after finding first SKILL.md
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to scan staged skills: %w", err)
	}

	if !found {
		return fmt.Errorf("no SKILL.md found in downloaded content")
	}

	// skillDir is the directory containing SKILL.md
	skillDir := filepath.Dir(skillMDPath)

	// Load skill from the SKILL.md file
	skill := m.loadSkillFromFile(skillMDPath)
	if skill == nil {
		return fmt.Errorf("failed to load skill from %s", skillMDPath)
	}

	// Security scan
	scanResult := ScanSkillSecurity(skill.Content)
	if !scanResult.Safe {
		return fmt.Errorf("security scan failed: %v", scanResult.Threats)
	}

	// Install skill with proper directory structure
	if err := m.installSkillWithDirectory(skill, skillDir, source, sourceID); err != nil {
		return fmt.Errorf("failed to install skill: %w", err)
	}

	// Record to Hub lock
	lockEntry := HubLockEntry{
		SkillName:     skill.Name,
		Source:        source,
		SourceID:      sourceID,
		InstalledAt:   time.Now(),
		SecurityAudit: "passed",
	}
	_ = m.AddHubLockEntry(lockEntry)

	return nil
}

// installSkillWithDirectory installs a skill with its complete directory structure
func (m *Manager) installSkillWithDirectory(skill *Skill, sourceDir string, source HubSource, sourceID string) error {
	if len(m.searchDirs) == 0 {
		return fmt.Errorf("no search directories configured")
	}

	// Use sourceID as directory name for consistency
	// If sourceID contains '/', use the last part (e.g., "owner/repo" -> "repo")
	skillDirName := sourceID
	if idx := strings.LastIndex(sourceID, "/"); idx >= 0 {
		skillDirName = sourceID[idx+1:]
	}
	if skillDirName == "" {
		skillDirName = skill.Name
	}

	// Create skill directory in the first search directory
	skillDir := filepath.Join(m.searchDirs[0], skillDirName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	// Copy all files from source directory to skill directory
	if err := copyDirectory(sourceDir, skillDir); err != nil {
		return fmt.Errorf("failed to copy skill files: %w", err)
	}

	// Save origin metadata
	registryURL := ""
	switch source {
	case HubSourceHub:
		registryURL = fmt.Sprintf("https://clawhub.ai/skills/%s", sourceID)
	case HubSourceGitHub:
		registryURL = fmt.Sprintf("https://github.com/%s", sourceID)
	default:
		if sourceID != "" {
			registryURL = sourceID
		}
	}
	originMeta := &SkillOriginMeta{
		Version:     1,
		OriginKind:  "third_party",
		Registry:    string(source),
		Slug:        sourceID,
		RegistryURL: registryURL,
		VersionStr:  "1.0.0",
		InstalledAt: time.Now().Unix(),
	}
	if err := SaveSkillOriginMeta(skillDir, originMeta); err != nil {
		fmt.Printf("Warning: failed to save origin metadata: %v\n", err)
	}

	// Reload skill from the new directory by finding SKILL.md inside it
	var reloadedSkill *Skill
	_ = filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "SKILL.md") {
			reloadedSkill = m.loadSkillFromFile(path)
			return filepath.SkipAll
		}
		return nil
	})
	if reloadedSkill != nil {
		reloadedSkill.Source = SkillSourceRegistry
		m.mu.Lock()
		m.skills[reloadedSkill.Name] = reloadedSkill
		m.mu.Unlock()
	}

	return nil
}

// copyDirectory copies all files from source to destination directory
func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from source
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		// Skip if it's the source directory itself
		if relPath == "." {
			return nil
		}

		destPath := filepath.Join(destination, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		// Use 0644 for all files (Windows compatible)
		return os.WriteFile(destPath, data, 0644)
	})
}

// extractZip 解压 ZIP 数据到指定目录
func (m *Manager) extractZip(data []byte, destDir string) error {
	reader := bytes.NewReader(data)
	zipReader, err := zip.NewReader(reader, int64(len(data)))
	if err != nil {
		return err
	}

	for _, file := range zipReader.File {
		// 防止路径遍历
		if strings.Contains(file.Name, "..") {
			continue
		}

		path := filepath.Join(destDir, file.Name)

		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}

		rc, err := file.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// =============================================================================
// Hub Lock Management (Hub 安装跟踪) - 参考 Hermes Agent .hub/lock.json
// =============================================================================

// lock.json 路径
func (m *Manager) hubLockPath() string {
	return filepath.Join(m.hubDir, "lock.json")
}

// loadHubLock 从 lock.json 加载 Hub 安装记录
func (m *Manager) loadHubLock() {
	m.hubLock = &HubLock{Entries: []HubLockEntry{}}

	lockPath := m.hubLockPath()
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return // 文件不存在或读取失败，返回空 lock
	}

	if err := json.Unmarshal(data, m.hubLock); err != nil {
		fmt.Printf("Warning: failed to parse hub lock: %v\n", err)
	}
}

// saveHubLock 保存 Hub 安装记录到 lock.json
func (m *Manager) saveHubLock() error {
	if m.hubLock == nil {
		m.hubLock = &HubLock{Entries: []HubLockEntry{}}
	}
	m.hubLock.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(m.hubLock, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hub lock: %w", err)
	}

	return os.WriteFile(m.hubLockPath(), data, 0644)
}

// AddHubLockEntry 添加 Hub 安装记录
func (m *Manager) AddHubLockEntry(entry HubLockEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在，存在则更新
	for i, e := range m.hubLock.Entries {
		if e.SkillName == entry.SkillName {
			m.hubLock.Entries[i] = entry
			return m.saveHubLock()
		}
	}

	m.hubLock.Entries = append(m.hubLock.Entries, entry)
	return m.saveHubLock()
}

// RemoveHubLockEntry 移除 Hub 安装记录
func (m *Manager) RemoveHubLockEntry(skillName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, e := range m.hubLock.Entries {
		if e.SkillName == skillName {
			m.hubLock.Entries = append(m.hubLock.Entries[:i], m.hubLock.Entries[i+1:]...)
			return m.saveHubLock()
		}
	}

	return nil
}

// GetHubLockEntries 返回所有 Hub 安装记录
func (m *Manager) GetHubLockEntries() []HubLockEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.hubLock == nil {
		return []HubLockEntry{}
	}
	return m.hubLock.Entries
}

// GetHubLockEntry 获取指定技能的 Hub 安装记录
func (m *Manager) GetHubLockEntry(skillName string) *HubLockEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.hubLock == nil {
		return nil
	}

	for _, e := range m.hubLock.Entries {
		if e.SkillName == skillName {
			return &e
		}
	}
	return nil
}

// IsHubInstalled 检查技能是否从 Hub 安装
func (m *Manager) IsHubInstalled(skillName string) bool {
	return m.GetHubLockEntry(skillName) != nil
}

// UninstallHubSkill 从 Hub 卸载技能（参考 Hermes Agent hermes skills uninstall）
func (m *Manager) UninstallHubSkill(skillName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否从 Hub 安装
	entry := m.GetHubLockEntry(skillName)
	if entry == nil {
		return fmt.Errorf("skill %s is not a hub skill", skillName)
	}

	// 删除技能文件
	skill, ok := m.skills[skillName]
	if ok && skill.Dir != "" {
		if err := os.RemoveAll(skill.Dir); err != nil {
			return fmt.Errorf("failed to remove skill directory: %w", err)
		}
		delete(m.skills, skillName)
	}

	// 从 lock.json 移除记录
	if err := m.RemoveHubLockEntry(skillName); err != nil {
		return fmt.Errorf("failed to remove lock entry: %w", err)
	}

	return nil
}

// appendAuditLog 添加审计日志
func (m *Manager) appendAuditLog(action, skillName string, source HubSource, status string) {
	auditLog := filepath.Join(m.hubDir, "audit.log")
	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("[%s] %s: skill=%s source=%s status=%s\n", timestamp, action, skillName, source, status)

	f, err := os.OpenFile(auditLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(logEntry)
}

// =============================================================================
// Bundled Manifest Management (内置技能跟踪) - 参考 Hermes Agent .bundled_manifest
// =============================================================================

// manifest 路径
func (m *Manager) bundledManifestPath() string {
	return filepath.Join(m.skillsDir, ".bundled_manifest")
}

// loadBundledManifest 从 .bundled_manifest 加载内置技能记录
func (m *Manager) loadBundledManifest() {
	m.bundledManifest = &BundledManifest{Entries: []BundledManifestEntry{}}

	manifestPath := m.bundledManifestPath()
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return // 文件不存在或读取失败，返回空 manifest
	}

	if err := json.Unmarshal(data, m.bundledManifest); err != nil {
		fmt.Printf("Warning: failed to parse bundled manifest: %v\n", err)
	}
}

// saveBundledManifest 保存内置技能记录到 .bundled_manifest
func (m *Manager) saveBundledManifest() error {
	if m.bundledManifest == nil {
		m.bundledManifest = &BundledManifest{Entries: []BundledManifestEntry{}}
	}
	m.bundledManifest.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(m.bundledManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal bundled manifest: %w", err)
	}

	return os.WriteFile(m.bundledManifestPath(), data, 0644)
}

// AddBundledManifestEntry 添加内置技能记录
func (m *Manager) AddBundledManifestEntry(entry BundledManifestEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, e := range m.bundledManifest.Entries {
		if e.SkillName == entry.SkillName {
			m.bundledManifest.Entries[i] = entry
			return m.saveBundledManifest()
		}
	}

	m.bundledManifest.Entries = append(m.bundledManifest.Entries, entry)
	return m.saveBundledManifest()
}

// GetBundledManifestEntries 返回所有内置技能记录
func (m *Manager) GetBundledManifestEntries() []BundledManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.bundledManifest == nil {
		return []BundledManifestEntry{}
	}
	return m.bundledManifest.Entries
}

// IsBundled 检查技能是否为内置技能
func (m *Manager) IsBundled(skillName string) bool {
	for _, e := range m.GetBundledManifestEntries() {
		if e.SkillName == skillName {
			return true
		}
	}
	return false
}

// =============================================================================
// Disabled Skills (禁用技能) - 参考 Hermes Agent config.yaml skills.disabled
// =============================================================================

// disabled.json 路径
func (m *Manager) disabledSkillsPath() string {
	return filepath.Join(m.skillsDir, ".disabled.json")
}

// loadDisabledSkills 从 .disabled.json 加载禁用技能配置
func (m *Manager) loadDisabledSkills() {
	m.disabledSkills = &DisabledSkillsConfig{Global: []string{}, Platform: make(map[string][]string)}

	disabledPath := m.disabledSkillsPath()
	data, err := os.ReadFile(disabledPath)
	if err != nil {
		return // 文件不存在或读取失败，返回空配置
	}

	if err := json.Unmarshal(data, m.disabledSkills); err != nil {
		fmt.Printf("Warning: failed to parse disabled skills: %v\n", err)
	}
}

// saveDisabledSkills 保存禁用技能配置到 .disabled.json
func (m *Manager) saveDisabledSkills() error {
	data, err := json.MarshalIndent(m.disabledSkills, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal disabled skills: %w", err)
	}

	return os.WriteFile(m.disabledSkillsPath(), data, 0644)
}

// DisableSkill 禁用指定技能
func (m *Manager) DisableSkill(skillName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, name := range m.disabledSkills.Global {
		if name == skillName {
			return nil // 已禁用
		}
	}

	m.disabledSkills.Global = append(m.disabledSkills.Global, skillName)
	return m.saveDisabledSkills()
}

// EnableSkill 启用指定技能
func (m *Manager) EnableSkill(skillName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, name := range m.disabledSkills.Global {
		if name == skillName {
			m.disabledSkills.Global = append(m.disabledSkills.Global[:i], m.disabledSkills.Global[i+1:]...)
			return m.saveDisabledSkills()
		}
	}

	return nil
}

// IsSkillDisabled 检查技能是否被禁用
func (m *Manager) IsSkillDisabled(skillName string, platform string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 检查全局禁用
	for _, name := range m.disabledSkills.Global {
		if name == skillName {
			return true
		}
	}

	// 检查平台禁用
	if platform != "" {
		if platformDisabled, ok := m.disabledSkills.Platform[platform]; ok {
			for _, name := range platformDisabled {
				if name == skillName {
					return true
				}
			}
		}
	}

	return false
}

// GetDisabledSkills 返回禁用技能列表
func (m *Manager) GetDisabledSkills() *DisabledSkillsConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &DisabledSkillsConfig{
		Global:   append([]string{}, m.disabledSkills.Global...),
		Platform: m.disabledSkills.Platform,
	}
}

// =============================================================================
// External Skill Directories
// =============================================================================

// GetVersions 获取技能版本历史（委托给 VersionManager）
func (m *Manager) GetVersions(skillName string) []SkillVersion {
	if len(m.searchDirs) == 0 {
		return []SkillVersion{}
	}
	vm := NewVersionManager(m.searchDirs[0])
	return vm.GetVersionHistory(skillName)
}

// GetEvolutionRecords 获取技能演化历史（委托给 SkillEvolutionManager）
func (m *Manager) GetEvolutionRecords(skillName string) []SkillEvolutionRecord {
	if len(m.searchDirs) == 0 {
		return []SkillEvolutionRecord{}
	}
	// 演化管理器需要 EffectivenessManager，这里简化为从文件加载
	em := NewSkillEvolutionManager(m, nil, nil, m.searchDirs[0])
	_ = em.LoadRecords()
	return em.GetEvolutionHistory(skillName)
}

// GetAllStatistics 获取所有技能统计数据（委托给 EffectivenessManager）
func (m *Manager) GetAllStatistics() []*SkillStatistics {
	if len(m.searchDirs) == 0 {
		return []*SkillStatistics{}
	}
	effMgr, err := NewEffectivenessManager(m.searchDirs[0])
	if err != nil {
		return []*SkillStatistics{}
	}
	return effMgr.GetAllStatistics()
}

// =============================================================================
// Auto-Skill Three-State Lifecycle Management (参考 Hermes Agent 的 approve/reject)
// =============================================================================

// GetAutoSkillsDir 返回自动技能的根目录
func (m *Manager) GetAutoSkillsDir() string {
	return m.autoSkillsDir
}

// SetAutoSkillsDir 设置自动技能根目录（供 server.go 在 cortex 绑定后覆盖）
func (m *Manager) SetAutoSkillsDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoSkillsDir = dir
	os.MkdirAll(filepath.Join(dir, "pending"), 0755)
	os.MkdirAll(filepath.Join(dir, "approved"), 0755)
	os.MkdirAll(filepath.Join(dir, "archived"), 0755)
}

// moveAutoSkill 将技能从一个状态目录移动到另一个状态目录
// 同时更新内存中的 skill.Status
func (m *Manager) moveAutoSkill(skillName string, from, to SkillStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[skillName]
	if !ok {
		return fmt.Errorf("skill %s not found", skillName)
	}
	if skill.Source != SkillSourceAuto {
		return fmt.Errorf("skill %s is not an auto-generated skill, cannot change status", skillName)
	}

	if skill.Status != from && skill.Status != "" {
		// 允许 from="" 表示首次从根目录迁移（兼容历史数据）
		if from != SkillStatusPending || skill.Status != from {
			return fmt.Errorf("skill %s status is %s, expected %s",
				skillName, skill.Status, from)
		}
	}

	// 确定源目录和目标目录
	srcDir := skill.Dir
	// 如果 skill.Dir 不包含状态子目录名，尝试根据当前状态推断
	if !strings.Contains(srcDir, string(from)) && from != "" {
		// 从 autoSkillsDir 的子目录中查找实际的源目录
		// 通过 dir 路径中的名字来精确定位
		candidate := filepath.Join(m.autoSkillsDir, string(from), filepath.Base(srcDir))
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			srcDir = candidate
		}
	}
	dstDir := filepath.Join(m.autoSkillsDir, string(to), filepath.Base(srcDir))

	if _, err := os.Stat(srcDir); err != nil {
		// 源目录不存在，只更新内存状态
		skill.Status = to
		skill.Dir = dstDir
		return nil
	}

	// 移动目录
	if err := os.Rename(srcDir, dstDir); err != nil {
		return fmt.Errorf("failed to move skill from %s to %s: %w", srcDir, dstDir, err)
	}

	skill.Status = to
	skill.Dir = dstDir

	// 同步更新 meta.json（如果存在）
	metaPath := filepath.Join(dstDir, "meta.json")
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta map[string]interface{}
		if json.Unmarshal(data, &meta) == nil {
			meta["status"] = string(to)
			if newData, err := json.MarshalIndent(meta, "", "  "); err == nil {
				os.WriteFile(metaPath, newData, 0644)
			}
		}
	}

	// 更新 tags 显示状态
	// 清理旧状态 tag，添加新状态 tag
	newTags := make([]string, 0, len(skill.Tags))
	for _, t := range skill.Tags {
		if t == "pending" || t == "approved" || t == "archived" || t == "rejected" {
			continue
		}
		newTags = append(newTags, t)
	}
	newTags = append(newTags, string(to))
	skill.Tags = newTags

	return nil
}

// ApproveAutoSkill 批准一个 pending 的自动生成技能（approved 状态会被 Agent 使用）
func (m *Manager) ApproveAutoSkill(skillName string) error {
	return m.moveAutoSkill(skillName, SkillStatusPending, SkillStatusApproved)
}

// RejectAutoSkill 拒绝一个 pending 的自动生成技能（标记为 rejected）
func (m *Manager) RejectAutoSkill(skillName string) error {
	return m.moveAutoSkill(skillName, SkillStatusPending, SkillStatusRejected)
}

// ArchiveAutoSkill 归档一个已批准技能（archive 状态，不被使用，保留内容）
func (m *Manager) ArchiveAutoSkill(skillName string) error {
	return m.moveAutoSkill(skillName, SkillStatusApproved, SkillStatusArchived)
}

// RestoreAutoSkill 从归档恢复到 approved
func (m *Manager) RestoreAutoSkill(skillName string) error {
	return m.moveAutoSkill(skillName, SkillStatusArchived, SkillStatusApproved)
}

// DeleteAutoSkill 彻底删除一个自动技能（通常是 rejected 或 archived 状态）
func (m *Manager) DeleteAutoSkill(skillName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[skillName]
	if !ok {
		return fmt.Errorf("skill %s not found", skillName)
	}
	if skill.Source != SkillSourceAuto {
		return fmt.Errorf("skill %s is not an auto-generated skill", skillName)
	}

	if skill.Dir != "" {
		if err := os.RemoveAll(skill.Dir); err != nil {
			return fmt.Errorf("failed to remove skill directory: %w", err)
		}
	}

	delete(m.skills, skillName)
	return nil
}

// ListAutoSkillsByStatus 按状态列出自动生成的技能
func (m *Manager) ListAutoSkillsByStatus(status SkillStatus) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Skill, 0)
	for _, skill := range m.skills {
		if skill.Source != SkillSourceAuto {
			continue
		}
		// 空状态视同 pending（兼容历史数据）
		effectiveStatus := skill.Status
		if effectiveStatus == "" {
			effectiveStatus = SkillStatusPending
		}
		if effectiveStatus == status {
			result = append(result, skill)
		}
	}
	return result
}

// GetSkillStatus 返回技能的有效状态（空状态视为 pending）
func (m *Manager) GetSkillStatus(skillName string) (SkillStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skill, ok := m.skills[skillName]
	if !ok {
		return "", fmt.Errorf("skill %s not found", skillName)
	}
	if skill.Source != SkillSourceAuto {
		return "", fmt.Errorf("skill %s is not an auto-generated skill", skillName)
	}
	if skill.Status == "" {
		return SkillStatusPending, nil
	}
	return skill.Status, nil
}

// GetSkillStatusCounts 返回各状态的自动技能数量（用于 Web UI 展示）
func (m *Manager) GetSkillStatusCounts() map[SkillStatus]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := map[SkillStatus]int{
		SkillStatusPending:  0,
		SkillStatusApproved: 0,
		SkillStatusArchived: 0,
		SkillStatusRejected: 0,
	}
	for _, skill := range m.skills {
		if skill.Source != SkillSourceAuto {
			continue
		}
		status := skill.Status
		if status == "" {
			status = SkillStatusPending
		}
		counts[status]++
	}
	return counts
}
