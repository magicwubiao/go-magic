package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// 懒连接（lazy）与工具清单缓存。
//
// 背景：ConnectConfigured 是"并发连接 + 等全部完成"的语义，连接器一多，
// 冷启动就被最慢的那个拖着；某个 server 卡在握手时还会让整轮启动等到超时。
//
// 做法：lazy 的 server 启动时不建连，只用上一次成功连接时缓存下来的
// tools/list 结果注册"影子工具"（名字、描述、schema 都是真的，只是背后
// 没有进程）。模型真去调它时，CallTool 先 EnsureConnected 再转发。
//
// 为什么可以安全地这么干：MCP 的 tools/list 对同一个 server 版本是稳定的，
// 缓存只是"上次看到的清单"，不是另一份事实。真正的事实仍然是连接后的
// tools/list，连上之后会刷新缓存。

// toolCacheVersion 变更时旧缓存直接丢弃（宁可从零重连一次，也不要让
// 结构不兼容的旧数据把启动搞出 panic）。
const toolCacheVersion = 1

// toolCacheFile 是落盘格式。
type toolCacheFile struct {
	Version int               `json:"version"`
	Updated time.Time         `json:"updated"`
	Servers map[string][]Tool `json:"servers"`
}

// SetToolCachePath 注入工具清单缓存路径（通常 <magicHome>/mcp/tool_cache.json）
// 并立即加载已有缓存。
//
// 与 SetTokenStore 同样的理由：本包不能 import pkg/config，路径由调用方注入。
// 加载失败不阻塞启动——缓存只是加速手段，读不到就当作没有。
func (m *Manager) SetToolCachePath(path string) error {
	if path == "" {
		return fmt.Errorf("tool cache path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create tool cache dir: %w", err)
	}

	cached, err := loadToolCache(path)
	if err != nil {
		log.Warnf("[MCP] tool cache at %s is unusable, starting empty: %v", path, err)
		cached = make(map[string][]Tool)
	}

	m.mu.Lock()
	m.toolCachePath = path
	m.cached = cached
	m.mu.Unlock()
	return nil
}

// EnsureConnected 让一个懒连接的 server 立刻建连（幂等）。
//
// 并发首次调用会串行在同一把 per-server 锁上：否则两个工具调用会各起一个
// stdio 子进程，先到的一个赢、另一个被 connect 的竞争检查丢掉并关闭 transport，
// 白白付出一次握手成本。
func (m *Manager) EnsureConnected(name string) error {
	if m.isConnected(name) {
		return nil
	}

	unlock := m.lazyLocks.Lock(name)
	defer unlock()

	// 双检：等锁期间可能已经被另一个调用连上了。
	if m.isConnected(name) {
		return nil
	}

	m.mu.RLock()
	cfg, ok := m.deferred[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("MCP server '%s' is not configured for lazy connect", name)
	}

	log.Infof("[MCP] lazy connect on demand: %s", name)
	if err := m.connect(name, cfg); err != nil {
		// 保留 deferred 记录：下一次调用还能重试，不能因为一次网络抖动
		// 就让这个 server 在当前进程里永久失效。
		return err
	}

	m.mu.Lock()
	delete(m.deferred, name)
	m.mu.Unlock()
	return nil
}

// IsDeferred 报告某个 server 是否处于"已登记但未连接"的懒加载状态。
func (m *Manager) IsDeferred(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.deferred[name]
	return ok
}

// deferServer 把 server 登记为懒连接并清掉可能残留的活连接配置。
func (m *Manager) deferServer(name string, cfg ServerConfig) {
	m.mu.Lock()
	m.deferred[name] = cfg
	delete(m.clients, name)
	m.mu.Unlock()
}

// shouldDefer 判断是否真的可以"先不连"。
//
// 必须同时满足两个条件：配置声明了 lazy，且缓存里有工具清单。没有缓存的
// lazy server 不能延后——工具面为空，模型根本看不到它，也就永远不会触发
// 首次调用，等于这个连接器装了个寂寞。这种情况下先连一次把缓存建立起来，
// 之后的启动才是懒的。
func (m *Manager) shouldDefer(name string, cfg ServerConfig) bool {
	if !cfg.Lazy {
		return false
	}
	return len(m.cachedTools(name)) > 0
}

