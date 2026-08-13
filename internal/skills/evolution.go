package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

// SkillEvolutionConfig 进化配置
type SkillEvolutionConfig struct {
	MinRecordsForEvolution int     `json:"min_records"`        // 最少记录数才进化 (默认 5)
	QualityThreshold       float64 `json:"quality_threshold"`  // 质量阈值 (默认 60)
	EvolutionInterval      int     `json:"evolution_interval"` // 进化间隔小时 (默认 24)
	MaxEvolutionSteps      int     `json:"max_steps"`          // 最大进化步数 (默认 10)
}

// SkillEvolutionRecord 进化记录
type SkillEvolutionRecord struct {
	ID            string    `json:"id"`
	SkillName     string    `json:"skill_name"`
	OldContent    string    `json:"old_content"`    // 进化前内容
	NewContent    string    `json:"new_content"`    // 进化后内容
	Reason        string    `json:"reason"`         // 进化原因
	QualityBefore float64   `json:"quality_before"` // 进化前质量
	QualityAfter  float64   `json:"quality_after"`  // 进化后质量 (待验证)
	Generation    int       `json:"generation"`     // 进化代数
	Timestamp     time.Time `json:"timestamp"`
	Status        string    `json:"status"` // pending/validated/reverted
}

// SkillEvolutionManager 进化管理器
type SkillEvolutionManager struct {
	config        SkillEvolutionConfig
	records       []SkillEvolutionRecord
	manager       *Manager
	effectiveness *EffectivenessManager
	provider      provider.Provider
	evolutionDB   string
	mu            sync.RWMutex
}

// EvolutionStrategy 进化策略类型
type EvolutionStrategy string

const (
	StrategyAddExamples    EvolutionStrategy = "add_examples"    // 添加示例
	StrategySimplify       EvolutionStrategy = "simplify"        // 简化指令
	StrategyAddBoundaries  EvolutionStrategy = "add_boundaries"  // 增加边界条件
	StrategyAdjustFormat   EvolutionStrategy = "adjust_format"   // 调整格式说明
	StrategyClarifyIntent  EvolutionStrategy = "clarify_intent"  // 澄清意图
	StrategyAddConstraints EvolutionStrategy = "add_constraints" // 添加约束条件
)

// EvolutionContext 进化上下文
type EvolutionContext struct {
	SkillName         string                      `json:"skill_name"`
	CurrentContent    string                      `json:"current_content"`
	Stats             *SkillStatistics            `json:"stats"`
	FailureCases      []*SkillEffectivenessRecord `json:"failure_cases"`
	CurrentGeneration int                         `json:"current_generation"`
}

// EvolutionResult 进化结果
type EvolutionResult struct {
	Strategy   EvolutionStrategy `json:"strategy"`
	NewContent string            `json:"new_content"`
	Reason     string            `json:"reason"`
	Confidence float64           `json:"confidence"`
}

// DefaultEvolutionConfig 返回默认进化配置
func DefaultEvolutionConfig() SkillEvolutionConfig {
	return SkillEvolutionConfig{
		MinRecordsForEvolution: 5,
		QualityThreshold:       60.0,
		EvolutionInterval:      24,
		MaxEvolutionSteps:      10,
	}
}

// NewSkillEvolutionManager 创建进化管理器
func NewSkillEvolutionManager(manager *Manager, effMgr *EffectivenessManager, prov provider.Provider, baseDir string) *SkillEvolutionManager {
	config := DefaultEvolutionConfig()

	return &SkillEvolutionManager{
		config:        config,
		records:       make([]SkillEvolutionRecord, 0),
		manager:       manager,
		effectiveness: effMgr,
		provider:      prov,
		evolutionDB:   filepath.Join(baseDir, "evolution_records.json"),
	}
}

// LoadRecords 从文件加载进化记录
func (em *SkillEvolutionManager) LoadRecords() error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if _, err := os.Stat(em.evolutionDB); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(em.evolutionDB)
	if err != nil {
		return fmt.Errorf("failed to read evolution records: %w", err)
	}

	if err := json.Unmarshal(data, &em.records); err != nil {
		return fmt.Errorf("failed to unmarshal evolution records: %w", err)
	}

	return nil
}

// SaveRecords 保存进化记录到文件
func (em *SkillEvolutionManager) SaveRecords() error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	data, err := json.MarshalIndent(em.records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal evolution records: %w", err)
	}

	if err := os.WriteFile(em.evolutionDB, data, 0644); err != nil {
		return fmt.Errorf("failed to write evolution records: %w", err)
	}

	return nil
}

