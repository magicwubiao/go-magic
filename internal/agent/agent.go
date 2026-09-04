package agent

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/redact"
	"github.com/magicwubiao/go-magic/pkg/types"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

// Tool dependency graph - tools that must run alone (cannot parallelize with same tool or related)
var (
	// Tools that should not run in parallel (to avoid conflicts)
	exclusiveTools = map[string]bool{
		"write_file":      true,
		"execute_command": true,
	}

	// Tools that need sequential execution
	sequentialTools = map[string]bool{
		"read_file":       true,
		"write_file":      true,
		"execute_command": true,
	}
)

// toolGroup defines a group of tools for execution
type toolGroup struct {
	tools      []types.ToolCall
	sequential bool
}

// ToolCallResult holds the result of a tool execution
type ToolCallResult struct {
	ID        string
	Name      string
	Content   string
	Err       error
	Execution time.Duration
}

// Agent handles AI conversation with tool execution
type Agent struct {
	provider    provider.Provider
	registry    ToolRegistry
	tools       []map[string]interface{} // tools schema for provider
	history     []provider.Message
	maxTurns    int
	maxTotalLen int // max chars in message history
	maxMsgLen   int // max chars per message
	// Steering settings
	maxIterations  int
	maxTokenBudget int64

	// Tool call loop detection
	toolCallHistory  []string       // 记录已调用的工具名
	toolCallCount    map[string]int // 每个工具被调用的次数
	sameToolLimit    int            // 同一工具最大调用次数
	consecutiveLimit int            // 连续 tool call 最大次数

	// Memory integration
	memoryEnabled bool

	// Cortex Agent six-system integration
	cortexManager *cortex.Manager

	// Sub-task delegation (auto-decompose complex tasks)
	subTaskEnabled bool

	// Hooks system
	hooks *hooks.HookManager

	// Event bus for observability
	bus *bus.EventBus

	// Session tracking
	session        string
	iterationCount int

	// Compression settings
	compressionEnabled bool
	compressionRatio   float64
	maxMemoryTokens    int

	// Token usage tracking
	tokenUsage      int64
	inputTokens     int
	outputTokens    int
	cacheReadTokens int

	// Secret redaction (default true)
	secretRedaction bool
}

// ToolRegistry interface for tool execution
type ToolRegistry interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error)
}

// AgentOption configures the agent
type AgentOption func(*Agent)

// SteeringConfig holds steering configuration
type SteeringConfig struct {
	MaxIterations  int
	MaxTokenBudget int64
}

