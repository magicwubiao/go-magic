package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// PluginEffectivenessRecord 插件效果记录
type PluginEffectivenessRecord struct {
	ID           string                 `json:"id"`
	PluginID     string                 `json:"plugin_id"`
	PluginName   string                 `json:"plugin_name"`
	Command      string                 `json:"command"`
	Args         []string               `json:"args"`
	Result       interface{}            `json:"result"`
	Success      bool                   `json:"success"`
	Duration     time.Duration          `json:"duration_ms"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	SessionID    string                 `json:"session_id"`
	Timestamp    time.Time              `json:"timestamp"`
}

// PluginStatistics 插件统计
type PluginStatistics struct {
	PluginID     string         `json:"plugin_id"`
	PluginName   string         `json:"plugin_name"`
	TotalCalls   int            `json:"total_calls"`
	SuccessCalls int            `json:"success_calls"`
	FailedCalls  int            `json:"failed_calls"`
	SuccessRate  float64        `json:"success_rate"`
	AvgDuration  int64          `json:"avg_duration_ms"`
	CommandStats map[string]int `json:"command_stats"` // command -> count
	LastUsed     time.Time      `json:"last_used"`
	Trend        string         `json:"trend"`
}

// EffectivenessManager 效果管理器
type EffectivenessManager struct {
	records   map[string]*PluginEffectivenessRecord
	stats     map[string]*PluginStatistics
	recordsDB string
	statsDB   string
	mu        sync.RWMutex
}

// NewEffectivenessManager 创建新的效果管理器
func NewEffectivenessManager(baseDir string) (*EffectivenessManager, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".magic", "plugins")
	}

	// 确保目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	em := &EffectivenessManager{
		records:   make(map[string]*PluginEffectivenessRecord),
		stats:     make(map[string]*PluginStatistics),
		recordsDB: filepath.Join(baseDir, "effectiveness.json"),
		statsDB:   filepath.Join(baseDir, "stats.json"),
	}

	// 加载已有数据
	if err := em.loadRecords(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load records: %w", err)
	}

	if err := em.loadStats(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load stats: %w", err)
	}

	return em, nil
}

// RecordPluginCall 记录插件调用
func (em *EffectivenessManager) RecordPluginCall(
	pluginID, pluginName, command string,
	args []string,
	result interface{},
	success bool,
	duration time.Duration,
	sessionID string,
) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	// 创建记录
	record := &PluginEffectivenessRecord{
		ID:        generateID(),
		PluginID:  pluginID,
		PluginName: pluginName,
		Command:   command,
		Args:      args,
		Result:    result,
		Success:   success,
		Duration:  duration,
		SessionID: sessionID,
		Timestamp: time.Now(),
	}

	if !success {
		if err, ok := result.(error); ok {
			record.ErrorMessage = err.Error()
		}
	}

	em.records[record.ID] = record

	// 更新统计
	em.updateStatistics(record)

	// 保存数据
	if err := em.saveRecords(); err != nil {
		return fmt.Errorf("failed to save records: %w", err)
	}

	if err := em.saveStats(); err != nil {
		return fmt.Errorf("failed to save stats: %w", err)
	}

	return nil
}

// GetPluginStatistics 获取指定插件的统计信息
func (em *EffectivenessManager) GetPluginStatistics(pluginID string) *PluginStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if stats, ok := em.stats[pluginID]; ok {
		// 复制一份避免外部修改
		statsCopy := *stats
		statsCopy.Trend = em.calculateTrendLocked(pluginID)
		return &statsCopy
	}

	return nil
}

// GetAllStatistics 获取所有插件的统计信息
func (em *EffectivenessManager) GetAllStatistics() []*PluginStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	result := make([]*PluginStatistics, 0, len(em.stats))
	for _, stats := range em.stats {
		statsCopy := *stats
		statsCopy.Trend = em.calculateTrendLocked(stats.PluginID)
		result = append(result, &statsCopy)
	}

	// 按总调用次数排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCalls > result[j].TotalCalls
	})

	return result
}

// GetTopPlugins 获取使用最多的插件
func (em *EffectivenessManager) GetTopPlugins(limit int) []*PluginStatistics {
	allStats := em.GetAllStatistics()

	if limit <= 0 || limit > len(allStats) {
		limit = len(allStats)
	}

	return allStats[:limit]
}

// GetFailingPlugins 获取失败的插件列表
func (em *EffectivenessManager) GetFailingPlugins() []*PluginStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	result := make([]*PluginStatistics, 0)
	for _, stats := range em.stats {
		if stats.SuccessRate < 0.5 || stats.FailedCalls > 0 {
			statsCopy := *stats
			statsCopy.Trend = em.calculateTrendLocked(stats.PluginID)
			result = append(result, &statsCopy)
		}
	}

	// 按失败率排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].SuccessRate < result[j].SuccessRate
	})

	return result
}

// CalculateTrend 计算插件趋势
func (em *EffectivenessManager) CalculateTrend(pluginID string) string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.calculateTrendLocked(pluginID)
}

// 内部方法：计算趋势（需要持有读锁）
func (em *EffectivenessManager) calculateTrendLocked(pluginID string) string {
	// 获取该插件的所有记录
	var pluginRecords []*PluginEffectivenessRecord
	for _, record := range em.records {
		if record.PluginID == pluginID {
			pluginRecords = append(pluginRecords, record)
		}
	}

	if len(pluginRecords) < 5 {
		return "insufficient_data"
	}

	// 按时间排序
	sort.Slice(pluginRecords, func(i, j int) bool {
		return pluginRecords[i].Timestamp.Before(pluginRecords[j].Timestamp)
	})

	// 分成两半比较成功率
	mid := len(pluginRecords) / 2
	firstHalfSuccess := 0
	secondHalfSuccess := 0

	for i, record := range pluginRecords {
		if i < mid {
			if record.Success {
				firstHalfSuccess++
			}
		} else {
			if record.Success {
				secondHalfSuccess++
			}
		}
	}

	firstHalfRate := float64(firstHalfSuccess) / float64(mid)
	secondHalfRate := float64(secondHalfSuccess) / float64(len(pluginRecords)-mid)

	diff := secondHalfRate - firstHalfRate
	switch {
	case diff > 0.1:
		return "improving"
	case diff < -0.1:
		return "declining"
	default:
		return "stable"
	}
}

// 内部方法：更新统计
func (em *EffectivenessManager) updateStatistics(record *PluginEffectivenessRecord) {
	stats, ok := em.stats[record.PluginID]
	if !ok {
		stats = &PluginStatistics{
			PluginID:     record.PluginID,
			PluginName:   record.PluginName,
			CommandStats: make(map[string]int),
		}
		em.stats[record.PluginID] = stats
	}

	stats.TotalCalls++
	if record.Success {
		stats.SuccessCalls++
	} else {
		stats.FailedCalls++
	}

	stats.SuccessRate = float64(stats.SuccessCalls) / float64(stats.TotalCalls)

	// 计算平均耗时
	currentAvg := time.Duration(stats.AvgDuration) * time.Millisecond
	newAvg := (currentAvg*time.Duration(stats.TotalCalls-1) + record.Duration) / time.Duration(stats.TotalCalls)
	stats.AvgDuration = int64(newAvg / time.Millisecond)

	// 更新命令统计
	stats.CommandStats[record.Command]++

	// 更新最后使用时间
	if record.Timestamp.After(stats.LastUsed) {
		stats.LastUsed = record.Timestamp
	}
}

// 内部方法：加载记录
func (em *EffectivenessManager) loadRecords() error {
	data, err := os.ReadFile(em.recordsDB)
	if err != nil {
		return err
	}

	var records []*PluginEffectivenessRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}

	for _, record := range records {
		em.records[record.ID] = record
	}

	return nil
}

// 内部方法：加载统计
func (em *EffectivenessManager) loadStats() error {
	data, err := os.ReadFile(em.statsDB)
	if err != nil {
		return err
	}

	var statsList []*PluginStatistics
	if err := json.Unmarshal(data, &statsList); err != nil {
		return err
	}

	for _, stats := range statsList {
		em.stats[stats.PluginID] = stats
	}

	return nil
}

// 内部方法：保存记录
func (em *EffectivenessManager) saveRecords() error {
	records := make([]*PluginEffectivenessRecord, 0, len(em.records))
	for _, record := range em.records {
		records = append(records, record)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(em.recordsDB, data, 0644)
}

// 内部方法：保存统计
func (em *EffectivenessManager) saveStats() error {
	stats := make([]*PluginStatistics, 0, len(em.stats))
	for _, s := range em.stats {
		stats = append(stats, s)
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(em.statsDB, data, 0644)
}

// 生成唯一ID
func generateID() string {
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), time.Now().Unix())
}
