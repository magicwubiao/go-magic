package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// ============================================================================
// 工具循环保护：四条入口必须等价
//
// 背景：agent 有四条工具循环 —— RunConversation、RunConversationWithMedia、
// RunConversationStreamWithMedia（web 聊天 + bot 流式）、RunWithCortex
// （gateway + server 非流式）。历史上只有前两条挂了
// sameToolLimit/consecutiveLimit 判定，后两条漏挂：工具死循环会一路烧到
// maxTurns，最后只回一句裸的 "exceeded maximum turns"。
//
// 这里锁死"两条补上的入口确实会提前收口"，否则它俩会再次退化成静默的
// 无限循环（回归表现是测试跑满 maxTurns 后报错，而不是断言失败）。
// ============================================================================

const (
	testLoopToolName = "loop_tool"
	// 收口提问里的特征串（见 concludeAfterToolLoop）。mock 据此判断
	// "这次是让我做总结"，返回 testSummaryMarker 而不是又一个工具调用。
	testSummaryPrompt = "final summary of what has been accomplished"
	testSummaryMarker = "LOOP-SUMMARY-MARKER"
)

// scriptedLoopProvider 永远重复调用同一个工具，直到被问到收口总结为止。
//
// 同时记录三类事实供断言：返回过几次工具调用、模型是否看见过引导文本、
// 一共被调用了几次。不实现流式接口 —— 流式入口会自动回落到非流式，
// 那条兜底路径同样经过循环判定。
type scriptedLoopProvider struct {
	mu        sync.Mutex
	toolTurns int
	chatCalls int
	sawGuide  bool
}

func (p *scriptedLoopProvider) Name() string { return "scripted-loop" }

func (p *scriptedLoopProvider) Chat(_ context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.chatCalls++
	for _, m := range messages {
		if strings.Contains(m.Content, guidePrefix) {
			p.sawGuide = true
		}
	}

	if lastMessageContains(messages, testSummaryPrompt) {
		return &provider.ChatResponse{Content: testSummaryMarker}, nil
	}

	p.toolTurns++
	return &provider.ChatResponse{
		ToolCalls: []types.ToolCall{{
			ID:   fmt.Sprintf("call-%d", p.toolTurns),
			Type: "function",
			Function: types.Function{
				Name:      testLoopToolName,
				Arguments: `{}`,
			},
		}},
	}, nil
}

func (p *scriptedLoopProvider) snapshot() (toolTurns int, sawGuide bool, chatCalls int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.toolTurns, p.sawGuide, p.chatCalls
}

func lastMessageContains(messages []provider.Message, needle string) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.Contains(messages[i].Content, needle) {
			return true
		}
	}
	return false
}

func newLoopTestAgent(t *testing.T, prov provider.Provider, opts ...AgentOption) *Agent {
	t.Helper()
	registry := &mockRegistry{tools: map[string]func(map[string]interface{}) (string, error){
		testLoopToolName: func(map[string]interface{}) (string, error) { return "ok", nil },
	}}
	return NewEnhancedAgent(prov, registry, nil, "You are a helpful assistant.", opts...)
}

// TestStreamLoopDetectionEndsTurnGracefully 锁死流式入口的循环保护。
//
// 这条路径是 web 聊天与 bot 流式的实际入口（chatqueue.go 的
// runAgentStreamWithMedia → RunConversationStreamWithMedia）。它此前完全没有
// 循环判定：模型只要反复调同一个工具，就会把 maxTurns 全烧完。
func TestStreamLoopDetectionEndsTurnGracefully(t *testing.T) {
	prov := &scriptedLoopProvider{}
	ag := newLoopTestAgent(t, prov)

	var out strings.Builder
	if err := ag.RunConversationStream(context.Background(), "keep going", func(content string, done bool) {
		out.WriteString(content)
	}); err != nil {
		t.Fatalf("stream path should end with a graceful summary, got error: %v", err)
	}

	toolTurns, _, _ := prov.snapshot()
	if toolTurns != ag.sameToolLimit {
		t.Fatalf("循环判定应在第 %d 次同工具调用时收口，实际跑了 %d 次（maxTurns=%d）",
			ag.sameToolLimit, toolTurns, ag.maxTurns)
	}

	// 收口文本必须被推给客户端：流式增量已在前面推过，客户端的最终
	// 可见内容里必须包含总结，否则用户只会看到"卡住后突然结束"。
	if !strings.Contains(out.String(), testSummaryMarker) {
		t.Fatalf("收口总结没有推给客户端，实际输出: %q", out.String())
	}
}

