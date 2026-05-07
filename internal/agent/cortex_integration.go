package agent

import (
	"context"
	"fmt"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/execution"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// RunWithCortex runs a conversation with full Cortex Agent integration
// This enhanced method leverages all six Cortex systems:
// 1. User Message Trigger (increments turn, may trigger nudge)
// 2. Periodic Nudge Mechanism (provides proactive suggestions)
// 3. Background Review System (continuous quality assessment)
// 4. Dual File Storage (MEMORY.md + USER.md with frozen snapshots)
// 5. Holographic Memory (SQLite FTS5 for semantic retrieval)
// 6. Memory Manager with Frozen Snapshot (prefix cache protection)
//
// Plus the three-layer architecture:
// - Layer 1: Perception (intent classification, entity extraction, noise detection)
// - Layer 2: Cognition (task planning, dynamic adjustment, sub-agent decisions)
// - Layer 3: Execution (checkpoint persistence, resume, result validation)
func (a *Agent) RunWithCortex(ctx context.Context, input string) (string, error) {
	if a.cortexManager == nil {
		// Fallback to regular RunConversation
		return a.RunConversation(ctx, input)
	}

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// ========== HERMES SYSTEM 1: User Message Trigger ==========
	// Increments turn counter and may trigger nudge
	a.cortexManager.OnUserMessage(input)

	// ========== LAYER 1: PERCEPTION - Analyze user input ==========
	// Parse intent, extract entities, detect noise
	perceptionResult := a.cortexManager.Perception.Parse(input, a.getRecentHistory(5))
	a.cortexManager.LastPerception = perceptionResult

	// Handle noise/clarification if needed
	if perceptionResult.Noise.HasNoise && len(perceptionResult.Noise.Suggestions) > 0 {
		// Log noise detection for debugging
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"noise_detected": true,
			"suggestions":    perceptionResult.Noise.Suggestions,
		})
	}

	// ========== LAYER 2: COGNITION - Create execution plan ==========
	// Dynamic planning based on perception complexity
	decision := a.cortexManager.Cognition.CreatePlan(input, perceptionResult)
	a.cortexManager.LastDecision = decision

	// Dynamically adjust maxIterations based on task complexity
	originalMaxTurns := a.maxTurns
	if decision.MaxTurns > 0 && decision.MaxTurns < a.maxTurns {
		a.maxTurns = decision.MaxTurns
	}

	// Check if clarification is needed
	if decision.ClarificationNeeded {
		clarificationMsg := "I need some clarification: " + decision.ClarificationQuestion
		a.history = append(a.history, provider.Message{
			Role:    "assistant",
			Content: clarificationMsg,
		})
		return clarificationMsg, nil
	}

	// Apply tool filter based on decision (if set)
	originalTools := a.tools
	if len(decision.ToolFilter) > 0 {
		a.tools = a.filterTools(decision.ToolFilter)
	}

	// ========== HERMES SYSTEM 4: Frozen Snapshot - Get memory for prompt ==========
	// Critical optimization: use frozen snapshot to protect prefix cache
	memoryPrompt := a.cortexManager.Snapshot.GetMemoryForPrompt()
	userPrompt := a.cortexManager.Snapshot.GetUserForPrompt()

	// Inject memory into system prompt (optional, controlled by memoryEnabled)
	if a.memoryEnabled && memoryPrompt != "" {
		a.injectMemoryIntoSystemPrompt(memoryPrompt, userPrompt)
	}

	// ========== LAYER 3: EXECUTION - Setup checkpoint ==========
	var checkpoint *execution.Checkpoint
	if decision.Plan != nil {
		checkpoint = a.cortexManager.Execution.StartCheckpoint("", decision.Plan)
		a.cortexManager.LastCheckpoint = checkpoint
	}

	// ========== MAIN CONVERSATION LOOP ==========
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: truncateStr(input, a.maxMsgLen),
	})
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// ========== HERMES SYSTEM 4: OnTurnStart - Freeze snapshot ==========
		// Critical: memory updates written to disk but NOT loaded into
		// system prompt until next session, protecting prefix cache hit rate
		a.cortexManager.OnTurnStart()

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			lastErr = fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
			break
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			lastErr = fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
			break
		}

		// Emit turn start event with Cortex context
		currentStepID := 0
		if decision.Plan != nil {
			currentStepID = decision.Plan.Steps[0].ID
		}
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn":            a.iterationCount,
			"perception":      perceptionResult.Intent.Type,
			"complexity":      perceptionResult.Intent.Complexity,
			"current_step_id": currentStepID,
		})

		// ========== LLM Call ==========
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.history,
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, hookDecision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("hook error: %w", err)
			continue
		}
		if hookDecision.Action == hooks.HookActionStop || hookDecision.Action == hooks.HookActionReject {
			lastErr = fmt.Errorf("hook rejected: %s", hookDecision.Reason)
			break
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
		llmResp, _, _ = a.hooks.AfterLLM(ctx, llmResp)

		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// ========== No tool calls - return response ==========
		if len(resp.ToolCalls) == 0 {
			content := truncateStr(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})

			// ========== LAYER 3: Complete checkpoint ==========
			if checkpoint != nil {
				a.cortexManager.Execution.CompleteCheckpoint(checkpoint)
			}

			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// ========== HERMES: Session end - refresh memory snapshot ==========
			// This ensures future conversations get the latest memory
			// while protecting the prefix cache during the current session
			a.cortexManager.OnSessionEnd()

			// Restore original settings
			a.maxTurns = originalMaxTurns
			a.tools = originalTools

			return resp.Content, nil
		}

		// ========== Execute tools ==========
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

		// Execute tools
		results, err := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindToolError, err.Error())
			continue
		}

		// ========== LAYER 3: Update checkpoint with tool results ==========
		if checkpoint != nil {
			for _, tc := range resp.ToolCalls {
				result := results[tc.ID]
				a.cortexManager.Execution.StoreToolResult(checkpoint, tc.Name, result)

				// Validate result
				validation := a.cortexManager.Execution.ValidateResult(checkpoint, tc.Name, result)
				if !validation.Passed {
					a.Emit(bus.EventKindWarning, map[string]interface{}{
						"validation_failed": true,
						"issues":           validation.Issues,
					})

					// Suggest recovery action
					recovery := a.cortexManager.Execution.SuggestRecoveryAction(checkpoint, nil)
					switch recovery {
					case execution.RecoveryRetry:
						// Continue - will retry
					case execution.RecoveryAlternative:
						// Log for potential alternative approach
						a.Emit(bus.EventKindWarning, map[string]interface{}{
							"recovery_action": "alternative",
						})
					case execution.RecoveryAskUser:
						// Break and ask user
						content := "I encountered an issue. " + fmt.Sprintf("Issues: %v. What would you like me to do?", validation.Issues)
						a.history = append(a.history, provider.Message{
							Role:    "assistant",
							Content: content,
						})
						lastErr = fmt.Errorf("recovery needed")
						break
					}
				}
			}

			// Update checkpoint turn count
			a.cortexManager.Execution.UpdateTurnCount(checkpoint, a.iterationCount+1)
		}

		// Add tool results to history
		for _, tc := range resp.ToolCalls {
			result := results[tc.ID]
			content := result.Content
			if result.Err != nil {
				content = fmt.Sprintf("Error: %v", result.Err)
				// Store error in checkpoint
				if checkpoint != nil {
					a.cortexManager.Execution.StoreError(checkpoint, tc.Name, result.Err)
				}
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

		// Check for recovery break
		if lastErr != nil && lastErr.Error() == "recovery needed" {
			break
		}
	}

	// ========== Session end cleanup ==========
	a.Emit(bus.EventKindAgentEnd, nil)
	a.cortexManager.OnSessionEnd()

	// Restore original settings
	a.maxTurns = originalMaxTurns
	a.tools = originalTools

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("exceeded maximum turns (%d)", a.maxTurns)
}

