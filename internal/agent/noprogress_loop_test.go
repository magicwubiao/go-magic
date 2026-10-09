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
		t.Fatalf("precondition failed: this case requires steps(%d) > consecutiveLimit(%d)", steps, ag.consecutiveLimit)
	}

	resp, err := ag.RunConversation(context.Background(), "refactor the whole module")
	if err != nil {
		t.Fatalf("a multi-step task should not fail: %v", err)
	}
	if resp != testFinalAnswerMarker {
		t.Fatalf("task cut short: expected the real final answer, got %q (the total call count looks like it is still cutting it off)", resp)
	}

	calls, summaryReq := prov.snapshot()
	if calls != steps {
		t.Fatalf("should complete all %d steps, got %d", steps, calls)
	}
	if summaryReq != 0 {
		t.Fatalf("a multi-step task making progress must not be asked to conclude, asked %d times", summaryReq)
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
		t.Fatalf("a dead loop should end with a gentle summary instead of an error: %v", err)
	}
	if resp != testSummaryMarker {
		t.Fatalf("a dead loop should conclude with a summary, got %q", resp)
	}

	toolTurns, _, _ := prov.snapshot()
	if toolTurns != ag.sameToolLimit {
		t.Fatalf("should conclude at identical-signature call #%d, got %d calls (maxTurns=%d)",
			ag.sameToolLimit, toolTurns, ag.maxTurns)
	}
}

// TestInterleavedRepeatIsNotALoop 锁死"长任务跑一阵就停"的线上故障。
//
// 旧实现的 sameToolLimit 判定用的是**本回合累计**次数：同一个(工具+参数)
// 在回合里第 3 次出现就收口，哪怕中间夹着大量有进展的调用。长任务里
// "读同一个文件三次以确认改动"、"同一条 build 命令跑三次"必然撞上它 ——
// concludeAfterToolLoop 立刻要求模型"不要再调工具，给总结"，用户看到的就是
// 任务跑到一半突然收尾（会话里会留下合成提示词 "Please provide a final
// summary ... Do not call any more tools"），只能手动说"继续处理"。
//
// 本用例构造"同一签名间隔出现、每次之间都夹着新签名的进展"，断言不得收口；
// 旧实现下第 3 次 read_file 就会触发。
func TestInterleavedRepeatIsNotALoop(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})

	const repeats = 5 // 远超 sameToolLimit(3) 的累计次数
	for i := 0; i < repeats; i++ {
		// 同一个调用（读同一个文件）反复出现……
		ag.recordToolCallSig("read_file", `{"path":"contact.html"}`)
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("spaced repetition #%d should not be flagged as a dead loop, got: %q", i+1, reason)
		}
		// ……但每次之间都夹着**新签名**的进展（改不同的文件）。
		for j := 0; j < ag.sameToolLimit; j++ {
			ag.recordToolCallSig("edit_file", fmt.Sprintf(`{"path":"file_%d.go"}`, i*10+j))
		}
	}

	if detected, reason := ag.detectToolLoop(); detected {
		t.Fatalf("spaced repetition with progress must not trigger loop conclusion, got: %q", reason)
	}
	if got, want := ag.toolCallHistoryLength(), repeats*(1+ag.sameToolLimit); got != want {
		t.Fatalf("call record count mismatch: got %d, want %d", got, want)
	}
}

// TestRepeatedResourceReadsWithoutModificationStop 锁死 2026-10-08 线上事故：
// "一个简单的任务一直在执行"。
//
// 事故形态（web 聊天、任务"把顶部导航改成几个已实现的页面 + 语言切换 + GitHub
// 地址"，工作目录 D:\project\article）：模型在 20:58 就改完了文件，之后又在
// 18 分钟里重新读同一批文件 80 余次、一个字没写，直到 30 分钟回合超时才被砍掉。
// 现场证据是这 4 个文件的 atime 一直在刷新、mtime 停在 20:58；session 里留下了
// 289,702 字符的循环念白（"Let me read the actual nav markup in all three files."
// 反复出现）。
//
// 这类调用**每一步签名都不同**（换文件轮着读、换关键词接着搜），所以前两道判据
// 全部失守：sameToolLimit 要求"连续 3 次完全相同"，consecutiveLimit 只要中间插入
// 一个新签名就清零。本用例复刻这个形态（三个文件轮着读、零修改），断言必须收口；
// 在加入 repeatedResourceLimit 之前，这里会一直读到 maxTurns / 回合超时。
func TestRepeatedResourceReadsWithoutModificationStop(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	limit := ag.repeatedResourceLimit
	if limit <= 1 {
		t.Fatalf("precondition failed: repeatedResourceLimit=%d", limit)
	}

	detected, reason := false, ""
	for i := 0; i < limit && !detected; i++ {
		for _, f := range []string{"index.html", "docs.html", "contact.html"} {
			ag.recordToolCallSig("read_file", fmt.Sprintf(`{"path":%q}`, f))
			if d, r := ag.detectToolLoop(); d {
				detected, reason = true, r
				break
			}
		}
	}
	if !detected {
		t.Fatalf("re-reading the same files with no modification in between must count as spinning (limit %d)", limit)
	}
	t.Logf("conclusion reason: %s", reason)
}

// TestReadsInterleavedWithModificationAreNotALoop 是上一条的反向保护：
// "改一次 → 读一次确认"重复再多轮都属于正常调试，不得被误杀。
func TestReadsInterleavedWithModificationAreNotALoop(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})

	for i := 0; i < ag.repeatedResourceLimit*4; i++ {
		ag.recordToolCallSig("file_edit",
			fmt.Sprintf(`{"path":"index.html","old_string":"v%d","new_string":"v%d"}`, i, i+1))
		ag.recordToolCallSig("read_file", `{"path":"index.html"}`)
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("round %d: post-edit verification was misjudged as spinning: %q", i+1, reason)
		}
	}
}

// TestDistinctResourceReadsAreNotALoop：读一堆**不同**的文件是正常工作方式，
// 无论多少次都不该触发（这条保护 toolCallSignature 注释里说的"合法重复"）。
func TestDistinctResourceReadsAreNotALoop(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	for i := 0; i < ag.repeatedResourceLimit*5; i++ {
		ag.recordToolCallSig("read_file", fmt.Sprintf(`{"path":"file_%d.go"}`, i))
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("reading different files must not be misjudged as spinning: %q", reason)
		}
	}
}

// TestResourceLessToolsAreNotCounted：参数里没有路径/关键词等"对象标识"的工具
// （如 kanban_create 批量建任务）不参与重复计数——它们的"对象"不可比较，
// 硬数会把一次并行建多个任务这类正常批量操作误杀。
func TestResourceLessToolsAreNotCounted(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	for i := 0; i < ag.repeatedResourceLimit*4; i++ {
		ag.recordToolCallSig("kanban_create", fmt.Sprintf(`{"title":"task %d"}`, i))
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("tools without a resource key must not count towards repetition: %q", reason)
		}
	}
}
