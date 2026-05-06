package memory

import (
	"container/list"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ImportanceDecayConfig configures how memory importance decays over time
type ImportanceDecayConfig struct {
	BaseDecayRate   float64 // Daily decay rate (0-1)
	MinImportance   int     // Minimum importance floor
	DecayInterval   time.Duration // How often to apply decay
}

// CacheConfig configures the result cache
type CacheConfig struct {
	Enabled       bool
	MaxEntries    int
	TTL           time.Duration
}

// EnhancedFTSStore provides advanced full-text search with semantic capabilities
type EnhancedFTSStore struct {
	db         *sql.DB
	dbPath     string
	baseDir    string
	
	// Cache
	cache      *SearchCache
	cacheConfig *CacheConfig
	
	// Importance tracking
	decayConfig *ImportanceDecayConfig
	decayTimer  *time.Timer
	
	// Semantic analysis
	synonymMap   map[string][]string
	stopWords    map[string]bool
	
	// Metrics
	stats       *FTSStats
	statsMu     sync.RWMutex
	
	// Memory deduplication
	hashIndex    map[string]int64 // hash -> memory_id
	
	// Cross-session linking
	sessionLinks []SessionLink
	linkMu       sync.RWMutex
}

// SessionLink represents a link between memories across sessions
type SessionLink struct {
	SourceID    int64     `json:"source_id"`
	TargetID    int64     `json:"target_id"`
	LinkType    string    `json:"link_type"` // "related", "follows", "references"
	CreatedAt   time.Time `json:"created_at"`
	Strength    float64  `json:"strength"` // 0-1
}

// FTSStats holds search statistics
type FTSStats struct {
	TotalSearches    int64            `json:"total_searches"`
	TotalHits        int64            `json:"total_hits"`
	AvgLatencyMs     float64          `json:"avg_latency_ms"`
	CacheHits        int64            `json:"cache_hits"`
	CacheMisses      int64            `json:"cache_misses"`
	QueryCounts      map[string]int  `json:"query_counts"`
	TopTerms         []TermFrequency  `json:"top_terms"`
}

// TermFrequency represents term frequency data
type TermFrequency struct {
	Term      string  `json:"term"`
	Frequency int     `json:"frequency"`
}

// SearchCache implements LRU caching for search results
type SearchCache struct {
	mu       sync.RWMutex
	entries  map[string]*CacheEntry
	lru      *list.List
	maxSize  int
	ttl      time.Duration
	enabled  bool
}

// CacheEntry represents a cached search result
type CacheEntry struct {
	key      string
	value    []SearchResult
	created  time.Time
	accesses int
	element  *list.Element
}

// NewEnhancedFTSStore creates an enhanced FTS store with semantic search
func NewEnhancedFTSStore(baseDir string) (*EnhancedFTSStore, error) {
	dbPath := filepath.Join(baseDir, "memory_enhanced.sqlite")
	os.MkdirAll(baseDir, 0755)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	store := &EnhancedFTSStore{
		db:         db,
		dbPath:     dbPath,
		baseDir:    baseDir,
		hashIndex:  make(map[string]int64),
		sessionLinks: make([]SessionLink, 0),
		stats: &FTSStats{
			QueryCounts: make(map[string]int),
			TopTerms:    make([]TermFrequency, 0),
		},
		synonymMap:  loadDefaultSynonyms(),
		stopWords:   loadStopWords(),
	}

	// Initialize cache
	store.cacheConfig = &CacheConfig{
		Enabled:    true,
		MaxEntries: 1000,
		TTL:        5 * time.Minute,
	}
	store.cache = newSearchCache(store.cacheConfig.MaxEntries, store.cacheConfig.TTL, store.cacheConfig.Enabled)

	// Initialize decay config
	store.decayConfig = &ImportanceDecayConfig{
		BaseDecayRate: 0.05, // 5% per day
		MinImportance: 1,
		DecayInterval: 1 * time.Hour,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		return nil, err
	}

	// Load existing hash index
	store.loadHashIndex()

	// Start importance decay
	store.startImportanceDecay()

	return store, nil
}

// newSearchCache creates a new search cache
func newSearchCache(maxSize int, ttl time.Duration, enabled bool) *SearchCache {
	return &SearchCache{
		entries: make(map[string]*CacheEntry),
		lru:     list.New(),
		maxSize: maxSize,
		ttl:     ttl,
		enabled: enabled,
	}
}

// loadDefaultSynonyms loads default synonym mappings
func loadDefaultSynonyms() map[string][]string {
	return map[string][]string{
		"create":   {"make", "generate", "build", "add", "new"},
		"delete":   {"remove", "drop", "erase", "clear"},
		"read":     {"get", "fetch", "retrieve", "load"},
		"write":    {"set", "update", "save", "store"},
		"search":   {"find", "query", "look", "seek"},
		"analyze":  {"examine", "review", "inspect", "check"},
		"execute":  {"run", "start", "launch", "begin"},
		"config":   {"configure", "setup", "settings", "options"},
		"error":    {"bug", "issue", "problem", "fault"},
		"success":  {"complete", "done", "finished", "ok"},
	}
}

// loadStopWords loads common stop words to exclude from indexing
func loadStopWords() map[string]bool {
	return map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"could": true, "should": true, "may": true, "might": true, "must": true,
		"shall": true, "can": true, "need": true, "it": true, "its": true,
		"this": true, "that": true, "these": true, "those": true,
	}
}

