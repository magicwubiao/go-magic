package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level 审计级别
type Level string

const (
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
	LevelAudit Level = "AUDIT"
)

// Category 操作类别
type Category string

const (
	CatAuth       Category = "AUTH"
	CatConfig     Category = "CONFIG"
	CatData       Category = "DATA"
	CatTool       Category = "TOOL"
	CatSkill      Category = "SKILL"
	CatSession    Category = "SESSION"
	CatPlugin     Category = "PLUGIN"
	CatSystem     Category = "SYSTEM"
	CatNetwork    Category = "NETWORK"
)

// Entry 审计条目
type Entry struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Level       Level                  `json:"level"`
	Category    Category               `json:"category"`
	Action      string                 `json:"action"`
	Actor       string                 `json:"actor"`
	Resource    string                 `json:"resource"`
	Result      string                 `json:"result"` // success, failure, partial
	StatusCode  int                    `json:"status_code,omitempty"`
	Error       string                 `json:"error,omitempty"`
	IPAddress   string                 `json:"ip_address,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	Duration    time.Duration          `json:"duration,omitempty"`
}

// Query 查询条件
type Query struct {
	StartTime   time.Time
	EndTime     time.Time
	Level       Level
	Category    Category
	Action      string
	Actor       string
	Resource    string
	Result      string
	Limit       int
	Offset      int
}

// Manager 审计管理器
type Manager struct {
	mu        sync.RWMutex
	dataDir   string
	entries   []*Entry
	maxSize   int // 最大内存条目数
	flushTick *time.Ticker
	stopChan  chan struct{}
}

// NewManager creates audit manager
func NewManager(dataDir string) (*Manager, error) {
	// Use default dir if empty
	if dataDir == "" {
		dataDir = filepath.Join(getMagicHomeDir(), "audit")
	}

	m := &Manager{
		dataDir: dataDir,
		entries: make([]*Entry, 0),
		maxSize: 10000,
		stopChan: make(chan struct{}),
	}

	// Create directory
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	// Load history
	if err := m.load(); err != nil {
		// Ignore load errors
	}

	// 启动定期刷新
	m.flushTick = time.NewTicker(5 * time.Minute)
	go m.autoFlush()

	return m, nil
}

// Close 关闭管理器
func (m *Manager) Close() error {
	close(m.stopChan)
	m.flushTick.Stop()
	return m.flush()
}

// autoFlush 自动刷新
func (m *Manager) autoFlush() {
	for {
		select {
		case <-m.flushTick.C:
			m.flush()
		case <-m.stopChan:
			return
		}
	}
}

// Log 记录审计条目
func (m *Manager) Log(level Level, category Category, action, actor, resource, result string, opts ...Option) *Entry {
	entry := &Entry{
		ID:        generateID(),
		Timestamp: time.Now(),
		Level:     level,
		Category:  category,
		Action:    action,
		Actor:     actor,
		Resource:  resource,
		Result:    result,
	}

	// 应用选项
	for _, opt := range opts {
		opt(entry)
	}

	// 添加到内存
	m.mu.Lock()
	m.entries = append(m.entries, entry)

	// 限制内存大小
	if len(m.entries) > m.maxSize {
		// 删除最老的条目
		m.entries = m.entries[len(m.entries)-m.maxSize:]
	}
	m.mu.Unlock()

	// 异步写入文件
	go m.appendToFile(entry)

	return entry
}

// Option 审计条目选项
type Option func(*Entry)

// WithError 设置错误
func WithError(err error) Option {
	return func(e *Entry) {
		if err != nil {
			e.Error = err.Error()
			e.Level = LevelError
		}
	}
}

// WithMetadata 设置元数据
func WithMetadata(metadata map[string]interface{}) Option {
	return func(e *Entry) {
		e.Metadata = metadata
	}
}

// WithIP 设置 IP 地址
func WithIP(ip string) Option {
	return func(e *Entry) {
		e.IPAddress = ip
	}
}

// WithSession 设置会话 ID
func WithSession(sessionID string) Option {
	return func(e *Entry) {
		e.SessionID = sessionID
	}
}

// WithDuration 设置持续时间
func WithDuration(d time.Duration) Option {
	return func(e *Entry) {
		e.Duration = d
	}
}

// WithStatusCode 设置状态码
func WithStatusCode(code int) Option {
	return func(e *Entry) {
		e.StatusCode = code
	}
}

// Query 查询审计记录
func (m *Manager) Query(q *Query) ([]*Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*Entry

	for _, entry := range m.entries {
		// 时间过滤
		if !q.StartTime.IsZero() && entry.Timestamp.Before(q.StartTime) {
			continue
		}
		if !q.EndTime.IsZero() && entry.Timestamp.After(q.EndTime) {
			continue
		}

		// 级别过滤
		if q.Level != "" && entry.Level != q.Level {
			continue
		}

		// 类别过滤
		if q.Category != "" && entry.Category != q.Category {
			continue
		}

		// 动作过滤
		if q.Action != "" && entry.Action != q.Action {
			continue
		}

		// 参与者过滤
		if q.Actor != "" && entry.Actor != q.Actor {
			continue
		}

		// 资源过滤
		if q.Resource != "" && entry.Resource != q.Resource {
			continue
		}

		// 结果过滤
		if q.Result != "" && entry.Result != q.Result {
			continue
		}

		results = append(results, entry)
	}

	// 分页
	if q.Limit > 0 {
		start := q.Offset
		if start >= len(results) {
			return []*Entry{}, nil
		}
		end := start + q.Limit
		if end > len(results) {
			end = len(results)
		}
		results = results[start:end]
	}

	return results, nil
}

// GetByID 根据 ID 获取条目
func (m *Manager) GetByID(id string) *Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entry := range m.entries {
		if entry.ID == id {
			return entry
		}
	}
	return nil
}

// GetRecent 获取最近的条目
func (m *Manager) GetRecent(limit int) []*Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit > len(m.entries) {
		limit = len(m.entries)
	}

	result := make([]*Entry, limit)
	copy(result, m.entries[len(m.entries)-limit:])
	return result
}

// GetStats 获取统计信息
func (m *Manager) GetStats(start, end time.Time) (*Stats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &Stats{
		ByLevel:   make(map[Level]int),
		ByCategory: make(map[Category]int),
		ByResult:  make(map[string]int),
	}

	for _, entry := range m.entries {
		if !start.IsZero() && entry.Timestamp.Before(start) {
			continue
		}
		if !end.IsZero() && entry.Timestamp.After(end) {
			continue
		}

		stats.Total++
		stats.ByLevel[entry.Level]++
		stats.ByCategory[entry.Category]++
		stats.ByResult[entry.Result]++

		if entry.Level == LevelError {
			stats.Errors++
		}
	}

	return stats, nil
}

// Stats 统计信息
type Stats struct {
	Total      int              `json:"total"`
	Errors     int              `json:"errors"`
	ByLevel    map[Level]int    `json:"by_level"`
	ByCategory map[Category]int `json:"by_category"`
	ByResult   map[string]int   `json:"by_result"`
}

// Export 导出审计记录
func (m *Manager) Export(start, end time.Time, format string) ([]byte, error) {
	entries, err := m.Query(&Query{
		StartTime: start,
		EndTime:   end,
		Limit:     0, // 获取所有
	})
	if err != nil {
		return nil, err
	}

	switch format {
	case "json":
		return json.MarshalIndent(entries, "", "  ")
	case "csv":
		return m.exportCSV(entries)
	default:
		return m.exportCSV(entries)
	}
}

// exportCSV 导出 CSV 格式
func (m *Manager) exportCSV(entries []*Entry) ([]byte, error) {
	var lines []string
	lines = append(lines, "ID,Timestamp,Level,Category,Action,Actor,Resource,Result,Error,SessionID")

	for _, e := range entries {
		line := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s",
			e.ID,
			e.Timestamp.Format(time.RFC3339),
			e.Level,
			e.Category,
			e.Action,
			e.Actor,
			e.Resource,
			e.Result,
			e.Error,
			e.SessionID,
		)
		lines = append(lines, line)
	}

	var result []byte
	for _, line := range lines {
		result = append(result, []byte(line+"\n")...)
	}
	return result, nil
}

// flush 刷新到磁盘
func (m *Manager) flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 写入所有条目到文件
	path := filepath.Join(m.dataDir, "audit.json")
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// appendToFile 追加到文件
func (m *Manager) appendToFile(entry *Entry) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := filepath.Join(m.dataDir, "audit.json")

	// 读取现有数据
	var entries []*Entry
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &entries)
	}

	// 添加新条目
	entries = append(entries, entry)

	// 限制文件大小（保留最近 100000 条）
	if len(entries) > 100000 {
		entries = entries[len(entries)-100000:]
	}

	// 写入
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// load 加载历史记录
func (m *Manager) load() error {
	path := filepath.Join(m.dataDir, "audit.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &m.entries)
}

// generateID 生成唯一 ID
func generateID() string {
	return fmt.Sprintf("aud_%d", time.Now().UnixNano())
}

// 便捷方法

// LogAuth 记录认证事件
func (m *Manager) LogAuth(action, actor, result string, err error) *Entry {
	return m.Log(LevelAudit, CatAuth, action, actor, "auth", result, WithError(err))
}

// LogConfig 记录配置变更
func (m *Manager) LogConfig(action, actor, resource, result string) *Entry {
	return m.Log(LevelInfo, CatConfig, action, actor, resource, result)
}

// LogData 记录数据操作
func (m *Manager) LogData(action, actor, resource, result string, metadata map[string]interface{}) *Entry {
	return m.Log(LevelInfo, CatData, action, actor, resource, result, WithMetadata(metadata))
}

// LogTool 记录工具执行
func (m *Manager) LogTool(name, result string, duration time.Duration, err error) *Entry {
	return m.Log(LevelInfo, CatTool, "execute", "user", name, result, WithDuration(duration), WithError(err))
}

// LogSkill 记录技能操作
func (m *Manager) LogSkill(action, name, result string) *Entry {
	return m.Log(LevelInfo, CatSkill, action, "user", name, result)
}

// LogSession 记录会话事件
func (m *Manager) LogSession(action, sessionID, result string) *Entry {
	return m.Log(LevelInfo, CatSession, action, "user", sessionID, result, WithSession(sessionID))
}

// LogPlugin 记录插件事件
func (m *Manager) LogPlugin(action, name, result string, err error) *Entry {
	return m.Log(LevelInfo, CatPlugin, action, "user", name, result, WithError(err))
}

// LogSystem 记录系统事件
func (m *Manager) LogSystem(action, result string, metadata map[string]interface{}) *Entry {
	return m.Log(LevelInfo, CatSystem, action, "system", "system", result, WithMetadata(metadata))
}

// LogError 记录错误
func (m *Manager) LogError(category Category, action, resource, errMsg string) *Entry {
	return m.Log(LevelError, category, action, "system", resource, "failure", WithError(fmt.Errorf("%s", errMsg)))
}

// getMagicHomeDir returns the magic home directory
func getMagicHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".go-magic")
}
