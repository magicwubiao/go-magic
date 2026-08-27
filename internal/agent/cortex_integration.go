package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/execution"
	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

// RunWithCortex runs a conversation with full Cortex Agent integration.
// This enhanced method leverages all Cortex systems:
//   - SOUL.md system personality
//   - USER.md user profile
//   - LLM Planner for complex task decomposition
//   - Context compression for long conversations
//   - Trajectory recording for self-evolution (GEPA)
//   - Prompt caching
//   - Perception / Cognition / Execution three-layer architecture
//   - Frozen snapshot memory protection
func (a *Agent) RunWithCortex(ctx context.Context, input string) (string, error) {
	if a.cortexManager == nil {
		return a.RunConversation(ctx, input)
	}

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// ========== CORTEX: User Message Trigger ==========
	a.cortexManager.OnUserMessage(input)

	// ========== CORTEX: Inject SOUL.md + UserProfile into system prompt ==========
	a.injectCortexContext()

	// ========== LAYER 1: PERCEPTION ==========
	perceptionResult := a.cortexManager.Perception.Parse(input, a.getRecentHistory(5))
	a.cortexManager.LastPerception = perceptionResult

	if perceptionResult.Noise.HasNoise && len(perceptionResult.Noise.Suggestions) > 0 {
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"noise_detected": true,
			"suggestions":    perceptionResult.Noise.Suggestions,
		})
	}

	// ========== LAYER 2: COGNITION - Use LLM Planner for complex tasks ==========
	plan := a.cortexManager.Cognition.CreatePlan(input, perceptionResult)
	a.cortexManager.LastDecision = plan

	// For complex tasks, try LLM planner
	useLLMPlan := false
	var llmPlan *cognition.Decision
	if a.cortexManager.LLMPlanner != nil && (perceptionResult.Intent.Complexity == perception.ComplexityMedium || perceptionResult.Intent.Complexity == perception.ComplexityAdvanced) {
		decision, err := a.cortexManager.LLMPlanner.CreatePlan(ctx, input)
		if err == nil && decision != nil {
			llmPlan = decision
			useLLMPlan = true
		}
	}

	// Select which plan to use
	activePlan := plan
	if useLLMPlan && llmPlan != nil {
		activePlan = llmPlan
	}

	// Dynamically adjust maxIterations
	originalMaxTurns := a.maxTurns
	if activePlan.MaxTurns > 0 && activePlan.MaxTurns < a.maxTurns {
		a.maxTurns = activePlan.MaxTurns
	}

	// Apply tool filter
	originalTools := a.tools
	if len(activePlan.ToolFilter) > 0 {
		a.tools = a.filterTools(activePlan.ToolFilter)
	}

	// ========== CORTEX: Frozen Snapshot Memory ==========
	memoryPrompt := a.cortexManager.Snapshot.GetMemoryForPrompt()
	userPrompt := a.cortexManager.Snapshot.GetUserForPrompt()
	if a.memoryEnabled && memoryPrompt != "" {
		a.injectMemoryIntoSystemPrompt(memoryPrompt, userPrompt)
	}

	// ========== CORTEX: Context Compression ==========
	if a.cortexManager.ContextCompressor != nil {
		a.compressWithContextEngine()
	}

	// ========== LAYER 3: EXECUTION - Setup checkpoint ==========
	var checkpoint *execution.Checkpoint
	if activePlan.Plan != nil {
		checkpoint = a.cortexManager.Execution.StartCheckpoint("", activePlan.Plan)
		a.cortexManager.LastCheckpoint = checkpoint
	}

	// ========== CORTEX: Initialize trajectory recording ==========
	var trajectorySteps []cortex.TrajectoryStep
	trajectoryStartTime := time.Now()

	// ========== MAIN CONVERSATION LOOP ==========
	a.history = append(a.history, provider.Message{
		Role:    "user",
		Content: utils.TruncateDetailed(input, a.maxMsgLen),
	})
	a.truncateHistory()

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		a.cortexManager.OnTurnStart()

		if a.iterationCount >= a.maxIterations {
			lastErr = fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
			break
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			lastErr = fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
			break
		}

		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn":       a.iterationCount,
			"perception": perceptionResult.Intent.Type,
			"complexity": perceptionResult.Intent.Complexity,
		})

		// ========== LLM Call ==========
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.history,
			Tools:    a.tools,
		}

		req, hookDecision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("hook error: %w", err)
			continue
		}
		if hookDecision.Action == hooks.HookActionStop || hookDecision.Action == hooks.HookActionReject {
			lastErr = fmt.Errorf("hook rejected: %s", hookDecision.Reason)
			break
		}

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
		a.trackUsage(resp)

		llmResp := &hooks.LLMHookResponse{
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		llmResp, _, _ = a.hooks.AfterLLM(ctx, llmResp)

		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// ========== No tool calls - return response ==========
		if len(resp.ToolCalls) == 0 {
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:    "assistant",
				Content: content,
			})

			if checkpoint != nil {
				a.cortexManager.Execution.CompleteCheckpoint(checkpoint)
			}

			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// Set conversation history for memory extraction before OnSessionEnd
			conversationHistory := make([]struct {
				Role    string
				Content string
			}, len(a.history))
			for i, msg := range a.history {
				conversationHistory[i].Role = string(msg.Role)
				conversationHistory[i].Content = msg.Content
			}
			a.cortexManager.SetConversationHistory(conversationHistory)
			a.cortexManager.OnSessionEnd()

			// ========== CORTEX: Record trajectory (no tools) ==========
			a.recordTrajectory(input, content, trajectorySteps, trajectoryStartTime, lastErr == nil)

			a.maxTurns = originalMaxTurns
			a.tools = originalTools
			return resp.Content, nil
		}

		// ========== Execute tools ==========
		toolCalls := make([]types.ToolCall, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			toolCalls[i] = types.ToolCall{
				ID:        tc.ID,
				Name:      tc.GetToolName(),
				Arguments: tc.Arguments,
				Function:  tc.Function,
			}
		}

		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   utils.TruncateDetailed(resp.Content, a.maxMsgLen),
			ToolCalls: toolCalls,
		})

		results, err := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindToolError, err.Error())
			continue
		}

		// Cortex: Record each tool call for skill pattern detection
		for _, tc := range resp.ToolCalls {
			a.cortexManager.Trigger.OnToolCall(tc.GetToolName(), nil)
		}

		// ========== LAYER 3: Update checkpoint + record trajectory steps ==========
		if checkpoint != nil {
			for _, tc := range resp.ToolCalls {
				result := results[tc.ID]
				toolName := tc.GetToolName()
				a.cortexManager.Execution.StoreToolResult(checkpoint, toolName, result)

				validation := a.cortexManager.Execution.ValidateResult(checkpoint, toolName, result)
				if !validation.Passed {
					a.Emit(bus.EventKindWarning, map[string]interface{}{
						"validation_failed": true,
						"issues":            validation.Issues,
					})

					recovery := a.cortexManager.Execution.SuggestRecoveryAction(checkpoint, nil)
					switch recovery {
					case execution.RecoveryRetry:
						// Continue
					case execution.RecoveryAlternative:
						a.Emit(bus.EventKindWarning, map[string]interface{}{"recovery_action": "alternative"})
					case execution.RecoveryAskUser:
						content := "I encountered an issue. " + fmt.Sprintf("Issues: %v. What would you like me to do?", validation.Issues)
						a.history = append(a.history, provider.Message{Role: "assistant", Content: content})
						lastErr = fmt.Errorf("recovery needed")
						break
					}
				}

				// Record trajectory step
				stepResult := ""
				if result.Err != nil {
					stepResult = result.Err.Error()
				} else {
					stepResult = result.Content
				}
				toolInputStr := ""
				if argsJSON, err := json.Marshal(tc.Arguments); err == nil {
					toolInputStr = string(argsJSON)
				}
				trajectorySteps = append(trajectorySteps, cortex.TrajectoryStep{
					ToolName:   toolName,
					ToolInput:  toolInputStr,
					ToolOutput: stepResult,
					Success:    result.Err == nil,
					Timestamp:  time.Now(),
				})
			}
			a.cortexManager.Execution.UpdateTurnCount(checkpoint, a.iterationCount+1)
		}

		// Add tool results to history
		for _, tc := range resp.ToolCalls {
			result := results[tc.ID]
			var content string
			if result.Err != nil {
				content = utils.ErrTruncateWithSpill(result.Err, tc.GetToolName(), a.maxMsgLen)
				if checkpoint != nil {
					a.cortexManager.Execution.StoreError(checkpoint, tc.GetToolName(), result.Err)
				}
			} else {
				content = utils.TruncateWithSpill(result.Content, tc.GetToolName(), a.maxMsgLen)
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: tc.ID,
			})
		}

		// ========== CORTEX: Context compression after tool execution ==========
		if a.cortexManager.ContextCompressor != nil {
			a.compressWithContextEngine()
		}

		a.truncateHistory()
		a.Emit(bus.EventKindTurnEnd, nil)

		// Cortex: OnTurnEnd - feed tool calls into skill pattern detection
		a.cortexManager.OnTurnEnd()

		if lastErr != nil && lastErr.Error() == "recovery needed" {
			break
		}
	}

	// ========== Session end cleanup ==========
	a.Emit(bus.EventKindAgentEnd, nil)

	// Set conversation history for memory extraction before OnSessionEnd
	conversationHistory := make([]struct {
		Role    string
		Content string
	}, len(a.history))
	for i, msg := range a.history {
		conversationHistory[i].Role = string(msg.Role)
		conversationHistory[i].Content = msg.Content
	}
	a.cortexManager.SetConversationHistory(conversationHistory)
	a.cortexManager.OnSessionEnd()

	// ========== CORTEX: Record trajectory (with tools) ==========
	finalContent := ""
	if lastErr == nil && len(a.history) > 0 {
		finalContent = a.history[len(a.history)-1].Content
	}
	a.recordTrajectory(input, finalContent, trajectorySteps, trajectoryStartTime, lastErr == nil)

	a.maxTurns = originalMaxTurns
	a.tools = originalTools

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("exceeded maximum turns (%d)", a.maxTurns)
}