// initSchema creates enhanced tables and indexes
func (f *EnhancedFTSStore) initSchema() error {
	// Main memory table with enhanced fields
	_, err := f.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			turn_number INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			content_type TEXT DEFAULT 'text',
			tags TEXT,
			importance INTEGER DEFAULT 5,
			expires_at TIMESTAMP,
			content_hash TEXT UNIQUE,
			summary TEXT,
			parent_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_accessed TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			access_count INTEGER DEFAULT 0,
			FOREIGN KEY (parent_id) REFERENCES memories(id)
		)
	`)
	if err != nil {
		return err
	}

	// FTS5 virtual table with enhanced configuration
	_, err = f.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts 
		USING fts5(
			content, 
			tags, 
			summary,
			content='memories', 
			content_rowid='id',
			tokenize='porter unicode61'
		)
	`)
	if err != nil {
		return err
	}

	// Semantic similarity table
	_, err = f.db.Exec(`
		CREATE TABLE IF NOT EXISTS semantic_similarity (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			memory_id_1 INTEGER NOT NULL,
			memory_id_2 INTEGER NOT NULL,
			similarity_score REAL NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (memory_id_1) REFERENCES memories(id),
			FOREIGN KEY (memory_id_2) REFERENCES memories(id),
			UNIQUE(memory_id_1, memory_id_2)
		)
	`)
	if err != nil {
		return err
	}

	// Session links table
	_, err = f.db.Exec(`
		CREATE TABLE IF NOT EXISTS session_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			target_id INTEGER NOT NULL,
			link_type TEXT NOT NULL,
			strength REAL DEFAULT 1.0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (source_id) REFERENCES memories(id),
			FOREIGN KEY (target_id) REFERENCES memories(id)
		)
	`)
	if err != nil {
		return err
	}

	// Triggers for FTS sync
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, tags, summary) VALUES (new.id, new.content, new.tags, new.summary);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			DELETE FROM memories_fts WHERE rowid = old.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, tags, summary) VALUES('delete', old.id, old.content, old.tags, old.summary);
			INSERT INTO memories_fts(rowid, content, tags, summary) VALUES (new.id, new.content, new.tags, new.summary);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_access AFTER UPDATE ON memories WHEN old.last_accessed IS NULL OR old.last_accessed != new.last_accessed BEGIN
			UPDATE memories SET access_count = access_count + 1 WHERE id = new.id;
		END`,
	}

	for _, trigger := range triggers {
		if _, err := f.db.Exec(trigger); err != nil {
			return err
		}
	}

	// Indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(content_type)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_similarity_score ON semantic_similarity(similarity_score)`,
		`CREATE INDEX IF NOT EXISTS idx_links_source ON session_links(source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_links_target ON session_links(target_id)`,
	}

	for _, idx := range indexes {
		if _, err := f.db.Exec(idx); err != nil {
			return err
		}
	}

	return nil
}

