package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelPricing 定义各模型的定价
var ModelPricing = map[string]ModelPrice{
	// OpenAI
	"gpt-4o":        {Input: 5.00, Output: 15.00, Unit: "1M tokens"},
	"gpt-4o-mini":   {Input: 0.15, Output: 0.60, Unit: "1M tokens"},
	"gpt-4-turbo":   {Input: 10.00, Output: 30.00, Unit: "1M tokens"},
	"gpt-4":         {Input: 30.00, Output: 60.00, Unit: "1M tokens"},
	"gpt-3.5-turbo": {Input: 0.50, Output: 1.50, Unit: "1M tokens"},
	// Anthropic
	"claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00, Unit: "1M tokens"},
	"claude-3-5-haiku-20241022":  {Input: 0.80, Output: 4.00, Unit: "1M tokens"},
	"claude-3-opus-20240229":     {Input: 15.00, Output: 75.00, Unit: "1M tokens"},
	"claude-3-sonnet-20240229":   {Input: 3.00, Output: 15.00, Unit: "1M tokens"},
	// DeepSeek
	"deepseek-chat":  {Input: 0.27, Output: 1.10, Unit: "1M tokens"},
	"deepseek-coder": {Input: 0.27, Output: 1.10, Unit: "1M tokens"},
	// Ollama (本地，免费)
	"llama3.2":  {Input: 0, Output: 0, Unit: "local"},
	"mistral":   {Input: 0, Output: 0, Unit: "local"},
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
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Cost         float64   `json:"cost"`
	SessionID    string    `json:"session_id,omitempty"`
	RequestType  string    `json:"request_type,omitempty"` // chat, completion, embedding, etc.
}

// DailyStats 每日统计
type DailyStats struct {
	Date          string                `json:"date"`
	TotalRequests int                   `json:"total_requests"`
	TotalInput    int                   `json:"total_input_tokens"`
	TotalOutput   int                   `json:"total_output_tokens"`
	TotalCost     float64               `json:"total_cost"`
	ByModel       map[string]ModelStats `json:"by_model"`
}

