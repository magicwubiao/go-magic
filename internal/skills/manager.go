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
	bundles         map[string]*SkillBundle   // 技能捆绑包
	categories      map[string]*SkillCategory // 技能分类（按目录层级）
	toolNames       []string                  // Cached tool names from registry
	registryURL     string                    // ClawHub or GitHub registry URL
	hubLock         *HubLock                  // Hub 安装跟踪 (.hub/lock.json)
	bundledManifest *BundledManifest          // 内置技能跟踪 (.bundled_manifest)
	disabledSkills  *DisabledSkillsConfig     // 禁用技能配置
	skillsDir       string                    // 技能目录路径 (~/.magic/skills)
	hubDir          string                    // Hub 目录路径 (~/.magic/skills/.hub)
}

// ManagerConfig 配置管理器
type ManagerConfig struct {
	SearchDirs  []string // 搜索目录列表
	BuiltinDir  string   // 内置技能目录
	RegistryURL string   // 技能注册表 URL
	ToolNames   []string // 可用工具名称列表（用于技能验证）
}

// NewManager creates a new skill manager with default configuration
func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	config := &ManagerConfig{
		SearchDirs: []string{
			filepath.Join(home, ".magic", "skills"),
			"skills",
			filepath.Join(".magic", "skills"),
		},
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

	m := &Manager{
		searchDirs:     config.SearchDirs,
		builtinDir:     config.BuiltinDir,
		registryURL:    config.RegistryURL,
		toolNames:      config.ToolNames,
		skills:         make(map[string]*Skill),
		bundles:        make(map[string]*SkillBundle),
		categories:     make(map[string]*SkillCategory),
		skillsDir:      skillsDir,
		hubDir:         filepath.Join(skillsDir, ".hub"),
		disabledSkills: &DisabledSkillsConfig{Platform: make(map[string][]string)},
	}

	// 创建 Hub 目录
	os.MkdirAll(m.hubDir, 0755)

	// 加载 Hub lock.json
	m.loadHubLock()

	// 加载 Bundled manifest
	m.loadBundledManifest()

	// 加载禁用技能配置
	m.loadDisabledSkills()

	// Load built-in skills
	if config.BuiltinDir != "" {
		if err := m.loadBuiltinSkills(); err != nil {
			// Don't fail on error, just log
			fmt.Printf("Warning: failed to load built-in skills: %v\n", err)
		}
	}

	if err := m.loadSkills(); err != nil {
		return nil, err
	}

	return m, nil
}

// NewManagerWithToolRegistry creates a manager with tool registry integration
func NewManagerWithToolRegistry(toolNames []string) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	config := &ManagerConfig{
		SearchDirs: []string{
			filepath.Join(home, ".magic", "skills"),
			"skills",
			filepath.Join(".magic", "skills"),
		},
		ToolNames: toolNames,
	}

	m, err := NewManagerWithConfig(config)
	if err != nil {
		return nil, err
	}

	m.toolNames = toolNames

	return m, nil
}

// SetToolNames 设置可用工具名称列表
func (m *Manager) SetToolNames(names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolNames = names
}

// loadBuiltinSkills 加载内置技能
func (m *Manager) loadBuiltinSkills() error {
	if m.builtinDir == "" {
		return nil
	}

	entries, err := os.ReadDir(m.builtinDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
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

// LoadBundles 从配置目录加载技能捆绑包
func (m *Manager) LoadBundles(bundlesDir string) error {
	if bundlesDir == "" {
		return nil
	}

	entries, err := os.ReadDir(bundlesDir)
	if err != nil {
		return nil // 目录不存在不算错误
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(bundlesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var config SkillBundleConfig
		if err := parseBundleYAML(data, &config); err != nil {
			continue
		}

		bundle := &SkillBundle{
			Name:        config.Name,
			Description: config.Description,
			Instruction: config.Instruction,
		}

		// 加载捆绑包中的技能
		for _, skillName := range config.Skills {
			if skill, err := m.Get(skillName); err == nil {
				bundle.Skills = append(bundle.Skills, skill)
			}
			// 缺失的技能被跳过而非报错（参考 Hermes 行为）
		}

		m.mu.Lock()
		m.bundles[config.Name] = bundle
		m.mu.Unlock()
	}

	return nil
}

// parseBundleYAML 解析捆绑包 YAML 配置（简易解析器）
func parseBundleYAML(data []byte, config *SkillBundleConfig) error {
	lines := strings.Split(string(data), "\n")
	var currentList *[]string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "name:") {
			config.Name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "name:")), "\"'")
		} else if strings.HasPrefix(trimmed, "description:") {
			config.Description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "description:")), "\"'")
		} else if strings.HasPrefix(trimmed, "instruction:") {
			// 多行 instruction
			config.Instruction = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "instruction:")), "\"'")
			currentList = nil
		} else if strings.HasPrefix(trimmed, "skills:") {
			currentList = &config.Skills
		} else if currentList != nil && strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(trimmed[2:])
			item = strings.Trim(item, "\"'")
			*currentList = append(*currentList, item)
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
				// 检查是否是分类目录（包含子目录，但自身没有 SKILL.md）
				isCategory := m.scanCategoryDir(path, dir, entry.Name())

				// Check for SKILL.md (Cortex format) - 直接放在搜索目录下的技能
				skillMdPath := filepath.Join(path, "SKILL.md")
				if _, err := os.Stat(skillMdPath); err == nil {
					skill := m.loadSkillFromFile(skillMdPath)
					if skill != nil {
						skill.Source = "local"
						if dir == m.searchDirs[0] || strings.Contains(dir, ".magic") {
							skill.Source = "global"
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

				// 如果是分类目录且没有直接包含 SKILL.md，跳过（子技能已在 scanCategoryDir 中加载）
				if isCategory {
					continue
				}
				continue
			}

			skill := m.loadSkillFromFile(path)
			if skill != nil {
				skill.Source = "local"
				m.skills[skill.Name] = skill
			}
		}
	}

	return nil
}