// NewAIAgent creates a new AI agent
func NewAIAgent(prov provider.Provider, registry ToolRegistry, tools []map[string]interface{}, systemPrompt string) *Agent {
	history := make([]provider.Message, 0)
	if systemPrompt != "" {
		history = append(history, provider.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	agent := &Agent{
		provider:         prov,
		registry:         registry,
		tools:            tools,
		history:          history,
		maxTurns:         60,
		maxIterations:    80,
		maxTotalLen:      200000, // 200K chars max history (~50K tokens)
		maxMsgLen:        50000,  // 50K chars per message (~12K tokens)
		maxTokenBudget:   0,
		sameToolLimit:    3,
		consecutiveLimit: 10,
		toolCallCount:    make(map[string]int),
		subTaskEnabled:   true,
		hooks:            hooks.NewHookManager(),
		bus:              bus.NewEventBus(),
	}

	agent.registerBuiltinHooks()

	return agent
}

// NewEnhancedAgent creates an agent with enhanced features
func NewEnhancedAgent(prov provider.Provider, registry ToolRegistry, tools []map[string]interface{}, systemPrompt string, opts ...AgentOption) *Agent {
	agent := NewAIAgent(prov, registry, tools, systemPrompt)

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

// WithSteering configures steering settings
func WithSteering(cfg SteeringConfig) AgentOption {
	return func(a *Agent) {
		if cfg.MaxIterations > 0 {
			a.maxIterations = cfg.MaxIterations
		}
		if cfg.MaxTokenBudget > 0 {
			a.maxTokenBudget = cfg.MaxTokenBudget
		}
	}
}

// WithLoopLimits configures loop detection limits
func WithLoopLimits(sameToolLimit, consecutiveLimit int) AgentOption {
	return func(a *Agent) {
		if sameToolLimit > 0 {
			a.sameToolLimit = sameToolLimit
		}
		if consecutiveLimit > 0 {
			a.consecutiveLimit = consecutiveLimit
		}
	}
}

// WithHooks registers hooks
func WithHooks(hookRegs ...hooks.HookRegistration) AgentOption {
	return func(a *Agent) {
		for _, reg := range hookRegs {
			a.hooks.Register(reg)
		}
	}
}

// WithEventBus sets a custom event bus
func WithEventBus(eventBus *bus.EventBus) AgentOption {
	return func(a *Agent) {
		a.bus = eventBus
	}
}

// WithMemory enables memory integration
func WithMemory(enabled bool) AgentOption {
	return func(a *Agent) {
		a.memoryEnabled = enabled
	}
}

// WithCortex enables Cortex Agent six-system integration
func WithCortex(mgr *cortex.Manager) AgentOption {
	return func(a *Agent) {
		a.cortexManager = mgr
	}
}

// WithSubTask enables or disables automatic sub-task delegation
func WithSubTask(enabled bool) AgentOption {
	return func(a *Agent) {
		a.subTaskEnabled = enabled
	}
}

// WithSecretRedaction enables or disables secret redaction (API keys, tokens, etc.)
func WithSecretRedaction(enabled bool) AgentOption {
	return func(a *Agent) {
		a.secretRedaction = enabled
	}
}

func (a *Agent) registerBuiltinHooks() {
	// Privacy hook
	a.hooks.Register(hooks.HookRegistration{
		Name:   "privacy",
		Source: hooks.HookSourceBuiltIn,
		Hook:   hooks.NewPrivacyHook(),
	})
	// Smart approval hook
	a.hooks.Register(hooks.HookRegistration{
		Name:   "approval",
		Source: hooks.HookSourceBuiltIn,
		Hook:   NewApprovalHook(),
	})
}

// SetSession sets the session ID for event tracking
func (a *Agent) SetSession(session string) {
	a.session = session
}

// Emit emits an event to the event bus
func (a *Agent) Emit(kind bus.EventKind, data interface{}) {
	a.bus.Emit(bus.Event{
		Kind:      kind,
		Turn:      a.iterationCount,
		SessionID: a.session,
		Data:      data,
	})
}

// AddSkillsContext adds skills context to system prompt
func (a *Agent) AddSkillsContext(skillsCtx string) {
	if skillsCtx == "" {
		return
	}

	for i, msg := range a.history {
		if msg.Role == "system" {
			a.history[i].Content += "\n\n" + skillsCtx
			return
		}
	}

	a.history = append([]provider.Message{{
		Role:    "system",
		Content: skillsCtx,
	}}, a.history...)
}

// trySubTaskDelegation checks if the task is complex and delegates to sub-task executor.
// Returns true if delegated and handled, false if should proceed normally.
func (a *Agent) trySubTaskDelegation(ctx context.Context, input string) (bool, string, error) {
	if !a.subTaskEnabled {
		return false, "", nil
	}

	analyzer := NewComplexityAnalyzer()
	if !analyzer.ShouldDecompose(input) {
		return false, "", nil
	}

	// Task is complex - delegate to sub-task executor
	result, err := a.ExecuteComplexTask(ctx, input)
	if err != nil {
		// If delegation fails, fall through to normal conversation
		return false, "", nil
	}

	if result == "" {
		return false, "", nil
	}

	// Add the sub-task results to conversation history
	a.history = append(a.history,
		provider.Message{Role: "user", Content: input},
		provider.Message{Role: "assistant", Content: result + "\n\nBased on the sub-task results above, here is my complete response:"},
	)

	return true, result, nil
}

// RunConversationWithMedia runs a conversation with multimodal input support.
// If contentParts is provided, it takes priority over plain text input.
func (a *Agent) RunConversationWithMedia(ctx context.Context, input string, contentParts []types.ContentPart) (string, error) {
	// If cortex is enabled, use the full cortex integration path
	if a.cortexManager != nil {
		return a.RunWithCortex(ctx, input)
	}

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Skip sub-task delegation when contentParts are present (e.g., multimodal input with images)
	// This prevents losing the media content when delegating to sub-task executor
	if len(contentParts) == 0 {
		// Auto sub-task delegation for complex tasks
		if delegated, result, err := a.trySubTaskDelegation(ctx, input); delegated {
			if err != nil {
				return "", err
			}
			// Synthesis prompt to summarize sub-task results
			return a.RunConversation(ctx,
				fmt.Sprintf(`I have completed the sub-tasks for the task. The sub-tasks execution results are:

%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`, result))
		}
	}

	// Build user message - use content parts if available, otherwise fall back to plain text
	userMsg := provider.Message{
		Role: "user",
	}
	if len(contentParts) > 0 {
		userMsg.ContentParts = contentParts
	} else {
		userMsg.Content = utils.TruncateDetailed(input, a.maxMsgLen)
	}
	a.history = append(a.history, userMsg)

	// Truncate history to prevent overflow
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Cortex: OnTurnStart - freezes memory snapshot for prefix cache
		if a.cortexManager != nil {
			a.cortexManager.OnTurnStart()
		}

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			return "", fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			return "", fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
		}

		// Emit turn start event
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn": a.iterationCount,
		})

		// Build LLM request
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.history,
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, decision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		if decision.Action == hooks.HookActionStop {
			return "", fmt.Errorf("hook stopped: %s", decision.Reason)
		}
		if decision.Action == hooks.HookActionReject {
			return "", fmt.Errorf("hook rejected: %s", decision.Reason)
		}

		// Use ChatWithTools for OpenAI provider if tools are available
		var resp *provider.ChatResponse
		type openAIlike interface {
			ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
		}
		if oa, ok := a.provider.(openAIlike); ok && len(a.tools) > 0 {
			resp, err = oa.ChatWithTools(ctx, req.Messages, req.Tools)
		} else {
			resp, err = a.provider.Chat(ctx, req.Messages)
		}

		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindError, err.Error())
			continue
		}

		// Track token usage from the response
		if resp.Usage != nil {
			a.inputTokens += resp.Usage.PromptTokens
			a.outputTokens += resp.Usage.CompletionTokens
		}

		// Call AfterLLM hooks
		llmResp := &hooks.LLMHookResponse{
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		llmResp, decision, err = a.hooks.AfterLLM(ctx, llmResp)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// No tool calls - return the response
		if len(resp.ToolCalls) == 0 {
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			return content, nil
		}

		// Has tool calls - execute them
		toolResults, err := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if err != nil {
			lastErr = err
			continue
		}

		// Add assistant message and tool results to history
		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   llmResp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, result := range toolResults {
			var resultContent string
			if result.Err != nil {
				resultContent = fmt.Sprintf("Error: %v", result.Err)
			} else {
				resultContent = utils.TruncateDetailed(result.Content, a.maxMsgLen)
			}
			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: result.ID,
			})
		}

		// Check for tool call loops
		if len(a.toolCallHistory) > 0 && a.toolCallHistory[len(a.toolCallHistory)-1] == "unknown" {
			// Replace last if unknown
			a.toolCallHistory = a.toolCallHistory[:len(a.toolCallHistory)-1]
		}
		for _, tc := range resp.ToolCalls {
			name := tc.Function.Name
			if name == "" {
				name = tc.Name
			}
			a.toolCallHistory = append(a.toolCallHistory, name)
		}

		// Detect loops
		loopDetected := false
		loopReason := ""
		toolCounts := make(map[string]int)
		for _, name := range a.toolCallHistory {
			toolCounts[name]++
			if toolCounts[name] > a.sameToolLimit {
				loopDetected = true
				loopReason = fmt.Sprintf("tool %s called %d times", name, toolCounts[name])
				break
			}
		}

		// Check consecutive tool calls limit
		if len(a.toolCallHistory) >= a.consecutiveLimit {
			loopDetected = true
			if loopReason == "" {
				loopReason = fmt.Sprintf("%d consecutive tool calls", len(a.toolCallHistory))
			}
		}

		if loopDetected {
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: utils.TruncateDetailed(resp.Content, a.maxMsgLen),
			})
			a.history = append(a.history, provider.Message{
				Role:    "user",
				Content: "Please provide a final summary of what has been accomplished so far. Do not call any more tools.",
			})
			finalResp, finalErr := a.provider.Chat(ctx, a.history)
			if finalErr != nil {
				return "", fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}
			return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
		}
	}

	// Try to get a summary from the LLM before giving up
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: "You have reached the maximum number of turns. Please provide a brief summary of what you accomplished and what remains incomplete. Do NOT call any more tools.",
	})
	if finalResp, finalErr := a.provider.Chat(ctx, a.history); finalErr == nil && finalResp.Content != "" {
		a.history = append(a.history, provider.Message{
			Role:    "assistant",
			Content: utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
		})
		return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	recentTools := []string{}
	if len(a.toolCallHistory) > 0 {
		recentCount := len(a.toolCallHistory)
		start := 0
		if recentCount > 5 {
			start = recentCount - 5
		}
		recentTools = a.toolCallHistory[start:]
	}
	return "", fmt.Errorf("exceeded maximum turns (%d). Completed %d turns with %d tool calls. Recent tools: %v",
		a.maxTurns, a.iterationCount, len(a.toolCallHistory), recentTools)
}