// loadHashIndex loads existing content hashes for deduplication
func (f *EnhancedFTSStore) loadHashIndex() {
	rows, err := f.db.Query("SELECT id, content_hash FROM memories WHERE content_hash IS NOT NULL")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var hash string
		if err := rows.Scan(&id, &hash); err == nil {
			f.hashIndex[hash] = id
		}
	}
}

// startImportanceDecay starts background importance decay
func (f *EnhancedFTSStore) startImportanceDecay() {
	f.decayTimer = time.NewTimer(f.decayConfig.DecayInterval)
	go func() {
		for range f.decayTimer.C {
			f.applyImportanceDecay()
			f.decayTimer.Reset(f.decayConfig.DecayInterval)
		}
	}()
}

// applyImportanceDecay applies decay to all memories
func (f *EnhancedFTSStore) applyImportanceDecay() {
	query := `
		UPDATE memories 
		SET importance = MAX(?, CAST(importance * ? AS INTEGER))
		WHERE importance > ?
	`
	
	days := f.decayConfig.DecayInterval.Hours() / 24
	decayFactor := math.Pow(1-f.decayConfig.BaseDecayRate, days)
	
	f.db.Exec(query, f.decayConfig.MinImportance, decayFactor, f.decayConfig.MinImportance)
}

// EnhancedMemoryRecord extends MemoryRecord with additional metadata
type EnhancedMemoryRecord struct {
	MemoryRecord
	Summary        string    `json:"summary,omitempty"`
	ContentHash    string    `json:"content_hash,omitempty"`
	ParentID       int64     `json:"parent_id,omitempty"`
	LastAccessed   time.Time `json:"last_accessed"`
	AccessCount    int       `json:"access_count"`
	SimilarMemories []int64  `json:"similar_memories,omitempty"`
}

// computeHash computes a hash for content deduplication
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

// generateSummary generates a summary for long content
func generateSummary(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	
	// Simple summarization: take first sentence or maxLen chars
	summary := content[:maxLen]
	
	// Try to end at a sentence boundary
	if idx := strings.LastIndexAny(summary, ".!?"); idx > maxLen/2 {
		summary = summary[:idx+1]
	} else {
		summary = summary + "..."
	}
	
	return summary
}

