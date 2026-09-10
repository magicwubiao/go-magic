package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// TerminalBackend defines the interface for different terminal execution backends
type TerminalBackend interface {
	// Name returns the backend name
	Name() string

	// Description returns the backend description
	Description() string

	// Execute runs a command in the backend
	Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error)

	// IsAvailable checks if the backend is available
	IsAvailable() bool

	// Health checks the backend health
	Health() error
}

// ExecutionResult represents the result of a command execution
type ExecutionResult struct {
	Command   string        `json:"command"`
	ExitCode  int           `json:"exit_code"`
	Output    string        `json:"output"`
	Duration  time.Duration `json:"duration"`
	Backend   string        `json:"backend"`
	Timestamp time.Time     `json:"timestamp"`
}

// LocalBackend executes commands locally
type LocalBackend struct {
	allowAny bool
}

func NewLocalBackend() *LocalBackend {
	return &LocalBackend{allowAny: false}
}

func (b *LocalBackend) Name() string { return "local" }

func (b *LocalBackend) Description() string {
	return "Local terminal execution on the current machine"
}

func (b *LocalBackend) IsAvailable() bool { return true }

func (b *LocalBackend) Health() error {
	cmd := exec.Command("echo", "health check")
	return cmd.Run()
}

func (b *LocalBackend) Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	start := time.Now()

	var execCmd *exec.Cmd
	if strings.Contains(cmd, "powershell") || strings.Contains(cmd, "cmd.exe") {
		execCmd = exec.CommandContext(ctx, "powershell", "-Command", cmd)
	} else {
		execCmd = exec.CommandContext(ctx, "bash", "-c", cmd)
	}

	if workDir != "" {
		execCmd.Dir = workDir
	} else if cwd, err := os.Getwd(); err == nil {
		execCmd.Dir = cwd
	}

	output, err := execCmd.CombinedOutput()

	result := &ExecutionResult{
		Command:   cmd,
		Output:    string(output),
		Duration:  time.Since(start),
		Backend:   "local",
		Timestamp: start,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124
			result.Output += "\n(Error: Command timed out)"
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
			result.Output += "\n(Error: " + err.Error() + ")"
		}
	}

	return result, nil
}

func (b *LocalBackend) SetAllowAny(allow bool) {
	b.allowAny = allow
}

// DockerBackend executes commands in Docker containers
type DockerBackend struct {
	image       string
	networkMode string
	memoryLimit string
	cpuLimit    string
	privileged  bool
}

func NewDockerBackend() *DockerBackend {
	return &DockerBackend{
		image:       "golang:1.25-alpine",
		networkMode: "bridge",
		memoryLimit: "512m",
		cpuLimit:    "1.0",
		privileged:  false,
	}
}

func (b *DockerBackend) Name() string { return "docker" }

func (b *DockerBackend) Description() string {
	return "Execute commands in isolated Docker containers"
}

func (b *DockerBackend) IsAvailable() bool {
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}

func (b *DockerBackend) Health() error {
	cmd := exec.Command("docker", "ps")
	return cmd.Run()
}

func (b *DockerBackend) SetImage(image string)       { b.image = image }
func (b *DockerBackend) SetMemoryLimit(limit string) { b.memoryLimit = limit }
func (b *DockerBackend) SetCpuLimit(limit string)    { b.cpuLimit = limit }

