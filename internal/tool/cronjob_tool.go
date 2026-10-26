package tool

import (
	"context"
	"fmt"
	"time"
)

// CronJobManager 抽象定时任务管理入口，由 internal/cron.Manager 实现
// （见 internal/cron/tool_bridge.go）。定义在 tool 包是为了避免与
// internal/cron 的循环依赖——cron 包本身 import 了 tool。
//
// 此前 CronJobTool 自带一份持久化，把 cron_jobs.json 写成 map 格式，
// 而 cron.Manager 按数组解析；工具创建过任务后 Manager 加载必然失败，
// 且失败被吞掉、所有已有任务静默丢失。现在工具只做参数校验和转发，
// 数据只有 Manager 一个权威来源。
type CronJobManager interface {
	// CreateToolJob 创建任务（校验 schedule），返回任务 ID。
	CreateToolJob(name, schedule, prompt, platform string) (string, error)
	// ListToolJobs 返回全部任务快照。
	ListToolJobs() []ToolJobInfo
	// GetToolJob 按 ID 或名字查单个任务。
	GetToolJob(id string) (*ToolJobInfo, error)
	// RemoveToolJob 删除任务。
	RemoveToolJob(id string) error
	// RunToolJob 立即触发一次执行（异步）。
	RunToolJob(id string) error
}

// ToolJobInfo 是 cronjob 工具视角的任务只读快照。
type ToolJobInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Schedule   string     `json:"schedule"`
	Prompt     string     `json:"prompt"`
	Platform   string     `json:"platform,omitempty"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at,omitempty"`
	NextRun    *time.Time `json:"next_run,omitempty"`
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
}

// CronJobTool manages scheduled tasks (cron jobs) on behalf of the agent.
// It is a thin adapter over CronJobManager (the cron.Manager instance);
// without a manager injected it degrades to an explanatory error instead of
// silently writing a second, incompatible copy of the jobs file.
type CronJobTool struct {
	mgr CronJobManager
}

// NewCronJobTool creates a new cron job tool. The manager is injected later
// via SetManager (see Registry.SetCronJobManager), because the registry is
// built before the cron manager exists.
func NewCronJobTool() *CronJobTool {
	return &CronJobTool{}
}

// SetManager 注入 cron.Manager。应在服务启动时调用一次。
func (t *CronJobTool) SetManager(mgr CronJobManager) {
	t.mgr = mgr
}

// Name returns the tool name
func (t *CronJobTool) Name() string {
	return "cronjob"
}

// Description returns the tool description
func (t *CronJobTool) Description() string {
	return "Manage scheduled tasks (cron jobs). Add, list, get, remove, or run scheduled tasks. Schedule uses 5-field cron format (minute hour day-of-month month day-of-week), e.g. '30 9 * * *' for every day at 09:30."
}

// Parameters returns the tool parameters schema
func (t *CronJobTool) Schema() map[string]interface{} { return t.Parameters() }

func (t *CronJobTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: add, list, get, remove, run",
				"enum":        []string{"add", "list", "get", "remove", "run"},
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Human-readable job name (recommended for add)",
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID or name (required for get, remove, run)",
			},
			"schedule": map[string]interface{}{
				"type":        "string",
				"description": "Cron schedule in 5-field format (e.g., '30 9 * * *'). Required for add.",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Prompt the agent executes on each run. Required for add.",
			},
			"platform": map[string]interface{}{
				"type":        "string",
				"description": "Optional delivery platform (e.g., cli, telegram, discord)",
			},
		},
		"required": []string{"action"},
	}
}

// Execute manages cron jobs
func (t *CronJobTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action is required")
	}

	if t.mgr == nil {
		return nil, fmt.Errorf("cron manager is not available in this process; start the gateway server ('magic gateway start') and retry, or manage jobs via the web UI")
	}

	switch action {
	case "add":
		return t.addJob(args)
	case "list":
		return t.listJobs()
	case "get":
		return t.getJob(args)
	case "remove":
		return t.removeJob(args)
	case "run":
		return t.runJob(args)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func (t *CronJobTool) addJob(args map[string]interface{}) (interface{}, error) {
	schedule := argString(args, "schedule")
	prompt := argString(args, "prompt")
	if schedule == "" {
		return nil, fmt.Errorf("schedule is required for add (5-field cron, e.g. '30 9 * * *')")
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required for add")
	}

	id, err := t.mgr.CreateToolJob(argString(args, "name"), schedule, prompt, argString(args, "platform"))
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "success",
		"job_id":  id,
		"message": "Cron job created and scheduled",
	}, nil
}

func (t *CronJobTool) listJobs() (interface{}, error) {
	jobs := t.mgr.ListToolJobs()
	return map[string]interface{}{
		"total": len(jobs),
		"jobs":  jobs,
	}, nil
}

func (t *CronJobTool) getJob(args map[string]interface{}) (interface{}, error) {
	id := argString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required for get")
	}
	job, err := t.mgr.GetToolJob(id)
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (t *CronJobTool) removeJob(args map[string]interface{}) (interface{}, error) {
	id := argString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required for remove")
	}

	if err := t.mgr.RemoveToolJob(id); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "success",
		"job_id":  id,
		"message": "Cron job removed",
	}, nil
}

func (t *CronJobTool) runJob(args map[string]interface{}) (interface{}, error) {
	id := argString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required for run")
	}

	if err := t.mgr.RunToolJob(id); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "success",
		"job_id":  id,
		"message": "Cron job triggered; check execution logs for the result",
	}, nil
}
