package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockFailTool 按指定次数失败、之后成功的测试桩：
// 用于验证"近期持续失败工具"的隐藏与自愈恢复。
type mockFailTool struct {
	BaseTool
	failRemaining int
}

func (t *mockFailTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if t.failRemaining > 0 {
		t.failRemaining--
		return nil, fmt.Errorf("simulated failure")
	}
	return "ok", nil
}

func namesOfAvailable(r *Registry) []string {
	out := []string{}
	for _, tl := range r.ListAvailable() {
		out = append(out, tl.Name())
	}
	return out
}

func containsTool(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// TestFailureHideFewSamplesNotHidden 小样本保护：偶发失败（不足最小样本数）
// 绝不能把工具藏掉 —— 一次网络抖动就隐藏会把模型逼上"没有工具可用"的路。
func TestFailureHideFewSamplesNotHidden(t *testing.T) {
	r := NewRegistry()
	ft := &mockFailTool{BaseTool: *NewBaseTool("flaky", "sometimes fails", nil), failRemaining: 2}
	r.Register(ft)

	for i := 0; i < 2; i++ {
		if _, err := r.Execute(context.Background(), "flaky", nil); err == nil {
			t.Fatalf("call %d should fail", i)
		}
	}

	if got := namesOfAvailable(r); !containsTool(got, "flaky") {
		t.Errorf("2 failures must not hide the tool (insufficient samples), got %v", got)
	}
}

// TestFailureHideAndRecover 锁死隐藏 + 自愈恢复的完整闭环：
//  1. 连续失败达到阈值 → 从 ListAvailable/ListWithSchemas 消失；
//  2. 但 Get/Execute 仍可用（后台任务不受影响）、管理端清单仍可见；
//  3. 后续成功把失败样本挤出窗口 → 工具自动回到模型清单。
func TestFailureHideAndRecover(t *testing.T) {
	r := NewRegistry()
	ft := &mockFailTool{BaseTool: *NewBaseTool("broken", "keeps failing", nil), failRemaining: failureHideMinSamples}
	r.Register(ft)

	for i := 0; i < failureHideMinSamples; i++ {
		if _, err := r.Execute(context.Background(), "broken", nil); err == nil {
			t.Fatalf("call %d should fail", i)
		}
	}

	// 隐藏生效
	if got := namesOfAvailable(r); containsTool(got, "broken") {
		t.Fatalf("tool should be hidden after %d consecutive failures, got %v", failureHideMinSamples, got)
	}
	for _, s := range r.ListWithSchemas() {
		if fn, ok := s["function"].(map[string]interface{}); ok && fn["name"] == "broken" {
			t.Error("broken must not appear in ListWithSchemas while failing")
		}
	}

	// 隐藏只是"模型看不见"，不是"不可用"
	if _, err := r.Get("broken"); err != nil {
		t.Errorf("Get must still work for a hidden tool: %v", err)
	}
	ft.failRemaining = 0
	if _, err := r.Execute(context.Background(), "broken", nil); err != nil {
		t.Errorf("Execute must still work for a hidden tool: %v", err)
	}
	if len(r.ListAllWithSchemas()) != 1 {
		t.Error("ListAllWithSchemas (admin view) must still include the hidden tool")
	}

	// 自愈：成功样本把失败样本挤出窗口（窗口 12 = 6 失败 + 6 成功，
	// 成功率 0.5 > 阈值），工具应重新可见
	for i := 0; i < failureHideWindow/2; i++ {
		if _, err := r.Execute(context.Background(), "broken", nil); err != nil {
			t.Fatalf("recovery call %d should succeed: %v", i, err)
		}
	}
	if got := namesOfAvailable(r); !containsTool(got, "broken") {
		t.Errorf("tool should be re-exposed after recent successes, got %v", got)
	}
}

// TestFailureHideStatsFlag ToolStats.Hidden 标志要随判定同步翻转，
// /tools/statistics 前端靠它解释"这个工具为什么不在清单里"。
func TestFailureHideStatsFlag(t *testing.T) {
	r := NewRegistry()
	ft := &mockFailTool{BaseTool: *NewBaseTool("sad", "always fails", nil), failRemaining: failureHideMinSamples}
	r.Register(ft)

	for i := 0; i < failureHideMinSamples; i++ {
		_, _ = r.Execute(context.Background(), "sad", nil)
	}
	if stats := r.GetStats()["sad"]; stats == nil || !stats.Hidden {
		t.Fatalf("stats.Hidden should be true after consecutive failures, got %+v", r.GetStats()["sad"])
	}

	ft.failRemaining = 0
	for i := 0; i < failureHideWindow/2; i++ {
		_, _ = r.Execute(context.Background(), "sad", nil)
	}
	if stats := r.GetStats()["sad"]; stats == nil || stats.Hidden {
		t.Fatalf("stats.Hidden should clear after recovery, got %+v", r.GetStats()["sad"])
	}
}

// TestFailureHideRecencyExpiry 隐藏的自愈第二种机制：时间过期。
// 把 recency 调到极短后，失败样本全部滑出窗口，工具恢复可见 ——
// 锁死"不存在一旦隐藏永远藏下去"的失败模式。
func TestFailureHideRecencyExpiry(t *testing.T) {
	oldRecency := failureHideRecency
	failureHideRecency = 1 * time.Millisecond
	t.Cleanup(func() { failureHideRecency = oldRecency })

	r := NewRegistry()
	ft := &mockFailTool{BaseTool: *NewBaseTool("stale", "failed in the past", nil), failRemaining: failureHideMinSamples}
	r.Register(ft)

	for i := 0; i < failureHideMinSamples; i++ {
		_, _ = r.Execute(context.Background(), "stale", nil)
	}
	if got := namesOfAvailable(r); containsTool(got, "stale") {
		t.Fatal("tool should be hidden immediately after failures")
	}

	time.Sleep(5 * time.Millisecond)
	if got := namesOfAvailable(r); !containsTool(got, "stale") {
		t.Errorf("tool should reappear once all failure samples age out, got %v", got)
	}
}

// TestExecutorTimeoutHint 锁死执行器层的超时兜底提示：外层 ctx 超时返回的
// 不能是裸的 context deadline exceeded，必须带上"别原样重试"的方向性提示；
// 且 errors.Is(err, context.DeadlineExceeded) 仍成立（调用方按错误类型分流）。
func TestExecutorTimeoutHint(t *testing.T) {
	e := &DefaultToolExecutor{}
	slow := &mockSlowTool{BaseTool: *NewBaseTool("slow", "sleeps forever", nil)}

	res := e.ExecuteWithProtection(context.Background(), slow, nil, 30*time.Millisecond)
	err := res.Error
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, DeadlineExceeded) must still hold, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Do not retry the same call unchanged") {
		t.Errorf("timeout error should carry the retry-guidance hint, got: %v", err)
	}
}

// mockSlowTool 睡到 ctx 被取消的测试桩。
type mockSlowTool struct{ BaseTool }

func (t *mockSlowTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
