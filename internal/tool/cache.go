package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ToolCache 工具结果缓存接口
type ToolCache interface {
	// Get 获取缓存结果
	Get(key string) (*ToolResult, bool)
	// Set 设置缓存结果
	Set(key string, result *ToolResult, ttl time.Duration)
	// Delete 删除缓存
	Delete(key string)
	// Clear 清空缓存
	Clear()
	// Size 返回缓存条目数
	Size() int
}

// InMemoryToolCache 内存缓存实现
type InMemoryToolCache struct {
	mu       sync.RWMutex
	items    map[string]*cacheItem
	maxSize  int
	evictFn  func(key string, item *cacheItem)
}

// cacheItem 缓存条目
type cacheItem struct {
	value      *ToolResult
	expiration time.Time
	createdAt  time.Time
	accessCount int
}

// NewInMemoryCache 创建内存缓存
func NewInMemoryCache(maxSize int) *InMemoryToolCache {
	cache := &InMemoryToolCache{
		items:   make(map[string]*cacheItem),
		maxSize: maxSize,
	}
	
	// 启动过期清理
	go cache.cleanupExpired()
	
	return cache
}

func (c *InMemoryToolCache) Get(key string) (*ToolResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	item, exists := c.items[key]
	if !exists {
		return nil, false
	}
	
	// 检查是否过期
	if time.Now().After(item.expiration) {
		delete(c.items, key)
		return nil, false
	}
	
	// 更新访问计数
	item.accessCount++
	
	return item.value, true
}

func (c *InMemoryToolCache) Set(key string, result *ToolResult, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// 检查缓存大小，必要时淘汰
	if len(c.items) >= c.maxSize {
		c.evictLRU()
	}
	
	c.items[key] = &cacheItem{
		value:       result,
		expiration:  time.Now().Add(ttl),
		createdAt:   time.Now(),
		accessCount: 0,
	}
}

func (c *InMemoryToolCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *InMemoryToolCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*cacheItem)
}

func (c *InMemoryToolCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// evictLRU 淘汰最少使用的条目
func (c *InMemoryToolCache) evictLRU() {
	var oldestKey string
	var oldestCount int = -1
	
	for key, item := range c.items {
		if oldestCount == -1 || item.accessCount < oldestCount {
			oldestKey = key
			oldestCount = item.accessCount
		}
	}
	
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

// cleanupExpired 清理过期条目
func (c *InMemoryToolCache) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, item := range c.items {
			if now.After(item.expiration) {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
	}
}

// ============================================================================
// Cache Key Builder
// ============================================================================

// CacheKeyBuilder 缓存 key 构建器
type CacheKeyBuilder struct {
	prefix string
}

func NewCacheKeyBuilder(prefix string) *CacheKeyBuilder {
	return &CacheKeyBuilder{prefix: prefix}
}

func (b *CacheKeyBuilder) Build(toolName string, params map[string]any) string {
	// 将 params 序列化为 JSON
	paramJSON, err := json.Marshal(params)
	if err != nil {
		// 如果序列化失败，使用简单拼接
		key := toolName
		for k, v := range params {
			key += fmt.Sprintf(":%s=%v", k, v)
		}
		return b.hashKey(key)
	}
	
	// 构建完整 key
	rawKey := toolName + ":" + string(paramJSON)
	return b.hashKey(rawKey)
}

func (b *CacheKeyBuilder) hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return b.prefix + ":" + hex.EncodeToString(hash[:16])
}

// ============================================================================
// TTLCache - TTL 缓存实现（基于过期时间）
// ============================================================================

type TTLCache struct {
	mu      sync.RWMutex
	items   map[string]*cacheItem
	defaultTTL time.Duration
}

func NewTTLCache(defaultTTL time.Duration) *TTLCache {
	cache := &TTLCache{
		items:      make(map[string]*cacheItem),
		defaultTTL: defaultTTL,
	}
	go cache.cleanupExpired()
	return cache
}

func (c *TTLCache) Get(key string) (*ToolResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	item, exists := c.items[key]
	if !exists || time.Now().After(item.expiration) {
		return nil, false
	}
	
	return item.value, true
}

func (c *TTLCache) Set(key string, result *ToolResult, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if ttl == 0 {
		ttl = c.defaultTTL
	}
	
	c.items[key] = &cacheItem{
		value:      result,
		expiration: time.Now().Add(ttl),
		createdAt:  time.Now(),
	}
}

func (c *TTLCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *TTLCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*cacheItem)
}

func (c *TTLCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *TTLCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, item := range c.items {
			if now.After(item.expiration) {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
	}
}

// ============================================================================
// Cache Statistics
// ============================================================================

// CacheStats 缓存统计
type CacheStats struct {
	Hits       int64
	Misses     int64
	Evictions  int64
	Items      int
	MemoryUsed int64
}

// CachedToolExecutor 带有缓存的执行器包装
type CachedToolExecutor struct {
	registry *Registry
	cache    ToolCache
	keyBuilder *CacheKeyBuilder
}

// NewCachedToolExecutor 创建缓存执行器
func NewCachedToolExecutor(registry *Registry, cache ToolCache) *CachedToolExecutor {
	return &CachedToolExecutor{
		registry:   registry,
		cache:      cache,
		keyBuilder: NewCacheKeyBuilder("tool"),
	}
}

// ExecuteCached 带缓存执行
func (e *CachedToolExecutor) ExecuteCached(ctx context.Context, toolName string, params map[string]any, ttl time.Duration) (*ToolResult, error) {
	// 构建缓存 key
	cacheKey := e.keyBuilder.Build(toolName, params)
	
	// 尝试获取缓存
	if cached, found := e.cache.Get(cacheKey); found {
		return cached, nil
	}
	
	// 执行工具
	tool, err := e.registry.Get(toolName)
	if err != nil {
		return nil, err
	}
	
	start := time.Now()
	result, err := tool.Execute(ctx, params)
	duration := time.Since(start)
	
	if err != nil {
		return &ToolResult{
			Success:  false,
			Error:    err.Error(),
			Duration: duration,
		}, nil
	}
	
	toolResult := &ToolResult{
		Success:  true,
		Data:     result,
		Duration: duration,
		Metadata: map[string]any{
			"tool_name": toolName,
			"cached":    false,
		},
	}
	
	// 缓存结果
	e.cache.Set(cacheKey, toolResult, ttl)
	
	return toolResult, nil
}

// InvalidateCache 使缓存失效
func (e *CachedToolExecutor) InvalidateCache(toolName string) {
	// 简单实现：清空所有缓存
	// 实际生产中应该按 toolName 模式匹配删除
	e.cache.Clear()
}
