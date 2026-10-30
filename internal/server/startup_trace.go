package server

import (
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// 启动耗时追踪。
//
// 背景：`magic server` 先执行 NewServer（配置、SQLite、技能、插件、cortex…），
// 全部返回后才 ListenAndServe。任何一处同步慢操作（插件 MCP 拉起 npx 子进程、
// 大 WAL 的 SQLite 打开、技能/记忆库扫描）都直接表现为"端口很久才通"，而
// 启动期此前一条日志都没有，只能靠猜。
//
// 这里给每个阶段打一条 "[startup]" 日志（毫秒级），下次启动就能一眼看到
// 时间花在哪儿。日志量是"每次启动十几行"，不构成噪声。

// processStart 是包初始化时刻，用来算"进程启动 → 端口可连"的总耗时。
var processStart = time.Now()

type startupTrace struct {
	began time.Time
	last  time.Time
}

func newStartupTrace() *startupTrace {
	now := time.Now()
	return &startupTrace{began: now, last: now}
}

// mark 记录一个阶段的耗时（距上一阶段 + 距进程启动累计）。
func (t *startupTrace) mark(label string) {
	if t == nil {
		return
	}
	now := time.Now()
	log.Infof("[startup] %-26s %7.1fms  (total %7.1fms)",
		label, ms(now.Sub(t.last)), ms(now.Sub(t.began)))
	t.last = now
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// markStartupReady 由 Start 在开始监听前调用，输出端到端总耗时。
func (t *startupTrace) markStartupReady() {
	if t == nil {
		return
	}
	now := time.Now()
	log.Infof("[startup] %-26s %7.1fms  (total %7.1fms)",
		"HTTP listener ready", ms(now.Sub(t.last)), ms(now.Sub(processStart)))
	t.last = now
}
