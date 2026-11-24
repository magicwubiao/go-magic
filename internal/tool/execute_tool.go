package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

const (
	// defaultTimeout is the chat-mode default for execute_command. 120s
	// matches the max timeout a tool-call argument may request in chat mode
	// (see Execute): installs, builds and test runs routinely exceed 30s,
	// and a too-eager default kills them before the model can react. The
	// model can still pass a smaller timeout for quick commands; coding
	// mode raises both via SetCodingMode.
	defaultTimeout  = 120 * time.Second
	maxOutputLength = 50 * 1024 // 50KB
)

// allowedCommands 是"标准工具链清单"：agent 日常会用到的常用命令。
//
// 它不是安全边界（cmd / Windows 壳 / python / node / git / docker / awk / find
// / tar 等本身就能执行任意代码，且执行的是整条 shell 串）。它的作用有两个：
//  1. 软策略（默认）下作为"常见命令"信号——不在清单里的命令由审批层升级为询问；
//  2. 严格模式（SetStrictWhitelist(true)）下作为硬拦截清单。
//
// 因此新增条目只需判断"这是不是常见工具"，安全性由 dangerousPatterns /
// injectionPatterns / 审批层负责。
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

	// Package runners / linters / debuggers —— 内置技能正文会直接建议模型运行这些
	// （如 code-quality 的 npx/gofmt/golangci-lint、debug-go 的 dlv），
	// 缺失会让"文档教模型跑、工具拒绝跑"。
	"npx": true, "bun": true, "bunx": true, "deno": true,
	"gofmt": true, "goimports": true, "golangci-lint": true,
	"staticcheck": true, "dlv": true,
	"uv": true, "uvx": true, "poetry": true, "pipenv": true,
	"ruff": true, "black": true, "mypy": true,
	"http": true, "httpie": true,
	"clinic": true, "kernprof": true,

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
	timeout    time.Duration
	maxOutput  int
	allowAny   bool
	workDir    string
	codingMode bool
	// strictWhitelist 为 true 时，未列入 allowedCommands 的命令在工具层直接拒绝
	// （旧行为）。默认 false：未列出的命令放行给审批层判定，见 checkWhitelist。
	strictWhitelist bool
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
				"type": "string",
				"description": "The command to execute (must be in whitelist). " +
					"Shell selection is automatic: Windows runs it via PowerShell (use ';' to chain commands — '&' is not a command separator there), Unix via bash.",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory (optional)",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 120, max: 120 in chat mode / 600 in coding mode)",
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

	var finalWorkDir string
	if workdir, ok := args["workdir"].(string); ok && workdir != "" {
		resolved, err := resolvePath(ctx, workdir)
		if err != nil {
			return map[string]interface{}{
				"exit_code": 1,
				"error":     fmt.Sprintf("invalid workdir: %v", err),
				"blocked":   "path_not_allowed",
			}, nil
		}
		finalWorkDir = resolved
	} else if ctxWorkDir := WorkDirFromContext(ctx); ctxWorkDir != "" {
		if sessionDir, err := EnsureSessionDir(ctx); err == nil && sessionDir != "" {
			finalWorkDir = sessionDir
		} else {
			finalWorkDir = ctxWorkDir
		}
	} else if t.workDir != "" {
		finalWorkDir = t.workDir
	} else {
		if cwd, err := os.Getwd(); err == nil {
			finalWorkDir = cwd
		}
	}

	if finalWorkDir != "" {
		security := FileSecurityFromContext(ctx)
		if security.Enabled {
			// 工作目录本身始终允许访问，传 finalWorkDir 作为 baseWorkDir
			// 让 checkPathBlocked 跳过对该路径的阻止检查。
			if err := checkPathBlocked(finalWorkDir, finalWorkDir, security); err != nil {
				return map[string]interface{}{
					"exit_code": 1,
					"error":     fmt.Sprintf("workdir blocked: %v", err),
					"blocked":   "path_blocked",
				}, nil
			}
		}
		cmd.Dir = finalWorkDir
	}

	output, err := cmd.CombinedOutput()

	// 中文 Windows 控制台代码页是 GBK：先按"合法 UTF-8 / GB18030 回退"解码，
	// 否则系统命令的中文输出会变成一串替换字符，模型据乱码误判结果。
	outputStr := decodeConsoleOutput(output)
	if len(outputStr) > t.maxOutput {
		outputStr = utils.TruncateDetailed(outputStr, t.maxOutput)
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
			result["hint"] = longRunningTimeoutHint(command, execTimeout)
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
	// 单引号内的内容不会被 shell 展开，$() 与反引号在其中只是字面量，不应误拦截。
	// 因此只对单引号之外的文本做检测。
	unquoted := stripSingleQuoted(cmd)
	for _, pattern := range injectionPatterns {
		if pattern.MatchString(unquoted) {
			return fmt.Errorf("shell injection detected: contains forbidden characters")
		}
	}
	return nil
}

