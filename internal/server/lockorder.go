package server

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/magicwubiao/go-magic/internal/agent"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 加锁顺序守卫。
//
// Server 上两把锁的顺序契约是 **s.mu → s.agentsMu**：
// syncConfigFromDisk 先拿 s.mu 再清 agent 缓存；旧代码里 handleModelSet
// 在持 s.mu 时进入 syncConfigFromDisk，于是反向路径（approval.go 审批广播、
// sessions.go 设置工作目录）持 s.agentsMu 去读 s.cfg 时就形成 AB-BA 死锁——
// 线上表现为"chat 页切完模型、再打开模型供应商页，整个后端卡死到重启"。
//
// 死锁只在特定交错下复现，靠测试碰运气不可靠。这里把"反序持有"变成
// 确定性失败：反序会立即 panic 并打出调用栈。
//
// 用 goroutine id 做精确判定，而不是"任意 goroutine 持 agentsMu"这种全局
// 粗判——后者在正常并发下会误报（A 持 agentsMu 读 cfg、B 持 s.mu 重建
// provider 是完全合法的组合）。守卫只在测试/调试下启用（strictLockOrder），
// 生产默认关闭。
type lockOrderGuard struct {
	// muDepth 统计当前持有 s.mu 的 goroutine 数（RWMutex 可能多个读者）。
	muDepth int64
	// muHolders 记录持 s.mu 的 goroutine id。用 map 而非单值：s.mu 是
	// RWMutex，允许多读者并存，单值会被后进入者覆盖。
	muHoldersMu sync.Mutex
	muHolders   map[int64]int
	// agentsHolders 记录当前持 agentsMu 的 goroutine id 集合。
	agentsHoldersMu sync.Mutex
	agentsHolders   map[int64]int
}

func (g *lockOrderGuard) addHolder(m *sync.Mutex, set *map[int64]int, gid int64) {
	m.Lock()
	if *set == nil {
		*set = make(map[int64]int)
	}
	(*set)[gid]++
	m.Unlock()
}

func (g *lockOrderGuard) removeHolder(m *sync.Mutex, set *map[int64]int, gid int64) {
	m.Lock()
	if (*set)[gid] <= 1 {
		delete(*set, gid)
	} else {
		(*set)[gid]--
	}
	m.Unlock()
}

func (g *lockOrderGuard) heldBy(m *sync.Mutex, set *map[int64]int, gid int64) bool {
	m.Lock()
	defer m.Unlock()
	return (*set)[gid] > 0
}

func (g *lockOrderGuard) addAgentsHolder(gid int64) {
	g.addHolder(&g.agentsHoldersMu, &g.agentsHolders, gid)
}

func (g *lockOrderGuard) removeAgentsHolder(gid int64) {
	g.removeHolder(&g.agentsHoldersMu, &g.agentsHolders, gid)
}

func (g *lockOrderGuard) agentsHeldBy(gid int64) bool {
	return g.heldBy(&g.agentsHoldersMu, &g.agentsHolders, gid)
}

func (g *lockOrderGuard) addMuHolder(gid int64) {
	g.addHolder(&g.muHoldersMu, &g.muHolders, gid)
}

func (g *lockOrderGuard) removeMuHolder(gid int64) {
	g.removeHolder(&g.muHoldersMu, &g.muHolders, gid)
}

func (g *lockOrderGuard) muHeldBy(gid int64) bool {
	return g.heldBy(&g.muHoldersMu, &g.muHolders, gid)
}

// curGoroutineID 从 runtime.Stack 头行解析 goroutine id。
// 只用于加锁顺序断言（调试路径），开销可接受。
func curGoroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// 形如 "goroutine 123 [running]:"
	fields := strings.Fields(string(buf[:n]))
	if len(fields) < 2 {
		return -1
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return -1
	}
	return id
}

// strictLockOrder 由测试置位，开启反序断言。
var strictLockOrder atomic.Bool

// EnableStrictLockOrder 打开加锁顺序断言。供测试与本地排障使用。
func EnableStrictLockOrder(on bool) { strictLockOrder.Store(on) }

// cfgWriteGen 每次 setCfg 自增。用于并发测试断言"配置替换与读取没有
// 交错到数据竞态"——本机跑不了 -race，靠这个计数器做廉价观测点。
var cfgWriteGen atomic.Int64

// cfgWriteCount 返回 s.cfg 被整体替换的累计次数（测试观测用）。
func cfgWriteCount() int64 { return cfgWriteGen.Load() }

// acquireServerMu / releaseServerMu 是获取 s.mu 的统一入口。
//
// 除了记账，还会在真正加锁**之前**断言：本 goroutine 当前没有持 agentsMu
// （否则 s.agentsMu → s.mu 的反序已经发生，必然与 syncConfigFromDisk 的
// s.mu → s.agentsMu 撞成 AB-BA 死锁）。
func (s *Server) acquireServerMu(caller string) {
	s.checkBeforeServerMu(caller)
	atomic.AddInt64(&s.lockOrder.muDepth, 1)
	if strictLockOrder.Load() {
		s.lockOrder.addMuHolder(curGoroutineID())
	}
	s.mu.Lock()
}

