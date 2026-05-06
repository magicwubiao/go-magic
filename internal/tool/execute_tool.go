package tool

import (
	"context"
	"fmt"
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
}

// Dangerous patterns - commands that can cause harm
var dangerousPatterns = []*regexp.Regexp{
	// File destruction
	regexp.MustCompile(`(?i)rm\s+-rf\s+/`),
	regexp.MustCompile(`(?i)del\s+/[sfq]\s+`),
	regexp.MustCompile(`(?i)format\s+[a-z]:`),
	regexp.MustCompile(`(?i)shred\s+`),
	// System modification
	regexp.MustCompile(`(?i)chmod\s+777`),
	regexp.MustCompile(`(?i)/etc/passwd`),
	regexp.MustCompile(`(?i)visudo`),
	regexp.MustCompile(`(?i)sudo\s+su`),
	regexp.MustCompile(`(?i)shutdown`),
	regexp.MustCompile(`(?i)reboot`),
	regexp.MustCompile(`(?i)init\s+0`),
	regexp.MustCompile(`(?i)systemctl\s+kill`),
	regexp.MustCompile(`(?i)pkill\s+-9`),
	// Network abuse
	regexp.MustCompile(`(?i)ddos`),
	regexp.MustCompile(`(?i)flood`),
	regexp.MustCompile(`(?i)ping\s+-f`),
	regexp.MustCompile(`(?i)smurf`),
	regexp.MustCompile(`(?i)netcat\s+-e`),
	regexp.MustCompile(`(?i)nc\s+-e`),
	// Credential theft
	regexp.MustCompile(`(?i)wget.*\|.*bash`),
	regexp.MustCompile(`(?i)curl.*\|.*sh`),
	regexp.MustCompile(`(?i)eval\s+\$\(`),
	regexp.MustCompile(`(?i)base64\s+-d.*\$`),
	regexp.MustCompile(`(?i)cat\s+/etc/shadow`),
	// Process injection
	regexp.MustCompile(`(?i)fork\s+bomb`),
	regexp.MustCompile(`(?i):\(`),
	regexp.MustCompile(`(?i);.*rm`),
	regexp.MustCompile(`(?i)&&.*rm`),
	regexp.MustCompile(`(?i)\|\|.*rm`),
	// Package manipulation
	regexp.MustCompile(`(?i)apt-get\s+remove.*--purge`),
	regexp.MustCompile(`(?i)yum\s+remove`),
	regexp.MustCompile(`(?i)rpm\s+-e\s+--nodeps`),
	// Service disruption
	regexp.MustCompile(`(?i)systemctl\s+stop\s+(sshd|firewalld|apache|httpd)`),
	regexp.MustCompile(`(?i)service\s+.*stop`),
	// Registry/system file deletion (Windows)
	regexp.MustCompile(`(?i)reg\s+delete`),
	regexp.MustCompile(`(?i)del\s+/f/s/q\s+.*windows`),
	regexp.MustCompile(`(?i)attrib\s+-h\s+-r`),
}

// Shell injection patterns
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[;&|]`),             // ; & |
	regexp.MustCompile("\x60"),              // backtick
	regexp.MustCompile(`\$\(`),              // $(
	regexp.MustCompile(`(^|\s)xargs(\s|$)`), // xargs
}

type ExecuteCommandTool struct {
	timeout   time.Duration
	maxOutput int
	allowAny  bool
}

func NewSecureExecuteCommandTool() *ExecuteCommandTool {
	return &ExecuteCommandTool{
		timeout:   defaultTimeout,
		maxOutput: maxOutputLength,
		allowAny:  false,
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
	if !ok {
		return nil, fmt.Errorf("command argument is required")
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}

	// Check for shell injection
	if err := t.checkInjection(command); err != nil {
		return map[string]interface{}{
			"exit_code": 1,
			"error":     err.Error(),
			"blocked":   "injection",
		}, nil
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
			"allowed":   "See execute_command tool for list of allowed commands",
		}, nil
	}

	// Build the command
	execTimeout := t.timeout
	if timeoutArg, ok := args["timeout"].(float64); ok {
		execTimeout = time.Duration(timeoutArg) * time.Second
		if execTimeout > 120*time.Second {
			execTimeout = 120 * time.Second // Max 2 minutes
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
			result["exit_code"] = 1
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

	// Extract the base command
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

// AddToWhitelist adds a command to the allowed list
func (t *ExecuteCommandTool) AddToWhitelist(cmd string) {
	allowedCommands[cmd] = true
}
