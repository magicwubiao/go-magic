package tool

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var (
	sessionSearchTool     *SessionSearchTool
	sessionSearchToolOnce sync.Once
)

// GetSessionSearchTool creates a new session search tool. The underlying
// sessions.db is opened lazily on first use so that merely registering the
// tool (e.g. in tests with a temp GO_MAGIC_HOME) does not pin the SQLite
// file open and break TempDir cleanup on Windows.
func GetSessionSearchTool() *SessionSearchTool {
	sessionSearchToolOnce.Do(func() {
		sessionSearchTool = &SessionSearchTool{}
	})
	return sessionSearchTool
}

// SessionSearchTool searches through past conversation sessions
type SessionSearchTool struct {
	storeOnce sync.Once
	store     *session.Store
	storeErr  error
}

// getStore lazily opens the sessions.db store on first use.
func (t *SessionSearchTool) getStore() (*session.Store, error) {
	t.storeOnce.Do(func() {
		dbPath := filepath.Join(config.GetMagicHome(), "sessions.db")
		store, err := session.NewStore(dbPath)
		if err != nil {
			t.storeErr = err
			return
		}
		t.store = store
	})
	return t.store, t.storeErr
}

// Name returns the tool name
func (t *SessionSearchTool) Name() string {
	return "session_search"
}

// Description returns the tool description
func (t *SessionSearchTool) Description() string {
	return "Search through past conversation sessions. Use this to find previous discussions, decisions, or code snippets from earlier conversations."
}

// Parameters returns the tool parameters schema
func (t *SessionSearchTool) Schema() map[string]interface{} { return t.Parameters() }

func (t *SessionSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query to find relevant sessions",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of results to return (default: 5)",
			},
			"platform": map[string]interface{}{
				"type":        "string",
				"description": "Filter by platform (e.g., cli, telegram, discord)",
			},
			"after": map[string]interface{}{
				"type":        "string",
				"description": "Only show sessions after this date (RFC3339 format)",
			},
			"before": map[string]interface{}{
				"type":        "string",
				"description": "Only show sessions before this date (RFC3339 format)",
			},
		},
		"required": []string{"query"},
	}
}

// Execute performs the session search
func (t *SessionSearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Get current profile's sessions
	profile := "default"
	if p, ok := args["profile"].(string); ok && p != "" {
		profile = p
	}

	store, err := t.getStore()
	if err != nil || store == nil {
		return nil, fmt.Errorf("session store unavailable: %v", err)
	}

	sessions, err := store.ListSessions(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %v", err)
	}

	// Filter by date range if specified
	if afterStr, ok := args["after"].(string); ok && afterStr != "" {
		if after, err := time.Parse(time.RFC3339, afterStr); err == nil {
			filtered := make([]*session.Session, 0)
			for _, s := range sessions {
				if s.CreatedAt.After(after) {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}
	}
	if beforeStr, ok := args["before"].(string); ok && beforeStr != "" {
		if before, err := time.Parse(time.RFC3339, beforeStr); err == nil {
			filtered := make([]*session.Session, 0)
			for _, s := range sessions {
				if s.CreatedAt.Before(before) {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}
	}

	// Search through sessions
	type SearchResult struct {
		ID        string    `json:"id"`
		Platform  string    `json:"platform"`
		CreatedAt time.Time `json:"created_at"`
		Snippet   string    `json:"snippet,omitempty"`
		Score     float64   `json:"score,omitempty"`
	}

	results := make([]SearchResult, 0)
	queryLower := strings.ToLower(query)

	for _, s := range sessions {
		// Search in messages
		var bestSnippet string
		var maxScore float64

		for _, msg := range s.Messages {
			content := strings.ToLower(msg.Content)
			if strings.Contains(content, queryLower) {
				// Calculate simple relevance score
				score := float64(strings.Count(content, queryLower)) / float64(len(content)+1)

				if score > maxScore {
					maxScore = score
					// Extract snippet around the match
					idx := strings.Index(content, queryLower)
					start := idx - 50
					if start < 0 {
						start = 0
					}
					end := idx + len(queryLower) + 50
					if end > len(msg.Content) {
						end = len(msg.Content)
					}
					bestSnippet = msg.Content[start:end]
					if start > 0 {
						bestSnippet = "..." + bestSnippet
					}
					if end < len(msg.Content) {
						bestSnippet = bestSnippet + "..."
					}
				}
			}
		}

		if maxScore > 0 {
			results = append(results, SearchResult{
				ID:        s.ID,
				Platform:  s.Platform,
				CreatedAt: s.CreatedAt,
				Snippet:   bestSnippet,
				Score:     maxScore,
			})

			if len(results) >= 5 {
				break
			}
		}
	}

	return map[string]interface{}{
		"total":   len(results),
		"query":   query,
		"results": results,
	}, nil
}
