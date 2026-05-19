package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	defaultTimeout  = 30 * time.Second
	maxOutputLength = 50 * 1024 // 50KB
)

// Allowed commands whitelist - expand as needed
var allowedCommands = map[string]bool{
	// File operations
	"ls": true, "dir": true, "cat": true, "type": true,
	"find": true, "grep": true, "wc": true, "head": true,
	"tail": true, "stat": true,
	"mkdir": true, "rmdir": true, "touch": true,
	"cp": true, "mv": true, "rm": true, "ln": true,
	"chmod": true, "chown": true,

	// Git operations
	"git": true, "github": true, "glab": true,

	// Development tools
	"go": true, "node": true, "npm": true, "pnpm": true,
	"yarn": true, "python": true, "python3": true,
	"pip": true, "pip3": true, "cargo": true, "rustc": true,
	"make": true, "cmake": true, "gcc": true, "g++": true,
	"clang": true, "clang++": true,

	// Container tools
	"docker": true, "kubectl": true, "helm": true,

	// System info
	"pwd": true, "whoami": true, "uname": true,
	"hostname": true, "uptime": true, "df": true,
	"du": true, "free": true, "ps": true, "top": true,
	"htop": true, "env": true, "date": true, "echo": true,
	"which": true, "where": true,

	// Network tools
	"curl": true, "wget": true, "ping": true,
	"traceroute": true, "netstat": true, "ss": true,
	"ip": true, "nslookup": true, "dig": true, "host": true,

	// Archive tools
	"tar": true, "gzip": true, "gunzip": true,
	"zip": true, "unzip": true, "7z": true,

	// Text tools
	"sed": true, "awk": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "jq": true,

	// Misc
	"systeminfo": true, "ver": true,

	// Windows-specific commands
	"del": true, "copy": true, "xcopy": true, "robocopy": true,
	"move": true, "rename": true, "ren": true,
	"attrib": true, "icacls": true, "takeown": true,
	"cd": true, "chdir": true,
	"cls": true, "color": true, "title": true,
	"tasklist": true, "taskkill": true,
	"reg": true, "schtasks": true,
	"powershell": true, "cmd": true,
	"findstr": true, "more": true,
	"set": true, "path": true,
	"fc": true, "comp": true,
	"diskpart": true, "chkdsk": true, "sfc": true,
	"driverquery": true, "msinfo32": true,
}

// dangerousPatterns checks for genuinely dangerous commands.
// Uses ^ prefix to anchor at start to avoid false positives on intermediate pipe outputs.
var dangerousPatterns = []*regexp.Regexp{
	// File destruction
	regexp.MustCompile(`(?i)^rm\s+-rf\s+/`),
	regexp.MustCompile(`(?i)^del\s+/[sfq]\s+`),
	regexp.MustCompile(`(?i)^format\s+[a-z]:`),
	regexp.MustCompile(`(?i)^shred\s+`),
	// System modification
	regexp.MustCompile(`(?i)^chmod\s+777\s`),
	regexp.MustCompile(`(?i)^sudo\s+rm`),
	regexp.MustCompile(`(?i)^visudo`),
	regexp.MustCompile(`(?i)^sudo\s+su$`),
	regexp.MustCompile(`(?i)^shutdown`),
	regexp.MustCompile(`(?i)^reboot`),
	regexp.MustCompile(`(?i)^init\s+0`),
	regexp.MustCompile(`(?i)^systemctl\s+kill`),
	regexp.MustCompile(`(?i)^pkill\s+-9`),
	// Credential theft (only at start of command)
	regexp.MustCompile(`(?i)^cat\s+/etc/shadow`),
	regexp.MustCompile(`(?i)^type\s+/etc/shadow`),
	// Pipe to shell (only when wget/curl is the first command)
	regexp.MustCompile(`(?i)^(wget|curl)\s+.*\|\s*(bash|sh|powershell)`),
	// Command substitution
	regexp.MustCompile(`(?i)^eval\s`),
	// Network abuse
	regexp.MustCompile(`(?i)^ddos`),
	regexp.MustCompile(`(?i)^flood`),
	regexp.MustCompile(`(?i)^ping\s+-f`),
	regexp.MustCompile(`(?i)^smurf`),
	regexp.MustCompile(`(?i)^netcat\s+-e`),
	regexp.MustCompile(`(?i)^nc\s+-e`),
	// Process injection
	regexp.MustCompile(`(?i)^:\s*\(\s*\)\s*\{`),
	// Package manipulation
	regexp.MustCompile(`(?i)^apt-get\s+remove\s+.*--purge`),
	regexp.MustCompile(`(?i)^yum\s+remove`),
	regexp.MustCompile(`(?i)^rpm\s+-e\s+--nodeps`),
	// Service disruption
	regexp.MustCompile(`(?i)^systemctl\s+stop\s+(sshd|firewalld|apache|httpd)`),
	regexp.MustCompile(`(?i)^service\s+.*stop`),
	// Dangerous Windows commands
	regexp.MustCompile(`(?i)^reg\s+delete\s+HK`),
	regexp.MustCompile(`(?i)^taskkill\s+/f\s+/im\s+.*\.exe`),
	regexp.MustCompile(`(?i)^del\s+/f/s/q\s+.*windows`),
}

