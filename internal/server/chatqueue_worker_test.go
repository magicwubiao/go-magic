package server

import (
	"testing"
	"time"
)

// 本文件钉住「worker 生命周期」与「队列里还有多少待执行消息」之间的关系。
//
// 背景：runQueue 的循环条件曾经是
//
//	for len(q.items) == 0 && q.hasSinksLocked() { q.cond.Wait() }
//	if len(q.items) == 0 { return }          // ← 只有队列空才退出
//
// 看起来没问题：有消息就继续跑。但它隐含了一个危险的耦合——**worker 的存活
// 依赖 SSE sink 的存在**。而 sink 的存活又依赖前端：
//
//	done 事件 → 前端 flushStreamBuffer → 若 state.queued 此刻为空则 close()
//
// 于是出现下面这条真实故障链（用户报告"消息执行完后，排队的消息没有发送执行"）：
//
//  1. 用户连发两条消息 A、B。A 立即被 worker 认领执行，B 留在 items 里。
//  2. B 的排队气泡在 stream_started(A) 时被前端从 state.queued 移出（转 running），
//     因此 A 还在跑的时候 state.queued 就是**空的**。
//  3. A 结束，worker 广播 done。
//  4. 前端收到 done，判 hasMoreQueued = state.queued.length > 0 → false，
//     立刻 close() 掉 SSE 连接。
//  5. sink 关闭 → hasSinksLocked() 变 false → worker 从 cond.Wait() 醒来
//     发现"没有 sink"，但**此时 items 里明明还有 B**。
//  6. 若这一瞬间 items 恰好被判定为空（见下），worker return，队列被回收，
//     B 永远没人执行。
//
// 第 6 步的竞态窗口来自 runQueue 的取件顺序：它先把 item 从 items 摘下来
// （q.items = q.items[1:]）再执行。所以"正在执行 A"期间 items 里是有 B 的，
// 但如果 B 是在 A 结束、done 已经广播出去之后才入队（用户在 A 收尾瞬间发消息），
// worker 已经走到 return 分支；此后 B 若没能重新拉起 worker，就永久滞留。

// TestSessionQueueWorkerKeepsDrainingAfterSinkClosed 验证核心不变量：
// **只要 items 里还有待执行消息，worker 就不应该退出**，哪怕所有 SSE 监听者
// 都已经断开。排队消息的执行必须与"有没有人在看"无关——否则用户闭掉页面
// 再回来就会发现消息卡在队列里。
func TestSessionQueueWorkerKeepsDrainingAfterSinkClosed(t *testing.T) {
	q := newSessionQueue()

	// 模拟：没有 sink（用户关闭了页面），但队列里有待执行消息。
	if q.hasAnySink() {
		t.Fatal("前置条件：本用例要求没有 sink")
	}
	q.mu.Lock()
	q.items = append(q.items, &queuedTurn{id: "pending-1", content: "pending-1"})
	pending := len(q.items)
	q.mu.Unlock()

	if pending != 1 {
		t.Fatalf("前置条件：应有 1 条待执行消息，实际 %d", pending)
	}

	// worker 的退出判定应当只看 items 是否为空，与 sink 无关。
	// 这里直接断言判定逻辑：items 非空 ⇒ 不应退出。
	if shouldWorkerExit(q) {
		t.Fatal("队列里仍有待执行消息，worker 却判定应当退出 —— " +
			"这会让这条消息永久滞留、永远不会被发送执行")
	}
}

// TestSessionQueueWorkerExitsOnlyWhenFullyDrained 覆盖另一侧：真正排空了
// 才允许退出。否则 worker 会空转，长跑服务上堆积 goroutine。
func TestSessionQueueWorkerExitsOnlyWhenFullyDrained(t *testing.T) {
	q := newSessionQueue()

	if !shouldWorkerExit(q) {
		t.Fatal("队列为空且无监听者时，worker 应当退出（否则空转泄漏 goroutine）")
	}

	// 无监听者 + 有消息 → 不退出
	q.mu.Lock()
	q.items = append(q.items, &queuedTurn{id: "x", content: "x"})
	q.mu.Unlock()
	if shouldWorkerExit(q) {
		t.Fatal("有消息时不应退出")
	}

	// 有监听者 + 无消息 → 保持存活等待新消息（这样前端可以复用连接）
	q2 := newSessionQueue()
	snk := q2.addSink()
	defer q2.closeSink(snk)
	if shouldWorkerExit(q2) {
		t.Fatal("还有监听者时不应退出，否则连接上的后续消息需要重连才能被消费")
	}
}

// TestSessionQueueEnqueueAfterWorkerExitRevives 覆盖 releaseWorker 的兜底：
// worker 判定"要退出"的同一瞬间有新消息入队，该消息必须仍被执行。
//
// 这是上面那条故障链的第 6 步：worker 已经走到 return，B 才入队。
// releaseWorker 必须发现 items 非空并把 worker 交还给这条新消息。
func TestSessionQueueEnqueueAfterWorkerExitRevives(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "revive"

	s.chatQueuesMu.Lock()
	q := newSessionQueue()
	q.workerLive = true
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	// 模拟竞态：worker 决定退出时队列里已有新消息。
	q.mu.Lock()
	q.items = append(q.items, &queuedTurn{id: "late", content: "late"})
	q.mu.Unlock()

	s.releaseWorker(sid, q)

	// 等待被重新拉起的 worker 认领这条消息。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		remaining := len(q.items)
		running := q.running
		q.mu.Unlock()
		if remaining == 0 || running {
			// 消息被认领了（或正在执行）——只要不是"留在 items 里没人管"即可。
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("worker 退出时残留的消息没有被重新认领，永远不会被执行")
}

// TestSessionQueueIdleTeardownKeepsPending 验证空闲回收的判据：
// 只有当队列既没有待执行消息、又没有监听者时才允许从 registry 里摘除。
// 若带着待执行消息摘除，那条消息也会一并消失。
func TestSessionQueueIdleTeardownKeepsPending(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "teardown"

	q := newSessionQueue()
	s.chatQueues[sid] = q
	q.mu.Lock()
	q.items = append(q.items, &queuedTurn{id: "keep", content: "keep"})
	q.mu.Unlock()

	s.dropSessionQueue(sid, q)

	s.chatQueuesMu.Lock()
	_, present := s.chatQueues[sid]
	s.chatQueuesMu.Unlock()
	if !present {
		t.Fatal("队列里仍有待执行消息，却被空闲回收摘除 —— 该消息会永久丢失")
	}
}
