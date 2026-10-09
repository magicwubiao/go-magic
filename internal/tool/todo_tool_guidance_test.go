package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completionGuidanceMarker 是"完成一个就立刻标记一个、不要攒到最后一次性标记"
// 这条规则在**所有**提示词里共用的标记短语。
//
// 为什么需要这个守卫：线上症状是"对话跑完了，列表里已完成项还挂在 pending"
// （实测 ~/.magic/todos/todos.json：47 条 completed 全部被同会话的 pending 兄弟
// 挡住没被清理）。成因之一是模型把 complete 攒到最后、甚至根本不发。
// 而给模型下这条指令的地方有 5 处（工具 Description 1 + Web 主聊天 1 +
// CLI 两个 prompt + gateway 1），各自独立演进——只改一处，其余入口照旧。
// 这个测试把 5 处绑在同一个短语上：任何一处把规则删掉或改写成模糊表述，立刻红。
const completionGuidanceMarker = "one completion per finished step"

// countGuidance 统计文件中标记短语的出现次数（大小写不敏感）。
func countGuidance(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Count(strings.ToLower(string(data)), completionGuidanceMarker)
}

// TestTodoCompletionGuidancePresentEverywhere 锁定"每完成一个即标记"这条规则
// 存在于每一个给模型下待办指令的入口。
func TestTodoCompletionGuidancePresentEverywhere(t *testing.T) {
	// ① 工具 Description：所有入口共用（TUI / Web / 网关 / bot / cron）。
	desc := strings.ToLower(GetTodoTool().Description())
	if !strings.Contains(desc, completionGuidanceMarker) {
		t.Fatalf("todo tool description no longer requires per-step completion; "+
			"expected %q in:\n%s", completionGuidanceMarker, GetTodoTool().Description())
	}

	// ② 工具返回值：模型每回合都会重读工具结果，比描述更靠近决策点。
	//    （下面 TestTodoCreateResultRemindsImmediateCompletion 单独验 create 的 message）

	// ③ 三份系统提示词源码。CLI 有两份（默认 + coding 模式），必须两处都有。
	targets := []struct {
		path  string
		label string
		want  int
	}{
		{filepath.Join("..", "server", "server.go"), "web chat system prompt", 1},
		{filepath.Join("..", "..", "cmd", "magic", "chat.go"), "CLI system prompts (default + coding)", 2},
		{filepath.Join("..", "..", "cmd", "magic", "gateway.go"), "gateway system prompt", 1},
	}
	for _, tg := range targets {
		if got := countGuidance(t, tg.path); got < tg.want {
			t.Fatalf("%s: expected at least %d occurrence(s) of %q, found %d — "+
				"该入口的\"完成一个标一个\"规则被删掉或改写了",
				tg.path, tg.want, completionGuidanceMarker, got)
		}
	}
}

// TestTodoCreateResultRemindsImmediateCompletion 确认 create 的返回值里带着
// 收尾提醒——这是模型看到"计划已建好"的同一时刻，最有效的纠偏点。
func TestTodoCreateResultRemindsImmediateCompletion(t *testing.T) {
	tt := newTestTodoTool(t)
	ctx := WithSessionID(context.Background(), "sess-guidance")

	res, err := tt.Execute(ctx, map[string]interface{}{"action": "create", "title": "step-one"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	msg, _ := res.(map[string]interface{})["message"].(string)
	if !strings.Contains(strings.ToLower(msg), completionGuidanceMarker) {
		t.Fatalf("create result message must remind per-step completion, got %q", msg)
	}
}
