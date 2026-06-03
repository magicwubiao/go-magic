package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SkillSource indicates where the skill comes from
type SkillSource string

const (
	SkillSourceLocal    SkillSource = "local"
	SkillSourceGlobal   SkillSource = "global"
	SkillSourceBuiltin  SkillSource = "builtin"
	SkillSourceRegistry SkillSource = "registry"
	SkillSourceAuto     SkillSource = "auto" // Agent 自动创建
)

// SkillMeta contains metadata about a skill
type SkillMeta struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Version     string      `json:"version,omitempty"`
	Author      string      `json:"author,omitempty"`
	License     string      `json:"license,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Category    string      `json:"category,omitempty"`
	Source      SkillSource `json:"source,omitempty"`
	InstalledAt time.Time   `json:"installed_at,omitempty"`
}

// Skill represents a unified skill with metadata and content
// This is the canonical type for all skills in the system
type Skill struct {
	SkillMeta
	Tools    []string               `json:"tools,omitempty"`    // Tools required by this skill
	Content  string                 `json:"content"`            // Main skill content/prompt
	Dir      string                 `json:"dir,omitempty"`      // Absolute path to skill directory
	Scripts  []string               `json:"scripts,omitempty"`  // Relative paths to scripts in scripts/
	Metadata map[string]interface{} `json:"metadata,omitempty"` // Additional metadata
}

// GetTools returns the list of tool names required by this skill
func (s *Skill) GetTools() []string {
	// First check explicit tools list
	if len(s.Tools) > 0 {
		return s.Tools
	}

	// Then check metadata
	if s.Metadata != nil {
		if tools, ok := s.Metadata["tools"].([]string); ok {
			return tools
		}
		// Check hermes format
		if hermes, ok := s.Metadata["hermes"].(map[string]interface{}); ok {
			if tools, ok := hermes["tools"].([]string); ok {
				return tools
			}
		}
	}

	return nil
}

// GetTags returns skill tags
func (s *Skill) GetTags() []string {
	if len(s.Tags) > 0 {
		return s.Tags
	}
	if s.Metadata != nil {
		if tags, ok := s.Metadata["tags"].([]string); ok {
			return tags
		}
		if hermes, ok := s.Metadata["hermes"].(map[string]interface{}); ok {
			if tags, ok := hermes["tags"].([]string); ok {
				return tags
			}
		}
	}
	return nil
}

// ToSkillMeta converts Skill to SkillMeta
func (s *Skill) ToSkillMeta() *SkillMeta {
	return &SkillMeta{
		Name:        s.Name,
		Description: s.Description,
		Version:     s.Version,
		Author:      s.Author,
		License:     s.License,
		Tags:        s.Tags,
		Category:    s.Category,
		Source:      s.Source,
		InstalledAt: s.InstalledAt,
	}
}

// NewSkill creates a new Skill with the given metadata
func NewSkill(name, description string) *Skill {
	return &Skill{
		SkillMeta: SkillMeta{
			Name:        name,
			Description: description,
			InstalledAt: time.Now(),
		},
		Metadata: make(map[string]interface{}),
	}
}

// ResolveContent returns the skill content with template variables replaced.
// Supported variables: ${MAGIC_SKILL_DIR}, ${MAGIC_SESSION_ID}
func (s *Skill) ResolveContent(sessionID string) string {
	content := s.Content
	dir := s.Dir
	if dir == "" {
		dir = "."
	}
	content = strings.ReplaceAll(content, "${MAGIC_SKILL_DIR}", dir)
	content = strings.ReplaceAll(content, "${MAGIC_SESSION_ID}", sessionID)
	return content
}

// SupportingFiles returns a formatted list of supporting files (scripts/, references/) with absolute paths.
func (s *Skill) SupportingFiles() string {
	if s.Dir == "" {
		return ""
	}
	var files []string
	// Scan scripts/ directory
	scriptsDir := filepath.Join(s.Dir, "scripts")
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				rel := filepath.Join("scripts", e.Name())
				abs := filepath.Join(s.Dir, rel)
				files = append(files, fmt.Sprintf("  %s -> %s", rel, abs))
			}
		}
	}
	// Scan references/ directory
	refsDir := filepath.Join(s.Dir, "references")
	if entries, err := os.ReadDir(refsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				rel := filepath.Join("references", e.Name())
				abs := filepath.Join(s.Dir, rel)
				files = append(files, fmt.Sprintf("  %s -> %s", rel, abs))
			}
		}
	}
	if len(files) == 0 {
		return ""
	}
	return "Supporting files:\n" + strings.Join(files, "\n")
}

// ============================================================================
// Progressive Disclosure (渐进式加载)
// ============================================================================

// SkillLoadLevel represents the level of skill content to load
type SkillLoadLevel int

const (
	// Level0: List only - returns name, description, category (~3k tokens equivalent)
	Level0 SkillLoadLevel = iota
	// Level1: Metadata + Summary - returns full content and metadata
	Level1
	// Level2: Full with references - returns specific reference files
	Level2
)

// String returns the string representation of the load level
func (l SkillLoadLevel) String() string {
	switch l {
	case Level0:
		return "list"
	case Level1:
		return "full"
	case Level2:
		return "reference"
	default:
		return "unknown"
	}
}

// SkillListItem is a lightweight item for Level 0 listing
type SkillListItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Version     string   `json:"version,omitempty"`
}

// SkillViewOptions contains options for viewing a skill at different levels
type SkillViewOptions struct {
	Level    SkillLoadLevel `json:"level"`
	Path     string         `json:"path,omitempty"`     // For Level2: specific reference file
	Platform string         `json:"platform,omitempty"` // For platform-specific skills
}

// =============================================================================
// Skill Manifest & Hub
// =============================================================================

// SkillManifest represents a skill's manifest file (SKILL.md frontmatter)
type SkillManifest struct {
	Name               string             `yaml:"name"`
	Description        string             `yaml:"description"`
	Version            string             `yaml:"version"`
	Platforms          []string           `yaml:"platforms,omitempty"`            // macos, linux, windows
	FallbackForToolset string             `yaml:"fallback_for_toolset,omitempty"` // Show when this toolset is unavailable
	FallbackForTools   []string           `yaml:"fallback_for_tools,omitempty"`   // Show when these tools are unavailable
	RequiresToolset    string             `yaml:"requires_toolset,omitempty"`     // Only show when this toolset is available
	RequiresTools      []string           `yaml:"requires_tools,omitempty"`       // Only show when these tools are available
	Tags               []string           `yaml:"tags,omitempty"`
	Category           string             `yaml:"category,omitempty"`
	Config             []SkillConfigEntry `yaml:"config,omitempty"`
	RequiredEnvVars    []EnvVarEntry      `yaml:"required_environment_variables,omitempty"`
}

// SkillConfigEntry represents a skill configuration setting
type SkillConfigEntry struct {
	Key         string `yaml:"key"`
	Description string `yaml:"description"`
	Default     string `yaml:"default,omitempty"`
	Prompt      string `yaml:"prompt,omitempty"`
}

// EnvVarEntry represents a required environment variable
type EnvVarEntry struct {
	Name        string `yaml:"name"`
	Prompt      string `yaml:"prompt,omitempty"`
	Help        string `yaml:"help,omitempty"`
	RequiredFor string `yaml:"required_for,omitempty"`
}

// HubSource represents a skill hub/source
type HubSource string

const (
	HubSourceLocal     HubSource = "local"
	HubSourceOfficial  HubSource = "official"
	HubSourceSkillsSh  HubSource = "skills.sh"
	HubSourceWellKnown HubSource = "well-known"
	HubSourceGitHub    HubSource = "github"
	HubSourceHub       HubSource = "hub"
)

// HubSkill represents a skill from a hub/registry
type HubSkill struct {
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Category      string    `json:"category,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Source        HubSource `json:"source"`
	SourceID      string    `json:"source_id,omitempty"` // e.g., "openai/skills/k8s"
	URL           string    `json:"url,omitempty"`       // Install URL
	Author        string    `json:"author,omitempty"`
	Stars         int       `json:"stars,omitempty"`
	Installs      int       `json:"installs,omitempty"`       // Weekly installs
	SecurityAudit string    `json:"security_audit,omitempty"` // audit status
	Verified      bool      `json:"verified,omitempty"`
}

