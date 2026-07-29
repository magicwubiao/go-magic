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
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/log"
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
	actionTimeout                       // 超时/取消——非用户主动决策，不污染 pattern 学习
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
	var approvalCtx string

	switch call.ToolName {
	case "execute_command":
		command, _ = call.ToolArgs["command"].(string)
		toolDesc = "shell command"
	case "write_file":
		path, _ := call.ToolArgs["path"].(string)
		content, _ := call.ToolArgs["content"].(string)
		command = fmt.Sprintf("write_file %s (%d bytes)", path, len(content))
		toolDesc = "file write"
		approvalCtx = buildWriteFileContext(path, content)
	case "file_edit":
		path, _ := call.ToolArgs["path"].(string)
		command = fmt.Sprintf("file_edit %s", path)
		toolDesc = "file edit"
		approvalCtx = buildFileEditContext(call.ToolArgs)
	case "execute_code":
		lang, _ := call.ToolArgs["language"].(string)
		code, _ := call.ToolArgs["code"].(string)
		command = fmt.Sprintf("execute_code %s (%d bytes)", lang, len(code))
		toolDesc = "code execution"
		approvalCtx = buildExecuteCodeContext(lang, code)
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
		Context:    approvalCtx,
	}
	_ = toolDesc // used for future logging extensions

	// Check session-level skip list first.
	if h.isSessionSkipped(sessionID, command) {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// Request approval from manager.
	result, err := h.manager.RequestApproval(req)
	if err != nil {
		log.Errorf("RequestApproval error for %q: %v", command, err)
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: fmt.Sprintf("Approval error: %v", err),
		}, nil
	}

	// If already approved by policy, notify and continue.
	// 但对 execute_code / write_file / file_edit 这类工具，其合成命令字符串
	// （如 "execute_code python (50 bytes)"）不包含真实代码/内容，assessRisk
	// 只能给出 Low 风险从而被 smartApprove 自动放行。这里对"仅因 Low 风险被放行"
	// 的内容类工具强制改为询问用户，避免任意代码/文件写入被静默执行。
	// 白名单/受信 pattern/只读命令 的放行 reason 不同，不受影响。
	if result.Approved {
		if isContentTool(call.ToolName) && result.Reason == "Low risk command" {
			result.Approved = false
			result.AskUser = true
			result.Reason = "Code/file operation requires confirmation"
			if req.Reason == "" {
				req.Reason = result.Reason
			}
		} else {
			h.manager.NotifyApproval(result, req)
			return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
		}
	}

	// If needs user confirmation.
	if result.AskUser {
		var approved bool

		// 将 RequestApproval 给出的 reason/risk 写回 req，供 CreatePendingApproval
		// 通过 OnPendingCreated 回调推送给 SSE 流（前端审批卡片展示）。
		if req.Reason == "" {
			req.Reason = result.Reason
		}

		if h.webMode {
			// Route to Web approval callback. 用 select 同时监听 ctx.Done()，
			// 以便 agent 取消时能立即返回，而不是被 PendingWebApproval 阻塞到超时。
			webResult, webErr := h.pendingWebApprovalWithCtx(ctx, req)
			if webErr != nil {
				return call, hooks.HookDecision{
					Action: hooks.HookActionReject,
					Reason: fmt.Sprintf("Web approval error: %v", webErr),
				}, nil
			}
			approved = webResult.Approved
			// 与 CLI 路径对齐：Web 审批结果同样走 Approve/Deny，以触发 pattern
			// 学习、受信标记和历史记录，避免 Web 端批准的命令不学习、统计缺失。
			// 但超时/ctx 取消不应记为"用户拒绝"——否则多次超时会让命令进入 denied
			// pattern，即使用户从未主动拒绝。超时走独立通知路径，不污染 pattern 学习。
			if isTimeoutOrCancelled(webResult) {
				h.manager.NotifyApproval(webResult, req)
			} else if approved {
				h.manager.Approve(req)
			} else {
				h.manager.Deny(req)
				// Notify callbacks of the web decision.
				h.manager.NotifyApproval(webResult, req)
			}
		} else if h.promptFunc != nil {
			// TUI or other custom prompt (e.g. bubbletea integration).
			approved = h.promptFunc(command, result.Reason, result.RiskLevel)
			if approved {
				h.manager.Approve(req)
			} else {
				h.manager.Deny(req)
			}
		} else if !isStdinTerminal() {
			// 非交互模式且没有自定义 prompt：fail-closed（默认拒绝）。
			// 之前的行为是自动批准，这会让 Critical 风险命令在 CI/管道里
			// 静默执行。要求调用方显式配置 web 审批或注入 promptFunc。
			fmt.Printf("  [DENIED] Non-interactive mode requires web approval or promptFunc: %s\n", command)
			approved = false
			h.manager.Deny(req)
			h.manager.NotifyApproval(&approval.ApprovalResult{
				Approved: false,
				Strategy: result.Strategy,
				Reason:   "Non-interactive mode denied (fail-closed)",
			}, req)
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
			case actionTimeout:
				// 超时/取消：不调用 Deny（避免污染 denied pattern 计数），
				// 仅通知回调记录历史，决策为 timeout。
				approved = false
				h.manager.NotifyApproval(&approval.ApprovalResult{
					Approved: false,
					Strategy: result.Strategy,
					Reason:   "Approval timed out",
				}, req)
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

// isContentTool 判断是否为"内容类"工具（执行代码 / 写文件 / 编辑文件）。
// 这类工具的审批命令是合成的（不含真实代码/内容），assessRisk 只能给出 Low 风险，
// 因此需要在 BeforeTool 中强制要求用户确认，避免任意代码或文件写入被静默放行。
func isContentTool(name string) bool {
	switch name {
	case "execute_code", "write_file", "file_edit":
		return true
	}
	return false
}

// pendingWebApprovalWithCtx 包装 h.manager.PendingWebApproval，使其可被 ctx 取消。
// 当 ctx 取消时立即返回 ctx.Err()；后台 goroutine 仍会阻塞到 PendingWebApproval
// 超时或被解析，但 agent 主流程可以及时响应取消信号。
func (h *ApprovalHook) pendingWebApprovalWithCtx(ctx context.Context, req *approval.ApprovalRequest) (*approval.ApprovalResult, error) {
	type webOutcome struct {
		result *approval.ApprovalResult
		err    error
	}
	outcomeCh := make(chan webOutcome, 1)
	go func() {
		r, err := h.manager.PendingWebApproval(req)
		outcomeCh <- webOutcome{result: r, err: err}
	}()
	select {
	case out := <-outcomeCh:
		return out.result, out.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// isTimeoutOrCancelled 判断审批结果是否为超时或取消（非用户主动决策）。
// 这类结果不应走 Approve/Deny 路径污染 pattern 学习，仅通知回调即可。
func isTimeoutOrCancelled(r *approval.ApprovalResult) bool {
	if r == nil {
		return true
	}
	reason := strings.ToLower(r.Reason)
	return strings.Contains(reason, "timed out") ||
		strings.Contains(reason, "timeout") ||
		strings.Contains(reason, "cancelled") ||
		strings.Contains(reason, "canceled")
}

// buildExecuteCodeContext 构造 execute_code 的审批上下文：包含语言和源码预览。
func buildExecuteCodeContext(lang, code string) string {
	if code == "" {
		return ""
	}
	preview := truncateCommand(code, 600)
	if lang == "" {
		lang = "unknown"
	}
	return fmt.Sprintf("language: %s\n--- code preview ---\n%s", lang, preview)
}

// buildWriteFileContext 构造 write_file 的审批上下文：包含目标路径和内容预览。
func buildWriteFileContext(path, content string) string {
	if path == "" && content == "" {
		return ""
	}
	preview := truncateCommand(content, 600)
	return fmt.Sprintf("path: %s\n--- content preview ---\n%s", path, preview)
}

// buildFileEditContext 构造 file_edit 的审批上下文：尝试展示 old/new 字段。
func buildFileEditContext(args map[string]interface{}) string {
	path, _ := args["path"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)
	if path == "" && oldText == "" && newText == "" {
		return ""
	}
	return fmt.Sprintf("path: %s\n--- old ---\n%s\n--- new ---\n%s",
		path,
		truncateCommand(oldText, 300),
		truncateCommand(newText, 300))
}

// promptUserConfirmation displays a rich interactive CLI prompt and returns the
// user's action. It supports a configurable timeout (default 30s); if the timeout
// expires the command is automatically rejected.
//
// 实现说明：stdin 的阻塞读取在 Go 中无法被外部取消（reader.ReadString 会一直
// 等到换行或 EOF）。因此当 ctx 取消或超时时，后台 goroutine 仍会阻塞在
// ReadString 上，直到下一次 stdin 输入到达才会退出。这是已知限制；为避免多个
// goroutine 同时竞争 stdin，本方法在 select 退出后通过 ch（容量 1）确保
// goroutine 最终能把结果写入 channel 而不阻塞自身。如果想彻底避免泄漏，
// 调用方应使用 promptFunc 注入可取消的 TUI 实现。
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
	// 容量为 1 的 channel：即使 select 已通过 ctx.Done()/timeout 退出，
	// 后台 goroutine 仍能写入结果而不阻塞，避免 goroutine 永久挂起在发送上。
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
		fmt.Printf("\n  Approval timed out (%s)\n", h.cliTimeout.Round(time.Second))
		return actionTimeout
	case <-ctx.Done():
		fmt.Printf("\n  Approval cancelled by context (%v)\n", ctx.Err())
		return actionTimeout
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
	// 用归一化后的 key（binary + 子命令）做精确匹配，避免仅凭 binary
	// 名匹配导致同 binary 的其它子命令被错误跳过。
	key := normalizeForSkip(command)
	if key == "" {
		return false
	}
	return patterns[key]
}

// addSessionSkip records that a command pattern should be skipped for this session.
func (h *ApprovalHook) addSessionSkip(sessionID, command string) {
	h.skipMutex.Lock()
	defer h.skipMutex.Unlock()

	if _, ok := h.skipPatterns[sessionID]; !ok {
		h.skipPatterns[sessionID] = make(map[string]bool)
	}
	// 使用 binary + 第一个子命令作为 skip key，而非仅 binary 名。
	// 这样 "git push" 与 "git status" 不会被同一次 skip 误伤。
	key := normalizeForSkip(command)
	if key == "" {
		return
	}
	h.skipPatterns[sessionID][key] = true
}

// ClearSessionSkip removes all skip patterns for the given session.
func (h *ApprovalHook) ClearSessionSkip(sessionID string) {
	h.skipMutex.Lock()
	defer h.skipMutex.Unlock()
	delete(h.skipPatterns, sessionID)
}

// ClearAllSessionSkip removes all skip patterns for every session.
// Agent.Reset 调用此方法以避免上个会话的 skip 决策污染新会话。
func (h *ApprovalHook) ClearAllSessionSkip() {
	h.skipMutex.Lock()
	defer h.skipMutex.Unlock()
	h.skipPatterns = make(map[string]map[string]bool)
}

// normalizeForSkip 提取 binary + 第一个非 flag 子命令作为 skip key。
// 例如 "git push origin main" -> "git push"，"rm -rf /tmp" -> "rm"。
// 这比仅用 binary 名更精准，又比完整命令更通用。
func normalizeForSkip(cmd string) string {
	tokens := strings.Fields(cmd)
	if len(tokens) == 0 {
		return ""
	}
	// 跳过开头的 flag tokens（罕见但稳健）
	idx := 0
	for idx < len(tokens) && strings.HasPrefix(tokens[idx], "-") {
		idx++
	}
	if idx >= len(tokens) {
		return tokens[0]
	}
	binary := tokens[idx]
	if idx+1 < len(tokens) && !strings.HasPrefix(tokens[idx+1], "-") {
		return binary + " " + tokens[idx+1]
	}
	return binary
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

// getSessionID extracts session ID from context.
// 优先使用 tool 包导出的类型化键（tool.WithSessionID 设置），
// 回退到字符串键 "session_id"（兼容旧调用方），最终回退到 "cli"。
func getSessionID(ctx context.Context) string {
	if ctx == nil {
		return "cli"
	}
	// 优先使用 tool 包的类型化键（与 tool.WithSessionID 配对）
	if sid := tool.SessionIDFromContext(ctx); sid != "" {
		return sid
	}
	// 回退到字符串键以兼容未使用 tool.WithSessionID 的调用方
	if sid, ok := ctx.Value("session_id").(string); ok && sid != "" {
		return sid
	}
	return "cli"
}

// getWorkingDir extracts working_dir from context if available.
// 优先使用 tool.WorkDirFromContext（类型化键），回退到字符串键 "working_dir"。
func getWorkingDir(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if dir := tool.WorkDirFromContext(ctx); dir != "" {
		return dir
	}
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
