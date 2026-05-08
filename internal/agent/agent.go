package agent

import (
	"context"
	"encoding/json"
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
	hooks *hooks.Manager

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
	tokenUsage int64
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
		provider:           prov,
		registry:           registry,
		tools:              tools,
		history:            history,
		maxTurns:           100,
		maxIterations:      100,
		maxTokenBudget:     0,
		sameToolLimit:       8,
		consecutiveLimit:   20,
		toolCallCount:      make(map[string]int),
		subTaskEnabled:     true,
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

// RunConversation runs a conversation with automatic tool execution
func (a *Agent) RunConversation(ctx context.Context, input string) (string, error) {
	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Cortex: User message trigger - increments turn counter, may trigger nudge
	if a.cortexManager != nil {
		a.cortexManager.OnUserMessage(input)
	}

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
		Content: truncateStr(input, a.maxMsgLen),
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
			content := truncateStr(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

			return resp.Content, nil
		}

		// Tool call loop detection - break if stuck in a loop
		for _, tc := range resp.ToolCalls {
			name := tc.Function.Name
			a.toolCallCount[name]++
			a.toolCallHistory = append(a.toolCallHistory, name)
		}

		// Check if same tool called too many times
		loopDetected := false
		for name, count := range a.toolCallCount {
			if count >= a.sameToolLimit {
				fmt.Printf("[WARN] Tool call loop detected: %s called %d times, forcing final response\n", name, count)
				loopDetected = true
				break
			}
		}

		// Check consecutive tool calls limit
		if len(a.toolCallHistory) >= a.consecutiveLimit {
			fmt.Printf("[WARN] Too many consecutive tool calls (%d), forcing final response\n", len(a.toolCallHistory))
			loopDetected = true
		}

		if loopDetected {
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: truncateStr(resp.Content, a.maxMsgLen),
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
				Content: truncateStr(finalResp.Content, a.maxMsgLen),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}
			return finalResp.Content, nil
		}
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
				handler(resp.Content, resp.Done)
			})
			if err == nil {
				streamed = true
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
				handler(resp.Content, resp.Done)
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
				ID:       tc.ID,
				Name:     tc.Function.Name,
				Type:     "function",
				Function: tc.Function,
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
			for _, tc := range toolCalls {
				errContent := fmt.Sprintf("Error: %v", err)
				a.history = append(a.history, provider.Message{
					Role:       "tool",
					Content:    truncateStr(errContent, a.maxMsgLen),
					ToolCallID: tc.ID,
				})
				toolName := tc.Function.Name
				if toolName == "" {
					toolName = tc.Name
				}
				handler(fmt.Sprintf("\n[Tool: %s] Error: %v\n", toolName, err), false)
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
				Content:    truncateStr(content, a.maxMsgLen),
				ToolCallID: tc.ID,
			})

			toolName := tc.Function.Name
			if toolName == "" {
				toolName = tc.Name
			}
			handler(fmt.Sprintf("\n[Tool: %s] %s\n", toolName, content), false)
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
	if len(sequential) > 0 {
		groups = append(groups, toolGroup{tools: sequential, sequential: true})
	}
	return groups
}

// Reset clears the conversation history
	a.tokenUsage = 0
	a.toolCallHistory = nil
	a.toolCallCount = make(map[string]int)
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
		totalLen -= len(a.history[idx].Content)
		a.history = append(a.history[:idx], a.history[idx+1:]...)

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

	var newHistory []provider.Message

	for _, m := range a.history {
		if m.Role == "system" {
			newHistory = append(newHistory, m)
			break
		}
	}

	for i := 0; i < keepFirst && i < len(userMsgs); i++ {
		newHistory = append(newHistory, userMsgs[i])
	}

	summary := a.generateCompressionSummary(userMsgs[keepFirst : len(userMsgs)-keepRecent])
	newHistory = append(newHistory, provider.Message{
		Role: "system",
		Content: fmt.Sprintf("\n\n[Previous conversation summary (%d messages summarized)]\n%s",
			totalMsgs-keepRecent-keepFirst, summary),
	})

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