// =============================================================================
// Conditional Activation
// =============================================================================

// SkillActivationCondition represents conditions for skill visibility
type SkillActivationCondition struct {
	FallbackForToolset string   `yaml:"fallback_for_toolset,omitempty"`
	FallbackForTools   []string `yaml:"fallback_for_tools,omitempty"`
	RequiresToolset    string   `yaml:"requires_toolset,omitempty"`
	RequiresTools      []string `yaml:"requires_tools,omitempty"`
	Platforms          []string `yaml:"platforms,omitempty"`
}

// IsVisible checks if the skill should be visible given available tools/toolsets
func (c *SkillActivationCondition) IsVisible(availableToolsets []string, availableTools []string, platform string) bool {
	// Platform check
	if len(c.Platforms) > 0 {
		platformMatch := false
		for _, p := range c.Platforms {
			if p == platform || (p == "linux" && platform == "linux") ||
				(p == "macos" && platform == "darwin") ||
				(p == "windows" && platform == "windows") {
				platformMatch = true
				break
			}
		}
		if !platformMatch {
			return false
		}
	}

	// Requires toolset check
	if c.RequiresToolset != "" {
		hasToolset := false
		for _, ts := range availableToolsets {
			if ts == c.RequiresToolset {
				hasToolset = true
				break
			}
		}
		if !hasToolset {
			return false
		}
	}

	// Requires tools check
	if len(c.RequiresTools) > 0 {
		hasAllTools := true
		for _, tool := range c.RequiresTools {
			found := false
			for _, t := range availableTools {
				if t == tool {
					found = true
					break
				}
			}
			if !found {
				hasAllTools = false
				break
			}
		}
		if !hasAllTools {
			return false
		}
	}

	// Fallback for toolset check (show when toolset is UNAVAILABLE)
	if c.FallbackForToolset != "" {
		for _, ts := range availableToolsets {
			if ts == c.FallbackForToolset {
				return false // Toolset is available, don't show
			}
		}
	}

	// Fallback for tools check (show when ALL fallback tools are UNAVAILABLE)
	if len(c.FallbackForTools) > 0 {
		allAvailable := true
		for _, tool := range c.FallbackForTools {
			found := false
			for _, t := range availableTools {
				if t == tool {
					found = true
					break
				}
			}
			if !found {
				allAvailable = false
				break
			}
		}
		if allAvailable {
			return false // All fallback tools available, don't show
		}
	}

	return true
}

