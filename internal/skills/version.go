package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// SkillVersion 技能版本
type SkillVersion struct {
	Version      string    `json:"version"` // 版本号 (1.0, 1.1, etc.)
	SkillName    string    `json:"skill_name"`
	Content      string    `json:"content"`       // 技能内容
	Description  string    `json:"description"`   // 版本描述
	Changes      []string  `json:"changes"`       // 变更列表
	QualityScore float64   `json:"quality_score"` // 该版本的质量评分
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by"` // 创建者 (user/gepa/manual)
	IsCurrent    bool      `json:"is_current"` // 是否当前版本
}

// VersionManager 版本管理器
type VersionManager struct {
	versions   map[string][]SkillVersion // SkillName -> Versions
	versionDir string                    // 版本存储目录
	mu         sync.RWMutex
}

// NewVersionManager 创建版本管理器
func NewVersionManager(baseDir string) *VersionManager {
	if baseDir == "" {
		baseDir = config.GetMagicHome()
	}

	versionDir := filepath.Join(baseDir, "skills", "versions")
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		// 如果创建失败，使用临时目录
		versionDir = os.TempDir()
	}

	vm := &VersionManager{
		versions:   make(map[string][]SkillVersion),
		versionDir: versionDir,
	}

	// 加载已有版本数据
	vm.loadAllVersions()

	return vm
}

// SaveVersion 保存新版本
func (vm *VersionManager) SaveVersion(skillName, content, description string, createdBy string) (*SkillVersion, error) {
	if skillName == "" {
		return nil, fmt.Errorf("skill name cannot be empty")
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()

	// 获取当前版本号
	currentVersion := vm.getCurrentVersionString(skillName)
	newVersion := vm.generateNextVersion(currentVersion, createdBy)

	// 将之前的版本标记为非当前
	versions := vm.versions[skillName]
	for i := range versions {
		versions[i].IsCurrent = false
	}

	// 提取变更列表
	changes := vm.extractChanges(skillName, content)

	// 计算质量评分（简单实现）
	qualityScore := vm.calculateQualityScore(content)

	version := SkillVersion{
		Version:      newVersion,
		SkillName:    skillName,
		Content:      content,
		Description:  description,
		Changes:      changes,
		QualityScore: qualityScore,
		CreatedAt:    time.Now(),
		CreatedBy:    createdBy,
		IsCurrent:    true,
	}

	// 添加到版本列表
	vm.versions[skillName] = append(versions, version)

	// 按版本号排序
	vm.sortVersions(skillName)

	// 保存到文件
	if err := vm.saveVersionsToFile(skillName); err != nil {
		return nil, fmt.Errorf("failed to save version: %w", err)
	}

	return &version, nil
}

// GetCurrentVersion 获取当前版本
func (vm *VersionManager) GetCurrentVersion(skillName string) *SkillVersion {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	versions, exists := vm.versions[skillName]
	if !exists {
		return nil
	}

	for _, v := range versions {
		if v.IsCurrent {
			return &v
		}
	}

	// 如果没有标记为当前的，返回最新的
	if len(versions) > 0 {
		v := versions[len(versions)-1]
		return &v
	}

	return nil
}

// GetVersion 获取指定版本
func (vm *VersionManager) GetVersion(skillName, version string) *SkillVersion {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	versions, exists := vm.versions[skillName]
	if !exists {
		return nil
	}

	for _, v := range versions {
		if v.Version == version {
			return &v
		}
	}

	return nil
}

// ListVersions 列出所有版本
func (vm *VersionManager) ListVersions(skillName string) []SkillVersion {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	versions, exists := vm.versions[skillName]
	if !exists {
		return nil
	}

	// 返回副本
	result := make([]SkillVersion, len(versions))
	copy(result, versions)

	// 按版本号降序排序（最新的在前）
	sort.Slice(result, func(i, j int) bool {
		return vm.compareVersions(result[i].Version, result[j].Version) > 0
	})

	return result
}

// Rollback 回滚到指定版本
func (vm *VersionManager) Rollback(skillName, targetVersion string) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	versions, exists := vm.versions[skillName]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillName)
	}

	// 查找目标版本
	targetIndex := -1
	for i, v := range versions {
		if v.Version == targetVersion {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		return fmt.Errorf("version not found: %s", targetVersion)
	}

	// 将所有版本标记为非当前
	for i := range versions {
		versions[i].IsCurrent = false
	}

	// 标记目标版本为当前
	versions[targetIndex].IsCurrent = true

	// 更新
	vm.versions[skillName] = versions

	// 保存
	if err := vm.saveVersionsToFile(skillName); err != nil {
		return fmt.Errorf("failed to save rollback: %w", err)
	}

	return nil
}

