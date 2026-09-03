package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassifyErrorNil(t *testing.T) {
	result := ClassifyError(nil, 0, "", "")
	if result != nil {
		t.Error("expected nil for nil error")
	}
}

func TestClassifyErrorStatusCodes(t *testing.T) {
	tests := []struct {
		statusCode     int
		expectedReason FailoverReason
		retryable      bool
	}{
		{401, FailoverAuth, true},
		{403, FailoverAuth, true},
		{402, FailoverBilling, false},
		{404, FailoverModelNotFound, false},
		{413, FailoverPayloadTooLarge, true},
		{429, FailoverRateLimit, true},
		{500, FailoverServerError, true},
		{502, FailoverServerError, true},
		{503, FailoverOverloaded, true},
		{529, FailoverOverloaded, true},
		{400, FailoverFormatError, false},
	}

	for _, tt := range tests {
		err := errors.New("test error")
		ce := ClassifyError(err, tt.statusCode, "openai", "gpt-4")
		if ce == nil {
			t.Fatalf("status %d: expected classification", tt.statusCode)
		}
		if ce.Reason != tt.expectedReason {
			t.Errorf("status %d: expected %s, got %s", tt.statusCode, tt.expectedReason, ce.Reason)
		}
		if ce.Retryable != tt.retryable {
			t.Errorf("status %d: expected retryable=%v, got %v", tt.statusCode, tt.retryable, ce.Retryable)
		}
	}
}

func TestClassifyErrorPatterns(t *testing.T) {
	tests := []struct {
		msg            string
		expectedReason FailoverReason
		retryable      bool
	}{
		{"rate limit exceeded", FailoverRateLimit, true},
		{"too many requests", FailoverRateLimit, true},
		{"insufficient credits", FailoverBilling, false},
		{"payment required", FailoverBilling, false},
		// Alternative word orders used by Anthropic and other providers must
		// still classify as billing; otherwise the error falls through to
		// FailoverUnknown and triggers the confusing "reason=unknown"
		// repeated-failure escalation that prompted this fix.
		{"Sorry, your account balance is insufficient", FailoverBilling, false},
		{"stream request failed: balance has been depleted", FailoverBilling, false},
		{"quota has been exceeded for this account", FailoverBilling, false},
		{"timeout", FailoverTimeout, true},
		{"connection timed out", FailoverTimeout, true},
		{"context length exceeded", FailoverContextOverflow, false},
		{"too many tokens", FailoverContextOverflow, false},
		{"model not found", FailoverModelNotFound, false},
		{"content policy violation", FailoverContentPolicyBlocked, false},
		{"authentication failed", FailoverAuth, true},
		{"internal server error", FailoverServerError, true},
		{"bad request", FailoverFormatError, false},
		// Zhipu/智谱 permanent 400 pathologies used to fall through to
		// FailoverUnknown Retryable=true, which burned turns and eventually
		// surfaced as the confusing "reason=unknown" escalation wrapper.
		{"api error [1214]: messages 参数非法。请检查文档。", FailoverFormatError, false},
		{"stream request failed: status 400: messages参数非法", FailoverFormatError, false},
		{"参数无效: messages role alternation violated", FailoverFormatError, false},
		{"some random error", FailoverUnknown, true},
	}

	for _, tt := range tests {
		err := errors.New(tt.msg)
		ce := ClassifyError(err, 0, "", "")
		if ce == nil {
			t.Fatalf("msg '%s': expected classification", tt.msg)
		}
		if ce.Reason != tt.expectedReason {
			t.Errorf("msg '%s': expected %s, got %s", tt.msg, tt.expectedReason, ce.Reason)
		}
		if ce.Retryable != tt.retryable {
			t.Errorf("msg '%s': expected retryable=%v, got %v", tt.msg, tt.retryable, ce.Retryable)
		}
	}
}

func TestClassifiedErrorIsAuth(t *testing.T) {
	authErr := &ClassifiedError{Reason: FailoverAuth}
	if !authErr.IsAuth() {
		t.Error("expected auth error to be identified as auth")
	}

	billingErr := &ClassifiedError{Reason: FailoverBilling}
	if billingErr.IsAuth() {
		t.Error("billing error should not be auth")
	}
}

func TestClassifiedErrorIsRetryable(t *testing.T) {
	retryable := &ClassifiedError{Reason: FailoverServerError, Retryable: true, ShouldAbort: false}
	if !retryable.IsRetryable() {
		t.Error("expected retryable")
	}

	nonRetryable := &ClassifiedError{Reason: FailoverBilling, Retryable: false}
	if nonRetryable.IsRetryable() {
		t.Error("expected non-retryable")
	}

	abort := &ClassifiedError{Reason: FailoverContentPolicyBlocked, Retryable: true, ShouldAbort: true}
	if abort.IsRetryable() {
		t.Error("abort should not be retryable")
	}
}

