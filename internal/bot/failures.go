package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// FailureCode is a machine-readable reason code for one failed bot turn.
// Mirrors Hermes' typed failure reasons so the web UI and callers can branch
// on the cause instead of scraping free-text error strings.
type FailureCode string

const (
	FailureUnknown           FailureCode = "unknown_error"
	FailureProviderQuota     FailureCode = "provider_quota_limit"
	FailureProviderRateLimit FailureCode = "provider_rate_limit"
	FailureContextOverflow   FailureCode = "context_overflow"
	FailureRuntimeOffline    FailureCode = "runtime_offline"
	FailureQueuedExpired     FailureCode = "queued_expired"
	FailureDeliveryTimeout   FailureCode = "delivery_timeout"
	FailureMissingConfig     FailureCode = "missing_config"
	FailureAuth              FailureCode = "auth_error"
	FailureTurnTimeout       FailureCode = "turn_timeout"
	FailureCanceled          FailureCode = "canceled"
	FailureToolError         FailureCode = "tool_error"
	FailureContentPolicy     FailureCode = "content_policy"
)

// failureClass describes how a failure should be handled by the worker.
type failureClass struct {
	Code      FailureCode
	Transient bool // safe to retry automatically
	Retryable bool // automatic retry will be attempted (once)
}

// classifyFailure maps an arbitrary turn error to a typed reason code plus
// retry hints. Classification is string/status-code based because providers
// surface errors as wrapped fmt errors; the rules below cover OpenAI-compatible
// providers (LongCat, DeepSeek, GLM, Qwen, etc.) and transport errors.
func classifyFailure(err error) failureClass {
	if err == nil {
		return failureClass{Code: FailureUnknown}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failureClass{Code: FailureTurnTimeout, Transient: true, Retryable: true}
	}
	if errors.Is(err, context.Canceled) {
		return failureClass{Code: FailureCanceled}
	}

	msg := strings.ToLower(err.Error())

	// Content-policy rejections (safety/moderation filters, harmful content,
	// etc.) — never retry. Checked before auth because some providers pair a
	// policy block with HTTP 403. Mirrors the agent-side retry classifier's
	// contentPolicyPatterns so both layers report the same reason.
	if strings.Contains(msg, "content_policy_error") ||
		strings.Contains(msg, "content policy") ||
		strings.Contains(msg, "safety filter") ||
		strings.Contains(msg, "moderation") ||
		strings.Contains(msg, "content blocked") ||
		strings.Contains(msg, "policy violation") ||
		strings.Contains(msg, "harmful content") {
		return failureClass{Code: FailureContentPolicy}
	}

	// Auth / credentials — never retry.
	if strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "api key") {
		return failureClass{Code: FailureAuth}
	}

	// Quota / billing — never retry (retrying burns money against the same cap).
	if strings.Contains(msg, "quota") ||
		strings.Contains(msg, "insufficient") ||
		strings.Contains(msg, "balance") ||
		strings.Contains(msg, "402") {
		return failureClass{Code: FailureProviderQuota}
	}

	// Rate limiting — transient, retry once.
	if strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") {
		return failureClass{Code: FailureProviderRateLimit, Transient: true, Retryable: true}
	}

	// Context overflow — retryable after history compaction.
	if strings.Contains(msg, "context length") ||
		strings.Contains(msg, "context window") ||
		strings.Contains(msg, "maximum context") ||
		strings.Contains(msg, "context_overflow") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "token limit") ||
		strings.Contains(msg, "max tokens") ||
		strings.Contains(msg, "maximum tokens") {
		return failureClass{Code: FailureContextOverflow, Transient: true, Retryable: true}
	}

	// Timeouts (non-context): delivery/network timeouts — transient, retry once.
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "deadline exceeded") {
		return failureClass{Code: FailureDeliveryTimeout, Transient: true, Retryable: true}
	}

	// Transport / server-side 5xx — transient, retry once.
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "offline") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "504") ||
		strings.Contains(msg, "server error") ||
		strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "internal error") {
		return failureClass{Code: FailureRuntimeOffline, Transient: true, Retryable: true}
	}

	// Missing provider/config wiring.
	if strings.Contains(msg, "not configured") ||
		strings.Contains(msg, "missing config") ||
		strings.Contains(msg, "unknown provider") {
		return failureClass{Code: FailureMissingConfig}
	}

	// Tool execution errors.
	if strings.Contains(msg, "tool") && (strings.Contains(msg, "exec") || strings.Contains(msg, "failed")) {
		return failureClass{Code: FailureToolError}
	}

	return failureClass{Code: FailureUnknown}
}

