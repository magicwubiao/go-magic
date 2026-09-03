package hooks

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// PrivacyHook provides PII detection and redaction
type PrivacyHook struct {
	redactor *privacy.Redactor
}

// urlLikeKeyPattern 匹配工具参数名中暗示是 URL/链接/地址 的键
// 命中此类键的 string 值不脱敏，避免把 URL 里的数字 ID（视频 ID、订单号等）替换为占位符
var urlLikeKeyPattern = regexp.MustCompile(`(?i)(^|_)(url|link|href|src|endpoint|address|uri|website|web|page|path)(_|\b)`)

// urlPrefixPattern 检测值是否以 http/https/file/ftp 等协议头开头
var urlPrefixPattern = regexp.MustCompile(`(?i)^(https?|file|ftp|ws|wss)://`)

// NewPrivacyHook creates a new privacy hook.
// cfg 为 nil 时使用 DefaultConfig（保持向后兼容）。
func NewPrivacyHook(cfg *privacy.Config) *PrivacyHook {
	if cfg == nil {
		cfg = privacy.DefaultConfig()
	}
	return &PrivacyHook{
		redactor: privacy.NewRedactor(cfg),
	}
}

func (h *PrivacyHook) Name() string {
	return "privacy"
}

// BeforeLLM redacts PII from messages before sending to LLM
func (h *PrivacyHook) BeforeLLM(ctx context.Context, req *LLMHookRequest) (*LLMHookRequest, HookDecision, error) {
	if req == nil {
		return nil, HookDecision{Action: HookActionContinue}, nil
	}

	// Redact messages (preserve all fields like ToolCalls and ToolCallID)
	redactedMessages := make([]provider.Message, len(req.Messages))
	for i, msg := range req.Messages {
		redactedMessages[i] = msg                                    // copy all fields first
		redactedMessages[i].Content = h.redactor.Redact(msg.Content) // then redact content
	}

	return &LLMHookRequest{
		Provider: req.Provider,
		Model:    req.Model,
		Messages: redactedMessages,
		Tools:    req.Tools,
	}, HookDecision{Action: HookActionContinue}, nil
}

// AfterLLM passes through the response without modification
func (h *PrivacyHook) AfterLLM(ctx context.Context, resp *LLMHookResponse) (*LLMHookResponse, HookDecision, error) {
	return resp, HookDecision{Action: HookActionContinue}, nil
}

// BeforeTool redacts PII from tool arguments before execution.
// 对 URL/链接类参数（键名或值暗示是 URL）豁免脱敏，避免破坏带数字 ID 的链接。
func (h *PrivacyHook) BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error) {
	if call == nil {
		return nil, HookDecision{Action: HookActionContinue}, nil
	}

	// Redact tool arguments
	if call.ToolArgs != nil {
		redacted := make(map[string]interface{})
		for k, v := range call.ToolArgs {
			if strVal, ok := v.(string); ok {
				// URL 类参数豁免：键名暗示是 URL，或值本身是 URL
				if isURLLike(k, strVal) {
					redacted[k] = strVal
				} else {
					redacted[k] = h.redactor.Redact(strVal)
				}
			} else {
				redacted[k] = v
			}
		}
		call.ToolArgs = redacted
	}

	return call, HookDecision{Action: HookActionContinue}, nil
}

// isURLLike 判断参数是否为 URL 类（不应脱敏）
func isURLLike(key, value string) bool {
	if urlLikeKeyPattern.MatchString(key) {
		return true
	}
	trimmed := strings.TrimSpace(value)
	if urlPrefixPattern.MatchString(trimmed) {
		return true
	}
	return false
}

// AfterTool passes through the result without modification
func (h *PrivacyHook) AfterTool(ctx context.Context, result *ToolResultHookResponse) (*ToolResultHookResponse, HookDecision, error) {
	return result, HookDecision{Action: HookActionContinue}, nil
}

// MessageFilterHook filters messages based on content
type MessageFilterHook struct {
	blockedPatterns []*regexp.Regexp
	warnPatterns    []*regexp.Regexp
}

