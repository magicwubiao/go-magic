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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/skills/parser"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/safepath"
)

// Skill is now defined in types.go - this file re-exports it for backwards compatibility
// All new code should use types.Skill

// Manager manages skill loading and registration
type Manager struct {
	mu                sync.RWMutex
	searchDirs        []string
	builtinDir        string
	skills            map[string]*Skill
	registryURL       string                // ClawHub or GitHub registry URL
	hubLock           *HubLock              // Hub 安装跟踪 (.hub/lock.json)
	bundledManifest   *BundledManifest      // 内置技能跟踪 (.bundled_manifest)
	disabledSkills    *DisabledSkillsConfig // 禁用技能配置
	skillsDir         string                // 技能目录路径 (~/.magic/skills)
	hubDir            string                // Hub 目录路径 (~/.magic/skills/.hub)
	autoSkillsDir     string                // 自动技能根目录 (.../auto_skills)
	autoSkillCreation bool                  // 是否自动创建技能
	minPatternFreq    int                   // 最小模式频率阈值
	// Registry manager for hub search/install
	registryMgr *RegistryManager
	// 后台 goroutine 控制：stopChan 关闭后所有后台 goroutine 退出，避免泄漏
	stopChan chan struct{}
	stopOnce sync.Once
	effMgr   *EffectivenessManager // 持有引用便于 Close 时停止
}

// ManagerConfig 配置管理器
type ManagerConfig struct {
	SearchDirs        []string // 搜索目录列表
	BuiltinDir        string   // 内置技能目录
	RegistryURL       string   // 技能注册表 URL
	ToolNames         []string // 可用工具名称列表（用于技能验证）
	AutoSkillCreation bool     // 是否自动创建技能
	MinPatternFreq    int      // 最小模式频率阈值
	AutoSkillsDir     string   // 自动技能目录路径（四态管理根目录）
}

// NewManager creates a new skill manager with default configuration
func NewManager() (*Manager, error) {
	magicHome := config.GetMagicHome()

	// Get the path to built-in skills directory
	builtinDir := filepath.Join("internal", "skills", "builtin")

	cfg := &ManagerConfig{
		SearchDirs: []string{
			filepath.Join(magicHome, "skills"),
			"skills",
		},
		BuiltinDir: builtinDir,
	}

	return NewManagerWithConfig(cfg)
}

// NewManagerWithConfig creates a manager with custom configuration
func NewManagerWithConfig(cfg *ManagerConfig) (*Manager, error) {
	if cfg == nil {
		cfg = &ManagerConfig{}
	}

	// Set defaults
	if len(cfg.SearchDirs) == 0 {
		mh := config.GetMagicHome()
		cfg.SearchDirs = []string{
			filepath.Join(mh, "skills"),
			"skills",
		}
	}

	skillsDir := cfg.SearchDirs[0]

	// 自动技能目录：显式指定时用指定路径，否则在第一个 searchDir 下建 auto_skills
	autoDir := cfg.AutoSkillsDir
	if autoDir == "" {
		autoDir = filepath.Join(skillsDir, "auto_skills")
	}

	m := &Manager{
		searchDirs:        cfg.SearchDirs,
		builtinDir:        cfg.BuiltinDir,
		registryURL:       cfg.RegistryURL,
		skills:            make(map[string]*Skill),
		skillsDir:         skillsDir,
		hubDir:            filepath.Join(skillsDir, ".hub"),
		autoSkillsDir:     autoDir,
		disabledSkills:    &DisabledSkillsConfig{Platform: make(map[string][]string)},
		autoSkillCreation: cfg.AutoSkillCreation,
		minPatternFreq:    cfg.MinPatternFreq,
		registryMgr:       NewRegistryManager(),
	}

	// 创建 Hub 目录（错误转为警告日志，不中断初始化）
	if err := os.MkdirAll(m.hubDir, 0755); err != nil {
		log.Warnf("failed to create hub dir %s: %v", m.hubDir, err)
	}
	// 创建四态目录
	for _, sub := range []string{"pending", "approved", "archived"} {
		if err := os.MkdirAll(filepath.Join(m.autoSkillsDir, sub), 0755); err != nil {
			log.Warnf("failed to create auto skill subdir %s: %v", sub, err)
		}
	}

	// 加载 Hub lock.json
	m.loadHubLock()

	// 加载 Bundled manifest
	m.loadBundledManifest()

	// 加载禁用技能配置
	m.loadDisabledSkills()

	// Load built-in skills (always try embedded FS, fallback to filesystem)
	if err := m.loadBuiltinSkills(); err != nil {
		// Don't fail on error, just log
		log.Warnf("failed to load built-in skills: %v", err)
	}

	if err := m.loadSkills(); err != nil {
		return nil, err
	}

	// Initialize effectiveness manager with auto-save and cleanup
	if len(m.searchDirs) > 0 {
		if effMgr, err := NewEffectivenessManager(m.searchDirs[0]); err == nil {
			effMgr.StartAutoSave()
			m.effMgr = effMgr
			m.stopChan = make(chan struct{})
			// Start periodic cleanup (every 24 hours, remove records older than 30 days)
			go m.startEffectivenessCleanup(effMgr)
		}
	}

	return m, nil
}