// CompareVersions 对比两个版本
func (vm *VersionManager) CompareVersions(skillName, v1, v2 string) (string, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	version1 := vm.GetVersion(skillName, v1)
	version2 := vm.GetVersion(skillName, v2)

	if version1 == nil {
		return "", fmt.Errorf("version not found: %s", v1)
	}
	if version2 == nil {
		return "", fmt.Errorf("version not found: %s", v2)
	}

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("=== Comparing %s vs %s ===\n\n", v1, v2))

	// 基本信息对比
	diff.WriteString("[基本信息]\n")
	diff.WriteString(fmt.Sprintf("  %s:\n", v1))
	diff.WriteString(fmt.Sprintf("    创建时间: %s\n", version1.CreatedAt.Format("2006-01-02 15:04:05")))
	diff.WriteString(fmt.Sprintf("    创建者: %s\n", version1.CreatedBy))
	diff.WriteString(fmt.Sprintf("    质量评分: %.1f\n", version1.QualityScore))
	diff.WriteString(fmt.Sprintf("  %s:\n", v2))
	diff.WriteString(fmt.Sprintf("    创建时间: %s\n", version2.CreatedAt.Format("2006-01-02 15:04:05")))
	diff.WriteString(fmt.Sprintf("    创建者: %s\n", version2.CreatedBy))
	diff.WriteString(fmt.Sprintf("    质量评分: %.1f\n", version2.QualityScore))

	// 变更对比
	diff.WriteString("\n[变更对比]\n")
	diff.WriteString(fmt.Sprintf("  %s 变更:\n", v1))
	for _, change := range version1.Changes {
		diff.WriteString(fmt.Sprintf("    - %s\n", change))
	}
	diff.WriteString(fmt.Sprintf("  %s 变更:\n", v2))
	for _, change := range version2.Changes {
		diff.WriteString(fmt.Sprintf("    - %s\n", change))
	}

	// 内容差异（简单行对比）
	diff.WriteString("\n[内容差异]\n")
	contentDiff := vm.generateContentDiff(version1.Content, version2.Content)
	diff.WriteString(contentDiff)

	return diff.String(), nil
}

// GetVersionHistory 获取版本历史
func (vm *VersionManager) GetVersionHistory(skillName string) []SkillVersion {
	return vm.ListVersions(skillName)
}

// DeleteVersion 删除指定版本
func (vm *VersionManager) DeleteVersion(skillName, version string) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	versions, exists := vm.versions[skillName]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillName)
	}

	// 查找并删除
	newVersions := make([]SkillVersion, 0, len(versions))
	found := false
	for _, v := range versions {
		if v.Version == version {
			found = true
			// 不能删除当前版本
			if v.IsCurrent {
				return fmt.Errorf("cannot delete current version")
			}
			continue
		}
		newVersions = append(newVersions, v)
	}

	if !found {
		return fmt.Errorf("version not found: %s", version)
	}

	vm.versions[skillName] = newVersions

	return vm.saveVersionsToFile(skillName)
}