func TestGetRecoveryStrategy(t *testing.T) {
	tests := []struct {
		reason         FailoverReason
		expectAbort    bool
		expectCompress bool
	}{
		{FailoverAuth, false, false},
		{FailoverAuthPermanent, true, false},
		{FailoverBilling, true, false}, // 402/余额不足是永久错误，必须立即 Abort 不能等 escalation
		{FailoverRateLimit, false, false},
		{FailoverContextOverflow, false, true},
		{FailoverPayloadTooLarge, false, true},
		{FailoverContentPolicyBlocked, true, false},
		{FailoverFormatError, true, false}, // Status-400 / [1214] messages 参数非法 is permanent — no amount of retrying fixes a bad payload shape.
	}

	for _, tt := range tests {
		ce := &ClassifiedError{Reason: tt.reason, Retryable: !tt.expectAbort}
		strategy := GetRecoveryStrategy(ce, 1)
		if strategy.Abort != tt.expectAbort {
			t.Errorf("%s: expected abort=%v, got %v", tt.reason, tt.expectAbort, strategy.Abort)
		}
		if strategy.Compress != tt.expectCompress {
			t.Errorf("%s: expected compress=%v, got %v", tt.reason, tt.expectCompress, strategy.Compress)
		}
	}
}

func TestClassifierCustomRules(t *testing.T) {
	c := NewClassifier()

	// Add a custom rule
	c.AddRule(func(err error, statusCode int, provider, model string) *ClassifiedError {
		if err != nil && err.Error() == "custom_error" {
			return &ClassifiedError{
				Reason:      "custom_reason",
				Retryable:   false,
				ShouldAbort: true,
			}
		}
		return nil
	})

	// Test custom rule matches
	ce := c.Classify(errors.New("custom_error"), 0, "", "")
	if ce == nil {
		t.Fatal("expected custom classification")
	}
	if ce.Reason != "custom_reason" {
		t.Errorf("expected custom_reason, got %s", ce.Reason)
	}

	// Test fallback to default
	ce = c.Classify(errors.New("rate limit"), 0, "", "")
	if ce == nil {
		t.Fatal("expected default classification")
	}
	if ce.Reason != FailoverRateLimit {
		t.Errorf("expected rate_limit, got %s", ce.Reason)
	}
}

func TestClassifyWithContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure context is cancelled

	err := errors.New("some error")
	ce := ClassifyWithContext(ctx, err, 0, "", "")
	if ce == nil {
		t.Fatal("expected classification")
	}

	if ce.ErrorContext["context_cancelled"] != true {
		t.Error("expected context_cancelled to be true")
	}
}

func TestMatchesAny(t *testing.T) {
	patterns := []string{"hello", "world"}
	if !matchesAny("hello there", patterns) {
		t.Error("expected match")
	}
	if !matchesAny("world peace", patterns) {
		t.Error("expected match")
	}
	if matchesAny("foo bar", patterns) {
		t.Error("expected no match")
	}
}

func TestClassifiedErrorString(t *testing.T) {
	ce := &ClassifiedError{
		Reason:    FailoverRateLimit,
		Message:   "too many requests",
		Retryable: true,
	}
	str := ce.String()
	if str == "" {
		t.Error("expected non-empty string")
	}
	if !contains(str, "rate_limit") {
		t.Error("expected reason in string")
	}
}

func TestExtractStatusCode(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		// Exact on-site error from the user's report: provider status wrapped
		// inside a stream-level error. Must extract 402 so the classifier
		// switch hits case 402 → FailoverBilling directly.
		{"stream request failed: stream API returned status 402: anthropic error: Sorry, your account balance is insufficient", 402},
		{"got HTTP status 429 Too Many Requests", 429},
		{"upstream error StatusCode=401", 401},
		{"error: status_code: 403 for request", 403},
		// 5xx codes should also extract cleanly
		{"server status 500", 500},
		// No status code present → 0
		{"some random error with no HTTP info", 0},
		{"", 0},
		// Non-3-digit numbers should not match (e.g. version strings)
		{"HTTP version 1.1 status 200 OK", 200}, // first match is "1.1 status 200" but regex requires 3-digit after HTTP, we want 200
	}

	for _, c := range cases {
		got := ExtractStatusCode(c.input)
		if got != c.expected {
			t.Errorf("ExtractStatusCode(%q) = %d, want %d", c.input, got, c.expected)
		}
	}
}

// TestClassifyStatusCodeFromMessage verifies the end-to-end scenario reported
// by the user: when status=402 is embedded in the message text AND the
// anthropic "balance is insufficient" wording is used, we still classify as
// FailoverBilling (not FailoverUnknown), Retryable=false, which in turn
// causes the agent loop to abort immediately instead of looping 3 turns.
func TestClassifyStatusCodeFromMessage(t *testing.T) {
	rawErr := errors.New("stream request failed: stream API returned status 402: anthropic error: Sorry, your account balance is insufficient")
	// Simulate what the agent call-sites do now: parse status from text.
	status := ExtractStatusCode(rawErr.Error())
	if status != 402 {
		t.Fatalf("expected extracted status=402, got %d", status)
	}
	ce := ClassifyError(rawErr, status, "anthropic", "claude-sonnet")
	if ce == nil {
		t.Fatal("expected classified error, got nil")
	}
	if ce.Reason != FailoverBilling {
		t.Errorf("expected FailoverBilling, got %s", ce.Reason)
	}
	if ce.Retryable {
		t.Error("expected billing error to be non-retryable")
	}
	strategy := GetRecoveryStrategy(ce, 1)
	if !strategy.Abort {
		t.Error("expected billing strategy to abort immediately")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