func (s *Server) releaseServerMu() {
	s.mu.Unlock()
	if strictLockOrder.Load() {
		s.lockOrder.removeMuHolder(curGoroutineID())
	}
	atomic.AddInt64(&s.lockOrder.muDepth, -1)
}

// acquireAgentsMu / releaseAgentsMu 是访问 s.agents 的唯一加锁入口。
//
// 顺序契约 **s.mu → s.agentsMu**：持 s.mu 时拿 agentsMu 是合法正向顺序，
// 反过来"持 agentsMu 再去拿 s.mu"才会死锁。
//
// 这里额外拦一种真实踩过的写法：**同一个 goroutine 先持 agentsMu，再进
// 一个会去拿 s.mu 的函数**。此时若 s.mu 的持有者就是本 goroutine，
// 说明它一边持 agentsMu 一边拿了 s.mu —— 反序。用 goroutine id 判定，
// 不依赖跨 goroutine 的全局计数（那种做法会误报）。
func (s *Server) acquireAgentsMu() {
	if strictLockOrder.Load() {
		if s.lockOrder.agentsHeldBy(curGoroutineID()) {
			panic("lockorder violation: this goroutine entered the agentsMu critical section twice (self-deadlock)")
		}
	}
	s.lockOrder.addAgentsHolder(curGoroutineID())
	s.agentsMu.Lock()
}

func (s *Server) releaseAgentsMu() {
	s.agentsMu.Unlock()
	s.lockOrder.removeAgentsHolder(curGoroutineID())
}

// checkBeforeServerMu 在获取 s.mu 之前调用：**本 goroutine** 若正持
// agentsMu，就不允许再拿 s.mu —— 那是 s.agentsMu → s.mu 的反序，
// 与 syncConfigFromDisk 的 s.mu → s.agentsMu 撞成 AB-BA 死锁。
//
// 用 goroutine id 精确判定，不做"任意 goroutine 持 agentsMu"的全局判断：
// 后者在正常并发下会误报（A 持 agentsMu 读 cfg、B 持 s.mu 重建 provider
// 是完全合法的组合）。
func (s *Server) checkBeforeServerMu(caller string) {
	if !strictLockOrder.Load() {
		return
	}
	if !s.lockOrder.agentsHeldBy(curGoroutineID()) {
		return
	}
	if caller == "" {
		caller = callerName()
	}
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	panic(fmt.Sprintf(
		"lockorder violation: acquiring s.mu while this goroutine holds agentsMu (%s) — "+
			"the order must be s.mu -> s.agentsMu, otherwise AB-BA deadlock.\n%s",
		caller, buf[:n]))
}

// callerName 取调用方函数名，让 panic 信息不必依赖各调用点手写标签。
func callerName() string {
	pc, _, _, ok := runtime.Caller(3)
	if !ok {
		return "unknown"
	}
	return runtime.FuncForPC(pc).Name()
}

// assertNotHoldingServerMu 断言本 goroutine 当前没有持 s.mu。用于
// syncConfigFromDisk 这类"内部要拿 agentsMu"的函数：调用方持 s.mu 就会
// 把 s.mu → s.agentsMu 的顺序带进来，与反向路径构成 AB-BA 死锁。
func (s *Server) assertNotHoldingServerMu(caller string) {
	if !strictLockOrder.Load() {
		return
	}
	if !s.lockOrder.muHeldBy(curGoroutineID()) {
		return
	}
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	panic(fmt.Sprintf(
		"lockorder violation: %s called while holding s.mu (it acquires agentsMu internally) — "+
			"release s.mu first, otherwise it forms an AB-BA deadlock with the reverse path.\n%s",
		caller, buf[:n]))
}

// clearAgents 清空按会话缓存的 agent（下一次对话重新构建，拿到新的
// provider/配置）。这是清空 agent 缓存的唯一入口。
//
// 只拿 agentsMu，绝不嵌套 s.mu —— 加锁顺序锁死为
// **s.mu → s.agentsMu**（见 syncConfigFromDisk），反向即 AB-BA 死锁。
func (s *Server) clearAgents() {
	s.acquireAgentsMu()
	s.agents = make(map[string]*agent.Agent)
	s.releaseAgentsMu()
}

// setCfg 替换内存里的整份配置快照。**所有 `s.cfg = ...` 写点都必须走这里**，
// 否则读取点（很多在 s.mu / s.agentsMu 之外）会与写入并发读到撕裂的指针。
//
// 只拿 cfgMu（叶子锁），因此可以从 s.mu 临界区内部安全调用。
func (s *Server) setCfg(cfg *appconfig.Config) {
	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgMu.Unlock()
	cfgWriteGen.Add(1)
}

// cfgSnapshot 返回当前配置快照的指针。
//
// 调用方拿到的是一个**不可变快照**：写入侧永远整体替换指针、绝不原地改
// 已有对象（handleConfig PUT 的合并分支是唯一例外，它在 s.mu 内原地
// 改写自己的快照，此时不会有人同时持有它）。因此拿到指针后可以自由
// 读取其中的字段，无需继续持 cfgMu。
//
// 注意：返回 nil 是合法的（尚未加载配置），调用方仍需判空。
func (s *Server) cfgSnapshot() *appconfig.Config {
	s.cfgMu.Lock()
	cfg := s.cfg
	s.cfgMu.Unlock()
	return cfg
}
