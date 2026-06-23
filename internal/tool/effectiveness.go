package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// ToolEffectivenessRecord 工具效果记录
type ToolEffectivenessRecord struct {
	ID           string        `json:"id"`
	ToolName     string        `json:"tool_name"`
	ToolsetName  string        `json:"toolset_name,omitempty"`
	InputSummary string        `json:"input_summary"`
	Output       string        `json:"output"`
	Success      bool          `json:"success"`
	Duration     time.Duration `json:"duration_ms"`
	ErrorMessage string        `json:"error_message,omitempty"`
	SessionID    string        `json:"session_id"`
	Timestamp    time.Time     `json:"timestamp"`
}

// ToolStatistics 工具统计
type ToolStatistics struct {
	ToolName     string    `json:"tool_name"`
	TotalCalls   int       `json:"total_calls"`
	SuccessCalls int       `json:"success_calls"`
	FailedCalls  int       `json:"failed_calls"`
	SuccessRate  float64   `json:"success_rate"`
	AvgDuration  int64     `json:"avg_duration_ms"`
	LastUsed     time.Time `json:"last_used"`
	Trend        string    `json:"trend"` // improving/stable/declining
}

// ToolsetStatistics 工具集统计
type ToolsetStatistics struct {
	ToolsetName string         `json:"toolset_name"`
	TotalCalls  int            `json:"total_calls"`
	ToolStats   map[string]int `json:"tool_stats"` // tool_name -> call_count
	LastUsed    time.Time      `json:"last_used"`
}

// EffectivenessManager 效果管理器
type EffectivenessManager struct {
	records      map[string]*ToolEffectivenessRecord
	toolStats    map[string]*ToolStatistics
	toolsetStats map[string]*ToolsetStatistics
	recordsDB    string
	statsDB      string
	mu           sync.RWMutex
}

// NewEffectivenessManager 创建新的效果管理器
func NewEffectivenessManager(baseDir string) (*EffectivenessManager, error) {
	if baseDir == "" {
		baseDir = config.GetMagicHome()
	}

	toolsDir := filepath.Join(baseDir, "tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create tools directory: %w", err)
	}

	em := &EffectivenessManager{
		records:      make(map[string]*ToolEffectivenessRecord),
		toolStats:    make(map[string]*ToolStatistics),
		toolsetStats: make(map[string]*ToolsetStatistics),
		recordsDB:    filepath.Join(toolsDir, "effectiveness.json"),
		statsDB:      filepath.Join(toolsDir, "stats.json"),
	}

	// 加载已有数据
	if err := em.load(); err != nil {
		// 如果文件不存在，忽略错误
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load data: %w", err)
		}
	}

	return em, nil
}

// RecordToolCall 记录工具调用
func (em *EffectivenessManager) RecordToolCall(toolName, toolsetName, input, output string, success bool, duration time.Duration, sessionID string) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	record := &ToolEffectivenessRecord{
		ID:           generateID(),
		ToolName:     toolName,
		ToolsetName:  toolsetName,
		InputSummary: truncateString(input, 200),
		Output:       truncateString(output, 500),
		Success:      success,
		Duration:     duration,
		SessionID:    sessionID,
		Timestamp:    time.Now(),
	}

	if !success {
		record.ErrorMessage = output
	}

	em.records[record.ID] = record

	// 更新工具统计
	em.updateToolStats(toolName, success, duration)

	// 更新工具集统计
	if toolsetName != "" {
		em.updateToolsetStats(toolsetName, toolName)
	}

	// 保存数据
	return em.save()
}

// GetToolStatistics 获取指定工具的统计信息
func (em *EffectivenessManager) GetToolStatistics(toolName string) *ToolStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if stats, ok := em.toolStats[toolName]; ok {
		// 计算趋势
		statsCopy := *stats
		statsCopy.Trend = em.calculateTrendLocked(toolName)
		return &statsCopy
	}

	return nil
}

// GetAllToolStatistics 获取所有工具的统计信息
func (em *EffectivenessManager) GetAllToolStatistics() []*ToolStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	result := make([]*ToolStatistics, 0, len(em.toolStats))
	for _, stats := range em.toolStats {
		statsCopy := *stats
		statsCopy.Trend = em.calculateTrendLocked(stats.ToolName)
		result = append(result, &statsCopy)
	}

	// 按总调用次数排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCalls > result[j].TotalCalls
	})

	return result
}

// GetToolsetStatistics 获取工具集统计信息
func (em *EffectivenessManager) GetToolsetStatistics(toolsetName string) *ToolsetStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if stats, ok := em.toolsetStats[toolsetName]; ok {
		statsCopy := *stats
		return &statsCopy
	}

	return nil
}

// GetTopTools 获取使用最多的工具
func (em *EffectivenessManager) GetTopTools(limit int) []*ToolStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	allStats := make([]*ToolStatistics, 0, len(em.toolStats))
	for _, stats := range em.toolStats {
		statsCopy := *stats
		statsCopy.Trend = em.calculateTrendLocked(stats.ToolName)
		allStats = append(allStats, &statsCopy)
	}

	// 按总调用次数排序
	sort.Slice(allStats, func(i, j int) bool {
		return allStats[i].TotalCalls > allStats[j].TotalCalls
	})

	if limit > 0 && limit < len(allStats) {
		return allStats[:limit]
	}

	return allStats
}