// RunConversation runs a conversation with automatic tool execution
func (a *Agent) RunConversation(ctx context.Context, input string) (string, error) {
	// If cortex is enabled, use the full cortex integration path
	if a.cortexManager != nil {
		return a.RunWithCortex(ctx, input)
	}

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Auto sub-task delegation for complex tasks
	if delegated, result, err := a.trySubTaskDelegation(ctx, input); delegated {
		if err != nil {
			return "", err
		}
		// Synthesis prompt to summarize sub-task results
		return a.RunConversation(ctx,
			fmt.Sprintf(`I have completed the sub-tasks for the task. The sub-tasks execution results are:

%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`, result))
	}

	// Truncate input
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: utils.TruncateDetailed(input, a.maxMsgLen),
	})

	// Truncate history to prevent overflow
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Cortex: OnTurnStart - freezes memory snapshot for prefix cache
		if a.cortexManager != nil {
			a.cortexManager.OnTurnStart()
		}

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			return "", fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			return "", fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
		}

		// Emit turn start event
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn": a.iterationCount,
		})

		// Build LLM request
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.history,
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, decision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		if decision.Action == hooks.HookActionStop {
			return "", fmt.Errorf("hook stopped: %s", decision.Reason)
		}
		if decision.Action == hooks.HookActionReject {
			return "", fmt.Errorf("hook rejected: %s", decision.Reason)
		}

		// Use ChatWithTools for OpenAI provider if tools are available
		var resp *provider.ChatResponse
		type openAIlike interface {
			ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
		}
		if oa, ok := a.provider.(openAIlike); ok && len(a.tools) > 0 {
			resp, err = oa.ChatWithTools(ctx, req.Messages, req.Tools)
		} else {
			resp, err = a.provider.Chat(ctx, req.Messages)
		}

		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindError, err.Error())
			continue
		}

		// Call AfterLLM hooks
		llmResp := &hooks.LLMHookResponse{
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		llmResp, decision, err = a.hooks.AfterLLM(ctx, llmResp)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// No tool calls - return the response
		if len(resp.ToolCalls) == 0 {
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

			return redact.RedactIfEnabled(resp.Content, a.secretRedaction), nil
		}

		// Tool call loop detection - track tool calls more precisely
		for _, tc := range resp.ToolCalls {
			name := tc.Function.Name
			a.toolCallCount[name]++
			a.toolCallHistory = append(a.toolCallHistory, name)
		}

		// Check if same tool called too many times (with more context)
		loopDetected := false
		loopReason := ""
		for name, count := range a.toolCallCount {
			if count >= a.sameToolLimit {
				loopDetected = true
				loopReason = fmt.Sprintf("tool %s called %d times", name, count)
				break
			}
		}

		// Check consecutive tool calls limit
		if len(a.toolCallHistory) >= a.consecutiveLimit {
			loopDetected = true
			if loopReason == "" {
				loopReason = fmt.Sprintf("%d consecutive tool calls", len(a.toolCallHistory))
			}
		}

		if loopDetected {
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: utils.TruncateDetailed(resp.Content, a.maxMsgLen),
			})
			a.history = append(a.history, provider.Message{
				Role:    "user",
				Content: "Please provide a final summary of what has been accomplished so far. Do not call any more tools.",
			})
			finalResp, finalErr := a.provider.Chat(ctx, a.history)
			if finalErr != nil {
				return "", fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}
			return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
		}

		// Execute tools and add results to history
		toolResults, execErr := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if execErr != nil {
			lastErr = execErr
			a.Emit(bus.EventKindToolError, execErr.Error())
			// Continue to add messages - toolResults may contain partial results
		}

		// Add assistant message with tool_calls
		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   llmResp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Add tool results - ensure every tool_call has a corresponding tool message
		for _, tc := range resp.ToolCalls {
			tcID := tc.ID
			if tcID == "" {
				tcID = "call_unknown"
			}

			var resultContent string
			if result, ok := toolResults[tcID]; ok {
				if result.Err != nil {
					resultContent = fmt.Sprintf("Error: %v", result.Err)
				} else {
					resultContent = utils.TruncateDetailed(result.Content, a.maxMsgLen)
				}
			} else {
				// No result found for this tool call
				if execErr != nil {
					resultContent = fmt.Sprintf("Error: %v", execErr)
				} else {
					resultContent = "Error: No result returned for tool call"
				}
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: tcID,
			})
		}
	}

	// Try to get a summary from the LLM before giving up
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: "You have reached the maximum number of turns. Please provide a brief summary of what you accomplished and what remains incomplete. Do NOT call any more tools.",
	})
	if finalResp, finalErr := a.provider.Chat(ctx, a.history); finalErr == nil && finalResp.Content != "" {
		a.history = append(a.history, provider.Message{
			Role:    "assistant",
			Content: utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
		})
		return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	recentTools := []string{}
	if len(a.toolCallHistory) > 0 {
		recentCount := len(a.toolCallHistory)
		start := 0
		if recentCount > 5 {
			start = recentCount - 5
		}
		recentTools = a.toolCallHistory[start:]
	}
	return "", fmt.Errorf("exceeded maximum turns (%d). Completed %d turns with %d tool calls. Recent tools: %v",
		a.maxTurns, a.iterationCount, len(a.toolCallHistory), recentTools)
}