// injectionPatterns detects actual shell injection attempts vs normal pipe/chain usage.
// Only blocks truly dangerous patterns like backtick execution and command substitution.
var injectionPatterns = []*regexp.Regexp{
	// Reject backtick execution (always dangerous)
	regexp.MustCompile("`[^`]+`"),
	// Reject $() command substitution (always dangerous)
	regexp.MustCompile(`\$\(`),
}

// pipeAndChainPatterns detect dangerous patterns in pipe/chain contexts.
// These patterns specifically require shell operators (|, ;, &&, ||) plus dangerous operations after them.
var pipeAndChainPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	// rm after pipe or chain (dangerous - file deletion)
	{regexp.MustCompile(`(?i)[|;&]\s*rm\s+(-rf\s+)?[/~\\]`), "rm dangerous after pipe/chain"},
	// wget/curl piped to shell (dangerous - remote code execution)
	{regexp.MustCompile(`(?i)(wget|curl)\s+.*\|\s*(bash|sh|powershell)`), "remote download piped to shell"},
	// dd after pipe (dangerous - disk destruction)
	{regexp.MustCompile(`(?i)[|;&]\s*dd\s+if=`), "dd dangerous after pipe/chain"},
	// mkfs after pipe (dangerous - filesystem destruction)
	{regexp.MustCompile(`(?i)[|;&]\s*mkfs`), "mkfs dangerous after pipe/chain"},
	// shutdown/reboot after pipe
	{regexp.MustCompile(`(?i)[|;&]\s*(shutdown|reboot|halt)`), "shutdown/reboot after pipe/chain"},
	// sudo after pipe (dangerous privilege escalation)
	{regexp.MustCompile(`(?i)[|;&]\s*sudo\s+(rm|shutdown|reboot|halt)`), "sudo dangerous after pipe/chain"},
}

type ExecuteCommandTool struct {
	timeout   time.Duration
	maxOutput int
	allowAny  bool
	workDir   string
	codingMode bool
}

func NewSecureExecuteCommandTool(workDir string) *ExecuteCommandTool {
	return &ExecuteCommandTool{
		timeout:   defaultTimeout,
		maxOutput: maxOutputLength,
		allowAny:  false,
		workDir:   workDir,
	}
}

func (t *ExecuteCommandTool) Name() string {
	return "execute_command"
}

func (t *ExecuteCommandTool) Description() string {
	return "Execute a shell command safely with whitelisted commands"
}

func (t *ExecuteCommandTool) Parameters() map[string]interface{} {
	return t.Schema()
}

