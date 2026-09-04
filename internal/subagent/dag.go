package subagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// SubTaskNode represents a node in the task dependency graph
type SubTaskNode struct {
	ID           string
	Description  string
	Role         string   // researcher, coder, reviewer, planner, etc.
	Tools        []string // Tools available to this sub-agent
	Priority     int
	Dependencies []string // IDs of tasks that must complete first
	Status       SubTaskStatus
	Result       string
	Error        string
	Agent        *SubAgent
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	Retries      int
	MaxRetries   int
}

// SubTaskStatus represents the status of a sub-task
type SubTaskStatus string

const (
	SubTaskPending   SubTaskStatus = "pending"
	SubTaskReady     SubTaskStatus = "ready"
	SubTaskRunning   SubTaskStatus = "running"
	SubTaskCompleted SubTaskStatus = "completed"
	SubTaskFailed    SubTaskStatus = "failed"
	SubTaskSkipped   SubTaskStatus = "skipped"
)

// TaskDAG manages a directed acyclic graph of sub-tasks
type TaskDAG struct {
	mu    sync.RWMutex
	nodes map[string]*SubTaskNode
	order []string // Topological order
}

// NewTaskDAG creates a new task DAG
func NewTaskDAG() *TaskDAG {
	return &TaskDAG{
		nodes: make(map[string]*SubTaskNode),
		order: make([]string, 0),
	}
}

// AddNode adds a task node to the DAG
func (dag *TaskDAG) AddNode(node *SubTaskNode) error {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	if node.ID == "" {
		return fmt.Errorf("task node must have an ID")
	}
	if _, exists := dag.nodes[node.ID]; exists {
		return fmt.Errorf("task node %s already exists", node.ID)
	}

	node.Status = SubTaskPending
	dag.nodes[node.ID] = node
	dag.order = append(dag.order, node.ID)

	return nil
}

// GetNode gets a task node by ID
func (dag *TaskDAG) GetNode(id string) *SubTaskNode {
	dag.mu.RLock()
	defer dag.mu.RUnlock()
	return dag.nodes[id]
}

// GetReadyTasks returns tasks whose dependencies are all satisfied
func (dag *TaskDAG) GetReadyTasks() []*SubTaskNode {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	var ready []*SubTaskNode
	for _, id := range dag.order {
		node := dag.nodes[id]
		if node.Status != SubTaskPending {
			continue
		}

		allDepsComplete := true
		for _, depID := range node.Dependencies {
			depNode, exists := dag.nodes[depID]
			if !exists || depNode.Status != SubTaskCompleted {
				allDepsComplete = false
				break
			}
		}

		if allDepsComplete {
			ready = append(ready, node)
		}
	}

	// Sort by priority (higher first)
	for i := 0; i < len(ready)-1; i++ {
		for j := i + 1; j < len(ready); j++ {
			if ready[i].Priority < ready[j].Priority {
				ready[i], ready[j] = ready[j], ready[i]
			}
		}
	}

	return ready
}

// UpdateStatus updates the status of a task node
func (dag *TaskDAG) UpdateStatus(id string, status SubTaskStatus) error {
	dag.mu.Lock()
	defer dag.mu.Unlock()

	node, exists := dag.nodes[id]
	if !exists {
		return fmt.Errorf("task node %s not found", id)
	}

	node.Status = status
	if status == SubTaskCompleted || status == SubTaskFailed {
		node.EndTime = time.Now()
		node.Duration = node.EndTime.Sub(node.StartTime)
	}

	return nil
}

// IsComplete returns whether all tasks are done (completed or failed)
func (dag *TaskDAG) IsComplete() bool {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	for _, node := range dag.nodes {
		if node.Status == SubTaskPending || node.Status == SubTaskReady || node.Status == SubTaskRunning {
			return false
		}
	}
	return true
}

// GetProgress returns the overall progress (0.0 - 1.0)
func (dag *TaskDAG) GetProgress() float64 {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	if len(dag.nodes) == 0 {
		return 0
	}

	completed := 0
	for _, node := range dag.nodes {
		if node.Status == SubTaskCompleted {
			completed++
		}
	}

	return float64(completed) / float64(len(dag.nodes))
}

// GetResults returns all completed results in topological order
func (dag *TaskDAG) GetResults() map[string]string {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	results := make(map[string]string)
	for id, node := range dag.nodes {
		if node.Status == SubTaskCompleted {
			results[id] = node.Result
		}
	}
	return results
}

// GetFailedTasks returns all failed tasks
func (dag *TaskDAG) GetFailedTasks() []*SubTaskNode {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	var failed []*SubTaskNode
	for _, node := range dag.nodes {
		if node.Status == SubTaskFailed {
			failed = append(failed, node)
		}
	}
	return failed
}

