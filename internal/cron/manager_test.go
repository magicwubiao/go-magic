package cron

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"
)

// TestCronRunDirName guards the per-run directory layout
// (cron/<jobID>/run_<yyyyMMdd-HHmmss>): each job run gets its own working
// directory instead of all jobs sharing <workingDir>/cron, where same-named
// artifacts overwrote each other and overlapping runs (manual + scheduled)
// raced on the same directory.
func TestCronRunDirName(t *testing.T) {
	now := time.Date(2026, 9, 23, 18, 40, 48, 0, time.Local)
	got := cronRunDirName(now)
	want := "run_20260923-184048"
	if got != want {
		t.Fatalf("cronRunDirName = %q, want %q", got, want)
	}
	if !regexp.MustCompile(`^run_\d{8}-\d{6}$`).MatchString(got) {
		t.Fatalf("run dir name %q must be human-readable run_<yyyyMMdd-HHmmss>", got)
	}
}

// TestExecutionLogWorkDirMarshals ensures the work_dir field round-trips so
// logs can answer "这次运行的产物在哪"; empty WorkDir must be omitted to keep
// legacy log files stable.
func TestExecutionLogWorkDirMarshals(t *testing.T) {
	in := ExecutionLog{
		ID:      "log_1",
		JobID:   "job-1",
		Status:  "success",
		WorkDir: `D:\workspace\cron\job-1\run_20260923-184048`,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ExecutionLog
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.WorkDir != in.WorkDir {
		t.Fatalf("work_dir round-trip mismatch: %q != %q", out.WorkDir, in.WorkDir)
	}

	empty := ExecutionLog{ID: "log_2", Status: "success"}
	data2, _ := json.Marshal(empty)
	if regexp.MustCompile(`"work_dir"`).Match(data2) {
		t.Fatalf("empty WorkDir must be omitted, got %s", data2)
	}
}