// CheckEvolutionNeeded 检查技能是否需要进化
func (em *SkillEvolutionManager) CheckEvolutionNeeded(skillName string) (bool, string) {
	// 获取技能效果统计
	stats := em.effectiveness.GetSkillStatistics(skillName)
	if stats == nil {
		return false, "暂无统计记录"
	}

	// 检查记录数是否足够 (使用 TotalInvocations)
	if stats.TotalInvocations < em.config.MinRecordsForEvolution {
		return false, fmt.Sprintf("记录数不足: %d < %d", stats.TotalInvocations, em.config.MinRecordsForEvolution)
	}

	// 检查质量是否低于阈值
	if stats.AvgQuality >= em.config.QualityThreshold {
		return false, fmt.Sprintf("质量达标: %.2f >= %.2f", stats.AvgQuality, em.config.QualityThreshold)
	}

	// 检查是否已经有过最近的进化记录
	if em.hasRecentEvolution(skillName) {
		return false, "最近已有进化记录，等待验证"
	}

	return true, fmt.Sprintf("质量 %.2f 低于阈值 %.2f，需要进化", stats.AvgQuality, em.config.QualityThreshold)
}

// hasRecentEvolution 检查是否有最近的待验证进化记录
func (em *SkillEvolutionManager) hasRecentEvolution(skillName string) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(em.config.EvolutionInterval) * time.Hour)

	for _, record := range em.records {
		if record.SkillName == skillName && record.Timestamp.After(cutoff) {
			if record.Status == "pending" {
				return true
			}
		}
	}

	return false
}

// getCurrentGeneration 获取技能的当前进化代数
func (em *SkillEvolutionManager) getCurrentGeneration(skillName string) int {
	em.mu.RLock()
	defer em.mu.RUnlock()

	maxGen := 0
	for _, record := range em.records {
		if record.SkillName == skillName && record.Generation > maxGen {
			maxGen = record.Generation
		}
	}

	return maxGen
}

// collectFailureCases 收集失败案例
func (em *SkillEvolutionManager) collectFailureCases(skillName string) []*SkillEffectivenessRecord {
	allRecords := em.effectiveness.GetRecordsForSkill(skillName, 100)
	failures := make([]*SkillEffectivenessRecord, 0)

	for _, record := range allRecords {
		if record.Success {
			continue
		}
		failures = append(failures, record)
	}

	// 只保留最近的10个失败案例
	if len(failures) > 10 {
		failures = failures[len(failures)-10:]
	}

	return failures
}

// EvolveSkill 进化技能
func (em *SkillEvolutionManager) EvolveSkill(ctx context.Context, skillName string) (*SkillEvolutionRecord, error) {
	// 再次检查是否需要进化
	needed, reason := em.CheckEvolutionNeeded(skillName)
	if !needed {
		return nil, fmt.Errorf("技能不需要进化: %s", reason)
	}

	// 获取技能
	skill, err := em.manager.Get(skillName)
	if err != nil {
		return nil, fmt.Errorf("获取技能失败: %w", err)
	}

	// 获取效果统计
	stats := em.effectiveness.GetSkillStatistics(skillName)

	// 收集失败案例
	failures := em.collectFailureCases(skillName)

	// 构建进化上下文
	evolutionCtx := EvolutionContext{
		SkillName:         skillName,
		CurrentContent:    skill.Content,
		Stats:             stats,
		FailureCases:      failures,
		CurrentGeneration: em.getCurrentGeneration(skillName),
	}

	// 使用GEPA引擎生成优化策略
	result, err := em.generateEvolutionStrategy(ctx, evolutionCtx)
	if err != nil {
		return nil, fmt.Errorf("生成进化策略失败: %w", err)
	}

	// 检查是否达到最大进化步数
	if evolutionCtx.CurrentGeneration >= em.config.MaxEvolutionSteps {
		return nil, fmt.Errorf("已达到最大进化步数: %d", em.config.MaxEvolutionSteps)
	}

	// 创建进化记录
	record := SkillEvolutionRecord{
		ID:            em.generateRecordID(),
		SkillName:     skillName,
		OldContent:    skill.Content,
		NewContent:    result.NewContent,
		Reason:        fmt.Sprintf("[%s] %s", result.Strategy, result.Reason),
		QualityBefore: stats.AvgQuality,
		QualityAfter:  0, // 待验证
		Generation:    evolutionCtx.CurrentGeneration + 1,
		Timestamp:     time.Now(),
		Status:        "pending",
	}

	// 应用新内容到技能
	skill.Content = result.NewContent
	if err := em.manager.Update(skillName, result.NewContent); err != nil {
		return nil, fmt.Errorf("更新技能内容失败: %w", err)
	}

	// 保存进化记录
	em.mu.Lock()
	em.records = append(em.records, record)
	em.mu.Unlock()

	if err := em.SaveRecords(); err != nil {
		return nil, fmt.Errorf("保存进化记录失败: %w", err)
	}

	return &record, nil
}