// StreamHandler is called for each streaming chunk
type StreamHandler func(content string, done bool)

// RunConversationStream runs a streaming conversation
func (a *Agent) RunConversationStream(ctx context.Context, input string, handler StreamHandler) error {
	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Cortex: inject memory context and adjust behavior
	if a.cortexManager != nil {
		a.cortexManager.OnUserMessage(input)

		// Inject memory and user context into system prompt
		if a.iterationCount == 0 {
			if memCtx := a.cortexManager.GetPromptContext(); memCtx != "" {
				a.AddSystemContext("[Memory Context]\n" + memCtx)
			}
			if userCtx := a.cortexManager.GetUserContext(); userCtx != "" {
				a.AddSystemContext("[User Profile]\n" + userCtx)
			}
		}
	}

	// Auto sub-task delegation for complex tasks
	if delegated, result, err := a.trySubTaskDelegation(ctx, input); delegated {
		if err != nil {
			handler(fmt.Sprintf("\nError delegating sub-tasks: %v\n", err), true)
			return err
		}
		// Stream the delegation result
		handler(fmt.Sprintf("\n[⚡ Task Auto-Delegated to Sub-Task Executor]\n"), false)
		handler(result, false)
		handler("\n\n[Synthesizing final response...]\n\n", false)

		// Run synthesis via streaming
		return a.RunConversationStream(ctx,
			fmt.Sprintf(`I have completed the sub-tasks for the task. The sub-tasks execution results are:

%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`, result),
			handler)
	}

	// Truncate input (stream path)
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: utils.TruncateDetailed(input, a.maxMsgLen),
	})

	// Truncate history to prevent overflow
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Cortex: freeze snapshot at turn start
		if a.cortexManager != nil {
			a.cortexManager.OnTurnStart()
		}

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			return fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			return fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
		}

		// Emit turn start event
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn": a.iterationCount,
		})

		// Notify handler that a new turn is starting (for multi-turn conversations with tool calls)
		if a.iterationCount > 0 {
			handler("\n>>>TURN_START<<<\n", false)
		}

		// Build LLM request
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.history,
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, decision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			return fmt.Errorf("hook error: %w", err)
		}
		if decision.Action == hooks.HookActionStop {
			return fmt.Errorf("hook stopped: %s", decision.Reason)
		}
		if decision.Action == hooks.HookActionReject {
			return fmt.Errorf("hook rejected: %s", decision.Reason)
		}

		// Try streaming first
		var fullContent string
		var toolCalls []types.ToolCall
		streamed := false

		// Check if provider supports streaming
		type streamer interface {
			StreamWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}, handler provider.StreamHandler) error
		}
		type simpleStreamer interface {
			Stream(ctx context.Context, messages []provider.Message, handler provider.StreamHandler) error
		}

		if st, ok := a.provider.(streamer); ok && len(a.tools) > 0 {
			// Streaming with tools
			err = st.StreamWithTools(ctx, req.Messages, req.Tools, func(resp *provider.StreamResponse) {
				if resp.Error != nil {
					lastErr = resp.Error
					return
				}
				if resp.Done {
					fullContent = resp.Content
					toolCalls = resp.ToolCalls
					for i := range toolCalls {
						if toolCalls[i].ID == "" {
							toolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
						}
						if toolCalls[i].Function.Name == "" {
							toolCalls[i].Function.Name = toolCalls[i].Name
						}
					}
				} else {
					fullContent += resp.Content
				}
				handler(redact.RedactIfEnabled(resp.Content, a.secretRedaction), resp.Done)
			})
			if err == nil {
				streamed = true
				// Fallback: if streaming returned no tool calls but the content looks like
				// the model should have called a tool, retry with non-streaming ChatWithTools
				if len(toolCalls) == 0 && fullContent != "" {
					stdlog.Printf("[WARN] Stream returned no tool calls, falling back to ChatWithTools")
					type openAIlikeFallback interface {
						ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
					}
					if oa, ok := a.provider.(openAIlikeFallback); ok {
						nonStreamResp, nsErr := oa.ChatWithTools(ctx, req.Messages, req.Tools)
						if nsErr == nil && len(nonStreamResp.ToolCalls) > 0 {
							stdlog.Printf("[WARN] ChatWithTools returned %d tool calls (stream parser bug)", len(nonStreamResp.ToolCalls))
							toolCalls = nonStreamResp.ToolCalls
							fullContent = nonStreamResp.Content
						}
					}
				}
			} else {
				lastErr = err
			}
		} else if ss, ok := a.provider.(simpleStreamer); ok {
			err = ss.Stream(ctx, req.Messages, func(resp *provider.StreamResponse) {
				if resp.Error != nil {
					lastErr = resp.Error
					return
				}
				if resp.Done {
					fullContent = resp.Content
				} else {
					fullContent += resp.Content
				}
				handler(redact.RedactIfEnabled(resp.Content, a.secretRedaction), resp.Done)
			})
			if err == nil {
				streamed = true
			} else {
				lastErr = err
			}
		}

		// Fall back to non-streaming if streaming failed
		if !streamed {
			var resp *provider.ChatResponse
			type openAIlike interface {
				ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
			}
			if oa, ok := a.provider.(openAIlike); ok && len(a.tools) > 0 {
				resp, err = oa.ChatWithTools(ctx, req.Messages, req.Tools)
			} else {
				resp, err = a.provider.Chat(ctx, req.Messages)
			}

			if err != nil {
				lastErr = err
				a.Emit(bus.EventKindError, err.Error())
				handler("", true)
				continue
			}

			fullContent = resp.Content
			toolCalls = resp.ToolCalls
			for i := range toolCalls {
				if toolCalls[i].ID == "" {
					toolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
				}
				if toolCalls[i].Function.Name == "" {
					toolCalls[i].Function.Name = toolCalls[i].Name
				}
			}
			handler(redact.RedactIfEnabled(resp.Content, a.secretRedaction), true)
		}

		// Call AfterLLM hooks
		llmResp := &hooks.LLMHookResponse{
			Content:   fullContent,
			ToolCalls: toolCalls,
		}
		llmResp, decision, err = a.hooks.AfterLLM(ctx, llmResp)
		if err != nil {
			return fmt.Errorf("hook error: %w", err)
		}

		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// No tool calls - return the response
		if len(toolCalls) == 0 {
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// Cortex: refresh snapshot at session end
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

			return nil
		}

		// Store tool calls for history
		tcs := make([]types.ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			name := tc.Function.Name
			if name == "" {
				name = tc.Name
			}
			tcs[i] = types.ToolCall{
				ID:       tc.ID,
				Name:     name,
				Type:     "function",
				Function: tc.Function,
			}
		}

		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   utils.TruncateDetailed(fullContent, a.maxMsgLen),
			ToolCalls: tcs,
		})

		// Execute tools with hooks
		results, err := a.executeToolsWithHooks(ctx, toolCalls)
		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindToolError, err.Error())
			for _, tc := range toolCalls {
				errContent := fmt.Sprintf("Error: %v", err)
				a.history = append(a.history, provider.Message{
					Role:       "tool",
					Content:    utils.TruncateDetailed(errContent, a.maxMsgLen),
					ToolCallID: tc.ID,
				})
				toolName := tc.Function.Name
				if toolName == "" {
					toolName = tc.Name
				}
				// Use special prefix to identify tool results
				handler(fmt.Sprintf("\n>>>TOOL_RESULT_START|%s<<<%s\n>>>TOOL_RESULT_END<<<\n", toolName, err), false)
			}
			continue
		}

		// Add results to history
		for _, tc := range toolCalls {
			result := results[tc.ID]
			content := result.Content
			if result.Err != nil {
				content = fmt.Sprintf("Error: %v", result.Err)
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    utils.TruncateDetailed(content, a.maxMsgLen),
				ToolCallID: tc.ID,
			})

			toolName := tc.Function.Name
			if toolName == "" {
				toolName = tc.Name
			}
			// Use special prefix to identify tool results for better display
			handler(fmt.Sprintf("\n>>>TOOL_RESULT_START|%s<<<\n%s\n>>>TOOL_RESULT_END<<<\n", toolName, redact.RedactIfEnabled(content, a.secretRedaction)), false)

			// Cortex: record tool call for review/pattern detection
			if a.cortexManager != nil {
				a.cortexManager.Trigger.OnToolCall(toolName, nil)
			}
		}

		// Check context after tool execution
		a.truncateHistory()
		a.Emit(bus.EventKindTurnEnd, nil)
	}

	a.Emit(bus.EventKindAgentEnd, nil)

	// Cortex: refresh snapshot at session end
	if a.cortexManager != nil {
		a.cortexManager.OnSessionEnd()
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("exceeded maximum turns (%d)", a.maxTurns)
}

