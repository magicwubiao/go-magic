package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// activePlanMarker 是 activePlanBlock 注入的权威计划块前缀。
const activePlanMarker = "[Active Plan]"

// systemTextOf 把一份出站请求里所有 system 消息拼起来，方便断言"注入了什么"。
func systemTextOf(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TestActivePlanSurvivesCompaction 是本轮根治的核心回归。
//
// 2026-10-09 线上事故：模型 20:08 建了 4 条待办、只标完 1 条（压缩之前那条），
// 之后 21 分钟再没发过 complete —— 因为待办 ID 只存在于 create 的工具结果里，
// 而它落在被历史压缩摘要掉的中段；摘要既不保留 ID，SummaryPrefix 还明说
// "早先的事已经处理完了、只回应最新用户消息"。
//
// 所以这里**故意把历史造成"已经没有待办工具结果"的形态**（压缩后的真实样子），
// 断言出站请求里仍然拿得到权威的在案计划与 ID。
func TestActivePlanSurvivesCompaction(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	ag.SetSession("sess-plan")
	ag.todoPlanSnapshot = func(sessionID string) []tool.TodoPlanEntry {
		if sessionID != "sess-plan" {
			t.Errorf("plan snapshot should target the agent session, got %q", sessionID)
		}
		return []tool.TodoPlanEntry{
			{ID: "todo_dm0ao00gapr8_ffac4946", Title: "生成 css/base.css 与各页面专属 CSS", Status: "in_progress"},
			{ID: "todo_dm0ao00hvue4_385ba6e7", Title: "渲染验证与交付说明", Status: "pending"},
		}
	}

	// 压缩之后的历史：中段全被换成一条摘要，待办工具结果**一条都不剩**。
	ag.history = []provider.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "将css文件提取出来"},
		{Role: "system", Content: compress.SummaryPrefix + "\n\n(css 抽取工作已在进行中)"},
		{Role: "assistant", Content: "continuing the extraction"},
		{Role: "user", Content: "继续"},
	}
	// 前提确认：历史里确实已经没有待办工具结果了（否则这个测试测不到东西）。
	if n := countTodoToolMessages(ag.history); n != 0 {
		t.Fatalf("fixture must start with zero todo tool results, got %d", n)
	}

	req := ag.buildLLMMessages()
	text := systemTextOf(req)

	if !strings.Contains(text, activePlanMarker) {
		t.Fatalf("active plan block must be injected into the request, got system text:\n%s", text)
	}
	// ID 是 complete/update 的唯一凭据 —— 这是整件事的重点。
	for _, id := range []string{"todo_dm0ao00gapr8_ffac4946", "todo_dm0ao00hvue4_385ba6e7"} {
		if !strings.Contains(text, id) {
			t.Fatalf("plan block must carry the todo id %q so the model can complete it, got:\n%s", id, text)
		}
	}
	for _, want := range []string{"AUTHORITATIVE", "[in_progress]", "[pending]", "action=complete", "one completion per finished step"} {
		if !strings.Contains(text, want) {
			t.Fatalf("plan block must contain %q, got:\n%s", want, text)
		}
	}

	// 幂等：注入发生在**出站副本**上，不得写回历史，也不能每轮叠加。
	histLen := len(ag.history)
	again := ag.buildLLMMessages()
	if len(ag.history) != histLen {
		t.Fatalf("building the request must not mutate history (len %d -> %d)", histLen, len(ag.history))
	}
	if n := strings.Count(systemTextOf(again), activePlanMarker); n != 1 {
		t.Fatalf("plan block must appear exactly once per request, got %d", n)
	}
}

