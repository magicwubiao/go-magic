package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manager handles batch processing operations
type Manager struct {
	config     *Config
	jobs       map[string]*Job
	jobsMu     sync.RWMutex
	resultChan chan *JobResult
	stopChan   chan struct{}
}

// Config holds batch configuration
type Config struct {
	MaxConcurrent int           // Max parallel jobs
	RetryCount    int           // Number of retries on failure
	RetryDelay    time.Duration // Delay between retries
	ResultDir     string        // Directory to save results
	ProgressDir   string        // Directory to save progress
	Timeout       time.Duration // Job timeout
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrent: 5,
		RetryCount:    3,
		RetryDelay:    5 * time.Second,
		ResultDir:     filepath.Join(os.Getenv("HOME"), ".magic", "batch-results"),
		ProgressDir:   filepath.Join(os.Getenv("HOME"), ".magic", "batch-progress"),
		Timeout:       1 * time.Hour,
	}
}

// Job represents a batch job
type Job struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Type           string                 `json:"type"` // trajectory, query, skill
	InputFile      string                 `json:"input_file"`
	OutputFile     string                 `json:"output_file"`
	Params         map[string]interface{} `json:"params"`
	Status         string                 `json:"status"`   // pending, running, completed, failed
	Progress       float64                `json:"progress"` // 0-100
	CreatedAt      time.Time              `json:"created_at"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	Error          string                 `json:"error,omitempty"`
	RetryCount     int                    `json:"retry_count"`
	TotalItems     int                    `json:"total_items"`
	ProcessedItems int                    `json:"processed_items"`
}

// JobResult contains job execution results
type JobResult struct {
	JobID       string         `json:"job_id"`
	Success     bool           `json:"success"`
	OutputFile  string         `json:"output_file"`
	Results     []TaskResult   `json:"results,omitempty"`
	Errors      []TaskError    `json:"errors,omitempty"`
	Summary     *ResultSummary `json:"summary,omitempty"`
	Duration    time.Duration  `json:"duration"`
	ProcessedAt time.Time      `json:"processed_at"`
}

// TaskResult represents a single task result
type TaskResult struct {
	TaskID   string                 `json:"task_id"`
	Input    interface{}            `json:"input"`
	Output   interface{}            `json:"output"`
	Duration time.Duration          `json:"duration"`
	Success  bool                   `json:"success"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TaskError represents a task error
type TaskError struct {
	TaskID string      `json:"task_id"`
	Error  string      `json:"error"`
	Input  interface{} `json:"input"`
}

// ResultSummary summarizes batch results
type ResultSummary struct {
	TotalTasks      int           `json:"total_tasks"`
	Successful      int           `json:"successful"`
	Failed          int           `json:"failed"`
	SuccessRate     float64       `json:"success_rate"`
	TotalDuration   time.Duration `json:"total_duration"`
	AvgTaskDuration time.Duration `json:"avg_task_duration"`
	TotalTokens     int64         `json:"total_tokens"`
	TotalCost       float64       `json:"total_cost"`
}

// TrajectoryJob represents a trajectory generation job
type TrajectoryJob struct {
	Job
	Tasks []TrajectoryTask `json:"tasks"`
}

// TrajectoryTask is a single trajectory generation task
type TrajectoryTask struct {
	TaskID        string           `json:"task_id"`
	Query         string           `json:"query"`
	SystemPrompt  string           `json:"system_prompt,omitempty"`
	Tools         []string         `json:"tools,omitempty"`
	MaxSteps      int              `json:"max_steps,omitempty"`
	ExpectedTools []string         `json:"expected_tools,omitempty"`
	Trajectory    []TrajectoryStep `json:"trajectory,omitempty"`
	Success       bool             `json:"success"`
	Error         string           `json:"error,omitempty"`
}

// TrajectoryStep represents a single step in trajectory
type TrajectoryStep struct {
	Step       int        `json:"step"`
	Role       string     `json:"role"` // user, assistant, tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolResult string     `json:"tool_result,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
}

// ToolCall represents a tool call in trajectory
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// QueryJob represents a batch query job
type QueryJob struct {
	Job
	Tasks []QueryTask `json:"tasks"`
}

// QueryTask is a single query task
type QueryTask struct {
	TaskID   string  `json:"task_id"`
	Query    string  `json:"query"`
	Response string  `json:"response,omitempty"`
	Tokens   int     `json:"tokens"`
	Cost     float64 `json:"cost"`
	Success  bool    `json:"success"`
	Error    string  `json:"error,omitempty"`
}

