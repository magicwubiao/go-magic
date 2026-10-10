package cortex

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecordTrajectoryCapsSteps 锁死单条轨迹的 step 数上界。
//
// 背景：几百轮长任务会产出几百个 step，每个 step 都带一段工具输出；没有上界时
// TrajectoryStore 会在内存与磁盘上双膨胀（启动时还被 loadTrajectories 全量读回）。
// 超出上限时保留**最近**的步骤。
func TestRecordTrajectoryCapsSteps(t *testing.T) {
	dir := t.TempDir()
	ts, err := NewTrajectoryStore(dir)
	if err != nil {
		t.Fatalf("NewTrajectoryStore: %v", err)
	}

	steps := make([]TrajectoryStep, 0, maxTrajectorySteps+50)
	for i := 0; i < maxTrajectorySteps+50; i++ {
		steps = append(steps, TrajectoryStep{
			ToolName:   "read_file",
			ToolOutput: "out",
			Success:    true,
		})
	}
	// 给最后一个 step 一个可识别的名字，用来确认保留的是链尾。
	steps[len(steps)-1].ToolName = "tail_marker"

	if err := ts.RecordTrajectory(&Trajectory{Task: "long task", Steps: steps}); err != nil {
		t.Fatalf("RecordTrajectory: %v", err)
	}

	if got := len(ts.trajectories); got != 1 {
		t.Fatalf("want 1 stored trajectory, got %d", got)
	}
	stored := ts.trajectories[0]
	if len(stored.Steps) != maxTrajectorySteps {
		t.Fatalf("steps must be capped to %d, got %d", maxTrajectorySteps, len(stored.Steps))
	}
	// 保留链尾：最后一个 step 是 tail_marker。
	if got := stored.Steps[len(stored.Steps)-1].ToolName; got != "tail_marker" {
		t.Fatalf("cap must keep the newest steps (tail), got last tool %q", got)
	}

	// 落盘形态也必须有界：重新加载后 step 数不反弹。
	ts2, err := NewTrajectoryStore(dir)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if len(ts2.trajectories) != 1 {
		t.Fatalf("reload: want 1 trajectory, got %d", len(ts2.trajectories))
	}
	if got := len(ts2.trajectories[0].Steps); got != maxTrajectorySteps {
		t.Fatalf("reloaded trajectory must stay capped at %d, got %d", maxTrajectorySteps, got)
	}

	// 文件确实存在于 trajectoryDir 下（确认写盘路径没变）。
	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil {
		t.Fatalf("read trajectories dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 trajectory file on disk, got %d", len(entries))
	}
}
