package cron

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	robfigcron "github.com/robfig/cron/v3"
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

// newTestManager 构造一个带临时持久化路径的空 Manager，仅供 Add/Update 的
// 字段语义测试使用（不启动调度器）。
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return &Manager{
		jobs:     map[string]*Job{},
		entryMap: map[string]robfigcron.EntryID{},
		jobsFile: filepath.Join(t.TempDir(), "cron_jobs.json"),
		cron:     robfigcron.New(), // Add(enabled) 会挂调度条目，不能为 nil
	}
}

// TestAddBackfillsCreatedAt 回归：此前 Job 没有 CreatedAt 字段，工具快照里
// 恒为零值，序列化成 "0001-01-01T00:00:00Z"，看起来像数据损坏。
func TestAddBackfillsCreatedAt(t *testing.T) {
	m := newTestManager(t)
	job := &Job{ID: "j1", Name: "news", Schedule: "30 9 * * *", Prompt: "p", Enabled: true}
	if err := m.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if job.CreatedAt.IsZero() {
		t.Fatal("Add must backfill CreatedAt; zero value serializes as 0001-01-01T00:00:00Z")
	}
}

// TestUpdateDisabledJobClearsNextRun 回归：pause 接口把 Enabled 置 false 后
// Update 只摘调度器、不清 next_run，出现 "enabled:false 却有 next_run" 的
// 矛盾数据。
func TestUpdateDisabledJobClearsNextRun(t *testing.T) {
	m := newTestManager(t)
	job := &Job{ID: "j2", Name: "news", Schedule: "30 9 * * *", Prompt: "p", Enabled: true}
	if err := m.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// 模拟 pause 场景：任务带着旧版残留的 next_run 进入 Update（Enabled 已置 false）
	next := time.Now().Add(time.Hour)
	job.Enabled = false
	job.NextRun = &next
	if err := m.Update(job); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if job.NextRun != nil {
		t.Fatalf("disabled job must not carry next_run; got %v", job.NextRun)
	}
}

// TestToolJobSnapshotOmitsZeroCreatedAt 回归：旧任务（修复前创建、JSON 里无
// created_at）的快照必须整体省略 created_at，而不是输出零值时间。
func TestToolJobSnapshotOmitsZeroCreatedAt(t *testing.T) {
	legacy := &Job{ID: "j3", Name: "news", Schedule: "30 9 * * *", Prompt: "p", Enabled: true}
	data, err := json.Marshal(toolJobSnapshot(legacy))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if regexp.MustCompile(`"created_at"`).Match(data) {
		t.Fatalf("legacy job (zero CreatedAt) must omit created_at, got %s", data)
	}

	created := time.Date(2026, 11, 24, 10, 0, 0, 0, time.Local)
	fresh := &Job{ID: "j4", Name: "n", Schedule: "30 9 * * *", Prompt: "p", CreatedAt: created}
	data2, _ := json.Marshal(toolJobSnapshot(fresh))
	if !regexp.MustCompile(`"created_at":"2026-11-24T10:00:00`).Match(data2) {
		t.Fatalf("fresh job must carry real created_at, got %s", data2)
	}
}
