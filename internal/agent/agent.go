package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
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

	// Context compression settings
	compressionEnabled bool
	compressionRatio   float64 // trigger compression when history exceeds this ratio

	// Hook and EventBus
	hooks   *hooks.HookManager
	bus     *bus.EventBus
	session string

	// Steering settings
	maxIterations  int
	maxTokenBudget int64
	tokenUsage     int64
	iterationCount int

	// Memory integration
	memoryEnabled bool

	// Cortex Agent six-system integration
	cortexManager *cortex.Manager
}

// ToolRegistry interface for tool execution
type ToolRegistry interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error)
}

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
		provider:           prov,
		registry:           registry,
		tools:              tools,
		history:            history,
		maxTurns:           10,
		maxTotalLen:        200000, // 200K chars
		maxMsgLen:          50000,  // 50K chars per message
		compressionEnabled: true,
		compressionRatio:   0.7, // trigger compression at 70%
		hooks:              hooks.NewHookManager(),
		bus:                bus.NewEventBus(),
		maxIterations:      50,
	}

	// Register built-in hooks
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

// AgentOption configures an agent
type AgentOption func(*Agent)

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

func (a *Agent) registerBuiltinHooks() {
	// Privacy hook
	a.hooks.Register(hooks.HookRegistration{
		Name:   "privacy",
		Source: hooks.HookSourceBuiltIn,
		Hook:   hooks.NewPrivacyHook(),
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

// RunConversation runs a conversation with automatic tool execution
func (a *Agent) RunConversation(ctx context.Context, input string) (string, error) {
	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Cortex: User message trigger - increments turn counter, may trigger nudge
	if a.cortexManager != nil {
		a.cortexManager.OnUserMessage(input)
	}

	// Truncate input
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: truncateStr(input, a.maxMsgLen),
	})

	// Truncate history to prevent overflow
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Cortex: OnTurnStart - freezes memory snapshot for prefix cache
		// Critical optimization: memory updates written to disk but NOT loaded
		// into system prompt until next session, protecting cache hit rate
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
			content := truncateStr(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// Cortex: Session end - refresh memory snapshot for next session
			// This ensures future conversations get the latest memory
			// while protecting the prefix cache during the current session
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

			return resp.Content, nil
		}

		// Store tool calls for history
		toolCalls := make([]types.ToolCall, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			toolCalls[i] = types.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			}
		}

		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   truncateStr(resp.Content, a.maxMsgLen),
			ToolCalls: toolCalls,
		})

		// Execute tools with hooks
		results, err := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindToolError, err.Error())
			continue
		}

		// Add results to history
		for _, tc := range resp.ToolCalls {
			result := results[tc.ID]
			content := result.Content
			if result.Err != nil {
				content = fmt.Sprintf("Error: %v", result.Err)
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    truncateStr(content, a.maxMsgLen),
				ToolCallID: tc.ID,
			})
		}

		// Check context after tool execution
		a.truncateHistory()
		a.Emit(bus.EventKindTurnEnd, nil)
	}

	a.Emit(bus.EventKindAgentEnd, nil)

	// Cortex: Session end - refresh memory snapshot for next session
	if a.cortexManager != nil {
		a.cortexManager.OnSessionEnd()
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("exceeded maximum turns (%d)", a.maxTurns)
}

// StreamHandler is called for each streaming chunk
type StreamHandler func(content string, done bool)

// RunConversationStream runs a streaming conversation
func (a *Agent) RunConversationStream(ctx context.Context, input string, handler StreamHandler) error {
	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Truncate input
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: truncateStr(input, a.maxMsgLen),
	})

	// Truncate history to prevent overflow
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
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
				fullContent = resp.Content
				toolCalls = resp.ToolCalls
				if !resp.Done {
					handler(resp.Content, false)
				}
			})
			if err == nil {
				streamed = true
			} else {
				lastErr = err
			}
		} else if ss, ok := a.provider.(simpleStreamer); ok {
			// Simple streaming
			err = ss.Stream(ctx, req.Messages, func(resp *provider.StreamResponse) {
				if resp.Error != nil {
					lastErr = resp.Error
					return
				}
				fullContent += resp.Content
				if !resp.Done {
					handler(resp.Content, false)
				}
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
			handler(resp.Content, true)
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
			content := truncateStr(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			return nil
		}

		// Store tool calls for history
		tcs := make([]types.ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			tcs[i] = types.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			}
		}

		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   truncateStr(fullContent, a.maxMsgLen),
			ToolCalls: tcs,
		})

		// Execute tools with hooks
		results, err := a.executeToolsWithHooks(ctx, toolCalls)
		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindToolError, err.Error())
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
				Content:    truncateStr(content, a.maxMsgLen),
				ToolCallID: tc.ID,
			})

			// Stream tool result
			handler(fmt.Sprintf("\n[Tool: %s] %s\n", tc.Name, content), false)
		}

		// Check context after tool execution
		a.truncateHistory()
		a.Emit(bus.EventKindTurnEnd, nil)
	}

	a.Emit(bus.EventKindAgentEnd, nil)
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

	// Group tools by execution mode
	groups := a.groupToolsForExecution(toolCalls)

	for _, group := range groups {
		if group.sequential {
			// Execute sequentially
			for _, tc := range group.tools {
				result := a.executeSingleToolWithHooks(ctx, tc)
				mu.Lock()
				results[tc.ID] = result
				mu.Unlock()
			}
		} else {
			// Execute in parallel using goroutines
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

			// Check for errors
			for err := range errCh {
				if err != nil {
					return results, err
				}
			}
		}
	}

	return results, nil
}

