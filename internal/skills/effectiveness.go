package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// SkillEffectivenessRecord 技能效果记录
type SkillEffectivenessRecord struct {
	ID            string    `json:"id"`
	SkillName     string    `json:"skill_name"`
	TaskType      string    `json:"task_type"`      // 任务类型
	InputSummary  string    `json:"input_summary"`  // 输入摘要
	OutputQuality float64   `json:"output_quality"` // 输出质量评分 0-100
	Success       bool      `json:"success"`        // 是否成功
	Duration      int64     `json:"duration_ms"`    // 执行时长
	ToolsUsed     []string  `json:"tools_used"`     // 使用的工具
	UserFeedback  string    `json:"user_feedback"`  // 用户反馈 (positive/neutral/negative)
	SessionID     string    `json:"session_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// SkillStatistics 技能统计
type SkillStatistics struct {
	SkillName        string    `json:"skill_name"`
	TotalInvocations int       `json:"total_invocations"`
	SuccessRate      float64   `json:"success_rate"`
	AvgQuality       float64   `json:"avg_quality"`
	AvgDuration      int64     `json:"avg_duration_ms"`
	PositiveRate     float64   `json:"positive_rate"`
	LastUsed         time.Time `json:"last_used"`
	Trend            string    `json:"trend"` // improving/stable/declining
}

// EffectivenessManager 效果管理器
type EffectivenessManager struct {
	records      map[string]*SkillEffectivenessRecord // ID -> Record
	stats        map[string]*SkillStatistics          // SkillName -> Stats
	runningStats map[string]*runningStat              // 增量统计缓存
	recordsDB    string                               // 存储路径
	statsDB      string
	mu           sync.RWMutex
	dirty        bool          // 脏标记，标识数据是否有变更
	saveTimer    *time.Ticker  // 定时保存器
	stopChan     chan struct{} // 停止信号
}

// runningStat 增量统计结构
type runningStat struct {
	totalInvocations int
	successCount     int
	qualitySum       float64
	durationSum      int64
	positiveCount    int
	lastUsed         time.Time
}

// NewEffectivenessManager 创建效果管理器
func NewEffectivenessManager(baseDir string) (*EffectivenessManager, error) {
	if baseDir == "" {
		baseDir = config.GetMagicHome()
	}

	skillsDir := filepath.Join(baseDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create skills directory: %w", err)
	}

	em := &EffectivenessManager{
		records:      make(map[string]*SkillEffectivenessRecord),
		stats:        make(map[string]*SkillStatistics),
		runningStats: make(map[string]*runningStat),
		recordsDB:    filepath.Join(skillsDir, "effectiveness.json"),
		statsDB:      filepath.Join(skillsDir, "stats.json"),
	}

	// 加载已有数据
	if err := em.loadRecords(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load records: %w", err)
	}

	if err := em.loadStats(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load stats: %w", err)
	}

	// 从已有记录重建增量统计
	em.rebuildRunningStats()

	return em, nil
}

// RecordInvocation 记录技能调用
func (em *EffectivenessManager) RecordInvocation(
	skillName, taskType, input string,
	outputQuality float64,
	success bool,
	duration int64,
	tools []string,
	sessionID string,
) error {
	if outputQuality < 0 {
		outputQuality = 0
	} else if outputQuality > 100 {
		outputQuality = 100
	}

	// 生成输入摘要（限制长度）
	inputSummary := input
	if len(inputSummary) > 200 {
		inputSummary = inputSummary[:200] + "..."
	}

	record := &SkillEffectivenessRecord{
		ID:            uuid.New().String(),
		SkillName:     skillName,
		TaskType:      taskType,
		InputSummary:  inputSummary,
		OutputQuality: outputQuality,
		Success:       success,
		Duration:      duration,
		ToolsUsed:     tools,
		UserFeedback:  "", // 初始为空，等待用户反馈
		SessionID:     sessionID,
		Timestamp:     time.Now(),
	}

	em.mu.Lock()
	em.records[record.ID] = record

	// 增量更新统计
	rs, exists := em.runningStats[skillName]
	if !exists {
		rs = &runningStat{}
		em.runningStats[skillName] = rs
	}
	rs.totalInvocations++
	if success {
		rs.successCount++
	}
	rs.qualitySum += outputQuality
	rs.durationSum += duration
	if record.Timestamp.After(rs.lastUsed) {
		rs.lastUsed = record.Timestamp
	}

	// 同步到 stats
	em.syncStats(skillName, rs)
	em.dirty = true
	em.mu.Unlock()

	return nil
}

// RecordFeedback 记录用户反馈
func (em *EffectivenessManager) RecordFeedback(recordID, feedback string) error {
	validFeedback := []string{"positive", "neutral", "negative"}
	isValid := false
	for _, vf := range validFeedback {
		if feedback == vf {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("invalid feedback: %s, must be one of: positive, neutral, negative", feedback)
	}

	em.mu.Lock()
	record, exists := em.records[recordID]
	if !exists {
		em.mu.Unlock()
		return fmt.Errorf("record not found: %s", recordID)
	}
	record.UserFeedback = feedback
	skillName := record.SkillName

	// 增量更新正面反馈计数
	if feedback == "positive" {
		if rs, ok := em.runningStats[skillName]; ok {
			rs.positiveCount++
			em.syncStats(skillName, rs)
		}
	}
	em.dirty = true
	em.mu.Unlock()

	return nil
}

// GetSkillStatistics 获取单个技能统计
func (em *EffectivenessManager) GetSkillStatistics(skillName string) *SkillStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if stats, exists := em.stats[skillName]; exists {
		// 创建副本
		statsCopy := *stats
		return &statsCopy
	}
	return nil
}

// GetAllStatistics 获取所有技能统计
func (em *EffectivenessManager) GetAllStatistics() []*SkillStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	result := make([]*SkillStatistics, 0, len(em.stats))
	for _, stats := range em.stats {
		statsCopy := *stats
		result = append(result, &statsCopy)
	}

	// 按调用次数排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalInvocations > result[j].TotalInvocations
	})

	return result
}

// GetTopSkills 获取效果最好的技能
func (em *EffectivenessManager) GetTopSkills(limit int) []*SkillStatistics {
	if limit <= 0 {
		limit = 10
	}

	allStats := em.GetAllStatistics()

	// 按平均质量和成功率排序
	sort.Slice(allStats, func(i, j int) bool {
		scoreI := allStats[i].AvgQuality * allStats[i].SuccessRate
		scoreJ := allStats[j].AvgQuality * allStats[j].SuccessRate
		return scoreI > scoreJ
	})

	if limit > len(allStats) {
		limit = len(allStats)
	}

	return allStats[:limit]
}

// GetDecliningSkills 获取效果下降的技能
func (em *EffectivenessManager) GetDecliningSkills() []*SkillStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	result := make([]*SkillStatistics, 0)
	for skillName := range em.stats {
		trend := em.calculateTrend(skillName)
		if trend == "declining" {
			statsCopy := *em.stats[skillName]
			statsCopy.Trend = trend
			result = append(result, &statsCopy)
		}
	}

	return result
}

// GetRecordsForSkill 获取技能的历史记录
func (em *EffectivenessManager) GetRecordsForSkill(skillName string, limit int) []*SkillEffectivenessRecord {
	em.mu.RLock()
	defer em.mu.RUnlock()

	var records []*SkillEffectivenessRecord
	for _, record := range em.records {
		if record.SkillName == skillName {
			recordCopy := *record
			records = append(records, &recordCopy)
		}
	}

	// 按时间倒序排序
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.After(records[j].Timestamp)
	})

	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}

	return records
}

// CalculateTrend 计算技能效果趋势
func (em *EffectivenessManager) CalculateTrend(skillName string) string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.calculateTrend(skillName)
}

// calculateTrend 内部计算趋势方法（不加锁）
func (em *EffectivenessManager) calculateTrend(skillName string) string {
	now := time.Now()
	recentStart := now.AddDate(0, 0, -7)
	previousStart := now.AddDate(0, 0, -14)

	var recentSum, previousSum float64
	var recentCount, previousCount int

	for _, record := range em.records {
		if record.SkillName != skillName {
			continue
		}

		if record.Timestamp.After(recentStart) {
			recentSum += record.OutputQuality
			recentCount++
		} else if record.Timestamp.After(previousStart) && record.Timestamp.Before(recentStart) {
			previousSum += record.OutputQuality
			previousCount++
		}
	}

	if recentCount == 0 || previousCount == 0 {
		return "stable"
	}

	recentAvg := recentSum / float64(recentCount)
	previousAvg := previousSum / float64(previousCount)

	difference := recentAvg - previousAvg
	percentageChange := (difference / previousAvg) * 100

	if percentageChange > 5 {
		return "improving"
	} else if percentageChange < -5 {
		return "declining"
	}
	return "stable"
}

// ClearOldRecords 清理旧记录
func (em *EffectivenessManager) ClearOldRecords(olderThan time.Duration) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)

	for id, record := range em.records {
		if record.Timestamp.Before(cutoff) {
			delete(em.records, id)
		}
	}

	// 重建增量统计
	em.runningStats = make(map[string]*runningStat)
	em.rebuildRunningStats()

	// 重建所有 stats
	em.stats = make(map[string]*SkillStatistics)
	for name, rs := range em.runningStats {
		em.syncStats(name, rs)
	}

	em.dirty = true
	return nil
}

// updateStatistics 更新单个技能的统计（使用增量统计）
func (em *EffectivenessManager) updateStatistics(skillName string) {
	rs, exists := em.runningStats[skillName]
	if !exists {
		return
	}
	em.syncStats(skillName, rs)
}

// recalculateAllStats 重新计算所有统计
func (em *EffectivenessManager) recalculateAllStats() {
	skillNames := make(map[string]bool)
	for _, record := range em.records {
		skillNames[record.SkillName] = true
	}

	em.stats = make(map[string]*SkillStatistics)
	for skillName := range skillNames {
		em.updateStatistics(skillName)
	}
}

// saveRecords 保存记录到文件
func (em *EffectivenessManager) saveRecords() error {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.saveRecordsLocked()
}

// saveStats 保存统计到文件
func (em *EffectivenessManager) saveStats() error {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.saveStatsLocked()
}

// saveRecordsLocked 保存记录（调用者需持有写锁）
func (em *EffectivenessManager) saveRecordsLocked() error {
	records := make([]*SkillEffectivenessRecord, 0, len(em.records))
	for _, record := range em.records {
		records = append(records, record)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal records: %w", err)
	}

	return os.WriteFile(em.recordsDB, data, 0644)
}

// saveStatsLocked 保存统计（调用者需持有写锁）
func (em *EffectivenessManager) saveStatsLocked() error {
	stats := make([]*SkillStatistics, 0, len(em.stats))
	for _, s := range em.stats {
		stats = append(stats, s)
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	return os.WriteFile(em.statsDB, data, 0644)
}

// syncStats 从增量统计同步到 SkillStatistics
func (em *EffectivenessManager) syncStats(skillName string, rs *runningStat) {
	if rs.totalInvocations == 0 {
		return
	}

	em.stats[skillName] = &SkillStatistics{
		SkillName:        skillName,
		TotalInvocations: rs.totalInvocations,
		SuccessRate:      float64(rs.successCount) / float64(rs.totalInvocations) * 100,
		AvgQuality:       rs.qualitySum / float64(rs.totalInvocations),
		AvgDuration:      rs.durationSum / int64(rs.totalInvocations),
		PositiveRate:     float64(rs.positiveCount) / float64(rs.totalInvocations) * 100,
		LastUsed:         rs.lastUsed,
		Trend:            em.calculateTrend(skillName),
	}
}

// rebuildRunningStats 从已有记录重建增量统计
func (em *EffectivenessManager) rebuildRunningStats() {
	for _, record := range em.records {
		rs, exists := em.runningStats[record.SkillName]
		if !exists {
			rs = &runningStat{}
			em.runningStats[record.SkillName] = rs
		}
		rs.totalInvocations++
		if record.Success {
			rs.successCount++
		}
		rs.qualitySum += record.OutputQuality
		rs.durationSum += record.Duration
		if record.UserFeedback == "positive" {
			rs.positiveCount++
		}
		if record.Timestamp.After(rs.lastUsed) {
			rs.lastUsed = record.Timestamp
		}
	}
}

// StartAutoSave 启动自动保存（每30秒检查脏标记）
func (em *EffectivenessManager) StartAutoSave() {
	em.saveTimer = time.NewTicker(30 * time.Second)
	em.stopChan = make(chan struct{})

	go func() {
		for {
			select {
			case <-em.saveTimer.C:
				em.mu.RLock()
				if em.dirty {
					em.mu.RUnlock()
					em.Flush()
				} else {
					em.mu.RUnlock()
				}
			case <-em.stopChan:
				em.saveTimer.Stop()
				return
			}
		}
	}()
}

// StopAutoSave 停止自动保存
func (em *EffectivenessManager) StopAutoSave() {
	if em.stopChan != nil {
		close(em.stopChan)
	}
	em.Flush()
}

// Flush 强制保存所有数据到磁盘
func (em *EffectivenessManager) Flush() {
	em.mu.Lock()
	defer em.mu.Unlock()

	_ = em.saveRecordsLocked()
	_ = em.saveStatsLocked()
	em.dirty = false
}

// loadRecords 从文件加载记录
func (em *EffectivenessManager) loadRecords() error {
	data, err := os.ReadFile(em.recordsDB)
	if err != nil {
		return err
	}

	var records []*SkillEffectivenessRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("failed to unmarshal records: %w", err)
	}

	for _, record := range records {
		em.records[record.ID] = record
	}

	return nil
}

// loadStats 从文件加载统计
func (em *EffectivenessManager) loadStats() error {
	data, err := os.ReadFile(em.statsDB)
	if err != nil {
		return err
	}

	var stats []*SkillStatistics
	if err := json.Unmarshal(data, &stats); err != nil {
		return fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	for _, s := range stats {
		em.stats[s.SkillName] = s
	}

	return nil
}