// GetSkillNames 获取所有技能名称
func (vm *VersionManager) GetSkillNames() []string {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	names := make([]string, 0, len(vm.versions))
	for name := range vm.versions {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// ExportVersion 导出版本到文件
func (vm *VersionManager) ExportVersion(skillName, version, exportPath string) error {
	v := vm.GetVersion(skillName, version)
	if v == nil {
		return fmt.Errorf("version not found: %s", version)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal version: %w", err)
	}

	if err := os.WriteFile(exportPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	return nil
}

// ImportVersion 从文件导入版本
func (vm *VersionManager) ImportVersion(importPath string) (*SkillVersion, error) {
	data, err := os.ReadFile(importPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read import file: %w", err)
	}

	var version SkillVersion
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version: %w", err)
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()

	// 检查版本是否已存在
	versions := vm.versions[version.SkillName]
	for _, v := range versions {
		if v.Version == version.Version {
			return nil, fmt.Errorf("version %s already exists for skill %s", version.Version, version.SkillName)
		}
	}

	// 如果不是当前版本，保持原样
	// 如果是当前版本，需要将其他版本标记为非当前
	if version.IsCurrent {
		for i := range versions {
			versions[i].IsCurrent = false
		}
	}

	vm.versions[version.SkillName] = append(versions, version)
	vm.sortVersions(version.SkillName)

	if err := vm.saveVersionsToFile(version.SkillName); err != nil {
		return nil, fmt.Errorf("failed to save imported version: %w", err)
	}

	return &version, nil
}

// getCurrentVersionString 获取当前版本号字符串
func (vm *VersionManager) getCurrentVersionString(skillName string) string {
	versions, exists := vm.versions[skillName]
	if !exists || len(versions) == 0 {
		return ""
	}

	// 查找当前版本
	for _, v := range versions {
		if v.IsCurrent {
			return v.Version
		}
	}

	// 如果没有标记为当前的，返回最新的
	return versions[len(versions)-1].Version
}

// generateNextVersion 生成下一个版本号
// 手动更新：major.minor（如 1.0 -> 1.1）
// GEPA进化：major.minor.patch（如 1.0 -> 1.0.1）
func (vm *VersionManager) generateNextVersion(currentVersion, createdBy string) string {
	if currentVersion == "" {
		return "1.0"
	}

	parts := strings.Split(currentVersion, ".")
	if len(parts) < 2 {
		return "1.0"
	}

	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])

	if createdBy == "gepa" || createdBy == "auto" {
		// GEPA进化：增加 patch 版本号
		if len(parts) >= 3 {
			patch, _ := strconv.Atoi(parts[2])
			return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
		}
		return fmt.Sprintf("%d.%d.1", major, minor)
	}

	// 手动更新：增加 minor 版本号
	return fmt.Sprintf("%d.%d", major, minor+1)
}

// extractChanges 提取变更列表
func (vm *VersionManager) extractChanges(skillName, newContent string) []string {
	current := vm.GetCurrentVersion(skillName)
	if current == nil {
		return []string{"Initial version"}
	}

	var changes []string

	// 简单的变更检测
	oldLines := strings.Split(current.Content, "\n")
	newLines := strings.Split(newContent, "\n")

	oldLineMap := make(map[string]bool)
	for _, line := range oldLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			oldLineMap[trimmed] = true
		}
	}

	newLineMap := make(map[string]bool)
	for _, line := range newLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			newLineMap[trimmed] = true
		}
	}

	// 检测新增内容
	addedCount := 0
	for line := range newLineMap {
		if !oldLineMap[line] {
			addedCount++
		}
	}

	// 检测删除内容
	removedCount := 0
	for line := range oldLineMap {
		if !newLineMap[line] {
			removedCount++
		}
	}

	if addedCount > 0 {
		changes = append(changes, fmt.Sprintf("Added %d lines", addedCount))
	}
	if removedCount > 0 {
		changes = append(changes, fmt.Sprintf("Removed %d lines", removedCount))
	}
	if len(changes) == 0 {
		changes = append(changes, "Minor updates")
	}

	return changes
}

