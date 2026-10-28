package agent

import (
	"fmt"
	"testing"
)

// TestProcessPollingIsNotFlaggedAsLoop 锁死"构建等待被误判成死循环"的线上故障。
//
// 故障现象：模型用 process 工具 wait 一个几分钟的构建，每次 timeout 都是 25，
// 参数完全相同。旧实现把这些重复签名计入循环检测，约 15 次轮询后触发
// concludeAfterToolLoop，模型被要求"不要再调工具"，提前写了总结。
func TestProcessPollingIsNotFlaggedAsLoop(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})

	waitArgs := `{"action":"wait","session_id":"proc_1","timeout":25}`
	logArgs := `{"action":"log","session_id":"proc_1"}`

	// 远超 consecutiveLimit(15) 的轮询次数，交替 wait/log。
	for i := 0; i < 40; i++ {
		ag.recordToolCallSig("process", waitArgs)
		ag.recordToolCallSig("process", logArgs)
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("process polling #%d must not trigger loop detection, got: %q", i+1, reason)
		}
	}

	if got := ag.toolCallHistoryLength(); got != 0 {
		t.Fatalf("polling calls should be exempt from history, got %d entries", got)
	}
}

// TestProcessRunStillCountsForLoop 确认豁免只针对轮询动作：
// 反复 run 同一个后台命令仍是真死循环，必须照常触发。
func TestProcessRunStillCountsForLoop(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})

	runArgs := `{"action":"run","command":"make build"}`
	for i := 0; i < ag.sameToolLimit; i++ {
		ag.recordToolCallSig("process", runArgs)
	}
	if detected, _ := ag.detectToolLoop(); !detected {
		t.Fatalf("repeated process run with identical args must still trigger loop detection (limit %d)", ag.sameToolLimit)
	}
}

// TestIsLoopExemptCall 覆盖豁免判定的边界：非 process 工具、非法 JSON、
// 缺失 action、以及 kill/list 这类非轮询动作都不能被豁免。
func TestIsLoopExemptCall(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
		want bool
	}{
		{"wait is exempt", "process", `{"action":"wait","session_id":"x"}`, true},
		{"poll is exempt", "process", `{"action":"poll","session_id":"x"}`, true},
		{"log is exempt", "process", `{"action":"log","session_id":"x"}`, true},
		{"run is not exempt", "process", `{"action":"run","command":"ls"}`, false},
		{"kill is not exempt", "process", `{"action":"kill","session_id":"x"}`, false},
		{"other tool with wait action", "browser_automation", `{"action":"wait"}`, false},
		{"malformed json", "process", `{"action":`, false},
		{"no action", "process", `{"session_id":"x"}`, false},
		{"empty args", "process", ``, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s", c.name), func(t *testing.T) {
			if got := isLoopExemptCall(c.tool, c.args); got != c.want {
				t.Fatalf("isLoopExemptCall(%q, %q) = %v, want %v", c.tool, c.args, got, c.want)
			}
		})
	}
}