// RunConversationStreamWithOutput runs streaming and returns output builder
func (a *Agent) RunConversationStreamWithOutput(ctx context.Context, input string) (*strings.Builder, error) {
	var output strings.Builder
	err := a.RunConversationStream(ctx, input, func(content string, done bool) {
		output.WriteString(content)
	})
	return &output, err
}

// executeToolsWithHooks executes tools with hook support
func (a *Agent) executeToolsWithHooks(ctx context.Context, toolCalls []types.ToolCall) (map[string]ToolCallResult, error) {
	results := make(map[string]ToolCallResult)
	var mu sync.Mutex

	// First, ensure all tool calls have an ID (modify in place)
	for i := range toolCalls {
		if toolCalls[i].ID == "" {
			toolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
		}
	}

	groups := a.groupToolsForExecution(toolCalls)

	for _, group := range groups {
		if group.sequential {
			for _, tc := range group.tools {
				result := a.executeSingleToolWithHooks(ctx, tc)
				mu.Lock()
				results[tc.ID] = result
				mu.Unlock()
			}
		} else {
			var wg sync.WaitGroup
			errCh := make(chan error, len(group.tools))

			for _, tc := range group.tools {
				tc := tc
				wg.Add(1)
				go func() {
					defer wg.Done()
					result := a.executeSingleToolWithHooks(ctx, tc)
					mu.Lock()
					results[tc.ID] = result
					if result.Err != nil {
						errCh <- result.Err
					}
					mu.Unlock()
				}()
			}

			wg.Wait()
			close(errCh)

			// Collect all errors but don't return early - all results must be processed
			var execErrors []error
			for err := range errCh {
				if err != nil {
					execErrors = append(execErrors, err)
				}
			}
			if len(execErrors) > 0 {
				// Return first error but still return all results
				return results, execErrors[0]
			}
		}
	}

	return results, nil
}