// =============================================================================
// Skill Bundles (技能捆绑包) - 参考 Hermes Agent
// =============================================================================

// SkillBundleConfig represents a skill bundle configuration
type SkillBundleConfig struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Skills      []string `yaml:"skills" json:"skills"`
	Instruction string   `yaml:"instruction" json:"instruction,omitempty"`
}

// SkillBundle represents a loaded skill bundle
type SkillBundle struct {
	Name        string
	Description string
	Skills      []*Skill
	Instruction string
}

// =============================================================================
// Security Scanner (安全扫描) - 参考 Hermes Agent
// =============================================================================

// SecurityScanResult represents the result of a security scan
type SecurityScanResult struct {
	Safe      bool     `json:"safe"`
	Threats   []string `json:"threats,omitempty"`
	Severity  string   `json:"severity,omitempty"` // low, medium, high
	ScannedAt string   `json:"scanned_at"`
}

// =============================================================================
// Skills Guidance (技能使用指导) - 参考 Hermes Agent
// =============================================================================

// SkillsGuidance 是注入系统提示词的技能使用指导
// 参考 Hermes Agent 的 SKILLS_GUIDANCE
const SkillsGuidance = `After completing a complex task (5+ tool calls), fixing a tricky error, or discovering a non-trivial workflow, save the approach as a skill with skill_manage so you can reuse it next time.
When using a skill and finding it outdated, incomplete, or wrong, patch it immediately with skill_manage(action='patch') -- don't wait to be asked. Skills that aren't maintained become liabilities.`

