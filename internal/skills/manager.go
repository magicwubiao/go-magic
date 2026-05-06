package skills

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Skill is now defined in types.go - this file re-exports it for backwards compatibility
// All new code should use types.Skill

// Manager manages skill loading and registration
type Manager struct {
	mu          sync.RWMutex
	searchDirs  []string
	builtinDir  string
	skills      map[string]*Skill
	toolNames   []string // Cached tool names from registry
	registryURL string   // ClawHub or GitHub registry URL
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

	m := &Manager{
		searchDirs:  config.SearchDirs,
		builtinDir:  config.BuiltinDir,
		registryURL: config.RegistryURL,
		toolNames:   config.ToolNames,
		skills:      make(map[string]*Skill),
	}

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

	switch ext {
	case ".json":
		return m.loadJSONSkill(path)
	case ".md", ".markdown":
		return m.loadMarkdownSkill(path)
	default:
		return m.loadTextSkill(path)
	}
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
	tags := []string{}
	tools := []string{}

	// If file is SKILL.md, use parent directory name as skill name
	if name == "SKILL" {
		name = filepath.Base(filepath.Dir(path))
	}

	// Try to extract metadata from YAML frontmatter
	if strings.HasPrefix(content, "---") {
		endMarker := strings.Index(content[3:], "---")
		if endMarker != -1 {
			frontmatter := content[3 : endMarker+3]
			name, description, tags, tools = parseFrontmatter(frontmatter, name)
		}
	}

	skill := &Skill{
		Name:        name,
		Description: description,
		Tags:        tags,
		Tools:       tools,
		Content:     content,
		Metadata:    make(map[string]interface{}),
	}

	return skill
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
		Name:    name,
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
		ctx += fmt.Sprintf("\n--- Skill: %s ---\n%s\n", skill.Name, skill.Content)
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
		ctx += fmt.Sprintf("\n--- Skill: %s ---\n%s\n", skill.Name, skill.Content)
	}
	return ctx
}

// MatchSkillsByInput 根据输入匹配相关技能
func (m *Manager) MatchSkillsByInput(input string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	input = strings.ToLower(input)
	matched := make([]*Skill, 0)

	for _, skill := range m.skills {
		// Check name
		if strings.Contains(strings.ToLower(skill.Name), input) {
			matched = append(matched, skill)
			continue
		}

		// Check description
		if strings.Contains(strings.ToLower(skill.Description), input) {
			matched = append(matched, skill)
			continue
		}

		// Check tags
		for _, tag := range skill.GetTags() {
			if strings.Contains(strings.ToLower(tag), input) {
				matched = append(matched, skill)
				break
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
		Name:        "imported-skill",
		Description: "Skill imported from URL",
		Content:     string(data),
		Tags:        []string{"imported"},
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