func (b *DockerBackend) Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	start := time.Now()

	// Build docker run command
	args := []string{"run", "--rm"}

	// Add resource limits
	if b.memoryLimit != "" {
		args = append(args, "--memory", b.memoryLimit)
	}
	if b.cpuLimit != "" {
		args = append(args, "--cpus", b.cpuLimit)
	}

	// Add working directory
	if workDir != "" {
		args = append(args, "-w", workDir)
	} else {
		args = append(args, "-w", "/workspace")
	}

	// Add current user to avoid root in container
	args = append(args, "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))

	// Add image and command
	args = append(args, b.image, "/bin/sh", "-c", cmd)

	execCmd := exec.CommandContext(ctx, "docker", args...)
	output, err := execCmd.CombinedOutput()

	result := &ExecutionResult{
		Command:   cmd,
		Output:    string(output),
		Duration:  time.Since(start),
		Backend:   "docker",
		Timestamp: start,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// SSHBackend executes commands over SSH
type SSHBackend struct {
	host     string
	user     string
	keyPath  string
	password string
	port     int
}

func NewSSHBackend() *SSHBackend {
	return &SSHBackend{
		port: 22,
	}
}

func (b *SSHBackend) Name() string { return "ssh" }

func (b *SSHBackend) Description() string {
	return "Execute commands on remote servers via SSH"
}

func (b *SSHBackend) IsAvailable() bool {
	// Check if SSH client is available
	cmd := exec.Command("ssh", "-V")
	return cmd.Run() == nil
}

func (b *SSHBackend) Health() error {
	if b.host == "" {
		return fmt.Errorf("SSH host not configured")
	}
	return nil
}

func (b *SSHBackend) Configure(host, user, keyPath, password string, port int) {
	b.host = host
	b.user = user
	b.keyPath = keyPath
	b.password = password
	b.port = port
}

func (b *SSHBackend) Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	start := time.Now()

	if b.host == "" {
		return nil, fmt.Errorf("SSH host not configured")
	}

	args := []string{}

	// SSH options for non-interactive execution
	args = append(args, "-o", "StrictHostKeyChecking=no")
	args = append(args, "-o", "UserKnownHostsFile=/dev/null")
	args = append(args, "-o", "BatchMode=yes")

	if b.keyPath != "" {
		args = append(args, "-i", b.keyPath)
	}

	if b.port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", b.port))
	}

	target := b.user + "@" + b.host
	args = append(args, target)

	// Wrap command with cd if workDir specified
	execCmd := cmd
	if workDir != "" {
		execCmd = fmt.Sprintf("cd %s && %s", workDir, cmd)
	}

	args = append(args, execCmd)

	execCmdObj := exec.CommandContext(ctx, "ssh", args...)
	output, err := execCmdObj.CombinedOutput()

	result := &ExecutionResult{
		Command:   cmd,
		Output:    string(output),
		Duration:  time.Since(start),
		Backend:   "ssh",
		Timestamp: start,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// BackendManager manages terminal backends
type BackendManager struct {
	backends       map[string]TerminalBackend
	mu             sync.RWMutex
	defaultBackend string
}

func NewBackendManager() *BackendManager {
	m := &BackendManager{
		backends:       make(map[string]TerminalBackend),
		defaultBackend: "local",
	}

	// Register default backends
	m.Register(NewLocalBackend())
	m.Register(NewDockerBackend())
	m.Register(NewSSHBackend())
	m.Register(NewDaytonaBackend())
	m.Register(NewSingularityBackend())
	m.Register(NewModalBackend())

	return m
}

func (m *BackendManager) Register(backend TerminalBackend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[backend.Name()] = backend
}

func (m *BackendManager) Get(name string) TerminalBackend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backends[name]
}

func (m *BackendManager) SetDefault(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.backends[name]; ok {
		m.defaultBackend = name
	}
}

func (m *BackendManager) GetDefault() TerminalBackend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backends[m.defaultBackend]
}

func (m *BackendManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}
	return names
}

func (m *BackendManager) Execute(ctx context.Context, backendName string, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	backend := m.Get(backendName)
	if backend == nil {
		backend = m.GetDefault()
	}
	return backend.Execute(ctx, cmd, workDir, timeout)
}

// TerminalTool provides enhanced terminal execution with backend support
type TerminalTool struct {
	manager  *BackendManager
	baseTool *ExecuteCommandTool
}

func NewTerminalTool() *TerminalTool {
	return &TerminalTool{
		manager:  NewBackendManager(),
		baseTool: NewSecureExecuteCommandTool(""),
	}
}

func (t *TerminalTool) Name() string { return "terminal" }

func (t *TerminalTool) Description() string {
	return "Execute shell commands. Supports multiple backends: local, docker, ssh"
}

