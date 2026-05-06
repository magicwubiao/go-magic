package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// BackoffStrategy defines the interface for backoff strategies
type BackoffStrategy interface {
	// NextDelay returns the delay for the next retry attempt
	// attempt starts at 1 for the first retry
	NextDelay(attempt int) time.Duration
}

// FixedBackoff is a backoff strategy with fixed intervals
type FixedBackoff time.Duration

// NextDelay implements BackoffStrategy
func (f FixedBackoff) NextDelay(attempt int) time.Duration {
	return time.Duration(f)
}

// LinearBackoff increases delay linearly with each attempt
type LinearBackoff struct {
	Base    time.Duration
	Increment time.Duration
	Max     time.Duration
}

// NextDelay implements BackoffStrategy
func (l LinearBackoff) NextDelay(attempt int) time.Duration {
	delay := l.Base + l.Increment*time.Duration(attempt-1)
	if delay > l.Max {
		delay = l.Max
	}
	return delay
}

// ExponentialBackoff increases delay exponentially with each attempt
type ExponentialBackoff struct {
	Base time.Duration
	Max  time.Duration
}

// NextDelay implements BackoffStrategy
func (e ExponentialBackoff) NextDelay(attempt int) time.Duration {
	delay := time.Duration(float64(e.Base) * math.Pow(2, float64(attempt-1)))
	if delay > e.Max {
		delay = e.Max
	}
	return delay
}

// ExponentialBackoffWithJitter adds jitter to exponential backoff
type ExponentialBackoffWithJitter struct {
	Base       time.Duration
	Max        time.Duration
	JitterFactor float64 // 0-1, percentage of jitter
}

// NextDelay implements BackoffStrategy
func (e ExponentialBackoffWithJitter) NextDelay(attempt int) time.Duration {
	baseDelay := time.Duration(float64(e.Base) * math.Pow(2, float64(attempt-1)))
	if baseDelay > e.Max {
		baseDelay = e.Max
	}
	
	// Add jitter
	jitter := time.Duration(float64(baseDelay) * e.JitterFactor * (rand.Float64()*2 - 1))
	return baseDelay + jitter
}

// FibonacciBackoff increases delay following the Fibonacci sequence
type FibonacciBackoff struct {
	Base time.Duration
	Max  time.Duration
}

// NextDelay implements BackoffStrategy
func (f FibonacciBackoff) NextDelay(attempt int) time.Duration {
	if attempt <= 2 {
		return f.Base
	}
	
	// Calculate Fibonacci number
	fib := fibonacci(attempt)
	delay := f.Base * time.Duration(fib)
	
	if delay > f.Max {
		delay = f.Max
	}
	return delay
}

// fibonacci calculates the nth Fibonacci number
func fibonacci(n int) int64 {
	if n <= 1 {
		return int64(n)
	}
	
	var a, b int64 = 0, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

// RetryCondition defines when to retry
type RetryCondition func(err error) bool

// RetryOnError creates a condition that retries on all errors
func RetryOnError(err error) bool {
	return true
}

// RetryOnTimeout creates a condition that retries only on timeout errors
func RetryOnTimeout(err error) bool {
	return strings.Contains(err.Error(), "timeout")
}

// RetryOnTemporary creates a condition that retries on temporary errors
func RetryOnTemporary(err error) bool {
	if te, ok := err.(interface{ Temporary() bool }); ok {
		return te.Temporary()
	}
	return false
}

// RetryConfig contains retry configuration
type RetryConfig struct {
	MaxAttempts int
	Backoff     BackoffStrategy
	RetryOn     []RetryCondition
	Timeout     time.Duration
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 3,
		Backoff:     ExponentialBackoff{Base: 100 * time.Millisecond, Max: 30 * time.Second},
		RetryOn:     []RetryCondition{RetryOnError},
		Timeout:     5 * time.Minute,
	}
}

// RetryResult contains the result of a retry operation
type RetryResult struct {
	Value      interface{}
	Error      error
	Attempts   int
	TotalTime  time.Duration
	LastDelay  time.Duration
	Success    bool
}

// Do executes the operation with retry logic
func Do(ctx context.Context, config *RetryConfig, op func() error) error {
	var lastErr error
	
	start := time.Now()
	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		default:
		}
		
		// Execute operation
		err := op()
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		// Check if we should retry
		shouldRetry := false
		for _, condition := range config.RetryOn {
			if condition(err) {
				shouldRetry = true
				break
			}
		}
		
		if !shouldRetry || attempt >= config.MaxAttempts {
			return err
		}
		
		// Calculate delay
		var delay time.Duration
		if config.Backoff != nil {
			delay = config.Backoff.NextDelay(attempt)
		}
		
		// Check timeout
		if config.Timeout > 0 && time.Since(start)+delay > config.Timeout {
			return fmt.Errorf("retry timeout exceeded: %w", err)
		}
		
		// Wait before next attempt
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled during backoff: %w", ctx.Err())
		case <-time.After(delay):
		}
	}
	
	return lastErr
}

