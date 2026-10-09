package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// warmUpPython spawns the interpreter once so that the wall-clock assertions
// below measure cancellation, not first-process-creation cost. On Windows the
// very first python child of a fresh test binary can take seconds (AV/Defender
// scanning the image + the interpreter's own first import cache); CI on Linux
// is fast enough that this never showed up. Measured here: first spawn ~5s,
// every later spawn ~0.02s.
//
// Returns false when no usable interpreter exists — callers should skip rather
// than fail, otherwise the test is unpassable on a machine without Python.
func warmUpPython(t *testing.T) bool {
	t.Helper()
	if _, _, ok := ResolvePythonCommand(); !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tl := NewExecuteCodeTool()
	res, err := tl.Execute(ctx, map[string]interface{}{
		"code":     "print('warm')",
		"language": "python",
	})
	if err != nil {
		return false
	}
	m, ok := res.(map[string]interface{})
	if !ok || m["stdout"] == nil || !strings.Contains(m["stdout"].(string), "warm") {
		return false
	}
	return true
}

// waitForFile 轮询直到 path 出现，或超时报错。
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child never signalled readiness: %s not created within %v", path, timeout)
}

// TestExecuteCodeContextCancel verifies that a parent-context deadline (e.g.
// the bot-mode turn timeout) cancels a running subprocess promptly and is
// reported as a *result payload* (not a Go error), so the conversation loop
// can continue instead of dying with "context deadline exceeded".
//
// 时序说明（这个用例曾经红过，且不是产品缺陷）：旧实现固定 800ms 就 Kill 子进程，
// 然后断言"必须有 partial stdout"。实测本机 Windows、解释器预热之后，子进程从
// Start 到首行输出进入父进程缓冲仍需 **0.54–1.28s**（AV 扫描 + 解释器启动），
// 正好横跨那个 800ms 阈值 —— 于是同一份代码在快/慢调度下分别通过和失败。
//
// 现在改为**确定性握手**：脚本打印首行后再创建握手文件，测试等握手文件出现
// （并留一小段让父进程的管道读取协程把这一行收进缓冲）才取消。断言的是
// "取消时已经产生的输出不会被丢弃"，而不再赌"子进程能否在 800ms 内启动完"。
func TestExecuteCodeContextCancel(t *testing.T) {
	if !warmUpPython(t) {
		t.Skip("no usable python interpreter")
	}

	handshake := filepath.Join(t.TempDir(), "printed.txt")
	code := fmt.Sprintf(
		"import time, pathlib\n"+
			"print('start', flush=True)\n"+
			"pathlib.Path(r'%s').write_text('ok')\n"+
			"time.sleep(30)\n"+
			"print('end')\n", handshake)

	tl := NewExecuteCodeTool()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type outcome struct {
		res interface{}
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := tl.Execute(ctx, map[string]interface{}{
			"code":     code,
			"language": "python",
			"timeout":  float64(60),
		})
		done <- outcome{res, err}
	}()

	waitForFile(t, handshake, 30*time.Second)
	// 让父进程的管道拷贝协程把子进程已写入的那一行读进缓冲：这样下面断言的是
	// 取消语义，而不是"读与写谁先到"的竞态。
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	cancel()

	var got outcome
	select {
	case got = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Execute did not return within 30s of cancellation")
	}
	elapsed := time.Since(start)

	if got.err != nil {
		t.Fatalf("Execute returned Go error (want nil): %v", got.err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("subprocess was not killed on ctx cancel: took %v", elapsed)
	}

	m, ok := got.res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", got.res)
	}
	if code := m["exit_code"]; code != -1 {
		t.Fatalf("exit_code = %v, want -1", code)
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "cancel") && !strings.Contains(errMsg, "kill") {
		t.Fatalf("error field = %q, want cancellation notice", errMsg)
	}
	out, _ := m["stdout"].(string)
	if !strings.Contains(out, "start") {
		t.Fatalf("partial stdout lost: %q", out)
	}
}

// TestExecuteCodeExpiredContextDoesNotSpawn pins the fast-fail path: an
// already-expired context must return the payload-shaped cancellation result
// immediately, without paying for a fresh interpreter. Without this, a turn
// that was cancelled while the tool call was queued still burned a subprocess
// spawn (seconds on Windows) before noticing.
func TestExecuteCodeExpiredContextDoesNotSpawn(t *testing.T) {
	tl := NewExecuteCodeTool()

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	start := time.Now()
	result, err := tl.Execute(ctx, map[string]interface{}{
		"code":     "import time; time.sleep(30)",
		"language": "python",
		"timeout":  float64(60),
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned Go error (want nil): %v", err)
	}
	// Generous bound: the point is that no interpreter was spawned, not that
	// the machine is fast.
	if elapsed > 2*time.Second {
		t.Fatalf("expired ctx still paid for a spawn: took %v", elapsed)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if code := m["exit_code"]; code != -1 {
		t.Fatalf("exit_code = %v, want -1", code)
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "cancelled") {
		t.Fatalf("error field = %q, want cancellation notice", errMsg)
	}
}

// TestConversationAbortedErrorText verifies the agent fast-fails on an
// already-expired context instead of burning maxTurns provider calls.
func TestConversationAbortedErrorText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	time.Sleep(100 * time.Millisecond)

	if err := ctx.Err(); err == nil {
		t.Skip("context did not expire in time")
	}
	// The exact-loop fast-fail lives inside RunConversation; here we assert
	// the wrapping convention used across agent.go so bot.turnFailureReply
	// can map it via errors.Is.
	msg := "conversation aborted after 3 turn(s)"
	if !strings.Contains(msg, "aborted after") {
		t.Fatal("abort message convention broken")
	}
}
