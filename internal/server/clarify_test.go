package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/tool"
)

// newClarifyTestServer returns a Server with only the clarification maps wired
// (avoids the full NewServer dependency tree).
func newClarifyTestServer() *Server {
	s := &Server{}
	s.clarifySSEHandlers = make(map[string]func(map[string]interface{}) bool)
	s.clarifications = make(map[string]*PendingClarification)
	return s
}

func TestClarifyAskNoChannel(t *testing.T) {
	s := newClarifyTestServer()
	// 会话无活跃 SSE 澄清通道 → 回落为 gateway/CLI 路径的哨兵错误。
	_, err := s.Ask(context.Background(), "sess-1", tool.ClarifyRequest{Question: "A or B?"})
	if !errors.Is(err, tool.ErrClarifyUnavailable) {
		t.Fatalf("expected ErrClarifyUnavailable, got %v", err)
	}
}

func TestClarifyAskRoundTrip(t *testing.T) {
	s := newClarifyTestServer()

	recv := make(chan struct{}, 1)
	// 模拟会话流的 SSE 推送回调：确认事件送达（计数），精确 payload 构造由
	// Ask 内部负责，此处只验证推送与唤醒闭环。
	s.registerClarifySSEHandler("sess-1", func(data string) bool {
		_ = data
		select {
		case recv <- struct{}{}:
		default:
		}
		return true
	})
	// 上面 register 的闭包已替换掉 gotPayload 途径，直接再验证 map 已登记。
	s.clarifySSEHandlersMu.Lock()
	_, ok := s.clarifySSEHandlers["sess-1"]
	s.clarifySSEHandlersMu.Unlock()
	if !ok {
		t.Fatal("clarify SSE handler not registered")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type askResult struct {
		ans *tool.ClarifyAnswer
		err error
	}
	resCh := make(chan askResult, 1)
	go func() {
		ans, err := s.Ask(ctx, "sess-1", tool.ClarifyRequest{
			Question: "A or B?",
			Options:  []string{"A", "B"},
		})
		resCh <- askResult{ans, err}
	}()

	// 等推送发生（Ask 内部注册 pending 后会 push）。
	select {
	case <-recv:
	case <-ctx.Done():
		t.Fatal("clarify card event was not pushed")
	}

	// 取出 pending id 并答复。
	s.clarificationsMu.Lock()
	var id string
	for k := range s.clarifications {
		id = k
	}
	s.clarificationsMu.Unlock()
	if id == "" {
		t.Fatal("no pending clarification registered")
	}

	if err := s.resolveClarification(id, &tool.ClarifyAnswer{Choices: []string{"A"}, Note: "用 A"}); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("Ask returned error: %v", res.err)
		}
		if len(res.ans.Choices) != 1 || res.ans.Choices[0] != "A" {
			t.Fatalf("unexpected answer: %+v", res.ans)
		}
		if res.ans.Note != "用 A" {
			t.Fatalf("unexpected note: %+v", res.ans)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ask did not resume after resolve")
	}

	// pending 应在 Ask 返回后清理。
	s.clarificationsMu.Lock()
	left := len(s.clarifications)
	s.clarificationsMu.Unlock()
	if left != 0 {
		t.Fatalf("pending clarification leaked: %d remain", left)
	}
}

// TestClarifyAskReachesQueueSink 钉死真正的 Web 路径：Web 会话的 SSE 连接是
// 挂在会话队列上的 sink（handleSessionStream），不是 registerClarifySSEHandler
// 那种"handler 里直写"的闭包。队列改造曾把卡片注册从 handleSessionStream 里
// 删掉，导致 clarify 工具拿不到通道、直接回落成普通结果——表现就是"澄清卡片
// 永远不弹出"。这里直接断言事件确实进了队列总线。
func TestClarifyAskReachesQueueSink(t *testing.T) {
	s := newClarifyTestServer()
	s.chatQueues = make(map[string]*sessionQueue)

	q := s.sessionQueueFor("sess-web")
	snk := q.addSink()
	defer func() {
		snk.cancel()
		q.closeSink(snk)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Ask(ctx, "sess-web", tool.ClarifyRequest{
			Question: "A or B?",
			Options:  []string{"A", "B"},
		})
	}()

	select {
	case ev := <-snk.evch:
		if !strings.Contains(ev.data, `"type":"clarify_required"`) {
			t.Fatalf("expected clarify_required frame, got %q", ev.data)
		}
		if !strings.Contains(ev.data, `"question":"A or B?"`) {
			t.Fatalf("clarify payload lost the question: %q", ev.data)
		}
	case <-ctx.Done():
		t.Fatal("clarify card was not delivered to the session queue sink")
	}

	// 收尾：取消回合，避免 Ask 挂到超时并把 goroutine 留在测试里。
	cancel()
	<-done
}

func TestClarifyAskCtxCancel(t *testing.T) {
	s := newClarifyTestServer()
	s.registerClarifySSEHandler("sess-2", func(data string) bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 回合被取消

	_, err := s.Ask(ctx, "sess-2", tool.ClarifyRequest{Question: "Q"})
	if err == nil {
		t.Fatal("expected error when turn context is canceled")
	}
	if errors.Is(err, tool.ErrClarifyUnavailable) {
		t.Fatal("channel exists; cancel must surface as ctx error, not unavailable")
	}
}

func TestClarifyResolveUnknown(t *testing.T) {
	s := newClarifyTestServer()
	if err := s.resolveClarification("nope", &tool.ClarifyAnswer{}); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestClarifyDismissWakesAsk(t *testing.T) {
	s := newClarifyTestServer()
	s.registerClarifySSEHandler("sess-3", func(data string) bool { return true })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type askResult struct {
		ans *tool.ClarifyAnswer
		err error
	}
	resCh := make(chan askResult, 1)
	go func() {
		ans, err := s.Ask(ctx, "sess-3", tool.ClarifyRequest{Question: "A or B?"})
		resCh <- askResult{ans, err}
	}()

	// 等 pending 注册完成。
	deadline := time.Now().Add(3 * time.Second)
	var id string
	for {
		s.clarificationsMu.Lock()
		for k := range s.clarifications {
			id = k
		}
		s.clarificationsMu.Unlock()
		if id != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no pending clarification registered")
	}

	// 用户点 ✕：dismiss 应唤醒 Ask 并返回错误（非 nil），pending 清理。
	if err := s.dismissClarification(id); err != nil {
		t.Fatalf("dismiss failed: %v", err)
	}
	// 幂等：重复 dismiss 不报错。
	if err := s.dismissClarification(id); err != nil {
		t.Fatalf("repeated dismiss should be idempotent, got %v", err)
	}

	select {
	case res := <-resCh:
		if res.err == nil {
			t.Fatalf("expected dismissal error, got answer %+v", res.ans)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ask did not wake up after dismiss")
	}

	s.clarificationsMu.Lock()
	left := len(s.clarifications)
	s.clarificationsMu.Unlock()
	if left != 0 {
		t.Fatalf("pending clarification leaked after dismiss: %d remain", left)
	}

	// 已清理后再 dismiss 报 not found。
	if err := s.dismissClarification(id); err == nil {
		t.Fatal("expected error dismissing already-cleaned clarification")
	}
}
