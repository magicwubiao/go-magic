package router

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ProviderRouter provides unified routing with model fallback support
type ProviderRouter struct {
	providers map[string]ProviderCreator
	cooldown  *CooldownTracker
}

// ProviderCreator creates a provider instance
type ProviderCreator func(config ProviderConfig) (Provider, error)

// ProviderConfig holds configuration for creating a provider
type ProviderConfig struct {
	ProviderName string
	Model        string
	APIKey       string
	APIBase      string
	Proxy        string
	ExtraBody    map[string]interface{}
}

// LLMResponse represents a response from LLM
type LLMResponse struct {
	Content     string
	RawResponse interface{}
}

// FailoverReason indicates why a failover occurred
type FailoverReason string

const (
	FailoverRateLimit   FailoverReason = "rate_limit"
	FailoverServerError FailoverReason = "server_error"
	FailoverTimeout     FailoverReason = "timeout"
	FailoverFormat      FailoverReason = "format_error"
	FailoverOther       FailoverReason = "other"
)

// FallbackCandidate represents one model/provider to try
type FallbackCandidate struct {
	Provider string
	Model    string
	RPM      int // requests per minute; 0 means unrestricted
}

// FallbackAttempt records one attempt in the fallback chain
type FallbackAttempt struct {
	Provider string
	Model    string
	Error    error
	Reason   FailoverReason
	Duration time.Duration
	Skipped  bool // true if skipped due to cooldown
}

// FallbackResult contains the successful response and metadata about all attempts
type FallbackResult struct {
	Response *LLMResponse
	Provider string
	Model    string
	Attempts []FallbackAttempt
}

// NewProviderRouter creates a new provider router
func NewProviderRouter() *ProviderRouter {
	return &ProviderRouter{
		providers: make(map[string]ProviderCreator),
		cooldown:  NewCooldownTracker(),
	}
}

// Register registers a provider creator
func (r *ProviderRouter) Register(name string, creator ProviderCreator) {
	r.providers[name] = creator
}

// ExtractProviderModel extracts provider and model from "provider/model" format
func ExtractProviderModel(modelRef string) (provider, model string) {
	modelRef = strings.TrimSpace(modelRef)
	parts := strings.SplitN(modelRef, "/", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "openai", modelRef
}

// ExecuteFallback runs the fallback chain for requests (on router instance)
func (r *ProviderRouter) ExecuteFallback(
	ctx context.Context,
	candidates []FallbackCandidate,
	run func(ctx context.Context, provider, model string) (*LLMResponse, error),
) (*FallbackResult, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("fallback: no candidates configured")
	}

	result := &FallbackResult{
		Attempts: make([]FallbackAttempt, 0, len(candidates)),
	}

	for i, candidate := range candidates {
		// Check context before each attempt
		if ctx.Err() == context.Canceled {
			return nil, context.Canceled
		}

		// Check cooldown
		key := fmt.Sprintf("%s/%s", candidate.Provider, candidate.Model)
		if !r.cooldown.IsAvailable(key) {
			remaining := r.cooldown.Remaining(key)
			result.Attempts = append(result.Attempts, FallbackAttempt{
				Provider: candidate.Provider,
				Model:    candidate.Model,
				Skipped:  true,
				Reason:   FailoverRateLimit,
				Error:    fmt.Errorf("%s in cooldown (%s remaining)", key, remaining.Round(time.Second)),
			})
			continue
		}

		// Execute the run function
		start := time.Now()
		resp, err := run(ctx, candidate.Provider, candidate.Model)
		elapsed := time.Since(start)

		if err == nil {
			// Success
			r.cooldown.MarkSuccess(key)
			result.Response = resp
			result.Provider = candidate.Provider
			result.Model = candidate.Model
			return result, nil
		}

		// Classify the error
		reason := ClassifyError(err)

		// Non-retriable error: abort immediately
		if reason == FailoverFormat {
			result.Attempts = append(result.Attempts, FallbackAttempt{
				Provider: candidate.Provider,
				Model:    candidate.Model,
				Error:    err,
				Reason:   reason,
				Duration: elapsed,
			})
			return nil, &FallbackExhaustedError{Attempts: result.Attempts}
		}

		// Retriable error: mark failure and continue to next candidate
		r.cooldown.MarkFailure(key, reason)
		result.Attempts = append(result.Attempts, FallbackAttempt{
			Provider: candidate.Provider,
			Model:    candidate.Model,
			Error:    err,
			Reason:   reason,
			Duration: elapsed,
		})

		// If this was the last candidate, return aggregate error
		if i == len(candidates)-1 {
			return nil, &FallbackExhaustedError{Attempts: result.Attempts}
		}
	}

	return nil, &FallbackExhaustedError{Attempts: result.Attempts}
}