func (t *TerminalTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The command to execute",
			},
			"backend": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"local", "docker", "ssh"},
				"description": "Execution backend to use (default: local)",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *TerminalTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	command, _ := args["command"].(string)
	backendName, _ := args["backend"].(string)
	workDir, _ := args["workdir"].(string)
	timeout := 30.0

	if tArg, ok := args["timeout"].(float64); ok {
		timeout = tArg
	}

	// Use base tool for local execution with security checks
	if backendName == "" || backendName == "local" {
		return t.baseTool.Execute(ctx, args)
	}

	// Use backend manager for other backends
	timeoutDur := time.Duration(timeout) * time.Second
	if timeoutDur > 120*time.Second {
		timeoutDur = 120 * time.Second
	}

	result, err := t.manager.Execute(ctx, backendName, command, workDir, timeoutDur)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"command":     result.Command,
		"exit_code":   result.ExitCode,
		"output":      result.Output,
		"backend":     result.Backend,
		"duration_ms": result.Duration.Milliseconds(),
	}, nil
}

// DaytonaBackend executes commands via Daytona (serverless dev environments)
type DaytonaBackend struct {
	workspace string
	image     string
	language  string
	serverURL string
	apiKey    string
	persist   bool // Persist environment between sessions
	autoWake  bool // Auto-wake on demand
}

func NewDaytonaBackend() *DaytonaBackend {
	return &DaytonaBackend{
		workspace: "go-magic",
		image:     "ubuntu:22.04",
		language:  "go",
		serverURL: "http://localhost:8998",
		persist:   true,
		autoWake:  true,
	}
}

func (b *DaytonaBackend) Name() string { return "daytona" }

func (b *DaytonaBackend) Description() string {
	return "Execute commands in Daytona serverless dev environments (auto-hibernates when idle)"
}

func (b *DaytonaBackend) IsAvailable() bool {
	// Check if daytona CLI is available
	cmd := exec.Command("daytona", "version")
	return cmd.Run() == nil
}

func (b *DaytonaBackend) Health() error {
	// Check Daytona server connection
	cmd := exec.Command("daytona", "ping")
	return cmd.Run()
}

func (b *DaytonaBackend) Configure(workspace, image, language, serverURL, apiKey string) {
	b.workspace = workspace
	b.image = image
	b.language = language
	b.serverURL = serverURL
	b.apiKey = apiKey
}

func (b *DaytonaBackend) SetPersist(persist bool)   { b.persist = persist }
func (b *DaytonaBackend) SetAutoWake(autoWake bool) { b.autoWake = autoWake }

