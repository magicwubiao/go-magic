// Package memory provides a persistent memory system with FTS5 full-text search
// inspired by Cortex Agent's memory architecture
package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Memory types
type MemoryType string

const (
	TypeAgent      MemoryType = "agent"      // Agent's own notes
	TypeUser       MemoryType = "user"       // User profile and preferences
	TypeSession    MemoryType = "session"    // Session-specific information
	TypeProject    MemoryType = "project"    // Project-related memories
	TypeKnowledge  MemoryType = "knowledge"  // General knowledge
	TypePreference MemoryType = "preference" // User preferences
)

// Memory represents a single memory entry
type Memory struct {
	ID          string     `json:"id"`
	Type        MemoryType `json:"type"`
	Content     string     `json:"content"`
	Scope       string     `json:"scope,omitempty"`      // Hierarchical path like /infrastructure/database
	Categories  []string   `json:"categories,omitempty"` // Tags
	Importance  float64    `json:"importance"`           // 0.0 - 1.0
	Metadata    string     `json:"metadata,omitempty"`   // JSON metadata
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastAccess  time.Time  `json:"last_access"`
	AccessCount int        `json:"access_count"`
	SessionID   string     `json:"session_id,omitempty"` // Associated session
	Source      string     `json:"source,omitempty"`     // How it was created
}

// MemoryConfig holds configuration for the memory system
type MemoryConfig struct {
	DBPath             string
	MaxContentLength   int    // Max characters per memory
	MaxAgentMemLength  int    // Max characters for agent memory file
	MaxUserMemLength   int    // Max characters for user memory file
	AutoSummarize      bool   // Enable automatic summarization
	SummarizeThreshold int    // Threshold for summarization (characters)
	LLMProvider        string // LLM provider for summarization
}

// DefaultConfig returns the default memory configuration
func DefaultConfig() *MemoryConfig {
	home := config.GetMagicHome()
	return &MemoryConfig{
		DBPath:             filepath.Join(home, "memories", "memory.db"),
		MaxContentLength:   5000,
		MaxAgentMemLength:  2200,
		MaxUserMemLength:   1375,
		AutoSummarize:      true,
		SummarizeThreshold: 3000,
		LLMProvider:        "openai",
	}
}

// Store manages the persistent memory system
type Store struct {
	db     *sql.DB
	config *MemoryConfig
	mu     sync.RWMutex

	// File-based memory paths (Cortex style)
	agentMemoryPath string
	userMemoryPath  string

	// fileMu 串行化 MEMORY.md / USER.md 的文件级读写，防止跨 goroutine append 互相覆盖
	fileMu sync.Mutex

	// llmProvider 允许外部注入用于 Summarize 的 provider（SetLLMProvider），
	// 避免依赖环境变量探测
	llmProvider atomic.Value // stores provider.Provider

	// totalSearches 统计搜索次数（MemoryStats.TotalSearches 数据源）
	totalSearches atomic.Int64
}

