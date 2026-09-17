package server

import (
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// 本文件钉住三种"撤销"语义的边界，特别是它们**互不越界**这一点。
//
// 三者容易在重构中被揉成一个，而揉错的方向几乎总是"顺手多杀一点"：
//
//   - cancelAll    ：取消当前回合 + 清空队列（停止键）
//   - dropItem     ：只丢点名的那一条，其余排队消息与当前回合都照旧
//   - clearPending ：只清空队列，**当前回合照常跑完**
//
// 最要命的是 clearPending 被实现成 cancelAll：用户只是想撤掉后面排着的
// 几条，结果正在生成的回答也被掐断了。这里的断言就是防止那次退化。

// TestClearPendingKeepsRunningTurn 是 clearPending 的核心契约：
// 队列被清空，但**运行中的回合不受任何影响**。
func TestClearPendingKeepsRunningTurn(t *testing.T) {
	q := newSessionQueue()

	// 构造"一个回合正在跑 + 后面排着两条"的状态。
	cancelled := false
	q.mu.Lock()
	q.running = true
	q.activeID = "running-1"
	q.cancel = func() { cancelled = true }
	q.items = []*queuedTurn{
		{id: "p1", content: "queued one"},
		{id: "p2", content: "queued two"},
	}
	q.mu.Unlock()

	dropped := q.clearPending()

	if dropped != 2 {
		t.Fatalf("clearPending 应丢弃 2 条排队消息，实际 %d", dropped)
	}
	if cancelled {
		t.Fatal("clearPending 取消了运行中的回合 —— 用户只是想撤掉后面排队的，不该掐断正在生成的回答")
	}

	q.mu.Lock()
	remaining := len(q.items)
	stillRunning := q.running
	activeID := q.activeID
	requested := q.cancelRequested
	q.mu.Unlock()

	if remaining != 0 {
		t.Fatalf("队列应被清空，实际还剩 %d 条", remaining)
	}
	if !stillRunning || activeID != "running-1" {
		t.Fatalf("运行中回合的状态被改动：running=%v activeID=%q", stillRunning, activeID)
	}
	if requested {
		t.Fatal("clearPending 不该置 cancelRequested —— 那会让当前回合被当成用户主动停止")
	}
}

// TestClearPendingVsCancelAll 把两者的差异摆在一起对照：同样的前置状态下，
// cancelAll 必须取消回合，clearPending 必须不取消。任何一方行为漂移都会失败。
func TestClearPendingVsCancelAll(t *testing.T) {
	newState := func() (*sessionQueue, *bool) {
		q := newSessionQueue()
		flag := new(bool)
		q.mu.Lock()
		q.running = true
		q.activeID = "turn-x"
		q.cancel = func() { *flag = true }
		q.items = []*queuedTurn{{id: "p1", content: "one"}}
		q.mu.Unlock()
		return q, flag
	}

	qClear, flagClear := newState()
	if n := qClear.clearPending(); n != 1 {
		t.Fatalf("clearPending 应丢弃 1 条，实际 %d", n)
	}
	if *flagClear {
		t.Fatal("clearPending 不得取消运行中的回合")
	}

	qCancel, flagCancel := newState()
	res := qCancel.cancelAll()
	if !res.active {
		t.Fatal("cancelAll 应报告已取消运行中的回合")
	}
	if !*flagCancel {
		t.Fatal("cancelAll 必须真正调用 cancel —— 停止键的语义就是连当前回合一起杀")
	}
	// 两者在"清空队列"这一点上是一致的。
	if res.pending != 1 {
		t.Fatalf("cancelAll 应报告丢弃 1 条排队消息，实际 %d", res.pending)
	}
}

// TestClearPendingEmptyQueueIsNoop 空队列上调用必须是无害的：
// 前端可能在队列刚被 worker 吃空的瞬间点"清空"，不能因此报错或改动状态。
func TestClearPendingEmptyQueueIsNoop(t *testing.T) {
	q := newSessionQueue()
	q.mu.Lock()
	q.running = true
	q.activeID = "busy"
	q.mu.Unlock()

	if n := q.clearPending(); n != 0 {
		t.Fatalf("空队列应返回 0，实际 %d", n)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.items != nil {
		t.Fatal("空队列被清空后 items 应为 nil")
	}
	if q.activeID != "busy" || !q.running {
		t.Fatal("空队列清空不该影响运行中回合的状态")
	}
}

// TestFindItemReturnsFullContent 钉住 /queue/{turnId}/content 的立身之本：
// 它必须回**未截断**的原文。
//
// 快照路径（snapshot → shortenQueuedContent）刻意裁到 120 字，那是给排队
// 列表一行显示用的。编辑回填若走那条路，用户写的长内容会被悄悄砍掉——
// 刷新页面后本地已无原件，这个问题必然暴露。
func TestFindItemReturnsFullContent(t *testing.T) {
	q := newSessionQueue()

	// 造一条明显超过预览上限的内容（中文按 rune 计数，用多字节字符验证
	// 截断是按字符而非按字节）。
	long := ""
	for i := 0; i < 300; i++ {
		long += "字"
	}
	if len([]rune(long)) <= queuedContentPreview {
		t.Fatalf("测试数据不够长：%d <= %d", len([]rune(long)), queuedContentPreview)
	}

	q.mu.Lock()
	q.items = []*queuedTurn{
		{
			id:      "long-1",
			content: long,
			persistedParts: []types.ContentPart{
				{Type: "file", File: &types.FileInfo{Name: "report.pdf", MimeType: "application/pdf", URL: "/api/uploads/report.pdf"}},
			},
		},
	}
	q.mu.Unlock()

	content, parts, ok := q.findItem("long-1")
	if !ok {
		t.Fatal("findItem 找不到刚放入的条目")
	}
	if content != long {
		t.Fatalf("findItem 返回了被截断的内容：len=%d 期望 %d", len([]rune(content)), len([]rune(long)))
	}

	// 对照：快照路径必须是截断的，否则这个测试就没有意义。
	snap := q.snapshot()
	if len(snap.items) != 1 {
		t.Fatalf("快照应有 1 条，实际 %d", len(snap.items))
	}
	if []rune(snap.items[0].Content)[len([]rune(snap.items[0].Content))-1] != '…' {
		t.Fatal("快照里的内容应当是截断并带省略号的 —— 若它已是全文，说明截断逻辑被去掉了，编辑回填的取舍需要重新设计")
	}

	if len(parts) != 1 || parts[0].File == nil || parts[0].File.URL != "/api/uploads/report.pdf" {
		t.Fatalf("findItem 应同时带回附件引用，实际 %+v", parts)
	}
}

// TestFindItemMissing 已被认领执行或已删除的条目必须报告 ok=false，
// handler 据此返回 404，前端提示"来不及编辑了"。
func TestFindItemMissing(t *testing.T) {
	q := newSessionQueue()
	q.mu.Lock()
	q.running = true
	q.activeID = "claimed"
	q.items = []*queuedTurn{{id: "still-here", content: "x"}}
	q.mu.Unlock()

	if _, _, ok := q.findItem("claimed"); ok {
		t.Fatal("已被 worker 认领的条目不该被 findItem 找到（它已不在 items 里）")
	}
	if _, _, ok := q.findItem("nope"); ok {
		t.Fatal("不存在的 id 应返回 ok=false")
	}
	if _, _, ok := q.findItem(""); ok {
		t.Fatal("空 id 应返回 ok=false")
	}
	if _, _, ok := q.findItem("still-here"); !ok {
		t.Fatal("仍在队列里的条目应当能找到")
	}
}

// TestDropItemDoesNotTouchCancel 复查第三种语义的边界：dropItem 既不该
// 清空其它排队消息，也不该碰运行中回合的 cancel。
func TestDropItemDoesNotTouchCancel(t *testing.T) {
	q := newSessionQueue()
	cancelled := false
	q.mu.Lock()
	q.running = true
	q.activeID = "turn-a"
	q.cancel = func() { cancelled = true }
	q.items = []*queuedTurn{
		{id: "keep", content: "keep"},
		{id: "drop", content: "drop"},
	}
	q.mu.Unlock()

	if !q.dropItem("drop") {
		t.Fatal("dropItem 应能找到并移除目标条目")
	}
	if cancelled {
		t.Fatal("dropItem 不该取消运行中的回合")
	}
	if !q.itemExists("keep") {
		t.Fatal("dropItem 误删了其它排队消息")
	}
	if q.itemExists("drop") {
		t.Fatal("目标条目没有被移除")
	}
}
