package agent

import (
	"errors"
	"strings"
	"testing"
)

// TestDeadlineStopReturnsIncompleteError 锁死："时限收尾"不是"完成"。
// 调用方必须能用 errors.Is(err, ErrTurnIncomplete) 把它与正常结束区分开，
// 否则用户会把没做完的任务当成已完成。
func TestDeadlineStopReturnsIncompleteError(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	a := newDeadlineTestAgent()
	a.session = "incomplete_sess"

	msg, err := a.deadlineStop("some task")
	if err == nil {
		t.Fatal("deadline stop must return an error so callers can tell it is not complete")
	}
	if !errors.Is(err, ErrTurnIncomplete) {
		t.Fatalf("errors.Is(err, ErrTurnIncomplete) = false; err = %v", err)
	}

	var ie *IncompleteError
	if !errors.As(err, &ie) {
		t.Fatalf("expected *IncompleteError, got %T", err)
	}
	if ie.Reason == "" {
		t.Error("IncompleteError.Reason must explain why the turn stopped")
	}

	// 用户可见文本保持不变，仍包含收尾说明。
	if !strings.Contains(msg, "[Deadline approaching]") {
		t.Fatalf("user-facing message changed unexpectedly: %s", msg)
	}
}

// TestPlainGracefulFinishIsNotIncomplete 保证 gracefulDeadlineFinish（只要文本的
// 旧接口）仍然只返回字符串，不会被误当作"未完成"错误路径。
func TestPlainGracefulFinishIsNotIncomplete(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
	a := newDeadlineTestAgent()
	a.session = "plain_sess"
	if msg := a.gracefulDeadlineFinish("t"); !strings.Contains(msg, "[Deadline approaching]") {
		t.Fatalf("unexpected message: %s", msg)
	}
}