// NewMessageFilterHook creates a new message filter hook
func NewMessageFilterHook() *MessageFilterHook {
	return &MessageFilterHook{
		blockedPatterns: make([]*regexp.Regexp, 0),
		warnPatterns:    make([]*regexp.Regexp, 0),
	}
}

func (h *MessageFilterHook) Name() string {
	return "message_filter"
}

// AddBlockedPattern adds a pattern that should be blocked
func (h *MessageFilterHook) AddBlockedPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	h.blockedPatterns = append(h.blockedPatterns, re)
	return nil
}

// AddWarnPattern adds a pattern that should trigger a warning
func (h *MessageFilterHook) AddWarnPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	h.warnPatterns = append(h.warnPatterns, re)
	return nil
}

// BeforeLLM checks messages for blocked content
func (h *MessageFilterHook) BeforeLLM(ctx context.Context, req *LLMHookRequest) (*LLMHookRequest, HookDecision, error) {
	if req == nil {
		return nil, HookDecision{Action: HookActionContinue}, nil
	}

	for _, msg := range req.Messages {
		for _, pattern := range h.blockedPatterns {
			if pattern.MatchString(msg.Content) {
				return req, HookDecision{
					Action: HookActionReject,
					Reason: "content blocked by policy",
				}, nil
			}
		}
	}

	return req, HookDecision{Action: HookActionContinue}, nil
}

// AfterLLM passes through the response
func (h *MessageFilterHook) AfterLLM(ctx context.Context, resp *LLMHookResponse) (*LLMHookResponse, HookDecision, error) {
	return resp, HookDecision{Action: HookActionContinue}, nil
}

// BeforeTool passes through tool calls
func (h *MessageFilterHook) BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error) {
	return call, HookDecision{Action: HookActionContinue}, nil
}

// AfterTool passes through results
func (h *MessageFilterHook) AfterTool(ctx context.Context, result *ToolResultHookResponse) (*ToolResultHookResponse, HookDecision, error) {
	return result, HookDecision{Action: HookActionContinue}, nil
}

// RateLimitHook limits the rate of LLM calls
type RateLimitHook struct {
	maxRequestsPerMinute int
	requestCounts        map[string]int
	lastReset            time.Time
}

// NewRateLimitHook creates a new rate limit hook
func NewRateLimitHook(maxPerMinute int) *RateLimitHook {
	return &RateLimitHook{
		maxRequestsPerMinute: maxPerMinute,
		requestCounts:        make(map[string]int),
		lastReset:            time.Now(),
	}
}

func (h *RateLimitHook) Name() string {
	return "rate_limit"
}

// BeforeLLM checks rate limits
func (h *RateLimitHook) BeforeLLM(ctx context.Context, req *LLMHookRequest) (*LLMHookRequest, HookDecision, error) {
	now := time.Now()

	// Reset counter every minute
	if now.Sub(h.lastReset) >= time.Minute {
		h.requestCounts = make(map[string]int)
		h.lastReset = now
	}

	key := req.Provider + "/" + req.Model
	count := h.requestCounts[key] + 1

	if count > h.maxRequestsPerMinute {
		return req, HookDecision{
			Action: HookActionStop,
			Reason: "rate limit exceeded",
		}, nil
	}

	h.requestCounts[key] = count
	return req, HookDecision{Action: HookActionContinue}, nil
}

// AfterLLM passes through the response
func (h *RateLimitHook) AfterLLM(ctx context.Context, resp *LLMHookResponse) (*LLMHookResponse, HookDecision, error) {
	return resp, HookDecision{Action: HookActionContinue}, nil
}

// BeforeTool passes through
func (h *RateLimitHook) BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error) {
	return call, HookDecision{Action: HookActionContinue}, nil
}

// AfterTool passes through
func (h *RateLimitHook) AfterTool(ctx context.Context, result *ToolResultHookResponse) (*ToolResultHookResponse, HookDecision, error) {
	return result, HookDecision{Action: HookActionContinue}, nil
}