// TestDetectToolLoopBoundary 固定两条阈值的语义。
//
// 统一前的两个实现阈值语义不一致：RunConversation 用 `count >= sameToolLimit`
// （默认 3 次触发），RunConversationWithMedia 用 `> sameToolLimit`（要 4 次）。
// 既然收敛成一套判定，就把 `>=` 这一侧钉住，避免将来又被改回去。
func TestDetectToolLoopBoundary(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})

	for i := 1; i < ag.sameToolLimit; i++ {
		ag.recordToolCall(testLoopToolName)
		if detected, _ := ag.detectToolLoop(); detected {
			t.Fatalf("同一工具第 %d 次调用不该触发（阈值 %d）", i, ag.sameToolLimit)
		}
	}
	ag.recordToolCall(testLoopToolName)
	detected, reason := ag.detectToolLoop()
	if !detected {
		t.Fatalf("同一工具第 %d 次调用必须触发（阈值 %d）", ag.sameToolLimit, ag.sameToolLimit)
	}
	if !strings.Contains(reason, testLoopToolName) {
		t.Fatalf("触发原因应点名工具，实际: %q", reason)
	}

	// 连续调用上限：每轮换一个工具名，绕开"同一工具"判定。
	ag2 := newLoopTestAgent(t, &scriptedLoopProvider{})
	for i := 0; i < ag2.consecutiveLimit-1; i++ {
		ag2.recordToolCall(fmt.Sprintf("tool_%d", i))
		if detected, _ := ag2.detectToolLoop(); detected {
			t.Fatalf("连续第 %d 次调用不该触发（上限 %d）", i+1, ag2.consecutiveLimit)
		}
	}
	ag2.recordToolCall(fmt.Sprintf("tool_%d", ag2.consecutiveLimit-1))
	if detected, reason := ag2.detectToolLoop(); !detected {
		t.Fatalf("连续第 %d 次调用必须触发（上限 %d）", ag2.consecutiveLimit, ag2.consecutiveLimit)
	} else if !strings.Contains(reason, "consecutive") {
		t.Fatalf("触发原因应说明是连续调用，实际: %q", reason)
	}
}

// TestCortexLoopDetectionAndGuideDrain 锁死 cortex 分流路径的两处缺口。
//
// 同一个 agent 上验证两件事：
//  1. 工具循环检测（此前完全没有，会烧到 maxTurns 后返回裸错误）；
//  2. 迭代顶部把引导并入历史（此前这条入口漏挂 drainGuidesIntoHistory，
//     模型在本回合里看不到用户中途插的话）。
//
// 断言方式：cortex 路径在跑满 maxTurns 时返回错误、在被判定为循环时返回
// 总结文本，所以 err == nil + toolTurns 很小 就是"提前收口"的判据。
func TestCortexLoopDetectionAndGuideDrain(t *testing.T) {
	prov := &scriptedLoopProvider{}

	mgr := cortex.NewManagerWithProfileAndConfig(t.TempDir(), prov, "", &cortex.ManagerConfig{Enabled: true})
	if mgr == nil || !mgr.IsEnabled() {
		t.Fatal("precondition failed: expected an enabled cortex manager")
	}
	// 收尾的沉淀抽取是异步写盘（endCortexTurn → EndSessionWithHistoryAsync）。
	// 不等它落定，就会撞上 TempDir 清理：断言全绿、却报
	// `unlinkat ...: directory not empty`（CI -race 上实测，本机偶发不中）。
	// 本行注册晚于 t.TempDir()，按 LIFO 先于目录删除执行。
	t.Cleanup(mgr.WaitPendingWrites)

	ag := newLoopTestAgent(t, prov, WithCortex(mgr))
	// 回合开始前注入：迭代 0 排水时并入尾随的本回合输入。
	ag.InjectGuide("guide-1", "switch to plan B")

	resp, err := ag.RunConversation(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("cortex path should end with a graceful summary, got error: %v", err)
	}
	if resp != testSummaryMarker {
		t.Fatalf("expected the loop-conclusion summary, got %q", resp)
	}

	toolTurns, sawGuide, _ := prov.snapshot()
	// +1 是给 LLM 规划的余量（复杂度判定为 medium 以上时 cortex 会问一次
	// "LLMPlanner"，mock 对它的答复同样是工具调用、解析失败后回落）。
	if toolTurns > ag.sameToolLimit+1 {
		t.Fatalf("cortex 路径没有在循环阈值处收口：跑了 %d 次工具调用（阈值 %d，maxTurns=%d）",
			toolTurns, ag.sameToolLimit, ag.maxTurns)
	}
	if !sawGuide {
		t.Fatal("cortex 路径没有把引导并入出站历史（drainGuidesIntoHistory 缺失）")
	}
}