// undefer 在 server 改为非懒连接（或从配置里删除）时清掉登记。
func (m *Manager) undefer(name string) {
	m.mu.Lock()
	delete(m.deferred, name)
	m.mu.Unlock()
}

// rememberTools 把某个 server 当前的工具清单写进缓存（连接成功 / 刷新后调用）。
func (m *Manager) rememberTools(name string, tools []Tool) {
	if len(tools) == 0 {
		return
	}

	m.mu.Lock()
	if m.cached == nil {
		m.cached = make(map[string][]Tool)
	}
	m.cached[name] = tools
	path := m.toolCachePath
	snapshot := m.snapshotCacheLocked()
	m.mu.Unlock()

	if path == "" {
		return
	}
	if err := writeToolCache(path, snapshot); err != nil {
		// 缓存写失败不影响连接本身，只影响下次冷启动的快照质量。
		log.Warnf("[MCP] failed to write tool cache %s: %v", path, err)
	}
}

// pruneCache 丢掉配置里已不存在的 server 的过期缓存（避免文件无限增长，
// 也避免"删掉的连接器"在启动时冒出一批影子工具）。
// keep 为空表示不做裁剪。
func (m *Manager) pruneCache(keep map[string]struct{}) {
	if keep == nil {
		return
	}

	m.mu.Lock()
	changed := false
	for name := range m.cached {
		if _, ok := keep[name]; !ok {
			delete(m.cached, name)
			changed = true
		}
	}
	path := m.toolCachePath
	var snapshot toolCacheFile
	if changed {
		snapshot = m.snapshotCacheLocked()
	}
	m.mu.Unlock()

	if !changed || path == "" {
		return
	}
	if err := writeToolCache(path, snapshot); err != nil {
		log.Warnf("[MCP] failed to prune tool cache %s: %v", path, err)
	}
}

// cachedTools 返回某个 server 的缓存工具（浅拷贝切片，元素本身只读）。
func (m *Manager) cachedTools(name string) []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tools, ok := m.cached[name]
	if !ok {
		return nil
	}
	out := make([]Tool, len(tools))
	copy(out, tools)
	return out
}

// CachedServers 返回缓存里有工具清单的 server 名（诊断用）。
func (m *Manager) CachedServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.cached))
	for name := range m.cached {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// snapshotCacheLocked 复制一份可落盘的缓存（调用方必须持有 m.mu）。
func (m *Manager) snapshotCacheLocked() toolCacheFile {
	servers := make(map[string][]Tool, len(m.cached))
	for name, tools := range m.cached {
		cp := make([]Tool, len(tools))
		copy(cp, tools)
		servers[name] = cp
	}
	return toolCacheFile{Version: toolCacheVersion, Updated: time.Now(), Servers: servers}
}

func loadToolCache(path string) (map[string][]Tool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string][]Tool), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return make(map[string][]Tool), nil
	}

	var f toolCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("invalid cache JSON: %w", err)
	}
	if f.Version != toolCacheVersion {
		return nil, fmt.Errorf("cache version %d, want %d", f.Version, toolCacheVersion)
	}
	if f.Servers == nil {
		f.Servers = make(map[string][]Tool)
	}
	return f.Servers, nil
}

// writeToolCache 原子写（temp + rename）：半截 JSON 会让下次启动直接丢缓存，
// 而缓存文件恰恰是在"上次好好的、这次要快"的时候最有用。
func writeToolCache(path string, f toolCacheFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tool_cache-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		// Windows 上 Chmod 语义有限，失败不致命。
		log.Debugf("[MCP] chmod tool cache: %v", err)
	}
	return os.Rename(tmpName, path)
}

// keyedMutex 是一组按 key 分配的互斥锁，用于"同一个 server 只连一次"。
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Lock 锁住 key 并返回解锁函数。
func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*sync.Mutex)
	}
	lock, ok := k.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		k.locks[key] = lock
	}
	k.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}
