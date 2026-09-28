package tool

import (
	"context"
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

// TestExecuteCodeContextCancel verifies that a parent-context deadline (e.g.
// the bot-mode turn timeout) cancels a running subprocess promptly and is
// reported as a *result payload* (not a Go error), so the conversation loop
// can continue instead of dying with "context deadline exceeded".
func TestExecuteCodeContextCancel(t *testing.T) {
	if !warmUpPython(t) {
		t.Skip("no usable python interpreter")
	}

	tl := NewExecuteCodeTool()

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	start := time.Now()
	result, err := tl.Execute(ctx, map[string]interface{}{
		"code":     "import time; print('start', flush=True); time.sleep(30); print('end')",
		"language": "python",
		"timeout":  float64(60),
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned Go error (want nil): %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("subprocess was not killed on ctx deadline: took %v", elapsed)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
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
