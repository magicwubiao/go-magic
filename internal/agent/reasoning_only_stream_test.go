package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// reasoningOnlyStreamProvider 模拟 DeepSeek 等 reasoning 模型的一种真实行为：
// 把全部产出放在 reasoning_content，content 始终为空。
//
// 这是"反复询问用户"事故的输入条件——旧实现会把这段 reasoning 当作正文
// 落库，历史里只留下没有闭合标签、也没有结论的 "<think>..."，模型下一轮
// 读到自己残缺的历史只能重新推导、重新确认。
type reasoningOnlyStreamProvider struct {
	mu        sync.Mutex
	reasoning string
	turns     int
}

func (p *reasoningOnlyStreamProvider) Name() string { return "reasoning-only" }

// Chat 非流式路径：同样只给 ReasoningContent。
func (p *reasoningOnlyStreamProvider) Chat(context.Context, []provider.Message) (*provider.ChatResponse, error) {
	return &provider.ChatResponse{ReasoningContent: p.reasoning}, nil
}

// StreamWithTools 只推 reasoning，content 恒为空，最后发 Done。
func (p *reasoningOnlyStreamProvider) StreamWithTools(
	_ context.Context,
	_ []provider.Message,
	_ []map[string]interface{},
	handler provider.StreamHandler,
) error {
	p.mu.Lock()
	p.turns++
	p.mu.Unlock()

	handler(&provider.StreamResponse{ReasoningContent: p.reasoning})
	handler(&provider.StreamResponse{ReasoningContent: " Still thinking..."})
	handler(&provider.StreamResponse{Done: true})
	return nil
}

// TestReasoningOnlyTurnIsNotReportedAsAnswer 锁死"reasoning 不得冒充正文"。
//
// 断言两件事：
//  1. 收口文本里不能出现 reasoning 原文（那是思考，不是回答）；
//  2. 历史里不得留下"只有 <think> 没有正文"的 assistant 消息。
func TestReasoningOnlyTurnIsNotReportedAsAnswer(t *testing.T) {
	const secret = "SECRET-REASONING-MUST-NOT-BECOME-ANSWER"
	prov := &reasoningOnlyStreamProvider{reasoning: secret}
	ag := newLoopTestAgent(t, prov, WithMaxTurns(3))

	var out strings.Builder
	// 流式入口失败（无正文）后可能回落非流式；两条路径都不许把 reasoning
	// 当正文。这里忽略返回错误——关键是"产出内容"与"历史"两个事实。
	_ = ag.RunConversationStream(context.Background(), "retag v0.5.22", func(content string, done bool) {
		out.WriteString(content)
	})

	if strings.Contains(out.String(), secret) {
		t.Fatalf("raw reasoning was pushed to the client as the answer: %q", out.String())
	}

	for _, m := range ag.getHistory() {
		if m.Role != "assistant" {
			continue
		}
		if strings.Contains(m.Content, secret) {
			t.Fatalf("raw reasoning was written into the assistant history: %q", m.Content)
		}
		if strings.TrimSpace(provider.StripThinkTrails(m.Content)) == "" && strings.Contains(m.Content, "<think") {
			t.Fatalf("history kept an assistant message with <think> but no body: %q", m.Content)
		}
	}
}

// 防止 types 包被判定为未使用（保持与其它测试一致的最小引用）。
var _ = types.ContentPart{}
