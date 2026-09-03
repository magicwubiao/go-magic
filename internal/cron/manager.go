package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	robfigcron "github.com/robfig/cron/v3"
)

// Job represents a scheduled cron job
type Job struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schedule    string                 `json:"schedule"`
	Prompt      string                 `json:"prompt"`
	Skills      []string               `json:"skills,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Enabled     bool                   `json:"enabled"`
	NoAgent     bool                   `json:"no_agent"` // Skip agent, run script directly
	Script      string                 `json:"script"`   // Script/command for no_agent mode
	NextRun     *time.Time             `json:"next_run,omitempty"`
	LastRun     *time.Time             `json:"last_run,omitempty"`
	LastStatus  string                 `json:"last_status,omitempty"` // success, failed, running
	LastError   string                 `json:"last_error,omitempty"`
	RunCount    int                    `json:"run_count,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ExecutionLog records the result of a job execution
type ExecutionLog struct {
	ID         string     `json:"id"`
	JobID      string     `json:"job_id"`
	JobName    string     `json:"job_name"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     string     `json:"status"` // success, failed, timeout
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Duration   string     `json:"duration,omitempty"`
}

// Manager manages cron jobs with real scheduling
type Manager struct {
	jobsFile   string
	logsFile   string
	jobs       map[string]*Job
	jobsMu     sync.RWMutex
	logs       []ExecutionLog
	logsMu     sync.RWMutex
	cron       *robfigcron.Cron
	entryMap   map[string]robfigcron.EntryID // job.ID -> cron entry ID
	ctx        context.Context
	cancel     context.CancelFunc
	prov       provider.Provider // LLM provider
	toolReg    *tool.Registry    // Tool registry for agent tools
	workingDir string            // Default working directory from config
}

// NewManager creates a new cron manager
func NewManager() (*Manager, error) {
	// Use GetMagicHome() to respect GO_MAGIC_HOME environment variable
	magicHome := config.GetMagicHome()
	home, _ := os.UserHomeDir() // non-fatal if fails

	// Ensure cron subdirectory exists
	cronDir := filepath.Join(magicHome, "cron")
	if err := os.MkdirAll(cronDir, 0755); err != nil {
		return nil, err
	}

	jobsFile := filepath.Join(cronDir, "cron_jobs.json")
	logsFile := filepath.Join(cronDir, "cron_logs.json")

	// Migrate legacy files if they exist (legacy files are always under ~/.magic/)
	if home != "" {
		migrateLegacyFiles(home, cronDir, jobsFile, logsFile)
	}

	m := &Manager{
		jobsFile: jobsFile,
		logsFile: logsFile,
		jobs:     make(map[string]*Job),
		entryMap: make(map[string]robfigcron.EntryID),
	}

	if err := m.loadJobs(); err != nil {
		m.jobs = make(map[string]*Job)
	}

	if err := m.loadLogs(); err != nil {
		m.logs = []ExecutionLog{}
	}

	// Create cron scheduler with standard 5-field format (min hour dom month dow)
	// Users can also use 6-field format (sec min hour dom month dow) with "0" prefix
	m.cron = robfigcron.New()

	return m, nil
}

// SetAgentDeps sets the agent dependencies (provider, tool registry)
func (m *Manager) SetAgentDeps(prov provider.Provider, reg *tool.Registry, workingDir string) {
	m.prov = prov
	m.toolReg = reg
	m.workingDir = workingDir
}

// Start begins the cron scheduler loop and loads all enabled jobs
func (m *Manager) Start() {
	m.ctx, m.cancel = context.WithCancel(context.Background())

	// Add all enabled jobs to the scheduler
	m.jobsMu.RLock()
	for _, job := range m.jobs {
		if job.Enabled && job.Schedule != "" {
			m.addJobToScheduler(job)
		}
	}
	m.jobsMu.RUnlock()

	m.cron.Start()
	log.Info("[Cron] Scheduler started")
}

// Stop gracefully stops the cron scheduler
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.cron != nil {
		ctx := m.cron.Stop()
		<-ctx.Done()
	}
	log.Info("[Cron] Scheduler stopped")
}

// addJobToScheduler adds a job to the robfig/cron scheduler
func (m *Manager) addJobToScheduler(job *Job) {
	if job.Schedule == "" {
		return
	}

	// Parse schedule - support both 5-field (standard) and 6-field (with seconds)
	schedule := job.Schedule
	parts := strings.Fields(schedule)
	var spec string
	if len(parts) == 5 {
		// Standard 5-field: min hour dom month dow
		spec = schedule
	} else if len(parts) == 6 {
		// 6-field with seconds: sec min hour dom month dow
		spec = schedule
	} else {
		log.Warnf("[Cron] Invalid schedule format for job %s: %s (expected 5 or 6 fields)", job.Name, schedule)
		return
	}

	jobID := job.ID
	entryID, err := m.cron.AddFunc(spec, func() {
		m.executeJob(jobID)
	})

	if err != nil {
		log.Warnf("[Cron] Failed to schedule job %s (%s): %v", job.Name, schedule, err)
		return
	}

	m.entryMap[jobID] = entryID

	// Update NextRun
	if entry := m.cron.Entry(entryID); entry.ID != 0 {
		next := entry.Next
		job.NextRun = &next
	}

	log.Infof("[Cron] Scheduled job %s: %s -> %s", job.Name, job.Schedule, spec)
}

// removeJobFromScheduler removes a job from the scheduler
func (m *Manager) removeJobFromScheduler(jobID string) {
	if entryID, ok := m.entryMap[jobID]; ok {
		m.cron.Remove(entryID)
		delete(m.entryMap, jobID)
	}
}

// executeJob runs a job and records the result
func (m *Manager) executeJob(jobID string) {
	m.jobsMu.RLock()
	job, ok := m.jobs[jobID]
	m.jobsMu.RUnlock()

	if !ok {
		return
	}

	startTime := time.Now()

	// Update status
	m.jobsMu.Lock()
	job.LastStatus = "running"
	job.LastRun = &startTime
	m.jobsMu.Unlock()
	m.saveJobs()

	// Create execution log
	execLog := ExecutionLog{
		ID:        fmt.Sprintf("log_%d", startTime.UnixNano()),
		JobID:     job.ID,
		JobName:   job.Name,
		StartedAt: startTime,
		Status:    "running",
	}

	var output string
	var execErr error

	// Execute based on mode
	if job.NoAgent && job.Script != "" {
		// Script mode - execute directly
		output, execErr = executeScript(m.ctx, job.Script)
	} else if job.Prompt != "" {
		// Agent mode - use LLM to generate and execute script
		output, execErr = m.executeAgentPrompt(m.ctx, job)
	} else if job.Script != "" {
		// Fallback to script mode if no prompt
		output, execErr = executeScript(m.ctx, job.Script)
	} else {
		execErr = fmt.Errorf("no prompt or script defined for job %s", job.Name)
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	// Update execution log
	execLog.FinishedAt = &endTime
	execLog.Duration = duration.String()
	execLog.Output = truncateString(output, 2000)

	if execErr != nil {
		execLog.Status = "failed"
		execLog.Error = execErr.Error()

		m.jobsMu.Lock()
		job.LastStatus = "failed"
		job.LastError = execErr.Error()
		m.jobsMu.Unlock()

		log.Warnf("[Cron] Job %s failed after %s: %v", job.Name, duration, execErr)
	} else {
		execLog.Status = "success"

		m.jobsMu.Lock()
		job.LastStatus = "success"
		job.LastError = ""
		job.RunCount++
		m.jobsMu.Unlock()

		log.Infof("[Cron] Job %s completed in %s", job.Name, duration)
	}

	// Update NextRun from scheduler
	if entryID, ok := m.entryMap[job.ID]; ok {
		if entry := m.cron.Entry(entryID); entry.ID != 0 {
			next := entry.Next
			job.NextRun = &next
		}
	}

	m.saveJobs()
	m.addLog(execLog)
}

// RunJob manually triggers a job execution
func (m *Manager) RunJob(ctx context.Context, job *Job) error {
	go m.executeJob(job.ID)
	return nil
}

// --- CRUD ---

func (m *Manager) List() []*Job {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (m *Manager) Add(job *Job) error {
	m.jobsMu.Lock()
	m.jobs[job.ID] = job
	m.jobsMu.Unlock()

	// If enabled, add to scheduler
	if job.Enabled {
		m.addJobToScheduler(job)
	}

	return m.saveJobs()
}

func (m *Manager) Remove(id string) error {
	m.jobsMu.Lock()
	m.removeJobFromScheduler(id)
	delete(m.jobs, id)
	m.jobsMu.Unlock()
	return m.saveJobs()
}

func (m *Manager) Get(name string) *Job {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	for _, job := range m.jobs {
		if strings.EqualFold(job.Name, name) {
			return job
		}
	}
	return nil
}

func (m *Manager) GetByID(id string) *Job {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()
	return m.jobs[id]
}

func (m *Manager) Update(job *Job) error {
	m.jobsMu.Lock()
	if _, exists := m.jobs[job.ID]; !exists {
		m.jobsMu.Unlock()
		return fmt.Errorf("job not found: %s", job.ID)
	}

	// Remove old scheduler entry
	m.removeJobFromScheduler(job.ID)

	m.jobs[job.ID] = job
	m.jobsMu.Unlock()

	// Re-add to scheduler if enabled
	if job.Enabled {
		m.addJobToScheduler(job)
	}

	return m.saveJobs()
}

func (m *Manager) GetDueJobs() []*Job {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

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

// --- Execution Logs ---

func (m *Manager) GetLogs(jobID string, limit int) []ExecutionLog {
	m.logsMu.RLock()
	defer m.logsMu.RUnlock()

	var result []ExecutionLog
	for i := len(m.logs) - 1; i >= 0 && len(result) < limit; i-- {
		logEntry := m.logs[i]
		if jobID != "" && logEntry.JobID != jobID {
			continue
		}
		result = append(result, logEntry)
	}

	if result == nil {
		result = []ExecutionLog{}
	}
	return result
}

func (m *Manager) ClearLogs(jobID string) error {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()

	if jobID != "" {
		var filtered []ExecutionLog
		for _, l := range m.logs {
			if l.JobID != jobID {
				filtered = append(filtered, l)
			}
		}
		m.logs = filtered
	} else {
		m.logs = []ExecutionLog{}
	}

	return m.saveLogs()
}

func (m *Manager) addLog(entry ExecutionLog) {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()

	// Keep max 1000 logs per job
	var jobLogCount int
	for _, l := range m.logs {
		if l.JobID == entry.JobID {
			jobLogCount++
		}
	}

	if jobLogCount > 1000 {
		var filtered []ExecutionLog
		count := 0
		for _, l := range m.logs {
			if l.JobID == entry.JobID {
				count++
				if count > 500 {
					continue // Skip old logs
				}
			}
			filtered = append(filtered, l)
		}
		m.logs = filtered
	}

	m.logs = append(m.logs, entry)

	// Save to file directly (we hold the lock)
	data, err := json.MarshalIndent(m.logs, "", "  ")
	if err != nil {
		log.Warnf("[Cron] Failed to marshal logs: %v", err)
		return
	}
	if err := os.WriteFile(m.logsFile, data, 0644); err != nil {
		log.Warnf("[Cron] Failed to save logs: %v", err)
	}
}

// --- Persistence ---

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
	// Note: caller should not hold jobsMu.Lock() when calling this
	m.jobsMu.RLock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.jobsMu.RUnlock()

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.jobsFile, data, 0644)
}

func (m *Manager) loadLogs() error {
	data, err := os.ReadFile(m.logsFile)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &m.logs)
}

func (m *Manager) saveLogs() error {
	m.logsMu.RLock()
	defer m.logsMu.RUnlock()

	data, err := json.MarshalIndent(m.logs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.logsFile, data, 0644)
}

// executeAgentPrompt uses the full Agent with all tools to execute the prompt
func (m *Manager) executeAgentPrompt(ctx context.Context, job *Job) (string, error) {
	if m.prov == nil {
		return "", fmt.Errorf("no LLM provider configured for agent mode")
	}
	if m.toolReg == nil {
		return "", fmt.Errorf("no tool registry configured for agent mode")
	}

	log.Infof("[Cron] Agent job %s executing: %s", job.Name, job.Prompt)

	// Get all available tools from registry
	tools := m.toolReg.ListWithSchemas()
	log.Infof("[Cron] Agent job %s has %d tools available", job.Name, len(tools))

	// Determine working directory - always use a "cron" subdirectory
	baseDir := m.workingDir
	if baseDir == "" {
		baseDir = filepath.Join(config.GetMagicHome(), "workspace")
	}
	workDir := filepath.Join(baseDir, "cron")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		log.Warnf("[Cron] Failed to create workspace %s: %v", workDir, err)
	}

	// System prompt for cron agent - simple, direct, no explanation
	systemPrompt := fmt.Sprintf(`You are a reliable task execution assistant. You have access to various tools.
Complete the user's request efficiently using available tools.
Focus on the result, not the process. Keep responses concise.

Your working directory is: %s
- Use write_file with RELATIVE paths to write files to this directory.
- Do NOT use absolute paths like /tmp/.`, workDir)

	// Agent options - balance between capability and safety
	agentOpts := []agent.AgentOption{
		agent.WithLoopLimits(5, 15), // Max 5 tool calls per turn, 15 turns max
		agent.WithSteering(agent.SteeringConfig{
			MaxIterations: 30, // Enough for complex multi-step tasks
		}),
	}

	// 应用 PII 脱敏配置（来自 config.Privacy）
	if cfg, _ := config.Load(); cfg != nil && cfg.Privacy != nil {
		agentOpts = append(agentOpts, agent.WithPrivacy(cfg.Privacy))
	}

	// Create agent with all tools (Cortex/Memory disabled by default)
	a := agent.NewEnhancedAgent(m.prov, m.toolReg, tools, systemPrompt, agentOpts...)

	// Set working directory in context
	if workDir != "" {
		ctx = tool.WithWorkDir(ctx, workDir)
	}

	// Run the conversation
	result, err := a.RunConversation(ctx, job.Prompt)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	log.Infof("[Cron] Agent job %s completed", job.Name)
	return result, nil
}

// removeMarkdownCodeBlocks removes markdown code block formatting
func removeMarkdownCodeBlocks(s string) string {
	// Remove ```language and ``` markers
	lines := strings.Split(s, "\n")
	var result []string
	inCodeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if !inCodeBlock && trimmed == "" {
			continue
		}
		result = append(result, line)
	}
	if len(result) == 0 {
		return s
	}
	return strings.Join(result, "\n")
}

// --- Script Execution (cross-platform) ---

// executeScript runs a script/command with 60s timeout
// Automatically detects PowerShell vs CMD on Windows
func executeScript(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		// Detect if script is PowerShell or CMD
		if isPowerShellScript(script) {
			cmd = exec.CommandContext(ctx, "powershell", "-Command", script)
		} else {
			cmd = exec.CommandContext(ctx, "cmd", "/C", script)
		}
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}

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

// isPowerShellScript detects if the script uses PowerShell cmdlets
func isPowerShellScript(script string) bool {
	// Common PowerShell indicators
	powershellPatterns := []string{
		"New-Item", "Get-Content", "Set-Content", "Remove-Item",
		"Write-Host", "Write-Output", "Get-Process", "Start-Process",
		"Test-Path", "Get-Location", "Set-Location", "Get-ChildItem",
		"ForEach-Object", "Where-Object", "Select-Object",
		"$env:", "$PSScriptRoot", "|", "`",
	}
	upperScript := strings.ToUpper(script)
	for _, pattern := range powershellPatterns {
		if strings.Contains(upperScript, strings.ToUpper(pattern)) {
			return true
		}
	}
	return false
}

