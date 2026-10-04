package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// newDistinctCallProvider 每次都发起一个**签名各不相同**的工具调用：
// 工具名与参数都随轮次变化，代表一个正在稳步推进的多步任务
// （读不同文件、改不同位置）。跑满 totalCalls 次后返回最终答案。
type newDistinctCallProvider struct {
	mu         sync.Mutex
	calls      int
	totalCalls int
	summaryReq int
}

func (p *newDistinctCallProvider) Name() string { return "distinct-call" }

func (p *newDistinctCallProvider) Chat(_ context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if lastMessageContains(messages, testSummaryPrompt) {
		p.summaryReq++
		return &provider.ChatResponse{Content: testSummaryMarker}, nil
	}
	if p.calls >= p.totalCalls {
		return &provider.ChatResponse{Content: testFinalAnswerMarker}, nil
	}
	idx := p.calls
	p.calls++
	return &provider.ChatResponse{
		ToolCalls: []types.ToolCall{{
			ID:   fmt.Sprintf("call-%d", idx),
			Type: "function",
			Function: types.Function{
				Name:      testLoopToolName,
				Arguments: fmt.Sprintf(`{"step":%d,"target":"file_%d.go"}`, idx, idx),
			},
		}},
	}, nil
}

func (p *newDistinctCallProvider) snapshot() (calls, summaryReq int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.summaryReq
}

// TestMultiStepTaskIsNotCutOffByCallVolume 锁死"几十轮就停止"的线上故障。
//
// 旧实现把 consecutiveLimit 当作**单回合调用总量**上限（默认 25）：一个需要
// 30+ 次各不相同调用的正常任务，会在第 25 次被 detectToolLoop 判成死循环，
// concludeAfterToolLoop 立刻要求模型"不要再调工具，给个总结"——用户看到的就是
// "几十轮就停止"，且与 maxTurns(150) 是两条完全不同的死因。
//
// 本用例用 30 次签名各异的调用（超过旧阈值 25）跑一轮完整任务，断言：
// 任务必须真正跑完（拿到最终答案），且**不得**被收口。
func TestMultiStepTaskIsNotCutOffByCallVolume(t *testing.T) {
	const steps = 30 // 明显超过旧的 consecutiveLimit(25)
	prov := &newDistinctCallProvider{totalCalls: steps}

	ag := newLoopTestAgent(t, prov)
	if ag.consecutiveLimit >= steps {
		t.Fatalf("前置条件失效：本用例要求 steps(%d) > consecutiveLimit(%d)", steps, ag.consecutiveLimit)
	}

	resp, err := ag.RunConversation(context.Background(), "refactor the whole module")
	if err != nil {
		t.Fatalf("多步任务不应报错：%v", err)
	}
	if resp != testFinalAnswerMarker {
		t.Fatalf("任务被提前打断：期望真实最终答案，实际 %q（疑似仍被调用总量腰斩）", resp)
	}

	calls, summaryReq := prov.snapshot()
	if calls != steps {
		t.Fatalf("应完成全部 %d 步，实际 %d 步", steps, calls)
	}
	if summaryReq != 0 {
		t.Fatalf("有进展的多步任务不该被要求收口，实际被收口 %d 次", summaryReq)
	}
}

// TestRepeatedSignatureStillStops 保证收紧口径后死循环仍能被及时收口——
// 修 bug 不能把保护一起修掉。用同一个(工具+参数)反复调用，必须在
// sameToolLimit 处收口，而不是烧满 maxTurns。
func TestRepeatedSignatureStillStops(t *testing.T) {
	prov := &scriptedLoopProvider{} // 永远重复同一个 {} 调用
	ag := newLoopTestAgent(t, prov)

	resp, err := ag.RunConversation(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("死循环应以温和总结收口而非报错：%v", err)
	}
	if resp != testSummaryMarker {
		t.Fatalf("死循环应收口给总结，实际 %q", resp)
	}

	toolTurns, _, _ := prov.snapshot()
	if toolTurns != ag.sameToolLimit {
		t.Fatalf("应在第 %d 次同签名调用处收口，实际 %d 次（maxTurns=%d）",
			ag.sameToolLimit, toolTurns, ag.maxTurns)
	}
}
