package tool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// JobStatus 异步任务状态
type JobStatus string

const (
	StatusPending   JobStatus = "pending"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// AsyncJob 异步任务
type AsyncJob struct {
	ID        string
	ToolName  string
	Params    map[string]any
	Status    JobStatus
	Result    *ToolResult
	Error     error
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time
}

// AsyncTool 异步工具接口
type AsyncTool interface {
	Tool
	// Start 启动异步执行，返回 jobID
	Start(ctx context.Context, params map[string]any) (string, error)
	// Status 获取任务状态
	Status(jobID string) (JobStatus, error)
	// Result 获取任务结果
	Result(jobID string) (*ToolResult, error)
	// Cancel 取消任务
	Cancel(jobID string) error
}

// AsyncToolExecutor 异步工具执行器
type AsyncToolExecutor struct {
	mu       sync.RWMutex
	jobs     map[string]*AsyncJob
	registry *Registry
	workerPool chan struct{}
	maxWorkers int
}

// NewAsyncToolExecutor 创建异步执行器
func NewAsyncToolExecutor(registry *Registry, maxWorkers int) *AsyncToolExecutor {
	if maxWorkers <= 0 {
		maxWorkers = 10
	}
	
	executor := &AsyncToolExecutor{
		jobs:        make(map[string]*AsyncJob),
		registry:    registry,
		workerPool:  make(chan struct{}, maxWorkers),
		maxWorkers: maxWorkers,
	}
	
	// 启动后台清理 goroutine
	go executor.cleanup()
	
	return executor
}

// Submit 提交异步任务
func (e *AsyncToolExecutor) Submit(ctx context.Context, toolName string, params map[string]any) (string, error) {
	// 获取工具
	tool, err := e.registry.Get(toolName)
	if err != nil {
		return "", err
	}
	
	// 检查是否为异步工具
	asyncTool, ok := tool.(AsyncTool)
	if !ok {
		// 如果不是异步工具，直接执行
		jobID := fmt.Sprintf("sync_%d", time.Now().UnixNano())
		job := &AsyncJob{
			ID:        jobID,
			ToolName:  toolName,
			Params:    params,
			Status:    StatusRunning,
			CreatedAt: time.Now(),
			StartedAt: time.Now(),
		}
		
		e.mu.Lock()
		e.jobs[jobID] = job
		e.mu.Unlock()
		
		// 同步执行
		go func() {
			result, err := tool.Execute(ctx, params)
			job.EndedAt = time.Now()
			if err != nil {
				job.Status = StatusFailed
				job.Error = err
			} else {
				job.Status = StatusCompleted
				if result != nil {
					job.Result = &ToolResult{
						Success: true,
						Data:    result,
					}
				}
			}
		}()
		
		return jobID, nil
	}
	
	// 异步工具
	jobID, err := asyncTool.Start(ctx, params)
	if err != nil {
		return "", err
	}
	
	// 创建 job 记录
	job := &AsyncJob{
		ID:        jobID,
		ToolName:  toolName,
		Params:    params,
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}
	
	e.mu.Lock()
	e.jobs[jobID] = job
	e.mu.Unlock()
	
	return jobID, nil
}

// GetJob 获取任务
func (e *AsyncToolExecutor) GetJob(jobID string) (*AsyncJob, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	job, ok := e.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	return job, nil
}

// ListJobs 列出所有任务
func (e *AsyncToolExecutor) ListJobs() []*AsyncJob {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	jobs := make([]*AsyncJob, 0, len(e.jobs))
	for _, job := range e.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// Cancel 取消任务
func (e *AsyncToolExecutor) Cancel(jobID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	job, ok := e.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	
	if job.Status == StatusCompleted || job.Status == StatusFailed || job.Status == StatusCancelled {
		return fmt.Errorf("job already finished")
	}
	
	job.Status = StatusCancelled
	job.EndedAt = time.Now()
	return nil
}

// cleanup 定期清理已完成的任务
func (e *AsyncToolExecutor) cleanup() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		e.mu.Lock()
		threshold := time.Now().Add(-1 * time.Hour)
		for id, job := range e.jobs {
			if job.EndedAt.Before(threshold) {
				delete(e.jobs, id)
			}
		}
		e.mu.Unlock()
	}
}

// ============================================================================
// BuiltinAsyncTool - 内置异步工具示例
// ============================================================================

// LongRunningTool 长时运行工具示例
type LongRunningTool struct {
	BaseTool
	executor *AsyncToolExecutor
	jobs     map[string]context.CancelFunc
	mu       sync.RWMutex
}

// NewLongRunningTool 创建长时运行工具
func NewLongRunningTool(executor *AsyncToolExecutor) *LongRunningTool {
	return &LongRunningTool{
		BaseTool: *NewBaseTool(
			"long_running_task",
			"Execute a long-running task asynchronously",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "string",
						"description": "Task identifier",
					},
					"duration": map[string]any{
						"type":        "number",
						"description": "Duration in seconds",
						"default":     10,
					},
				},
				"required": []string{"task_id"},
			},
		),
		executor: executor,
		jobs:     make(map[string]context.CancelFunc),
	}
}

func (t *LongRunningTool) Start(ctx context.Context, params map[string]any) (string, error) {
	taskID, ok := params["task_id"].(string)
	if !ok {
		return "", fmt.Errorf("task_id is required")
	}
	
	duration := 10
	if d, ok := params["duration"].(float64); ok {
		duration = int(d)
	}
	
	jobID := fmt.Sprintf("long_task_%s_%d", taskID, time.Now().UnixNano())
	
	// 创建可取消的 context
	jobCtx, cancel := context.WithCancel(ctx)
	
	t.mu.Lock()
	t.jobs[jobID] = cancel
	t.mu.Unlock()
	
	// 异步执行
	go func() {
		ticker := time.NewTicker(time.Duration(duration) * time.Second)
		select {
		case <-ticker.C:
			// 任务完成
		case <-jobCtx.Done():
			// 任务被取消
		}
		
		t.mu.Lock()
		delete(t.jobs, jobID)
		t.mu.Unlock()
	}()
	
	return jobID, nil
}

func (t *LongRunningTool) Status(jobID string) (JobStatus, error) {
	t.mu.RLock()
	_, exists := t.jobs[jobID]
	t.mu.RUnlock()
	
	if exists {
		return StatusRunning, nil
	}
	return StatusCompleted, nil
}

func (t *LongRunningTool) Result(jobID string) (*ToolResult, error) {
	return &ToolResult{
		Success: true,
		Data:    map[string]any{"job_id": jobID, "status": "completed"},
	}, nil
}

func (t *LongRunningTool) Cancel(jobID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if cancel, ok := t.jobs[jobID]; ok {
		cancel()
		delete(t.jobs, jobID)
	}
	return nil
}
