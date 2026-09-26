package server

import (
	"testing"
	"time"
)

// 本文件钉住 worker 生命周期改造后的三条不变量。它们共同针对用户报告的
// 现象：「对话早就停止了，发新消息却一直在排队中」。
//
//   - 不变量一：入队与 worker 拉起判定原子化后，"worker 正在退出"与
//     "新消息入队"无论怎么交错，消息都必然有人消费（不再有孤儿队列）。
//   - 不变量二：代数守卫——旧一代 worker 的退出收尾不能复位新一代的
//     workerLive，更不能回收还挂着新一代 worker 的队列。
//   - 不变量三：卡死看门狗强制解锁后，队列状态复原、排队消息仍会被
//     新一代 worker 消费——会话不再可能被一个无视取消的回合永久钉死。

// TestEnqueueAfterExitingWorkerTeardownStillConsumed 覆盖交错的第一种顺序：
// releaseWorker（退出收尾）完整跑完（复位 + 空闲回收）之后，新消息才入队。
// 入队路径必须自己重新创建队列并拉起 worker，而不是把消息塞进已被回收的
// 旧队列对象里。
func TestEnqueueAfterExitingWorkerTeardownStillConsumed(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "teardown-then-enqueue"

	s.chatQueuesMu.Lock()
	q := newSessionQueue()
	q.workerLive = true // 模拟：旧 worker 仍持有所有权
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	// 旧 worker 退出：无待执行、无监听者 → 复位并回收。
	s.releaseWorker(sid, q, q.workerGen)

	s.chatQueuesMu.Lock()
	_, present := s.chatQueues[sid]
	s.chatQueuesMu.Unlock()
	if present {
		t.Fatal("前置条件：空闲队列应被回收摘除")
	}

	// 随后新消息入队：必须落在 map 里新建的队列上并拉起 worker。
	item, dup := s.enqueueChatTurn(sid, "hello", nil, nil, &turnRunCtx{fileOps: NewTurnFileOpTracker()}, "", "")
	if item == nil || dup {
		t.Fatalf("enqueueChatTurn 返回异常：item=%v dup=%v", item, dup)
	}

	waitForQueueClaim(t, q, item.id)
}

// TestEnqueueBeforeExitingWorkerTeardownStillConsumed 覆盖交错的第二种顺序：
// 新消息先入队（此刻 workerLive 仍为 true，判定不拉新 worker），旧 worker
// 的退出收尾在其后完成。收尾发现 items 非空，必须重新拉起 worker 把消息
// 交还消费——这正是历史上"消息永远排队"的窗口。
func TestEnqueueBeforeExitingWorkerTeardownStillConsumed(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "enqueue-then-teardown"

	s.chatQueuesMu.Lock()
	q := newSessionQueue()
	q.workerLive = true // 模拟：旧 worker 仍持有所有权
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	item, dup := s.enqueueChatTurn(sid, "hello", nil, nil, &turnRunCtx{fileOps: NewTurnFileOpTracker()}, "", "")
	if item == nil || dup {
		t.Fatalf("enqueueChatTurn 返回异常：item=%v dup=%v", item, dup)
	}

	// 旧 worker 此刻退出：items 非空 → 不得回收队列，且必须重拉 worker。
	s.releaseWorker(sid, q, q.workerGen)

	waitForQueueClaim(t, q, item.id)
}

// TestStaleWorkerReleaseCannotClobberNewGeneration 钉住代数守卫：旧一代
// worker 的 defer releaseWorker 在所有权已移交（新一代被拉起）后到达时，
// 不得复位 workerLive、不得回收队列——否则新一代 worker 还在跑，队列却被
// 摘除，后续消息全部进入"无人认领"状态。
func TestStaleWorkerReleaseCannotClobberNewGeneration(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "stale-release"

	s.chatQueuesMu.Lock()
	q := newSessionQueue()
	q.workerLive = true
	q.workerGen = 5 // 新一代已在跑
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	// 旧一代（gen=3）的收尾迟到：必须整体退让。
	s.releaseWorker(sid, q, 3)

	s.chatQueuesMu.Lock()
	live := q.workerLive
	_, present := s.chatQueues[sid]
	s.chatQueuesMu.Unlock()
	if !live {
		t.Fatal("旧一代的 releaseWorker 复位了新一代的 workerLive —— 队列将无人消费")
	}
	if !present {
		t.Fatal("旧一代的 releaseWorker 回收了仍有 worker 的队列")
	}
}

