// Package memory provides a persistent memory system with FTS5 full-text search.
//
// This is the semantic memory store: typed facts/preferences/notes with
// importance, scope, categories, and command-trust history. It is the single
// canonical SQLite-backed memory for the project. File-based prompt memory
// (MEMORY.md / USER.md) is owned by SnapshotManager, not here.
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
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// MemoryType classifies what kind of thing a Memory is.
type MemoryType string

const (
	TypeAgent      MemoryType = "agent"      // Agent's own notes
	TypeUser       MemoryType = "user"       // User profile and preferences
	TypeSession    MemoryType = "session"    // Session-specific information
	TypeProject    MemoryType = "project"    // Project-related memories
	TypeKnowledge  MemoryType = "knowledge"  // General knowledge
	TypePreference MemoryType = "preference" // User preferences
)

// Memory represents a single memory entry.
type Memory struct {
	ID          string     `json:"id"`
	Type        MemoryType `json:"type"`
	Content     string     `json:"content"`
	Scope       string     `json:"scope,omitempty"`      // Hierarchical path or lookup key (e.g. /infrastructure/database)
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

// MemoryConfig holds configuration for the memory system.
type MemoryConfig struct {
	DBPath             string
	MaxContentLength   int    // Max characters per memory
	AutoSummarize      bool   // Enable automatic summarization
	SummarizeThreshold int    // Threshold for summarization (characters)
	LLMProvider        string // LLM provider for summarization
}

// DefaultConfig returns the default memory configuration.
func DefaultConfig() *MemoryConfig {
	home, _ := os.UserHomeDir()
	return &MemoryConfig{
		DBPath:             filepath.Join(home, ".magic", "memories", "memory.db"),
		MaxContentLength:   5000,
		AutoSummarize:      true,
		SummarizeThreshold: 3000,
		LLMProvider:        "openai",
	}
}

// Store manages the persistent semantic memory system.
type Store struct {
	db     *sql.DB
	config *MemoryConfig
	mu     sync.RWMutex
}

// NewStore creates a new memory store.
func NewStore(config *MemoryConfig) (*Store, error) {
	if config == nil {
		config = DefaultConfig()
	}

	dir := filepath.Dir(config.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	db, err := openMemoryDB(config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{
		db:     db,
		config: config,
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// openMemoryDB opens a SQLite database with WAL journaling and a busy timeout
// so concurrent readers/writers don't immediately fail with "database is locked".
// A single connection is used: modernc.org/sqlite serializes better this way
// and the memory store is low-traffic.
func openMemoryDB(path string) (*sql.DB, error) {
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// initSchema creates the database tables.
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

	-- FTS5 virtual table for full-text search (external content table).
	CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		content,
		categories,
		scope,
		content='memories',
		content_rowid='rowid'
	);

	-- Triggers keep the FTS index in sync using the external-content
	-- 'delete' command. (A plain DELETE/UPDATE on memories_fts does NOT
	-- work for external-content tables and silently corrupts the index.)
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

	-- Indexes for faster lookups
	CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
	CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
	CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id);
	CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance);
	CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at);

	-- Command approval history (for approval learning). command_hash is UNIQUE
	-- so the ON CONFLICT(command_hash) upsert in RecordCommandAction works.
	CREATE TABLE IF NOT EXISTS command_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		command TEXT NOT NULL,
		command_hash TEXT NOT NULL UNIQUE,
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

// Store adds a new memory (or replaces one with the same ID).
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

	categories, err := json.Marshal(m.Categories)
	if err != nil {
		categories = []byte("[]")
	}
	if m.Categories == nil {
		categories = []byte("[]")
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO memories
		(id, type, content, scope, categories, importance, metadata, created_at, updated_at, last_access, access_count, session_id, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.Type, m.Content, m.Scope, string(categories), m.Importance, m.Metadata,
		m.CreatedAt.Format(time.RFC3339), m.UpdatedAt.Format(time.RFC3339),
		m.LastAccess.Format(time.RFC3339), m.AccessCount, m.SessionID, m.Source)

	return err
}

// buildTypeFilter returns a parameterized "AND m.type IN (...)" fragment and
// its args. Using placeholders (never string interpolation) avoids SQL injection
// when callers pass arbitrary type strings.
func buildTypeFilter(types []MemoryType) (string, []interface{}) {
	if len(types) == 0 {
		return "", nil
	}
	placeholders := make([]string, len(types))
	args := make([]interface{}, len(types))
	for i, t := range types {
		placeholders[i] = "?"
		args[i] = string(t)
	}
	return fmt.Sprintf("AND m.type IN (%s)", strings.Join(placeholders, ",")), args
}

// Recall searches for memories relevant to query using FTS5 BM25 ranking.
func (s *Store) Recall(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	if limit <= 0 {
		limit = 10
	}

	typeFilter, typeArgs := buildTypeFilter(memoryTypes)

	memories, err := s.recallFTS(query, limit, typeFilter, typeArgs)
	if err != nil {
		// FTS can fail on queries it can't tokenize; fall back to LIKE.
		log.Debugf("memory: FTS recall failed, using LIKE fallback: %v", err)
		return s.recallFallback(query, limit, memoryTypes...)
	}
	return memories, nil
}

func (s *Store) recallFTS(query string, limit int, typeFilter string, typeArgs []interface{}) ([]*Memory, error) {
	ftsQuery := escapeFTSQuery(query)

	var sqlQuery string
	args := []interface{}{}

	if ftsQuery == "" {
		// No usable search terms: return recent high-importance memories.
		sqlQuery = fmt.Sprintf(`
			SELECT m.id, m.type, m.content, m.scope, m.categories, m.importance,
			       m.metadata, m.created_at, m.updated_at, m.last_access, m.access_count,
			       m.session_id, m.source
			FROM memories m
			WHERE 1=1 %s
			ORDER BY m.importance DESC, m.updated_at DESC
			LIMIT ?`, typeFilter)
		args = append(args, typeArgs...)
		args = append(args, limit)
	} else {
		sqlQuery = fmt.Sprintf(`
			SELECT m.id, m.type, m.content, m.scope, m.categories, m.importance,
			       m.metadata, m.created_at, m.updated_at, m.last_access, m.access_count,
			       m.session_id, m.source
			FROM memories m
			JOIN memories_fts ON m.rowid = memories_fts.rowid
			WHERE memories_fts MATCH ? %s
			ORDER BY bm25(memories_fts)
			LIMIT ?`, typeFilter)
		args = append(args, ftsQuery)
		args = append(args, typeArgs...)
		args = append(args, limit)
	}

	s.mu.RLock()
	rows, err := s.db.Query(sqlQuery, args...)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAndTrack(rows)
}

// recallFallback uses LIKE for basic search when FTS fails.
func (s *Store) recallFallback(query string, limit int, memoryTypes ...MemoryType) ([]*Memory, error) {
	typeFilter, typeArgs := buildTypeFilter(memoryTypes)
	likeQuery := "%" + query + "%"
	sqlQuery := fmt.Sprintf(`
		SELECT m.id, m.type, m.content, m.scope, m.categories, m.importance,
		       m.metadata, m.created_at, m.updated_at, m.last_access, m.access_count,
		       m.session_id, m.source
		FROM memories m
		WHERE (m.content LIKE ? OR m.scope LIKE ? OR m.categories LIKE ?)
		%s
		ORDER BY m.importance DESC, m.access_count DESC
		LIMIT ?`, typeFilter)
	args := []interface{}{likeQuery, likeQuery, likeQuery}
	args = append(args, typeArgs...)
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAndTrack(rows)
}

// Search performs FTS5 full-text search across all memory types.
func (s *Store) Search(query string, limit int) ([]*Memory, error) {
	return s.Recall(query, limit)
}

// scanAndTrack scans rows into Memory structs and bumps access stats for the
// returned IDs in a single UPDATE (no N+1 queries, no writes inside the row
// loop).
func (s *Store) scanAndTrack(rows *sql.Rows) ([]*Memory, error) {
	var memories []*Memory
	var ids []string
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

		_ = json.Unmarshal([]byte(categories), &m.Categories)
		_ = json.Unmarshal([]byte(metadata), &m.Metadata)

		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		m.LastAccess, _ = time.Parse(time.RFC3339, lastAccess)

		memories = append(memories, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.touchAccess(ids)
	return memories, nil
}

// touchAccess updates last_access and increments access_count for the given IDs
// in one statement.
func (s *Store) touchAccess(ids []string) {
	if len(ids) == 0 {
		return
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := fmt.Sprintf("UPDATE memories SET last_access = ?, access_count = access_count + 1 WHERE id IN (%s)",
		strings.Join(placeholders, ","))
	if _, err := s.db.Exec(q, args...); err != nil {
		log.Warnf("memory: failed to touch access for %d ids: %v", len(ids), err)
	}
}

// List returns memories, optionally filtered by type, newest/most-important first.
func (s *Store) List(memoryType MemoryType, limit, offset int) ([]*Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, type, content, scope, categories, importance, metadata,
			created_at, updated_at, last_access, access_count, session_id, source
		FROM memories`
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
	return s.scanAndTrack(rows)
}

// GetByScope returns the most recently updated memory with the given scope
// (used by the key/value memory tools to recall by exact key).
func (s *Store) GetByScope(scope string) (*Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT id, type, content, scope, categories, importance, metadata,
		created_at, updated_at, last_access, access_count, session_id, source
		FROM memories WHERE scope = ? ORDER BY updated_at DESC LIMIT 1`, scope)

	m := &Memory{}
	var categories, metadata string
	var createdAt, updatedAt, lastAccess string
	err := row.Scan(&m.ID, &m.Type, &m.Content, &m.Scope, &categories, &m.Importance,
		&metadata, &createdAt, &updatedAt, &lastAccess, &m.AccessCount, &m.SessionID, &m.Source)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(categories), &m.Categories)
	_ = json.Unmarshal([]byte(metadata), &m.Metadata)
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	m.LastAccess, _ = time.Parse(time.RFC3339, lastAccess)
	return m, nil
}

// Delete removes a memory by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM memories WHERE id = ?", id)
	return err
}

// DeleteByScope removes all memories in a scope (including sub-scopes).
func (s *Store) DeleteByScope(scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM memories WHERE scope = ? OR scope LIKE ?", scope, scope+"/%")
	return err
}

// Update updates an existing memory's mutable fields.
func (s *Store) Update(m *Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m.UpdatedAt = time.Now().UTC()
	categories, err := json.Marshal(m.Categories)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		UPDATE memories SET
			content = ?, scope = ?, categories = ?, importance = ?,
			metadata = ?, updated_at = ?
		WHERE id = ?
	`, m.Content, m.Scope, string(categories), m.Importance, m.Metadata, m.UpdatedAt.Format(time.RFC3339), m.ID)
	return err
}

// Summarize uses an LLM (if available) to summarize memories, otherwise a
// basic concatenation.
func (s *Store) Summarize(memories []*Memory) (string, error) {
	if len(memories) == 0 {
		return "", nil
	}
	if s.config.LLMProvider == "" {
		return s.basicSummary(memories), nil
	}
	summary, err := s.llmSummarize(memories)
	if err != nil {
		log.Warnf("LLM summarization failed, using basic summary: %v", err)
		return s.basicSummary(memories), nil
	}
	return summary, nil
}

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

func (s *Store) llmSummarize(memories []*Memory) (string, error) {
	var prompt strings.Builder
	prompt.WriteString("You are a helpful assistant. Please summarize the following memories in a concise way.\n\n")
	prompt.WriteString("Memories:\n\n")

	for i, m := range memories {
		prompt.WriteString(fmt.Sprintf("[%d] (%s) %s: %s\n", i+1, m.Type, m.Scope, m.Content))
	}
	prompt.WriteString("\nPlease provide a brief summary highlighting the key information:")

	p := s.getLLMProvider()
	if p == nil {
		return "", fmt.Errorf("no LLM provider available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Chat(ctx, []types.Message{
		{Role: "user", Content: prompt.String()},
	})
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}
	return resp.Content, nil
}

// getLLMProvider returns a provider configured from environment variables.
func (s *Store) getLLMProvider() provider.Provider {
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		return provider.NewOpenAIProvider(apiKey, "", "gpt-3.5-turbo")
	}
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		return provider.NewAnthropicProvider(apiKey, "claude-3-haiku-20240307")
	}
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		return provider.NewDeepSeekProvider(apiKey, "deepseek-chat")
	}
	return nil
}

// RecordCommandAction records a command approval/denial for trust learning.
func (s *Store) RecordCommandAction(command, action, sessionID string) error {
	hash := hashCommand(command)
	now := time.Now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()

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

// GetCommandTrustLevel returns how trusted a command pattern is.
func (s *Store) GetCommandTrustLevel(commandHash string) (action string, count int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	err = s.db.QueryRow(
		"SELECT action, count FROM command_history WHERE command_hash = ?",
		commandHash,
	).Scan(&action, &count)
	return
}

// HashCommand returns the hash of a command for use with GetCommandTrustLevel.
func HashCommand(cmd string) string {
	return hashCommand(cmd)
}

// Close closes the memory store.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// generateID creates a unique, collision-resistant ID using crypto/rand.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to timestamp.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Patterns used to normalize commands before hashing, so that two commands
// differing only in concrete values (numbers, emails, IPs, UUIDs) hash equally.
var (
	hashNumRE   = regexp.MustCompile(`\d+`)
	hashEmailRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	hashIPv4RE  = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	hashUUIDRE  = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	hashWSRE    = regexp.MustCompile(`\s+`)
)

// hashCommand produces a stable SHA-256 hash of a normalized command. Concrete
// values (numbers, emails, IPs, UUIDs) are masked so structurally similar
// commands map to the same trust record.
func hashCommand(cmd string) string {
	n := strings.ToLower(strings.TrimSpace(cmd))
	n = hashUUIDRE.ReplaceAllString(n, "{uuid}")
	n = hashEmailRE.ReplaceAllString(n, "{email}")
	n = hashIPv4RE.ReplaceAllString(n, "{ip}")
	n = hashNumRE.ReplaceAllString(n, "{n}")
	n = hashWSRE.ReplaceAllString(n, " ")
	h := sha256.Sum256([]byte(n))
	return hex.EncodeToString(h[:])
}

// ftsTermRE extracts alphanumeric terms from a free-form query for FTS5.
var ftsTermRE = regexp.MustCompile(`[A-Za-z0-9_]+`)

// escapeFTSQuery converts a free-form query string into a safe FTS5 MATCH
// expression. Each term is double-quoted (with internal quotes escaped) and
// terms are AND-ed; the final term gets a trailing "*" for prefix matching.
// Returns "" if no usable terms exist, signaling the caller to use a non-FTS
// query. This avoids FTS5 syntax errors on queries containing reserved
// characters (parens, colons, OR, *, etc.).
func escapeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	terms := ftsTermRE.FindAllString(query, -1)
	seen := make(map[string]bool)
	var clean []string
	for _, t := range terms {
		t = strings.ToLower(t)
		if len(t) < 2 || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
	}
	if len(clean) == 0 {
		return ""
	}
	var parts []string
	for i, t := range clean {
		q := strings.ReplaceAll(t, `"`, `""`)
		if i == len(clean)-1 {
			parts = append(parts, `"`+q+`"*`)
		} else {
			parts = append(parts, `"`+q+`"`)
		}
	}
	return strings.Join(parts, " AND ")
}

// MemoryStats holds memory store statistics.
type MemoryStats struct {
	TotalMemories int
	ByType        map[MemoryType]int
	TotalSearches int
	AvgImportance float64
	LastUpdated   time.Time
}

// Stats returns memory statistics.
func (s *Store) Stats() (*MemoryStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &MemoryStats{
		ByType: make(map[MemoryType]int),
	}

	s.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&stats.TotalMemories)

	rows, err := s.db.Query("SELECT type, COUNT(*) FROM memories GROUP BY type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var t MemoryType
		var count int
		if err := rows.Scan(&t, &count); err != nil {
			return nil, err
		}
		stats.ByType[t] = count
	}

	s.db.QueryRow("SELECT AVG(importance) FROM memories").Scan(&stats.AvgImportance)

	var lastUpdated string
	if err := s.db.QueryRow("SELECT MAX(updated_at) FROM memories").Scan(&lastUpdated); err == nil {
		stats.LastUpdated, _ = time.Parse(time.RFC3339, lastUpdated)
	}

	return stats, nil
}