// generateEvolutionStrategy 使用GEPA引擎生成进化策略
func (em *SkillEvolutionManager) generateEvolutionStrategy(ctx context.Context, evoCtx EvolutionContext) (*EvolutionResult, error) {
	if em.provider == nil {
		return nil, fmt.Errorf("provider not available")
	}

	// 构建提示词
	prompt := em.buildEvolutionPrompt(evoCtx)

	// 调用provider生成优化策略
	resp, err := em.provider.Chat(ctx, []provider.Message{{Role: "user", Content: prompt}})
	if err != nil {
		return nil, fmt.Errorf("GEPA引擎调用失败: %w", err)
	}

	// 解析响应
	result, err := em.parseEvolutionResponse(resp.Content, evoCtx.CurrentContent)
	if err != nil {
		return nil, fmt.Errorf("解析进化响应失败: %w", err)
	}

	return result, nil
}

// buildEvolutionPrompt 构建进化提示词
func (em *SkillEvolutionManager) buildEvolutionPrompt(evoCtx EvolutionContext) string {
	var sb strings.Builder

	sb.WriteString("你是一位专业的AI提示词优化专家。请分析以下技能的效果数据，并提供优化后的提示词内容。\n\n")

	sb.WriteString("## 技能名称\n")
	sb.WriteString(evoCtx.SkillName)
	sb.WriteString("\n\n")

	sb.WriteString("## 当前提示词内容\n```\n")
	sb.WriteString(evoCtx.CurrentContent)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## 效果统计\n")
	if evoCtx.Stats != nil {
		sb.WriteString(fmt.Sprintf("- 总调用数: %d\n", evoCtx.Stats.TotalInvocations))
		sb.WriteString(fmt.Sprintf("- 平均质量: %.2f\n", evoCtx.Stats.AvgQuality))
		sb.WriteString(fmt.Sprintf("- 成功率: %.2f%%\n", evoCtx.Stats.SuccessRate*100))
	}
	sb.WriteString(fmt.Sprintf("- 当前进化代数: %d\n", evoCtx.CurrentGeneration))
	sb.WriteString("\n")

	if len(evoCtx.FailureCases) > 0 {
		sb.WriteString("## 失败案例分析\n")
		for i, failure := range evoCtx.FailureCases {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... 还有 %d 个失败案例\n", len(evoCtx.FailureCases)-5))
				break
			}
			successStr := "失败"
			if failure.Success {
				successStr = "成功"
			}
			sb.WriteString(fmt.Sprintf("### 案例 %d (%s)\n", i+1, successStr))
			sb.WriteString(fmt.Sprintf("输入: %s\n", utils.Truncate(failure.InputSummary, 200)))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("## 可选的优化策略\n")
	sb.WriteString("1. add_examples - 添加具体示例说明期望的输出格式\n")
	sb.WriteString("2. simplify - 简化复杂或冗余的指令\n")
	sb.WriteString("3. add_boundaries - 增加边界条件说明\n")
	sb.WriteString("4. adjust_format - 调整输出格式说明\n")
	sb.WriteString("5. clarify_intent - 澄清任务意图和目标\n")
	sb.WriteString("6. add_constraints - 添加明确的约束条件\n\n")

	sb.WriteString("## 输出要求\n")
	sb.WriteString("请以JSON格式返回优化结果，包含以下字段:\n")
	sb.WriteString(`{`)
	sb.WriteString(`"strategy": "策略类型", `)
	sb.WriteString(`"new_content": "优化后的完整提示词内容", `)
	sb.WriteString(`"reason": "选择此策略的原因", `)
	sb.WriteString(`"confidence": 0.85`)
	sb.WriteString(`}`)
	sb.WriteString("\n\n请确保 new_content 是完整的、可直接使用的提示词内容。")

	return sb.String()
}

// parseEvolutionResponse 解析进化响应
func (em *SkillEvolutionManager) parseEvolutionResponse(response, oldContent string) (*EvolutionResult, error) {
	// 尝试从响应中提取JSON
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")

	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("无法从响应中提取JSON")
	}

	jsonStr := response[jsonStart : jsonEnd+1]

	var result EvolutionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	// 验证结果
	if result.NewContent == "" {
		return nil, fmt.Errorf("优化后的内容为空")
	}

	if result.Strategy == "" {
		result.Strategy = StrategyClarifyIntent
	}

	return &result, nil
}

