package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
	"golang.org/x/term"
)

// defaultApprovalTimeout is the default CLI confirmation timeout.
const defaultApprovalTimeout = 30 * time.Second

// userAction represents the user's decision during interactive CLI confirmation.
type userAction int

const (
	actionApprove     userAction = iota // [A]pprove - approve this command once
	actionDeny                          // [D]eny - reject this command
	actionTrust                         // [T]rust - trust this pattern permanently
	actionSkipSession                   // [S]kip session - skip approval for similar commands in this session
	actionQuit                          // [Q]uit - abort the entire session
)

// ApprovalHook provides command approval functionality using the smart approval system.
// It supports CLI interactive prompts with rich risk display, Web-based approval mode,
// TUI mode via injectable PromptFunc, approval timeout handling, and session-level skip behavior.
type ApprovalHook struct {
	manager      *approval.Manager
	webMode      bool
	cliTimeout   time.Duration
	skipMutex    sync.Mutex
	skipPatterns map[string]map[string]bool // sessionID -> normalizedPattern -> skipped

	// promptFunc allows TUI or other non-stdio environments to provide
	// a custom confirmation prompt. When nil, the default CLI stdin reader is used.
	promptFunc func(command, reason string, riskLevel approval.RiskLevel) bool
}

// ApprovalPromptFunc is the signature for a custom approval prompt.
// Returns true to approve, false to deny.
type ApprovalPromptFunc func(command, reason string, riskLevel approval.RiskLevel) bool

// NewApprovalHook creates a new approval hook with smart approval.
func NewApprovalHook() *ApprovalHook {
	mgr, err := approval.NewManager(nil)
	if err != nil {
		mgr, _ = approval.NewManager(approval.DefaultConfig())
	}
	return &ApprovalHook{
		manager:      mgr,
		cliTimeout:   defaultApprovalTimeout,
		skipPatterns: make(map[string]map[string]bool),
	}
}

// NewApprovalHookWithManager creates a new approval hook using an existing manager.
func NewApprovalHookWithManager(mgr *approval.Manager) *ApprovalHook {
	if mgr == nil {
		return NewApprovalHook()
	}
	return &ApprovalHook{
		manager:      mgr,
		cliTimeout:   defaultApprovalTimeout,
		skipPatterns: make(map[string]map[string]bool),
	}
}

// Name returns the hook name.
func (h *ApprovalHook) Name() string { return "approval" }

// SetWebMode enables or disables Web-based approval mode.
// When enabled, user-facing confirmations are routed through the Web callback
// system (PendingWebApproval) instead of CLI prompts.
func (h *ApprovalHook) SetWebMode(enabled bool) {
	h.webMode = enabled
}

// SetPromptFunc injects a custom approval prompt function for TUI or other
// non-stdio environments. When set, this function is called instead of the
// default CLI stdin reader.
func (h *ApprovalHook) SetPromptFunc(fn ApprovalPromptFunc) {
	h.promptFunc = fn
}

// GetManager exposes the underlying approval.Manager for Web API usage.
func (h *ApprovalHook) GetManager() *approval.Manager {
	return h.manager
}

