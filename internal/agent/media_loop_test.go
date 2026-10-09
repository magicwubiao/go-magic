package agent

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// ============================================================================
// 媒体回合（RunConversationWithMedia / RunConversationStreamWithMedia）
//
// 现场（群聊发图卡死）：这两条循环是四条工具循环里唯一既没有调
// maybeCompressContext、又在 provider 出错时裸 continue 的 —— 一次挂住的
// HTTP 请求（客户端 180s 超时）会被立刻重发，几轮下来把整个回合预算吃光，
// 成员一个字都产不出来，而群聊协调器还在原地等它，整轮因此长时间"卡着"。
// ============================================================================

// timeoutProbeProvider 每次调用都返回一个传输层超时错误，并记录调用次数。
type timeoutProbeProvider struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (p *timeoutProbeProvider) Name() string { return "timeout-probe" }

func (p *timeoutProbeProvider) Chat(context.Context, []provider.Message) (*provider.ChatResponse, error) {
	p.mu.Lock()
	p.calls++
	err := p.err
	p.mu.Unlock()
	return nil, err
}

func (p *timeoutProbeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// staticTextProvider 永远回一句固定文本（不调工具）。
type staticTextProvider struct{ text string }

func (p *staticTextProvider) Name() string { return "static-text" }

func (p *staticTextProvider) Chat(context.Context, []provider.Message) (*provider.ChatResponse, error) {
	return &provider.ChatResponse{Content: p.text}, nil
}

func mediaParts() []types.ContentPart {
	return []types.ContentPart{
		{Type: "text", Text: "look at this image"},
		{Type: "image_url", ImageURL: &types.MediaURL{URL: "data:image/png;base64,AAAA"}},
	}
}

// TestMediaTurnStopsRetryingAfterProviderTimeout 锁死"挂住的请求不再原地重试"。
//
// 传输超时是客户端 180s 上限触发的：紧接着重试只会再挂一次（回合预算通常只有
// 几分钟），最终整个回合没有任何输出。修复前这里是裸 continue，provider 会
// 被连续调用 maxTurns 次。
func TestMediaTurnStopsRetryingAfterProviderTimeout(t *testing.T) {
	prov := &timeoutProbeProvider{err: &url.Error{Op: "Post", URL: "http://example.invalid", Err: context.DeadlineExceeded}}
	ag := newLoopTestAgent(t, prov, WithMaxTurns(5))

	_, err := ag.RunConversationWithMedia(context.Background(), "look at this image", mediaParts())
	if err == nil {
		t.Fatal("a hung provider request must fail the media turn, not loop silently")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should name the timeout as the cause, got %v", err)
	}
	if got := prov.callCount(); got != 1 {
		t.Fatalf("provider called %d times; a transport timeout must not be retried inside the same turn "+
			"(each retry re-enters the stall and the turn ends with no output at all)", got)
	}
}

// TestMediaTurnPropagatesDeadContextImmediately 对照 RunConversation 的既有行为：
// ctx 已结束时立刻以明确原因返回，而不是继续往过期上下文里发请求。
func TestMediaTurnPropagatesDeadContextImmediately(t *testing.T) {
	prov := &timeoutProbeProvider{err: errors.New("context deadline exceeded")}
	ag := newLoopTestAgent(t, prov, WithMaxTurns(5))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ag.RunConversationWithMedia(ctx, "look at this image", mediaParts()); err == nil {
		t.Fatal("expected the cancelled turn to fail")
	}
	if got := prov.callCount(); got != 0 {
		t.Fatalf("provider called %d times on an already-cancelled turn", got)
	}
}

// TestMediaTurnsCompactContext 锁死媒体循环的上下文压缩。
//
// 压缩阈值只有 8000 tokens，而媒体循环此前完全不压缩 ⇒ 带图会话的历史只增
// 不减（实测群聊成员会话膨胀到 177KB、含多份内联图片），每轮把整包历史重发
// 一次，回合越跑越慢。两条媒体入口都必须压缩：非流式（bot 群聊/单聊）与流式
// （web 聊天带图）。
func TestMediaTurnsCompactContext(t *testing.T) {
	entries := func() []provider.Message {
		return []provider.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "u1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "u2"},
			{Role: "assistant", Content: "a2"},
			{Role: "user", Content: "u3"},
			{Role: "assistant", Content: "a3"},
			{Role: "user", Content: "u4"},
			{Role: "assistant", Content: "a4"},
			{Role: "user", Content: "u5"},
		}
	}
	countSummary := func(ag *Agent) int {
		n := 0
		for _, m := range ag.history {
			if strings.Contains(m.Content, compress.SummaryPrefix) {
				n++
			}
		}
		return n
	}

	t.Run("non-streaming", func(t *testing.T) {
		ag := newLoopTestAgent(t, &staticTextProvider{text: "answer"}, WithMaxTurns(1))
		ag.compressor = compress.NewCompressor(1) // 阈值 1：必然触发压缩
		ag.history = entries()

		if _, err := ag.RunConversationWithMedia(context.Background(), "look at this image", mediaParts()); err != nil {
			t.Fatalf("RunConversationWithMedia: %v", err)
		}
		if countSummary(ag) != 1 {
			t.Fatalf("media turn did not compact the context (history=%d msgs, summaries=%d)",
				len(ag.history), countSummary(ag))
		}
	})

	t.Run("streaming", func(t *testing.T) {
		ag := newLoopTestAgent(t, &staticTextProvider{text: "answer"}, WithMaxTurns(1))
		ag.compressor = compress.NewCompressor(1)
		ag.history = entries()

		// provider 不实现流式接口 ⇒ 流式入口内部回落到非流式，仍走同一条循环。
		err := ag.RunConversationStreamWithMedia(context.Background(), "look at this image", mediaParts(),
			func(string, bool) {})
		if err != nil {
			t.Fatalf("RunConversationStreamWithMedia: %v", err)
		}
		if countSummary(ag) != 1 {
			t.Fatalf("streaming media turn did not compact the context (history=%d msgs, summaries=%d)",
				len(ag.history), countSummary(ag))
		}
	})
}