// TurnError wraps a failed turn with its machine-readable failure code.
// processMessage returns this so synchronous callers (web API, CLI) can
// branch on the typed reason instead of scraping error text.
type TurnError struct {
	Err  error
	Code FailureCode
}

func (e *TurnError) Error() string {
	if e.Err == nil {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *TurnError) Unwrap() error { return e.Err }

// turnFailureReplyCoded converts a turn failure into a user-facing message
// plus its machine-readable code (so callers can branch on the reason).
func turnFailureReplyCoded(err error, turnTimeout time.Duration) (string, FailureCode) {
	cls := classifyFailure(err)
	switch cls.Code {
	case FailureTurnTimeout:
		return fmt.Sprintf("(⏱ 单轮对话超时:本回合耗时超过上限 %s,已中止执行。已完成的部分已保留,发送「继续」可在新回合接着处理。)", turnTimeout), cls.Code
	case FailureCanceled:
		return "(⏹ 本回合已被取消,已完成的部分已保留。)", cls.Code
	case FailureProviderRateLimit:
		return "(⏳ 模型服务触发限流(429),请稍后再试。)", cls.Code
	case FailureProviderQuota:
		return "(💰 模型服务额度不足,请检查账户余额/配额后重试。)", cls.Code
	case FailureContextOverflow:
		return "(📏 上下文超出模型限制,已自动压缩历史后重试仍失败。)", cls.Code
	case FailureRuntimeOffline:
		return "(📡 模型服务暂时不可用,请稍后再试。)", cls.Code
	case FailureAuth:
		return "(🔑 模型服务鉴权失败,请检查 API Key/Provider 配置。)", cls.Code
	case FailureContentPolicy:
		return "(🚫 内容被模型安全策略拦截,请调整表述后重试。)", cls.Code
	case FailureDeliveryTimeout:
		return "(⏱ 请求超时未收到响应,请稍后再试。)", cls.Code
	case FailureMissingConfig:
		return "(⚙ 缺少必要的配置,请检查 provider/model 设置。)", cls.Code
	default:
		return fmt.Sprintf("(error: %v)", err), cls.Code
	}
}

// turnFailureReply keeps the legacy text-only signature for callers that only
// need the human-facing string (tests, TUI). The coded variant is used by the
// worker so results carry a machine-readable reason.
func turnFailureReply(err error, turnTimeout time.Duration) string {
	reply, _ := turnFailureReplyCoded(err, turnTimeout)
	return reply
}

// compactHistory drops the oldest complete turns (user-turn boundaries) from
// provider-message history, keeping the leading system prompt(s) and at most
// half of the remaining messages. Used to recover from context overflow.
func compactHistory(history []provider.Message) []provider.Message {
	if len(history) <= 8 {
		return history
	}
	start := 0
	for start < len(history) && history[start].Role == "system" {
		start++
	}
	if start >= len(history) {
		return history
	}
	keep := (len(history) - start) / 2
	if keep < 4 {
		keep = 4
	}
	// Cut back to a user-turn boundary so tool pairs stay intact.
	end := len(history) - keep
	for end > start && history[end].Role != "user" {
		end++
	}
	if end >= len(history)-1 {
		return history[:start+keep] // give up on boundary alignment, keep tail
	}
	out := make([]provider.Message, 0, len(history)-end)
	out = append(out, history[:start]...) // system prompts
	out = append(out, history[end:]...)   // tail turns
	return out
}
