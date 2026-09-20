package tool

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regression for "process 工具需要 bash": the background-process tool (and the
// local terminal backend) hard-coded `bash -c`, so on a Windows host without Git
// Bash every background job failed with
// "failed to start process: exec: bash: executable file not found in %PATH%"
// even though execute_command worked on the same machine.

func TestShellCommandUsesHostShell(t *testing.T) {
	if hostShell().Binary == "" {
		t.Fatal("no shell resolved on this host")
	}
	if _, err := exec.LookPath(hostShell().Binary); err != nil {
		t.Errorf("resolved shell %q is not executable: %v", hostShell().Binary, err)
	}

	cmd, err := shellCommand(context.Background(), "echo shell-ok")
	if err != nil {
		t.Fatalf("shellCommand: %v", err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shell %s could not run a trivial command: %v (output: %s)", shellName(), err, out)
	}
	if !strings.Contains(string(out), "shell-ok") {
		t.Errorf("output %q does not contain the echoed text", out)
	}
}

// TestProcessToolRunsWithoutBash also pins the workdir contract: a background job
// started without an explicit workdir must run in the *session* workdir, not in
// whatever directory the server process was launched from.
func TestProcessToolRunsWithoutBash(t *testing.T) {
	work := t.TempDir()
	chdirOutsideWorkDir(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = WithWorkDir(ctx, work)

	pt := NewProcessTool()
	res, err := pt.Execute(ctx, map[string]interface{}{"action": "run", "command": "echo process-ok"})
	if err != nil {
		t.Fatalf("process run must not require bash: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	sid, _ := m["session_id"].(string)
	if sid == "" {
		t.Fatalf("no session_id in %#v", m)
	}
	t.Cleanup(func() {
		_, _ = pt.Execute(context.Background(), map[string]interface{}{"action": "kill", "session_id": sid})
	})

	if got, _ := m["workdir"].(string); got != filepath.Clean(work) {
		t.Errorf("workdir = %q, want the session workdir %q", got, work)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		poll, err := pt.Execute(ctx, map[string]interface{}{"action": "poll", "session_id": sid})
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if status, _ := poll.(map[string]interface{})["status"].(string); status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background process did not finish in time")
		}
		time.Sleep(50 * time.Millisecond)
	}

	logRes, err := pt.Execute(ctx, map[string]interface{}{"action": "log", "session_id": sid})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	out, _ := logRes.(map[string]interface{})["output"].(string)
	if !strings.Contains(out, "process-ok") {
		t.Fatalf("captured output %q does not contain the command output", out)
	}
}
