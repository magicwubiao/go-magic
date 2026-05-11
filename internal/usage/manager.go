package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ModelPricing 定义各模型的定价
var ModelPricing = map[string]ModelPrice{
	// OpenAI
	"gpt-4o":           {Input: 5.00, Output: 15.00, Unit: "1M tokens"},
	"gpt-4o-mini":      {Input: 0.15, Output: 0.60, Unit: "1M tokens"},
	"gpt-4-turbo":      {Input: 10.00, Output: 30.00, Unit: "1M tokens"},
	"gpt-4":            {Input: 30.00, Output: 60.00, Unit: "1M tokens"},
	"gpt-3.5-turbo":    {Input: 0.50, Output: 1.50, Unit: "1M tokens"},
	// Anthropic
	"claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00, Unit: "1M tokens"},
	"claude-3-5-haiku-20241022": {Input: 0.80, Output: 4.00, Unit: "1M tokens"},
	"claude-3-opus-20240229":    {Input: 15.00, Output: 75.00, Unit: "1M tokens"},
	"claude-3-sonnet-20240229":  {Input: 3.00, Output: 15.00, Unit: "1M tokens"},
	// DeepSeek
	"deepseek-chat":  {Input: 0.27, Output: 1.10, Unit: "1M tokens"},
	"deepseek-coder": {Input: 0.27, Output: 1.10, Unit: "1M tokens"},
	// Ollama (本地，免费)
	"llama3.2": {Input: 0, Output: 0, Unit: "local"},
	"mistral":  {Input: 0, Output: 0, Unit: "local"},
	"codellama": {Input: 0, Output: 0, Unit: "local"},
	// OpenRouter
	"openrouter/anthropic/claude-3.5-sonnet": {Input: 3.00, Output: 15.00, Unit: "1M tokens"},
	"openrouter/openai/gpt-4o":               {Input: 5.00, Output: 15.00, Unit: "1M tokens"},
	"openrouter/deepseek/deepseek-chat-v3":   {Input: 0.27, Output: 1.10, Unit: "1M tokens"},
}

// ModelPrice 模型定价结构
type ModelPrice struct {
	Input  float64 // 每百万输入 token 成本
	Output float64 // 每百万输出 token 成本
	Unit   string  // 计量单位
}

// UsageRecord 单次使用记录
type UsageRecord struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Model       string    `json:"model"`
	Provider    string    `json:"provider"`
	InputTokens int       `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	Cost        float64   `json:"cost"`
	SessionID   string    `json:"session_id,omitempty"`
	RequestType string    `json:"request_type,omitempty"` // chat, completion, embedding, etc.
}

// DailyStats 每日统计
type DailyStats struct {
	Date           string  `json:"date"`
	TotalRequests  int     `json:"total_requests"`
	TotalInput     int     `json:"total_input_tokens"`
	TotalOutput    int     `json:"total_output_tokens"`
	TotalCost      float64 `json:"total_cost"`
	ByModel        map[string]ModelStats `json:"by_model"`
}

// ModelStats 模型统计
type ModelStats struct {
	Requests    int     `json:"requests"`
	InputTokens int     `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Cost        float64 `json:"cost"`
}

// MonthlyBudget 月度预算
type MonthlyBudget struct {
	Month      string  `json:"month"`
	Limit      float64 `json:"limit"`       // 预算上限（美元）
	Current    float64 `json:"current"`     // 当前花费
	AlertThreshold float64 `json:"alert_threshold"` // 告警阈值（百分比）
}

// Manager 使用量管理器
type Manager struct {
	mu            sync.RWMutex
	dataDir       string
	records       []UsageRecord
	dailyStats    map[string]*DailyStats
	budget        *MonthlyBudget
	lastSave      time.Time
}

// NewManager 创建新的使用量管理器
func NewManager(dataDir string) (*Manager, error) {
	m := &Manager{
		dataDir:    dataDir,
		records:    make([]UsageRecord, 0),
		dailyStats: make(map[string]*DailyStats),
	}

	// 创建数据目录
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	// 加载历史数据
	if err := m.load(); err != nil {
		// 如果加载失败，从头开始
		m.initCurrentMonth()
	}

	return m, nil
}

