package server

import (
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// guideTestServer 构造最小可用的 Server + 预置 agent（零值 Agent 的收件箱
// 可直接使用）。sessionStore 保持 nil：persistGuideMessage 对 nil store
// 静默跳过，注入路径不得因此报错。
func guideTestServer(t *testing.T) (*Server, *agent.Agent) {
	t.Helper()
	s := &Server{
		agents:     make(map[string]*agent.Agent),
		chatQueues: make(map[string]*sessionQueue),
	}
	a := &agent.Agent{}
	s.agents["sess"] = a
	return s, a
}

// startFakeTurn 把队列置为"回合在跑"（模拟 runQueue 认领条目后的状态）。
func startFakeTurn(q *sessionQueue) {
	q.mu.Lock()
	q.running = true
	q.mu.Unlock()
}

// TestTryInjectGuideFallsBackWithoutQueue 没有队列（会话从未跑过回合）时
// 必须回落普通入队，且不能污染收件箱。
func TestTryInjectGuideFallsBackWithoutQueue(t *testing.T) {
	s, a := guideTestServer(t)
	if resp := s.tryInjectGuide("sess", &parsedChatPayload{content: "改用 SQLite"}); resp != nil {
		t.Fatalf("expected nil (fallback), got %#v", resp)
	}
	if got := a.DrainGuides(); len(got) != 0 {
		t.Fatalf("inbox polluted: %#v", got)
	}
}

// TestTryInjectGuideFallsBackWhenIdle 队列空闲（没有回合在跑）时回落。
// 这是防"引导悬空"的第一道闸：空闲时注入会滞留到未来某个无关回合的开头。
func TestTryInjectGuideFallsBackWhenIdle(t *testing.T) {
	s, a := guideTestServer(t)
	q := s.sessionQueueFor("sess")
	defer s.dropSessionQueue("sess", q)

	if resp := s.tryInjectGuide("sess", &parsedChatPayload{content: "改用 SQLite"}); resp != nil {
		t.Fatalf("expected nil (fallback), got %#v", resp)
	}
	if got := a.DrainGuides(); len(got) != 0 {
		t.Fatalf("inbox polluted: %#v", got)
	}
}

// TestTryInjectGuideFallsBackWithMediaParts 纯附件引导（无文本部件）回落：
// media 部件的还原管线只存在于「入队 → 回合开跑」路径，注入路径没有还原点，
// 纯附件无法即时注入，回落入队走完整还原管线。
func TestTryInjectGuideFallsBackWithMediaParts(t *testing.T) {
	s, a := guideTestServer(t)
	q := s.sessionQueueFor("sess")
	defer s.dropSessionQueue("sess", q)
	startFakeTurn(q)

	parsed := &parsedChatPayload{
		content: "看这张图",
		contentParts: []types.ContentPart{
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,xxx"}},
		},
	}
	if resp := s.tryInjectGuide("sess", parsed); resp != nil {
		t.Fatalf("expected nil (fallback) for pure-media parts, got %#v", resp)
	}
	if got := a.DrainGuides(); len(got) != 0 {
		t.Fatalf("inbox polluted: %#v", got)
	}
}

// TestTryInjectGuideInjectsTextPartOfMultimodal 多模态（带图+文本）引导：提取
// text 部件作为纯文本即时注入，保住用户文本指引不被丢；图片不随注入路径携带。
func TestTryInjectGuideInjectsTextPartOfMultimodal(t *testing.T) {
	s, a := guideTestServer(t)
	q := s.sessionQueueFor("sess")
	defer s.dropSessionQueue("sess", q)
	startFakeTurn(q)

	parsed := &parsedChatPayload{
		content: "看这张图，聚焦后端",
		contentParts: []types.ContentPart{
			{Type: "text", Text: "看这张图，聚焦后端"},
			{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,xxx"}},
		},
	}
	resp := s.tryInjectGuide("sess", parsed)
	if resp == nil {
		t.Fatal("expected guided response for multimodal text part, got nil (fallback)")
	}
	if resp["guided"] != true {
		t.Fatalf("guided flag = %#v, want true", resp["guided"])
	}
	if content, _ := resp["content"].(string); content != "看这张图，聚焦后端" {
		t.Fatalf("content = %q, want text part 看这张图，聚焦后端", content)
	}
	if got := a.DrainGuides(); len(got) != 1 || got[0] != "看这张图，聚焦后端" {
		t.Fatalf("model-side drain = %#v, want [看这张图，聚焦后端]", got)
	}
}

// TestTryInjectGuideInjectsWhenRunning 核心正路：回合在跑时注入成功——
// 响应 guided=true、收件箱拿到文本、guide_added 事件到达监听中的 SSE sink。
// 随后模型侧排水（模拟迭代顶部），收件箱清空、残留回收不再重复捞取。
func TestTryInjectGuideInjectsWhenRunning(t *testing.T) {
	s, a := guideTestServer(t)
	q := s.sessionQueueFor("sess")
	defer s.dropSessionQueue("sess", q)
	snk := q.addSink()
	defer q.closeSink(snk)
	startFakeTurn(q)

	resp := s.tryInjectGuide("sess", &parsedChatPayload{content: "  聚焦后端性能  "})
	if resp == nil {
		t.Fatal("expected guided response, got nil (fallback)")
	}
	if resp["guided"] != true {
		t.Fatalf("guided flag = %#v, want true", resp["guided"])
	}
	if id, _ := resp["id"].(string); id == "" {
		t.Fatalf("response id empty: %#v", resp)
	}
	if content, _ := resp["content"].(string); content != "聚焦后端性能" {
		t.Fatalf("content = %q, want trimmed 聚焦后端性能", content)
	}

	// guide_added 事件必须到达监听中的 sink。
	select {
	case ev := <-snk.evch:
		if !strings.Contains(ev.data, `"guide_added"`) || !strings.Contains(ev.data, "聚焦后端性能") {
			t.Fatalf("guide_added frame unexpected: %q", ev.data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guide_added event never reached the sink")
	}

	// 模型侧迭代顶部排水：拿走后，收尾回收不得再捞到（不重复）。
	if got := a.DrainGuides(); len(got) != 1 || got[0] != "聚焦后端性能" {
		t.Fatalf("model-side drain = %#v, want [聚焦后端性能]", got)
	}
	// 模拟 runQueue 收尾临界区。
	q.mu.Lock()
	q.running = false
	leftovers := a.DrainGuides()
	q.mu.Unlock()
	if len(leftovers) != 0 {
		t.Fatalf("leftover reclaim double-fetched: %#v", leftovers)
	}
}

// TestGuideHandshakeReclaimCatchesLateInjection 竞态握手：注入发生在
// "模型已结束迭代"之后（来不及被模型看到）时，收尾回收必须把它捞走转成
// 新排队回合——绝不悬空到未来某个无关回合的开头。
func TestGuideHandshakeReclaimCatchesLateInjection(t *testing.T) {
	s, a := guideTestServer(t)
	q := s.sessionQueueFor("sess")
	defer s.dropSessionQueue("sess", q)
	startFakeTurn(q)

	if resp := s.tryInjectGuide("sess", &parsedChatPayload{content: "收尾时补充：输出 CSV 格式"}); resp == nil {
		t.Fatal("expected guided response while running")
	}

	// 模拟 runQueue 收尾临界区：running 翻负与残留回收同一临界区（顺序
	// 与 chatqueue.go 保持一致）。
	q.mu.Lock()
	q.running = false
	leftovers := a.DrainGuides()
	q.mu.Unlock()

	if len(leftovers) != 1 || leftovers[0] != "收尾时补充：输出 CSV 格式" {
		t.Fatalf("leftover reclaim missed the late guide: %#v", leftovers)
	}
}

// TestGuideBlankContentFallsBack 空白内容回落（不注入、不落库）。
func TestGuideBlankContentFallsBack(t *testing.T) {
	s, a := guideTestServer(t)
	q := s.sessionQueueFor("sess")
	defer s.dropSessionQueue("sess", q)
	startFakeTurn(q)

	if resp := s.tryInjectGuide("sess", &parsedChatPayload{content: "   "}); resp != nil {
		t.Fatalf("expected nil for blank content, got %#v", resp)
	}
	if got := a.DrainGuides(); len(got) != 0 {
		t.Fatalf("inbox polluted: %#v", got)
	}
}
