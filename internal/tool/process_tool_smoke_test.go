package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProcessToolRunSmoke(t *testing.T) {
	pt := NewProcessTool()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// run
	res, err := pt.Execute(ctx, map[string]interface{}{
		"action":  "run",
		"command": "echo hello-bg; sleep 0.2; echo done-bg",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	m := res.(map[string]interface{})
	sid, _ := m["session_id"].(string)
	if sid == "" {
		t.Fatalf("no session_id: %#v", m)
	}

	// wait
	res2, err := pt.Execute(ctx, map[string]interface{}{"action": "wait", "session_id": sid, "timeout": 10.0})
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	w := res2.(map[string]interface{})
	if w["status"] != "exited" {
		t.Fatalf("status = %v, want exited", w["status"])
	}

	// log
	res3, err := pt.Execute(ctx, map[string]interface{}{"action": "log", "session_id": sid})
	if err != nil {
		t.Fatalf("log failed: %v", err)
	}
	l := res3.(map[string]interface{})
	out, _ := l["output"].(string)
	if !contains(out, "hello-bg") || !contains(out, "done-bg") {
		t.Fatalf("log output missing expected lines: %q", out)
	}

	// list
	if _, err := pt.Execute(ctx, map[string]interface{}{"action": "list"}); err != nil {
		t.Fatalf("list failed: %v", err)
	}

	// unknown action message
	_, err = pt.Execute(ctx, map[string]interface{}{"action": "exec"})
	if err == nil || !strings.Contains(err.Error(), "valid actions: run, list, poll, wait, kill, write, log") {
		t.Fatalf("unexpected unknown-action error: %v", err)
	}

	// start alias
	if _, err := pt.Execute(ctx, map[string]interface{}{"action": "start", "command": "echo alias-ok"}); err != nil {
		t.Fatalf("start alias failed: %v", err)
	}
}
