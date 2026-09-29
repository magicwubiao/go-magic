package bot

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// blockingLLM 模拟一个"收到请求就不返回"的成员回合：第一次调用关闭 started
// 通知测试，然后阻塞在 release 上（测试在 Cleanup 里关掉它）。**不要**阻塞
// 在 req.Context() 上 —— 实测客户端取消后 httptest 服务端的请求上下文未必
// 被唤醒，httptest.Server.Close 会挂在活跃连接上，整个测试进程卡死。
type blockingLLM struct {
	started chan struct{}
	once    sync.Once
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func newBlockingLLM() *blockingLLM {
	return &blockingLLM{started: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingLLM) handler(w http.ResponseWriter, req *http.Request) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.once.Do(func() { close(b.started) })
	<-b.release
}

func (b *blockingLLM) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// waitTimeout 在 d 内等到 ch，否则让测试立刻失败（而不是把 5 分钟的回合
// 预算跑完才暴露问题）。
func waitTimeout(t *testing.T, ch <-chan struct{}, d time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestStopRoomRoundCancelsRunningMemberTurn 覆盖"停止群聊"的核心不变量：
// 停止必须取消正在发言的成员回合（LLM 调用真的被打断），剩余成员不再被
// 调用，发送方拿到部分历史而不是错误。修复前的行为是——唯一的"停止"手段
// 是删房间；即便房间被删，在跑的成员回合也会继续把整个回合预算烧完。
func TestStopRoomRoundCancelsRunningMemberTurn(t *testing.T) {
	bl := newBlockingLLM()
	mgr, roomID, _ := newRoomRigHandler(t, bl.handler, 2)
	// Cleanup 是 LIFO：这条先注册（在本测试里后注册的都会先于它跑），放在
	// rig 之后注册保证它先于 server.Close / mgr.Stop 执行，放行被阻塞的
	// handler，否则 httptest.Close 会挂在活跃连接上。
	t.Cleanup(func() { close(bl.release) })

	resCh := make(chan *RoomResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := mgr.SendToRoom(context.Background(), roomID, "开始任务", "")
		resCh <- res
		errCh <- err
	}()

	waitTimeout(t, bl.started, 10*time.Second, "first member turn reaching the LLM")

	if !mgr.StopRoomRound(roomID) {
		t.Fatal("StopRoomRound reported no running round")
	}

	select {
	case res := <-resCh:
		if res == nil {
			t.Fatal("nil room result after stop")
		}
		if err := <-errCh; err != nil {
			t.Fatalf("SendToRoom returned error after stop: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("round did not finish after stop — the member turn kept running")
	}

	// 在跑的成员回合被取消：只有 alice 那 1 次调用，bob 不该被调。
	time.Sleep(300 * time.Millisecond)
	if got := bl.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 member LLM call after stop, got %d", got)
	}

	// 回合已结束：再停一次是无操作。
	if mgr.StopRoomRound(roomID) {
		t.Fatal("StopRoomRound on an idle room should be a no-op")
	}
}

// TestStopRoomRoundDropsQueuedRequests 覆盖排队消息：第一轮被阻塞期间，
// 第二条用户消息排在 triggerCh 里。停止之后它绝不能照常开跑（否则用户按
// 了停止还能看到新一轮动起来），发送方要立刻拿到当前历史。
func TestStopRoomRoundDropsQueuedRequests(t *testing.T) {
	bl := newBlockingLLM()
	mgr, roomID, _ := newRoomRigHandler(t, bl.handler, 2)
	t.Cleanup(func() { close(bl.release) })

	resA := make(chan *RoomResult, 1)
	errA := make(chan error, 1)
	go func() {
		res, err := mgr.SendToRoom(context.Background(), roomID, "第一个任务", "")
		resA <- res
		errA <- err
	}()
	waitTimeout(t, bl.started, 10*time.Second, "first member turn reaching the LLM")

	resB := make(chan *RoomResult, 1)
	errB := make(chan error, 1)
	go func() {
		res, err := mgr.SendToRoom(context.Background(), roomID, "第二个任务", "")
		resB <- res
		errB <- err
	}()

	// 给第二个请求一点时间完成入队（它此刻应排在 triggerCh 里，因为房间
	// 协调器是串行的）。
	time.Sleep(200 * time.Millisecond)

	if !mgr.StopRoomRound(roomID) {
		t.Fatal("StopRoomRound reported no running round")
	}

	for name, ch := range map[string]<-chan *RoomResult{"A": resA, "B": resB} {
		select {
		case res := <-ch:
			if res == nil {
				t.Fatalf("sender %s got nil result", name)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("sender %s still waiting — its round was not stopped", name)
		}
	}
	if err := <-errA; err != nil {
		t.Fatalf("sender A returned error after stop: %v", err)
	}
	if err := <-errB; err != nil {
		t.Fatalf("sender B returned error after stop: %v", err)
	}

	// 停止前排队的那条消息不允许再开跑：只有第一轮的 1 次 LLM 调用。
	time.Sleep(500 * time.Millisecond)
	if got := bl.callCount(); got != 1 {
		t.Fatalf("queued request started a new round after stop (%d LLM calls, want 1)", got)
	}
}

// TestStopRoomRoundIdleIsNoOp 覆盖空闲房间：没有进行中的回合时停止是
// 无操作，且 stopGen 不能卡死之后正常的回合。
func TestStopRoomRoundIdleIsNoOp(t *testing.T) {
	llm := &recordingLLM{reply: "好的"}
	mgr, roomID, _ := newRoomRigHandler(t, llm.handler, 1)

	if mgr.StopRoomRound(roomID) {
		t.Fatal("stop on an idle room must report false")
	}

	// 停过一次之后，下一个正常请求必须照常完成。
	resCh := make(chan error, 1)
	go func() {
		_, err := mgr.SendToRoom(context.Background(), roomID, "停止之后的正常消息", "")
		resCh <- err
	}()
	select {
	case err := <-resCh:
		if err != nil {
			t.Fatalf("normal round after a stop failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("normal round after a stop did not finish")
	}
	if len(llm.snapshot()) == 0 {
		t.Fatal("LLM was never called for the post-stop round")
	}
}