// ========== Helper methods for Cortex integration ==========

// getRecentHistory returns recent conversation history for perception
func (a *Agent) getRecentHistory(count int) []string {
	var history []string
	start := len(a.history) - count
	if start < 0 {
		start = 0
	}
	for i := start; i < len(a.history); i++ {
		if a.history[i].Role == "user" {
			history = append(history, a.history[i].Content)
		}
	}
	return history
}

// injectMemoryIntoSystemPrompt injects memory content into the system prompt
func (a *Agent) injectMemoryIntoSystemPrompt(memory, user string) {
	for i, msg := range a.history {
		if msg.Role == "system" {
			// Append memory to existing system prompt
			injection := fmt.Sprintf("\n\n[MEMORY]\n%s\n\n[USER PROFILE]\n%s", memory, user)
			a.history[i].Content += injection
			return
		}
	}
}

// filterTools returns only tools matching the given names
func (a *Agent) filterTools(allowed []string) []map[string]interface{} {
	allowedMap := make(map[string]bool)
	for _, name := range allowed {
		allowedMap[name] = true
	}

	var filtered []map[string]interface{}
	for _, tool := range a.tools {
		if name, ok := tool["type"].(string); ok {
			if allowedMap[name] {
				filtered = append(filtered, tool)
			}
		}
	}
	return filtered
}

// Additional event kinds for Cortex integration
const (
	EventKindWarning = "warning"
)
