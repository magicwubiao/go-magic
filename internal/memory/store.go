// Package memory provides a persistent memory system with FTS5 full-text search
// inspired by Cortex Agent's memory architecture
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
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
	home, _ := os.UserHomeDir()
	return &MemoryConfig{
		DBPath:             filepath.Join(home, ".magic", "memories", "memory.db"),
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
}

// NewStore creates a new memory store
func NewStore(config *MemoryConfig) (*Store, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Ensure directory exists
	dir := filepath.Dir(config.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	// Initialize SQLite database
	db, err := sql.Open("sqlite3", config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{
		db:     db,
		config: config,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Set up file paths
	home, _ := os.UserHomeDir()
	memoryDir := filepath.Join(home, ".magic", "memories")
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

	categories, _ := json.Marshal(m.Categories)
	if m.Categories == nil {
		categories = []byte("[]")
	}

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO memories 
		(id, type, content, scope, categories, importance, metadata, created_at, updated_at, last_access, access_count, session_id, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.Type, m.Content, m.Scope, string(categories), m.Importance, m.Metadata,
		m.CreatedAt.Format(time.RFC3339), m.UpdatedAt.Format(time.RFC3339),
		m.LastAccess.Format(time.RFC3339), m.AccessCount, m.SessionID, m.Source)

	return err
}

// Recall searches for relevant memories based on query
func (s *Store) Recall(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 10
	}

	typeFilter := ""
	if len(memoryTypes) > 0 {
		types := make([]string, len(memoryTypes))
		for i, t := range memoryTypes {
			types[i] = fmt.Sprintf("'%s'", t)
		}
		typeFilter = fmt.Sprintf("AND type IN (%s)", strings.Join(types, ", "))
	}

	// Use FTS5 for search
	ftsQuery := query
	if ftsQuery != "" {
		// Escape special FTS5 characters and add prefix matching
		ftsQuery = fmt.Sprintf("\"%s\"*", strings.ReplaceAll(ftsQuery, "\"", "\"\""))
	}

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

	rows, err := s.db.Query(sqlQuery, ftsQuery, limit)
	if err != nil {
		// Fallback to LIKE search if FTS fails
		return s.recallFallback(query, limit, memoryTypes...)
	}
	defer rows.Close()

	return s.scanMemories(rows)
}

// recallFallback uses LIKE for basic search when FTS fails
func (s *Store) recallFallback(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	typeFilter := ""
	if len(memoryTypes) > 0 {
		types := make([]string, len(memoryTypes))
		for i, t := range memoryTypes {
			types[i] = fmt.Sprintf("'%s'", t)
		}
		typeFilter = fmt.Sprintf("AND type IN (%s)", strings.Join(types, ", "))
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

	rows, err := s.db.Query(sqlQuery, likeQuery, likeQuery, likeQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMemories(rows)
}

// Search performs FTS5 full-text search
func (s *Store) Search(query string, limit int) ([]*Memory, error) {
	return s.Recall(query, limit)
}

// scanMemories scans rows into Memory structs
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
		json.Unmarshal([]byte(metadata), &m.Metadata)

		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		m.LastAccess, _ = time.Parse(time.RFC3339, lastAccess)

		// Update access stats
		s.db.Exec("UPDATE memories SET last_access = ?, access_count = access_count + 1 WHERE id = ?",
			time.Now().UTC().Format(time.RFC3339), m.ID)

		memories = append(memories, m)
	}
	return memories, nil
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

// llmSummarize uses LLM to generate a summary
func (s *Store) llmSummarize(memories []*Memory) (string, error) {
	// Build prompt for summarization
	var prompt strings.Builder
	prompt.WriteString("You are a helpful assistant. Please summarize the following memories in a concise way.\n\n")
	prompt.WriteString("Memories:\n\n")

	for i, m := range memories {
		prompt.WriteString(fmt.Sprintf("[%d] (%s) %s: %s\n", i+1, m.Type, m.Scope, m.Content))
	}

	prompt.WriteString("\nPlease provide a brief summary highlighting the key information:")

	// Try to get provider from environment or config
	provider := s.getLLMProvider()
	if provider == nil {
		return "", fmt.Errorf("no LLM provider available")
	}

	// Call LLM
	ctx := context.Background()
	resp, err := provider.Chat(ctx, []provider.Message{
		{Role: "user", Content: prompt.String()},
	})
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	return resp.Content, nil
}

// getLLMProvider returns the configured LLM provider
func (s *Store) getLLMProvider() provider.Provider {
	// Check if provider is configured via environment or config
	// For now, try common providers
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		return provider.NewOpenAIProvider(apiKey, "", "gpt-3.5-turbo")
	}

	apiKey = os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		return provider.NewAnthropicProvider(apiKey, "claude-3-haiku-20240307")
	}

	apiKey = os.Getenv("DEEPSEEK_API_KEY")
	if apiKey != "" {
		return provider.NewDeepSeekProvider(apiKey, "deepseek-chat")
	}

	return nil
}

// ReadAgentMemory reads the Cortex-style agent memory file
func (s *Store) ReadAgentMemory() (string, error) {
	content, err := os.ReadFile(s.agentMemoryPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteAgentMemory writes to the Cortex-style agent memory file
func (s *Store) WriteAgentMemory(content string) error {
	// Enforce character limit
	if len(content) > s.config.MaxAgentMemLength {
		content = content[:s.config.MaxAgentMemLength]
	}
	return os.WriteFile(s.agentMemoryPath, []byte(content), 0644)
}

// ReadUserMemory reads the Cortex-style user profile file
func (s *Store) ReadUserMemory() (string, error) {
	content, err := os.ReadFile(s.userMemoryPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteUserMemory writes to the Cortex-style user profile file
func (s *Store) WriteUserMemory(content string) error {
	if len(content) > s.config.MaxUserMemLength {
		content = content[:s.config.MaxUserMemLength]
	}
	return os.WriteFile(s.userMemoryPath, []byte(content), 0644)
}

// AppendAgentMemory appends content to agent memory (Cortex-style)
func (s *Store) AppendAgentMemory(content string) error {
	current, err := s.ReadAgentMemory()
	if err != nil {
		current = "# Agent Memory\n\n## Notes\n\n"
	}

	// Check if we need to truncate
	newContent := current + content + "\n"
	if len(newContent) > s.config.MaxAgentMemLength {
		// Simple truncation - keep the newer content
		available := s.config.MaxAgentMemLength - len(content) - 10
		if available > 100 {
			current = current[:available] + "\n...\n"
		}
		newContent = current + content + "\n"
	}

	return s.WriteAgentMemory(newContent)
}

// AppendUserMemory appends content to user memory (Cortex-style)
func (s *Store) AppendUserMemory(content string) error {
	current, err := s.ReadUserMemory()
	if err != nil {
		current = "# User Profile\n\n## Basic Info\n\n"
	}

	newContent := current + content + "\n"
	if len(newContent) > s.config.MaxUserMemLength {
		available := s.config.MaxUserMemLength - len(content) - 10
		if available > 100 {
			current = current[:available] + "\n...\n"
		}
		newContent = current + content + "\n"
	}

	return s.WriteUserMemory(newContent)
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
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
}

// hashCommand creates a hash for command pattern matching
func hashCommand(cmd string) string {
	// Simple hash - normalize the command
	normalized := strings.ToLower(strings.TrimSpace(cmd))
	// Remove specific values but keep structure
	normalized = strings.ReplaceAll(normalized, "1234", "{NUM}")
	normalized = strings.ReplaceAll(normalized, "test-user", "{USER}")
	return fmt.Sprintf("%x", len(normalized))
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

	// Total count
	s.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&stats.TotalMemories)

	// By type
	rows, err := s.db.Query("SELECT type, COUNT(*) FROM memories GROUP BY type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var t MemoryType
		var count int
		rows.Scan(&t, &count)
		stats.ByType[t] = count
	}

	// Average importance
	s.db.QueryRow("SELECT AVG(importance) FROM memories").Scan(&stats.AvgImportance)

	return stats, nil
}