// BeforeTool handles approval for tool execution.
// Intercepts high-risk tools: execute_command, write_file, file_edit, execute_code
func (h *ApprovalHook) BeforeTool(ctx context.Context, call *hooks.ToolCallHookRequest) (*hooks.ToolCallHookRequest, hooks.HookDecision, error) {
	var command string
	var toolDesc string

	switch call.ToolName {
	case "execute_command":
		command, _ = call.ToolArgs["command"].(string)
		toolDesc = "shell command"
	case "write_file":
		path, _ := call.ToolArgs["path"].(string)
		content, _ := call.ToolArgs["content"].(string)
		command = fmt.Sprintf("write_file %s (%d bytes)", path, len(content))
		toolDesc = "file write"
	case "file_edit":
		path, _ := call.ToolArgs["path"].(string)
		command = fmt.Sprintf("file_edit %s", path)
		toolDesc = "file edit"
	case "execute_code":
		lang, _ := call.ToolArgs["language"].(string)
		code, _ := call.ToolArgs["code"].(string)
		command = fmt.Sprintf("execute_code %s (%d bytes)", lang, len(code))
		toolDesc = "code execution"
	default:
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	if command == "" {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	sessionID := getSessionID(ctx)
	workingDir := getWorkingDir(ctx)

	req := &approval.ApprovalRequest{
		Command:    command,
		SessionID:  sessionID,
		WorkingDir: workingDir,
	}
	_ = toolDesc // used for future logging extensions

	// Check session-level skip list first.
	if h.isSessionSkipped(sessionID, command) {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// Request approval from manager.
	result, err := h.manager.RequestApproval(req)
	if err != nil {
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: fmt.Sprintf("Approval error: %v", err),
		}, nil
	}

	// If already approved by policy, notify and continue.
	if result.Approved {
		h.manager.NotifyApproval(result, req)
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// If needs user confirmation.
	if result.AskUser {
		var approved bool

		if h.webMode {
			// Route to Web approval callback.
			webResult, webErr := h.manager.PendingWebApproval(req)
			if webErr != nil {
				return call, hooks.HookDecision{
					Action: hooks.HookActionReject,
					Reason: fmt.Sprintf("Web approval error: %v", webErr),
				}, nil
			}
			approved = webResult.Approved
			// Notify callbacks of the web decision.
			h.manager.NotifyApproval(webResult, req)
		} else if h.promptFunc != nil {
			// TUI or other custom prompt (e.g. bubbletea integration).
			approved = h.promptFunc(command, result.Reason, result.RiskLevel)
			if approved {
				h.manager.Approve(req)
			} else {
				h.manager.Deny(req)
			}
		} else if !isStdinTerminal() {
			// Non-interactive, no custom prompt: auto-approve with warning.
			fmt.Printf("  [AUTO-APPROVED] Non-interactive mode: %s\n", command)
			approved = true
			h.manager.Approve(req)
		} else {
			// CLI interactive prompt with timeout.
			act := h.promptUserConfirmation(ctx, command, result.Reason, result.RiskLevel)
			switch act {
			case actionApprove:
				approved = true
				h.manager.Approve(req)
				h.manager.NotifyApproval(&approval.ApprovalResult{
					Approved:  true,
					Strategy:  result.Strategy,
					Reason:    "User approved via CLI",
					RiskLevel: result.RiskLevel,
				}, req)
			case actionTrust:
				approved = true
				h.manager.Approve(req)
				h.manager.AddToWhitelist(command)
				h.manager.NotifyApproval(&approval.ApprovalResult{
					Approved:  true,
					Trusted:   true,
					Strategy:  result.Strategy,
					Reason:    "User trusted pattern via CLI",
					RiskLevel: result.RiskLevel,
				}, req)
			case actionSkipSession:
				approved = true
				h.addSessionSkip(sessionID, command)
				h.manager.Approve(req)
				h.manager.NotifyApproval(&approval.ApprovalResult{
					Approved:  true,
					Strategy:  result.Strategy,
					Reason:    "User skipped session approval",
					RiskLevel: result.RiskLevel,
				}, req)
			case actionDeny:
				approved = false
				h.manager.Deny(req)
				h.manager.NotifyApproval(&approval.ApprovalResult{
					Approved: false,
					Strategy: result.Strategy,
					Reason:   "User denied via CLI",
				}, req)
			case actionQuit:
				h.manager.Deny(req)
				h.manager.NotifyApproval(&approval.ApprovalResult{
					Approved: false,
					Strategy: result.Strategy,
					Reason:   "User quit session",
				}, req)
				return call, hooks.HookDecision{
					Action: hooks.HookActionReject,
					Reason: "User quit the session",
				}, nil
			}
		}

		if approved {
			return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
		}
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: fmt.Sprintf("User rejected command: %s", result.Reason),
		}, nil
	}

	// Rejected by policy, notify and return.
	h.manager.NotifyApproval(result, req)
	return call, hooks.HookDecision{
		Action: hooks.HookActionReject,
		Reason: result.Reason,
	}, nil
}

// promptUserConfirmation displays a rich interactive CLI prompt and returns the
// user's action. It supports a configurable timeout (default 30s); if the timeout
// expires the command is automatically rejected.
func (h *ApprovalHook) promptUserConfirmation(ctx context.Context, command, reason string, riskLevel approval.RiskLevel) userAction {
	// Display rich approval prompt.
	fmt.Println()
	fmt.Println("  ========================================")
	fmt.Printf("  Command Approval Required\n")
	fmt.Println("  ========================================")
	fmt.Printf("  Command : %s\n", truncateCommand(command, 72))
	fmt.Printf("  Risk    : %s %s\n", riskLevelEmoji(riskLevel), riskLevel)
	fmt.Printf("  Category: %s\n", riskCategoryLabel(command, riskLevel))

	if hasBypassWarning(command) {
		fmt.Printf("  WARNING : Bypass attempt detected!\n")
	}
	fmt.Printf("  Reason  : %s\n", reason)
	fmt.Println("  ----------------------------------------")
	fmt.Println("  [A]pprove  [D]eny  [T]rust  [S]kip session  [Q]uit")
	fmt.Printf("  (timeout: %s): ", h.cliTimeout.Round(time.Second))

	// Non-blocking timeout via goroutine + channel.
	type cliResult struct {
		input string
		err   error
	}
	ch := make(chan cliResult, 1)

	go func() {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		ch <- cliResult{input: line, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			fmt.Printf("\n  Input error: %v -> denied\n", res.err)
			return actionDeny
		}
		input := strings.TrimSpace(strings.ToLower(res.input))
		switch input {
		case "a", "approve":
			return actionApprove
		case "d", "deny":
			return actionDeny
		case "t", "trust":
			return actionTrust
		case "s", "skip":
			return actionSkipSession
		case "q", "quit":
			return actionQuit
		default:
			fmt.Printf("  Unknown option '%s' -> denied\n", input)
			return actionDeny
		}
	case <-time.After(h.cliTimeout):
		fmt.Printf("\n  Approval timed out (%s) -> denied\n", h.cliTimeout.Round(time.Second))
		return actionDeny
	}
}

// AfterTool passes through the result unchanged.
func (h *ApprovalHook) AfterTool(ctx context.Context, result *hooks.ToolResultHookResponse) (*hooks.ToolResultHookResponse, hooks.HookDecision, error) {
	return result, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// BeforeLLM passes through the request unchanged.
func (h *ApprovalHook) BeforeLLM(ctx context.Context, req *hooks.LLMHookRequest) (*hooks.LLMHookRequest, hooks.HookDecision, error) {
	return req, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// AfterLLM passes through the response unchanged.
func (h *ApprovalHook) AfterLLM(ctx context.Context, resp *hooks.LLMHookResponse) (*hooks.LLMHookResponse, hooks.HookDecision, error) {
	return resp, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// ApproveTool handles approval request (for gateway integration).
func (h *ApprovalHook) ApproveTool(ctx context.Context, req *hooks.ToolApprovalRequest) (hooks.ApprovalDecision, error) {
	command, _ := req.ToolArgs["command"].(string)
	sessionID := getSessionID(ctx)
	workingDir := getWorkingDir(ctx)

	approvalReq := &approval.ApprovalRequest{
		Command:    command,
		SessionID:  sessionID,
		WorkingDir: workingDir,
	}
	result, err := h.manager.RequestApproval(approvalReq)
	if err != nil {
		return hooks.ApprovalDecision{Approved: false, Reason: err.Error()}, err
	}

	// Notify callbacks.
	h.manager.NotifyApproval(result, approvalReq)

	return hooks.ApprovalDecision{
		Approved: result.Approved,
		Reason:   result.Reason,
	}, nil
}

// ---------------------------------------------------------------------------
// Session-level skip helpers
// ---------------------------------------------------------------------------

// isSessionSkipped checks whether a command pattern has been skipped for the session.
func (h *ApprovalHook) isSessionSkipped(sessionID, command string) bool {
	h.skipMutex.Lock()
	defer h.skipMutex.Unlock()

	patterns, ok := h.skipPatterns[sessionID]
	if !ok {
		return false
	}
	// Use a simple prefix / contains heuristic: if any skipped pattern is a
	// prefix of the command, consider it skipped.
	for pattern := range patterns {
		if strings.HasPrefix(command, pattern) {
			return true
		}
	}
	return false
}

// addSessionSkip records that a command pattern should be skipped for this session.
func (h *ApprovalHook) addSessionSkip(sessionID, command string) {
	h.skipMutex.Lock()
	defer h.skipMutex.Unlock()

	if _, ok := h.skipPatterns[sessionID]; !ok {
		h.skipPatterns[sessionID] = make(map[string]bool)
	}
	// Extract the binary (first token) as the skip key so that all commands
	// sharing the same binary are skipped.
	tokens := strings.Fields(command)
	if len(tokens) > 0 {
		h.skipPatterns[sessionID][tokens[0]] = true
	}
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

// getSessionID extracts session ID from context
func getSessionID(ctx context.Context) string {
	if id, ok := ctx.Value("session_id").(string); ok {
		return id
	}
	return "cli"
}

// getWorkingDir extracts working_dir from context if available.
func getWorkingDir(ctx context.Context) string {
	if dir, ok := ctx.Value("working_dir").(string); ok {
		return dir
	}
	return ""
}

// ---------------------------------------------------------------------------
// Display helpers
// ---------------------------------------------------------------------------

// riskLevelEmoji returns a text label for the given risk level.
func riskLevelEmoji(level approval.RiskLevel) string {
	switch level {
	case approval.RiskLow:
		return "low"
	case approval.RiskMedium:
		return "medium"
	case approval.RiskHigh:
		return "high"
	case approval.RiskCritical:
		return "critical"
	default:
		return "low"
	}
}

// riskCategoryLabel returns a human-readable risk category label for a command.
func riskCategoryLabel(command string, level approval.RiskLevel) string {
	lower := strings.ToLower(command)

	categories := []struct {
		name     string
		keywords []string
	}{
		{"file_destruct", []string{"rm -rf", "rm -r", "truncate", "shred", "dd if=", "> /dev/"}},
		{"network", []string{"curl ", "wget ", "nc ", "ncat ", "telnet "}},
		{"privilege_esc", []string{"chmod 777", "chown", "sudo ", "su ", "setuid"}},
		{"data_access", []string{"cat /etc/", "cat /var/", ".ssh/", "/etc/shadow", "/etc/passwd"}},
		{"system", []string{"shutdown", "reboot", "halt", "mkfs", "fdisk", "parted"}},
		{"package_mgmt", []string{"pip install", "npm install", "yarn add", "cargo install", "go install", "apt install", "brew install", "docker run"}},
	}

	for _, cat := range categories {
		for _, kw := range cat.keywords {
			if strings.Contains(lower, kw) {
				return cat.name
			}
		}
	}

	// Default category based on risk level.
	switch level {
	case approval.RiskLow:
		return "general"
	case approval.RiskMedium:
		return "moderate"
	case approval.RiskHigh:
		return "elevated"
	case approval.RiskCritical:
		return "critical_operation"
	default:
		return "unknown"
	}
}

// hasBypassWarning returns true if the command contains known bypass patterns.
// This detects obfuscation techniques, not legitimate use of these tools.
func hasBypassWarning(command string) bool {
	lower := strings.ToLower(command)
	// Only flag actual obfuscation attempts: decode+execute or shell pipeline
	bypassIndicators := []string{
		"base64 -d", "xxd -r", "eval $", "| sh", "| bash",
	}
	for _, indicator := range bypassIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// truncateCommand truncates a command string for display purposes.
func truncateCommand(cmd string, maxLen int) string {
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen-3] + "..."
}

// isStdinTerminal returns true if os.Stdin is a real terminal (interactive).
func isStdinTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
