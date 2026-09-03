// Package retry provides intelligent error classification and recovery strategies
// for long-running agent tasks. Inspired by Hermes Agent's error_classifier.
package retry

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// statusCodeRe extracts a 3-digit HTTP status code when it appears inside a
// provider error string. Common formats:
//   - "stream API returned status 402: anthropic error: ..."
//   - "got HTTP status 429 Too Many Requests"
//   - "StatusCode=401" / "status_code: 403"
//
// Agent call-sites historically hard-code statusCode=0 because the raw
// transport status has been wrapped into err.Error(). Extracting it here
// lets classification honour the real numeric status (401/402/429 branches
// are deterministic and much more reliable than pure substring matching).
var statusCodeRe = regexp.MustCompile(`(?i)(?:status(?:\s*_?\s*code)?\s*[:=]?\s*|HTTP\s+)(\d{3})`)

// ExtractStatusCode returns the first 3-digit HTTP status code embedded in
// errMsg, or 0 when none is present.
func ExtractStatusCode(errMsg string) int {
	if errMsg == "" {
		return 0
	}
	m := statusCodeRe.FindStringSubmatch(errMsg)
	if len(m) < 2 {
		return 0
	}
	code, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return code
}

// FailoverReason categorizes why an API call or tool execution failed.
// This determines the recovery strategy.
type FailoverReason string

const (
	// Authentication / authorization
	FailoverAuth          FailoverReason = "auth"           // Transient auth (401/403) — refresh/rotate
	FailoverAuthPermanent FailoverReason = "auth_permanent" // Auth failed after refresh — abort

	// Billing / quota
	FailoverBilling   FailoverReason = "billing"    // 402 or credit exhaustion — rotate immediately
	FailoverRateLimit FailoverReason = "rate_limit" // 429 or throttling — backoff then rotate

	// Server-side
	FailoverOverloaded  FailoverReason = "overloaded"   // 503/529 — provider overloaded, backoff
	FailoverServerError FailoverReason = "server_error" // 500/502 — internal error, retry

	// Transport
	FailoverTimeout FailoverReason = "timeout" // Connection/read timeout — rebuild + retry

	// Context / payload
	FailoverContextOverflow FailoverReason = "context_overflow"  // Context too large — compress
	FailoverPayloadTooLarge FailoverReason = "payload_too_large" // 413 — compress payload
	FailoverImageTooLarge   FailoverReason = "image_too_large"   // Image exceeds limit — shrink

	// Model / provider policy
	FailoverModelNotFound         FailoverReason = "model_not_found"         // 404 or invalid model
	FailoverProviderPolicyBlocked FailoverReason = "provider_policy_blocked" // Policy blocked
	FailoverContentPolicyBlocked  FailoverReason = "content_policy_blocked"  // Safety filter

	// Request format
	FailoverFormatError             FailoverReason = "format_error"              // 400 bad request
	FailoverInvalidEncryptedContent FailoverReason = "invalid_encrypted_content" // Replay blob rejected

	// Catch-all
	FailoverUnknown FailoverReason = "unknown" // Unclassifiable — retry with backoff
)

// ClassifiedError contains structured error classification with recovery hints.
type ClassifiedError struct {
	Reason       FailoverReason
	StatusCode   int
	Provider     string
	Model        string
	Message      string
	ErrorContext map[string]interface{}

	// Recovery action hints
	Retryable              bool
	ShouldCompress         bool
	ShouldRotateCredential bool
	ShouldFallback         bool
	ShouldAbort            bool
}

// IsAuth returns true if this is an authentication error.
func (ce *ClassifiedError) IsAuth() bool {
	return ce.Reason == FailoverAuth || ce.Reason == FailoverAuthPermanent
}

// IsRetryable returns true if this error is retryable.
func (ce *ClassifiedError) IsRetryable() bool {
	return ce.Retryable && !ce.ShouldAbort
}

// IsTransient returns true if this failure is transient (timeouts, rate limits,
// server-side 5xx, provider overload). Transient failures must NOT be encoded
// as permanent skill or reflection lessons — they reflect environment conditions,
// not a flawed approach. This closes the Hermes Agent Issue #6051 pattern where
// transient failures were being captured as permanent "this tool doesn't work"
// lessons. Callers that gate skill creation / blocker recording on this method
// avoid poisoning procedural memory with ephemeral noise.
func (ce *ClassifiedError) IsTransient() bool {
	if ce == nil {
		return false
	}
	switch ce.Reason {
	case FailoverTimeout,
		FailoverRateLimit,
		FailoverOverloaded,
		FailoverServerError:
		return true
	default:
		return false
	}
}

