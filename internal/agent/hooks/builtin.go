package hooks

import (
	"context"
	"regexp"
	"time"

	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// PrivacyHook provides PII detection and redaction
type PrivacyHook struct {
	redactor *privacy.Redactor
}

// NewPrivacyHook creates a new privacy hook
func NewPrivacyHook() *PrivacyHook {
	cfg := privacy.DefaultConfig()
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

	// Redact messages
	redactedMessages := make([]provider.Message, len(req.Messages))
	for i, msg := range req.Messages {
		redactedMessages[i] = provider.Message{
			Role:    msg.Role,
			Content: h.redactor.Redact(msg.Content),
		}
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

// BeforeTool redacts PII from tool arguments before execution
func (h *PrivacyHook) BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error) {
	if call == nil {
		return nil, HookDecision{Action: HookActionContinue}, nil
	}

	// Redact tool arguments
	if call.ToolArgs != nil {
		redacted := make(map[string]interface{})
		for k, v := range call.ToolArgs {
			if strVal, ok := v.(string); ok {
				redacted[k] = h.redactor.Redact(strVal)
			} else {
				redacted[k] = v
			}
		}
		call.ToolArgs = redacted
	}

	return call, HookDecision{Action: HookActionContinue}, nil
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