// GetAllNodes returns all nodes in topological order
func (dag *TaskDAG) GetAllNodes() []*SubTaskNode {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	nodes := make([]*SubTaskNode, 0, len(dag.order))
	for _, id := range dag.order {
		nodes = append(nodes, dag.nodes[id])
	}
	return nodes
}

// ResultSynthesizer synthesizes results from multiple sub-agents
type ResultSynthesizer struct {
	provider provider.Provider
}

// NewResultSynthesizer creates a new result synthesizer
func NewResultSynthesizer(prov provider.Provider) *ResultSynthesizer {
	return &ResultSynthesizer{
		provider: prov,
	}
}

// SynthesizeResults combines results from multiple sub-tasks into a coherent final result
func (rs *ResultSynthesizer) SynthesizeResults(ctx context.Context, goal string, results map[string]string, failedTasks []*SubTaskNode) (string, error) {
	if rs.provider == nil {
		return rs.simpleSynthesize(goal, results, failedTasks), nil
	}

	// Build prompt with all results
	var resultsBuilder strings.Builder
	resultsBuilder.WriteString(fmt.Sprintf("Overall Goal: %s\n\n", goal))
	resultsBuilder.WriteString("Sub-task Results:\n")

	i := 1
	for taskID, result := range results {
		resultsBuilder.WriteString(fmt.Sprintf("\n--- Sub-task %d: %s ---\n", i, taskID))
		if len(result) > 2000 {
			// NOTE: avoid square brackets — this text reaches the LLM and
			// GLM mimics bracketed markers as a structural template.
			// rune 安全截断：字节截断会把中文切成乱码（U+FFFD）
			r := []rune(result)
			resultsBuilder.WriteString(string(r[:2000]) + "\n... (truncated)")
		} else {
			resultsBuilder.WriteString(result)
		}
		resultsBuilder.WriteString("\n")
		i++
	}

	if len(failedTasks) > 0 {
		resultsBuilder.WriteString("\nFailed Sub-tasks:\n")
		for _, task := range failedTasks {
			resultsBuilder.WriteString(fmt.Sprintf("- %s: %s\n", task.ID, task.Error))
		}
	}

	prompt := fmt.Sprintf(`You are a result synthesis expert. Combine the results from multiple sub-tasks into a single, coherent final answer.

%s

Please provide:
1. A comprehensive summary of what was accomplished
2. The key findings or outputs from each sub-task
3. How the results relate to and support each other
4. Any gaps or limitations (from failed tasks)
5. A clear final answer to the original goal

Structure the response clearly with headings. Make sure the final result is greater than the sum of its parts - identify connections and insights that emerge from combining the results.`, resultsBuilder.String())

	resp, err := rs.provider.Chat(ctx, []provider.Message{
		{Role: "system", Content: "You are an expert at synthesizing information from multiple sources into coherent, insightful answers."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Warnf("[SubAgent] LLM synthesis failed, using simple synthesis: %v", err)
		return rs.simpleSynthesize(goal, results, failedTasks), nil
	}

	return resp.Content, nil
}

func (rs *ResultSynthesizer) simpleSynthesize(goal string, results map[string]string, failedTasks []*SubTaskNode) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Results for: %s\n\n", goal))
	sb.WriteString(fmt.Sprintf("Completed %d sub-tasks", len(results)))
	if len(failedTasks) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d failed)", len(failedTasks)))
	}
	sb.WriteString("\n\n")

	i := 1
	for taskID, result := range results {
		sb.WriteString(fmt.Sprintf("## Sub-task %d: %s\n\n", i, taskID))
		sb.WriteString(result)
		sb.WriteString("\n\n")
		i++
	}

	if len(failedTasks) > 0 {
		sb.WriteString("## Failed Tasks\n\n")
		for _, task := range failedTasks {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", task.ID, task.Error))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// DAGExecutor executes a task DAG with configurable concurrency
type DAGExecutor struct {
	dag         *TaskDAG
	provider    provider.Provider
	synthesizer *ResultSynthesizer

	// Configuration
	MaxConcurrency int
	Timeout        time.Duration
}

// DAGExecutorConfig configures the DAG executor
type DAGExecutorConfig struct {
	MaxConcurrency int
	Timeout        time.Duration
}

// DefaultDAGExecutorConfig returns default config
func DefaultDAGExecutorConfig() DAGExecutorConfig {
	return DAGExecutorConfig{
		MaxConcurrency: 3,
		Timeout:        10 * time.Minute,
	}
}

// NewDAGExecutor creates a new DAG executor
func NewDAGExecutor(dag *TaskDAG, prov provider.Provider, cfg DAGExecutorConfig) *DAGExecutor {
	return &DAGExecutor{
		dag:            dag,
		provider:       prov,
		synthesizer:    NewResultSynthesizer(prov),
		MaxConcurrency: cfg.MaxConcurrency,
		Timeout:        cfg.Timeout,
	}
}

// Execute executes all tasks in the DAG respecting dependencies
func (exec *DAGExecutor) Execute(ctx context.Context, goal string, createAgent func(role string, tools []string) *SubAgent) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, exec.Timeout)
	defer cancel()

	sem := make(chan struct{}, exec.MaxConcurrency)
	var wg sync.WaitGroup

	// Continue until all tasks are done or context is cancelled
	for !exec.dag.IsComplete() {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("DAG execution cancelled: %w", ctx.Err())
		default:
		}

		readyTasks := exec.dag.GetReadyTasks()
		if len(readyTasks) == 0 {
			// No ready tasks but not complete - wait for running tasks
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Start ready tasks (respecting concurrency limit)
		for _, task := range readyTasks {
			select {
			case sem <- struct{}{}:
				// Got a slot, start the task
				wg.Add(1)
				go func(t *SubTaskNode) {
					defer wg.Done()
					defer func() { <-sem }()

					exec.runTask(ctx, t, goal, createAgent)
				}(task)

				// Mark as running immediately
				exec.dag.UpdateStatus(task.ID, SubTaskRunning)
				task.StartTime = time.Now()

			default:
				// No slots available, skip for now
				break
			}
		}

		// Small delay to avoid busy waiting
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Synthesize results
	results := exec.dag.GetResults()
	failed := exec.dag.GetFailedTasks()

	finalResult, err := exec.synthesizer.SynthesizeResults(ctx, goal, results, failed)
	if err != nil {
		return "", err
	}

	return finalResult, nil
}

func (exec *DAGExecutor) runTask(ctx context.Context, task *SubTaskNode, goal string, createAgent func(role string, tools []string) *SubAgent) {
	if createAgent == nil {
		exec.dag.UpdateStatus(task.ID, SubTaskFailed)
		task.Error = "no agent factory provided"
		return
	}

	agent := createAgent(task.Role, task.Tools)
	if agent == nil {
		exec.dag.UpdateStatus(task.ID, SubTaskFailed)
		task.Error = "failed to create sub-agent"
		return
	}

	// Build task-specific prompt
	taskPrompt := fmt.Sprintf(`You are working on sub-task: %s

This is part of the larger goal: %s

Complete your assigned sub-task to the best of your ability. Provide detailed results that can be combined with other sub-task results later.`, task.Description, goal)

	result, err := agent.Run(ctx, taskPrompt)
	if err != nil {
		task.Retries++
		if task.Retries < task.MaxRetries {
			// Retry
			log.Infof("[DAGExecutor] Retrying task %s (attempt %d/%d)", task.ID, task.Retries, task.MaxRetries)
			exec.dag.UpdateStatus(task.ID, SubTaskPending)
			return
		}
		exec.dag.UpdateStatus(task.ID, SubTaskFailed)
		task.Error = err.Error()
		log.Warnf("[DAGExecutor] Task %s failed after %d retries: %v", task.ID, task.Retries, err)
		return
	}

	task.Result = result
	exec.dag.UpdateStatus(task.ID, SubTaskCompleted)
	log.Infof("[DAGExecutor] Task %s completed in %v", task.ID, task.Duration)
}

// BuildDAGFromPlan builds a task DAG from a plan description
func BuildDAGFromPlan(planDescription string, tasks []struct {
	ID          string
	Description string
	Role        string
	Tools       []string
	Deps        []string
	Priority    int
}) (*TaskDAG, error) {
	dag := NewTaskDAG()

	for _, t := range tasks {
		node := &SubTaskNode{
			ID:           t.ID,
			Description:  t.Description,
			Role:         t.Role,
			Tools:        t.Tools,
			Priority:     t.Priority,
			Dependencies: t.Deps,
			MaxRetries:   2,
		}
		if err := dag.AddNode(node); err != nil {
			return nil, fmt.Errorf("failed to add node %s: %w", t.ID, err)
		}
	}

	return dag, nil
}

// GenerateDAGSummary generates a text summary of the DAG status
func (dag *TaskDAG) GenerateDAGSummary() string {
	dag.mu.RLock()
	defer dag.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("=== Task DAG Status ===\n\n")

	for i, id := range dag.order {
		node := dag.nodes[id]
		var statusIcon string
		switch node.Status {
		case SubTaskCompleted:
			statusIcon = "✅"
		case SubTaskFailed:
			statusIcon = "❌"
		case SubTaskRunning:
			statusIcon = "🔄"
		case SubTaskReady:
			statusIcon = "⏳"
		default:
			statusIcon = "📋"
		}

		sb.WriteString(fmt.Sprintf("%s %d. %s [%s]", statusIcon, i+1, node.ID, node.Status))
		if len(node.Dependencies) > 0 {
			sb.WriteString(fmt.Sprintf(" (depends on: %s)", strings.Join(node.Dependencies, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\nProgress: %.0f%% (%d/%d completed)\n",
		dag.GetProgress()*100,
		int(dag.GetProgress()*float64(len(dag.nodes))),
		len(dag.nodes)))

	return sb.String()
}