// scanCategoryDir 扫描分类目录，加载子目录中的技能
// 返回 true 表示该目录被识别为分类目录
func (m *Manager) scanCategoryDir(categoryPath, parentDir, categoryName string) bool {
	entries, err := os.ReadDir(categoryPath)
	if err != nil {
		return false
	}

	// 检查是否有子目录（有子目录则视为分类目录）
	hasSubdirs := false
	var skillNames []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// 跳过排除目录
		if IsExcludedDir(entry.Name()) {
			continue
		}
		hasSubdirs = true

		subPath := filepath.Join(categoryPath, entry.Name())

		// 尝试加载子目录中的技能
		skillMdPath := filepath.Join(subPath, "SKILL.md")
		if _, err := os.Stat(skillMdPath); err == nil {
			skill := m.loadSkillFromFile(skillMdPath)
			if skill != nil {
				skill.Source = "local"
				if strings.Contains(parentDir, ".magic") {
					skill.Source = "global"
				}
				// 自动设置分类
				if skill.Category == "" {
					skill.Category = categoryName
				}
				m.skills[skill.Name] = skill
				skillNames = append(skillNames, skill.Name)
			}
			continue
		}

		// 递归扫描更深层的分类（支持多级分类）
		if m.scanCategoryDir(subPath, parentDir, categoryName+"/"+entry.Name()) {
			// 子分类已在递归中处理
		}
	}

	// 如果有子技能，注册为分类
	if len(skillNames) > 0 {
		absPath, _ := filepath.Abs(categoryPath)
		m.categories[categoryName] = &SkillCategory{
			Name:       categoryName,
			Path:       absPath,
			SkillCount: len(skillNames),
			Skills:     skillNames,
			Source:     SkillSourceGlobal,
		}
	}

	return hasSubdirs
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

// ListByTags returns skills filtered by tags
func (m *Manager) ListByTags(tags []string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Skill, 0)
	for _, s := range m.skills {
		for _, tag := range tags {
			for _, skillTag := range s.GetTags() {
				if strings.EqualFold(skillTag, tag) {
					result = append(result, s)
					break
				}
			}
		}
	}
	return result
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
func (m *Manager) GetSkillsContext() string {
	var ctx string
	for _, skill := range m.skills {
		content := skill.ResolveContent("")
		ctx += fmt.Sprintf("\n--- Skill: %s ---\n[Skill directory: %s]\n", skill.Name, skill.Dir)
		if files := skill.SupportingFiles(); files != "" {
			ctx += files + "\n"
		}
		ctx += content + "\n"
	}
	return ctx
}

// GetSkillsContextForTags returns skills context for specific tags
func (m *Manager) GetSkillsContextForTags(tags []string) string {
	skills := m.ListByTags(tags)
	if len(skills) == 0 {
		return ""
	}

	var ctx string
	for _, skill := range skills {
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

// SkillMetadata represents skill metadata from registry
type SkillMetadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
	URL         string   `json:"url"`
}

// SearchRegistry searches the skill registry
func (m *Manager) SearchRegistry(keyword string) ([]SkillMetadata, error) {
	// First, search local skills directory
	localResults := m.searchLocalSkills(keyword)

	// If registry URL is configured, search remote
	if m.registryURL != "" {
		remoteResults, err := m.searchRemoteRegistry(keyword)
		if err != nil {
			// Return local results even if remote fails
			return localResults, nil
		}
		// Merge results, avoiding duplicates
		return m.mergeMetadata(localResults, remoteResults), nil
	}

	return localResults, nil
}

// searchLocalSkills searches local skills directory
func (m *Manager) searchLocalSkills(keyword string) []SkillMetadata {
	keyword = strings.ToLower(keyword)
	var results []SkillMetadata

	for _, skill := range m.skills {
		if strings.Contains(strings.ToLower(skill.Name), keyword) ||
			strings.Contains(strings.ToLower(skill.Description), keyword) {
			results = append(results, SkillMetadata{
				Name:        skill.Name,
				Description: skill.Description,
				Version:     "local",
				Tags:        skill.GetTags(),
				URL:         "", // Local skill, no URL
			})
		}
	}

	return results
}

// searchRemoteRegistry searches the remote registry
func (m *Manager) searchRemoteRegistry(keyword string) ([]SkillMetadata, error) {
	// Build registry search URL
	searchURL := fmt.Sprintf("%s/skills/search?q=%s", strings.TrimSuffix(m.registryURL, "/"), keyword)

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var results []SkillMetadata
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to parse registry response: %w", err)
	}

	return results, nil
}