// calculateQualityScore 计算质量评分
func (vm *VersionManager) calculateQualityScore(content string) float64 {
	score := 50.0 // 基础分

	// 内容长度评分
	contentLen := len(content)
	if contentLen > 1000 {
		score += 20
	} else if contentLen > 500 {
		score += 10
	}

	// 结构完整性评分
	if strings.Contains(content, "#") {
		score += 10 // 包含标题
	}
	if strings.Contains(content, "```") {
		score += 10 // 包含代码块
	}
	if strings.Contains(content, "-") || strings.Contains(content, "*") {
		score += 5 // 包含列表
	}

	// 限制在 0-100 范围内
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return score
}

// sortVersions 对版本进行排序
func (vm *VersionManager) sortVersions(skillName string) {
	versions := vm.versions[skillName]
	sort.Slice(versions, func(i, j int) bool {
		return vm.compareVersions(versions[i].Version, versions[j].Version) < 0
	})
	vm.versions[skillName] = versions
}

// compareVersions 比较两个版本号
// 返回值: >0 表示 v1 > v2, <0 表示 v1 < v2, 0 表示相等
func (vm *VersionManager) compareVersions(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var num1, num2 int
		if i < len(parts1) {
			num1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			num2, _ = strconv.Atoi(parts2[i])
		}

		if num1 > num2 {
			return 1
		}
		if num1 < num2 {
			return -1
		}
	}

	return 0
}

// generateContentDiff 生成内容差异
func (vm *VersionManager) generateContentDiff(content1, content2 string) string {
	lines1 := strings.Split(content1, "\n")
	lines2 := strings.Split(content2, "\n")

	var diff strings.Builder
	maxLines := len(lines1)
	if len(lines2) > maxLines {
		maxLines = len(lines2)
	}

	added := 0
	removed := 0
	modified := 0

	for i := 0; i < maxLines; i++ {
		var line1, line2 string
		if i < len(lines1) {
			line1 = lines1[i]
		}
		if i < len(lines2) {
			line2 = lines2[i]
		}

		if line1 == "" && line2 != "" {
			added++
		} else if line1 != "" && line2 == "" {
			removed++
		} else if line1 != line2 {
			modified++
		}
	}

	diff.WriteString(fmt.Sprintf("  新增: %d 行\n", added))
	diff.WriteString(fmt.Sprintf("  删除: %d 行\n", removed))
	diff.WriteString(fmt.Sprintf("  修改: %d 行\n", modified))

	return diff.String()
}

// saveVersionsToFile 保存版本到文件
func (vm *VersionManager) saveVersionsToFile(skillName string) error {
	versions := vm.versions[skillName]

	// 生成文件名
	fileName := fmt.Sprintf("%s_versions.json", sanitizeFileName(skillName))
	filePath := filepath.Join(vm.versionDir, fileName)

	data, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal versions: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write versions file: %w", err)
	}

	return nil
}

// loadAllVersions 加载所有版本数据
func (vm *VersionManager) loadAllVersions() {
	entries, err := os.ReadDir(vm.versionDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, "_versions.json") {
			continue
		}

		// 提取技能名称
		skillName := strings.TrimSuffix(name, "_versions.json")

		filePath := filepath.Join(vm.versionDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var versions []SkillVersion
		if err := json.Unmarshal(data, &versions); err != nil {
			continue
		}

		vm.versions[skillName] = versions
	}
}

// sanitizeFileName 清理文件名
func sanitizeFileName(name string) string {
	// 替换不安全的字符
	unsafe := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := name
	for _, char := range unsafe {
		result = strings.ReplaceAll(result, char, "_")
	}
	return result
}

// GenerateVersionID 生成唯一版本ID
func GenerateVersionID() string {
	return uuid.New().String()
}
