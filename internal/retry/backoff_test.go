package retry

import (
	"errors"
	"testing"
	"time"
)

func TestJitteredBackoff(t *testing.T) {
	// Test that backoff increases with attempt number
	var delays []time.Duration
	for i := 1; i <= 5; i++ {
		delay := JitteredBackoff(i)
		delays = append(delays, delay)
	}

	// Each delay should be >= base delay (with jitter)
	for i, d := range delays {
		if d < 5*time.Second {
			t.Errorf("attempt %d: delay %v should be >= 5s", i+1, d)
		}
	}

	// Delays should generally increase (though jitter can make this non-monotonic)
	// We just verify they're all positive
	for i, d := range delays {
		if d <= 0 {
			t.Errorf("attempt %d: delay should be positive, got %v", i+1, d)
		}
	}
}

func TestJitteredBackoffMaxCap(t *testing.T) {
	// Test that very high attempts don't exceed max delay
	delay := JitteredBackoff(100)
	if delay > 120*time.Second+60*time.Second { // max + max jitter
		t.Errorf("delay %v should be capped near max", delay)
	}
}

func TestJitteredBackoffSeconds(t *testing.T) {
	seconds := JitteredBackoffSeconds(1)
	if seconds < 5.0 {
		t.Errorf("expected >= 5.0 seconds, got %f", seconds)
	}
}

func TestDecorrelatedJitter(t *testing.T) {
	// Test decorrelated jitter produces valid delays
	for i := 1; i <= 5; i++ {
		delay := DecorrelatedJitter(i)
		if delay < 5*time.Second {
			t.Errorf("attempt %d: decorrelated delay %v should be >= 5s", i, delay)
		}
		if delay > 120*time.Second*3 {
			t.Errorf("attempt %d: decorrelated delay %v should be bounded", i, delay)
		}
	}
}

func TestLinearBackoff(t *testing.T) {
	delay := LinearBackoff(3, 2*time.Second, 10*time.Second)
	expected := 6 * time.Second
	if delay != expected {
		t.Errorf("expected %v, got %v", expected, delay)
	}

	// Test max cap
	delay = LinearBackoff(100, 1*time.Second, 5*time.Second)
	if delay != 5*time.Second {
		t.Errorf("expected 5s cap, got %v", delay)
	}
}

func TestFixedBackoff(t *testing.T) {
	delay := FixedBackoff(3 * time.Second)
	if delay != 3*time.Second {
		t.Errorf("expected 3s, got %v", delay)
	}
}

func TestRetryFunc(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("retry me")
		}
		return nil
	}

	err := RetryFunc(fn, 5)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryFuncExhausted(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("always fails")
	}

	err := RetryFunc(fn, 3)
	if err == nil {
		t.Error("expected error")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryFuncWithResult(t *testing.T) {
	attempts := 0
	fn := func() (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("retry")
		}
		return "success", nil
	}

	result, err := RetryFuncWithResult(fn, 5)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected 'success', got %s", result)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetryWithClassifier(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("rate limit exceeded")
	}

	classifier := NewClassifier()
	err := RetryWithClassifier(fn, classifier, 3)
	if err == nil {
		t.Error("expected error after retries")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryWithClassifierNonRetryable(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("insufficient credits")
	}

	classifier := NewClassifier()
	err := RetryWithClassifier(fn, classifier, 5)
	if err == nil {
		t.Error("expected error")
	}
	// Should stop after first attempt because billing errors are non-retryable
	if attempts != 1 {
		t.Errorf("expected 1 attempt (non-retryable), got %d", attempts)
	}
}

func TestRetryWithClassifierAbort(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("content policy violation")
	}

	classifier := NewClassifier()
	err := RetryWithClassifier(fn, classifier, 5)
	if err == nil {
		t.Error("expected error")
	}
	// Should stop after first attempt because content policy errors abort
	if attempts != 1 {
		t.Errorf("expected 1 attempt (abort), got %d", attempts)
	}
}

func TestBackoffConfig(t *testing.T) {
	cfg := DefaultBackoffConfig()
	if cfg.BaseDelay != 5*time.Second {
		t.Errorf("expected 5s base delay, got %v", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 120*time.Second {
		t.Errorf("expected 120s max delay, got %v", cfg.MaxDelay)
	}
	if cfg.JitterRatio != 0.5 {
		t.Errorf("expected 0.5 jitter ratio, got %f", cfg.JitterRatio)
	}
}

func TestPowInt(t *testing.T) {
	if powInt(2, 0) != 1 {
		t.Error("2^0 should be 1")
	}
	if powInt(2, 3) != 8 {
		t.Error("2^3 should be 8")
	}
	if powInt(3, 2) != 9 {
		t.Error("3^2 should be 9")
	}
}

func TestJitterDecorrelation(t *testing.T) {
	// Run multiple backoffs and verify they produce different values
	// (testing that jitter actually decorrelates)
	values := make(map[time.Duration]int)
	for i := 0; i < 20; i++ {
		d := JitteredBackoff(1)
		values[d]++
	}

	// With jitter, we should have multiple different values
	if len(values) < 5 {
		t.Errorf("expected jitter to produce varied delays, got %d unique values", len(values))
	}
}