func (t *ExecuteCommandTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The command to execute (must be in whitelist)",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory (optional)",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecuteCommandTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		// Return helpful message instead of error to avoid LLM retry loop
		return map[string]interface{}{
			"error":   "command argument is required",
			"hint":    "Please provide the command to execute, e.g. {\"command\": \"ls -la\"}",
			"success": false,
		}, nil
	}

	command = strings.TrimSpace(command)
	if command == "" {
		// Return helpful message instead of error to avoid LLM retry loop
		return map[string]interface{}{
			"error":   "command cannot be empty",
			"hint":    "Please provide a non-empty command to execute",
			"success": false,
		}, nil
	}

	// Check for shell injection (backtick, $()) - skipped in coding mode
	if !t.codingMode {
		if err := t.checkInjection(command); err != nil {
			return map[string]interface{}{
				"exit_code": 1,
				"error":     err.Error(),
				"blocked":   "injection",
			}, nil
		}

		// Check for pipe/chain dangerous usage (must run before checkDangerous
		// to catch patterns like "curl ... | bash" which span both)
		if err := t.checkPipeAndChain(command); err != nil {
			return map[string]interface{}{
				"exit_code": 1,
				"error":     err.Error(),
				"blocked":   "dangerous_pipe",
			}, nil
		}
	}

	// Check for dangerous patterns
	if err := t.checkDangerous(command); err != nil {
		return map[string]interface{}{
			"exit_code": 1,
			"error":     err.Error(),
			"blocked":   "dangerous",
		}, nil
	}

	// Check whitelist
	if err := t.checkWhitelist(command); err != nil {
		return map[string]interface{}{
			"exit_code": 1,
			"error":     err.Error(),
			"blocked":   "not_whitelisted",
		}, nil
	}

	// Build the command
	execTimeout := t.timeout
	if timeoutArg, ok := args["timeout"].(float64); ok {
		execTimeout = time.Duration(timeoutArg) * time.Second
		// In coding mode, allow up to 10 minutes; otherwise max 2 minutes
		maxTimeout := 120 * time.Second
		if t.codingMode {
			maxTimeout = 600 * time.Second
		}
		if execTimeout > maxTimeout {
			execTimeout = maxTimeout
		}
	}

	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", command)
	}

	if workdir, ok := args["workdir"].(string); ok && workdir != "" {
		cmd.Dir = workdir
	} else if t.workDir != "" {
		cmd.Dir = t.workDir
	} else {
		// Fallback to current working directory if no workDir configured
		if cwd, err := os.Getwd(); err == nil {
			cmd.Dir = cwd
		}
	}

	output, err := cmd.CombinedOutput()

	// Truncate output if too long
	outputStr := string(output)
	if len(outputStr) > t.maxOutput {
		outputStr = outputStr[:t.maxOutput] + fmt.Sprintf("\n... [output truncated, total %d bytes]", len(output))
	}

	result := map[string]interface{}{
		"command":   command,
		"exit_code": 0,
		"output":    outputStr,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result["exit_code"] = 124
			result["error"] = "command timed out"
			result["timeout_seconds"] = execTimeout.Seconds()
		} else {
			exitCode := 1
			// Try to extract exit code from exec error
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			result["exit_code"] = exitCode
			result["error"] = err.Error()
		}
	}

	return result, nil
}

func (t *ExecuteCommandTool) checkInjection(cmd string) error {
	for _, pattern := range injectionPatterns {
		if pattern.MatchString(cmd) {
			return fmt.Errorf("shell injection detected: contains forbidden characters")
		}
	}
	return nil
}

func (t *ExecuteCommandTool) checkPipeAndChain(cmd string) error {
	for _, pc := range pipeAndChainPatterns {
		if pc.pattern.MatchString(cmd) {
			return fmt.Errorf("command blocked: %s", pc.reason)
		}
	}
	return nil
}

func (t *ExecuteCommandTool) checkDangerous(cmd string) error {
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(cmd) {
			return fmt.Errorf("dangerous command pattern detected and blocked")
		}
	}
	return nil
}

func (t *ExecuteCommandTool) checkWhitelist(cmd string) error {
	if t.allowAny {
		return nil
	}

	// Extract the base command - get the first word before any shell operators
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	baseCmd := parts[0]
	if !allowedCommands[baseCmd] {
		return fmt.Errorf("command '%s' is not in the whitelist", baseCmd)
	}

	return nil
}

// SetAllowAny allows executing arbitrary commands (use with caution)
func (t *ExecuteCommandTool) SetAllowAny(allow bool) {
	t.allowAny = allow
}

// SetCodingMode enables coding mode with relaxed restrictions
func (t *ExecuteCommandTool) SetCodingMode(enabled bool) {
	t.codingMode = enabled
	if enabled {
		t.timeout = 300 * time.Second  // 5 minutes for coding mode (was 120s)
		t.maxOutput = 1024 * 1024      // 1MB output (was 200KB)
		t.allowAny = true
	}
}

// SetCodingModeAdvanced enables advanced coding mode with even more relaxed restrictions
func (t *ExecuteCommandTool) SetCodingModeAdvanced(enabled bool) {
	t.codingMode = enabled
	if enabled {
		t.timeout = 600 * time.Second  // 10 minutes for advanced coding
		t.maxOutput = 2 * 1024 * 1024  // 2MB output
		t.allowAny = true
	}
}

// AddToWhitelist adds a command to the allowed list
func (t *ExecuteCommandTool) AddToWhitelist(cmd string) {
	allowedCommands[cmd] = true
}