// String returns a human-readable description.
func (ce *ClassifiedError) String() string {
	return fmt.Sprintf("[%s] %s (retryable=%v, compress=%v, fallback=%v)",
		ce.Reason, ce.Message, ce.Retryable, ce.ShouldCompress, ce.ShouldFallback)
}

// Error patterns for classification
var (
	// Billing exhaustion patterns
	billingPatterns = []string{
		"insufficient credits", "insufficient_quota", "insufficient balance",
		"credit balance", "credits exhausted", "no usable credits",
		"top up your credits", "payment required", "billing hard limit",
		"exceeded your current quota", "account is deactivated",
		"plan does not include", "out of funds", "run out of funds",
		"balance_depleted", "not available on the free tier",
		// Alternative word orders used by some providers (e.g. Anthropic).
		// Without these the error falls through to FailoverUnknown, the
		// Retryable=false / Abort path never fires, and the agent burns
		// turns until the repeated-failure detector escalates — producing
		// the confusing "reason=unknown" wrapper the user sees.
		"balance is insufficient", "balance has been depleted",
		"quota has been exceeded", "credit limit reached",
		"billing quota exceeded",
	}

	// Rate limit patterns
	rateLimitPatterns = []string{
		"rate limit", "rate_limit", "too many requests", "throttled",
		"requests per minute", "tokens per minute", "requests per day",
		"try again in", "please retry after", "resource_exhausted",
		"rate increased too quickly", "throttlingexception",
		"too many concurrent requests", "servicequotaexceededexception",
	}

	// Timeout patterns
	timeoutPatterns = []string{
		"timeout", "timed out", "deadline exceeded", "connection timed out",
		"read timeout", "write timeout", "context deadline exceeded",
	}

	// Context overflow patterns
	contextOverflowPatterns = []string{
		"context length exceeded", "maximum context length", "token limit",
		"context too long", "too many tokens", "maximum token",
		"input length", "sequence length", "context window",
	}

	// Payload too large patterns
	payloadTooLargePatterns = []string{
		"request entity too large", "payload too large", "error code: 413",
		"413 request entity too large",
	}

	// Model not found patterns
	modelNotFoundPatterns = []string{
		"model not found", "invalid model", "model does not exist",
		"unknown model", "model_not_found", "no such model",
	}

	// Content policy patterns
	contentPolicyPatterns = []string{
		"content policy", "safety filter", "moderation",
		"content blocked", "policy violation", "harmful content",
	}

	// Authentication patterns
	authPatterns = []string{
		"authentication", "unauthorized", "invalid api key",
		"invalid token", "api key invalid", "auth failed",
	}

	// Server error patterns
	serverErrorPatterns = []string{
		"internal server error", "server error", "bad gateway",
		"service unavailable", "gateway timeout", "temporary error",
	}

	// Format error patterns
	formatErrorPatterns = []string{
		"bad request", "invalid request", "malformed",
		"validation failed", "schema error", "parse error",
		// Zhipu / other Chinese providers use explicit numeric codes for
		// permanent bad-request pathologies. 1214 is "messages parameter
		// invalid" (role alternation violated / orphan tool messages /
		// truncated message pairs), 1261 is "invalid parameter", etc.
		// Without these the classifier falls through to FailoverUnknown,
		// Retryable=true, so the agent retries a permanently broken message
		// payload until the escalation detector fires with "reason=unknown".
		// English permanent format errors.
		"messages parameter", "invalid messages", "invalid parameter",
		"bad request: messages", "messages must be",
		"missing role", "invalid role",
		// Chinese permanent format errors used by Zhipu, Aliyun, etc.
		"参数非法", "参数无效", "参数格式错误", "参数不合法",
		"messages 参数非法", "messages参数非法",
		"[1214]", "错误码 1214", "api error 1214",
		"[1261]", "错误码 1261", "api error 1261",
	}
)

