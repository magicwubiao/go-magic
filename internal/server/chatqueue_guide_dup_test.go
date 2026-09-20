package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// guideDupTestServer 构造带真实会话存储的 Server：引导重复落库/重复气泡
// 只有在真实落库路径上才看得出来（persistGuideMessage 对 nil store 静默跳过）。
func guideDupTestServer(t *testing.T, sessionID string) (*Server, *agent.Agent, *session.Store) {
	t.Helper()
	store, err := session.NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SaveSession(context.Background(), &session.Session{
		ID:       sessionID,
		Profile:  "test",
		Platform: "web",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	s := &Server{
		agents:       make(map[string]*agent.Agent),
		chatQueues:   make(map[string]*sessionQueue),
		sessionStore: store,
	}
	a := &agent.Agent{}
	s.agents[sessionID] = a
	return s, a, store
}

// markWorkerLive 把队列标记为"已有 worker 在跑"——回收发生在 runQueue 内部，
// 真实的 workerLive 此刻必然为真。标记后 enqueueChatTurn 不会再拉起第二个
// worker，测试因此能稳定观察到入队后的队列内容（否则 worker 会立刻把项取走）。
func markWorkerLive(s *Server, q *sessionQueue) {
	s.chatQueuesMu.Lock()
	q.workerLive = true
	s.chatQueuesMu.Unlock()
}

// historyTexts 读出会话历史里每个 user 消息的 id + 内容，供重复检测使用。
func historyTexts(t *testing.T, store *session.Store, sessionID string) [][2]string {
	t.Helper()
	sess, err := store.LoadSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	out := make([][2]string, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		out = append(out, [2]string{m.ID, m.Content})
	}
	return out
}

// TestGuideLeftoverReclaimKeepsSingleMessage 是"引导发送两次"的核心回归。
//
// 场景（真实且高频）：引导在「模型本轮已经不会再排水」的时刻注入——注入路径
// 立即落库并广播（用户已看到气泡），随后 runQueue 收尾发现收件箱里还有残留
// （模型没来得及消费），把它转成一条新的排队回合。
//
// 修复前：回收出的回合用**新** id 入队，回合开跑时 persistUserMessage 又写一条
// 同内容 user 消息 → 会话历史里两条一模一样的消息（刷新页面后永久可见），
// 且前端会多出一个排队气泡 → 用户看到"引导发了两次"。
//
// 修复后：回收项沿用引导自己的 id，落库按 id 幂等（同 id 只留一条），
// 前端按 user_<id> 去重 → 全程只有一条。
func TestGuideLeftoverReclaimKeepsSingleMessage(t *testing.T) {
	const sid = "sess-guide-dup"
	const text = "收尾时补充：输出 CSV 格式"
	s, a, store := guideDupTestServer(t, sid)

	q := s.sessionQueueFor(sid)
	defer s.dropSessionQueue(sid, q)
	markWorkerLive(s, q)
	startFakeTurn(q)

	resp := s.tryInjectGuide(sid, &parsedChatPayload{content: text})
	if resp == nil {
		t.Fatal("expected guided response while running, got nil (fallback)")
	}
	guideID, _ := resp["id"].(string)
	if guideID == "" {
		t.Fatalf("guide response carries no id: %#v", resp)
	}

	// 注入即落库：历史里此刻恰好一条（id 与响应一致）。
	afterInject := historyTexts(t, store, sid)
	if len(afterInject) != 1 {
		t.Fatalf("history after inject = %#v, want exactly 1 message", afterInject)
	}
	if afterInject[0][0] != guideID {
		t.Fatalf("persisted guide id = %q, want response id %q (前端按 id 去重)", afterInject[0][0], guideID)
	}

	// 模拟 runQueue 收尾临界区：running 翻负 + 残留回收同一临界区。
	q.mu.Lock()
	q.running = false
	leftovers := a.DrainGuideItems()
	q.mu.Unlock()

	if len(leftovers) != 1 {
		t.Fatalf("leftover reclaim missed the late guide: %#v", leftovers)
	}
	if leftovers[0].ID != guideID {
		t.Fatalf("leftover carries id %q, want the injected id %q — 否则回收出的回合无法与已落库消息对齐",
			leftovers[0].ID, guideID)
	}

	s.reclaimLeftoverGuides(sid, leftovers)

	// 回收出的排队项必须复用同一个 id：前端 promoteQueuedToMessage 的键是
	// user_<id>，同键才会被认成"同一条消息"而不新增气泡。
	q.mu.Lock()
	if len(q.items) != 1 {
		q.mu.Unlock()
		t.Fatalf("queue length = %d, want 1 requeued turn", len(q.items))
	}
	requeued := q.items[0]
	q.mu.Unlock()
	if requeued.id != guideID {
		t.Fatalf("requeued turn id = %q, want %q (id 复用是去重的唯一依据)", requeued.id, guideID)
	}
	if requeued.content != text {
		t.Fatalf("requeued turn content = %q, want %q", requeued.content, text)
	}

	// 回收出的回合真正开跑：persistUserMessage 必须按 id 幂等，不得再写一条。
	s.persistUserMessage(sid, requeued)
	final := historyTexts(t, store, sid)
	if len(final) != 1 {
		t.Fatalf("history = %#v, want exactly 1 message（同 id 只应保留一条，重复即用户可见的\"引导发两次\"）", final)
	}
	if final[0][1] != text {
		t.Fatalf("history[0] content = %q, want %q", final[0][1], text)
	}
}

// TestConvertDBMessagesToAPIPreservesStoredID 消息接口必须保留落库 id。
//
// 前端判断"这条消息是否已在列表里"时认两种 id 形态（内存态 user_<id> 与
// 服务端原始 id）。若读接口一律改写成位置 id msg_<i>，刷新页面后「引导消息」
// 与「由它回收而成的排队回合」就对不上，回合开始时前端会再补一个同内容气泡
// —— 又一次"引导发了两次"。无 id 的历史消息仍退回 msg_<i>。
func TestConvertDBMessagesToAPIPreservesStoredID(t *testing.T) {
	out := convertDBMessagesToAPI("sess", []types.Message{
		{ID: "guide-uuid", Role: "user", Content: "改成只讲后端性能"},
		{Role: "assistant", Content: "好"},
	})
	if got := out[0]["id"]; got != "guide-uuid" {
		t.Fatalf("stored id = %#v, want guide-uuid（保留落库 id 才能与内存态气泡对齐）", got)
	}
	if got := out[1]["id"]; got != "msg_1" {
		t.Fatalf("id-less message = %#v, want msg_1（按位置兜底）", got)
	}
}

// TestReclaimLeftoverGuidesCountsRequeued 校验回收返回的 pending 数就是 done 帧
// 的 queue_depth：必须在入队之后统计（否则前端/转发层会误判队列已空而关连接，
// 下一条回合的事件就推不到客户端）。
func TestReclaimLeftoverGuidesCountsRequeued(t *testing.T) {
	const sid = "sess-guide-count"
	s, _, _ := guideDupTestServer(t, sid)
	q := s.sessionQueueFor(sid)
	defer s.dropSessionQueue(sid, q)
	markWorkerLive(s, q)

	items := []agent.GuideItem{{ID: "g-1", Text: "第一条补充"}, {ID: "g-2", Text: "  "}}
	if got := s.reclaimLeftoverGuides(sid, items); got != 1 {
		t.Fatalf("pending after reclaim = %d, want 1（空白引导不得入队）", got)
	}
	q.mu.Lock()
	got := len(q.items)
	q.mu.Unlock()
	if got != 1 {
		t.Fatalf("queue length = %d, want 1", got)
	}
}
