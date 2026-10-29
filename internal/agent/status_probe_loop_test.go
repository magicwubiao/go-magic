package agent

import (
	"fmt"
	"testing"
)

// 本文件锁死"合法轮询不被判成死循环"。
//
// 背景：sameToolLimit(3) 的判据是"同一个(工具+参数)连续重复"。这对
// execute_command / terminal 里的**只读状态探测**是误伤——等部署、等构建、
// 等 CI 的常规做法就是每隔几十秒重跑同一条 `docker ps` / `git status`，
// 在第 3 次就被收口，用户看到的是"任务在等待期间被无端打断"。
// 处理方式：这类调用仍参与判环（否则空转无人管），但用更宽松的
// statusProbeLoopLimit。

func TestStatusProbeCommandClassification(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		// 典型轮询：只读
		{"docker ps", true},
		{"docker logs -f api --tail 50", true},
		{"docker compose ps", true},
		{"kubectl get pods -n prod", true},
		{"git status --porcelain", true},
		{"git log --oneline -5", true},
		{"systemctl is-active magic", true},
		{"cd /app && git status", true},
		{"sleep 20 && curl-free check", false}, // 未知命令 → 不算探测
		{"kubectl get pods | grep Running", true},
		{"tail -n 40 /var/log/magic.log", true},
		{"ps aux | grep magic", true},

		// 写操作 / 状态变更：必须照常按 sameToolLimit 判
		{"docker rm -f api", false},
		{"docker compose up -d", false},
		{"kubectl apply -f deploy.yaml", false},
		{"git commit -m x", false},
		{"git checkout main", false},
		{"systemctl restart magic", false},
		{"sed -i 's/a/b/' app.go", false},
		{"rm -rf build", false},
		{"go build ./...", false},

		// 重定向 / 写文件痕迹：一段可疑就整条不算探测
		{"git status > /tmp/status.txt", false},
		{"cat app.go | tee /tmp/a.go", false},
		{"curl -s http://localhost:8080/healthz", false},

		// 混合命令：有一段是写操作就不算
		{"docker ps && git commit -m wip", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isStatusProbeCommand(c.cmd); got != c.want {
			t.Errorf("isStatusProbeCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// TestRepeatedStatusProbeSurvivesSameToolLimit：同一条只读探测命令重复
// sameToolLimit 次不得触发（这正是等部署的形态），到 statusProbeLoopLimit 才收口。
func TestRepeatedStatusProbeSurvivesSameToolLimit(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	args := `{"command":"kubectl get pods -n prod"}`

	if ag.sameToolLimit >= statusProbeLoopLimit {
		t.Fatalf("precondition failed: statusProbeLoopLimit=%d must exceed sameToolLimit=%d",
			statusProbeLoopLimit, ag.sameToolLimit)
	}

	for i := 1; i < statusProbeLoopLimit; i++ {
		ag.recordToolCallSig("execute_command", args)
		if detected, reason := ag.detectToolLoop(); detected {
			t.Fatalf("poll #%d of a read-only status command must not be flagged (limit %d), got: %q",
				i, statusProbeLoopLimit, reason)
		}
	}
	ag.recordToolCallSig("execute_command", args)
	if detected, _ := ag.detectToolLoop(); !detected {
		t.Fatalf("poll #%d must still be stopped: an endlessly re-asked status command is a spin", statusProbeLoopLimit)
	}
}

// TestRepeatedMutatingCommandStillStops：非探测命令（构建、删除、apply）
// 必须继续按 sameToolLimit 收口——放宽只针对只读探测，不能给死循环开口子。
func TestRepeatedMutatingCommandStillStops(t *testing.T) {
	for _, cmd := range []string{"go build ./...", "rm -rf build", "kubectl apply -f deploy.yaml"} {
		ag := newLoopTestAgent(t, &scriptedLoopProvider{})
		args := fmt.Sprintf(`{"command":%q}`, cmd)
		for i := 0; i < ag.sameToolLimit; i++ {
			ag.recordToolCallSig("execute_command", args)
		}
		if detected, _ := ag.detectToolLoop(); !detected {
			t.Errorf("repeating %q %d times must still be judged a loop", cmd, ag.sameToolLimit)
		}
	}
}

// TestStatusProbeOnlyForShellTools：只读探测的宽松上限只适用于 shell 工具，
// 其他工具（哪怕参数里恰好带 command 字段）不得走这条豁免。
func TestStatusProbeOnlyForShellTools(t *testing.T) {
	if isStatusProbeCall("write_file", `{"command":"docker ps"}`) {
		t.Error("write_file must never be treated as a read-only status probe")
	}
	if isStatusProbeCall("process", `{"command":"docker ps"}`) {
		t.Error("process is judged by its own polling exemptions, not this one")
	}
	if !isStatusProbeCall("terminal", `{"command":"docker ps"}`) {
		t.Error("terminal with a read-only status command should be recognised")
	}
	if !isStatusProbeCall("execute_command", `{"command":"git status"}`) {
		t.Error("execute_command with a read-only status command should be recognised")
	}
}
