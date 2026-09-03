package bot

import (
	"errors"
	"testing"
	"time"
)

// TestClassifyFailureContentPolicy verifies provider content-policy rejections
// map to FailureContentPolicy (not the generic tool_error fallback), and are
// non-retryable.
func TestClassifyFailureContentPolicy(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"openai-compatible type label", errors.New("chat with tools request failed: api error [content_policy_error]: content blocked by policy violation")},
		{"policy violation text", errors.New("api error: content blocked by policy violation")},
		{"harmful content", errors.New("the response was filtered: harmful content detected")},
		{"moderation", errors.New("moderation: safety filter triggered")},
		{"403-paired policy block", errors.New("status 403, api error [content_policy_error]: policy violation")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls := classifyFailure(tc.err)
			if cls.Code != FailureContentPolicy {
				t.Errorf("classifyFailure(%q) = %s, want %s", tc.err, cls.Code, FailureContentPolicy)
			}
			if cls.Transient || cls.Retryable {
				t.Errorf("content policy failure must not be retryable: %+v", cls)
			}
		})
	}
}

// TestClassifyFailureToolError guards the generic tool-error branch: failures
// that mention tools but carry no content-policy wording must still map to
// FailureToolError.
func TestClassifyFailureToolError(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"tool exec failure", errors.New("tool exec failed: boom")},
		{"tool call failed", errors.New("tool call failed: boom")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls := classifyFailure(tc.err)
			if cls.Code != FailureToolError {
				t.Errorf("classifyFailure(%q) = %s, want %s", tc.err, cls.Code, FailureToolError)
			}
		})
	}
}

// TestTurnFailureReplyContentPolicy verifies the user-facing message for a
// content-policy block is the friendly hint, carrying the typed code.
func TestTurnFailureReplyContentPolicy(t *testing.T) {
	err := errors.New("chat with tools request failed: api error [content_policy_error]: content blocked by policy violation")
	reply, code := turnFailureReplyCoded(err, 5*time.Minute)
	if code != FailureContentPolicy {
		t.Errorf("code = %s, want %s", code, FailureContentPolicy)
	}
	if reply == "(error: "+err.Error()+")" {
		t.Errorf("expected friendly hint, got raw error passthrough: %q", reply)
	}
}