// NewStore creates a new memory store
func NewStore(memCfg *MemoryConfig) (*Store, error) {
	if memCfg == nil {
		memCfg = DefaultConfig()
	}

	// Ensure directory exists
	dir := filepath.Dir(memCfg.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	// Initialize SQLite database.
	// 启用 WAL + busy_timeout：多 goroutine / 多进程并发写入时避免 SQLITE_BUSY，
	// WAL 模式让读写不互相阻塞。
	dsn := "file:" + memCfg.DBPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{
		db:     db,
		config: memCfg,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Set up file paths
	memoryDir := filepath.Join(config.GetMagicHome(), "memories")
	store.agentMemoryPath = filepath.Join(memoryDir, "MEMORY.md")
	store.userMemoryPath = filepath.Join(memoryDir, "USER.md")

	// Ensure file-based memories exist
	store.ensureMemoryFiles()

	return store, nil
}

// initSchema creates the database tables
func (s *Store) initSchema() error {
	schema := `
	-- Main memories table
	CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		content TEXT NOT NULL,
		scope TEXT DEFAULT '',
		categories TEXT DEFAULT '[]',
		importance REAL DEFAULT 0.5,
		metadata TEXT DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		last_access TEXT NOT NULL,
		access_count INTEGER DEFAULT 0,
		session_id TEXT DEFAULT '',
		source TEXT DEFAULT ''
	);

	-- FTS5 virtual table for full-text search
	CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		content,
		categories,
		scope,
		content='memories',
		content_rowid='rowid'
	);

	-- Triggers to keep FTS index in sync
	CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, content, categories, scope) 
		VALUES (new.rowid, new.content, new.categories, new.scope);
	END;

	CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, content, categories, scope) 
		VALUES ('delete', old.rowid, old.content, old.categories, old.scope);
	END;

	CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, content, categories, scope) 
		VALUES ('delete', old.rowid, old.content, old.categories, old.scope);
		INSERT INTO memories_fts(rowid, content, categories, scope) 
		VALUES (new.rowid, new.content, new.categories, new.scope);
	END;

	-- Index for faster lookups
	CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
	CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
	CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id);
	CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance);
	CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at);

	-- Command approval history (for approval learning)
	CREATE TABLE IF NOT EXISTS command_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		command TEXT NOT NULL,
		command_hash TEXT NOT NULL,
		action TEXT NOT NULL, -- approved, denied, auto_approved
		session_id TEXT DEFAULT '',
		created_at TEXT NOT NULL,
		count INTEGER DEFAULT 1
	);

	CREATE INDEX IF NOT EXISTS idx_command_hash ON command_history(command_hash);
	`

	_, err := s.db.Exec(schema)
	return err
}

// ensureMemoryFiles creates Cortex-style memory files if they don't exist
func (s *Store) ensureMemoryFiles() {
	for _, path := range []string{s.agentMemoryPath, s.userMemoryPath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.MkdirAll(filepath.Dir(path), 0755)
			var template string
			if strings.HasSuffix(path, "MEMORY.md") {
				template = "# Agent Memory\n\n## Notes\n\n"
			} else {
				template = "# User Profile\n\n## Basic Info\n\n"
			}
			os.WriteFile(path, []byte(template), 0644)
		}
	}
}

// Store adds a new memory
func (s *Store) Store(m *Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m.ID == "" {
		m.ID = generateID()
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	m.LastAccess = now

	categoriesJSON, err := json.Marshal(m.Categories)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}
	if m.Categories == nil {
		categoriesJSON = []byte("[]")
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO memories 
		(id, type, content, scope, categories, importance, metadata, created_at, updated_at, last_access, access_count, session_id, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.Type, m.Content, m.Scope, string(categoriesJSON), m.Importance, m.Metadata,
		m.CreatedAt.Format(time.RFC3339), m.UpdatedAt.Format(time.RFC3339),
		m.LastAccess.Format(time.RFC3339), m.AccessCount, m.SessionID, m.Source)

	return err
}

// Recall searches for relevant memories based on query
func (s *Store) Recall(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	if limit <= 0 {
		limit = 10
	}

	// 先用读锁读取记忆，避免在读锁下执行写操作破坏锁语义
	s.mu.RLock()
	memories, err := s.queryRecall(query, limit, memoryTypes...)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	// 释放读锁后，再批量更新访问统计（内部持写锁）
	s.batchUpdateAccess(memories)
	return memories, nil
}

// queryRecall 执行 FTS5 检索，不持锁（由调用方持锁），也不更新访问统计
func (s *Store) queryRecall(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	// CJK（中文/日文/韩文）文本没有空格分词，FTS5 默认 tokenizer 无法有效匹配，
	// 直接走 LIKE 兜底检索，避免中文查询静默返回空结果。
	if containsCJK(query) {
		return s.queryRecallFallback(query, limit, memoryTypes...)
	}

	// 用参数化占位符构造 type 过滤条件，避免 SQL 注入
	typeFilter := ""
	typeArgs := []interface{}{}
	if len(memoryTypes) > 0 {
		placeholders := make([]string, len(memoryTypes))
		for i, t := range memoryTypes {
			placeholders[i] = "?"
			typeArgs = append(typeArgs, t)
		}
		typeFilter = fmt.Sprintf("AND type IN (%s)", strings.Join(placeholders, ", "))
	}

	// 用 FTS5 检索
	ftsQuery := sanitizeFTSQuery(query)

	sqlQuery := fmt.Sprintf(`
		SELECT m.id, m.type, m.content, m.scope, m.categories, m.importance,
			   m.metadata, m.created_at, m.updated_at, m.last_access, m.access_count,
			   m.session_id, m.source,
			   bm25(memories_fts) as rank
		FROM memories m
		JOIN memories_fts ON m.rowid = memories_fts.rowid
		WHERE memories_fts MATCH ?
		%s
		ORDER BY rank
		LIMIT ?
	`, typeFilter)

	// 参数顺序：MATCH ? , type IN (?,?,?) , LIMIT ?
	args := []interface{}{ftsQuery}
	args = append(args, typeArgs...)
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		// FTS 失败时回退到 LIKE 检索
		return s.queryRecallFallback(query, limit, memoryTypes...)
	}
	defer rows.Close()

	return s.scanMemories(rows)
}

// queryRecallFallback 在 FTS 失败时用 LIKE 进行基础检索，不持锁，也不更新访问统计
func (s *Store) queryRecallFallback(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	// 用参数化占位符构造 type 过滤条件，避免 SQL 注入
	typeFilter := ""
	typeArgs := []interface{}{}
	if len(memoryTypes) > 0 {
		placeholders := make([]string, len(memoryTypes))
		for i, t := range memoryTypes {
			placeholders[i] = "?"
			typeArgs = append(typeArgs, t)
		}
		typeFilter = fmt.Sprintf("AND type IN (%s)", strings.Join(placeholders, ", "))
	}

	likeQuery := "%" + query + "%"
	sqlQuery := fmt.Sprintf(`
		SELECT id, type, content, scope, categories, importance,
			   metadata, created_at, updated_at, last_access, access_count,
			   session_id, source
		FROM memories
		WHERE (content LIKE ? OR scope LIKE ? OR categories LIKE ?)
		%s
		ORDER BY importance DESC, access_count DESC
		LIMIT ?
	`, typeFilter)

	// 参数顺序：LIKE ? , LIKE ? , LIKE ? , type IN (?,?,?) , LIMIT ?
	args := []interface{}{likeQuery, likeQuery, likeQuery}
	args = append(args, typeArgs...)
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMemories(rows)
}

// Search performs FTS5 full-text search
func (s *Store) Search(query string, limit int) ([]*Memory, error) {
	s.totalSearches.Add(1)
	return s.Recall(query, limit)
}

// scanMemories scans rows into Memory structs（仅扫描行，不更新访问统计）
func (s *Store) scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var categories, metadata string
		var createdAt, updatedAt, lastAccess string

		err := rows.Scan(&m.ID, &m.Type, &m.Content, &m.Scope, &categories,
			&m.Importance, &metadata, &createdAt, &updatedAt, &lastAccess,
			&m.AccessCount, &m.SessionID, &m.Source)
		if err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(categories), &m.Categories)
		// Metadata 本身就是 JSON 字符串，直接赋值。
		// （旧实现把 JSON 对象 Unmarshal 进 string 字段，必然失败且错误被忽略，
		//  导致 decay_factor 等元数据在读取后全部丢失。）
		m.Metadata = metadata

		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		m.LastAccess, _ = time.Parse(time.RFC3339, lastAccess)

		memories = append(memories, m)
	}
	return memories, nil
}