// executeSingleToolWithHooks executes a single tool with hooks
func (a *Agent) executeSingleToolWithHooks(ctx context.Context, tc types.ToolCall) ToolCallResult {
	start := time.Now()

	// Emit before tool event
	a.Emit(bus.EventKindToolBefore, map[string]interface{}{
		"tool": tc.Name,
		"args": tc.Arguments,
	})

	// Call BeforeTool hooks
	callReq := &hooks.ToolCallHookRequest{
		ToolName: tc.Name,
		ToolArgs: tc.Arguments,
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

	// Execute tool
	result, err := a.registry.Execute(ctx, tc.Name, callReq.ToolArgs)
	elapsed := time.Since(start)

	content := ""
	if err != nil {
		content = fmt.Sprintf("Error: %v", err)
	} else {
		content = fmt.Sprintf("%v", result)
	}

	// Call AfterTool hooks
	resultResp := &hooks.ToolResultHookResponse{
		ToolName:    tc.Name,
		ToolArgs:    callReq.ToolArgs,
		Result:      result,
		Error:       err,
		ExecutionMs: elapsed.Milliseconds(),
	}
	resultResp, _, _ = a.hooks.AfterTool(ctx, resultResp)

	// Emit after tool event
	a.Emit(bus.EventKindToolAfter, map[string]interface{}{
		"tool":  tc.Name,
		"error": err,
		"ms":    elapsed.Milliseconds(),
	})

	return ToolCallResult{
		ID:        tc.ID,
		Name:      tc.Name,
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
		if exclusiveTools[tc.Name] || sequentialTools[tc.Name] {
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
	a.history = make([]provider.Message, 0)
	a.iterationCount = 0
	a.tokenUsage = 0
}

// GetHistory returns the conversation history
func (a *Agent) GetHistory() []provider.Message {
	return a.history
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
	totalLen := a.GetHistoryLength()

	// Check if we need compression
	if a.compressionEnabled && totalLen > int(float64(a.maxTotalLen)*a.compressionRatio) {
		a.compressHistory()
		return
	}

	// Simple truncation if not compressing
	if totalLen < a.maxTotalLen {
		return
	}

	// Find system prompt index
	systemIdx := -1
	for i, m := range a.history {
		if m.Role == "system" {
			systemIdx = i
			break
		}
	}

	// Remove oldest user/tool messages, keeping system
	for totalLen > a.maxTotalLen && len(a.history) > 1 {
		idx := 0
		if systemIdx == 0 {
			idx = 1 // Skip system
		}
		totalLen -= len(a.history[idx].Content)
		a.history = append(a.history[:idx], a.history[idx+1:]...)

		// If system was deleted, re-find it
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
	// Separate messages by role
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

	// Calculate how many to keep
	totalMsgs := len(userMsgs)
	keepRecent := 4
	keepFirst := 2

	if totalMsgs <= keepRecent+keepFirst {
		return
	}

	// Build new history with compression summary
	var newHistory []provider.Message

	// Keep system prompt
	for _, m := range a.history {
		if m.Role == "system" {
			newHistory = append(newHistory, m)
			break
		}
	}

	// Keep first messages
	for i := 0; i < keepFirst && i < len(userMsgs); i++ {
		newHistory = append(newHistory, userMsgs[i])
	}

	// Add compression summary
	summary := a.generateCompressionSummary(userMsgs[keepFirst : len(userMsgs)-keepRecent])
	newHistory = append(newHistory, provider.Message{
		Role: "system",
		Content: fmt.Sprintf("\n\n[Previous conversation summary (%d messages summarized)]\n%s",
			totalMsgs-keepRecent-keepFirst, summary),
	})

	// Keep recent messages
	for i := len(userMsgs) - keepRecent; i < len(userMsgs); i++ {
		newHistory = append(newHistory, userMsgs[i])
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

// truncateStr truncates a string to maximum length
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... [truncated, total %d chars]", len(s))
}