// executeSingleToolWithHooks executes a single tool with hooks
func (a *Agent) executeSingleToolWithHooks(ctx context.Context, tc types.ToolCall) ToolCallResult {
	start := time.Now()

	toolName := tc.Function.Name
	if toolName == "" {
		toolName = tc.Name
	}

	var toolArgs map[string]interface{}
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &toolArgs); err != nil {
			toolArgs = map[string]interface{}{"input": tc.Function.Arguments}
		}
	} else if tc.Arguments != nil {
		toolArgs = tc.Arguments
	} else {
		toolArgs = map[string]interface{}{}
	}

	a.Emit(bus.EventKindToolBefore, map[string]interface{}{
		"tool": toolName,
		"args": toolArgs,
	})

	callReq := &hooks.ToolCallHookRequest{
		ToolName: toolName,
		ToolArgs: toolArgs,
	}
	callReq, decision, err := a.hooks.BeforeTool(ctx, callReq)
	if err != nil {
		return ToolCallResult{
			ID:      tc.ID,
			Name:    tc.Name,
			Content: fmt.Sprintf("Hook error: %v", err),
			Err:     err,
		}
	}
	if decision.Action == hooks.HookActionReject {
		return ToolCallResult{
			ID:      tc.ID,
			Name:    tc.Name,
			Content: fmt.Sprintf("Rejected by hook: %s", decision.Reason),
			Err:     fmt.Errorf("rejected: %s", decision.Reason),
		}
	}

	result, err := a.registry.Execute(ctx, toolName, callReq.ToolArgs)
	elapsed := time.Since(start)

	if err != nil {
		stdlog.Printf("[TOOL] %s error after %v: %v", toolName, elapsed, err)
	} else {
		stdlog.Printf("[TOOL] %s success after %v", toolName, elapsed)
	}

	content := ""
	if err != nil {
		content = fmt.Sprintf("Error: %v", err)
	} else {
		switch v := result.(type) {
		case string:
			content = v
		case nil:
			content = ""
		default:
			if jsonBytes, jsonErr := json.Marshal(result); jsonErr == nil {
				content = string(jsonBytes)
			} else {
				content = fmt.Sprintf("%v", result)
			}
		}
	}

	resultResp := &hooks.ToolResultHookResponse{
		ToolName:    toolName,
		ToolArgs:    toolArgs,
		Result:      result,
		Error:       err,
		ExecutionMs: elapsed.Milliseconds(),
	}
	resultResp, _, _ = a.hooks.AfterTool(ctx, resultResp)

	a.Emit(bus.EventKindToolAfter, map[string]interface{}{
		"tool":  toolName,
		"error": err,
		"ms":    elapsed.Milliseconds(),
	})

	return ToolCallResult{
		ID:        tc.ID,
		Name:      toolName,
		Content:   content,
		Err:       err,
		Execution: elapsed,
	}
}