// batchUpdateAccess 批量更新记忆的最后访问时间和访问次数，持写锁
func (s *Store) batchUpdateAccess(memories []*Memory) {
	if len(memories) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	placeholders := make([]string, len(memories))
	args := make([]interface{}, 0, len(memories)+1)
	// 参数顺序：SET last_access = ? , WHERE id IN (?,?,?)
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	for i, m := range memories {
		placeholders[i] = "?"
		args = append(args, m.ID)
	}

	query := fmt.Sprintf(`
		UPDATE memories SET last_access = ?, access_count = access_count + 1
		WHERE id IN (%s)
	`, strings.Join(placeholders, ", "))
	if _, err := s.db.Exec(query, args...); err != nil {
		log.Warnf("batch update access stats failed: %v", err)
	}
}

// containsCJK checks whether text contains Chinese/Japanese/Korean characters.
func containsCJK(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// sanitizeFTSQuery 对 FTS5 查询进行转义与清理：
//   - 移除控制字符（< 0x20 的 rune 替换为空格）
//   - 双引号转义为 ""
//   - 用双引号包裹整个查询作为短语匹配
//   - 末尾加 * 做前缀匹配
func sanitizeFTSQuery(query string) string {
	if query == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range query {
		if r < 0x20 {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	cleaned := b.String()
	escaped := strings.ReplaceAll(cleaned, "\"", "\"\"")
	return "\"" + escaped + "\"*"
}

// List returns all memories, optionally filtered by type
func (s *Store) List(memoryType MemoryType, limit, offset int) ([]*Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	query := "SELECT id, type, content, scope, categories, importance, metadata, created_at, updated_at, last_access, access_count, session_id, source FROM memories"
	args := []interface{}{}

	if memoryType != "" {
		query += " WHERE type = ?"
		args = append(args, memoryType)
	}

	query += " ORDER BY importance DESC, updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMemories(rows)
}

// Delete removes a memory by ID
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM memories WHERE id = ?", id)
	return err
}

// DeleteByScope removes all memories in a scope
func (s *Store) DeleteByScope(scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM memories WHERE scope = ? OR scope LIKE ?", scope, scope+"/%")
	return err
}

// Update updates an existing memory
func (s *Store) Update(m *Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m.UpdatedAt = time.Now().UTC()
	categories, _ := json.Marshal(m.Categories)

	_, err := s.db.Exec(`
		UPDATE memories SET 
			content = ?, scope = ?, categories = ?, importance = ?, 
			metadata = ?, updated_at = ?
		WHERE id = ?
	`, m.Content, m.Scope, string(categories), m.Importance, m.Metadata, m.UpdatedAt.Format(time.RFC3339), m.ID)

	return err
}

// Summarize uses LLM to summarize memories
func (s *Store) Summarize(memories []*Memory) (string, error) {
	if len(memories) == 0 {
		return "", nil
	}

	// If no LLM provider configured, fall back to basic concatenation
	if s.config.LLMProvider == "" {
		return s.basicSummary(memories), nil
	}

	// Try to use LLM for summarization
	summary, err := s.llmSummarize(memories)
	if err != nil {
		// Fall back to basic summary on error
		log.Warnf("LLM summarization failed, using basic summary: %v", err)
		return s.basicSummary(memories), nil
	}

	return summary, nil
}

// basicSummary creates a basic summary without LLM
func (s *Store) basicSummary(memories []*Memory) string {
	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Found %d relevant memories:\n\n", len(memories)))

	for i, m := range memories {
		if i >= 5 {
			summary.WriteString(fmt.Sprintf("... and %d more\n", len(memories)-5))
			break
		}
		summary.WriteString(fmt.Sprintf("## %s [%s]\n%s\n\n", m.Type, m.Scope, m.Content))
	}

	return summary.String()
}

// SetLLMProvider 注入用于 Summarize 的 LLM provider。
// 设置后不再依赖环境变量探测（OPENAI_API_KEY 等），统一走项目配置体系。
func (s *Store) SetLLMProvider(p provider.Provider) {
	s.llmProvider.Store(p)
}

// llmSummarize uses LLM to generate a summary
func (s *Store) llmSummarize(memories []*Memory) (string, error) {
	// Build prompt for summarization
	var prompt strings.Builder
	prompt.WriteString("You are a helpful assistant. Please summarize the following memories in a concise way.\n\n")
	prompt.WriteString("Memories:\n\n")

	for i, m := range memories {
		prompt.WriteString(fmt.Sprintf("%d. (%s) %s: %s\n", i+1, m.Type, m.Scope, m.Content))
	}

	prompt.WriteString("\nPlease provide a brief summary highlighting the key information:")

	// Try to get provider from environment or config
	llm := s.getLLMProvider()
	if llm == nil {
		return "", fmt.Errorf("no LLM provider available")
	}

	// Call LLM
	ctx := context.Background()
	resp, err := llm.Chat(ctx, []types.Message{
		{Role: "user", Content: prompt.String()},
	})
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	return resp.Content, nil
}

// getLLMProvider returns the configured LLM provider.
// 优先使用 SetLLMProvider 注入的实例；否则回退到环境变量探测。
func (s *Store) getLLMProvider() provider.Provider {
	// 1) 外部显式注入的 provider（推荐路径）
	if p, ok := s.llmProvider.Load().(provider.Provider); ok && p != nil {
		return p
	}

	// 2) 兼容旧方式：环境变量探测
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		log.Warnf("[Memory] using OPENAI_API_KEY env fallback for summarization; prefer Store.SetLLMProvider()")
		return provider.NewOpenAICompatibleProvider("openai", apiKey, "https://api.openai.com/v1", "gpt-3.5-turbo", nil)
	}

	apiKey = os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		return provider.NewAnthropicProvider(apiKey, "claude-3-haiku-20240307")
	}

	apiKey = os.Getenv("DEEPSEEK_API_KEY")
	if apiKey != "" {
		return provider.NewDeepSeekProvider(apiKey, "", "deepseek-chat", nil)
	}

	return nil
}

