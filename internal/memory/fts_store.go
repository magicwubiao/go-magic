package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FTSStore provides full-text search across all conversation history
// This is System 5 of the Cortex six systems:
// "Holographic memory retrieval with SQLite FTS5"
type FTSStore struct {
	db     *sql.DB
	dbPath string
}

// NewFTSStore creates a new FTS-based memory store
func NewFTSStore(baseDir string) (*FTSStore, error) {
	dbPath := filepath.Join(baseDir, "memory.sqlite")
	os.MkdirAll(baseDir, 0755)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	store := &FTSStore{
		db:     db,
		dbPath: dbPath,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		return nil, err
	}

	return store, nil
}

// initSchema creates the necessary tables and indexes
func (f *FTSStore) initSchema() error {
	// Main memory table
	_, err := f.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			turn_number INTEGER NOT NULL,
			role TEXT NOT NULL,           -- user, assistant, tool
			content TEXT NOT NULL,
			content_type TEXT DEFAULT 'text', -- text, summary, insight, skill
			tags TEXT,                     -- comma-separated tags
			importance INTEGER DEFAULT 5,  -- 1-10, higher = more important
			embedding BLOB,                -- for future vector search
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// FTS5 virtual table for full-text search
	_, err = f.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts 
		USING fts5(content, tags, content='memories', content_rowid='id')
	`)
	if err != nil {
		return err
	}

	// Triggers to keep FTS index in sync
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			DELETE FROM memories_fts WHERE rowid = old.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
			UPDATE memories_fts SET content = new.content, tags = new.tags WHERE rowid = new.id;
		END`,
	}

	for _, trigger := range triggers {
		if _, err := f.db.Exec(trigger); err != nil {
			return err
		}
	}

	// Indexes for performance
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at)`,
	}

	for _, idx := range indexes {
		if _, err := f.db.Exec(idx); err != nil {
			return err
		}
	}

	return nil
}

// MemoryRecord represents a single memory entry
type MemoryRecord struct {
	ID          int       `json:"id"`
	SessionID   string    `json:"session_id"`
	TurnNumber  int       `json:"turn_number"`
	Role        string    `json:"role"`
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"`
	Tags        []string  `json:"tags,omitempty"`
	Importance  int       `json:"importance"`
	CreatedAt   time.Time `json:"created_at"`
}

// SearchResult represents a search result with rank
type SearchResult struct {
	MemoryRecord
	Rank   float64 `json:"rank"`   // BM25 score, lower = better
	Snippet string  `json:"snippet"` // Highlighted snippet
}