// TestStallWatchdogForceRecovery 钉住看门狗的强制解锁行为：回合超过
// deadline+宽限仍未结束时，队列状态必须复原、排队消息必须由换代 worker
// 继续消费。这里直接调用 forceRecoverStalledTurn 模拟"看门狗到点"，避免
// 在测试里等待真实的 30 分钟超时。
func TestStallWatchdogForceRecovery(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "stall-recovery"

	s.chatQueuesMu.Lock()
	q := newSessionQueue()
	q.workerLive = true // 僵尸 worker 仍握着所有权
	q.workerGen = 7
	q.turnEpoch = 3
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	// 模拟回合正在跑：running=true + activeID 匹配。
	q.mu.Lock()
	q.running = true
	q.activeID = "stuck-turn"
	q.turnStartedAt = time.Now().Add(-time.Hour)
	q.items = append(q.items, &queuedTurn{id: "next", content: "next", run: &turnRunCtx{fileOps: NewTurnFileOpTracker()}})
	q.mu.Unlock()

	// 看门狗到点：(itemID, gen, epoch) 与当前状态完全一致 → 触发强制解锁。
	s.forceRecoverStalledTurn(sid, q, "stuck-turn", 7, 3, nil)

	q.mu.Lock()
	epoch := q.turnEpoch
	running := q.running
	activeID := q.activeID
	startedAtZero := q.turnStartedAt.IsZero()
	q.mu.Unlock()
	if epoch != 4 {
		t.Fatalf("强制解锁应递增 turnEpoch，实际 %d —— 僵尸 worker 将无法识别自己已被收走", epoch)
	}
	if running || activeID != "" || !startedAtZero {
		t.Fatalf("强制解锁后队列状态未复原：running=%v activeID=%q startedAtZero=%v", running, activeID, startedAtZero)
	}

	// 排队消息必须被换代 worker（gen=8）认领执行，而不是永远滞留。
	waitForQueueClaim(t, q, "next")
}

// TestStallWatchdogIgnoresFinishedTurn 覆盖看门狗的判定侧：回合早已正常
// 结束（activeID/epoch 不匹配）时，强制解锁绝不能触发，否则会把正常收尾
// 的队列状态错误翻覆。
func TestStallWatchdogIgnoresFinishedTurn(t *testing.T) {
	s := &Server{chatQueues: make(map[string]*sessionQueue)}
	const sid = "stall-ignore"

	s.chatQueuesMu.Lock()
	q := newSessionQueue()
	q.workerLive = true
	q.workerGen = 2
	q.turnEpoch = 5
	s.chatQueues[sid] = q
	s.chatQueuesMu.Unlock()

	q.mu.Lock()
	q.running = true
	q.activeID = "other-turn" // 新回合已开始：旧看门狗的 itemID 已不匹配
	q.mu.Unlock()

	s.forceRecoverStalledTurn(sid, q, "old-turn", 2, 5, nil)

	q.mu.Lock()
	epoch := q.turnEpoch
	running := q.running
	activeID := q.activeID
	q.mu.Unlock()
	if epoch != 5 || !running || activeID != "other-turn" {
		t.Fatalf("不匹配的看门狗触发了强制解锁：epoch=%d running=%v activeID=%q", epoch, running, activeID)
	}
}

// waitForQueueClaim 等待指定队列项被 worker 认领（开始执行）或已被执行完。
// 超时说明消息滞留在 items 里无人消费——即"一直在排队中"的服务端形态。
//
// 注意测试环境没有 provider，回合会在认领后毫秒级结束并清空队列（空闲回收
// 随即可能把队列摘除），因此这里持有队列对象本身轮询，"items 已清空"即视为
// 已被认领；不能反复从 map 查找（队列可能已被回收）。
func waitForQueueClaim(t *testing.T, q *sessionQueue, turnID string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		remaining := len(q.items)
		activeID := q.activeID
		q.mu.Unlock()
		if remaining == 0 || activeID == turnID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	q.mu.Lock()
	remaining := len(q.items)
	live := q.workerLive
	q.mu.Unlock()
	t.Fatalf("排队消息 %s 无人认领：items=%d workerLive=%v —— 用户将看到它永远\"排队中\"", turnID, remaining, live)
}