// ClassifyError analyzes an error and returns a structured classification.
func ClassifyError(err error, statusCode int, provider, model string) *ClassifiedError {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	ce := &ClassifiedError{
		StatusCode:   statusCode,
		Provider:     provider,
		Model:        model,
		Message:      err.Error(),
		ErrorContext: make(map[string]interface{}),
		Retryable:    true,
	}

	// Check status code first
	switch statusCode {
	case 401, 403:
		ce.Reason = FailoverAuth
		ce.ShouldRotateCredential = true
		return ce
	case 402:
		ce.Reason = FailoverBilling
		ce.ShouldRotateCredential = true
		ce.Retryable = false
		return ce
	case 404:
		ce.Reason = FailoverModelNotFound
		ce.ShouldFallback = true
		ce.Retryable = false
		return ce
	case 413:
		ce.Reason = FailoverPayloadTooLarge
		ce.ShouldCompress = true
		return ce
	case 429:
		ce.Reason = FailoverRateLimit
		ce.ShouldRotateCredential = true
		return ce
	case 500, 502:
		ce.Reason = FailoverServerError
		return ce
	case 503, 529:
		ce.Reason = FailoverOverloaded
		return ce
	case 400:
		ce.Reason = FailoverFormatError
		ce.Retryable = false
		return ce
	}

	// Pattern matching for message-based classification
	if matchesAny(msg, billingPatterns) {
		ce.Reason = FailoverBilling
		ce.ShouldRotateCredential = true
		ce.Retryable = false
		return ce
	}

	if matchesAny(msg, rateLimitPatterns) {
		ce.Reason = FailoverRateLimit
		ce.ShouldRotateCredential = true
		return ce
	}

	if matchesAny(msg, timeoutPatterns) {
		ce.Reason = FailoverTimeout
		return ce
	}

	if matchesAny(msg, contextOverflowPatterns) {
		ce.Reason = FailoverContextOverflow
		ce.ShouldCompress = true
		ce.Retryable = false
		return ce
	}

	if matchesAny(msg, payloadTooLargePatterns) {
		ce.Reason = FailoverPayloadTooLarge
		ce.ShouldCompress = true
		return ce
	}

	if matchesAny(msg, modelNotFoundPatterns) {
		ce.Reason = FailoverModelNotFound
		ce.ShouldFallback = true
		ce.Retryable = false
		return ce
	}

	if matchesAny(msg, contentPolicyPatterns) {
		ce.Reason = FailoverContentPolicyBlocked
		ce.Retryable = false
		ce.ShouldAbort = true
		return ce
	}

	if matchesAny(msg, authPatterns) {
		ce.Reason = FailoverAuth
		ce.ShouldRotateCredential = true
		return ce
	}

	if matchesAny(msg, serverErrorPatterns) {
		ce.Reason = FailoverServerError
		return ce
	}

	if matchesAny(msg, formatErrorPatterns) {
		ce.Reason = FailoverFormatError
		ce.Retryable = false
		return ce
	}

	// Default: unknown but retryable
	ce.Reason = FailoverUnknown
	return ce
}

// matchesAny checks if str contains any of the patterns.
func matchesAny(str string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(str, p) {
			return true
		}
	}
	return false
}

// RecoveryStrategy defines how to recover from a classified error.
type RecoveryStrategy struct {
	Action        string
	Delay         time.Duration
	MaxRetries    int
	FallbackModel string
	Compress      bool
	Abort         bool
}