// mergeMetadata merges local and remote metadata, avoiding duplicates
func (m *Manager) mergeMetadata(local, remote []SkillMetadata) []SkillMetadata {
	seen := make(map[string]bool)

	var merged []SkillMetadata

	// Add local first (they take precedence)
	for _, m := range local {
		seen[m.Name] = true
		merged = append(merged, m)
	}

	// Add remote that aren't duplicates
	for _, r := range remote {
		if !seen[r.Name] {
			merged = append(merged, r)
		}
	}

	return merged
}

// InstallFromRegistry installs a skill from registry
func (m *Manager) InstallFromRegistry(name string) error {
	// Check if already installed
	if _, err := m.Get(name); err == nil {
		return fmt.Errorf("skill %s is already installed", name)
	}

	// If registry URL is set, try to download from remote
	if m.registryURL != "" {
		if err := m.installFromRemote(name); err != nil {
			return fmt.Errorf("failed to install from registry: %w", err)
		}
		return nil
	}

	// Try to find in local skills directory
	if err := m.installFromLocal(name); err != nil {
		return fmt.Errorf("skill %s not found in local registry", name)
	}

	return nil
}

// installFromRemote downloads and installs a skill from remote registry
func (m *Manager) installFromRemote(name string) error {
	// Build download URL
	downloadURL := fmt.Sprintf("%s/skills/%s/download", strings.TrimSuffix(m.registryURL, "/"), name)

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("skill not found in registry (status %d)", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "zip") {
		return m.installZipSkill(name, resp.Body)
	}

	// Otherwise, treat as JSON
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read skill data: %w", err)
	}

	var skill Skill
	if err := json.Unmarshal(data, &skill); err != nil {
		return fmt.Errorf("failed to parse skill: %w", err)
	}

	return m.Add(&skill)
}

// installZipSkill installs a skill from a ZIP archive
func (m *Manager) installZipSkill(name string, reader io.Reader) error {
	// Create temporary directory for extraction
	tmpDir, err := os.MkdirTemp("", "skill-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract ZIP
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read zip data: %w", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("failed to read zip: %w", err)
	}

	// Extract all files
	for _, f := range zipReader.File {
		fpath := filepath.Join(tmpDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("failed to extract file %s: %w", f.Name, err)
		}

		inFile, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open zip file %s: %w", f.Name, err)
		}

		_, err = io.Copy(outFile, inFile)
		outFile.Close()
		inFile.Close()
		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", f.Name, err)
		}
	}

	// Find skill file
	skillPath := filepath.Join(tmpDir, name+".json")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		// Try SKILL.md
		skillPath = filepath.Join(tmpDir, "SKILL.md")
	}

	// Load and add skill
	skill := m.loadSkillFromFile(skillPath)
	if skill == nil {
		return fmt.Errorf("failed to load skill from extracted files")
	}

	return m.Add(skill)
}

// installFromLocal installs a skill from local skills directory
func (m *Manager) installFromLocal(name string) error {
	// Search in local skills
	for _, dir := range m.searchDirs {
		// Try direct file match
		skillPath := filepath.Join(dir, name+".json")
		if _, err := os.Stat(skillPath); err == nil {
			skill := m.loadSkillFromFile(skillPath)
			if skill != nil {
				return m.Add(skill)
			}
		}

		// Try directory match
		skillDir := filepath.Join(dir, name)
		if info, err := os.Stat(skillDir); err == nil && info.IsDir() {
			// Check for SKILL.md
			manifestPath := filepath.Join(skillDir, "SKILL.md")
			if _, err := os.Stat(manifestPath); err == nil {
				skill := m.loadSkillFromManifest(manifestPath)
				if skill != nil {
					return m.Add(skill)
				}
			}

			// Check for manifest.json
			manifestPath = filepath.Join(skillDir, "manifest.json")
			if _, err := os.Stat(manifestPath); err == nil {
				skill := m.loadSkillFromManifest(manifestPath)
				if skill != nil {
					return m.Add(skill)
				}
			}
		}
	}

	return fmt.Errorf("skill %s not found in local directories", name)
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
func (m *Manager) GetSkillsList() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Available skills (use /skill <name> for details):\n")
	for name, skill := range m.skills {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, skill.Description))
	}
	return sb.String()
}

// GetBundle 获取技能捆绑包
func (m *Manager) GetBundle(name string) (*SkillBundle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bundle, ok := m.bundles[name]
	if !ok {
		return nil, fmt.Errorf("bundle %s not found", name)
	}
	return bundle, nil
}

// ListBundles 列出所有捆绑包
func (m *Manager) ListBundles() []*SkillBundle {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bundles := make([]*SkillBundle, 0, len(m.bundles))
	for _, b := range m.bundles {
		bundles = append(bundles, b)
	}
	return bundles
}