// =============================================================================
// Skill Categories (技能分类) - 参考 Hermes Agent 目录层级分类
// =============================================================================

// SkillCategory 表示一个技能分类（对应目录层级）
type SkillCategory struct {
	Name        string    `json:"name"`        // 分类名称（目录名）
	Description string    `json:"description"` // 分类描述
	Path        string    `json:"path"`        // 分类目录绝对路径
	SkillCount  int       `json:"skill_count"` // 该分类下的技能数量
	Skills      []string  `json:"skills"`      // 技能名称列表
	Parent      string    `json:"parent,omitempty"` // 父分类名称
	Source      SkillSource `json:"source"`    // 来源
}

// CategoryTree 表示分类树结构
type CategoryTree struct {
	Category *SkillCategory   `json:"category"`
	Children []*CategoryTree  `json:"children,omitempty"`
}

// =============================================================================
// Hub Lock (Hub 安装跟踪) - 参考 Hermes Agent .hub/lock.json
// =============================================================================

// HubLockEntry 表示一条 Hub 技能安装记录
type HubLockEntry struct {
	SkillName    string    `json:"skill_name"`
	Source       HubSource `json:"source"`
	SourceID     string    `json:"source_id"`
	URL          string    `json:"url,omitempty"`
	Version      string    `json:"version,omitempty"`
	InstalledAt  time.Time `json:"installed_at"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
	SecurityAudit string   `json:"security_audit,omitempty"` // passed, failed, pending
}

// HubLock 表示完整的 lock.json
type HubLock struct {
	Entries   []HubLockEntry `json:"entries"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// =============================================================================
// Bundled Manifest (内置技能跟踪) - 参考 Hermes Agent .bundled_manifest
// =============================================================================

// BundledManifestEntry 表示一条内置技能种子化记录
type BundledManifestEntry struct {
	SkillName string    `json:"skill_name"`
	Category  string    `json:"category,omitempty"`
	Path      string    `json:"path"`        // 相对路径
	SHA256    string    `json:"sha256"`      // 原始内容哈希，用于检测用户修改
	SeededAt  time.Time `json:"seeded_at"`
}

// BundledManifest 表示完整的 .bundled_manifest
type BundledManifest struct {
	Entries   []BundledManifestEntry `json:"entries"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// =============================================================================
// Disabled Skills (禁用技能) - 参考 Hermes Agent config.yaml skills.disabled
// =============================================================================

// DisabledSkillsConfig 表示禁用技能配置
type DisabledSkillsConfig struct {
	Global    []string            `json:"global,omitempty"`    // 全局禁用
	Platform  map[string][]string `json:"platform,omitempty"`  // 按平台禁用
}

// =============================================================================
// Excluded Directories (排除目录) - 参考 Hermes Agent EXCLUDED_SKILL_DIRS
// =============================================================================

// DefaultExcludedDirs 是默认排除的目录名列表
// 这些目录不会被当作技能或分类扫描
var DefaultExcludedDirs = []string{
	".git", ".github", ".hub", ".archive",
	".venv", "venv", "node_modules", "site-packages",
	"__pycache__", ".tox", ".nox", ".pytest_cache",
	".mypy_cache", ".ruff_cache", ".bundle",
}

// IsExcludedDir 检查目录名是否在排除列表中
func IsExcludedDir(name string) bool {
	for _, excluded := range DefaultExcludedDirs {
		if name == excluded {
			return true
		}
	}
	return false
}
