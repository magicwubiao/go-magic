package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// SubTask represents a sub-task to be executed
type SubTask struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Goal        string    `json:"goal"`
	Status      string    `json:"status"` // pending, running, completed, failed
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
}

// SubTaskManager manages sub-task decomposition and execution
type SubTaskManager struct {
	mu       sync.Mutex
	tasks    []SubTask
	provider provider.Provider
	registry ToolRegistry
	tools    []map[string]interface{}
	maxTurns int
}

// NewSubTaskManager creates a new SubTaskManager
func NewSubTaskManager(prov provider.Provider, registry ToolRegistry, tools []map[string]interface{}) *SubTaskManager {
	return &SubTaskManager{
		tasks:    make([]SubTask, 0),
		provider: prov,
		registry: registry,
		tools:    tools,
		maxTurns: 30, // subtasks get more turns for complex work
	}
}

// DecomposeTask analyzes a complex task and breaks it into sub-tasks
func (stm *SubTaskManager) DecomposeTask(ctx context.Context, taskDescription string) ([]SubTask, error) {
	stm.mu.Lock()
	defer stm.mu.Unlock()

	// Build prompt for task decomposition
	decompositionPrompt := fmt.Sprintf(`You are a task decomposition expert. Your job is to break down complex tasks into clear, actionable sub-tasks.

Given the following task, break it down into 2-5 sub-tasks that can be executed independently.
Each sub-task should have a clear goal and description.

Task: %s

Respond with a JSON array of objects, each with:
- "id": A unique identifier (e.g., "subtask-1", "subtask-2")
- "title": Short title of the sub-task
- "description": Brief description of what needs to be done
- "goal": The specific goal/acceptance criteria for this sub-task

Return ONLY the JSON array, no other text.`, taskDescription)

	messages := []provider.Message{
		{Role: "system", Content: decompositionPrompt},
	}

	// Call provider to decompose
	var resp *provider.ChatResponse
	var err error

	type openAIlike interface {
		ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
	}
	if oa, ok := stm.provider.(openAIlike); ok {
		resp, err = oa.ChatWithTools(ctx, messages, nil)
	} else {
		resp, err = stm.provider.Chat(ctx, messages)
	}

	if err != nil {
		return nil, fmt.Errorf("task decomposition failed: %w", err)
	}

	// Parse the response as JSON
	var subTasks []SubTask
	content := resp.Content

	// Try to find and extract JSON from the response
	jsonStart := -1
	jsonEnd := -1
	for i := 0; i < len(content); i++ {
		if content[i] == '[' {
			jsonStart = i
			break
		}
	}
	if jsonStart >= 0 {
		for i := len(content) - 1; i >= jsonStart; i-- {
			if content[i] == ']' {
				jsonEnd = i + 1
				break
			}
		}
	}

	if jsonStart >= 0 && jsonEnd > jsonStart {
		content = content[jsonStart:jsonEnd]
	}

	if err := json.Unmarshal([]byte(content), &subTasks); err != nil {
		// Fallback: try to parse as a single task
		return []SubTask{{
			ID:          "subtask-1",
			Title:       taskDescription,
			Description: taskDescription,
			Goal:        taskDescription,
			Status:      "pending",
		}}, nil
	}

	// Initialize status for all tasks
	for i := range subTasks {
		subTasks[i].Status = "pending"
		if subTasks[i].ID == "" {
			subTasks[i].ID = fmt.Sprintf("subtask-%d", i+1)
		}
	}

	stm.tasks = subTasks
	return subTasks, nil
}

