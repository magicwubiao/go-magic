package agent

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/cognition"
)

// openAITool 构造一条与 server.getToolsSchema 完全同构的工具 schema，
// 用于回归测试。结构必须是 {"type":"function","function":{"name":...}}。
func openAITool(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":       name,
			"parameters": map[string]interface{}{"type": "object"},
		},
	}
}

// TestFilterToolsMatchesByFunctionName 锁死"过滤后工具被清空"的回归。
//
// 旧实现读的是 tool["type"]（恒为字面量 "function"），拿它去比对工具名，
// 永不命中 ⇒ 只要 LLM 计划里的 ToolFilter 非空，agent 就失去全部工具，
// 模型只能反复反问用户（简单任务"老是来问我"的主因）。
func TestFilterToolsMatchesByFunctionName(t *testing.T) {
	ag := &Agent{tools: []map[string]interface{}{
		openAITool("read_file"),
		openAITool("write_file"),
		openAITool("execute_command"),
	}}

	got := ag.filterTools([]string{"read_file", "write_file"})
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d (%v)", len(got), names(got))
	}
	want := map[string]bool{"read_file": true, "write_file": true}
	for _, tool := range got {
		if !want[toolSchemaName(tool)] {
			t.Errorf("unexpected tool survived the filter: %q", toolSchemaName(tool))
		}
	}
	// 被排除的工具必须真的被排除（证明过滤是有作用的，不只是"全放行"）。
	for _, tool := range got {
		if toolSchemaName(tool) == "execute_command" {
			t.Error("execute_command should have been filtered out")
		}
	}
}

// TestFilterToolsNeverReturnsEmpty 锁死防御性兜底：不管模型给出多离谱的
// 过滤器，agent 都不允许被削成"无工具"。失去工具 = 无法完成任务 = 反复
// 反问用户，这个失败模式比"忽略了一个幻觉过滤器"严重得多。
func TestFilterToolsNeverReturnsEmpty(t *testing.T) {
	tools := []map[string]interface{}{
		openAITool("read_file"),
		openAITool("write_file"),
	}
	ag := &Agent{tools: tools}

	cases := []struct {
		name    string
		allowed []string
	}{
		{"hallucinated names", []string{"no_such_tool", "another_ghost"}},
		{"empty slice", []string{}},
		{"nil slice", nil},
		{"only empty strings", []string{"", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ag.filterTools(tc.allowed)
			if len(got) != len(tools) {
				t.Fatalf("filter must fall back to the full tool set, got %d want %d (%v)",
					len(got), len(tools), names(got))
			}
		})
	}
}

// TestPlanMaxTurnsFloor 锁死"模型自填 max_turns 把预算压到个位数"的回归。
// 启发式 planner 会给出 8/15/25，直接采纳会让任务跑不完就收尾反问用户。
func TestPlanMaxTurnsFloor(t *testing.T) {
	const current = 150

	cases := []struct {
		name string
		plan int
		want int // 0 = 不收敛，保持 current
	}{
		{"tiny plan is clamped up to the floor", 8, minCortexMaxTurns},
		{"below-floor plan is clamped", 1, minCortexMaxTurns},
		{"exactly at floor passes through", minCortexMaxTurns, minCortexMaxTurns},
		{"wider-than-floor plan is honoured", 60, 60},
		{"plan equal to budget means no change", current, 0},
		{"plan above budget must not widen it", 900, 0},
		{"missing plan means no change", 0, 0},
		{"negative plan means no change", -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planMaxTurns(&cognition.Decision{MaxTurns: tc.plan}, current)
			if got != tc.want {
				t.Errorf("planMaxTurns(plan=%d, current=%d) = %d, want %d",
					tc.plan, current, got, tc.want)
			}
		})
	}

	if got := planMaxTurns(nil, current); got != 0 {
		t.Errorf("nil plan must not change the budget, got %d", got)
	}
}

// TestPlanMaxTurnsGuardsSmallBudget 小预算下夹取不得反向放大。
// 若当前预算本来就低于下限（例如配置里 max_turns=10），planMaxTurns
// 绝不能把它抬到 minCortexMaxTurns。
func TestPlanMaxTurnsGuardsSmallBudget(t *testing.T) {
	const small = 10
	if got := planMaxTurns(&cognition.Decision{MaxTurns: 3}, small); got != 0 {
		t.Errorf("must not widen a budget smaller than the floor, got %d", got)
	}
}

func names(tools []map[string]interface{}) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, toolSchemaName(tool))
	}
	return out
}