// generateRecordID 生成记录ID
func (em *SkillEvolutionManager) generateRecordID() string {
	return fmt.Sprintf("evo_%d_%d", time.Now().Unix(), time.Now().Nanosecond())
}

// ValidateEvolution 验证进化效果
func (em *SkillEvolutionManager) ValidateEvolution(recordID string, newQuality float64) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	// 查找记录
	recordIndex := -1
	for i, record := range em.records {
		if record.ID == recordID {
			recordIndex = i
			break
		}
	}

	if recordIndex == -1 {
		return fmt.Errorf("进化记录不存在: %s", recordID)
	}

	record := &em.records[recordIndex]

	if record.Status != "pending" {
		return fmt.Errorf("记录状态不是pending: %s", record.Status)
	}

	// 更新质量分数
	record.QualityAfter = newQuality

	// 判断是否验证通过 (新质量 > 旧质量 + 10%)
	improvementThreshold := record.QualityBefore * 1.1

	if newQuality >= improvementThreshold {
		record.Status = "validated"
	} else if newQuality < record.QualityBefore {
		// 质量下降，自动回滚
		record.Status = "reverted"
		if err := em.revertSkillContent(record.SkillName, record.OldContent); err != nil {
			return fmt.Errorf("自动回滚失败: %w", err)
		}
	} else {
		// 质量提升但不显著，保持pending等待人工确认
		record.Status = "pending_review"
	}

	return em.SaveRecords()
}

// RevertEvolution 回滚进化
func (em *SkillEvolutionManager) RevertEvolution(recordID string) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	// 查找记录
	recordIndex := -1
	for i, record := range em.records {
		if record.ID == recordID {
			recordIndex = i
			break
		}
	}

	if recordIndex == -1 {
		return fmt.Errorf("进化记录不存在: %s", recordID)
	}

	record := &em.records[recordIndex]

	if record.Status == "reverted" {
		return fmt.Errorf("记录已经回滚")
	}

	// 回滚技能内容
	if err := em.revertSkillContent(record.SkillName, record.OldContent); err != nil {
		return fmt.Errorf("回滚技能内容失败: %w", err)
	}

	record.Status = "reverted"

	return em.SaveRecords()
}

// revertSkillContent 回滚技能内容
func (em *SkillEvolutionManager) revertSkillContent(skillName, oldContent string) error {
	return em.manager.Update(skillName, oldContent)
}

// GetEvolutionHistory 获取技能的进化历史
func (em *SkillEvolutionManager) GetEvolutionHistory(skillName string) []SkillEvolutionRecord {
	em.mu.RLock()
	defer em.mu.RUnlock()

	history := make([]SkillEvolutionRecord, 0)
	for _, record := range em.records {
		if record.SkillName == skillName {
			history = append(history, record)
		}
	}

	return history
}

// GetPendingEvolutions 获取待验证的进化记录
func (em *SkillEvolutionManager) GetPendingEvolutions() []SkillEvolutionRecord {
	em.mu.RLock()
	defer em.mu.RUnlock()

	pending := make([]SkillEvolutionRecord, 0)
	for _, record := range em.records {
		if record.Status == "pending" || record.Status == "pending_review" {
			pending = append(pending, record)
		}
	}

	return pending
}

// RunAutoEvolution 自动检查所有技能并进化
func (em *SkillEvolutionManager) RunAutoEvolution(ctx context.Context) error {
	// 获取所有技能
	skills := em.manager.List()

	var evolvedCount int
	var errors []string

	for _, skill := range skills {
		needed, _ := em.CheckEvolutionNeeded(skill.Name)
		if !needed {
			continue
		}

		_, err := em.EvolveSkill(ctx, skill.Name)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", skill.Name, err))
			continue
		}

		evolvedCount++
	}

	if len(errors) > 0 {
		return fmt.Errorf("自动进化完成: %d 个技能已进化, 错误: %v", evolvedCount, errors)
	}

	return nil
}

// UpdateConfig 更新进化配置
func (em *SkillEvolutionManager) UpdateConfig(config SkillEvolutionConfig) {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.config = config
}

// GetConfig 获取当前配置
func (em *SkillEvolutionManager) GetConfig() SkillEvolutionConfig {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.config
}