// ExecuteSubTask executes a single sub-task and returns the result
func (stm *SubTaskManager) ExecuteSubTask(ctx context.Context, task SubTask) (string, error) {
	stm.mu.Lock()
	// Update task status
	for i := range stm.tasks {
		if stm.tasks[i].ID == task.ID {
			stm.tasks[i].Status = "running"
			stm.tasks[i].StartedAt = time.Now()
			break
		}
	}
	stm.mu.Unlock()

	// Create a focused agent for this sub-task
	subPrompt := fmt.Sprintf(`You are working on a specific sub-task. Focus ONLY on this task and complete it efficiently.

Sub-Task: %s
Goal: %s

Complete this sub-task using the available tools. Be focused and direct.`, task.Title, task.Goal)

	subHistory := []provider.Message{
		{Role: "system", Content: subPrompt},
		{Role: "user", Content: fmt.Sprintf("Please complete this sub-task: %s\n\nGoal: %s", task.Description, task.Goal)},
	}

	var lastResult string
	var lastErr error

	for turn := 0; turn < stm.maxTurns; turn++ {
		// Call provider
		var resp *provider.ChatResponse
		var err error

		type openAIlike interface {
			ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
		}
		if oa, ok := stm.provider.(openAIlike); ok && len(stm.tools) > 0 {
			resp, err = oa.ChatWithTools(ctx, subHistory, stm.tools)
		} else {
			resp, err = stm.provider.Chat(ctx, subHistory)
		}

		if err != nil {
			lastErr = err
			break
		}

		// No tool calls - task is complete
		if len(resp.ToolCalls) == 0 {
			lastResult = resp.Content
			// Update task status
			stm.mu.Lock()
			for i := range stm.tasks {
				if stm.tasks[i].ID == task.ID {
					stm.tasks[i].Status = "completed"
					stm.tasks[i].Result = lastResult
					stm.tasks[i].FinishedAt = time.Now()
					break
				}
			}
			stm.mu.Unlock()
			return lastResult, nil
		}

		// Add assistant message with tool calls
		subHistory = append(subHistory, provider.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tools
		for _, tc := range resp.ToolCalls {
			toolName := tc.GetToolName()
			var toolArgs map[string]interface{}
			if tc.Function.Arguments != "" {
				json.Unmarshal([]byte(tc.Function.Arguments), &toolArgs)
			} else if tc.Arguments != nil {
				toolArgs = tc.Arguments
			}

			result, err := stm.registry.Execute(ctx, toolName, toolArgs)
			content := ""
			if err != nil {
				content = fmt.Sprintf("Error: %v", err)
			} else if result != nil {
				if s, ok := result.(string); ok {
					content = s
				} else if b, jsonErr := json.Marshal(result); jsonErr == nil {
					content = string(b)
				} else {
					content = fmt.Sprintf("%v", result)
				}
			}

			subHistory = append(subHistory, provider.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: tc.ID,
			})
		}
	}

	// If we get here, the sub-task wasn't completed
	stm.mu.Lock()
	for i := range stm.tasks {
		if stm.tasks[i].ID == task.ID {
			stm.tasks[i].Status = "failed"
			if lastErr != nil {
				stm.tasks[i].Error = lastErr.Error()
			} else {
				stm.tasks[i].Error = "exceeded maximum turns"
			}
			stm.tasks[i].Result = lastResult
			stm.tasks[i].FinishedAt = time.Now()
			break
		}
	}
	stm.mu.Unlock()

	if lastErr != nil {
		return lastResult, fmt.Errorf("sub-task failed: %w", lastErr)
	}
	return lastResult, fmt.Errorf("sub-task exceeded maximum turns")
}

// GetSubTaskSummary returns a summary of all sub-tasks and their results
func (stm *SubTaskManager) GetSubTaskSummary() string {
	stm.mu.Lock()
	defer stm.mu.Unlock()

	if len(stm.tasks) == 0 {
		return ""
	}

	var summary strings.Builder
	summary.WriteString("\n\n[Sub-Task Execution Summary]\n")
	for _, task := range stm.tasks {
		statusEmoji := map[string]string{
			"completed": "✅",
			"failed":    "❌",
			"running":   "🔄",
			"pending":   "⏳",
		}
		emoji := statusEmoji[task.Status]
		if emoji == "" {
			emoji = "❓"
		}

		summary.WriteString(fmt.Sprintf("%s Sub-Task: %s\n", emoji, task.Title))
		summary.WriteString(fmt.Sprintf("   Status: %s\n", task.Status))
		if task.Result != "" {
			// Truncate long results
			result := task.Result
			if len(result) > 200 {
				result = result[:200] + "..."
			}
			summary.WriteString(fmt.Sprintf("   Result: %s\n", result))
		}
		if task.Error != "" {
			summary.WriteString(fmt.Sprintf("   Error: %s\n", task.Error))
		}
		summary.WriteString("\n")
	}

	return summary.String()
}

// ComplexityAnalyzer analyzes task complexity and determines if decomposition is needed
type ComplexityAnalyzer struct {
	// Thresholds for complexity scoring
	lengthThreshold    int
	multiStepThreshold int
	toolCountThreshold int
}

// NewComplexityAnalyzer creates a new complexity analyzer
func NewComplexityAnalyzer() *ComplexityAnalyzer {
	return &ComplexityAnalyzer{
		lengthThreshold:    200, // characters in task description
		multiStepThreshold: 3,   // steps mentioned
		toolCountThreshold: 3,   // tools likely needed
	}
}