// ReadAgentMemory reads the Cortex-style agent memory file
func (s *Store) ReadAgentMemory() (string, error) {
	// 文件读写全程持 fileMu，防止并发 append 互相覆盖
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	content, err := os.ReadFile(s.agentMemoryPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteAgentMemory writes to the Cortex-style agent memory file
func (s *Store) WriteAgentMemory(content string) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	return s.writeAgentMemoryLocked(content)
}

// writeAgentMemoryLocked 写入 agent 记忆文件，调用方必须已持有 s.fileMu
func (s *Store) writeAgentMemoryLocked(content string) error {
	// 按字符限制截断，回退到 UTF-8 rune 边界，避免切断多字节字符
	content = truncateString(content, s.config.MaxAgentMemLength)
	return os.WriteFile(s.agentMemoryPath, []byte(content), 0644)
}

// ReadUserMemory reads the Cortex-style user profile file
func (s *Store) ReadUserMemory() (string, error) {
	// 文件读写全程持 fileMu，防止并发 append 互相覆盖
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	content, err := os.ReadFile(s.userMemoryPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteUserMemory writes to the Cortex-style user profile file
func (s *Store) WriteUserMemory(content string) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	return s.writeUserMemoryLocked(content)
}

// writeUserMemoryLocked 写入用户记忆文件，调用方必须已持有 s.fileMu
func (s *Store) writeUserMemoryLocked(content string) error {
	// 按字符限制截断，回退到 UTF-8 rune 边界，避免切断多字节字符
	content = truncateString(content, s.config.MaxUserMemLength)
	return os.WriteFile(s.userMemoryPath, []byte(content), 0644)
}

// AppendAgentMemory appends content to agent memory (Cortex-style)
func (s *Store) AppendAgentMemory(content string) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	current, err := os.ReadFile(s.agentMemoryPath)
	if err != nil {
		current = []byte("# Agent Memory\n\n## Notes\n\n")
	}

	// Check if we need to truncate
	newContent := string(current) + content + "\n"
	if len(newContent) > s.config.MaxAgentMemLength {
		// Simple truncation - keep the newer content
		available := s.config.MaxAgentMemLength - len(content) - 10
		if available > 100 {
			// 截断回退到 UTF-8 rune 边界，避免切断多字节字符
			newCurrent := truncateString(string(current), available) + "\n...\n"
			newContent = newCurrent + content + "\n"
		} else {
			// 新内容本身超长或剩余空间不足：丢弃最旧内容，保留最新 content
			log.Warnf("[Memory] agent memory overflow (content %d chars), keeping newest entry only", len(content))
			newContent = content + "\n"
		}
	}

	return s.writeAgentMemoryLocked(newContent)
}

// AppendUserMemory appends content to user memory (Cortex-style)
func (s *Store) AppendUserMemory(content string) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	current, err := os.ReadFile(s.userMemoryPath)
	if err != nil {
		current = []byte("# User Profile\n\n## Basic Info\n\n")
	}

	newContent := string(current) + content + "\n"
	if len(newContent) > s.config.MaxUserMemLength {
		available := s.config.MaxUserMemLength - len(content) - 10
		if available > 100 {
			// 截断回退到 UTF-8 rune 边界，避免切断多字节字符
			newCurrent := truncateString(string(current), available) + "\n...\n"
			newContent = newCurrent + content + "\n"
		} else {
			log.Warnf("[Memory] user memory overflow (content %d chars), keeping newest entry only", len(content))
			newContent = content + "\n"
		}
	}

	return s.writeUserMemoryLocked(newContent)
}