// groupToolsForExecution groups tools by whether they can be executed in parallel
func (a *Agent) groupToolsForExecution(toolCalls []types.ToolCall) []toolGroup {
	var parallel []types.ToolCall
	var sequential []types.ToolCall

	for _, tc := range toolCalls {
		toolName := tc.Function.Name
		if toolName == "" {
			toolName = tc.Name
		}
		if exclusiveTools[toolName] || sequentialTools[toolName] {
			sequential = append(sequential, tc)
		} else {
			parallel = append(parallel, tc)
		}
	}

	var groups []toolGroup
	if len(parallel) > 0 {
		groups = append(groups, toolGroup{tools: parallel, sequential: false})
	}
	if len(sequential) > 0 {
		groups = append(groups, toolGroup{tools: sequential, sequential: true})
	}
	return groups
}

// Reset clears the conversation history
func (a *Agent) Reset() {
	a.history = a.history[:1] // Keep system prompt
	a.tokenUsage = 0
	a.inputTokens = 0
	a.outputTokens = 0
	a.cacheReadTokens = 0
	a.toolCallHistory = nil
	a.toolCallCount = make(map[string]int)
}

// GetHistory returns the conversation history
func (a *Agent) GetHistory() []provider.Message {
	return a.history
}

// GetTokenStats returns the token usage statistics
func (a *Agent) GetTokenStats() (inputTokens, outputTokens, cacheReadTokens int) {
	return a.inputTokens, a.outputTokens, a.cacheReadTokens
}

// TokenUsage represents token usage statistics for tracking
type TokenUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
}

// GetTokenUsage returns the usage statistics as a TokenUsage struct
func (a *Agent) GetTokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:     a.inputTokens,
		OutputTokens:    a.outputTokens,
		CacheReadTokens: a.cacheReadTokens,
	}
}

// SetHistory sets the conversation history
func (a *Agent) SetHistory(history []provider.Message) {
	a.history = history
}

// GetHistoryLength returns the current history length in characters
func (a *Agent) GetHistoryLength() int {
	total := 0
	for _, m := range a.history {
		total += len(m.Content)
	}
	return total
}

// truncateHistory truncates message history to prevent overflow
func (a *Agent) truncateHistory() {
	// If maxTotalLen is 0 (unset), skip truncation entirely
	if a.maxTotalLen <= 0 {
		return
	}

	totalLen := a.GetHistoryLength()

	if a.compressionEnabled && totalLen > int(float64(a.maxTotalLen)*a.compressionRatio) {
		a.compressHistory()
		return
	}

	if totalLen < a.maxTotalLen {
		return
	}

	systemIdx := -1
	for i, m := range a.history {
		if m.Role == "system" {
			systemIdx = i
			break
		}
	}

	for totalLen > a.maxTotalLen && len(a.history) > 1 {
		idx := 0
		if systemIdx == 0 {
			idx = 1
		}

		// Skip if this message is a tool result — we must delete the
		// preceding assistant (tool_calls) message first to keep the
		// sequence valid for the API.
		if a.history[idx].Role == "tool" {
			// Find the assistant message that owns this tool result
			found := false
			for j := idx - 1; j >= 0; j-- {
				if a.history[j].Role == "assistant" && len(a.history[j].ToolCalls) > 0 {
					// Remove the entire group: assistant(tool_calls) + all following tool messages
					removeStart := j
					removeEnd := j + 1
					for removeEnd < len(a.history) && a.history[removeEnd].Role == "tool" {
						totalLen -= len(a.history[removeEnd].Content)
						removeEnd++
					}
					totalLen -= len(a.history[j].Content)
					a.history = append(a.history[:removeStart], a.history[removeEnd:]...)
					found = true
					break
				}
			}
			if found {
				// Recalculate systemIdx
				systemIdx = -1
				for i, m := range a.history {
					if m.Role == "system" {
						systemIdx = i
						break
					}
				}
				continue
			}
		}

		// If this is an assistant message with tool_calls, also remove following tool messages
		if a.history[idx].Role == "assistant" && len(a.history[idx].ToolCalls) > 0 {
			removeEnd := idx + 1
			for removeEnd < len(a.history) && a.history[removeEnd].Role == "tool" {
				totalLen -= len(a.history[removeEnd].Content)
				removeEnd++
			}
			totalLen -= len(a.history[idx].Content)
			a.history = append(a.history[:idx], a.history[removeEnd:]...)
		} else {
			totalLen -= len(a.history[idx].Content)
			a.history = append(a.history[:idx], a.history[idx+1:]...)
		}

		if systemIdx == idx {
			systemIdx = -1
			for i, m := range a.history {
				if m.Role == "system" {
					systemIdx = i
					break
				}
			}
		}
	}
}