// GetFailingTools 获取失败的工具列表
func (em *EffectivenessManager) GetFailingTools() []*ToolStatistics {
	em.mu.RLock()
	defer em.mu.RUnlock()

	var failingTools []*ToolStatistics
	for _, stats := range em.toolStats {
		if stats.FailedCalls > 0 {
			statsCopy := *stats
			statsCopy.Trend = em.calculateTrendLocked(stats.ToolName)
			failingTools = append(failingTools, &statsCopy)
		}
	}

	// 按失败次数排序
	sort.Slice(failingTools, func(i, j int) bool {
		return failingTools[i].FailedCalls > failingTools[j].FailedCalls
	})

	return failingTools
}

// CalculateTrend 计算工具使用趋势
func (em *EffectivenessManager) CalculateTrend(toolName string) string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.calculateTrendLocked(toolName)
}

// 内部方法：计算趋势（需要持有读锁）
func (em *EffectivenessManager) calculateTrendLocked(toolName string) string {
	var toolRecords []*ToolEffectivenessRecord
	for _, record := range em.records {
		if record.ToolName == toolName {
			toolRecords = append(toolRecords, record)
		}
	}

	if len(toolRecords) < 10 {
		return "insufficient_data"
	}

	// 按时间排序
	sort.Slice(toolRecords, func(i, j int) bool {
		return toolRecords[i].Timestamp.Before(toolRecords[j].Timestamp)
	})

	// 分成两半计算成功率
	mid := len(toolRecords) / 2
	firstHalfSuccess := 0
	secondHalfSuccess := 0

	for i := 0; i < mid; i++ {
		if toolRecords[i].Success {
			firstHalfSuccess++
		}
	}

	for i := mid; i < len(toolRecords); i++ {
		if toolRecords[i].Success {
			secondHalfSuccess++
		}
	}

	firstHalfRate := float64(firstHalfSuccess) / float64(mid)
	secondHalfRate := float64(secondHalfSuccess) / float64(len(toolRecords)-mid)

	diff := secondHalfRate - firstHalfRate
	if diff > 0.1 {
		return "improving"
	} else if diff < -0.1 {
		return "declining"
	}
	return "stable"
}

// 更新工具统计
func (em *EffectivenessManager) updateToolStats(toolName string, success bool, duration time.Duration) {
	stats, ok := em.toolStats[toolName]
	if !ok {
		stats = &ToolStatistics{
			ToolName: toolName,
		}
		em.toolStats[toolName] = stats
	}

	stats.TotalCalls++
	if success {
		stats.SuccessCalls++
	} else {
		stats.FailedCalls++
	}

	stats.SuccessRate = float64(stats.SuccessCalls) / float64(stats.TotalCalls)

	// 更新平均耗时
	if stats.AvgDuration == 0 {
		stats.AvgDuration = int64(duration.Milliseconds())
	} else {
		oldAvg := float64(stats.AvgDuration)
		newDuration := float64(duration.Milliseconds())
		stats.AvgDuration = int64((oldAvg*float64(stats.TotalCalls-1) + newDuration) / float64(stats.TotalCalls))
	}

	stats.LastUsed = time.Now()
}

// 更新工具集统计
func (em *EffectivenessManager) updateToolsetStats(toolsetName, toolName string) {
	stats, ok := em.toolsetStats[toolsetName]
	if !ok {
		stats = &ToolsetStatistics{
			ToolsetName: toolsetName,
			ToolStats:   make(map[string]int),
		}
		em.toolsetStats[toolsetName] = stats
	}

	stats.TotalCalls++
	stats.ToolStats[toolName]++
	stats.LastUsed = time.Now()
}

// 保存数据到文件
func (em *EffectivenessManager) save() error {
	// 保存记录
	recordsData, err := json.MarshalIndent(em.records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal records: %w", err)
	}

	if err := os.WriteFile(em.recordsDB, recordsData, 0644); err != nil {
		return fmt.Errorf("failed to write records file: %w", err)
	}

	// 保存统计
	statsData := map[string]interface{}{
		"tools":    em.toolStats,
		"toolsets": em.toolsetStats,
	}

	statsJSON, err := json.MarshalIndent(statsData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	if err := os.WriteFile(em.statsDB, statsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write stats file: %w", err)
	}

	return nil
}

// 从文件加载数据
func (em *EffectivenessManager) load() error {
	// 加载记录
	recordsData, err := os.ReadFile(em.recordsDB)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(recordsData, &em.records); err != nil {
		return fmt.Errorf("failed to unmarshal records: %w", err)
	}

	// 加载统计
	statsData, err := os.ReadFile(em.statsDB)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var statsContainer struct {
		Tools    map[string]*ToolStatistics    `json:"tools"`
		Toolsets map[string]*ToolsetStatistics `json:"toolsets"`
	}

	if err := json.Unmarshal(statsData, &statsContainer); err != nil {
		return fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	if statsContainer.Tools != nil {
		em.toolStats = statsContainer.Tools
	}
	if statsContainer.Toolsets != nil {
		em.toolsetStats = statsContainer.Toolsets
	}

	return nil
}

// 生成唯一ID
func generateID() string {
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), time.Now().Unix())
}

// truncateString 已在 browser_automation.go 中定义
