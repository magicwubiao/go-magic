package retry

import (
	"errors"
	"testing"
	"time"
)

func TestRepeatedFailureDetectorEscalatesAtThreshold(t *testing.T) {
	cfg := RepeatedFailureConfig{Threshold: 3, Window: time.Minute, MaxMessageLen: 100}
	d := NewRepeatedFailureDetector(cfg)

	ce := ClassifyError(errors.New("connection timed out"), 0, "openai", "gpt-4") // FailoverTimeout

	// First two occurrences: no escalation.
	if esc := d.Record(ce, "execute_command", "timeout 1"); esc != nil {
		t.Fatalf("expected no escalation on 1st occurrence, got %v", esc)
	}
	if esc := d.Record(ce, "execute_command", "timeout 2"); esc != nil {
		t.Fatalf("expected no escalation on 2nd occurrence, got %v", esc)
	}
	// Third occurrence crosses the threshold.
	esc := d.Record(ce, "execute_command", "timeout 3")
	if esc == nil {
		t.Fatal("expected escalation on 3rd occurrence, got nil")
	}
	if esc.Count != 3 {
		t.Errorf("expected count=3, got %d", esc.Count)
	}
	if esc.Reason != FailoverTimeout {
		t.Errorf("expected reason=%s, got %s", FailoverTimeout, esc.Reason)
	}
	if esc.ToolName != "execute_command" {
		t.Errorf("expected tool=execute_command, got %q", esc.ToolName)
	}
}

func TestRepeatedFailureDetectorDifferentSignaturesDoNotEscalate(t *testing.T) {
	cfg := RepeatedFailureConfig{Threshold: 3, Window: time.Minute, MaxMessageLen: 100}
	d := NewRepeatedFailureDetector(cfg)

	timeoutErr := ClassifyError(errors.New("timed out"), 0, "openai", "gpt-4")      // FailoverTimeout
	rateLimitErr := ClassifyError(errors.New("rate limit"), 429, "openai", "gpt-4") // FailoverRateLimit
	formatErr := ClassifyError(errors.New("bad request"), 400, "openai", "gpt-4")   // FailoverFormatError

	// Different reasons/tools are different equivalence classes; never escalate.
	d.Record(timeoutErr, "execute_command", "t1")
	d.Record(rateLimitErr, "execute_command", "r1")
	d.Record(formatErr, "execute_command", "f1")
	if esc := d.Record(timeoutErr, "web_search", "t2"); esc != nil {
		t.Fatalf("different tool should not escalate, got %v", esc)
	}
}

func TestRepeatedFailureDetectorNilSafe(t *testing.T) {
	d := NewRepeatedFailureDetector(DefaultRepeatedFailureConfig())
	if esc := d.Record(nil, "tool", "msg"); esc != nil {
		t.Fatalf("nil classified error should not escalate, got %v", esc)
	}
	if esc := d.RecordError(nil, 0, "", "", ""); esc != nil {
		t.Fatalf("nil error should not escalate, got %v", esc)
	}
	// Nil receiver is safe.
	var nilDetector *RepeatedFailureDetector
	if esc := nilDetector.Record(nil, "", ""); esc != nil {
		t.Fatalf("nil receiver should not escalate, got %v", esc)
	}
	nilDetector.Reset()
	nilDetector.ResetTool("x")
}

func TestRepeatedFailureDetectorWindowEviction(t *testing.T) {
	cfg := RepeatedFailureConfig{Threshold: 2, Window: 50 * time.Millisecond, MaxMessageLen: 100}
	d := NewRepeatedFailureDetector(cfg)

	ce := ClassifyError(errors.New("server error"), 500, "openai", "gpt-4") // FailoverServerError
	d.Record(ce, "execute_command", "e1")
	// Wait beyond the window so the prior occurrence is evicted.
	time.Sleep(80 * time.Millisecond)
	// A single fresh occurrence after eviction should NOT escalate (threshold 2).
	if esc := d.Record(ce, "execute_command", "e2"); esc != nil {
		t.Fatalf("post-eviction single occurrence should not escalate, got %v", esc)
	}
	// A second fresh occurrence within the window should escalate.
	if esc := d.Record(ce, "execute_command", "e3"); esc == nil {
		t.Fatal("expected escalation after two within-window occurrences")
	}
}

func TestRepeatedFailureDetectorResetTool(t *testing.T) {
	cfg := RepeatedFailureConfig{Threshold: 2, Window: time.Minute, MaxMessageLen: 100}
	d := NewRepeatedFailureDetector(cfg)

	ce := ClassifyError(errors.New("timed out"), 0, "openai", "gpt-4")
	d.Record(ce, "execute_command", "e1")
	// Successful recovery for execute_command resets its streak.
	d.ResetTool("execute_command")
	// Next occurrence is the first in a fresh streak; should not escalate.
	if esc := d.Record(ce, "execute_command", "e2"); esc != nil {
		t.Fatalf("after ResetTool, single occurrence should not escalate, got %v", esc)
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		reason    FailoverReason
		transient bool
	}{
		{FailoverTimeout, true},
		{FailoverRateLimit, true},
		{FailoverOverloaded, true},
		{FailoverServerError, true},
		{FailoverAuth, false},
		{FailoverBilling, false},
		{FailoverFormatError, false},
		{FailoverContentPolicyBlocked, false},
		{FailoverModelNotFound, false},
	}
	for _, c := range cases {
		ce := &ClassifiedError{Reason: c.reason}
		if got := ce.IsTransient(); got != c.transient {
			t.Errorf("reason=%s: expected transient=%v, got %v", c.reason, c.transient, got)
		}
	}
	// Nil receiver safety.
	var nilCE *ClassifiedError
	if nilCE.IsTransient() {
		t.Error("nil ClassifiedError should not be transient")
	}
}