// ScanSkillSecurity 扫描技能内容的安全性
// 参考 Hermes Agent 的安全扫描：检测数据泄露、提示注入、破坏性命令
func ScanSkillSecurity(content string) *SecurityScanResult {
	result := &SecurityScanResult{
		ScannedAt: time.Now().Format(time.RFC3339),
		Safe:      true,
	}

	// 检测提示注入模式
	injectionPatterns := []string{
		"ignore all previous instructions",
		"ignore all above instructions",
		"you are now",
		"pretend you are",
		"new instructions:",
		"system prompt:",
		"forget everything",
	}
	for _, pattern := range injectionPatterns {
		if strings.Contains(strings.ToLower(content), pattern) {
			result.Safe = false
			result.Threats = append(result.Threats, "potential prompt injection: "+pattern)
			result.Severity = "high"
		}
	}

	// 检测数据泄露模式
	dataLeakPatterns := []string{
		"api_key",
		"secret_key",
		"password",
		"credential",
		"private_key",
		"token",
	}
	for _, pattern := range dataLeakPatterns {
		if strings.Contains(strings.ToLower(content), pattern) {
			// 检查是否是模板变量（如 ${API_KEY}）而非实际值
			if !strings.Contains(content, "${") && !strings.Contains(content, "{{") {
				result.Safe = false
				result.Threats = append(result.Threats, "potential data leak: "+pattern)
				if result.Severity != "high" {
					result.Severity = "medium"
				}
			}
		}
	}

	// 检测破坏性命令
	destructivePatterns := []string{
		"rm -rf /",
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
func (m *Manager) GetSkillsContextForPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Available skills (use skill_view for details):\n")
	for name, skill := range m.skills {
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

// GetCategories returns all unique skill categories
func (m *Manager) GetCategories() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	categorySet := make(map[string]bool)
	for _, skill := range m.skills {
		if skill.Category != "" {
			categorySet[skill.Category] = true
		}
	}

	categories := make([]string, 0, len(categorySet))
	for cat := range categorySet {
		categories = append(categories, cat)
	}
	sort.Strings(categories)
	return categories
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
func (m *Manager) Create(name, description, content, category string, tags []string) (*Skill, error) {
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
category: %s
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
`, name, description, category, strings.Join(tags, ", "), name, content)

	if err := os.WriteFile(skillMdPath, []byte(skillContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write skill file: %w", err)
	}

	// Create and add skill
	skill := &Skill{
		SkillMeta: SkillMeta{
			Name:        name,
			Description: description,
			Version:     "1.0.0",
			Category:    category,
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

// UpdateMetadata updates skill metadata (name, description, category, tags)
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
	skill.Category = meta.Category
	skill.Tags = meta.Tags

	// Write to skill.yaml if directory exists
	if skill.Dir == "" {
		return nil
	}

	skillFile := filepath.Join(skill.Dir, "skill.yaml")
	content := fmt.Sprintf("name: %s\ndescription: %s\ncategory: %s\ntags: %s\n",
		meta.Name, meta.Description, meta.Category, strings.Join(meta.Tags, ","))
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

// =============================================================================
// Category Management (分类管理) - 参考 Hermes Agent 目录层级分类
// =============================================================================

// GetSkillCategories 获取所有技能分类（目录层级分类）
func (m *Manager) GetSkillCategories() []*SkillCategory {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*SkillCategory, 0, len(m.categories))
	for _, cat := range m.categories {
		catCopy := *cat
		catCopy.Skills = append([]string{}, cat.Skills...)
		result = append(result, &catCopy)
	}

	// 按名称排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// GetCategory 获取指定分类
func (m *Manager) GetCategory(name string) (*SkillCategory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cat, ok := m.categories[name]
	if !ok {
		return nil, fmt.Errorf("category %s not found", name)
	}

	catCopy := *cat
	catCopy.Skills = append([]string{}, cat.Skills...)
	return &catCopy, nil
}

// GetSkillsInCategory 获取指定分类下的所有技能
func (m *Manager) GetSkillsInCategory(categoryName string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cat, ok := m.categories[categoryName]
	if !ok {
		return nil
	}

	result := make([]*Skill, 0, len(cat.Skills))
	for _, skillName := range cat.Skills {
		if skill, exists := m.skills[skillName]; exists {
			skillCopy := *skill
			result = append(result, &skillCopy)
		}
	}

	return result
}

// GetCategoryTree 获取分类树结构
func (m *Manager) GetCategoryTree() []*CategoryTree {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 构建分类树
	rootMap := make(map[string]*CategoryTree)

	for _, cat := range m.categories {
		tree := &CategoryTree{
			Category: &SkillCategory{
				Name:        cat.Name,
				Description: cat.Description,
				Path:        cat.Path,
				SkillCount:  cat.SkillCount,
				Skills:      append([]string{}, cat.Skills...),
				Parent:      cat.Parent,
				Source:      cat.Source,
			},
		}
		rootMap[cat.Name] = tree
	}

	// 构建父子关系
	var roots []*CategoryTree
	for name, tree := range rootMap {
		// 检查是否是子分类（名称包含 /）
		if idx := strings.LastIndex(name, "/"); idx > 0 {
			parentName := name[:idx]
			if parent, ok := rootMap[parentName]; ok {
				parent.Children = append(parent.Children, tree)
				tree.Category.Parent = parentName
				continue
			}
		}
		roots = append(roots, tree)
	}

	return roots
}

// CreateCategory 创建新分类目录
func (m *Manager) CreateCategory(name, description string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 确定分类目录路径
	var catDir string
	for _, dir := range m.searchDirs {
		if strings.HasPrefix(dir, os.Getenv("HOME")) || strings.Contains(dir, ".magic") {
			catDir = filepath.Join(dir, name)
			break
		}
	}

	if catDir == "" {
		catDir = filepath.Join(m.searchDirs[0], name)
	}

	// 创建目录
	if err := os.MkdirAll(catDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create category directory: %w", err)
	}

	// 注册分类
	absPath, _ := filepath.Abs(catDir)
	m.categories[name] = &SkillCategory{
		Name:        name,
		Description: description,
		Path:        absPath,
		Skills:      []string{},
		Source:      SkillSourceGlobal,
	}

	return catDir, nil
}

// DeleteCategory 删除分类（仅删除空分类）
func (m *Manager) DeleteCategory(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cat, ok := m.categories[name]
	if !ok {
		return fmt.Errorf("category %s not found", name)
	}

	// 检查分类下是否还有技能
	if cat.SkillCount > 0 {
		return fmt.Errorf("category %s is not empty (contains %d skills)", name, cat.SkillCount)
	}

	// 删除目录
	if cat.Path != "" {
		if err := os.Remove(cat.Path); err != nil {
			return fmt.Errorf("failed to remove category directory: %w", err)
		}
	}

	delete(m.categories, name)
	return nil
}

// MoveSkillToCategory 将技能移动到指定分类
func (m *Manager) MoveSkillToCategory(skillName, categoryName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[skillName]
	if !ok {
		return fmt.Errorf("skill %s not found", skillName)
	}

	cat, ok := m.categories[categoryName]
	if !ok {
		return fmt.Errorf("category %s not found", categoryName)
	}

	if skill.Dir == "" {
		return fmt.Errorf("skill %s has no directory", skillName)
	}

	// 移动目录
	newDir := filepath.Join(cat.Path, filepath.Base(skill.Dir))
	if err := os.Rename(skill.Dir, newDir); err != nil {
		return fmt.Errorf("failed to move skill directory: %w", err)
	}

	// 更新技能信息
	skill.Dir = newDir
	skill.Category = categoryName

	// 更新分类信息
	cat.Skills = append(cat.Skills, skillName)
	cat.SkillCount++

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

// WriteSkillFile 向技能目录写入参考文件（参考 Hermes Agent 的 write_file 操作）
// 支持 references/、scripts/、templates/ 子目录
func (m *Manager) WriteSkillFile(name, filePath, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	if skill.Dir == "" {
		return fmt.Errorf("skill %s has no directory", name)
	}

	// 安全扫描
	scanResult := ScanSkillSecurity(content)
	if !scanResult.Safe {
		return fmt.Errorf("write blocked by security scan: %s", strings.Join(scanResult.Threats, "; "))
	}

	// 构建完整路径（防止路径遍历攻击）
	cleanPath := filepath.Clean(filePath)
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal detected: %s", filePath)
	}

	fullPath := filepath.Join(skill.Dir, cleanPath)

	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// RemoveSkillFile 从技能目录删除参考文件（参考 Hermes Agent 的 remove_file 操作）
func (m *Manager) RemoveSkillFile(name, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	if skill.Dir == "" {
		return fmt.Errorf("skill %s has no directory", name)
	}

	// 防止路径遍历攻击
	cleanPath := filepath.Clean(filePath)
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal detected: %s", filePath)
	}

	// 保护 SKILL.md 不被删除
	if cleanPath == "SKILL.md" || cleanPath == filepath.Join(skill.Dir, "SKILL.md") {
		return fmt.Errorf("cannot remove SKILL.md, use delete action instead")
	}

	fullPath := filepath.Join(skill.Dir, cleanPath)

	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	return nil
}

// =============================================================================
// Progressive Disclosure (渐进式加载)
// =============================================================================

// LoadSkillAtLevel loads a skill at the specified level
// Level 0: List only - returns name, description, category
// Level 1: Full content - returns complete skill content
// Level 2: With references - returns specific reference file
func (m *Manager) LoadSkillAtLevel(name string, options *SkillViewOptions) (interface{}, error) {
	skill, err := m.Get(name)
	if err != nil {
		return nil, err
	}

	switch options.Level {
	case Level0:
		// Return lightweight list item
		return SkillListItem{
			Name:        skill.Name,
			Description: skill.Description,
			Category:    skill.Category,
			Tags:        skill.Tags,
			Version:     skill.Version,
		}, nil

	case Level1:
		// Return full skill content
		content := skill.Content
		if content == "" {
			// Try to load from SKILL.md
			if skill.Dir != "" {
				skillMdPath := filepath.Join(skill.Dir, "SKILL.md")
				if data, err := os.ReadFile(skillMdPath); err == nil {
					content = string(data)
				}
			}
		}
		return map[string]interface{}{
			"name":        skill.Name,
			"description": skill.Description,
			"content":     content,
			"category":    skill.Category,
			"tags":        skill.Tags,
			"version":     skill.Version,
			"author":      skill.Author,
			"source":      skill.Source,
			"tools":       skill.GetTools(),
			"dir":         skill.Dir,
			"supporting":  skill.SupportingFiles(),
		}, nil

	case Level2:
		// Return specific reference file
		if options.Path == "" {
			return nil, fmt.Errorf("path required for Level2 load")
		}
		if skill.Dir == "" {
			return nil, fmt.Errorf("skill has no directory")
		}
		refPath := filepath.Join(skill.Dir, options.Path)
		data, err := os.ReadFile(refPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read reference file: %w", err)
		}
		return map[string]interface{}{
			"name":    skill.Name,
			"path":    options.Path,
			"content": string(data),
		}, nil

	default:
		return nil, fmt.Errorf("unknown load level: %d", options.Level)
	}
}

// ListSkillsAtLevel0 returns lightweight skill list for efficient indexing
func (m *Manager) ListSkillsAtLevel0() ([]SkillListItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]SkillListItem, 0, len(m.skills))
	for _, skill := range m.skills {
		items = append(items, SkillListItem{
			Name:        skill.Name,
			Description: skill.Description,
			Category:    skill.Category,
			Tags:        skill.Tags,
			Version:     skill.Version,
		})
	}
	return items, nil
}

// GetSkillsByCategory returns skills grouped by category
func (m *Manager) GetSkillsByCategory() map[string][]SkillListItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]SkillListItem)
	for _, skill := range m.skills {
		cat := skill.Category
		if cat == "" {
			cat = "uncategorized"
		}
		result[cat] = append(result[cat], SkillListItem{
			Name:        skill.Name,
			Description: skill.Description,
			Category:    cat,
			Tags:        skill.Tags,
			Version:     skill.Version,
		})
	}
	return result
}

// FilterSkillsByCondition returns visible skills based on activation conditions
func (m *Manager) FilterSkillsByCondition(availableToolsets, availableTools []string, platform string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var visible []*Skill
	for _, skill := range m.skills {
		if skill.Metadata == nil {
			// No conditions, always visible
			visible = append(visible, skill)
			continue
		}

		// Check hermes conditions
		if hermes, ok := skill.Metadata["hermes"].(map[string]interface{}); ok {
			cond := &SkillActivationCondition{}

			if v, ok := hermes["fallback_for_toolset"].(string); ok {
				cond.FallbackForToolset = v
			}
			if v, ok := hermes["requires_toolset"].(string); ok {
				cond.RequiresToolset = v
			}
			if v, ok := hermes["platforms"]; ok {
				if platforms, ok := v.([]interface{}); ok {
					for _, p := range platforms {
						if pStr, ok := p.(string); ok {
							cond.Platforms = append(cond.Platforms, pStr)
						}
					}
				}
			}

			if cond.RequiresToolset != "" || len(cond.Platforms) > 0 ||
				cond.FallbackForToolset != "" {
				if cond.IsVisible(availableToolsets, availableTools, platform) {
					visible = append(visible, skill)
				}
			} else {
				visible = append(visible, skill)
			}
		} else {
			visible = append(visible, skill)
		}
	}
	return visible
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

// SearchHub searches all configured hubs for skills
func (m *Manager) SearchHub(keyword string, sources []HubSource) ([]HubSkill, error) {
	var allSkills []HubSkill

	for _, source := range sources {
		skills, err := m.searchHubSource(keyword, source)
		if err != nil {
			continue // Skip errors, try next source
		}
		allSkills = append(allSkills, skills...)
	}

	return allSkills, nil
}

// searchHubSource searches a specific hub source
func (m *Manager) searchHubSource(keyword string, source HubSource) ([]HubSkill, error) {
	switch source {
	case HubSourceOfficial:
		return m.searchOfficialSkills(keyword)
	case HubSourceSkillsSh:
		return m.searchSkillsSh(keyword)
	case HubSourceHub:
		return m.searchHubSkills(keyword)
	default:
		return nil, fmt.Errorf("unsupported hub source: %s", source)
	}
}

// searchOfficialSkills searches Hermes official skills from GitHub
func (m *Manager) searchOfficialSkills(keyword string) ([]HubSkill, error) {
	// 从 Hermes Agent 官方仓库获取可选技能列表
	// https://github.com/NousResearch/hermes-agent/tree/main/optional-skills
	apiURL := "https://api.github.com/repos/NousResearch/hermes-agent/contents/optional-skills"

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		// 如果 API 调用失败，返回硬编码的常用技能
		return m.getFallbackOfficialSkills(keyword), nil
	}

	req.Header.Set("User-Agent", "go-magic-skill-manager")

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return m.getFallbackOfficialSkills(keyword), nil
	}
	defer resp.Body.Close()

	var contents []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Path string `json:"path"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return m.getFallbackOfficialSkills(keyword), nil
	}

	var results []HubSkill
	keyword = strings.ToLower(keyword)

	for _, item := range contents {
		if item.Type != "dir" {
			continue
		}

		skill := HubSkill{
			Name:        item.Name,
			Description: fmt.Sprintf("Hermes official skill: %s", item.Name),
			Category:    "official",
			Source:      HubSourceOfficial,
			SourceID:    item.Name,
			URL:         fmt.Sprintf("https://github.com/NousResearch/hermes-agent/tree/main/optional-skills/%s", item.Name),
		}

		// 关键词过滤
		if keyword != "" {
			if !strings.Contains(strings.ToLower(skill.Name), keyword) &&
				!strings.Contains(strings.ToLower(skill.Description), keyword) {
				continue
			}
		}

		results = append(results, skill)
	}

	return results, nil
}

// getFallbackOfficialSkills 返回硬编码的官方技能列表（API 失败时使用）
func (m *Manager) getFallbackOfficialSkills(keyword string) []HubSkill {
	officialSkills := []HubSkill{
		{Name: "security/1password", Description: "1Password integration for secure credential management", Category: "security", Source: HubSourceOfficial, SourceID: "security/1password"},
		{Name: "security/bitwarden", Description: "Bitwarden password manager integration", Category: "security", Source: HubSourceOfficial, SourceID: "security/bitwarden"},
		{Name: "migration/openclaw", Description: "Migration guide from OpenClaw", Category: "migration", Source: HubSourceOfficial, SourceID: "migration/openclaw"},
		{Name: "devtools/kubernetes", Description: "Kubernetes deployment and management", Category: "devtools", Source: HubSourceOfficial, SourceID: "devtools/kubernetes"},
		{Name: "devtools/docker", Description: "Docker container management", Category: "devtools", Source: HubSourceOfficial, SourceID: "devtools/docker"},
	}

	if keyword == "" {
		return officialSkills
	}

	var results []HubSkill
	keyword = strings.ToLower(keyword)
	for _, skill := range officialSkills {
		if strings.Contains(strings.ToLower(skill.Name), keyword) ||
			strings.Contains(strings.ToLower(skill.Description), keyword) {
			results = append(results, skill)
		}
	}
	return results
}

// searchSkillsSh searches skills.sh registry
func (m *Manager) searchSkillsSh(keyword string) ([]HubSkill, error) {
	// For now, return mock data - real implementation would call skills.sh API
	mockSkills := []HubSkill{
		{Name: "vercel-labs/agent-skills/vercel-react-best-practices", Description: "React best practices for Vercel", Category: "frontend", Source: HubSourceSkillsSh},
		{Name: "anthropics/skills/pdf", Description: "PDF processing and analysis", Category: "document", Source: HubSourceSkillsSh},
	}

	var results []HubSkill
	keyword = strings.ToLower(keyword)
	for _, skill := range mockSkills {
		if strings.Contains(strings.ToLower(skill.Name), keyword) ||
			strings.Contains(strings.ToLower(skill.Description), keyword) {
			results = append(results, skill)
		}
	}
	return results, nil
}

// searchHubSkills searches ClawHub marketplace
func (m *Manager) searchHubSkills(keyword string) ([]HubSkill, error) {
	// For now, return mock data - real implementation would call clawhub.ai API
	mockSkills := []HubSkill{
		{Name: "k8s-deploy", Description: "Kubernetes deployment workflow", Category: "devtools", Source: HubSourceHub},
		{Name: "git-workflow", Description: "Git workflow automation", Category: "devtools/git", Source: HubSourceHub},
	}

	var results []HubSkill
	keyword = strings.ToLower(keyword)
	for _, skill := range mockSkills {
		if strings.Contains(strings.ToLower(skill.Name), keyword) ||
			strings.Contains(strings.ToLower(skill.Description), keyword) {
			results = append(results, skill)
		}
	}
	return results, nil
}

// InstallFromHub installs a skill from a hub source
// 支持从 GitHub 仓库目录、skills.sh、well-known 端点等来源安装
func (m *Manager) InstallFromHub(source HubSource, sourceID string) error {
	var installURL string

	switch source {
	case HubSourceOfficial:
		// Hermes 官方技能：从 GitHub 仓库目录安装
		installURL = fmt.Sprintf("https://github.com/NousResearch/hermes-agent/tree/main/optional-skills/%s", sourceID)
	case HubSourceSkillsSh:
		// skills.sh 注册表
		installURL = fmt.Sprintf("https://%s", sourceID)
	case HubSourceWellKnown:
		// well-known 端点
		installURL = sourceID
	case HubSourceGitHub:
		// 直接 GitHub 仓库路径
		installURL = fmt.Sprintf("https://github.com/%s", sourceID)
	case HubSourceHub:
		installURL = fmt.Sprintf("https://clawhub.ai/skills/%s", sourceID)
	default:
		return fmt.Errorf("unsupported hub source: %s", source)
	}

	// 使用 importer 下载器从远程安装
	return m.installFromRemoteURL(installURL, source)
}

// installFromRemoteURL 从远程 URL 下载并安装技能
// 复用 InstallFromURL 的下载逻辑，避免循环导入 importer 包
func (m *Manager) installFromRemoteURL(rawURL string, source HubSource) error {
	fmt.Printf("Downloading skill from: %s\n", rawURL)

	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "go-magic-skill-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 下载文件
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "go-magic-skill-manager")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// 读取响应内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 判断内容类型并处理
	contentType := resp.Header.Get("Content-Type")
	isZip := strings.Contains(contentType, "zip") || strings.HasSuffix(rawURL, ".zip")
	isTarGz := strings.Contains(contentType, "gzip") || strings.HasSuffix(rawURL, ".tar.gz")

	var skillDir string

	if isZip || isTarGz {
		// 解压到临时目录
		if isZip {
			if err := m.extractZip(body, tmpDir); err != nil {
				return fmt.Errorf("failed to extract zip: %w", err)
			}
		}
		skillDir = tmpDir
	} else if strings.Contains(contentType, "json") || strings.HasSuffix(rawURL, ".json") {
		// JSON 格式（manifest.json）
		manifestPath := filepath.Join(tmpDir, "manifest.json")
		if err := os.WriteFile(manifestPath, body, 0644); err != nil {
			return fmt.Errorf("failed to write manifest: %w", err)
		}
		skillDir = tmpDir
	} else {
		// 可能是 Markdown 或文本（SKILL.md）
		skillMdPath := filepath.Join(tmpDir, "SKILL.md")
		if err := os.WriteFile(skillMdPath, body, 0644); err != nil {
			return fmt.Errorf("failed to write SKILL.md: %w", err)
		}
		skillDir = tmpDir
	}

	fmt.Printf("Downloaded to: %s\n", skillDir)

	// 尝试加载下载的技能
	skill := m.loadSkillFromFile(skillDir)
	if skill == nil {
		// 可能是目录，尝试找到 SKILL.md
		skillMdPath := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMdPath); err == nil {
			skill = m.loadSkillFromFile(skillMdPath)
		}
	}

	if skill == nil {
		return fmt.Errorf("no valid skill found in downloaded content")
	}

	// 安全扫描
	scanResult := ScanSkillSecurity(skill.Content)
	if !scanResult.Safe {
		return fmt.Errorf("security scan failed: %s", strings.Join(scanResult.Threats, "; "))
	}

	// 设置来源
	skill.Source = SkillSourceRegistry

	// 安装到全局技能目录
	if err := m.Add(skill); err != nil {
		return err
	}

	// 记录到 Hub lock.json
	lockEntry := HubLockEntry{
		SkillName:     skill.Name,
		Source:        source,
		SourceID:      rawURL,
		URL:           rawURL,
		InstalledAt:   time.Now(),
		SecurityAudit: "passed",
	}
	if err := m.AddHubLockEntry(lockEntry); err != nil {
		fmt.Printf("Warning: failed to save hub lock entry: %v\n", err)
	}

	// 添加审计日志
	m.appendAuditLog("install", skill.Name, source, "success")

	return nil
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

// CheckForUpdates checks installed hub skills for updates
func (m *Manager) CheckForUpdates() (map[string]bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	updates := make(map[string]bool)
	for name, skill := range m.skills {
		if skill.Source == SkillSourceRegistry {
			// In real implementation, compare with remote version
			updates[name] = false // No update available by default
		}
	}
	return updates, nil
}

// UpdateHubSkill updates a skill from its hub source
func (m *Manager) UpdateHubSkill(name string) error {
	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	if skill.Source != SkillSourceRegistry {
		return fmt.Errorf("skill %s is not a hub skill", name)
	}

	// Re-install from hub
	return m.InstallFromHub(HubSource(skill.Source), name)
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

// DisableSkillForPlatform 按平台禁用技能
func (m *Manager) DisableSkillForPlatform(skillName, platform string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.disabledSkills.Platform == nil {
		m.disabledSkills.Platform = make(map[string][]string)
	}

	platformDisabled, ok := m.disabledSkills.Platform[platform]
	if !ok {
		platformDisabled = []string{}
	}

	for _, name := range platformDisabled {
		if name == skillName {
			return nil // 已禁用
		}
	}

	m.disabledSkills.Platform[platform] = append(platformDisabled, skillName)
	return m.saveDisabledSkills()
}

// EnableSkillForPlatform 按平台启用技能
func (m *Manager) EnableSkillForPlatform(skillName, platform string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	platformDisabled, ok := m.disabledSkills.Platform[platform]
	if !ok {
		return nil
	}

	for i, name := range platformDisabled {
		if name == skillName {
			m.disabledSkills.Platform[platform] = append(platformDisabled[:i], platformDisabled[i+1:]...)
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

	// 添加审计日志
	m.appendAuditLog("uninstall", skillName, entry.Source, "success")

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
// External Skill Directories
// =============================================================================

// ExternalDir represents an external skill directory configuration
type ExternalDir struct {
	Path     string `yaml:"path"`
	ReadOnly bool   `yaml:"read_only"`
}

// SetExternalDirs sets additional external skill directories to scan
func (m *Manager) SetExternalDirs(dirs []ExternalDir) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Scan external directories but don't allow writes to them
	for _, dir := range dirs {
		if dir.Path == "" {
			continue
		}

		// Expand ~ to home directory
		expandPath := os.Expand(dir.Path, func(key string) string {
			if key == "~" {
				home, _ := os.UserHomeDir()
				return home
			}
			return os.Getenv(key)
		})

		// Add to search dirs if not already present
		found := false
		for _, existing := range m.searchDirs {
			if existing == expandPath {
				found = true
				break
			}
		}
		if !found {
			m.searchDirs = append(m.searchDirs, expandPath)
		}
	}
}

// GetExternalDirs returns configured external directories
func (m *Manager) GetExternalDirs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.searchDirs
}

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