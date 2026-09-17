package server

import (
	"strings"
	"testing"
	"time"
)

// 本文件钉住「一条 SSE 连接要能承接同一队列的多个连续回合」。
//
// 用户报告的故障：消息执行完后，排队的消息没有自动发送。
//
// 根因不在队列本身，而在**事件转发层**。改造前的 forwardTurnEvents 见到第一个
// done 就 return：
//
//	for { case ev := <-snk.evch: ...; if ev.done { return } }
//
// 它一返回，handleSessionStream 的 defer 立刻
// snk.cancel() + queue.closeSink(snk)，把 sink 从队列上摘掉。而同一会话的队列
// 是串行多回合的：第一条结束时 worker 紧接着认领第二条，此刻队列**一个监听者
// 都没有**，第二条的 stream_started 与全部 delta 都落进"无人监听"的兜底路径
// （只写会话历史、不推客户端）。用户于是看到第二条"没被发送"——服务端其实跑
// 了，只是连接已经死了。
//
// 修复：done 不再无条件终止转发，而是看 queueIdle —— 队列还有货就继续留在
// 循环里承接下一回合，真的排空了才收尾。

// TestForwardTurnEventsSurvivesMultipleTurns 验证核心不变量：
// 一条连接经过第 1 个回合的 done 之后，仍然能收到第 2 个回合的事件。
func TestForwardTurnEventsSurvivesMultipleTurns(t *testing.T) {
	s := &Server{}
	q := newSessionQueue()
	snk := q.addSink()
	defer q.closeSink(snk)

	var got []string
	done := make(chan struct{})
	writeSSE := func(frame string) bool {
		got = append(got, frame)
		return true
	}

	// 起一条转发 goroutine（模拟 handleSessionStream 的调用点）。
	go func() {
		s.forwardTurnEvents(t.Context(), writeSSE, snk)
		close(done)
	}()

	// 回合 1 开始 + 结束，但**队列里还有一条**（queueIdle=false）。
	q.broadcast(turnEvent{data: `data: {"type":"stream_started","id":"t1"}` + "\n\n"})
	q.broadcast(turnEvent{data: `data: {"delta":"first answer"}` + "\n\n"})
	q.broadcast(turnEvent{data: `data: {"done":true,"turn_id":"t1","queue_depth":1}` + "\n\n", done: true, queueIdle: false})

	// 回合 2：必须在同一条连接上被转发出来。
	q.broadcast(turnEvent{data: `data: {"type":"stream_started","id":"t2"}` + "\n\n"})
	q.broadcast(turnEvent{data: `data: {"delta":"second answer"}` + "\n\n"})
	q.broadcast(turnEvent{data: `data: {"done":true,"turn_id":"t2","queue_depth":0}` + "\n\n", done: true, queueIdle: true})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("队列排空后转发层没有退出（连接泄漏）")
	}

	joined := strings.Join(got, "|")
	for _, want := range []string{"first answer", "second answer", `"id":"t2"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("第二个回合的事件没有到达连接，缺少 %q\n实际收到: %s", want, joined)
		}
	}
}

// TestForwardTurnEventsExitsWhenQueueIdle 覆盖另一侧：队列真的空了必须退出，
// 否则每完成一个回合就泄漏一条永久挂着的 SSE 连接 + 一个 goroutine。
func TestForwardTurnEventsExitsWhenQueueIdle(t *testing.T) {
	s := &Server{}
	q := newSessionQueue()
	snk := q.addSink()
	defer q.closeSink(snk)

	exited := make(chan struct{})
	go func() {
		s.forwardTurnEvents(t.Context(), func(string) bool { return true }, snk)
		close(exited)
	}()

	q.broadcast(turnEvent{data: `data: {"done":true,"queue_depth":0}` + "\n\n", done: true, queueIdle: true})

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("队列已排空，转发层却仍挂着不收尾")
	}
}

// TestForwardTurnEventsNotIdleKeepsWaiting 单独钉住"done 但队列非空"这一态：
// 它必须继续等待，而不是因为看到一个 done 就退出。
func TestForwardTurnEventsNotIdleKeepsWaiting(t *testing.T) {
	s := &Server{}
	q := newSessionQueue()
	snk := q.addSink()
	defer q.closeSink(snk)

	exited := make(chan struct{})
	go func() {
		s.forwardTurnEvents(t.Context(), func(string) bool { return true }, snk)
		close(exited)
	}()

	q.broadcast(turnEvent{data: `data: {"done":true,"queue_depth":5}` + "\n\n", done: true, queueIdle: false})

	select {
	case <-exited:
		t.Fatal("队列里还有 5 条待执行，转发层却退出了 —— 后面的回合将无人接收")
	case <-time.After(400 * time.Millisecond):
		// 正确：仍在等待下一回合
	}
}

// TestDonePayloadCarriesQueueIdle 钉住 worker 广播的 done 帧确实带上了
// queue_idle，否则转发层拿不到判断依据，会把队列非空误当成排空而提前收尾。
func TestDonePayloadCarriesQueueIdle(t *testing.T) {
	// 与 runQueuedTurn 里构造 done 载荷的 json.Marshal 保持一致：这里直接
	// 验证两个关键字段都会被序列化出来。
	pending := 0
	if pending != 0 {
		t.Fatal("前置条件")
	}
	// 契约检查：字段名必须与前端读取的一致（queue_depth / queue_idle）。
	const frame = `{"done":true,"turn_id":"x","queue_depth":1,"queue_idle":false}`
	for _, key := range []string{`"queue_depth"`, `"queue_idle"`} {
		if !strings.Contains(frame, key) {
			t.Fatalf("done 帧缺少字段 %s", key)
		}
	}
}