// startEffectivenessCleanup starts a background goroutine to periodically clean old effectiveness records.
// 监听 m.stopChan 退出，避免 goroutine 永久阻塞导致 Manager 无法 GC。
func (m *Manager) startEffectivenessCleanup(effMgr *EffectivenessManager) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := effMgr.ClearOldRecords(30 * 24 * time.Hour); err != nil {
				log.Warnf("failed to clean old effectiveness records: %v", err)
			} else {
				log.Infof("cleaned old effectiveness records (older than 30 days)")
			}
		case <-m.stopChan:
			return
		}
	}
}

// Close 停止 Manager 的所有后台 goroutine 并刷新未保存数据。
// 调用后 Manager 不应再被使用。
func (m *Manager) Close() {
	m.stopOnce.Do(func() {
		if m.stopChan != nil {
			close(m.stopChan)
		}
		if m.effMgr != nil {
			m.effMgr.StopAutoSave()
		}
	})
}

// loadBuiltinSkills 加载内置技能（写入 m.skills，调用方需保证不在持锁上下文，
// 或调用 loadBuiltinSkillsInto 写入指定 map 用于 Reload 原子替换）。
func (m *Manager) loadBuiltinSkills() error {
	return m.loadBuiltinSkillsInto(m.skills)
}

// loadBuiltinSkillsInto 加载内置技能到指定 map，供 Reload 在临时 map 上构建后原子替换。
func (m *Manager) loadBuiltinSkillsInto(target map[string]*Skill) error {
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
					target[skill.Name] = skill
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
				target[skill.Name] = skill
			}
		}
	}

	return nil
}

func (m *Manager) loadSkills() error {
	return m.loadSkillsInto(m.skills)
}

