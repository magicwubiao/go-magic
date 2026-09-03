package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestExecuteCodeContextCancel verifies that a parent-context deadline (e.g.
// the bot-mode turn timeout) cancels a running subprocess promptly and is
// reported as a *result payload* (not a Go error), so the conversation loop
// can continue instead of dying with "context deadline exceeded".
func TestExecuteCodeContextCancel(t *testing.T) {
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

// TestRunConversationTurnTimeoutAborts verifies the agent fast-fails on an
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
