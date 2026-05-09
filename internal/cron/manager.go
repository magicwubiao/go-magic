package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Job struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schedule    string                 `json:"schedule"`
	Prompt      string                 `json:"prompt"`
	Skills      []string               `json:"skills,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Enabled     bool                   `json:"enabled"`
	NoAgent     bool                   `json:"no_agent"`  // Skip agent, run script directly
	Script      string                 `json:"script"`     // Script/command for no_agent mode
	NextRun     *time.Time            `json:"next_run,omitempty"`
	LastRun     *time.Time            `json:"last_run,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type Manager struct {
	jobsFile string
	jobs     map[string]*Job
}

func NewManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	jobsFile := filepath.Join(home, ".magic", "cron_jobs.json")
	m := &Manager{
		jobsFile: jobsFile,
		jobs:     make(map[string]*Job),
	}

	if err := m.loadJobs(); err != nil {
		m.jobs = make(map[string]*Job)
	}

	return m, nil
}

func (m *Manager) loadJobs() error {
	data, err := os.ReadFile(m.jobsFile)
	if err != nil {
		return err
	}

	var jobs []*Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
	}

	for _, job := range jobs {
		m.jobs[job.ID] = job
	}
	return nil
}

func (m *Manager) saveJobs() error {
	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.jobsFile, data, 0644)
}

func (m *Manager) List() []*Job {
	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (m *Manager) Add(job *Job) error {
	m.jobs[job.ID] = job
	return m.saveJobs()
}

func (m *Manager) Remove(id string) error {
	delete(m.jobs, id)
	return m.saveJobs()
}

func (m *Manager) Get(name string) *Job {
	// Find by name (case-insensitive)
	for _, job := range m.jobs {
		if job.Name == name {
			return job
		}
	}
	return nil
}

func (m *Manager) Update(job *Job) error {
	if _, exists := m.jobs[job.ID]; !exists {
		return fmt.Errorf("job not found: %s", job.ID)
	}
	m.jobs[job.ID] = job
	return m.saveJobs()
}

func (m *Manager) GetDueJobs() []*Job {
	var due []*Job
	now := time.Now()

	for _, job := range m.jobs {
		if !job.Enabled {
			continue
		}
		if job.NextRun != nil && job.NextRun.Before(now) {
			due = append(due, job)
		}
	}
	return due
}

func (m *Manager) RunJob(ctx context.Context, job *Job) error {
	fmt.Printf("Running job: %s\n", job.Name)

	// no_agent mode: execute script directly without agent
	if job.NoAgent && job.Script != "" {
		output, err := executeScript(ctx, job.Script)
		if err != nil {
			fmt.Printf("no_agent cron script error: %v\n", err)
			return err
		}
		output = strings.TrimSpace(output)
		if output != "" {
			fmt.Printf("Script output: %s\n", output)
			// In a real implementation, sendToChannel(job.ChannelID, output) would be called
			// For now, just print to stdout
			if job.Platform != "" {
				fmt.Printf("[%s] %s\n", job.Platform, output)
			}
		}
		// Silent if output is empty
	}

	now := time.Now()
	job.LastRun = &now

	next := now.Add(24 * time.Hour)
	job.NextRun = &next

	return m.saveJobs()
}

// executeScript runs a script/command with 60s timeout
func executeScript(ctx context.Context, script string) (string, error) {
	// Create a context with 60s timeout
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Use shell to run the script
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errMsg := stderr.String()
		if errMsg != "" {
			return "", fmt.Errorf("script error: %s", errMsg)
		}
		return "", fmt.Errorf("script error: %v", err)
	}

	return stdout.String(), nil
}
