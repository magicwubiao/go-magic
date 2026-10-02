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
// 空名工具调用的循环保护（线上：微信 gateway 收到
// "AI processing failed: exceeded maximum turns (300)"，但文件已经生成）
//
// 背景：provider 解析丢字段、或模型吐坏响应时，会出现 function.name 为空的
// 工具调用。这类调用在修复前对三条保护同时不可见：
//  1. executeToolsWithHooks 把它转成合成错误结果，**不落 registry** ⇒ 没有
//     [TOOL] 日志，日志上看着"什么都没发生"；
//  2. recordToolCallsForLoop 显式跳过空名 ⇒ detectToolLoop 永远判不出来；
//  3. executeToolsWithHooks 返回 nil error ⇒ lastErr 保持 nil，没有任何
//     分类器/失败检测器介入。
// 于是四条循环都会一路烧到 maxTurns，最后只回一句裸的
// "exceeded maximum turns"，而此前写好的文件还躺在磁盘上 —— 用户看到的就是
// "活干完了，却只收到一句报错"。
//
// 这里锁死"空名调用必须计入循环阈值并在阈值处提前收口"。maxTurns 特意设成
// 20（远小于内置 150），旧行为会跑满 20 轮后报错而不是断言失败，回归一眼可辨。
// ============================================================================

const emptyToolCallTestMaxTurns = 20

// emptyToolCallProvider 永远返回"没有工具名"的工具调用（参数固定），
// 被要求收口总结时返回 testSummaryMarker。
type emptyToolCallProvider struct {
	mu        sync.Mutex
	toolTurns int
}

func (p *emptyToolCallProvider) Name() string { return "empty-tool-call" }

func (p *emptyToolCallProvider) Chat(_ context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if lastMessageContains(messages, testSummaryPrompt) {
		return &provider.ChatResponse{Content: testSummaryMarker}, nil
	}

	p.toolTurns++
	return &provider.ChatResponse{
		ToolCalls: []types.ToolCall{{
			ID:   fmt.Sprintf("call-%d", p.toolTurns),
			Type: "function",
			// Name 与 Function.Name 同时留空：GetToolName() 两者都读，
			// 任何一个有值都不算"空名调用"。
			Function: types.Function{Name: "", Arguments: `{}`},
		}},
	}, nil
}

func (p *emptyToolCallProvider) snapshot() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.toolTurns
}

// TestEmptyToolCallCountsTowardLoopDetection 钉住"空名调用也进循环历史"。
//
// 这是修复的根：只要它被跳过，四条循环就都没有兜底，只能烧到 maxTurns。
func TestEmptyToolCallCountsTowardLoopDetection(t *testing.T) {
	ag := newLoopTestAgent(t, &emptyToolCallProvider{})

	emptyCall := []types.ToolCall{{Type: "function", Function: types.Function{Name: "", Arguments: `{}`}}}

	for i := 1; i < ag.sameToolLimit; i++ {
		ag.recordToolCallsForLoop(emptyCall)
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("同一空名调用第 %d 次不该触发（阈值 %d），实际: %q", i, ag.sameToolLimit, reason)
		}
	}

	ag.recordToolCallsForLoop(emptyCall)
	if got := ag.toolCallHistoryLength(); got != ag.sameToolLimit {
		t.Fatalf("空名调用没有被记账：历史 %d 条，期望 %d 条", got, ag.sameToolLimit)
	}
	detected, reason := ag.detectToolLoop()
	if !detected {
		t.Fatalf("同一空名调用第 %d 次必须触发死循环判定（阈值 %d）",
			ag.sameToolLimit, ag.sameToolLimit)
	}
	if !strings.Contains(reason, "empty tool call") {
		t.Fatalf("触发原因应点明是空工具调用，实际: %q", reason)
	}
}

