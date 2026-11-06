package subagent

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// 回归：executeTask 曾硬用 config.Timeout（默认 120s），无视 delegate_task
// 传入的 timeout_seconds——子任务一跑过 2 分钟就被误判为"系统取消"。
// taskTimeout 必须优先取 per-task 值，缺省才回落配置。
func TestTaskTimeout(t *testing.T) {
	fallback := 120 * time.Second

	if got := taskTimeout(nil, fallback); got != fallback {
		t.Errorf("taskTimeout(nil) = %v, want fallback %v", got, fallback)
	}
	if got := taskTimeout(&Task{}, fallback); got != fallback {
		t.Errorf("taskTimeout(no context) = %v, want fallback %v", got, fallback)
	}

	// JSON 反序列化后数值是 float64
	task := &Task{Context: map[string]interface{}{"timeout_seconds": float64(300)}}
	if got := taskTimeout(task, fallback); got != 300*time.Second {
		t.Errorf("taskTimeout(float64 300) = %v, want 300s", got)
	}

	// Go 内部路径是 int
	task = &Task{Context: map[string]interface{}{"timeout_seconds": 600}}
	if got := taskTimeout(task, fallback); got != 600*time.Second {
		t.Errorf("taskTimeout(int 600) = %v, want 600s", got)
	}

	// 非法值回落
	task = &Task{Context: map[string]interface{}{"timeout_seconds": float64(-5)}}
	if got := taskTimeout(task, fallback); got != fallback {
		t.Errorf("taskTimeout(negative) = %v, want fallback %v", got, fallback)
	}
	task = &Task{Context: map[string]interface{}{"timeout_seconds": "300"}}
	if got := taskTimeout(task, fallback); got != fallback {
		t.Errorf("taskTimeout(string) = %v, want fallback %v", got, fallback)
	}
}

// 回归：WaitForResult 曾从共享 resultChan 消费结果，读到别的任务的结果
// 会被永久丢弃。现在只轮询 results map：等待 A 时先落地的 B 结果必须原样保留。
func TestWaitForResultDoesNotEatOtherResults(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)
	defer func() { m.cancel() }() // 只停 ctx，避免 Stop() 关闭已无人写的 channel

	other := &Result{TaskID: "task-b", Success: true, Output: "b"}
	mine := &Result{TaskID: "task-a", Success: true, Output: "a"}

	// 模拟 B 先完成、A 稍后完成
	m.storeResult(other)
	go func() {
		time.Sleep(150 * time.Millisecond)
		m.storeResult(mine)
	}()

	got, err := m.WaitForResult("task-a", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResult(task-a) error: %v", err)
	}
	if got.TaskID != "task-a" {
		t.Fatalf("WaitForResult returned task %q, want task-a", got.TaskID)
	}

	// B 的结果不能被吞掉
	if _, ok := m.results["task-b"]; !ok {
		t.Fatal("result for task-b was consumed/lost while waiting for task-a")
	}
}

// 回归：SubAgent.Run 曾在 err != nil 时 return "", err，把触达回合上限时
// runner 随错误返回的已产出内容丢掉，主 agent 只看到一句错误。
type partialRunner struct{ out, errText string }

func (r *partialRunner) RunConversation(ctx context.Context, input string) (string, error) {
	return r.out, fmt.Errorf("%s", r.errText)
}

func TestSubAgentRunKeepsPartialOutputOnError(t *testing.T) {
	s := &SubAgent{runner: &partialRunner{out: "partial progress", errText: "exceeded maximum turns"}}

	output, err := s.Run(context.Background(), "do it")
	if err == nil {
		t.Fatal("expected error")
	}
	if output != "partial progress" {
		t.Fatalf("partial output lost on error: got %q", output)
	}
}

// 回归：cancel() 必须真正取消执行 ctx（cancel_task 对运行中任务曾无效，
// 只设标志位，runner 检查的是 ctx），且 cancelFn 为 nil / 重复调用要安全。
func TestSubAgentCancelAbortsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &SubAgent{cancelFn: cancel}
	s.cancel()
	if ctx.Err() == nil {
		t.Fatal("cancel() did not abort execution context")
	}
	s.cancel() // 幂等，不 panic

	// cancelFn 为 nil 不 panic
	s2 := &SubAgent{}
	s2.cancel()
}