func (b *DaytonaBackend) Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	start := time.Now()

	// Build daytona run command
	args := []string{"run"}

	// Add workspace name
	args = append(args, "--workspace", b.workspace)

	// Add language/image
	if b.image != "" {
		args = append(args, "--image", b.image)
	}

	// Add timeout
	args = append(args, "--timeout", fmt.Sprintf("%d", int(timeout.Seconds())))

	// Set working directory
	if workDir != "" {
		args = append(args, "--dir", workDir)
	}

	// Add command
	args = append(args, "--", cmd)

	execCmd := exec.CommandContext(ctx, "daytona", args...)
	output, err := execCmd.CombinedOutput()

	result := &ExecutionResult{
		Command:   cmd,
		Output:    string(output),
		Duration:  time.Since(start),
		Backend:   "daytona",
		Timestamp: start,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124
			result.Output += "\n(Error: Command timed out)"
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// SingularityBackend executes commands in Singularity containers
type SingularityBackend struct {
	imagePath   string
	bindPaths   []string
	overlayPath string
	noCache     bool
	contain     bool
}

func NewSingularityBackend() *SingularityBackend {
	return &SingularityBackend{
		contain: true,
	}
}

func (b *SingularityBackend) Name() string { return "singularity" }

func (b *SingularityBackend) Description() string {
	return "Execute commands in Singularity containers (for HPC/scientific computing)"
}

func (b *SingularityBackend) IsAvailable() bool {
	cmd := exec.Command("singularity", "version")
	return cmd.Run() == nil
}

func (b *SingularityBackend) Health() error {
	cmd := exec.Command("singularity", "instance", "list")
	return cmd.Run()
}

func (b *SingularityBackend) Configure(imagePath string, bindPaths []string, overlayPath string) {
	b.imagePath = imagePath
	b.bindPaths = bindPaths
	b.overlayPath = overlayPath
}

func (b *SingularityBackend) SetContain(contain bool) { b.contain = contain }

func (b *SingularityBackend) Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	start := time.Now()

	if b.imagePath == "" {
		return nil, fmt.Errorf("Singularity image not configured")
	}

	args := []string{"exec"}

	// Add containment options for security
	if b.contain {
		args = append(args, "--contain")
		args = append(args, "--cleanenv")
	}

	// Add bind paths
	for _, bind := range b.bindPaths {
		args = append(args, "--bind", bind)
	}

	// Add overlay for persistent home
	if b.overlayPath != "" {
		args = append(args, "--overlay", b.overlayPath+":ro")
	}

	// Set working directory
	if workDir != "" {
		args = append(args, "--pwd", workDir)
	}

	// Add image and command
	args = append(args, b.imagePath, "/bin/sh", "-c", cmd)

	execCmd := exec.CommandContext(ctx, "singularity", args...)
	output, err := execCmd.CombinedOutput()

	result := &ExecutionResult{
		Command:   cmd,
		Output:    string(output),
		Duration:  time.Since(start),
		Backend:   "singularity",
		Timestamp: start,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// ModalBackend executes commands via Modal (serverless GPU/CPUs)
type ModalBackend struct {
	appName    string
	volumePath string
	gpu        string
	memory     int // MB
	cpu        float64
}

func NewModalBackend() *ModalBackend {
	return &ModalBackend{
		appName: "go-magic",
		memory:  1024,
		cpu:     1.0,
	}
}

func (b *ModalBackend) Name() string { return "modal" }

func (b *ModalBackend) Description() string {
	return "Execute commands on Modal serverless infrastructure (GPU/CPU, scales to zero)"
}

func (b *ModalBackend) IsAvailable() bool {
	cmd := exec.Command("modal", "token", "verify")
	return cmd.Run() == nil
}

func (b *ModalBackend) Health() error {
	cmd := exec.Command("modal", "setup", "status")
	return cmd.Run()
}

func (b *ModalBackend) Configure(appName, volumePath, gpu string, memory int, cpu float64) {
	b.appName = appName
	b.volumePath = volumePath
	b.gpu = gpu
	b.memory = memory
	b.cpu = cpu
}

func (b *ModalBackend) SetGPU(gpu string)    { b.gpu = gpu }
func (b *ModalBackend) SetMemory(memory int) { b.memory = memory }

func (b *ModalBackend) Execute(ctx context.Context, cmd string, workDir string, timeout time.Duration) (*ExecutionResult, error) {
	start := time.Now()

	args := []string{"run"}

	// Add app name
	args = append(args, "--app", b.appName)

	// Add GPU
	if b.gpu != "" {
		args = append(args, "--gpu", b.gpu)
	}

	// Add memory
	args = append(args, "--memory", fmt.Sprintf("%d", b.memory))

	// Add CPU
	args = append(args, "--cpu", fmt.Sprintf("%.1f", b.cpu))

	// Set timeout (Modal uses seconds)
	args = append(args, "--timeout", fmt.Sprintf("%d", int(timeout.Seconds())))

	// Add volume mount
	if b.volumePath != "" {
		args = append(args, "--volume", b.volumePath)
	}

	// Add command
	args = append(args, "--", cmd)

	execCmd := exec.CommandContext(ctx, "modal", args...)
	output, err := execCmd.CombinedOutput()

	result := &ExecutionResult{
		Command:   cmd,
		Output:    string(output),
		Duration:  time.Since(start),
		Backend:   "modal",
		Timestamp: start,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	return result, nil
}

// ProcessTool manages background processes
type ProcessTool struct {
	processes map[string]*ProcessInfo
	mu        sync.Mutex
	seq       int
}

// procLogBuf is a bounded tail buffer capturing combined stdout/stderr.
type procLogBuf struct {
	mu  sync.Mutex
	buf []byte
}

const procLogMaxBytes = 64 * 1024

func (b *procLogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > procLogMaxBytes {
		// Keep the tail; align to next newline for readability.
		b.buf = b.buf[len(b.buf)-procLogMaxBytes:]
		if idx := indexByte(b.buf, '\n'); idx >= 0 && idx < len(b.buf)-1 {
			b.buf = b.buf[idx+1:]
		}
	}
	return len(p), nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func (b *procLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

type ProcessInfo struct {
	ID       string    `json:"id"`
	Command  string    `json:"command"`
	Backend  string    `json:"backend"`
	WorkDir  string    `json:"workdir"`
	PID      int       `json:"pid"`
	Start    time.Time `json:"start"`
	Status   string    `json:"status"`
	ExitCode int       `json:"exit_code,omitempty"`

	cmd   *exec.Cmd
	stdin io.WriteCloser
	log   *procLogBuf
}

func NewProcessTool() *ProcessTool {
	return &ProcessTool{
		processes: make(map[string]*ProcessInfo),
	}
}

func (t *ProcessTool) Name() string { return "process" }

// procSnapshot returns a shallow copy of the process record so mutable
// fields (Status/ExitCode, written by the reaper goroutine under the
// lock) are never read outside the lock — otherwise `-race` flags the
// reaper's write against poll/wait/log reads. The cmd/stdin/log
// pointers in the copy are shared on purpose: the log buffer has its
// own mutex, and kill/write run under the tool lock.
func (t *ProcessTool) procSnapshot(sessionID string) (ProcessInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.processes[sessionID]
	if !ok {
		return ProcessInfo{}, false
	}
	return *p, true
}

func (t *ProcessTool) Description() string {
	return "Start and manage BACKGROUND processes. Actions: run (start a command in background, returns session_id immediately — use this instead of blocking the terminal for long-running commands), list, poll (check status), wait (block until exit or timeout), kill, write (write to process stdin), log (read captured output so far)."
}

func (t *ProcessTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"run", "list", "poll", "wait", "kill", "write", "log"},
				"description": "Action to perform. run=start a new background process (requires command); poll/wait/kill/write/log operate on an existing session_id.",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Process session ID (returned by run)",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Command to run (required for action=run)",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory (for action=run)",
			},
			"data": map[string]interface{}{
				"type":        "string",
				"description": "Data to write to process stdin (for action=write)",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Max seconds to block for action=wait (default: 30)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ProcessTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action, _ := args["action"].(string)

	switch action {
	case "run", "start": // "start" is a common LLM alias
		return t.runProcess(ctx, args)

	case "list":
		t.mu.Lock()
		defer t.mu.Unlock()

		processes := make([]map[string]interface{}, 0)
		for _, p := range t.processes {
			processes = append(processes, map[string]interface{}{
				"session_id": p.ID,
				"command":    p.Command,
				"backend":    p.Backend,
				"pid":        p.PID,
				"status":     p.Status,
				"start":      p.Start.Format(time.RFC3339),
			})
		}
		return map[string]interface{}{"processes": processes}, nil

	case "poll":
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return nil, fmt.Errorf("session_id is required for action=poll")
		}

		p, ok := t.procSnapshot(sessionID)
		if !ok {
			return map[string]interface{}{
				"status": "not_found",
				"error":  "Process not found",
			}, nil
		}

		return map[string]interface{}{
			"session_id": p.ID,
			"status":     p.Status,
			"pid":        p.PID,
			"command":    p.Command,
			"exit_code":  p.ExitCode,
		}, nil

	case "wait":
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return nil, fmt.Errorf("session_id is required for action=wait")
		}

		timeout := 30.0
		if tv, ok := args["timeout"].(float64); ok && tv > 0 {
			timeout = tv
		}
		deadline := time.After(time.Duration(timeout * float64(time.Second)))

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			p, ok := t.procSnapshot(sessionID)
			if !ok {
				return map[string]interface{}{
					"session_id": sessionID,
					"status":     "not_found",
					"error":      "Process not found",
				}, nil
			}
			if p.Status != "running" {
				return map[string]interface{}{
					"session_id": p.ID,
					"status":     p.Status,
					"pid":        p.PID,
					"command":    p.Command,
					"exit_code":  p.ExitCode,
				}, nil
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("wait canceled: %w", ctx.Err())
			case <-deadline:
				return map[string]interface{}{
					"session_id": p.ID,
					"status":     p.Status,
					"pid":        p.PID,
					"note":       fmt.Sprintf("still running after %.0fs; use action=log to inspect output", timeout),
				}, nil
			case <-ticker.C:
			}
		}

	case "kill":
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return nil, fmt.Errorf("session_id is required for action=kill")
		}

		t.mu.Lock()
		defer t.mu.Unlock()

		if p, ok := t.processes[sessionID]; ok {
			// Cross-platform: Process.Kill works on Windows where the `kill`
			// shell command does not exist.
			if p.cmd != nil && p.cmd.Process != nil && p.Status == "running" {
				_ = p.cmd.Process.Kill()
			}
			p.Status = "killed"
		}

		return map[string]interface{}{"session_id": sessionID, "status": "killed"}, nil

	case "write":
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return nil, fmt.Errorf("session_id is required for action=write")
		}
		data, _ := args["data"].(string)
		if data == "" {
			return nil, fmt.Errorf("data is required for action=write")
		}

		t.mu.Lock()
		p, ok := t.processes[sessionID]
		t.mu.Unlock()
		if !ok {
			return map[string]interface{}{
				"session_id": sessionID,
				"status":     "not_found",
				"error":      "Process not found",
			}, nil
		}
		if p.stdin == nil {
			return nil, fmt.Errorf("process %s has no stdin (it was not started with a stdin pipe)", sessionID)
		}
		if _, err := io.WriteString(p.stdin, data); err != nil {
			return nil, fmt.Errorf("failed to write to process stdin: %w", err)
		}
		return map[string]interface{}{"session_id": sessionID, "written": len(data)}, nil

	case "log":
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return nil, fmt.Errorf("session_id is required for action=log")
		}

		p, ok := t.procSnapshot(sessionID)
		if !ok {
			return map[string]interface{}{
				"session_id": sessionID,
				"status":     "not_found",
				"error":      "Process not found",
			}, nil
		}

		return map[string]interface{}{
			"session_id": p.ID,
			"status":     p.Status,
			"pid":        p.PID,
			"exit_code":  p.ExitCode,
			"output":     p.log.String(),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (valid actions: run, list, poll, wait, kill, write, log)", action)
	}
}

