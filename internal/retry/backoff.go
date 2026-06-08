// Package retry provides jittered exponential backoff for decorrelated retries.
// Inspired by Hermes Agent's retry_utils, this prevents thundering-herd retry spikes
// when multiple sessions hit the same rate-limited provider concurrently.
package retry

import (
	"math/rand"
	"sync"
	"time"
)

// Monotonic counter for jitter seed uniqueness within the same process.
// Protected by a lock to avoid race conditions in concurrent retry paths.
var (
	_jitterCounter int64
	_jitterLock    sync.Mutex
)

// BackoffConfig configures the backoff behavior.
type BackoffConfig struct {
	BaseDelay     time.Duration // Base delay for attempt 1 (default 5s)
	MaxDelay      time.Duration // Maximum delay cap (default 120s)
	JitterRatio   float64       // Fraction of delay for random jitter range (default 0.5)
	MaxAttempts   int           // Maximum retry attempts (default 5)
	ExponentialBase float64     // Exponential base (default 2.0)
}

// DefaultBackoffConfig returns the default backoff configuration.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:       5 * time.Second,
		MaxDelay:        120 * time.Second,
		JitterRatio:     0.5,
		MaxAttempts:     5,
		ExponentialBase: 2.0,
	}
}

// JitteredBackoff computes a jittered exponential backoff delay.
//
// Args:
//   - attempt: 1-based retry attempt number.
//   - baseDelay: Base delay for attempt 1.
//   - maxDelay: Maximum delay cap.
//   - jitterRatio: Fraction of computed delay to use as random jitter range.
//
// Returns:
//   - Delay in seconds: min(base * 2^(attempt-1), maxDelay) + jitter.
//
// The jitter decorrelates concurrent retries so multiple sessions
// hitting the same provider don't all retry at the same instant.
func JitteredBackoff(attempt int, config ...BackoffConfig) time.Duration {
	cfg := DefaultBackoffConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	_jitterLock.Lock()
	_jitterCounter++
	tick := _jitterCounter
	_jitterLock.Unlock()

	exponent := max(0, attempt-1)
	var delay time.Duration

	if exponent >= 63 || cfg.BaseDelay <= 0 {
		delay = cfg.MaxDelay
	} else {
		delay = min(cfg.BaseDelay*time.Duration(powInt(int(cfg.ExponentialBase), exponent)), cfg.MaxDelay)
	}

	// Seed from time + counter for decorrelation even with coarse clocks.
	seed := (time.Now().UnixNano() ^ (tick * 0x9E3779B9)) & 0xFFFFFFFF
	rng := rand.New(rand.NewSource(seed))
	jitter := time.Duration(rng.Float64() * cfg.JitterRatio * float64(delay))

	return delay + jitter
}

// JitteredBackoffSeconds returns the backoff delay in seconds as a float64.
func JitteredBackoffSeconds(attempt int, config ...BackoffConfig) float64 {
	return JitteredBackoff(attempt, config...).Seconds()
}

// SleepJittered sleeps for the jittered backoff duration.
func SleepJittered(attempt int, config ...BackoffConfig) {
	time.Sleep(JitteredBackoff(attempt, config...))
}

// powInt computes base^exp for integer values.
func powInt(base, exp int) int {
	if exp == 0 {
		return 1
	}
	result := base
	for i := 1; i < exp; i++ {
		result *= base
	}
	return result
}

// RetryFunc executes a function with jittered backoff retries.
// Returns the last error if all retries are exhausted.
func RetryFunc(fn func() error, maxAttempts int, config ...BackoffConfig) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < maxAttempts {
			SleepJittered(attempt, config...)
		}
	}
	return lastErr
}

// RetryFuncWithResult executes a function that returns a result with jittered backoff retries.
func RetryFuncWithResult[T any](fn func() (T, error), maxAttempts int, config ...BackoffConfig) (T, error) {
	var zero T
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt < maxAttempts {
			SleepJittered(attempt, config...)
		}
	}
	return zero, lastErr
}

// RetryWithClassifier executes a function with intelligent error classification and backoff.
// Uses the error classifier to determine if retries should continue.
func RetryWithClassifier(fn func() error, classifier *Classifier, maxAttempts int, config ...BackoffConfig) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		// Classify the error
		ce := classifier.Classify(err, 0, "", "")
		if !ce.IsRetryable() {
			return err // Non-retryable error, return immediately
		}

		strategy := GetRecoveryStrategy(ce, attempt)
		if strategy.Abort {
			return err // Strategy says abort
		}

		if attempt < maxAttempts {
			SleepJittered(attempt, config...)
		}
	}
	return lastErr
}

// LinearBackoff provides a simple linear backoff without jitter.
// Useful when jitter is not desired.
func LinearBackoff(attempt int, baseDelay time.Duration, maxDelay time.Duration) time.Duration {
	delay := time.Duration(attempt) * baseDelay
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// FixedBackoff provides a fixed delay regardless of attempt number.
func FixedBackoff(delay time.Duration) time.Duration {
	return delay
}

// DecorrelatedJitter provides an alternative jitter strategy that increases
// the jitter range with each attempt for better decorrelation.
func DecorrelatedJitter(attempt int, config ...BackoffConfig) time.Duration {
	cfg := DefaultBackoffConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	_jitterLock.Lock()
	_jitterCounter++
	tick := _jitterCounter
	_jitterLock.Unlock()

	// Use decorrelated jitter formula: delay = min(maxDelay, random(baseDelay, delay * 3))
	seed := (time.Now().UnixNano() ^ (tick * 0x9E3779B9)) & 0xFFFFFFFF
	rng := rand.New(rand.NewSource(seed))

	// Calculate base exponential delay
	exponent := max(0, attempt-1)
	var baseDelay time.Duration
	if exponent >= 63 || cfg.BaseDelay <= 0 {
		baseDelay = cfg.MaxDelay
	} else {
		baseDelay = min(cfg.BaseDelay*time.Duration(powInt(int(cfg.ExponentialBase), exponent)), cfg.MaxDelay)
	}

	// Decorrelated jitter: random between baseDelay and baseDelay * 3
	maxJitterDelay := baseDelay * 3
	if maxJitterDelay > cfg.MaxDelay {
		maxJitterDelay = cfg.MaxDelay
	}

	jitterRange := maxJitterDelay - baseDelay
	if jitterRange < 0 {
		jitterRange = 0
	}

	jitter := time.Duration(rng.Float64() * float64(jitterRange))
	return baseDelay + jitter
}

// BackoffStrategy defines the interface for backoff strategies.
// Used by internal/provider for retry configuration.
type BackoffStrategy interface {
	Backoff(attempt int) time.Duration
	NextDelay(attempt int) time.Duration
}

// ExponentialBackoff implements a simple exponential backoff strategy.
type ExponentialBackoff struct {
	Base time.Duration // Base delay for first retry
	Max  time.Duration // Maximum delay cap
}

// Backoff returns the delay for the given attempt (1-based).
func (eb ExponentialBackoff) Backoff(attempt int) time.Duration {
	return eb.NextDelay(attempt)
}

// NextDelay returns the delay for the given attempt (1-based).
func (eb ExponentialBackoff) NextDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	delay := eb.Base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > eb.Max {
			return eb.Max
		}
	}
	if delay > eb.Max {
		return eb.Max
	}
	return delay
}
