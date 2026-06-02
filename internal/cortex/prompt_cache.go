package cortex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// PromptCache implements Anthropic/OpenAI-style prompt caching
type PromptCache struct {
	mu           sync.RWMutex
	cacheDir     string
	cache        map[string]*CachedPrompt
	provider     provider.Provider
	maxCacheSize int
	ttl          time.Duration
}

// CachedPrompt represents a cached prompt with its cache key
type CachedPrompt struct {
	Key         string
	Prefix      string  // The cached prefix content
	CacheBreaks []int64 // Message indices where cache breaks occur
	CreatedAt   time.Time
	LastUsedAt  time.Time
	HitCount    int
	Tokens      int
}

// NewPromptCache creates a new prompt cache
func NewPromptCache(provider provider.Provider, cacheDir string) (*PromptCache, error) {
	pc := &PromptCache{
		cacheDir:     cacheDir,
		cache:        make(map[string]*CachedPrompt),
		provider:     provider,
		maxCacheSize: 100,
		ttl:          24 * time.Hour,
	}

	// Create cache directory
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	// Load existing cache from disk
	pc.loadFromDisk()

	return pc, nil
}

// CachePrefix generates a cache key and stores the prefix
// The prefix typically includes: system prompt, SOUL, MEMORY, USER, skills
func (pc *PromptCache) CachePrefix(ctx context.Context, prefixContent string) (string, error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Generate cache key from content hash
	key := pc.generateKey(prefixContent)

	// Check if already cached
	if cached, ok := pc.cache[key]; ok {
		cached.LastUsedAt = time.Now()
		cached.HitCount++
		pc.saveToDisk(key, cached)
		return key, nil
	}

	// Create new cache entry
	cached := &CachedPrompt{
		Key:        key,
		Prefix:     prefixContent,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		HitCount:   1,
		Tokens:     estimateTokens(prefixContent),
	}

	pc.cache[key] = cached

	// Evict old entries if needed
	pc.evictIfNeeded()

	// Save to disk
	pc.saveToDisk(key, cached)

	return key, nil
}

// GetCachedPrefix retrieves a cached prefix by key
func (pc *PromptCache) GetCachedPrefix(key string) (string, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	if cached, ok := pc.cache[key]; ok {
		// Check TTL
		if time.Since(cached.LastUsedAt) > pc.ttl {
			return "", false
		}
		cached.LastUsedAt = time.Now()
		return cached.Prefix, true
	}

	return "", false
}

// AddCacheBreak marks a message index as a cache break point
func (pc *PromptCache) AddCacheBreak(key string, messageIndex int64) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if cached, ok := pc.cache[key]; ok {
		cached.CacheBreaks = append(cached.CacheBreaks, messageIndex)
		return pc.saveToDisk(key, cached)
	}

	return fmt.Errorf("cache key not found: %s", key)
}

// BuildMessagesWithCache builds messages with cache control hints
// Returns messages with cache_control metadata for providers that support it
func (pc *PromptCache) BuildMessagesWithCache(
	systemPrefix string,
	memoryCtx string,
	userCtx string,
	conversation []provider.Message,
) ([]provider.Message, string, error) {
	// Combine prefix content
	prefixContent := systemPrefix + "\n\n" + memoryCtx + "\n\n" + userCtx

	// Get or create cache key
	key, err := pc.CachePrefix(context.Background(), prefixContent)
	if err != nil {
		return nil, "", err
	}

	// Build messages with cache headers
	messages := []provider.Message{
		{
			Role:    "system",
			Content: prefixContent + "\n<!-- cached: " + key + " -->",
		},
	}

	// Add conversation history
	messages = append(messages, conversation...)

	return messages, key, nil
}

// generateKey creates a SHA256 hash of the content
func (pc *PromptCache) generateKey(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// evictIfNeeded removes old cache entries
func (pc *PromptCache) evictIfNeeded() {
	if len(pc.cache) <= pc.maxCacheSize {
		return
	}

	// Find oldest entries
	type entry struct {
		key    string
		access time.Time
	}
	var entries []entry

	for key, cached := range pc.cache {
		entries = append(entries, entry{key, cached.LastUsedAt})
	}

	// Sort by access time (oldest first)
	// Simple bubble sort for small datasets
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].access.After(entries[j].access) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove oldest entries
	toRemove := len(entries) - pc.maxCacheSize + 10 // Keep some buffer
	for i := 0; i < toRemove && i < len(entries); i++ {
		delete(pc.cache, entries[i].key)
		pc.deleteFromDisk(entries[i].key)
	}
}

// saveToDisk saves a cache entry to disk
func (pc *PromptCache) saveToDisk(key string, cached *CachedPrompt) error {
	path := filepath.Join(pc.cacheDir, key+".json")

	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// loadFromDisk loads cache entries from disk
func (pc *PromptCache) loadFromDisk() {
	entries, err := os.ReadDir(pc.cacheDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		key := entry.Name()[:len(entry.Name())-5] // Remove .json
		path := filepath.Join(pc.cacheDir, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cached CachedPrompt
		if err := json.Unmarshal(data, &cached); err != nil {
			continue
		}

		// Check TTL
		if time.Since(cached.LastUsedAt) > pc.ttl {
			os.Remove(path)
			continue
		}

		pc.cache[key] = &cached
	}
}

// deleteFromDisk removes a cache entry from disk
func (pc *PromptCache) deleteFromDisk(key string) {
	path := filepath.Join(pc.cacheDir, key+".json")
	os.Remove(path)
}

// GetCacheStats returns cache statistics
func (pc *PromptCache) GetCacheStats() map[string]interface{} {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	var totalTokens, totalHits int
	oldestAccess := time.Now()
	newestAccess := time.Time{}

	for _, cached := range pc.cache {
		totalTokens += cached.Tokens
		totalHits += cached.HitCount
		if cached.LastUsedAt.Before(oldestAccess) {
			oldestAccess = cached.LastUsedAt
		}
		if cached.LastUsedAt.After(newestAccess) {
			newestAccess = cached.LastUsedAt
		}
	}

	return map[string]interface{}{
		"entries":       len(pc.cache),
		"total_tokens":  totalTokens,
		"total_hits":    totalHits,
		"oldest_access": oldestAccess,
		"newest_access": newestAccess,
		"cache_dir":     pc.cacheDir,
	}
}

// Clear removes all cache entries
func (pc *PromptCache) Clear() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	for key := range pc.cache {
		pc.deleteFromDisk(key)
	}
	pc.cache = make(map[string]*CachedPrompt)

	return nil
}

// estimateTokens estimates token count (rough approximation)
func estimateTokens(text string) int {
	// Rough approximation: ~4 chars per token for English
	return len(text) / 4
}
