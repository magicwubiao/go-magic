// Package main demonstrates FTS memory capabilities
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/magicwubiao/go-magic/internal/memory"
)

func main() {
	fmt.Println("=== FTS Memory Demo ===\n")
	
	// Create enhanced FTS store
	store, err := memory.NewEnhancedFTSStore("./tmp/fts_demo")
	if err != nil {
		log.Fatalf("Failed to create FTS store: %v", err)
	}
	defer store.Close()
	
	// Add sample memories
	addSampleMemories(store)
	
	// Perform searches
	performSearches(store)
	
	// Get related memories
	getRelated(store)
	
	// Show statistics
	showStats(store)
}

func addSampleMemories(store *memory.EnhancedFTSStore) {
	fmt.Println("--- Adding Sample Memories ---")
	
	memories := []struct {
		session   string
		turn      int
		role      string
		content   string
		contentType string
		importance int
		tags      []string
	}{
		{"session-1", 1, "user", "How do I create a new Go project?", "text", 5, []string{"go", "project", "beginner"}},
		{"session-1", 2, "assistant", "You can use 'go mod init' to create a new Go module", "text", 8, []string{"go", "modules"}},
		{"session-1", 3, "user", "What about testing in Go?", "text", 6, []string{"go", "testing"}},
		{"session-1", 4, "assistant", "Go has built-in testing with 'testing' package and 'go test' command", "text", 9, []string{"go", "testing", "tdd"}},
		{"session-2", 1, "user", "I need to parse JSON in my application", "text", 7, []string{"json", "parsing"}},
		{"session-2", 2, "assistant", "Use encoding/json package with json.Unmarshal for parsing", "text", 8, []string{"json", "go", "parsing"}},
		{"session-3", 1, "user", "How do I handle errors gracefully?", "text", 7, []string{"error", "handling", "best-practices"}},
		{"session-3", 2, "assistant", "Return errors from functions and handle them with if err != nil pattern", "text", 9, []string{"error", "go", "patterns"}},
	}
	
	for _, m := range memories {
		record := &memory.EnhancedMemoryRecord{
			MemoryRecord: memory.MemoryRecord{
				SessionID:   m.session,
				TurnNumber:  m.turn,
				Role:        m.role,
				Content:     m.content,
				ContentType: m.contentType,
				Tags:        m.tags,
				Importance:  m.importance,
			},
		}
		
		if err := store.Add(record); err != nil {
			log.Printf("Failed to add memory: %v", err)
		} else {
			fmt.Printf("Added: [%s] %s\n", m.session, truncate(m.content, 50))
		}
	}
	
	// Test deduplication
	fmt.Println("\nTesting deduplication...")
	duplicate := &memory.EnhancedMemoryRecord{
		MemoryRecord: memory.MemoryRecord{
			SessionID:   "session-4",
			TurnNumber:  1,
			Role:        "user",
			Content:     "How do I create a new Go project?", // Same as session-1
			ContentType: "text",
			Tags:        []string{"go"},
			Importance:  5,
		},
	}
	
	if err := store.Add(duplicate); err != nil {
		fmt.Printf("Duplicate detected (expected): %v\n", err)
	}
}

func performSearches(store *memory.EnhancedFTSStore) {
	fmt.Println("\n--- Performing Searches ---")
	
	searches := []struct {
		query    string
		options  *memory.EnhancedSearchOptions
	}{
		{"testing", &memory.EnhancedSearchOptions{Limit: 5}},
		{"go", &memory.EnhancedSearchOptions{Limit: 5}},
		{"error handling", &memory.EnhancedSearchOptions{Limit: 5, UseSynonyms: true}},
		{"parsing", &memory.EnhancedSearchOptions{Limit: 5}},
	}
	
	for _, s := range searches {
		fmt.Printf("\nQuery: '%s'\n", s.query)
		
		results, err := store.Search(s.query, s.options)
		if err != nil {
			log.Printf("Search failed: %v", err)
			continue
		}
		
		fmt.Printf("Found %d results:\n", len(results))
		for _, r := range results {
			fmt.Printf("  - [%s] %s (rank: %.2f)\n", r.SessionID, r.Snippet, r.Rank)
		}
	}
	
	// Test context retrieval
	fmt.Println("\n--- Context Retrieval ---")
	context := store.GetContext("go project", 500)
	if context != "" {
		fmt.Printf("Retrieved context:\n%s\n", truncate(context, 200))
	}
}

func getRelated(store *memory.EnhancedFTSStore) {
	fmt.Println("\n--- Related Memories ---")
	
	// Get results first
	results, err := store.Search("testing", &memory.EnhancedSearchOptions{Limit: 1})
	if err != nil || len(results) == 0 {
		fmt.Println("No results to find related memories from")
		return
	}
	
	related, err := store.GetRelatedMemories(int64(results[0].ID), 3)
	if err != nil {
		log.Printf("Failed to get related memories: %v", err)
		return
	}
	
	fmt.Printf("Found %d related memories\n", len(related))
	for _, r := range related {
		fmt.Printf("  - %s\n", truncate(r.Content, 50))
	}
}

func showStats(store *memory.EnhancedFTSStore) {
	fmt.Println("\n--- Memory Statistics ---")
	
	// Get FTS stats
	stats := store.GetStats()
	fmt.Printf("Total searches: %d\n", stats.TotalSearches)
	fmt.Printf("Total hits: %d\n", stats.TotalHits)
	fmt.Printf("Avg latency: %.2fms\n", stats.AvgLatencyMs)
	fmt.Printf("Cache hit rate: %.1f%%\n", 
		float64(stats.CacheHits)/float64(stats.CacheHits+stats.CacheMisses)*100)
	
	// Get DB stats
	dbStats, err := store.GetDBStats()
	if err != nil {
		log.Printf("Failed to get DB stats: %v", err)
		return
	}
	
	fmt.Printf("Total memories: %d\n", dbStats["total_memories"])
	fmt.Printf("DB size: %.2f KB\n", float64(dbStats["db_size_bytes"].(int64))/1024)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func init() {
	// Seed random for jitter in enhanced store
	_ = time.Now()
}