// stripSingleQuoted 去掉单引号包裹的片段（不处理转义，单引号内本就不解释转义）。
// 未闭合的单引号会保留其后的全部内容，以保证安全侧判定。
func stripSingleQuoted(cmd string) string {
	var b strings.Builder
	inSingle := false
	for _, r := range cmd {
		if r == '\'' {
			inSingle = !inSingle
			continue
		}
		if !inSingle {
			b.WriteRune(r)
		}
	}
	if inSingle {
		// 未闭合：整体保守返回原文本，避免因引号不配对而漏检。
		return cmd
	}
	return b.String()
}

// isEnvAssignment 判断是否为 shell 环境变量赋值前缀，形如 NAME=value。
// NAME 必须是合法标识符（字母或下划线开头，后接字母、数字、下划线）。
func isEnvAssignment(word string) bool {
	eq := strings.IndexByte(word, '=')
	if eq <= 0 {
		return false
	}
	for i, r := range word[:eq] {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
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

// BaseCommand 返回命令的首个可执行文件名，跳过开头的 NAME=value 环境变量赋值。
// 例如 "GOOS=linux go build ./..." -> "go"；"FOO=bar" -> ""（只有赋值没有命令）。
func BaseCommand(cmd string) string {
	for _, p := range strings.Fields(cmd) {
		if isEnvAssignment(p) {
			continue
		}
		return p
	}
	return ""
}

// IsStandardCommand 报告 binary 是否在标准工具链清单内。
// 注意：该清单是"常见工具"清单，不构成安全边界——安全判定在
// dangerousPatterns / injectionPatterns / 审批层。
func IsStandardCommand(binary string) bool {
	return allowedCommands[binary]
}

// StandardCommands 返回标准工具链清单的排序副本（供诊断/展示/测试使用）。
func StandardCommands() []string {
	out := make([]string, 0, len(allowedCommands))
	for name := range allowedCommands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// UnlistedCommandError 表示命令不在标准工具链清单内。
// 软策略（默认）下不会返回它——未列出的命令交由审批层判定；只有
// SetStrictWhitelist(true) 的严格模式才用它硬拦。
type UnlistedCommandError struct {
	Command string
}

func (e *UnlistedCommandError) Error() string {
	return fmt.Sprintf("command '%s' is not in the standard toolchain list (strict mode)", e.Command)
}

func (t *ExecuteCommandTool) checkWhitelist(cmd string) error {
	if t.allowAny {
		return nil
	}

	// 首个非赋值 token 才是命令本体（"GOOS=linux go build" 的命令是 go）。
	baseCmd := BaseCommand(cmd)
	if baseCmd == "" {
		// 只有赋值没有命令（如 "FOO=bar"），无需执行，视为空命令
		return fmt.Errorf("empty command")
	}
	if allowedCommands[baseCmd] {
		return nil
	}

	// 软策略（默认）：未列出的命令不再在工具层硬拦，改为由审批层按其策略
	// 询问或判定（审批钩子在工具执行前运行，未列入标准工具链的命令会被升级为
	// 询问）。这样既不会出现"文档教模型跑、工具拒绝跑"，也不放宽真正危险的
	// 模式——dangerousPatterns / injectionPatterns 仍然硬拦。
	if !t.strictWhitelist {
		log.Debugf("[ExecuteCommand] %q is not in the standard toolchain list; deferring to approval policy", baseCmd)
		return nil
	}

	return &UnlistedCommandError{Command: baseCmd}
}

// SetAllowAny allows executing arbitrary commands (use with caution)
func (t *ExecuteCommandTool) SetAllowAny(allow bool) {
	t.allowAny = allow
}

// SetStrictWhitelist 切换命令清单的强制模式。
//
// 默认 false（软策略）：未列入标准工具链清单的命令放行给审批层，由审批策略
// 决定询问还是放行（见 checkWhitelist）。
// true：未列出的命令在工具层直接拒绝——只有"只允许已知工具链"的部署才需要，
// 正常开发环境开启会让 npx/gofmt 这类常用命令全部被拒。
func (t *ExecuteCommandTool) SetStrictWhitelist(strict bool) {
	t.strictWhitelist = strict
}

// SetCodingMode enables coding mode with relaxed restrictions
// coding 模式仅放宽超时和输出大小限制，不再绕过 dangerousPatterns/injectionPatterns 检查，
// 也不再设置 allowAny=true，以保留命令白名单校验。
func (t *ExecuteCommandTool) SetCodingMode(enabled bool) {
	t.codingMode = enabled
	if enabled {
		t.timeout = 300 * time.Second // 5 minutes for coding mode (was 120s)
		t.maxOutput = 1024 * 1024     // 1MB output (was 200KB)
		// 不再设置 allowAny = true，保留安全检查
	}
}

// SetCodingModeAdvanced enables advanced coding mode with even more relaxed restrictions
// 同样不再绕过白名单，仅放宽超时和输出限制。
func (t *ExecuteCommandTool) SetCodingModeAdvanced(enabled bool) {
	t.codingMode = enabled
	if enabled {
		t.timeout = 600 * time.Second // 10 minutes for advanced coding
		t.maxOutput = 2 * 1024 * 1024 // 2MB output
		// 不再设置 allowAny = true，保留安全检查
	}
}

// AddToWhitelist adds a command to the allowed list
func (t *ExecuteCommandTool) AddToWhitelist(cmd string) {
	allowedCommands[cmd] = true
}
