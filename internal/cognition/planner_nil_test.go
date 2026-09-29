package cognition

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/perception"
)

// TestCreatePlanWithNilPerceptionResult 锁死"规则兜底规划必 panic"这个回归。
//
// Planner.CreatePlan 的旧实现无条件解引用 result（result.Intent.Complexity），
// 而当时唯一的 nil 调用方（已随计划模式于 2026-09-29 删除的
// agent.PlanExecutor.createRuleBasedPlan）走的正是最需要兜底的那条路径：
// LLM 规划失败（例如模型在 JSON 前后带散文导致解析失败）→ 回落到规则规划 → panic。
// web 队列会把它 recover 成"整轮内部错误"，bot/gateway 的 goroutine 里则是进程级崩溃。
//
// 调用方现已全部传非 nil，但"允许传 nil"仍是 CreatePlan 的公开契约，这个测试把它钉住 ——
// 免得将来有人看到没有 nil 调用方就把守卫删掉。
func TestCreatePlanWithNilPerceptionResult(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CreatePlan(task, nil) panicked: %v", r)
		}
	}()

	decision := NewPlanner().CreatePlan("Build a login page and test it", nil)
	if decision == nil {
		t.Fatal("expected a non-nil decision")
	}
	if decision.Plan == nil {
		t.Fatal("expected a rule-based plan for a task-shaped input")
	}
	if len(decision.Plan.Steps) == 0 {
		t.Fatal("expected at least one step in the fallback plan")
	}
	if decision.Plan.Complexity != perception.ComplexityMedium {
		t.Fatalf("expected the nil-result default complexity to be medium, got %q",
			decision.Plan.Complexity)
	}

	// 显式传一个中等复杂度的 task 感知结果，路径与兜底一致（不该偷偷偏离）。
	explicit := NewPlanner().CreatePlan("Build a login page and test it", &perception.PerceptionResult{
		Intent: perception.IntentClassification{
			Type:       perception.IntentTask,
			Complexity: perception.ComplexityMedium,
		},
	})
	if explicit.Plan == nil || len(explicit.Plan.Steps) != len(decision.Plan.Steps) {
		t.Fatalf("nil-result fallback diverged from an explicit medium/task perception: %d vs %d steps",
			len(decision.Plan.Steps), len(explicit.Plan.Steps))
	}
}