// fallbackExecute is a package-level fallback executor (standalone version)
func fallbackExecute(
	ctx context.Context,
	candidates []FallbackCandidate,
	run func(ctx context.Context, provider, model string) (*LLMResponse, error),
) (*FallbackResult, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("fallback: no candidates configured")
	}

	result := &FallbackResult{
		Attempts: make([]FallbackAttempt, 0, len(candidates)),
	}

	for i, candidate := range candidates {
		// Check context before each attempt
		if ctx.Err() == context.Canceled {
			return nil, context.Canceled
		}

		// Execute the run function
		start := time.Now()
		resp, err := run(ctx, candidate.Provider, candidate.Model)
		elapsed := time.Since(start)

		if err == nil {
			result.Response = resp
			result.Provider = candidate.Provider
			result.Model = candidate.Model
			return result, nil
		}

		// Classify the error
		reason := ClassifyError(err)

		// Non-retriable error: abort immediately
		if reason == FailoverFormat {
			result.Attempts = append(result.Attempts, FallbackAttempt{
				Provider: candidate.Provider,
				Model:    candidate.Model,
				Error:    err,
				Reason:   reason,
				Duration: elapsed,
			})
			return nil, &FallbackExhaustedError{Attempts: result.Attempts}
		}

		// Record attempt and continue
		result.Attempts = append(result.Attempts, FallbackAttempt{
			Provider: candidate.Provider,
			Model:    candidate.Model,
			Error:    err,
			Reason:   reason,
			Duration: elapsed,
		})

		// If this was the last candidate, return aggregate error
		if i == len(candidates)-1 {
			return nil, &FallbackExhaustedError{Attempts: result.Attempts}
		}
	}

	return nil, &FallbackExhaustedError{Attempts: result.Attempts}
}

// ClassifyError classifies an error for failover decisions
func ClassifyError(err error) FailoverReason {
	if err == nil {
		return FailoverOther
	}

	errMsg := strings.ToLower(err.Error())

	// Rate limit errors
	if strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "rate_limit") {
		return FailoverRateLimit
	}

	// Server errors
	if strings.Contains(errMsg, "500") ||
		strings.Contains(errMsg, "502") ||
		strings.Contains(errMsg, "503") ||
		strings.Contains(errMsg, "server error") ||
		strings.Contains(errMsg, "internal server error") {
		return FailoverServerError
	}

	// Timeout
	if strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "context deadline exceeded") {
		return FailoverTimeout
	}

	// Format errors (non-retriable)
	if strings.Contains(errMsg, "invalid request") ||
		strings.Contains(errMsg, "bad request") ||
		strings.Contains(errMsg, "400") {
		return FailoverFormat
	}

	return FailoverOther
}

// FallbackExhaustedError indicates all fallback candidates were tried and failed
type FallbackExhaustedError struct {
	Attempts []FallbackAttempt
}

func (e *FallbackExhaustedError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("fallback: all %d candidates failed:", len(e.Attempts)))
	for i, a := range e.Attempts {
		if a.Skipped {
			sb.WriteString(fmt.Sprintf("\n  [%d] %s/%s: skipped (cooldown)", i+1, a.Provider, a.Model))
		} else {
			sb.WriteString(fmt.Sprintf("\n  [%d] %s/%s: %v (reason=%s, %s)",
				i+1, a.Provider, a.Model, a.Error, a.Reason, a.Duration.Round(time.Millisecond)))
		}
	}
	return sb.String()
}