// initCurrentMonth 初始化当月预算
func (m *Manager) initCurrentMonth() {
	now := time.Now()
	month := now.Format("2006-01")
	m.budget = &MonthlyBudget{
		Month:         month,
		Limit:         100.0, // 默认 $100
		Current:       0,
		AlertThreshold: 0.8, // 80%
	}
}

// Record 记录一次使用
func (m *Manager) Record(inputTokens, outputTokens int, model, provider, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 计算成本
	cost := m.calculateCost(inputTokens, outputTokens, model)

	record := UsageRecord{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:    time.Now(),
		Model:        model,
		Provider:     provider,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		SessionID:    sessionID,
		RequestType:  "chat",
	}

	m.records = append(m.records, record)
	m.updateDailyStats(&record)
	m.updateMonthlyBudget(cost)

	// 每 10 条记录保存一次
	if len(m.records) >= 10 || time.Since(m.lastSave) > time.Minute {
		m.save()
	}

	return nil
}

// calculateCost 计算成本
func (m *Manager) calculateCost(inputTokens, outputTokens int, model string) float64 {
	// 查找模型定价
	price, ok := ModelPricing[model]
	if !ok {
		// 尝试模糊匹配
		for key, p := range ModelPricing {
			if contains(key, model) || contains(model, key) {
				price = p
				break
			}
		}
	}

	// 本地模型免费
	if price.Unit == "local" {
		return 0
	}

	inputCost := float64(inputTokens) / 1_000_000 * price.Input
	outputCost := float64(outputTokens) / 1_000_000 * price.Output
	return inputCost + outputCost
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr))
}

// updateDailyStats 更新每日统计
func (m *Manager) updateDailyStats(record *UsageRecord) {
	date := record.Timestamp.Format("2006-01-02")
	stats, ok := m.dailyStats[date]
	if !ok {
		stats = &DailyStats{
			Date:    date,
			ByModel: make(map[string]ModelStats),
		}
		m.dailyStats[date] = stats
	}

	stats.TotalRequests++
	stats.TotalInput += record.InputTokens
	stats.TotalOutput += record.OutputTokens
	stats.TotalCost += record.Cost

	// 按模型统计
	modelStats := stats.ByModel[record.Model]
	modelStats.Requests++
	modelStats.InputTokens += record.InputTokens
	modelStats.OutputTokens += record.OutputTokens
	modelStats.Cost += record.Cost
	stats.ByModel[record.Model] = modelStats
}

// updateMonthlyBudget 更新月度预算
func (m *Manager) updateMonthlyBudget(cost float64) {
	now := time.Now()
	month := now.Format("2006-01")

	if m.budget == nil || m.budget.Month != month {
		m.initCurrentMonth()
		m.budget.Month = month
	}

	m.budget.Current += cost
}

// GetDailyStats 获取指定日期的统计
func (m *Manager) GetDailyStats(date string) (*DailyStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, ok := m.dailyStats[date]
	if !ok {
		return &DailyStats{Date: date, ByModel: make(map[string]ModelStats)}, nil
	}

	// 返回副本
	result := *stats
	result.ByModel = make(map[string]ModelStats)
	for k, v := range stats.ByModel {
		result.ByModel[k] = v
	}
	return &result, nil
}

// GetTodayStats 获取今日统计
func (m *Manager) GetTodayStats() (*DailyStats, error) {
	return m.GetDailyStats(time.Now().Format("2006-01-02"))
}

// GetWeeklyStats 获取本周统计
func (m *Manager) GetWeeklyStats() (*DailyStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()))

	result := &DailyStats{
		Date:     fmt.Sprintf("%s to %s", startOfWeek.Format("2006-01-02"), now.Format("2006-01-02")),
		ByModel:  make(map[string]ModelStats),
	}

	for date, stats := range m.dailyStats {
		t, _ := time.Parse("2006-01-02", date)
		if t.After(startOfWeek) || t.Equal(startOfWeek) {
			result.TotalRequests += stats.TotalRequests
			result.TotalInput += stats.TotalInput
			result.TotalOutput += stats.TotalOutput
			result.TotalCost += stats.TotalCost

			for model, ms := range stats.ByModel {
				combined := result.ByModel[model]
				combined.Requests += ms.Requests
				combined.InputTokens += ms.InputTokens
				combined.OutputTokens += ms.OutputTokens
				combined.Cost += ms.Cost
				result.ByModel[model] = combined
			}
		}
	}

	return result, nil
}

