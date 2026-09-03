package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/complexity"
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
		jsonContent := content[jsonStart:jsonEnd]
		if err := json.Unmarshal([]byte(jsonContent), &subTasks); err != nil {
			return nil, fmt.Errorf("failed to parse sub-tasks JSON: %w", err)
		}
	} else {
		// Fallback: try parsing entire content
		if err := json.Unmarshal([]byte(content), &subTasks); err != nil {
			return nil, fmt.Errorf("failed to parse sub-tasks: %w", err)
		}
	}

	// Set initial status
	for i := range subTasks {
		subTasks[i].Status = "pending"
	}

	stm.tasks = subTasks
	return subTasks, nil
}

// ExecuteSubTask executes a single sub-task
func (stm *SubTaskManager) ExecuteSubTask(ctx context.Context, subTask *SubTask) error {
	stm.mu.Lock()
	defer stm.mu.Unlock()

	subTask.Status = "running"
	subTask.StartedAt = time.Now()

	// Create a simple agent for this subtask
	agent := NewAIAgent(stm.provider, stm.registry, stm.tools, "")

	// Execute the subtask
	result, err := agent.RunConversation(ctx, subTask.Description)
	if err != nil {
		subTask.Status = "failed"
		subTask.Error = err.Error()
		subTask.FinishedAt = time.Now()
		return err
	}

	subTask.Status = "completed"
	subTask.Result = result
	subTask.FinishedAt = time.Now()
	return nil
}

// ExecuteAll executes all sub-tasks sequentially
func (stm *SubTaskManager) ExecuteAll(ctx context.Context) ([]SubTask, error) {
	for i := range stm.tasks {
		if err := stm.ExecuteSubTask(ctx, &stm.tasks[i]); err != nil {
			// Continue with other tasks even if one fails
			continue
		}
	}
	return stm.tasks, nil
}

// GetTasks returns all tasks
func (stm *SubTaskManager) GetTasks() []SubTask {
	stm.mu.Lock()
	defer stm.mu.Unlock()
	return stm.tasks
}

// ====== Complexity Analysis (delegates to complexity package) ======

// ComplexityAnalyzer is an alias for complexity.Analyzer for backward compatibility
type ComplexityAnalyzer = complexity.Analyzer

// ComplexityScore is an alias for complexity.Score for backward compatibility
type ComplexityScore = complexity.Score

// NewComplexityAnalyzer creates a new complexity analyzer
// This is a wrapper around complexity.NewAnalyzer() for backward compatibility
func NewComplexityAnalyzer() *ComplexityAnalyzer {
	return complexity.NewAnalyzer()
}

// ====== Sub-Task Execution Integration in Agent ======

// subTaskManagerInstance returns a per-agent sub-task manager
func (a *Agent) subTaskManagerInstance() *SubTaskManager {
	return NewSubTaskManager(a.provider, a.registry, a.tools)
}

// ExecuteComplexTask decomposes a complex task, executes sub-tasks, and returns aggregated results
func (a *Agent) ExecuteComplexTask(ctx context.Context, taskDescription string) (string, error) {
	analyzer := complexity.NewAnalyzer()
	score := analyzer.Analyze(taskDescription)

	// If complexity is low, just return empty to indicate no decomposition needed
	if !score.ShouldSplit {
		return "", nil
	}

	// Create sub-task manager
	stm := a.subTaskManagerInstance()

	// Decompose task
	subTasks, err := stm.DecomposeTask(ctx, taskDescription)
	if err != nil {
		return "", fmt.Errorf("failed to decompose task: %w", err)
	}

	if len(subTasks) == 0 {
		return "", nil
	}

	// Execute all sub-tasks
	completedTasks, err := stm.ExecuteAll(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to execute sub-tasks: %w", err)
	}

	// Aggregate results
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Task decomposed into %d sub-tasks:\n\n", len(completedTasks)))

	for _, task := range completedTasks {
		result.WriteString(fmt.Sprintf("## %s\n", task.Title))
		result.WriteString(fmt.Sprintf("Status: %s\n", task.Status))
		if task.Result != "" {
			result.WriteString(fmt.Sprintf("Result: %s\n", task.Result))
		}
		if task.Error != "" {
			result.WriteString(fmt.Sprintf("Error: %s\n", task.Error))
		}
		result.WriteString("\n")
	}

	return result.String(), nil
}