// TestActivePlanBlockConditions 固定注入条件的边界：没会话 / 没有未完成项
// 都不得注入；条目过多要裁剪；标题里的换行不能打断列表结构。
func TestActivePlanBlockConditions(t *testing.T) {
	t.Run("no session", func(t *testing.T) {
		ag := newLoopTestAgent(t, &scriptedLoopProvider{})
		ag.todoPlanSnapshot = func(string) []tool.TodoPlanEntry {
			return []tool.TodoPlanEntry{{ID: "todo_1", Title: "x", Status: "pending"}}
		}
		if block := ag.activePlanBlock(); block != "" {
			t.Fatalf("no session => no plan block (否则会把别人的计划注入本次请求), got %q", block)
		}
	})

	t.Run("nothing unfinished", func(t *testing.T) {
		ag := newLoopTestAgent(t, &scriptedLoopProvider{})
		ag.SetSession("sess-x")
		ag.todoPlanSnapshot = func(string) []tool.TodoPlanEntry { return nil }
		if block := ag.activePlanBlock(); block != "" {
			t.Fatalf("no unfinished item => no plan block (不给无关回合加噪声), got %q", block)
		}
	})

	t.Run("caps the list and flattens titles", func(t *testing.T) {
		ag := newLoopTestAgent(t, &scriptedLoopProvider{})
		ag.SetSession("sess-x")
		ag.todoPlanSnapshot = func(string) []tool.TodoPlanEntry {
			entries := make([]tool.TodoPlanEntry, 0, 13)
			for i := 0; i < 13; i++ {
				entries = append(entries, tool.TodoPlanEntry{
					ID:     "todo_" + string(rune('a'+i)),
					Title:  "step",
					Status: "pending",
				})
			}
			// 模型自己写的标题可能带换行 —— 那会打断 "- id=... " 的列表结构。
			entries[0].Title = "多行\n标题\r\n第二行"
			return entries
		}
		block := ag.activePlanBlock()
		if !strings.Contains(block, "...and 1 more") {
			t.Fatalf("13 entries should be capped at the injection budget, got:\n%s", block)
		}
		if strings.Contains(block, "多行\n标题") {
			t.Fatalf("titles must be flattened to a single line, got:\n%s", block)
		}
		if !strings.Contains(block, "多行 标题 第二行") {
			t.Fatalf("flattened title must keep its words, got:\n%s", block)
		}
	})
}