// Add adds a new memory with deduplication and summarization
func (f *EnhancedFTSStore) Add(memory *EnhancedMemoryRecord) error {
	// Check for duplicates
	contentHash := computeHash(memory.Content)
	if existingID, ok := f.hashIndex[contentHash]; ok {
		// Update access time for existing memory
		f.db.Exec("UPDATE memories SET last_accessed = CURRENT_TIMESTAMP WHERE id = ?", existingID)
		memory.ID = int(existingID)
		return fmt.Errorf("duplicate content, existing id: %d", existingID)
	}

	// Generate summary for long content
	if len(memory.Content) > 200 && memory.Summary == "" {
		memory.Summary = generateSummary(memory.Content, 150)
	}

	tags := strings.Join(memory.Tags, ",")

	result, err := f.db.Exec(`
		INSERT INTO memories (session_id, turn_number, role, content, content_type, tags, importance, content_hash, summary, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, memory.SessionID, memory.TurnNumber, memory.Role, memory.Content, memory.ContentType, tags, memory.Importance, contentHash, memory.Summary, memory.ParentID)

	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	memory.ID = int(id)
	memory.ContentHash = contentHash
	f.hashIndex[contentHash] = id

	// Find similar memories asynchronously
	go f.findSimilarMemories(id, memory.Content)

	return nil
}

// findSimilarMemories finds and links similar memories
func (f *EnhancedFTSStore) findSimilarMemories(memoryID int64, content string) {
	// Simple similarity: check for common terms
	terms := f.extractTerms(content)
	if len(terms) < 3 {
		return
	}

	// Build similarity query
	query := strings.Join(terms[:min(5, len(terms))], " OR ")
	
	rows, err := f.db.Query(`
		SELECT m.id, m.content
		FROM memories m
		JOIN memories_fts f ON m.id = f.rowid
		WHERE memories_fts MATCH ? AND m.id != ?
		LIMIT 10
	`, query, memoryID)

	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var otherContent string
		if err := rows.Scan(&id, &otherContent); err != nil {
			continue
		}

		// Calculate simple similarity score
		score := f.calculateSimilarity(content, otherContent)
		if score > 0.3 {
			f.linkMemories(memoryID, id, "related", score)
		}
	}
}

// extractTerms extracts significant terms from content
func (f *EnhancedFTSStore) extractTerms(content string) []string {
	content = strings.ToLower(content)
	content = strings.ReplaceAll(content, "[^a-z0-9 ]", " ")
	words := strings.Fields(content)
	
	terms := make([]string, 0)
	for _, word := range words {
		if len(word) > 2 && !f.stopWords[word] {
			terms = append(terms, word)
		}
	}
	
	return terms
}

// calculateSimilarity calculates a simple similarity score
func (f *EnhancedFTSStore) calculateSimilarity(content1, content2 string) float64 {
	terms1 := f.extractTerms(content1)
	terms2 := f.extractTerms(content2)
	
	if len(terms1) == 0 || len(terms2) == 0 {
		return 0
	}
	
	// Jaccard similarity
	set1 := make(map[string]bool)
	for _, t := range terms1 {
		set1[t] = true
	}
	
	intersection := 0
	for _, t := range terms2 {
		if set1[t] {
			intersection++
		}
	}
	
	union := len(terms1) + len(terms2) - intersection
	if union == 0 {
		return 0
	}
	
	return float64(intersection) / float64(union)
}

// linkMemories creates a link between two memories
func (f *EnhancedFTSStore) linkMemories(sourceID, targetID int64, linkType string, strength float64) {
	f.db.Exec(`
		INSERT OR REPLACE INTO session_links (source_id, target_id, link_type, strength)
		VALUES (?, ?, ?, ?)
	`, sourceID, targetID, linkType, strength)
}

// EnhancedSearchOptions provides advanced search options
type EnhancedSearchOptions struct {
	// Basic options
	Limit        int
	Offset       int
	
	// Semantic options
	UseSynonyms  bool
	MinSimilarity float64
	
	// Time-based options
	TimeRange    *TimeRange
	SessionFilter []string
	
	// Content filters
	ContentTypes []string
	MinImportance int
	Tags         []string
	
	// Ranking options
	BoostRecent  bool
	BoostImportance bool
	BoostFrequency bool
	
	// Context options
	IncludeRelated bool
	MaxRelated     int
}

// TimeRange represents a time range for filtering
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// Search performs enhanced full-text search with caching
func (f *EnhancedFTSStore) Search(query string, options *EnhancedSearchOptions) ([]SearchResult, error) {
	startTime := time.Now()
	
	// Apply defaults
	if options == nil {
		options = &EnhancedSearchOptions{Limit: 10}
	}
	if options.Limit <= 0 {
		options.Limit = 10
	}

	// Build cache key
	cacheKey := f.buildCacheKey(query, options)

	// Check cache
	if f.cache.enabled {
		if cached, ok := f.cache.get(cacheKey); ok {
			f.recordStat("cache_hit", 1)
			return cached, nil
		}
	}

	// Expand query with synonyms
	expandedQuery := query
	if options.UseSynonyms {
		expandedQuery = f.expandWithSynonyms(query)
	}

	// Build SQL query
	sql, args := f.buildSearchSQL(expandedQuery, options)

	// Execute search
	rows, err := f.db.Query(sql, args...)
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
			continue
		}

		if tags != "" {
			r.Tags = strings.Split(tags, ",")
		}

		// Update access tracking
		f.db.Exec("UPDATE memories SET last_accessed = CURRENT_TIMESTAMP WHERE id = ?", r.ID)

		results = append(results, r)
	}

	// Cache results
	if f.cache.enabled && len(results) > 0 {
		f.cache.set(cacheKey, results)
	}

	// Record stats
	latency := time.Since(startTime).Milliseconds()
	f.recordStats(query, len(results), latency)

	return results, nil
}

// buildCacheKey builds a cache key for the query
func (f *EnhancedFTSStore) buildCacheKey(query string, options *EnhancedSearchOptions) string {
	data, _ := json.Marshal(options)
	return fmt.Sprintf("%s:%s", query, string(data))
}

// expandWithSynonyms expands query with synonyms
func (f *EnhancedFTSStore) expandWithSynonyms(query string) string {
	terms := f.extractTerms(query)
	var expanded []string

	for _, term := range terms {
		expanded = append(expanded, term)
		if synonyms, ok := f.synonymMap[term]; ok {
			expanded = append(expanded, synonyms...)
		}
	}

	return strings.Join(expanded, " OR ")
}

// buildSearchSQL builds the SQL query for search
func (f *EnhancedFTSStore) buildSearchSQL(query string, options *EnhancedSearchOptions) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	// Main FTS query
	baseQuery := `
		SELECT 
			m.id, m.session_id, m.turn_number, m.role, m.content, 
			m.content_type, m.tags, m.importance, m.created_at,
			rank,
			snippet(memories_fts, -1, '**', '**', '...', 64) as snippet
		FROM memories m
		JOIN memories_fts f ON m.id = f.rowid
		WHERE memories_fts MATCH ?
	`
	args = append(args, query)
	conditions = append(conditions, baseQuery)

	// Time range filter
	if options.TimeRange != nil {
		conditions[0] = strings.Replace(conditions[0], "WHERE", "WHERE m.created_at >= ? AND", 1)
		args = append(args, options.TimeRange.Start)
		conditions[0] = strings.Replace(conditions[0], "WHERE", "WHERE m.created_at <= ? AND", 1)
		args = append(args, options.TimeRange.End)
	}

	// Session filter
	if len(options.SessionFilter) > 0 {
		placeholders := make([]string, len(options.SessionFilter))
		for i, s := range options.SessionFilter {
			placeholders[i] = "?"
			args = append(args, s)
		}
		conditions[0] = strings.Replace(conditions[0], "WHERE", fmt.Sprintf("WHERE m.session_id IN (%s) AND", strings.Join(placeholders, ",")), 1)
	}

	// Content type filter
	if len(options.ContentTypes) > 0 {
		placeholders := make([]string, len(options.ContentTypes))
		for i, ct := range options.ContentTypes {
			placeholders[i] = "?"
			args = append(args, ct)
		}
		conditions[0] = strings.Replace(conditions[0], "WHERE", fmt.Sprintf("WHERE m.content_type IN (%s) AND", strings.Join(placeholders, ",")), 1)
	}

	// Importance filter
	if options.MinImportance > 0 {
		conditions[0] = strings.Replace(conditions[0], "WHERE", fmt.Sprintf("WHERE m.importance >= %d AND", options.MinImportance), 1)
	}

	// Assemble final query
	sql := strings.Join(conditions, " ") + `
		ORDER BY rank ASC
		LIMIT ? OFFSET ?
	`
	args = append(args, options.Limit, options.Offset)

	return sql, args
}

// recordStats records search statistics
func (f *EnhancedFTSStore) recordStats(query string, hits int, latencyMs int64) {
	f.statsMu.Lock()
	defer f.statsMu.Unlock()

	f.stats.TotalSearches++
	f.stats.TotalHits += int64(hits)
	
	// Running average of latency
	f.stats.AvgLatencyMs = (f.stats.AvgLatencyMs*float64(f.stats.TotalSearches-1) + float64(latencyMs)) / float64(f.stats.TotalSearches)
	
	// Track query counts
	f.stats.QueryCounts[query]++
	
	// Update top terms
	f.updateTopTerms(query)
}

// recordStat records a single stat
func (f *EnhancedFTSStore) recordStat(name string, value int64) {
	f.statsMu.Lock()
	defer f.statsMu.Unlock()

	switch name {
	case "cache_hit":
		f.stats.CacheHits += value
	case "cache_miss":
		f.stats.CacheMisses += value
	}
}

// updateTopTerms updates the top terms list
func (f *EnhancedFTSStore) updateTopTerms(query string) {
	terms := f.extractTerms(query)
	for _, term := range terms {
		found := false
		for i := range f.stats.TopTerms {
			if f.stats.TopTerms[i].Term == term {
				f.stats.TopTerms[i].Frequency++
				found = true
				break
			}
		}
		if !found {
			f.stats.TopTerms = append(f.stats.TopTerms, TermFrequency{Term: term, Frequency: 1})
		}
	}

	// Sort and limit
	sort.Slice(f.stats.TopTerms, func(i, j int) bool {
		return f.stats.TopTerms[i].Frequency > f.stats.TopTerms[j].Frequency
	})
	if len(f.stats.TopTerms) > 50 {
		f.stats.TopTerms = f.stats.TopTerms[:50]
	}
}

// GetRelatedMemories retrieves related memories via links
func (f *EnhancedFTSStore) GetRelatedMemories(memoryID int64, limit int) ([]EnhancedMemoryRecord, error) {
	if limit <= 0 {
		limit = 5
	}

	rows, err := f.db.Query(`
		SELECT 
			m.id, m.session_id, m.turn_number, m.role, m.content,
			m.content_type, m.tags, m.importance, m.created_at,
			m.summary, m.content_hash, m.parent_id, m.last_accessed, m.access_count,
			l.strength
		FROM memories m
		JOIN session_links l ON (m.id = l.source_id OR m.id = l.target_id)
		WHERE (l.source_id = ? OR l.target_id = ?) AND m.id != ?
		ORDER BY l.strength DESC
		LIMIT ?
	`, memoryID, memoryID, memoryID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []EnhancedMemoryRecord
	for rows.Next() {
		var r EnhancedMemoryRecord
		var tags string
		var strength float64

		err := rows.Scan(
			&r.ID, &r.SessionID, &r.TurnNumber, &r.Role, &r.Content,
			&r.ContentType, &tags, &r.Importance, &r.CreatedAt,
			&r.Summary, &r.ContentHash, &r.ParentID, &r.LastAccessed, &r.AccessCount,
			&strength,
		)
		if err != nil {
			continue
		}

		if tags != "" {
			r.Tags = strings.Split(tags, ",")
		}

		results = append(results, r)
	}

	return results, nil
}

// GetContext retrieves relevant context with cross-session linking
func (f *EnhancedFTSStore) GetContext(query string, maxTokens int) string {
	results, err := f.Search(query, &EnhancedSearchOptions{
		Limit:            5,
		UseSynonyms:      true,
		IncludeRelated:   true,
		MaxRelated:       3,
	})
	if err != nil || len(results) == 0 {
		return ""
	}

	var context strings.Builder
	totalTokens := 0

	for _, result := range results {
		// Include snippet
		snippet := result.Snippet
		if snippet == "" {
			snippet = result.Content
		}
		
		tokens := len(snippet) / 4 // Rough token estimate
		if totalTokens+tokens > maxTokens {
			break
		}

		context.WriteString(fmt.Sprintf("[Session %s, Turn %d] %s\n\n",
			result.SessionID, result.TurnNumber, snippet))
		totalTokens += tokens
	}

	return context.String()
}

// GetCrossSessionMemories retrieves memories from previous sessions
func (f *EnhancedFTSStore) GetCrossSessionMemories(query string, currentSession string, limit int) ([]SearchResult, error) {
	return f.Search(query, &EnhancedSearchOptions{
		Limit:           limit,
		SessionFilter:   []string{}, // Empty means all sessions except current
		UseSynonyms:     true,
		BoostRecent:     true,
	})
}

// get retrieves a memory by ID with access tracking
func (f *EnhancedFTSStore) get(id int) (*EnhancedMemoryRecord, error) {
	row := f.db.QueryRow(`
		SELECT 
			id, session_id, turn_number, role, content, content_type, 
			tags, importance, created_at, summary, content_hash, parent_id,
			last_accessed, access_count
		FROM memories WHERE id = ?
	`, id)

	var r EnhancedMemoryRecord
	var tags string

	err := row.Scan(
		&r.ID, &r.SessionID, &r.TurnNumber, &r.Role, &r.Content,
		&r.ContentType, &tags, &r.Importance, &r.CreatedAt,
		&r.Summary, &r.ContentHash, &r.ParentID, &r.LastAccessed, &r.AccessCount,
	)
	if err != nil {
		return nil, err
	}

	if tags != "" {
		r.Tags = strings.Split(tags, ",")
	}

	return &r, nil
}

// GetStats returns search statistics
func (f *EnhancedFTSStore) GetStats() *FTSStats {
	f.statsMu.RLock()
	defer f.statsMu.RUnlock()

	stats := *f.stats
	return &stats
}

// SetCacheEnabled enables or disables caching
func (f *EnhancedFTSStore) SetCacheEnabled(enabled bool) {
	f.cache.enabled = enabled
}

// ClearCache clears the search cache
func (f *EnhancedFTSStore) ClearCache() {
	f.cache.clear()
}

// SetDecayConfig updates the importance decay configuration
func (f *EnhancedFTSStore) SetDecayConfig(config *ImportanceDecayConfig) {
	f.decayConfig = config
}

// AddSynonym adds a synonym mapping
func (f *EnhancedFTSStore) AddSynonym(term string, synonyms []string) {
	f.synonymMap[term] = synonyms
}

// Cleanup removes old or low-importance memories
func (f *EnhancedFTSStore) Cleanup(olderThan time.Duration, minImportance int) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	
	result, err := f.db.Exec(`
		DELETE FROM memories 
		WHERE created_at < ? AND importance < ? AND access_count < 5
	`, cutoff, minImportance)
	
	if err != nil {
		return 0, err
	}
	
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// get returns a cached result
func (c *SearchCache) get(key string) ([]SearchResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.enabled {
		return nil, false
	}

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// Check TTL
	if time.Since(entry.created) > c.ttl {
		return nil, false
	}

	// Update LRU
	c.lru.MoveToFront(entry.element)
	entry.accesses++

	return entry.value, true
}

// set stores a result in cache
func (c *SearchCache) set(key string, value []SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return
	}

	// Check if entry exists
	if entry, ok := c.entries[key]; ok {
		entry.value = value
		entry.created = time.Now()
		c.lru.MoveToFront(entry.element)
		return
	}

	// Evict if full
	if c.lru.Len() >= c.maxSize {
		c.evictLRU()
	}

	// Add new entry
	entry = &CacheEntry{
		key:     key,
		value:   value,
		created: time.Now(),
	}
	entry.element = c.lru.PushFront(entry)
	c.entries[key] = entry
}

// evictLRU evicts the least recently used entry
func (c *SearchCache) evictLRU() {
	if c.lru.Len() == 0 {
		return
	}

	element := c.lru.Back()
	if element != nil {
		entry := c.lru.Remove(element).(*CacheEntry)
		delete(c.entries, entry.key)
	}
}

// clear clears all cache entries
func (c *SearchCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.lru = list.New()
}

// Close closes the FTS store
func (f *EnhancedFTSStore) Close() error {
	if f.decayTimer != nil {
		f.decayTimer.Stop()
	}
	return f.db.Close()
}

// GetDBStats returns database statistics
func (f *EnhancedFTSStore) GetDBStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total memories
	var totalMemories int
	f.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&totalMemories)
	stats["total_memories"] = totalMemories

	// Total size
	stat, err := os.Stat(f.dbPath)
	if err == nil {
		stats["db_size_bytes"] = stat.Size()
	}

	// Average importance
	var avgImportance float64
	f.db.QueryRow("SELECT AVG(importance) FROM memories").Scan(&avgImportance)
	stats["avg_importance"] = avgImportance

	// Memories by type
	typeCounts := make(map[string]int)
	rows, err := f.db.Query("SELECT content_type, COUNT(*) FROM memories GROUP BY content_type")
	if err == nil {
		for rows.Next() {
			var ct string
			var count int
			if rows.Scan(&ct, &count) == nil {
				typeCounts[ct] = count
			}
		}
		rows.Close()
	}
	stats["by_type"] = typeCounts

	// Links count
	var linksCount int
	f.db.QueryRow("SELECT COUNT(*) FROM session_links").Scan(&linksCount)
	stats["total_links"] = linksCount

	return stats, nil
}