// TestEmptyToolCallLoopEndsTurnGracefully 锁死四条入口都在阈值处提前收口。
//
// 四条入口 —— RunConversation、RunConversationWithMedia、
// RunConversationStreamWithMedia（web 聊天 + bot 流式）、RunWithCortex
// （gateway / server 非流式）—— 用的是同一套判定，缺一条就等于给线上留一个
// 静默烧满 maxTurns 的入口。
func TestEmptyToolCallLoopEndsTurnGracefully(t *testing.T) {
	run := func(t *testing.T, ag *Agent, invoke func(ctx context.Context, ag *Agent) (string, error)) {
		t.Helper()
		resp, err := invoke(context.Background(), ag)
		if err != nil {
			t.Fatalf("空名工具调用应被提前收口并给出总结，实际返回错误: %v", err)
		}
		if !strings.Contains(resp, testSummaryMarker) {
			t.Fatalf("应收口为总结文本，实际: %q", resp)
		}
	}

	t.Run("RunConversation", func(t *testing.T) {
		prov := &emptyToolCallProvider{}
		ag := newLoopTestAgent(t, prov, WithMaxTurns(emptyToolCallTestMaxTurns))
		run(t, ag, func(ctx context.Context, ag *Agent) (string, error) {
			return ag.RunConversation(ctx, "keep going")
		})
		toolTurns := prov.snapshot()
		if toolTurns >= emptyToolCallTestMaxTurns {
			t.Fatalf("没有提前收口：跑了 %d 次（maxTurns=%d）", toolTurns, emptyToolCallTestMaxTurns)
		}
		if toolTurns > ag.sameToolLimit {
			t.Fatalf("应在第 %d 次同签名调用处收口，实际 %d 次", ag.sameToolLimit, toolTurns)
		}
	})

	t.Run("RunConversationWithMedia", func(t *testing.T) {
		prov := &emptyToolCallProvider{}
		ag := newLoopTestAgent(t, prov, WithMaxTurns(emptyToolCallTestMaxTurns))
		parts := []types.ContentPart{{Type: "text", Text: "draw"}}
		run(t, ag, func(ctx context.Context, ag *Agent) (string, error) {
			return ag.RunConversationWithMedia(ctx, "keep going", parts)
		})
		if toolTurns := prov.snapshot(); toolTurns > ag.sameToolLimit {
			t.Fatalf("应在第 %d 次同签名调用处收口，实际 %d 次（maxTurns=%d）",
				ag.sameToolLimit, toolTurns, emptyToolCallTestMaxTurns)
		}
	})

	t.Run("RunConversationStream", func(t *testing.T) {
		prov := &emptyToolCallProvider{}
		ag := newLoopTestAgent(t, prov, WithMaxTurns(emptyToolCallTestMaxTurns))
		run(t, ag, func(ctx context.Context, ag *Agent) (string, error) {
			var out strings.Builder
			err := ag.RunConversationStream(ctx, "keep going", func(content string, done bool) {
				out.WriteString(content)
			})
			return out.String(), err
		})
		if toolTurns := prov.snapshot(); toolTurns > ag.sameToolLimit {
			t.Fatalf("应在第 %d 次同签名调用处收口，实际 %d 次（maxTurns=%d）",
				ag.sameToolLimit, toolTurns, emptyToolCallTestMaxTurns)
		}
	})

	t.Run("RunWithCortex", func(t *testing.T) {
		prov := &emptyToolCallProvider{}
		mgr := cortex.NewManagerWithProfileAndConfig(t.TempDir(), prov, "", &cortex.ManagerConfig{Enabled: true})
		if mgr == nil || !mgr.IsEnabled() {
			t.Fatal("precondition failed: expected an enabled cortex manager")
		}
		// 收尾的沉淀抽取是异步写盘，不等落定会撞上 TempDir 清理
		// （断言全绿却报 directory not empty）。本行晚于 t.TempDir() 注册，
		// 按 LIFO 先于目录删除执行。
		t.Cleanup(mgr.WaitPendingWrites)

		ag := newLoopTestAgent(t, prov, WithCortex(mgr), WithMaxTurns(emptyToolCallTestMaxTurns))
		run(t, ag, func(ctx context.Context, ag *Agent) (string, error) {
			return ag.RunConversation(ctx, "keep going")
		})
		// +1 是给 cortex 规划的余量（复杂度达 medium 时先问一次 LLMPlanner，
		// mock 对它同样回工具调用、解析失败后回落）——见
		// TestCortexLoopDetectionAndGuideDrain 的同款容差。
		if toolTurns := prov.snapshot(); toolTurns > ag.sameToolLimit+1 {
			t.Fatalf("cortex 路径应在循环阈值处收口：跑了 %d 次（阈值 %d，maxTurns=%d）",
				toolTurns, ag.sameToolLimit, emptyToolCallTestMaxTurns)
		}
	})
}

// TestMaxTurnsExhaustedErrorCarriesDiagnostics 钉住"回合上限耗尽"的报错内容。
//
// 用户报告过"文件已经生成、却只收到一句 exceeded maximum turns"，裸报错里
// 缺的正是这几项：已完成轮数 / 本回合工具调用次数 / 最近调用过哪些工具。
// 四条入口必须从这里取同一句报错（cortex 与流式此前各写一份裸文案）。
func TestMaxTurnsExhaustedErrorCarriesDiagnostics(t *testing.T) {
	ag := newLoopTestAgent(t, &emptyToolCallProvider{})
	ag.iterationCount = 7
	ag.recordToolCall("write_file")
	ag.recordToolCall(emptyToolCallName)

	err := ag.maxTurnsExhaustedError()
	msg := err.Error()
	for _, want := range []string{
		fmt.Sprintf("exceeded maximum turns (%d)", ag.maxTurns),
		"Completed 7 turns",
		"Recent tools:",
		"write_file",
		emptyToolCallName,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("报错缺少 %q，实际: %s", want, msg)
		}
	}
}
