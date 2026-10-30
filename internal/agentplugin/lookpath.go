package agentplugin

// 命令存在性探测 + 短 TTL 缓存。
//
// 为什么需要缓存：Windows 上 LookPath 找不到命令时要扫完整条 PATH，单次可达
// 几百毫秒；插件页每次刷新都要为每个 CLI 连接器探一次，十几个条目串行就是
// 好几秒（"CLI 应用渲染慢"的老问题）。缓存 30 秒，安装完成后用
// InvalidateCommandCache 立刻失效。

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

// commandCacheTTL 是探测结果的有效期：足够挡住页面连续刷新，又不至于
// 让用户装完命令后盯着"未安装"发呆（安装路径还会显式失效缓存）。
const commandCacheTTL = 30 * time.Second

type commandCacheEntry struct {
	available bool
	expires   time.Time
}

var commandCache = struct {
	mu sync.Mutex
	m  map[string]commandCacheEntry
}{m: map[string]commandCacheEntry{}}

// CommandAvailable 报告命令是否能从 PATH 上找到（带缓存）。
func CommandAvailable(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false
	}
	now := time.Now()

	commandCache.mu.Lock()
	if e, ok := commandCache.m[cmd]; ok && now.Before(e.expires) {
		commandCache.mu.Unlock()
		return e.available
	}
	commandCache.mu.Unlock()

	// 只有单个 path 值的命令（如 "docker"）才查 PATH；带空格的命令是模板
	// 片段，交给连接器自己在执行时报错，这里不猜。
	_, err := exec.LookPath(cmd)
	available := err == nil

	commandCache.mu.Lock()
	commandCache.m[cmd] = commandCacheEntry{available: available, expires: now.Add(commandCacheTTL)}
	commandCache.mu.Unlock()
	return available
}

// PrimeCommands 并行预热一组命令的存在性。
//
// 串行探测在 Windows 上会把插件列表拖到秒级；并发探完再顺序读缓存，
// 总耗时等于最慢的一条。
func PrimeCommands(commands []string) {
	seen := make(map[string]bool, len(commands))
	var wg sync.WaitGroup
	for _, raw := range commands {
		cmd := strings.TrimSpace(raw)
		if cmd == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			CommandAvailable(c)
		}(cmd)
	}
	wg.Wait()
}

// InvalidateCommandCache 清空探测缓存（安装 / 卸载 CLI 之后调用）。
func InvalidateCommandCache() {
	commandCache.mu.Lock()
	commandCache.m = map[string]commandCacheEntry{}
	commandCache.mu.Unlock()
}
