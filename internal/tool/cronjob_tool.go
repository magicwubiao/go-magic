package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CronJobTool manages scheduled tasks (cron jobs)
type CronJobTool struct {
	jobsFile string
}

// NewCronJobTool creates a new cron job tool
func NewCronJobTool() *CronJobTool {
	home, _ := os.UserHomeDir()
	jobsFile := filepath.Join(home, ".magic", "cron_jobs.json")
	return &CronJobTool{jobsFile: jobsFile}
}

// Name returns the tool name
func (t *CronJobTool) Name() string {
	return "cronjob"
}

// Description returns the tool description
func (t *CronJobTool) Description() string {
	return "Manage scheduled tasks (cron jobs). Add, list, remove, or run scheduled tasks."
}

// Parameters returns the tool parameters schema
func (t *CronJobTool) Schema() map[string]interface{} { return t.Parameters() }

func (t *CronJobTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: add, list, remove, run",
				"enum":        []string{"add", "list", "remove", "run"},
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID (required for remove, run)",
			},
			"schedule": map[string]interface{}{
				"type":        "string",
				"description": "Cron schedule (e.g., '30 9 * * *')",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Prompt to execute for this job",
			},
			"platform": map[string]interface{}{
				"type":        "string",
				"description": "Delivery platform (e.g., cli, telegram, discord)",
			},
		},
		"required": []string{"action"},
	}
}

// Job represents a cron job
type Job struct {
	ID        string     `json:"id"`
	Name      string     `json:"name,omitempty"`
	Schedule  string     `json:"schedule"`
	Prompt    string     `json:"prompt"`
	Platform  string     `json:"platform,omitempty"`
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
	LastRun   *time.Time `json:"last_run,omitempty"`
}

// Execute manages cron jobs
func (t *CronJobTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "add":
		return t.addJob(ctx, args)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(args)
	case "run":
		return t.runJob(args)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (t *CronJobTool) addJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedule, ok := args["schedule"].(string)
	if !ok || schedule == "" {
		return nil, fmt.Errorf("schedule is required for add")
	}
	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return nil, fmt.Errorf("prompt is required for add")
	}

	job := &Job{
		ID:        fmt.Sprintf("job_%d", time.Now().UnixNano()),
		Schedule:  schedule,
		Prompt:    prompt,
		Enabled:   true,
		CreatedAt: time.Now(),
	}

	if platform, ok := args["platform"].(string); ok {
		job.Platform = platform
	}

	// Load existing jobs
	jobs, err := t.loadJobs()
	if err != nil {
		jobs = make(map[string]*Job)
	}

	jobs[job.ID] = job

	if err := t.saveJobs(jobs); err != nil {
		return nil, fmt.Errorf("failed to save jobs: %v", err)
	}

	return map[string]interface{}{
		"status":  "success",
		"job_id":  job.ID,
		"message": "Cron job added successfully",
	}, nil
}

func (t *CronJobTool) listJobs() (interface{}, error) {
	jobs, err := t.loadJobs()
	if err != nil {
		return nil, fmt.Errorf("failed to load jobs: %v", err)
	}

	result := make([]map[string]interface{}, 0, len(jobs))
	for _, job := range jobs {
		item := map[string]interface{}{
			"id":       job.ID,
			"name":     job.Name,
			"schedule": job.Schedule,
			"enabled":  job.Enabled,
			"platform": job.Platform,
		}
		if !job.CreatedAt.IsZero() {
			item["created_at"] = job.CreatedAt.Format(time.RFC3339)
		}
		result = append(result, item)
	}

	return map[string]interface{}{
		"total": len(result),
		"jobs":  result,
	}, nil
}

func (t *CronJobTool) removeJob(args map[string]interface{}) (interface{}, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required for remove")
	}

	jobs, err := t.loadJobs()
	if err != nil {
		return nil, fmt.Errorf("failed to load jobs: %v", err)
	}

	if _, exists := jobs[id]; !exists {
		return nil, fmt.Errorf("job not found: %s", id)
	}

	delete(jobs, id)

	if err := t.saveJobs(jobs); err != nil {
		return nil, fmt.Errorf("failed to save jobs: %v", err)
	}

	return map[string]interface{}{
		"status":  "success",
		"job_id":  id,
		"message": "Cron job removed successfully",
	}, nil
}

func (t *CronJobTool) runJob(args map[string]interface{}) (interface{}, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required for run")
	}

	return map[string]interface{}{
		"status":  "info",
		"job_id":  id,
		"message": "Cron job execution requires the cron manager to be running.",
		"note":    "Start the gateway with 'magic gateway start' to enable cron scheduling.",
	}, nil
}

func (t *CronJobTool) loadJobs() (map[string]*Job, error) {
	data, err := os.ReadFile(t.jobsFile)
	if err != nil {
		return nil, err
	}

	var jobs map[string]*Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (t *CronJobTool) saveJobs(jobs map[string]*Job) error {
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	// Ensure directory exists
	dir := filepath.Dir(t.jobsFile)
	os.MkdirAll(dir, 0755)
	return os.WriteFile(t.jobsFile, data, 0644)
}
