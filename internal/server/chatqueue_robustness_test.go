package server

import (
	"testing"
	"time"
)

// 本文件钉住两条与"对话早就结束/停止了，发新消息却长时间排队"直接相关的
// 队列不变量。两者都会让队列**看起来**还在忙、或让消息**永远没人消费**。

// TestEnqueueChatTurnDuplicateStillSpawnsWorker 覆盖查重命中时的 worker 兜底。
//
// 缺陷形态：enqueueChatTurn 先判断"要不要拉 worker"并置位 workerLive，随后才做
// 查重；命中重复（同会话同内容的弱网重试）就直接 return，把"已经置位但没有
// 任何 goroutine"的状态留在队列上。此后所有消息都走 spawnWorker=false 分支，
// 堆在 items 里没人执行——前端一直显示"排队中"。
func TestEnqueueChatTurnDuplicateStillSpawnsWorker(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "dup-spawn"

	q := newSessionQueue()
	s.chatQueuesMu.Lock()
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	// 队列里已有一条待执行消息，且此刻没有存活 worker（workerLive=false）。
	q.mu.Lock()
	q.items = append(q.items, &queuedTurn{id: "existing", content: "same content"})
	q.mu.Unlock()

	// 同内容再次提交 → 命中查重。
	item, dup := s.enqueueChatTurn(sid, "same content", nil, nil, &turnRunCtx{fileOps: NewTurnFileOpTracker()}, "", "")
	if item == nil {
		t.Fatal("enqueueChatTurn returned nil item")
	}
	if !dup {
		t.Fatalf("expected duplicate detection, got dup=false (item id %q)", item.id)
	}

	// 不变量：workerLive 置位就必须真的有 worker 在消费队列。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		remaining := len(q.items)
		running := q.running
		q.mu.Unlock()
		if remaining == 0 || running {
			return // 被认领（或已执行完）
		}
		time.Sleep(10 * time.Millisecond)
	}

	q.mu.Lock()
	live := q.workerLive
	remaining := len(q.items)
	q.mu.Unlock()
	t.Fatalf("duplicate hit left the queue unattended: items=%d workerLive=%v —— 消息会一直排队、永不执行", remaining, live)
}

// TestQueuedTurnPanicDoesNotLeaveQueueRunning 覆盖回合内 panic 的收尾。
//
// 缺陷形态：收尾（running 翻负、cancel 复位、done 广播）写在 runQueuedTurn
// 之后。回合内部 panic 会整段跳过收尾，q.running 永远停在 true——/running
// 一直回答"有回合在跑"，前端把后续消息显示为"排队中"等待一个永远不会来的
// stream_started。历史上这条路径真实触发过（Cortex 禁用时 Trigger 为 nil）。
func TestQueuedTurnPanicDoesNotLeaveQueueRunning(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "panic-turn"

	q := newSessionQueue()
	s.chatQueuesMu.Lock()
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	q.workerLive = true
	// fileOps 为 nil → runQueuedTurn 收尾处调用 Result() 时必定 panic，
	// 用来模拟"回合并发路径里的任意 panic"。
	q.enqueue(&queuedTurn{id: "t1", content: "boom", run: &turnRunCtx{fileOps: nil}})

	safeGo(func() { s.runQueue(sid, q) })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		running := q.running
		turns := q.turns
		remaining := len(q.items)
		q.mu.Unlock()
		if !running && turns >= 1 && remaining == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	q.mu.Lock()
	running := q.running
	turns := q.turns
	remaining := len(q.items)
	q.mu.Unlock()
	t.Fatalf("panic in queued turn left the queue stuck: running=%v turns=%d items=%d —— "+
		"/running 会一直报'有回合在跑'，新消息只能排队", running, turns, remaining)
}