// GetMonthlyStats 获取本月统计
func (m *Manager) GetMonthlyStats() (*DailyStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	month := now.Format("2006-01")

	result := &DailyStats{
		Date:     month,
		ByModel:  make(map[string]ModelStats),
	}

	for date, stats := range m.dailyStats {
		if len(date) >= 7 && date[:7] == month {
			result.TotalRequests += stats.TotalRequests
			result.TotalInput += stats.TotalInput
			result.TotalOutput += stats.TotalOutput
			result.TotalCost += stats.TotalCost

			for model, ms := range stats.ByModel {
				combined := result.ByModel[model]
				combined.Requests += ms.Requests
				combined.InputTokens += ms.InputTokens
				combined.OutputTokens += ms.OutputTokens
				combined.Cost += ms.Cost
				result.ByModel[model] = combined
			}
		}
	}

	return result, nil
}

// GetBudget 获取月度预算状态
func (m *Manager) GetBudget() *MonthlyBudget {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.budget == nil {
		return &MonthlyBudget{
			Month:         time.Now().Format("2006-01"),
			Limit:         100.0,
			Current:       0,
			AlertThreshold: 0.8,
		}
	}

	return m.budget
}

// SetBudget 设置月度预算
func (m *Manager) SetBudget(limit, alertThreshold float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	month := time.Now().Format("2006-01")
	m.budget = &MonthlyBudget{
		Month:          month,
		Limit:          limit,
		Current:        m.budget.Current,
		AlertThreshold: alertThreshold,
	}

	return m.saveBudget()
}

// IsBudgetExceeded 检查是否超过预算
func (m *Manager) IsBudgetExceeded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.budget == nil || m.budget.Limit <= 0 {
		return false
	}

	return m.budget.Current >= m.budget.Limit
}

// IsBudgetWarning 检查是否需要告警
func (m *Manager) IsBudgetWarning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.budget == nil || m.budget.Limit <= 0 {
		return false
	}

	ratio := m.budget.Current / m.budget.Limit
	return ratio >= m.budget.AlertThreshold
}

// save 保存数据
func (m *Manager) save() error {
	m.lastSave = time.Now()

	// 保存记录
	recordsPath := filepath.Join(m.dataDir, "records.json")
	data, err := json.MarshalIndent(m.records, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(recordsPath, data, 0644); err != nil {
		return err
	}

	// 保存每日统计
	statsPath := filepath.Join(m.dataDir, "daily_stats.json")
	data, err = json.MarshalIndent(m.dailyStats, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statsPath, data, 0644)
}

// saveBudget 保存预算
func (m *Manager) saveBudget() error {
	if m.budget == nil {
		return nil
	}

	budgetPath := filepath.Join(m.dataDir, "budget.json")
	data, err := json.MarshalIndent(m.budget, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(budgetPath, data, 0644)
}

// load 加载数据
func (m *Manager) load() error {
	// 加载记录
	recordsPath := filepath.Join(m.dataDir, "records.json")
	if data, err := os.ReadFile(recordsPath); err == nil {
		json.Unmarshal(data, &m.records)
	}

	// 加载每日统计
	statsPath := filepath.Join(m.dataDir, "daily_stats.json")
	if data, err := os.ReadFile(statsPath); err == nil {
		json.Unmarshal(data, &m.dailyStats)
	}

	// 加载预算
	budgetPath := filepath.Join(m.dataDir, "budget.json")
	if data, err := os.ReadFile(budgetPath); err == nil {
		json.Unmarshal(data, &m.budget)
	}

	// 检查是否是当月
	if m.budget != nil && m.budget.Month != time.Now().Format("2006-01") {
		m.initCurrentMonth()
	}

	return nil
}

// GetAllRecords 获取所有记录（可分页）
func (m *Manager) GetAllRecords(limit, offset int) []UsageRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if offset >= len(m.records) {
		return []UsageRecord{}
	}

	end := offset + limit
	if end > len(m.records) {
		end = len(m.records)
	}

	// 返回副本
	result := make([]UsageRecord, end-offset)
	copy(result, m.records[offset:end])
	return result
}

// ExportJSON 导出为 JSON
func (m *Manager) ExportJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data := map[string]interface{}{
		"records":      m.records,
		"daily_stats":  m.dailyStats,
		"budget":       m.budget,
	}

	return json.MarshalIndent(data, "", "  ")
}