// RecordCommandAction records a command approval/denial
func (s *Store) RecordCommandAction(command, action, sessionID string) error {
	hash := hashCommand(command)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.Exec(`
		INSERT INTO command_history (command, command_hash, action, session_id, created_at, count)
		VALUES (?, ?, ?, ?, ?, 1)
		ON CONFLICT(command_hash) DO UPDATE SET 
			count = count + 1,
			action = excluded.action,
			created_at = excluded.created_at
	`, command, hash, action, sessionID, now)

	return err
}

// GetCommandTrustLevel returns how trusted a command pattern is
func (s *Store) GetCommandTrustLevel(commandHash string) (action string, count int, err error) {
	err = s.db.QueryRow(
		"SELECT action, count FROM command_history WHERE command_hash = ?",
		commandHash,
	).Scan(&action, &count)
	return
}

// Close closes the memory store
func (s *Store) Close() error {
	return s.db.Close()
}

// generateID creates a unique ID
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 回退到时间戳+进程号，避免两次相同时间戳造成碰撞
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	}
	return hex.EncodeToString(b)
}

// hashCommand creates a hash for command pattern matching
func hashCommand(cmd string) string {
	h := sha256.Sum256([]byte(cmd))
	return hex.EncodeToString(h[:])
}

// Stats returns memory statistics
type MemoryStats struct {
	TotalMemories int
	ByType        map[MemoryType]int
	TotalSearches int
	AvgImportance float64
	LastUpdated   time.Time
}

func (s *Store) Stats() (*MemoryStats, error) {
	stats := &MemoryStats{
		ByType: make(map[MemoryType]int),
	}

	// 总数
	if err := s.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&stats.TotalMemories); err != nil {
		return nil, fmt.Errorf("failed to query total memories: %w", err)
	}

	// 按类型分组
	rows, err := s.db.Query("SELECT type, COUNT(*) FROM memories GROUP BY type")
	if err != nil {
		return nil, fmt.Errorf("failed to query memories by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var t MemoryType
		var count int
		if err := rows.Scan(&t, &count); err != nil {
			return nil, fmt.Errorf("failed to scan memory type row: %w", err)
		}
		stats.ByType[t] = count
	}

	// 平均重要度（空表时 AVG 返回 NULL，用 sql.NullFloat64 接收）
	var avg sql.NullFloat64
	if err := s.db.QueryRow("SELECT AVG(importance) FROM memories").Scan(&avg); err != nil {
		return nil, fmt.Errorf("failed to query average importance: %w", err)
	}
	if avg.Valid {
		stats.AvgImportance = avg.Float64
	}

	stats.TotalSearches = int(s.totalSearches.Load())

	return stats, nil
}