// TestCountTodoToolMessages 锁住"待办状态是否被摘要出上下文"这个观测点。
func TestCountTodoToolMessages(t *testing.T) {
	hist := []provider.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{{
			ID: "c1", Name: "todo", Arguments: map[string]interface{}{"action": "create"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: `{"id":"todo_1"}`},
		// 少数 provider 只填 Function.Name，兜底必须能认出它。
		{Role: "assistant", ToolCalls: []types.ToolCall{{
			ID: "c2", Function: types.Function{Name: "todo", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "c2", Content: `{"id":"todo_2"}`},
		{Role: "assistant", ToolCalls: []types.ToolCall{{
			ID: "c3", Name: "read_file", Arguments: map[string]interface{}{"path": "a.css"},
		}}},
		{Role: "tool", ToolCallID: "c3", Content: "body"},
		// 没有归属的 tool 消息不能算进来。
		{Role: "tool", ToolCallID: "orphan", Content: "?"},
	}
	if got := countTodoToolMessages(hist); got != 2 {
		t.Fatalf("expected 2 todo tool results, got %d", got)
	}
}

// compactionHistory 造一份"待办建在中段"的历史，复刻 2026-10-09 线上形态：
// 待办不是回合开头建的（那样会被 safeHeadEnd 拉进受保护的头部），而是干了一段
// 活之后才建，于是它落在中段 —— 正是会被摘要掉的位置。
//
// 工具结果刻意给足体积（真实的 read_file 动辄几 KB）：兜底摘要是"每条观察保留
// 1500 字符头部"，历史太短时摘要反而可能比被替换的中段还长，那样就测不到
// "历史被缩短"了。
func compactionHistory() []provider.Message {
	body := func(tag string) string {
		return "body-" + tag + ":" + strings.Repeat("x", 3000)
	}
	readCall := func(id string) provider.Message {
		return provider.Message{Role: "assistant", ToolCalls: []types.ToolCall{{
			ID: id, Name: "read_file", Arguments: map[string]interface{}{"path": "a.css"},
		}}}
	}
	return []provider.Message{
		{Role: "user", Content: "req"},
		readCall("r0"),
		{Role: "tool", ToolCallID: "r0", Content: body("0")},
		{Role: "assistant", Content: "分析中"},
		{Role: "user", Content: "continue"},
		// ↓ 待办：创建 + 结果，落在中段
		{Role: "assistant", ToolCalls: []types.ToolCall{{
			ID: "t1", Name: "todo", Arguments: map[string]interface{}{"action": "create", "title": "step-1"},
		}}},
		{Role: "tool", ToolCallID: "t1", Content: `{"id":"todo_live_1","title":"step-1"}`},
		readCall("r1"),
		{Role: "tool", ToolCallID: "r1", Content: body("1")},
		{Role: "assistant", Content: "写文件中"},
		{Role: "user", Content: "again"},
		readCall("r2"),
		{Role: "tool", ToolCallID: "r2", Content: body("2")},
		readCall("r3"),
		{Role: "tool", ToolCallID: "r3", Content: body("3")},
		{Role: "assistant", Content: "working"},
		{Role: "user", Content: "ok"},
		readCall("r4"),
		{Role: "tool", ToolCallID: "r4", Content: body("4")},
		{Role: "assistant", Content: "still working"},
	}
}

// TestCompactionEvictsTodoStateAndPlanCoversIt 是端到端回归：真的跑一次历史压缩，
// 确认（a）待办工具结果确实被摘要出上下文 —— 也就是事故的成因，
// 以及（b）即便如此，出站请求里依然拿得到权威在案计划与 ID —— 也就是本轮的修法。
//
// 这两个断言必须放在一起：只测 (b) 会掩盖"压缩确实会吃掉待办"这一事实，
// 只测 (a) 则等于承认问题无解。
func TestCompactionEvictsTodoStateAndPlanCoversIt(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	ag.SetSession("sess-compact")
	ag.history = compactionHistory()

	// 小压缩器：ProtectFirstN/LastN 都收到最小，保证中段被摘要。
	ag.compressor = compress.NewCompressor(1)
	ag.compressor.ProtectFirstN = 2
	ag.compressor.ProtectLastN = 2

	beforeTodo := countTodoToolMessages(ag.history)
	beforeChars := ag.GetHistoryLength()
	if beforeTodo != 1 {
		t.Fatalf("fixture must contain exactly 1 todo tool result, got %d", beforeTodo)
	}

	if !ag.compressContext() {
		t.Fatalf("compression should have shortened the history")
	}
	if ag.GetHistoryLength() >= beforeChars {
		t.Fatalf("history should shrink: %d -> %d", beforeChars, ag.GetHistoryLength())
	}
	// (a) 事故成因：待办结果（ID 的唯一载体）被摘要掉了。
	if after := countTodoToolMessages(ag.history); after != 0 {
		t.Fatalf("the todo tool result should have been summarised away, still %d present", after)
	}

	// (b) 修法：模型仍然拿得到 ID。
	ag.todoPlanSnapshot = func(sessionID string) []tool.TodoPlanEntry {
		if sessionID != "sess-compact" {
			t.Errorf("plan snapshot should target the agent session, got %q", sessionID)
		}
		return []tool.TodoPlanEntry{{ID: "todo_live_1", Title: "step-1", Status: "pending"}}
	}
	text := systemTextOf(ag.buildLLMMessages())
	if !strings.Contains(text, "todo_live_1") {
		t.Fatalf("after compaction the model must still be able to see the todo id, got:\n%s", text)
	}
	if !strings.Contains(text, activePlanMarker) {
		t.Fatalf("active plan block missing after compaction, got:\n%s", text)
	}
}