// compressHistory performs smart context compression
func (a *Agent) compressHistory() {
	var userMsgs []provider.Message
	var assistantMsgs []provider.Message

	for _, m := range a.history {
		switch m.Role {
		case "user":
			userMsgs = append(userMsgs, m)
		case "assistant":
			assistantMsgs = append(assistantMsgs, m)
		}
	}

	totalMsgs := len(userMsgs)
	keepRecent := 4
	keepFirst := 2

	if totalMsgs <= keepRecent+keepFirst {
		return
	}

	// Build a set of indices to keep (system + first N user msgs + last N user msgs + their assistant replies)
	keepIndices := make(map[int]bool)

	// Always keep system messages
	for i, m := range a.history {
		if m.Role == "system" {
			keepIndices[i] = true
		}
	}

	// Keep first N user messages and their adjacent assistant/tool messages
	kept := 0
	for i, m := range a.history {
		if m.Role == "user" && kept < keepFirst {
			keepIndices[i] = true
			kept++
			// Also keep the assistant reply and any tool messages that follow
			for j := i + 1; j < len(a.history); j++ {
				if a.history[j].Role == "assistant" || a.history[j].Role == "tool" {
					keepIndices[j] = true
				} else {
					break
				}
			}
		}
	}

	// Keep last N user messages and their adjacent assistant/tool messages
	kept = 0
	for i := len(a.history) - 1; i >= 0 && kept < keepRecent; i-- {
		if a.history[i].Role == "user" {
			keepIndices[i] = true
			kept++
			// Also keep the assistant reply and any tool messages that follow
			for j := i + 1; j < len(a.history); j++ {
				if a.history[j].Role == "assistant" || a.history[j].Role == "tool" {
					keepIndices[j] = true
				} else {
					break
				}
			}
		}
	}

	// Generate summary of removed messages
	var removedMsgs []provider.Message
	for i, m := range a.history {
		if !keepIndices[i] && m.Role == "user" {
			removedMsgs = append(removedMsgs, m)
		}
	}

	var newHistory []provider.Message
	for i, m := range a.history {
		if keepIndices[i] {
			newHistory = append(newHistory, m)
		}
	}

	// Insert summary after system messages
	if len(removedMsgs) > 0 {
		summary := a.generateCompressionSummary(removedMsgs)
		summaryMsg := provider.Message{
			Role: "system",
			Content: fmt.Sprintf("\n\n[Previous conversation summary (%d messages summarized)]\n%s",
				len(removedMsgs), summary),
		}
		// Insert after the last system message
		insertIdx := 0
		for i, m := range newHistory {
			if m.Role == "system" {
				insertIdx = i + 1
			}
		}
		newHistory = append(newHistory[:insertIdx], append([]provider.Message{summaryMsg}, newHistory[insertIdx:]...)...)
	}

	a.history = newHistory
}

// generateCompressionSummary generates a summary of old messages
func (a *Agent) generateCompressionSummary(messages []provider.Message) string {
	var summary strings.Builder
	summary.WriteString("Summary of earlier conversation:\n")

	userCount := 0
	actionCount := 0

	for _, m := range messages {
		if m.Role == "user" {
			userCount++
			content := m.Content
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			if userCount <= 3 {
				summary.WriteString(fmt.Sprintf("- User: %s\n", content))
			}
		} else if m.Role == "tool" && !strings.Contains(m.Content, "Error:") {
			actionCount++
			if actionCount <= 3 {
				if len(m.Content) > 50 {
					summary.WriteString(fmt.Sprintf("- Tool result: %s...\n", m.Content[:50]))
				} else {
					summary.WriteString(fmt.Sprintf("- Tool result: %s\n", m.Content))
				}
			}
		}
	}

	if userCount > 3 {
		summary.WriteString(fmt.Sprintf("- ... and %d more exchanges\n", userCount-3))
	}

	return summary.String()
}

// EnableCompression enables/disables context compression
func (a *Agent) EnableCompression(enabled bool) {
	a.compressionEnabled = enabled
}

// SetCompressionRatio sets the threshold ratio for compression
func (a *Agent) SetCompressionRatio(ratio float64) {
	if ratio > 0.3 && ratio <= 1.0 {
		a.compressionRatio = ratio
	}
}

// SetMaxIterations sets the maximum iterations
func (a *Agent) SetMaxIterations(max int) {
	if max > 0 {
		a.maxIterations = max
	}
}

// AddSystemContext appends context to the system prompt message.
// If no system message exists, creates one.
func (a *Agent) AddSystemContext(ctx string) {
	if len(a.history) == 0 || a.history[0].Role != "system" {
		a.history = append([]provider.Message{{Role: "system", Content: ctx}}, a.history...)
		return
	}
	a.history[0].Content += "\n\n" + ctx
}

// GetProvider returns the agent's provider for use by other components
func (a *Agent) GetProvider() provider.Provider {
	return a.provider
}