// AnalyzeComplexity analyzes a task and returns a complexity score (0.0 - 1.0)
func (ca *ComplexityAnalyzer) AnalyzeComplexity(task string) float64 {
	score := 0.0

	// Factor 1: Length of description
	if len(task) > ca.lengthThreshold {
		score += 0.2
	}
	if len(task) > ca.lengthThreshold*2 {
		score += 0.1
	}

	// Factor 2: Multi-step indicators
	stepIndicators := []string{
		"first", "then", "next", "after", "finally",
		"步骤", "首先", "然后", "接着", "最后",
		"step 1", "step 2", "step 3",
		"1.", "2.", "3.",
	}
	stepCount := 0
	taskLower := strings.ToLower(task)
	for _, indicator := range stepIndicators {
		if strings.Contains(taskLower, indicator) {
			stepCount++
		}
	}
	if stepCount >= ca.multiStepThreshold {
		score += 0.3
	} else if stepCount > 0 {
		score += 0.15
	}

	// Factor 3: Tool mentions
	toolIndicators := []string{
		"file", "search", "execute", "write", "read", "create",
		"文件", "搜索", "执行", "编写", "读取", "创建",
		"api", "database", "test", "deploy", "build",
	}
	toolCount := 0
	for _, indicator := range toolIndicators {
		if strings.Contains(taskLower, indicator) {
			toolCount++
		}
	}
	if toolCount >= ca.toolCountThreshold {
		score += 0.3
	} else if toolCount > 0 {
		score += 0.15
	}

	// Factor 4: Complex action verbs
	complexVerbs := []string{
		"refactor", "migrate", "implement", "integrate",
		"重构", "迁移", "实现", "集成",
		"optimize", "redesign", "restructure",
	}
	for _, verb := range complexVerbs {
		if strings.Contains(taskLower, verb) {
			score += 0.1
			break
		}
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// ShouldDecompose returns true if the task should be decomposed
func (ca *ComplexityAnalyzer) ShouldDecompose(task string) bool {
	score := ca.AnalyzeComplexity(task)
	// Decompose if complexity is medium-high (>= 0.4)
	return score >= 0.4
}

// ====== Sub-Task Execution Integration in Agent ======

// subtaskManager is the global sub-task manager instance (deprecated, use Agent.SubTaskManager)
var (
	globalSubTaskManager *SubTaskManager
	subTaskMu            sync.Mutex
)

// GetSubTaskManager returns or creates the global SubTaskManager (deprecated)
func (a *Agent) GetSubTaskManager() *SubTaskManager {
	subTaskMu.Lock()
	defer subTaskMu.Unlock()

	if globalSubTaskManager == nil {
		globalSubTaskManager = NewSubTaskManager(a.provider, a.registry, a.tools)
	}
	return globalSubTaskManager
}

// subTaskManagerInstance returns a per-agent sub-task manager
func (a *Agent) subTaskManagerInstance() *SubTaskManager {
	return NewSubTaskManager(a.provider, a.registry, a.tools)
}

// ExecuteComplexTask decomposes a complex task, executes sub-tasks, and returns aggregated results
func (a *Agent) ExecuteComplexTask(ctx context.Context, taskDescription string) (string, error) {
	analyzer := NewComplexityAnalyzer()
	score := analyzer.AnalyzeComplexity(taskDescription)

	// If complexity is low, just return empty to indicate no decomposition needed
	if !analyzer.ShouldDecompose(taskDescription) {
		return "", nil
	}

	stm := a.subTaskManagerInstance()

	// Step 1: Decompose the task
	subTasks, err := stm.DecomposeTask(ctx, taskDescription)
	if err != nil {
		return "", fmt.Errorf("task decomposition failed: %w", err)
	}

	if len(subTasks) == 0 {
		return "", nil
	}

	// Step 2: Execute sub-tasks
	var results []string
	for _, task := range subTasks {
		result, err := stm.ExecuteSubTask(ctx, task)
		if err != nil {
			results = append(results, fmt.Sprintf("[%s] Failed: %v", task.Title, err))
		} else {
			results = append(results, fmt.Sprintf("[%s] Completed: %s", task.Title, result))
		}
	}

	// Step 3: Build summary
	summary := stm.GetSubTaskSummary()

	// Build aggregated response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✅ **Complex Task Analysis Complete**\n"))
	sb.WriteString(fmt.Sprintf("Complexity Score: %.0f%%\n\n", score*100))
	sb.WriteString(fmt.Sprintf("The task was decomposed into %d sub-tasks:\n\n", len(subTasks)))

	for i, result := range results {
		sb.WriteString(fmt.Sprintf("**Sub-Task %d:**\n", i+1))
		sb.WriteString(result)
		sb.WriteString("\n\n")
	}

	sb.WriteString(summary)

	return sb.String(), nil
}

// RunConversationWithSubTask is an enhanced RunConversation that automatically
// delegates complex sub-tasks to the sub-task executor
func (a *Agent) RunConversationWithSubTask(ctx context.Context, input string) (string, error) {
	// First, check if this is a complex task that should be decomposed
	if result, err := a.ExecuteComplexTask(ctx, input); err == nil && result != "" {
		// Add the sub-task results to conversation history
		a.history = append(a.history,
			provider.Message{Role: "user", Content: input},
			provider.Message{Role: "assistant", Content: result + "\n\nBased on the sub-task results above, here is my complete response:"},
		)

		// Continue with normal conversation to get final synthesis
		// Build a synthesis message with the sub-task results
		synthesisInput := fmt.Sprintf(`I have completed the sub-tasks for: %s

The sub-tasks execution results are:
%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`,
			input, result)

		return a.RunConversation(ctx, synthesisInput)
	}

	// For simple tasks, run normally
	return a.RunConversation(ctx, input)
}