// Add adds a new memory to the FTS store
func (f *FTSStore) Add(memory *MemoryRecord) error {
	tags := strings.Join(memory.Tags, ",")

	result, err := f.db.Exec(`
		INSERT INTO memories (session_id, turn_number, role, content, content_type, tags, importance)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, memory.SessionID, memory.TurnNumber, memory.Role, memory.Content, memory.ContentType, tags, memory.Importance)

	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	memory.ID = int(id)
	return nil
}

// Search performs a full-text search across all memories
func (f *FTSStore) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	// Use BM25 ranking for relevance
	rows, err := f.db.Query(`
		SELECT 
			m.id, m.session_id, m.turn_number, m.role, m.content, 
			m.content_type, m.tags, m.importance, m.created_at,
			rank,
			snippet(memories_fts, -1, '**', '**', '...', 64)
		FROM memories m
		JOIN memories_fts f ON m.id = f.rowid
		WHERE memories_fts MATCH ?
		ORDER BY rank ASC
		LIMIT ?
	`, query, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags string

		err := rows.Scan(
			&r.ID, &r.SessionID, &r.TurnNumber, &r.Role, &r.Content,
			&r.ContentType, &tags, &r.Importance, &r.CreatedAt,
			&r.Rank, &r.Snippet,
		)
		if err != nil {
			return nil, err
		}

		if tags != "" {
			r.Tags = strings.Split(tags, ",")
		}

		results = append(results, r)
	}

	return results, nil
}

// GetContext retrieves relevant context for a query
// This is used to augment the system prompt with relevant memories
func (f *FTSStore) GetContext(query string, maxTokens int) string {
	results, err := f.Search(query, 5)
	if err != nil || len(results) == 0 {
		return ""
	}

	var context strings.Builder
	context.WriteString("## Relevant Memories\n\n")

	for _, r := range results {
		context.WriteString(fmt.Sprintf("- [%s] %s\n", r.Role, r.Content))
		if context.Len() > maxTokens {
			break
		}
	}

	return context.String()
}

// GetSession retrieves all memories for a specific session
func (f *FTSStore) GetSession(sessionID string) ([]MemoryRecord, error) {
	rows, err := f.db.Query(`
		SELECT id, session_id, turn_number, role, content, content_type, tags, importance, created_at
		FROM memories
		WHERE session_id = ?
		ORDER BY turn_number ASC
	`, sessionID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var r MemoryRecord
		var tags string

		err := rows.Scan(&r.ID, &r.SessionID, &r.TurnNumber, &r.Role, &r.Content,
			&r.ContentType, &tags, &r.Importance, &r.CreatedAt)
		if err != nil {
			return nil, err
		}

		if tags != "" {
			r.Tags = strings.Split(tags, ",")
		}
		results = append(results, r)
	}

	return results, nil
}

// AddInsight adds a structured insight learned from conversation
func (f *FTSStore) AddInsight(sessionID string, insight string, importance int) error {
	return f.Add(&MemoryRecord{
		SessionID:   sessionID,
		TurnNumber:  0, // 0 = not tied to specific turn
		Role:        "system",
		Content:     insight,
		ContentType: "insight",
		Importance:  importance,
		Tags:        []string{"insight", "learned"},
	})
}

// GetInsights retrieves all learned insights
func (f *FTSStore) GetInsights(minImportance int) ([]MemoryRecord, error) {
	rows, err := f.db.Query(`
		SELECT id, session_id, turn_number, role, content, content_type, tags, importance, created_at
		FROM memories
		WHERE content_type = 'insight' AND importance >= ?
		ORDER BY importance DESC, created_at DESC
	`, minImportance)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var r MemoryRecord
		var tags string

		err := rows.Scan(&r.ID, &r.SessionID, &r.TurnNumber, &r.Role, &r.Content,
			&r.ContentType, &tags, &r.Importance, &r.CreatedAt)
		if err != nil {
			return nil, err
		}

		if tags != "" {
			r.Tags = strings.Split(tags, ",")
		}
		results = append(results, r)
	}

	return results, nil
}

// CleanupOld removes old memories beyond retention policy
func (f *FTSStore) CleanupOld(olderThan time.Duration, keepMinImportance int) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := f.db.Exec(`
		DELETE FROM memories
		WHERE created_at < ? AND importance < ?
	`, cutoff, keepMinImportance)

	if err != nil {
		return 0, err
	}

	deleted, _ := result.RowsAffected()
	return int(deleted), nil
}

// GetStats returns statistics about the memory store
func (f *FTSStore) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total memories
	var total int
	f.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&total)
	stats["total_memories"] = total

	// By content type
	rows, _ := f.db.Query(`
		SELECT content_type, COUNT(*) FROM memories GROUP BY content_type
	`)
	if rows != nil {
		defer rows.Close()
		typeCounts := make(map[string]int)
		for rows.Next() {
			var t string
			var c int
			if rows.Scan(&t, &c) == nil {
				typeCounts[t] = c
			}
		}
		stats["by_type"] = typeCounts
	}

	// Session count
	var sessions int
	f.db.QueryRow("SELECT COUNT(DISTINCT session_id) FROM memories").Scan(&sessions)
	stats["sessions"] = sessions

	return stats, nil
}

// Close closes the database connection
func (f *FTSStore) Close() error {
	return f.db.Close()
}