// ModelStats 模型统计
type ModelStats struct {
	Requests     int     `json:"requests"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// MonthlyBudget 月度预算
type MonthlyBudget struct {
	Month          string  `json:"month"`
	Limit          float64 `json:"limit"`           // 预算上限（美元）
	Current        float64 `json:"current"`         // 当前花费
	AlertThreshold float64 `json:"alert_threshold"` // 告警阈值（百分比）
}

// Manager 使用量管理器
type Manager struct {
	mu         sync.RWMutex
	dataDir    string
	records    []UsageRecord
	dailyStats map[string]*DailyStats
	budget     *MonthlyBudget
	lastSave   time.Time
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
		Month:          month,
		Limit:          100.0, // 默认 $100
		Current:        0,
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

	// Always save after recording
	m.save()

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
		Date:    fmt.Sprintf("%s to %s", startOfWeek.Format("2006-01-02"), now.Format("2006-01-02")),
		ByModel: make(map[string]ModelStats),
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
		Date:    month,
		ByModel: make(map[string]ModelStats),
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
			Month:          time.Now().Format("2006-01"),
			Limit:          100.0,
			Current:        0,
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

	// 确保预算已初始化
	if m.budget == nil || m.budget.Month != time.Now().Format("2006-01") {
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
		"records":     m.records,
		"daily_stats": m.dailyStats,
		"budget":      m.budget,
	}

	return json.MarshalIndent(data, "", "  ")
}

// Insights provides detailed usage analysis
type Insights struct {
	Period          string             `json:"period"`
	TotalRequests   int                `json:"total_requests"`
	TotalTokens     int                `json:"total_tokens"`
	TotalCost       float64            `json:"total_cost"`
	AvgTokensPerReq float64            `json:"avg_tokens_per_request"`
	AvgCostPerReq   float64            `json:"avg_cost_per_request"`
	TopModels       []ModelUsage       `json:"top_models"`
	TopProviders    []ProviderUsage    `json:"top_providers"`
	DailyTrend      []DailyTrend       `json:"daily_trend"`
	PeakHours       []HourlyUsage      `json:"peak_hours"`
	CostBreakdown   map[string]float64 `json:"cost_breakdown"`
	Recommendations []string           `json:"recommendations"`
	GeneratedAt     int64              `json:"generated_at"`
}

// ModelUsage represents usage by model
type ModelUsage struct {
	Model      string  `json:"model"`
	Requests   int     `json:"requests"`
	Tokens     int     `json:"tokens"`
	Cost       float64 `json:"cost"`
	Percentage float64 `json:"percentage"`
}

// ProviderUsage represents usage by provider
type ProviderUsage struct {
	Provider   string  `json:"provider"`
	Requests   int     `json:"requests"`
	Tokens     int     `json:"tokens"`
	Cost       float64 `json:"cost"`
	Percentage float64 `json:"percentage"`
}

// DailyTrend represents daily usage trend
type DailyTrend struct {
	Date     string  `json:"date"`
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// HourlyUsage represents hourly usage pattern
type HourlyUsage struct {
	Hour     int `json:"hour"`
	Requests int `json:"requests"`
	Tokens   int `json:"tokens"`
}

// GetInsights generates detailed usage insights for the specified period
func (m *Manager) GetInsights(days int) (*Insights, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	insights := &Insights{
		CostBreakdown:   make(map[string]float64),
		Recommendations: make([]string, 0),
		GeneratedAt:     time.Now().Unix(),
	}

	// Calculate period
	now := time.Now()
	startDate := now.AddDate(0, 0, -days)
	insights.Period = fmt.Sprintf("Last %d days", days)

	// Aggregate data
	var totalTokens int
	modelUsage := make(map[string]*ModelUsage)
	providerUsage := make(map[string]*ProviderUsage)
	hourlyUsage := make(map[int]*HourlyUsage)
	dailyTrend := make(map[string]*DailyTrend)

	for _, record := range m.records {
		if record.Timestamp.Before(startDate) {
			continue
		}

		insights.TotalRequests++
		tokens := record.InputTokens + record.OutputTokens
		totalTokens += tokens
		insights.TotalCost += record.Cost

		// Model usage
		if mu, ok := modelUsage[record.Model]; ok {
			mu.Requests++
			mu.Tokens += tokens
			mu.Cost += record.Cost
		} else {
			modelUsage[record.Model] = &ModelUsage{
				Model:    record.Model,
				Requests: 1,
				Tokens:   tokens,
				Cost:     record.Cost,
			}
		}

		// Provider usage
		if pu, ok := providerUsage[record.Provider]; ok {
			pu.Requests++
			pu.Tokens += tokens
			pu.Cost += record.Cost
		} else {
			providerUsage[record.Provider] = &ProviderUsage{
				Provider: record.Provider,
				Requests: 1,
				Tokens:   tokens,
				Cost:     record.Cost,
			}
		}

		// Hourly usage
		hour := record.Timestamp.Hour()
		if hu, ok := hourlyUsage[hour]; ok {
			hu.Requests++
			hu.Tokens += tokens
		} else {
			hourlyUsage[hour] = &HourlyUsage{
				Hour:     hour,
				Requests: 1,
				Tokens:   tokens,
			}
		}

		// Daily trend
		date := record.Timestamp.Format("2006-01-02")
		if dt, ok := dailyTrend[date]; ok {
			dt.Requests++
			dt.Tokens += tokens
			dt.Cost += record.Cost
		} else {
			dailyTrend[date] = &DailyTrend{
				Date:     date,
				Requests: 1,
				Tokens:   tokens,
				Cost:     record.Cost,
			}
		}

		// Cost breakdown by provider
		insights.CostBreakdown[record.Provider] += record.Cost
	}

	// Calculate averages
	if insights.TotalRequests > 0 {
		insights.AvgTokensPerReq = float64(totalTokens) / float64(insights.TotalRequests)
		insights.AvgCostPerReq = insights.TotalCost / float64(insights.TotalRequests)
	}
	insights.TotalTokens = totalTokens

	// Top models
	for _, mu := range modelUsage {
		mu.Percentage = float64(mu.Tokens) / float64(totalTokens) * 100
		insights.TopModels = append(insights.TopModels, *mu)
	}
	sort.Slice(insights.TopModels, func(i, j int) bool {
		return insights.TopModels[i].Tokens > insights.TopModels[j].Tokens
	})
	if len(insights.TopModels) > 5 {
		insights.TopModels = insights.TopModels[:5]
	}

	// Top providers
	totalProviderTokens := 0
	for _, pu := range providerUsage {
		totalProviderTokens += pu.Tokens
	}
	for _, pu := range providerUsage {
		if totalProviderTokens > 0 {
			pu.Percentage = float64(pu.Tokens) / float64(totalProviderTokens) * 100
		}
		insights.TopProviders = append(insights.TopProviders, *pu)
	}
	sort.Slice(insights.TopProviders, func(i, j int) bool {
		return insights.TopProviders[i].Tokens > insights.TopProviders[j].Tokens
	})

	// Daily trend
	for _, dt := range dailyTrend {
		insights.DailyTrend = append(insights.DailyTrend, *dt)
	}
	sort.Slice(insights.DailyTrend, func(i, j int) bool {
		return insights.DailyTrend[i].Date < insights.DailyTrend[j].Date
	})

	// Peak hours
	for _, hu := range hourlyUsage {
		insights.PeakHours = append(insights.PeakHours, *hu)
	}
	sort.Slice(insights.PeakHours, func(i, j int) bool {
		return insights.PeakHours[i].Requests > insights.PeakHours[j].Requests
	})

	// Generate recommendations
	insights.Recommendations = m.generateRecommendations(insights)

	return insights, nil
}

// generateRecommendations generates cost-saving recommendations
func (m *Manager) generateRecommendations(insights *Insights) []string {
	var recs []string

	// Check for expensive models
	if len(insights.TopModels) > 0 {
		topModel := insights.TopModels[0]
		if topModel.Cost > 50 && topModel.Model != "" {
			recs = append(recs, fmt.Sprintf("Consider using a smaller model like gpt-4o-mini for simpler tasks (currently using %s)", topModel.Model))
		}
	}

	// Check for high request count
	if insights.TotalRequests > 1000 {
		recs = append(recs, "High request volume detected. Consider implementing request batching for efficiency.")
	}

	// Check for unused providers
	if len(insights.TopProviders) > 1 {
		recs = append(recs, "Multiple providers in use. Standardizing on one provider may reduce complexity.")
	}

	// Check peak usage
	if len(insights.PeakHours) > 0 {
		topHour := insights.PeakHours[0]
		if topHour.Requests > insights.TotalRequests/2 {
			recs = append(recs, fmt.Sprintf("Usage is concentrated around hour %d. Consider scheduling heavy tasks during off-peak hours.", topHour.Hour))
		}
	}

	// Check average cost
	if insights.AvgCostPerReq > 0.10 {
		recs = append(recs, "Average cost per request is above typical. Review request complexity and model selection.")
	}

	// Budget warning
	if m.budget != nil && m.budget.Current > m.budget.Limit*0.8 {
		recs = append(recs, fmt.Sprintf("Monthly budget is %.0f%% used. Consider optimizing usage.", m.budget.Current/m.budget.Limit*100))
	}

	if len(recs) == 0 {
		recs = append(recs, "Usage looks healthy. No major optimizations needed.")
	}

	return recs
}

// GetInsightsJSON generates insights and returns as JSON string
func (m *Manager) GetInsightsJSON(days int) (string, error) {
	insights, err := m.GetInsights(days)
	if err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(insights, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// FormatInsightsText formats insights as human-readable text
func (m *Manager) FormatInsightsText(days int) (string, error) {
	insights, err := m.GetInsights(days)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("## Usage Insights (%s)\n\n", insights.Period))
	b.WriteString(fmt.Sprintf("**Total Requests:** %d\n", insights.TotalRequests))
	b.WriteString(fmt.Sprintf("**Total Tokens:** %s\n", formatNumber(insights.TotalTokens)))
	b.WriteString(fmt.Sprintf("**Total Cost:** $%.4f\n", insights.TotalCost))
	b.WriteString(fmt.Sprintf("**Avg Tokens/Request:** %.0f\n", insights.AvgTokensPerReq))
	b.WriteString(fmt.Sprintf("**Avg Cost/Request:** $%.4f\n\n", insights.AvgCostPerReq))

	if len(insights.TopModels) > 0 {
		b.WriteString("### Top Models\n\n")
		for i, mu := range insights.TopModels {
			b.WriteString(fmt.Sprintf("%d. %s - %d tokens (%.1f%%, $%.4f)\n",
				i+1, mu.Model, mu.Tokens, mu.Percentage, mu.Cost))
		}
		b.WriteString("\n")
	}

	if len(insights.TopProviders) > 0 {
		b.WriteString("### Top Providers\n\n")
		for i, pu := range insights.TopProviders {
			b.WriteString(fmt.Sprintf("%d. %s - %d tokens (%.1f%%, $%.4f)\n",
				i+1, pu.Provider, pu.Tokens, pu.Percentage, pu.Cost))
		}
		b.WriteString("\n")
	}

	if len(insights.Recommendations) > 0 {
		b.WriteString("### Recommendations\n\n")
		for i, rec := range insights.Recommendations {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
		}
	}

	return b.String(), nil
}

// formatNumber formats large numbers with commas
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