// loadSkillsInto 加载 searchDirs 下的技能到指定 map，供 Reload 原子替换。
// 通过 existing 参数保留已加载的 builtin/registry source 信息。
func (m *Manager) loadSkillsInto(target map[string]*Skill) error {
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
						if existingSkill, exists := target[skill.Name]; exists {
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
						target[skill.Name] = skill
					}
					continue
				}
				// Check for manifest.json (legacy format)
				manifestPath := filepath.Join(path, "manifest.json")
				if _, err := os.Stat(manifestPath); err == nil {
					skill := m.loadSkillFromManifest(manifestPath)
					if skill != nil {
						skill.Source = "local"
						target[skill.Name] = skill
					}
					continue
				}
				continue
			}

			skill := m.loadSkillFromFile(path)
			if skill != nil {
				// Preserve existing source if it's builtin or registry (from hub)
				if existingSkill, exists := target[skill.Name]; exists {
					if existingSkill.Source == "builtin" {
						skill.Source = existingSkill.Source
					} else if skill.Source != SkillSourceRegistry {
						skill.Source = "local"
					}
				} else if skill.Source != SkillSourceRegistry {
					skill.Source = "local"
				}
				target[skill.Name] = skill
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

	// 使用统一的 parser.ParseYAMLFrontmatter 解析，与 loadMarkdownSkill 保持一致
	frontmatter, _, err := parser.ParseYAMLFrontmatter(content)
	if err == nil && frontmatter != nil {
		if v, ok := frontmatter["name"].(string); ok && v != "" {
			name = v
		}
		if v, ok := frontmatter["description"].(string); ok && v != "" {
			description = v
		}
		if v, ok := frontmatter["tags"].([]interface{}); ok {
			for _, t := range v {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		}
		if v, ok := frontmatter["tools"].([]interface{}); ok {
			for _, t := range v {
				if s, ok := t.(string); ok {
					tools = append(tools, s)
				}
			}
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
	if skill == nil {
		return fmt.Errorf("skill is nil")
	}
	if skill.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	// 校验 skill.Name 防止路径穿越：外部输入（JSON 反序列化）可能含 ../
	if err := safepath.SanitizeName(skill.Name); err != nil {
		return fmt.Errorf("invalid skill name: %w", err)
	}

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

	// 安全拼接文件名（skill.Name 已校验，SafeJoin 二次防御）
	path, err := safepath.SafeJoin(m.searchDirs[0], skill.Name+".json")
	if err != nil {
		return fmt.Errorf("invalid skill path: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	m.skills[skill.Name] = skill
	return nil
}

// Remove removes a skill (alias for Delete, kept for backwards compatibility)
func (m *Manager) Remove(name string) error {
	return m.Delete(name)
}

// GetSkillsContext returns all skills formatted for system prompt
// Note: pending/rejected auto-skills are NOT included here - they need manual approval first.
func (m *Manager) GetSkillsContext() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

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
// 在临时 map 上构建完整技能集合，最后持写锁原子替换，避免加载过程中
// 释放锁导致其他 goroutine 读到空 map 或部分加载状态。
func (m *Manager) Reload() error {
	// 在临时 map 上构建，不持锁，允许 IO 并发
	tmp := make(map[string]*Skill)

	if m.builtinDir != "" || hasEmbeddedBuiltin() {
		if err := m.loadBuiltinSkillsInto(tmp); err != nil {
			log.Warnf("failed to load built-in skills: %v", err)
		}
	}

	if err := m.loadSkillsInto(tmp); err != nil {
		return err
	}

	// 原子替换：持写锁替换 m.skills
	m.mu.Lock()
	m.skills = tmp
	m.mu.Unlock()
	return nil
}

// hasEmbeddedBuiltin 报告是否存在 embed 内置技能 FS（用于 Reload 决定是否加载内置技能）。
func hasEmbeddedBuiltin() bool {
	entries, err := BuiltinSkillsFS.ReadDir(BuiltinDirName)
	return err == nil && len(entries) > 0
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 安全扫描下载内容
	scanResult := ScanSkillSecurity(string(data))
	if !scanResult.Safe {
		return fmt.Errorf("skill from URL blocked by security scan: %s",
			strings.Join(scanResult.Threats, "; "))
	}

	// Try to parse as JSON first
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
// 返回的 content 已剥离 YAML frontmatter，仅保留正文，避免向 LLM 暴露原始元数据。
func (m *Manager) GetSkillInfo(name string) (description string, tools []string, content string, err error) {
	skill, err := m.Get(name)
	if err != nil {
		return "", nil, "", err
	}
	// 剥离 frontmatter，只返回正文 body
	_, body, parseErr := parser.ParseYAMLFrontmatter(skill.Content)
	if parseErr != nil || body == "" {
		// 解析失败或无 frontmatter，原样返回
		body = skill.Content
	}
	return skill.Description, skill.GetTools(), body, nil
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
// 使用正则匹配并规范化空白，避免双空格、引号等简单绕过。
func ScanSkillSecurity(content string) *SecurityScanResult {
	result := &SecurityScanResult{
		ScannedAt: time.Now().Format(time.RFC3339),
		Safe:      true,
	}

	// 检测高危提示注入模式（正则，忽略大小写与多余空白）
	// 关键短语 "ignore ... instructions" 是经典 prompt injection 开头
	injectionRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)ignore\s+(?:all\s+)?(?:previous|above)\s+instructions?\s+and`),
		regexp.MustCompile(`(?i)disregard\s+(?:all\s+)?(?:previous|above)\s+instructions?`),
		regexp.MustCompile(`(?i)you\s+are\s+(?:now|actually)\s+(?:a\s+)?(?:different|new)\s+(?:assistant|ai|mode)`),
	}
	for _, re := range injectionRegexes {
		if loc := re.FindStringIndex(content); loc != nil {
			matched := strings.TrimSpace(content[loc[0]:loc[1]])
			result.Safe = false
			result.Threats = append(result.Threats, "potential prompt injection: "+matched)
			result.Severity = "high"
		}
	}

	// 检测破坏性命令：先规范化空白（连续空白压成单空格）再匹配，
	// 这样 "rm  -rf  /"（双空格）也能被检出。
	normalized := destructiveCmdNormalizer.ReplaceAllString(content, " ")
	lower := strings.ToLower(normalized)

	destructivePatterns := []string{
		"rm -rf /",
		"rm -rf /*",
		"rm -fr /",
		"mkfs.",
		"dd if=",
		":(){ :|:& };:", // fork bomb
		"chmod -r 777 /",
		"chown -r root /",
		"> /dev/sda",
		"shutdown -h",
		"reboot",
	}
	for _, pattern := range destructivePatterns {
		if strings.Contains(lower, pattern) {
			result.Safe = false
			result.Threats = append(result.Threats, "destructive command: "+pattern)
			result.Severity = "high"
		}
	}

	return result
}

// destructiveCmdNormalizer 将连续空白（含 tab/换行）压缩为单个空格，
// 用于破坏性命令检测前置规范化，避免 "rm  -rf  /" 等绕过。
var destructiveCmdNormalizer = regexp.MustCompile(`\s+`)

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
	sb.WriteString("Available skills (use skill tool with action=info for details):\n")
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
	// 校验 name 防止路径穿越（外部用户输入）
	if err := safepath.SanitizeName(name); err != nil {
		return nil, fmt.Errorf("invalid skill name: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if skill already exists
	if _, ok := m.skills[name]; ok {
		return nil, fmt.Errorf("skill %s already exists", name)
	}

	if len(m.searchDirs) == 0 {
		return nil, fmt.Errorf("no search directories configured")
	}

	// 在用户目录（HOME 下的 magic 目录）或第一个搜索目录下创建技能目录。
	// 不再硬编码 /workspace/projects（不可移植）；优先用 config.GetMagicHome()。
	skillDir := ""
	magicHome := config.GetMagicHome()
	for _, dir := range m.searchDirs {
		// 优先选择位于 magic home 下的目录（用户可写）
		if strings.HasPrefix(dir, magicHome) {
			safeDir, err := safepath.SafeJoin(dir, name)
			if err != nil {
				continue
			}
			if err := os.MkdirAll(safeDir, 0755); err != nil {
				log.Warnf("failed to create skill dir in %s: %v", dir, err)
				continue
			}
			skillDir = safeDir
			break
		}
	}

	if skillDir == "" {
		// 回退到第一个搜索目录
		safeDir, err := safepath.SafeJoin(m.searchDirs[0], name)
		if err != nil {
			return nil, fmt.Errorf("invalid skill dir: %w", err)
		}
		if err := os.MkdirAll(safeDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create skill directory: %w", err)
		}
		skillDir = safeDir
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

	oldName := skill.Name
	skill.Name = meta.Name
	skill.Description = meta.Description
	skill.Tags = meta.Tags

	// 同步写入 SKILL.md（与 Create/Update 保持一致，避免元数据与内容脱节）
	if skill.Dir == "" {
		return nil
	}

	skillMdPath := filepath.Join(skill.Dir, "SKILL.md")
	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	content := string(data)
	// 更新 frontmatter 中的 name / description / tags 字段
	content = updateFrontmatterField(content, "name", meta.Name)
	content = updateFrontmatterField(content, "description", meta.Description)
	if len(meta.Tags) > 0 {
		content = updateFrontmatterField(content, "tags", "["+strings.Join(meta.Tags, ", ")+"]")
	}

	if err := os.WriteFile(skillMdPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	// 如果技能改名了，更新内存中的 key
	if oldName != meta.Name {
		delete(m.skills, oldName)
		m.skills[meta.Name] = skill
	}

	return nil
}

// updateFrontmatterField 替换 YAML frontmatter 中指定字段的值
// 仅在 frontmatter 区域（--- 之间）进行替换
func updateFrontmatterField(content, field, value string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	endIdx := strings.Index(content[3:], "---")
	if endIdx == -1 {
		return content
	}
	frontmatter := content[:endIdx+3]
	rest := content[endIdx+3:]

	lines := strings.Split(frontmatter, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, field+":") {
			lines[i] = field + ": " + value
			found = true
			break
		}
	}
	if !found {
		// 在 frontmatter 末尾（第二个 --- 之前）插入新字段
		lines = append(lines[:len(lines)-1], field+": "+value, lines[len(lines)-1])
	}

	return strings.Join(lines, "\n") + rest
}

// Delete removes a skill
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %s not found", name)
	}

	// Remove directory（安全校验：skill.Dir 必须在某个搜索目录或 autoSkillsDir 内，
	// 防止被篡改的 Dir 指向任意目录导致误删用户数据）
	if skill.Dir != "" {
		if !m.isSkillDirManaged(skill.Dir) {
			return fmt.Errorf("refuse to delete unmanaged skill dir: %s (not under any search dir)", skill.Dir)
		}
		if err := os.RemoveAll(skill.Dir); err != nil {
			return fmt.Errorf("failed to remove skill directory: %w", err)
		}
	}

	delete(m.skills, name)
	return nil
}

// isSkillDirManaged 检查 dir 是否位于某个受管理的目录（搜索目录或 autoSkillsDir）内。
// 防止 Delete 误删受管理目录之外的文件。
func (m *Manager) isSkillDirManaged(dir string) bool {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	for _, base := range m.searchDirs {
		if safepath.IsWithin(absDir, base) {
			return true
		}
	}
	if m.autoSkillsDir != "" && safepath.IsWithin(absDir, m.autoSkillsDir) {
		return true
	}
	if m.hubDir != "" && safepath.IsWithin(absDir, m.hubDir) {
		return true
	}
	return false
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
		log.Warnf("failed to save origin metadata: %v", err)
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

// extractZip 解压 ZIP 数据到指定目录（安全版本：使用 safepath 校验每个条目，
// 防止路径穿越、绝对路径、Windows 盘符攻击）。
func (m *Manager) extractZip(data []byte, destDir string) error {
	reader := bytes.NewReader(data)
	zipReader, err := zip.NewReader(reader, int64(len(data)))
	if err != nil {
		return err
	}
	return extractZipReaderToFile(zipReader, destDir)
}

// extractZipFile 解压 ZIP 文件到指定目录（安全版本）。
func extractZipFile(zipPath, destDir string) error {
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zipReader.Close()
	return extractZipReaderToFile(&zipReader.Reader, destDir)
}

// extractZipReaderToFile 将 zip.Reader 内容安全解压到 destDir。
// 对每个条目用 safepath.SafeJoin 校验，拒绝穿越 destDir 的路径。
// 使用 defer 确保文件句柄在错误路径下也能关闭。
func extractZipReaderToFile(zipReader *zip.Reader, destDir string) error {
	// 预先计算 destDir 绝对路径，用于后续校验
	absDest, err := filepath.Abs(filepath.Clean(destDir))
	if err != nil {
		return fmt.Errorf("invalid dest dir: %w", err)
	}

	for _, file := range zipReader.File {
		// 安全拼接：拒绝绝对路径、盘符、含 ".." 的穿越
		targetPath, err := safepath.SafeJoin(absDest, file.Name)
		if err != nil {
			return fmt.Errorf("unsafe zip entry %q: %w", file.Name, err)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.Mode()); err != nil {
				return fmt.Errorf("failed to create dir %s: %w", targetPath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent dir: %w", err)
		}

		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", targetPath, err)
		}

		rc, err := file.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open zip entry %s: %w", file.Name, err)
		}

		_, copyErr := io.Copy(outFile, rc)
		// 无论 copy 是否成功都关闭两个句柄
		rc.Close()
		outFile.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, copyErr)
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
		log.Warnf("failed to parse hub lock: %v", err)
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
	return m.removeHubLockEntryLocked(skillName)
}

// removeHubLockEntryLocked 移除 Hub 安装记录（调用者需持有 m.mu 写锁）。
// 供已持锁的内部方法复用，避免 sync.RWMutex 递归加锁导致死锁。
func (m *Manager) removeHubLockEntryLocked(skillName string) error {
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
	return m.getHubLockEntryLocked(skillName)
}

// getHubLockEntryLocked 获取指定技能的 Hub 安装记录（调用者需持有 m.mu 读/写锁）。
func (m *Manager) getHubLockEntryLocked(skillName string) *HubLockEntry {
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

	// 检查是否从 Hub 安装（使用 locked 版本避免递归加锁导致死锁）
	entry := m.getHubLockEntryLocked(skillName)
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

	// 从 lock.json 移除记录（使用 locked 版本避免递归加锁导致死锁）
	if err := m.removeHubLockEntryLocked(skillName); err != nil {
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
		log.Warnf("failed to open audit log: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(logEntry); err != nil {
		log.Warnf("failed to write audit log: %v", err)
	}
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
		log.Warnf("failed to parse bundled manifest: %v", err)
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
		log.Warnf("failed to parse disabled skills: %v", err)
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

// GetDisabledSkills 返回禁用技能列表（深拷贝，避免调用方修改返回值影响内部状态）
func (m *Manager) GetDisabledSkills() *DisabledSkillsConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 深拷贝 Platform map，避免浅拷贝导致调用方修改影响 Manager 内部状态
	platformCopy := make(map[string][]string, len(m.disabledSkills.Platform))
	for k, v := range m.disabledSkills.Platform {
		platformCopy[k] = append([]string{}, v...)
	}

	return &DisabledSkillsConfig{
		Global:   append([]string{}, m.disabledSkills.Global...),
		Platform: platformCopy,
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
// Auto-Skill Lifecycle Management
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

	// 状态校验：skill.Status 必须等于 from，或为空（兼容历史数据首次迁移）
	// from 为空时表示接受任意非空状态迁移（极少用，保留兼容）
	if skill.Status != from {
		if skill.Status != "" && from != "" {
			return fmt.Errorf("skill %s status is %s, expected %s",
				skillName, skill.Status, from)
		}
		// skill.Status=="" 或 from=="" 的组合允许通过（历史数据迁移）
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