// GetRecoveryStrategy returns the recommended recovery strategy for a classified error.
func GetRecoveryStrategy(ce *ClassifiedError, attempt int) RecoveryStrategy {
	if ce == nil {
		return RecoveryStrategy{Action: "continue"}
	}

	// Calculate jittered backoff delay
	delay := JitteredBackoff(attempt)

	switch ce.Reason {
	case FailoverAuth:
		return RecoveryStrategy{
			Action:     "retry_with_rotation",
			Delay:      delay,
			MaxRetries: 2,
		}
	case FailoverAuthPermanent:
		return RecoveryStrategy{Action: "abort", Abort: true}
	case FailoverBilling:
		// Billing / quota exhaustion is permanent for the current credential
		// (the same key will keep returning 402 until the account is topped
		// up). The rotate_provider action referenced in the comment above is
		// not currently implemented in the agent loop, so returning it here
		// would silently spin until the repeated-failure detector escalates.
		// Aborting immediately surfaces the raw 402 / "insufficient balance"
		// message to the user on the VERY FIRST failed call of every new
		// request, so re-typing a prompt no longer looks like "the app is
		// stuck repeating the same escalation wrapper".
		return RecoveryStrategy{Action: "abort", Abort: true}
	case FailoverRateLimit:
		return RecoveryStrategy{
			Action:     "backoff",
			Delay:      delay * 2, // Longer delay for rate limits
			MaxRetries: 5,
		}
	case FailoverOverloaded:
		return RecoveryStrategy{
			Action:     "backoff",
			Delay:      delay * 3,
			MaxRetries: 3,
		}
	case FailoverServerError:
		return RecoveryStrategy{
			Action:     "retry",
			Delay:      delay,
			MaxRetries: 3,
		}
	case FailoverTimeout:
		return RecoveryStrategy{
			Action:     "retry_with_rebuild",
			Delay:      delay,
			MaxRetries: 3,
		}
	case FailoverContextOverflow:
		return RecoveryStrategy{
			Action:   "compress",
			Compress: true,
			Delay:    0,
		}
	case FailoverPayloadTooLarge:
		return RecoveryStrategy{
			Action:   "compress_payload",
			Compress: true,
			Delay:    0,
		}
	case FailoverModelNotFound:
		return RecoveryStrategy{
			Action:        "fallback_model",
			FallbackModel: "gpt-4o-mini", // Default fallback
			Abort:         false,
		}
	case FailoverContentPolicyBlocked:
		return RecoveryStrategy{Action: "abort", Abort: true}
	case FailoverFormatError:
		// Format errors are permanent for the same payload: e.g. Zhipu 1214
		// "messages 参数非法" means the message sequence violated role
		// alternation rules, left an orphan tool/tool_call pair after a
		// mid-conversation truncate/sanitize, or sent an empty role.
		// Retrying "with sanitization" once sounds helpful but in practice
		// the one re-send still fails identically (the next turn still has
		// the same malformed history), burns 1 API call, and lets the
		// repeated-failure detector wrap the underlying error inside a
		// confusing "reason=unknown" escalation wrapper after N turns.
		// Aborting immediately surfaces the raw 400 / "[1214] messages
		// 参数非法" to the user on the VERY FIRST failed call of every new
		// request, which makes it much easier to diagnose a history bug vs.
		// a flaky provider issue. The sanitizeHistory path on the NEXT
		// constructRequest call is the right place to fix malformed history
		// — not a post-failure one-shot retry with zero visibility.
		return RecoveryStrategy{Action: "abort", Abort: true}
	default:
		return RecoveryStrategy{
			Action:     "retry",
			Delay:      delay,
			MaxRetries: 3,
		}
	}
}

// Classifier is a reusable error classifier with custom rules.
type Classifier struct {
	customRules []func(error, int, string, string) *ClassifiedError
}

// NewClassifier creates a new classifier.
func NewClassifier() *Classifier {
	return &Classifier{
		customRules: make([]func(error, int, string, string) *ClassifiedError, 0),
	}
}

// AddRule adds a custom classification rule.
func (c *Classifier) AddRule(rule func(error, int, string, string) *ClassifiedError) {
	c.customRules = append(c.customRules, rule)
}

// Classify runs all rules and returns the first match, or falls back to default.
func (c *Classifier) Classify(err error, statusCode int, provider, model string) *ClassifiedError {
	for _, rule := range c.customRules {
		if result := rule(err, statusCode, provider, model); result != nil {
			return result
		}
	}
	return ClassifyError(err, statusCode, provider, model)
}

// Context-aware classification
func ClassifyWithContext(ctx context.Context, err error, statusCode int, provider, model string) *ClassifiedError {
	ce := ClassifyError(err, statusCode, provider, model)
	if ce == nil {
		return nil
	}

	// Check if context was cancelled
	if ctx.Err() != nil {
		ce.ErrorContext["context_cancelled"] = true
		ce.ErrorContext["context_error"] = ctx.Err().Error()
		if ctx.Err() == context.DeadlineExceeded {
			ce.Reason = FailoverTimeout
		}
	}

	return ce
}
