package tool

import (
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/subagent"
)

// TestStampTimeIsRealUTC 锁死子代理任务时间戳的时区语义。
// 历史 bug：用 task.CreatedAt.Format("2006-01-02T15:04:05Z") 输出，
// 布局里的 Z 是字面量，GMT+8 本地时间被原样贴上 Z，序列化结果比真实 UTC 晚 8 小时。
func TestStampTimeIsRealUTC(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	local := time.Date(2026, 11, 7, 14, 1, 15, 0, loc)

	got := stampTime(local)
	if want := "2026-11-07T06:01:15Z"; got != want {
		t.Fatalf("stampTime(%s) = %q, want %q (本地时间必须以 UTC 输出，不能只贴 Z)", local, got, want)
	}

	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("输出 %q 不是合法 RFC3339: %v", got, err)
	}
	if !parsed.Equal(local) {
		t.Fatalf("往返解析丢失时刻：解析得 %s，原值 %s", parsed, local)
	}

	// 已是 UTC 的输入保持稳定
	if u := stampTime(local.UTC()); u != got {
		t.Fatalf("UTC 输入输出不一致：%q vs %q", u, got)
	}
}

// TestTaskResponseIncludesUnixTimestamps 保证每个 ISO 时间字段都有对应的 *_unix 秒级字段，
// 供调用方排序/对账时绕开任何时区标注问题。
func TestTaskResponseIncludesUnixTimestamps(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	created := time.Date(2026, 11, 7, 14, 1, 15, 0, loc)
	started := created.Add(2 * time.Second)
	completed := started.Add(30 * time.Second)

	task := &subagent.Task{
		ID:          "task-1",
		Status:      subagent.TaskStatusCompleted,
		CreatedAt:   created,
		StartedAt:   &started,
		CompletedAt: &completed,
	}

	resp := (&PollTaskTool{}).buildTaskResponse(task, nil)

	wantUnix := map[string]int64{
		"created_at_unix":   created.Unix(),
		"started_at_unix":   started.Unix(),
		"completed_at_unix": completed.Unix(),
	}
	for key, want := range wantUnix {
		got, ok := resp[key].(int64)
		if !ok {
			t.Fatalf("%s 缺失或类型不是 int64: %#v", key, resp[key])
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", key, got, want)
		}
	}

	// ISO 串与 unix 字段必须描述同一时刻（防止两套时间各写各的）
	for isoKey, unixKey := range map[string]string{
		"created_at":   "created_at_unix",
		"started_at":   "started_at_unix",
		"completed_at": "completed_at_unix",
	} {
		iso, ok := resp[isoKey].(string)
		if !ok {
			t.Fatalf("%s 缺失或类型不是 string: %#v", isoKey, resp[isoKey])
		}
		parsed, err := time.Parse(time.RFC3339, iso)
		if err != nil {
			t.Fatalf("%s = %q 不是合法 RFC3339: %v", isoKey, iso, err)
		}
		if parsed.Unix() != resp[unixKey].(int64) {
			t.Fatalf("%s(%q) 与 %s(%d) 不是同一时刻", isoKey, iso, unixKey, resp[unixKey].(int64))
		}
	}

	// 无 started/completed 的 queued 任务不应伪造这两个字段
	pending := &subagent.Task{ID: "task-2", Status: subagent.TaskStatusPending, CreatedAt: created}
	presp := (&PollTaskTool{}).buildTaskResponse(pending, nil)
	for _, key := range []string{"started_at", "started_at_unix", "completed_at", "completed_at_unix"} {
		if _, exists := presp[key]; exists {
			t.Fatalf("pending 任务不应输出 %s", key)
		}
	}
}

// TestFailedTaskResponseCarriesWorkspaceHint 锁死"failed ≠ 没干活"的提示。
// 实测场景：子代理改完文件、跑过测试，只在最后写摘要时上游断流，任务状态却是
// failed。若响应里只有 error，调用方很可能直接重派，白烧 token 并覆盖正确实现。
func TestFailedTaskResponseCarriesWorkspaceHint(t *testing.T) {
	created := time.Now()
	task := &subagent.Task{ID: "task-fail", Status: subagent.TaskStatusFailed, CreatedAt: created}
	res := &subagent.Result{
		TaskID:  task.ID,
		Success: false,
		Error:   "stream operation aborted: context deadline exceeded",
		Output:  "[aborted run salvage] turns=3 tool_calls=9",
	}

	resp := (&PollTaskTool{}).buildTaskResponse(task, res)

	hint, ok := resp["hint"].(string)
	if !ok {
		t.Fatalf("失败任务必须带 hint，实际响应：%#v", resp)
	}
	if !strings.Contains(hint, "git status") || !strings.Contains(hint, "partial_output") {
		t.Fatalf("hint 应同时指向 partial_output 与工作区检查，实际：%q", hint)
	}
	if resp["partial_output"] != res.Output {
		t.Fatalf("partial_output 未透传：%#v", resp["partial_output"])
	}

	// 成功任务不该出现这条提示（否则等于每天喊狼来了）
	okTask := &subagent.Task{ID: "task-ok", Status: subagent.TaskStatusCompleted, CreatedAt: created}
	okResp := (&PollTaskTool{}).buildTaskResponse(okTask, &subagent.Result{TaskID: okTask.ID, Success: true, Output: "done"})
	if _, exists := okResp["hint"]; exists {
		t.Fatalf("completed 任务不应输出 hint：%#v", okResp["hint"])
	}
}