// NewManager creates a new batch manager
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	// Ensure directories exist
	os.MkdirAll(config.ResultDir, 0755)
	os.MkdirAll(config.ProgressDir, 0755)

	return &Manager{
		config:     config,
		jobs:       make(map[string]*Job),
		resultChan: make(chan *JobResult, 100),
		stopChan:   make(chan struct{}),
	}
}

// CreateJob creates a new batch job
func (m *Manager) CreateJob(jobType string, inputFile string, params map[string]interface{}) (*Job, error) {
	job := &Job{
		ID:        fmt.Sprintf("job_%d", time.Now().UnixNano()),
		Type:      jobType,
		InputFile: inputFile,
		Params:    params,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	// Set output file
	_ = filepath.Ext(inputFile)
	job.OutputFile = filepath.Join(m.config.ResultDir, job.ID+".json")

	m.jobsMu.Lock()
	m.jobs[job.ID] = job
	m.jobsMu.Unlock()

	return job, nil
}

// GetJob returns a job by ID
func (m *Manager) GetJob(id string) *Job {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()
	return m.jobs[id]
}

// ListJobs returns all jobs
func (m *Manager) ListJobs() []*Job {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// RunJob executes a job
func (m *Manager) RunJob(ctx context.Context, jobID string) (*JobResult, error) {
	job := m.GetJob(jobID)
	if job == nil {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = "running"
	now := time.Now()
	job.StartedAt = &now

	// Load input file
	input, err := m.loadInput(job.InputFile, job.Type)
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		return nil, err
	}

	// Execute based on type
	var result *JobResult
	switch job.Type {
	case "trajectory":
		result, err = m.runTrajectoryJob(ctx, job, input)
	case "query":
		result, err = m.runQueryJob(ctx, job, input)
	default:
		result, err = m.runGenericJob(ctx, job, input)
	}

	// Update job status
	job.Status = "completed"
	completed := time.Now()
	job.CompletedAt = &completed

	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
	}

	// Save result
	if result != nil {
		m.saveResult(result)
	}

	return result, err
}

// runTrajectoryJob executes a trajectory generation job
func (m *Manager) runTrajectoryJob(ctx context.Context, job *Job, input interface{}) (*JobResult, error) {
	result := &JobResult{
		JobID:       job.ID,
		ProcessedAt: time.Now(),
		Results:     make([]TaskResult, 0),
		Errors:      make([]TaskError, 0),
	}

	startTime := time.Now()

	// Type assert input
	tasks, ok := input.([]TrajectoryTask)
	if !ok {
		return nil, fmt.Errorf("invalid trajectory input format")
	}

	job.TotalItems = len(tasks)
	totalTokens := int64(0)
	totalCost := 0.0

	// Process tasks with concurrency limit
	sem := make(chan struct{}, m.config.MaxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, task := range tasks {
		wg.Add(1)
		go func(t TrajectoryTask, idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			taskResult := m.executeTrajectoryTask(ctx, t)

			mu.Lock()
			if taskResult.Success {
				result.Results = append(result.Results, taskResult)
			} else {
				result.Errors = append(result.Errors, TaskError{
					TaskID: t.TaskID,
					Error:  taskResult.Output.(string),
					Input:  t.Query,
				})
			}
			totalTokens += taskResult.Metadata["tokens"].(int64)
			totalCost += taskResult.Metadata["cost"].(float64)
			job.ProcessedItems = idx + 1
			job.Progress = float64(job.ProcessedItems) / float64(job.TotalItems) * 100
			m.saveProgress(job)
			mu.Unlock()
		}(task, i)
	}

	wg.Wait()
	result.Duration = time.Since(startTime)

	// Calculate summary
	result.Summary = &ResultSummary{
		TotalTasks:      job.TotalItems,
		Successful:      len(result.Results),
		Failed:          len(result.Errors),
		SuccessRate:     float64(len(result.Results)) / float64(job.TotalItems),
		TotalDuration:   result.Duration,
		AvgTaskDuration: result.Duration / time.Duration(job.TotalItems),
		TotalTokens:     totalTokens,
		TotalCost:       totalCost,
	}

	result.Success = len(result.Errors) == 0
	return result, nil
}

// executeTrajectoryTask executes a single trajectory task
func (m *Manager) executeTrajectoryTask(ctx context.Context, task TrajectoryTask) TaskResult {
	result := TaskResult{
		TaskID:   task.TaskID,
		Input:    task.Query,
		Success:  true,
		Metadata: make(map[string]interface{}),
	}

	startTime := time.Now()

	// Simulate trajectory generation
	// In real implementation, this would call the agent
	trajectory := []TrajectoryStep{
		{
			Step:      1,
			Role:      "user",
			Content:   task.Query,
			Timestamp: time.Now(),
		},
		{
			Step:      2,
			Role:      "assistant",
			Content:   "Processing your request...",
			Timestamp: time.Now(),
		},
	}

	result.Output = trajectory
	result.Duration = time.Since(startTime)
	result.Metadata["tokens"] = int64(100) // Estimate
	result.Metadata["cost"] = 0.001        // Estimate

	return result
}

// runQueryJob executes a batch query job
func (m *Manager) runQueryJob(ctx context.Context, job *Job, input interface{}) (*JobResult, error) {
	result := &JobResult{
		JobID:       job.ID,
		ProcessedAt: time.Now(),
		Results:     make([]TaskResult, 0),
		Errors:      make([]TaskError, 0),
	}

	startTime := time.Now()

	tasks, ok := input.([]QueryTask)
	if !ok {
		return nil, fmt.Errorf("invalid query input format")
	}

	job.TotalItems = len(tasks)

	sem := make(chan struct{}, m.config.MaxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	totalTokens := int64(0)
	totalCost := 0.0

	for i, task := range tasks {
		wg.Add(1)
		go func(t QueryTask, idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			taskResult := m.executeQueryTask(ctx, t)

			mu.Lock()
			if taskResult.Success {
				result.Results = append(result.Results, taskResult)
			} else {
				result.Errors = append(result.Errors, TaskError{
					TaskID: t.TaskID,
					Error:  taskResult.Output.(string),
					Input:  t.Query,
				})
			}
			totalTokens += taskResult.Metadata["tokens"].(int64)
			totalCost += taskResult.Metadata["cost"].(float64)
			job.ProcessedItems = idx + 1
			job.Progress = float64(job.ProcessedItems) / float64(job.TotalItems) * 100
			m.saveProgress(job)
			mu.Unlock()
		}(task, i)
	}

	wg.Wait()
	result.Duration = time.Since(startTime)

	result.Summary = &ResultSummary{
		TotalTasks:      job.TotalItems,
		Successful:      len(result.Results),
		Failed:          len(result.Errors),
		SuccessRate:     float64(len(result.Results)) / float64(job.TotalItems),
		TotalDuration:   result.Duration,
		AvgTaskDuration: result.Duration / time.Duration(job.TotalItems),
		TotalTokens:     totalTokens,
		TotalCost:       totalCost,
	}

	result.Success = len(result.Errors) == 0
	return result, nil
}

// executeQueryTask executes a single query task
func (m *Manager) executeQueryTask(ctx context.Context, task QueryTask) TaskResult {
	result := TaskResult{
		TaskID:   task.TaskID,
		Input:    task.Query,
		Success:  true,
		Metadata: make(map[string]interface{}),
	}

	startTime := time.Now()

	// Simulate query execution
	result.Output = fmt.Sprintf("Response to: %s", task.Query)
	result.Duration = time.Since(startTime)
	result.Metadata["tokens"] = int64(50)
	result.Metadata["cost"] = 0.0005

	return result
}

// runGenericJob runs a generic batch job
func (m *Manager) runGenericJob(ctx context.Context, job *Job, input interface{}) (*JobResult, error) {
	result := &JobResult{
		JobID:       job.ID,
		Success:     true,
		ProcessedAt: time.Now(),
	}

	// Generic processing
	result.OutputFile = job.OutputFile
	return result, nil
}

// loadInput loads input data from file
func (m *Manager) loadInput(filePath string, jobType string) (interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read input file: %w", err)
	}

	switch jobType {
	case "trajectory":
		var tasks []TrajectoryTask
		if err := json.Unmarshal(data, &tasks); err != nil {
			return nil, fmt.Errorf("invalid trajectory JSON: %w", err)
		}
		return tasks, nil
	case "query":
		var tasks []QueryTask
		if err := json.Unmarshal(data, &tasks); err != nil {
			return nil, fmt.Errorf("invalid query JSON: %w", err)
		}
		return tasks, nil
	default:
		var generic interface{}
		if err := json.Unmarshal(data, &generic); err != nil {
			return nil, err
		}
		return generic, nil
	}
}

// saveResult saves job result to file
func (m *Manager) saveResult(result *JobResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(result.JobID+".json", data, 0644)
}

// saveProgress saves job progress
func (m *Manager) saveProgress(job *Job) error {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	progressFile := filepath.Join(m.config.ProgressDir, job.ID+".progress.json")
	return os.WriteFile(progressFile, data, 0644)
}

// LoadProgress loads job progress from file
func (m *Manager) LoadProgress(jobID string) (*Job, error) {
	progressFile := filepath.Join(m.config.ProgressDir, jobID+".progress.json")
	data, err := os.ReadFile(progressFile)
	if err != nil {
		return nil, err
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}

	return &job, nil
}

// ExportResults exports job results to various formats
func (m *Manager) ExportResults(jobID string, format string) ([]byte, error) {
	result := &JobResult{}
	data, err := os.ReadFile(jobID + ".json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, err
	}

	switch format {
	case "json":
		return json.MarshalIndent(result, "", "  ")
	case "csv":
		return m.exportToCSV(result)
	case "markdown":
		return m.exportToMarkdown(result)
	default:
		return json.Marshal(result)
	}
}

// exportToCSV exports results to CSV format
func (m *Manager) exportToCSV(result *JobResult) ([]byte, error) {
	var lines []string
	lines = append(lines, "TaskID,Success,Duration,Tokens,Cost")

	for _, r := range result.Results {
		tokens := r.Metadata["tokens"].(int64)
		cost := r.Metadata["cost"].(float64)
		lines = append(lines, fmt.Sprintf("%s,true,%s,%d,%.6f", r.TaskID, r.Duration, tokens, cost))
	}

	for _, e := range result.Errors {
		lines = append(lines, fmt.Sprintf("%s,false,0,0,0", e.TaskID))
	}

	output := strings.Join(lines, "\n")
	return []byte(output), nil
}

// exportToMarkdown exports results to Markdown format
func (m *Manager) exportToMarkdown(result *JobResult) ([]byte, error) {
	var lines []string
	lines = append(lines, "# Batch Job Results")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("**Job ID:** %s", result.JobID))
	lines = append(lines, fmt.Sprintf("**Processed At:** %s", result.ProcessedAt.Format(time.RFC3339)))
	lines = append(lines, "")

	if result.Summary != nil {
		lines = append(lines, "## Summary")
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("- Total Tasks: %d", result.Summary.TotalTasks))
		lines = append(lines, fmt.Sprintf("- Successful: %d", result.Summary.Successful))
		lines = append(lines, fmt.Sprintf("- Failed: %d", result.Summary.Failed))
		lines = append(lines, fmt.Sprintf("- Success Rate: %.1f%%", result.Summary.SuccessRate*100))
		lines = append(lines, fmt.Sprintf("- Total Duration: %s", result.Summary.TotalDuration))
		lines = append(lines, fmt.Sprintf("- Total Tokens: %d", result.Summary.TotalTokens))
		lines = append(lines, fmt.Sprintf("- Total Cost: $%.4f", result.Summary.TotalCost))
		lines = append(lines, "")
	}

	if len(result.Errors) > 0 {
		lines = append(lines, "## Errors")
		lines = append(lines, "")
		for _, e := range result.Errors {
			lines = append(lines, fmt.Sprintf("- **%s:** %s", e.TaskID, e.Error))
		}
		lines = append(lines, "")
	}

	return []byte(strings.Join(lines, "\n")), nil
}