// DoWithResult executes the operation with retry logic and returns a result
func DoWithResult(ctx context.Context, config *RetryConfig, op func() (interface{}, error)) *RetryResult {
	result := &RetryResult{}
	start := time.Now()
	
	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			result.Error = fmt.Errorf("retry cancelled: %w", ctx.Err())
			return result
		default:
		}
		
		// Execute operation
		value, err := op()
		result.Value = value
		
		if err == nil {
			result.Success = true
			result.Attempts = attempt
			result.TotalTime = time.Since(start)
			return result
		}
		
		result.Error = err
		result.Attempts = attempt
		result.LastDelay = config.Backoff.NextDelay(attempt)
		
		// Check if we should retry
		shouldRetry := false
		for _, condition := range config.RetryOn {
			if condition(err) {
				shouldRetry = true
				break
			}
		}
		
		if !shouldRetry || attempt >= config.MaxAttempts {
			result.TotalTime = time.Since(start)
			return result
		}
		
		// Check timeout
		if config.Timeout > 0 && time.Since(start)+result.LastDelay > config.Timeout {
			result.Error = fmt.Errorf("retry timeout exceeded: %w", err)
			result.TotalTime = time.Since(start)
			return result
		}
		
		// Wait before next attempt
		select {
		case <-ctx.Done():
			result.Error = fmt.Errorf("retry cancelled during backoff: %w", ctx.Err())
			result.TotalTime = time.Since(start)
			return result
		case <-time.After(result.LastDelay):
		}
	}
	
	result.TotalTime = time.Since(start)
	return result
}

// Retryer is a retry helper with built-in state
type Retryer struct {
	config    *RetryConfig
	attempts  int
	totalTime time.Duration
	lastDelay time.Duration
}

// NewRetryer creates a new Retryer with the given config
func NewRetryer(config *RetryConfig) *Retryer {
	return &Retryer{
		config: config,
	}
}

// Attempt executes a single retry attempt
func (r *Retryer) Attempt(ctx context.Context, op func() error) error {
	r.attempts++
	
	// Check timeout
	if r.config.Timeout > 0 && r.totalTime > r.config.Timeout {
		return fmt.Errorf("retry timeout exceeded")
	}
	
	err := op()
	if err == nil {
		return nil
	}
	
	// Check if we should retry
	shouldRetry := false
	for _, condition := range r.config.RetryOn {
		if condition(err) {
			shouldRetry = true
			break
		}
	}
	
	if !shouldRetry {
		return err
	}
	
	// Calculate delay
	if r.config.Backoff != nil {
		r.lastDelay = r.config.Backoff.NextDelay(r.attempts)
	}
	
	return err
}

// Wait waits for the backoff delay
func (r *Retryer) Wait(ctx context.Context) error {
	if r.lastDelay == 0 {
		return nil
	}
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(r.lastDelay):
		r.totalTime += r.lastDelay
		return nil
	}
}

// Reset resets the retryer state
func (r *Retryer) Reset() {
	r.attempts = 0
	r.totalTime = 0
	r.lastDelay = 0
}

// Attempts returns the number of attempts made
func (r *Retryer) Attempts() int {
	return r.attempts
}

// TotalTime returns the total time spent retrying
func (r *Retryer) TotalTime() time.Duration {
	return r.totalTime
}

// LastDelay returns the last backoff delay
func (r *Retryer) LastDelay() time.Duration {
	return r.lastDelay
}

// ExponentialBackoffDefault is a sensible default exponential backoff
var ExponentialBackoffDefault = ExponentialBackoffWithJitter{
	Base:         100 * time.Millisecond,
	Max:          30 * time.Second,
	JitterFactor: 0.2,
}

// LinearBackoffDefault is a sensible default linear backoff
var LinearBackoffDefault = LinearBackoff{
	Base:      100 * time.Millisecond,
	Increment: 100 * time.Millisecond,
	Max:       30 * time.Second,
}

// FixedBackoffDefault is a sensible default fixed backoff
var FixedBackoffDefault = FixedBackoff(1 * time.Second)

// RetryConfigForNetwork creates a retry config optimized for network operations
func RetryConfigForNetwork() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 5,
		Backoff:     ExponentialBackoffDefault,
		RetryOn:     []RetryCondition{RetryOnError},
		Timeout:     30 * time.Second,
	}
}

// RetryConfigForIO creates a retry config optimized for I/O operations
func RetryConfigForIO() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 3,
		Backoff:     LinearBackoffDefault,
		RetryOn:     []RetryCondition{RetryOnError},
		Timeout:     10 * time.Second,
	}
}

// RetryConfigForDatabase creates a retry config optimized for database operations
func RetryConfigForDatabase() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 3,
		Backoff:     FixedBackoff(500 * time.Millisecond),
		RetryOn:     []RetryCondition{RetryOnTemporary},
		Timeout:     15 * time.Second,
	}
}