// --- Helpers ---

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ValidateSchedule checks if a cron expression is valid
func ValidateSchedule(schedule string) error {
	parts := strings.Fields(schedule)
	if len(parts) != 5 && len(parts) != 6 {
		return fmt.Errorf("invalid cron expression: expected 5 or 6 fields, got %d", len(parts))
	}

	parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
	if len(parts) == 6 {
		parser = robfigcron.NewParser(robfigcron.Second | robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
	}

	_, err := parser.Parse(schedule)
	return err
}

// GetNextRun calculates the next run time for a schedule expression
func GetNextRun(schedule string) (*time.Time, error) {
	parts := strings.Fields(schedule)
	var parser robfigcron.Parser

	if len(parts) == 6 {
		parser = robfigcron.NewParser(robfigcron.Second | robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
	} else {
		parser = robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
	}

	sched, err := parser.Parse(schedule)
	if err != nil {
		return nil, err
	}

	next := sched.Next(time.Now())
	return &next, nil
}

// migrateLegacyFiles migrates cron files from legacy location (~/.magic/) to new location (~/.magic/cron/)
func migrateLegacyFiles(home, cronDir, newJobsFile, newLogsFile string) {
	// Migrate cron_jobs.json
	legacyJobsFile := filepath.Join(home, ".magic", "cron_jobs.json")
	if _, err := os.Stat(legacyJobsFile); err == nil {
		// Legacy file exists, migrate it
		if data, err := os.ReadFile(legacyJobsFile); err == nil {
			_ = os.WriteFile(newJobsFile, data, 0644)
			_ = os.Remove(legacyJobsFile)
		}
	}

	// Migrate cron_logs.json
	legacyLogsFile := filepath.Join(home, ".magic", "cron_logs.json")
	if _, err := os.Stat(legacyLogsFile); err == nil {
		// Legacy file exists, migrate it
		if data, err := os.ReadFile(legacyLogsFile); err == nil {
			_ = os.WriteFile(newLogsFile, data, 0644)
			_ = os.Remove(legacyLogsFile)
		}
	}
}