// CancelJob cancels a running job
func (m *Manager) CancelJob(jobID string) error {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status == "running" {
		job.Status = "cancelled"
	}
	return nil
}

// DeleteJob removes a job
func (m *Manager) DeleteJob(jobID string) error {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	if _, ok := m.jobs[jobID]; !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	delete(m.jobs, jobID)
	return nil
}

// ResultChan returns the result channel
func (m *Manager) ResultChan() <-chan *JobResult {
	return m.resultChan
}

// ============= RL Training Integration =============

// ExportTrajectories exports trajectories for RL training
func ExportTrajectories(results []TaskResult, format string) ([]byte, error) {
	type RLTrajectory struct {
		TaskID    string   `json:"task_id"`
		Query     string   `json:"query"`
		Response  string   `json:"response"`
		ToolsUsed []string `json:"tools_used"`
		Success   bool     `json:"success"`
		Reward    float64  `json:"reward"`
	}

	trajectories := make([]RLTrajectory, 0, len(results))
	for _, r := range results {
		// Convert to RL format
		traj := RLTrajectory{
			TaskID:  r.TaskID,
			Query:   fmt.Sprintf("%v", r.Input),
			Success: r.Success,
			Reward:  1.0,
		}
		if !r.Success {
			traj.Reward = 0.0
		}
		trajectories = append(trajectories, traj)
	}

	return json.MarshalIndent(trajectories, "", "  ")
}