// ========== Cortex Integration Helper Methods ==========

// injectCortexContext injects SOUL.md personality and UserProfile into system prompt
func (a *Agent) injectCortexContext() {
	if a.cortexManager == nil {
		return
	}

	var injections []string

	// Inject SOUL.md personality
	if a.cortexManager.Soul != nil {
		soulPrompt := a.cortexManager.Soul.GetSoulForPrompt()
		if soulPrompt != "" {
			injections = append(injections, soulPrompt)
		}
	}

	// Inject UserProfile
	if a.cortexManager.UserProfile != nil {
		userProfile := a.cortexManager.UserProfile.GetForPrompt()
		if userProfile != "" {
			injections = append(injections, userProfile)
		}
	}

	// Inject into system message
	if len(injections) > 0 {
		for i, msg := range a.history {
			if msg.Role == "system" {
				for _, injection := range injections {
					a.history[i].Content += "\n\n" + injection
				}
				return
			}
		}
	}
}

// compressWithContextEngine uses Cortex ContextCompressor for intelligent compression
func (a *Agent) compressWithContextEngine() {
	if a.cortexManager == nil || a.cortexManager.ContextCompressor == nil {
		return
	}

	compressor := a.cortexManager.ContextCompressor
	if compressor.ShouldCompress(a.history) {
		result, err := compressor.Compress(context.Background(), a.history)
		if err == nil && result.Removed > 0 {
			a.history = result.Messages
			a.Emit(bus.EventKindWarning, map[string]interface{}{
				"compression": true,
				"removed":     result.Removed,
				"ratio":       result.Ratio,
			})
		}
	}
}

// recordTrajectory records the execution trajectory for GEPA learning
func (a *Agent) recordTrajectory(
	input string,
	output string,
	steps []cortex.TrajectoryStep,
	startTime time.Time,
	success bool,
) {
	if a.cortexManager == nil || a.cortexManager.TrajectoryStore == nil {
		return
	}

	trajectory := &cortex.Trajectory{
		Task:      input,
		Result:    output,
		Steps:     steps,
		Success:   success,
		StartTime: startTime,
		Model:     a.provider.Name(),
	}

	if err := a.cortexManager.TrajectoryStore.RecordTrajectory(trajectory); err == nil {
		a.Emit(bus.EventKindTurnEnd, map[string]interface{}{
			"trajectory_recorded": true,
			"trajectory_id":       trajectory.ID,
		})
	}
}

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