// runProcess launches a command in the background, registers it and returns
// immediately with the session_id. Combined stdout/stderr is captured into a
// bounded buffer readable via action=log.
func (t *ProcessTool) runProcess(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command is required for action=run")
	}
	workDir, _ := args["workdir"].(string)

	execCmd := exec.Command("bash", "-c", command)
	execCmd.Dir = workDir
	if execCmd.Dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			execCmd.Dir = cwd
		}
	}

	logBuf := &procLogBuf{}
	execCmd.Stdout = logBuf
	execCmd.Stderr = logBuf
	stdin, err := execCmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := execCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	t.mu.Lock()
	t.seq++
	info := &ProcessInfo{
		ID:      fmt.Sprintf("proc_%d_%d", time.Now().Unix(), t.seq),
		Command: command,
		Backend: "local",
		WorkDir: execCmd.Dir,
		PID:     execCmd.Process.Pid,
		Start:   time.Now(),
		Status:  "running",
		cmd:     execCmd,
		stdin:   stdin,
		log:     logBuf,
	}
	t.processes[info.ID] = info
	t.mu.Unlock()

	// Reap the process when it exits so it does not linger as a zombie and
	// so poll/wait see the final status + exit code.
	go func() {
		waitErr := execCmd.Wait()
		closeErr := stdin.Close()
		t.mu.Lock()
		defer t.mu.Unlock()
		if info.Status == "killed" {
			return
		}
		if waitErr == nil {
			info.Status = "exited"
			info.ExitCode = 0
		} else if exitErr, ok := waitErr.(*exec.ExitError); ok {
			info.Status = "exited"
			info.ExitCode = exitErr.ExitCode()
		} else {
			info.Status = "failed"
			info.ExitCode = -1
			fmt.Fprintf(info.log, "\n(process error: %v)", waitErr)
		}
		if closeErr != nil && waitErr == nil {
			// stdin close race with an already-exited child is harmless
			_ = closeErr
		}
	}()

	return map[string]interface{}{
		"session_id": info.ID,
		"pid":        info.PID,
		"status":     info.Status,
		"command":    info.Command,
		"note":       "Started in background. Use action=log to read output, action=poll/wait to check completion, action=kill to stop.",
	}, nil
}

// ExportTerminalBackendsJSON exports available backends as JSON
func ExportTerminalBackendsJSON() string {
	manager := NewBackendManager()
	backends := manager.List()

	result := make([]map[string]interface{}, 0)
	for _, name := range backends {
		b := manager.Get(name)
		result = append(result, map[string]interface{}{
			"name":        b.Name(),
			"description": b.Description(),
			"available":   b.IsAvailable(),
		})
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return string(jsonBytes)
}
