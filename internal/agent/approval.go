package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// approvalAware 由"自己知道要不要人工确认"的工具实现。
//
// 目前只有 MCP 连接器的工具（mcp.MCPTool）实现：它按 tool annotations 或
// server 级 require_approval 给出结论。用接口而不是开关表，是为了让新增
// 工具自己声明策略，不必在每个调用点维护白名单。
type approvalAware interface {
	RequiresApproval() bool
	ApprovalSummary() string
}

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
	manager *approval.Manager
	webMode bool
	// nonInteractive 标记"这个会话没有任何向人弹出审批提示的通道"（消息网关、
	// 后台守护进程）。置位后即便 stdin 恰好是个终端也不走 CLI 交互提示——
	// 守护进程的控制台前没有人会来点"同意"，读 stdin 只会把整轮对话阻塞到超时。
	nonInteractive bool
	// channel 标明本钩子所属的通道（web / cli / gateway / bot / unattended），
	// 随每个审批请求一起交给 Manager，用于按通道解析策略。
	channel      approval.Channel
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
		channel:      approval.ChannelCLI,
		cliTimeout:   defaultApprovalTimeout,
		skipPatterns: make(map[string]map[string]bool),
	}
}

// NewApprovalHookWithManager creates a new approval hook using an existing manager.
func NewApprovalHookWithManager(mgr *approval.Manager) *ApprovalHook {
	if mgr == nil {
		return NewApprovalHook()
	}
	h := &ApprovalHook{
		manager:      mgr,
		channel:      approval.ChannelCLI,
		cliTimeout:   defaultApprovalTimeout,
		skipPatterns: make(map[string]map[string]bool),
	}
	h.applyManagerConfig()
	return h
}

// applyManagerConfig 把 manager 里配置的审批超时接到 CLI 提示上。
//
// 此前 cliTimeout 是硬编码 30s 常量，配置里的 approval_timeout（默认 60）在
// CLI 路径上完全不生效——用户在设置页调超时，改的只是 Web pending 审批，
// CLI 提示仍然 30 秒就超时。现在两者同源。
func (h *ApprovalHook) applyManagerConfig() {
	if h.manager == nil {
		return
	}
	if secs := h.manager.GetConfig().ApprovalTimeout; secs > 0 {
		h.cliTimeout = time.Duration(secs) * time.Second
	}
}

// SetChannel 声明本钩子所属的通道，随每个审批请求传给 Manager 用于按通道
// 解析策略（见 approval.ApprovalConfig.ChannelOverrides）。
// 无人值守通道未显式配置策略时兜底 auto_narrow。
func (h *ApprovalHook) SetChannel(ch approval.Channel) {
	h.channel = ch
}

// Channel 返回本钩子声明的通道（未声明时为零值）。
func (h *ApprovalHook) Channel() approval.Channel { return h.channel }

// SetCLITimeout 覆盖 CLI 提示超时（0 或负值忽略，保持配置值）。
func (h *ApprovalHook) SetCLITimeout(d time.Duration) {
	if d > 0 {
		h.cliTimeout = d
	}
}

// CLITimeout 返回当前 CLI 提示超时，供测试与诊断使用。
func (h *ApprovalHook) CLITimeout() time.Duration { return h.cliTimeout }

// Name returns the hook name.
func (h *ApprovalHook) Name() string { return "approval" }

// SetWebMode enables or disables Web-based approval mode.
// When enabled, user-facing confirmations are routed through the Web callback
// system (PendingWebApproval) instead of CLI prompts.
func (h *ApprovalHook) SetWebMode(enabled bool) {
	h.webMode = enabled
}

// SetNonInteractive marks this hook as having no human-facing prompt channel:
// the agent runs inside a daemon (message gateway, background worker) where
// nobody is watching the console. Content tools that need confirmation then
// fail closed immediately with an actionable reason, instead of blocking the
// turn on a CLI prompt that will only time out (default 30s) and poison the
// denied-pattern learning with a decision no user ever made.
//
// Callers that DO have a prompt channel (TUI via SetPromptFunc, web via
// SetWebMode) must not set this.
func (h *ApprovalHook) SetNonInteractive(enabled bool) {
	h.nonInteractive = enabled
}

// IsNonInteractive reports whether the hook was marked as having no
// human-facing prompt channel (see SetNonInteractive).
func (h *ApprovalHook) IsNonInteractive() bool { return h.nonInteractive }

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
	case "computer_use":
		// 桌面控制：能点任意位置、向任意窗口打字（发消息、点支付、确认弹窗）。
		// 这是整个工具集里权限最高的一项，必须逐次确认。
		command = describeComputerUseCall(call.ToolArgs)
		toolDesc = "desktop control"
		approvalCtx = buildComputerUseContext(call.ToolArgs)
	default:
		// 连接器工具（mcp_*）：由工具自己声明是否需要确认。以前的 default
		// 分支直接放行，意味着一个暴露 delete_file 的 MCP server 可以被模型
		// 直接调用，而同样的操作走 bash 一定弹窗——这是明确的越权缺口。
		if !call.RequiresApproval {
			return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
		}
		command = call.ApprovalSummary
		if command == "" {
			command = call.ToolName
		}
		toolDesc = "connector tool call"
		approvalCtx = buildConnectorCallContext(call)
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
		Channel:    h.channel,
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

	// 工具层的命令清单（tool.allowedCommands）默认是软策略：未列出的命令不会
	// 在工具层被硬拦，而是落到这里判定。为了让"软"不等于"静默放行"，这里把
	// "不在标准工具链清单"的命令从自动放行升级为询问——低风险命令（如
	// `foo_bar_baz --flag`）原本会被 smartApprove 静默放行，升级后用户能看到它。
	//
	// 只在"仅因低风险被放行"时升级。其它放行（审批白名单 / 只读命令 / 受信
	// pattern / 学习结果）都代表用户或策略已经明确认可过这条命令，不应被推翻；
	// strategy=auto 更是用户在配置里明确表态"都别问"。
	if call.ToolName == "execute_command" && result.Approved &&
		result.Strategy != approval.StrategyAutoApprove && result.Reason == lowRiskAutoApproveReason {
		if base := tool.BaseCommand(command); base != "" && !tool.IsStandardCommand(base) {
			result = &approval.ApprovalResult{
				Approved:  false,
				AskUser:   true,
				Strategy:  result.Strategy,
				RiskLevel: result.RiskLevel,
				Reason:    fmt.Sprintf("Command %q is not in the standard toolchain list — confirm to run it", base),
			}
			req.Reason = result.Reason
		}
	}

	// enable_cli_confirm：CLI 下"每条命令都要确认"的显式开关。
	//
	// 此前这个配置项只被 Manager.CLIConfirm 读取，而那个方法零调用方 ——
	// 设置页上的开关是个假开关。现在的语义（与 UI 提示"在 CLI 中执行命令前
	// 要求用户确认"一致）：置位后，即使策略判定为自动放行（smart 低风险 /
	// auto），CLI 通道也要先问一次。白名单 / 受信 pattern / 只读命令不升级
	// ——那是用户已经明确认可过的路径。
	if h.cliConfirmEscalates(call.ToolName, result) {
		result = &approval.ApprovalResult{
			Approved:  false,
			AskUser:   true,
			Strategy:  result.Strategy,
			RiskLevel: result.RiskLevel,
			Reason:    "CLI confirmation is enabled (approval.enable_cli_confirm) — confirm to run this tool call",
		}
		if req.Reason == "" {
			req.Reason = result.Reason
		}
	}

	// If already approved by policy, notify and continue.
	// 但对 execute_code / write_file / file_edit 这类工具，其合成命令字符串
	// （如 "execute_code python (50 bytes)"）不包含真实代码/内容，assessRisk
	// 只能给出 Low 风险从而被 smartApprove 自动放行。这里对"仅因 Low 风险被放行"
	// 的内容类工具强制改为询问用户，避免任意代码/文件写入被静默执行。
	// 白名单/受信 pattern/只读命令 的放行 reason 不同，不受影响。
	if result.Approved {
		// computer_use 的合成命令只含"动作 + 坐标"，assessRisk 看不到它真正
		// 要做什么，只会判低风险并静默放行。桌面控制的爆炸半径远大于任何
		// 单条命令（可以点掉确认框、往聊天窗口打字），所以只要不是用户显式
		// 授予过（会话跳过 / 白名单 / 已学习 pattern），就必须先问。
		// 注意：会话级"全部允许"在 isSessionSkipped 那一步就已经返回了，
		// 不会落到这里，因此不至于每次点击都弹窗。
		if call.ToolName == "computer_use" && result.Reason == lowRiskAutoApproveReason {
			result.Approved = false
			result.AskUser = true
			result.RiskLevel = approval.RiskHigh
			result.Reason = "Desktop control requires confirmation — this can click and type into any application"
			if req.Reason == "" {
				req.Reason = result.Reason
			}
		} else if call.RequiresApproval && result.Reason == lowRiskAutoApproveReason {
			// 连接器工具的合成命令（"MCP github → delete_repo"）不含参数里
			// 的破坏性语义，assessRisk 只会判低风险并静默放行。工具自己已经
			// 声明要确认了（destructiveHint / require_approval），这里不能
			// 被自动放行推翻。
			result.Approved = false
			result.AskUser = true
			result.RiskLevel = approval.RiskHigh
			result.Reason = fmt.Sprintf("%s requires confirmation — this connector tool can change external state", command)
			if req.Reason == "" {
				req.Reason = result.Reason
			}
		} else if isContentTool(call.ToolName) && result.Reason == lowRiskAutoApproveReason {
			// C2：内容工具"范围放行"。write_file/file_edit 的目标路径位于会话
			// 工作目录内时，自动放行并写入审计历史；工作目录外仍要求用户确认。
			// execute_code 无路径可判定范围，保持强制确认。
			if h.isPathWithinWorkdir(call.ToolName, call.ToolArgs, workingDir) {
				scoped := &approval.ApprovalResult{
					Approved:  true,
					Trusted:   true,
					Strategy:  result.Strategy,
					Reason:    "Path within working directory (scoped auto-approval)",
					RiskLevel: result.RiskLevel,
				}
				h.manager.RecordResult(req, scoped)
				h.manager.NotifyApproval(scoped, req)
				return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
			}
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
		// denyReason carries the truthful denial attribution into the tool
		// result. Previously every deny here was reported as "User rejected
		// command: ..." — including the non-interactive fail-closed path where
		// NO user was ever asked — which made the model believe the user had
		// actively refused ("用户拒绝了写入操作") instead of fixing the real
		// cause (missing approval wiring / out-of-scope path).
		denyReason := result.Reason

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
				// C1：审批超时按配置的超时策略处理（ResolveTimeout 内部根据
				// timeout_strategy 决定放行/拒绝，写入审计历史并通知回调）。
				timeoutResult := h.manager.ResolveTimeout(req)
				approved = timeoutResult.Approved
				if !approved {
					denyReason = "Approval timed out and was auto-resolved to deny: " + timeoutResult.Reason
				}
			} else if approved {
				h.manager.Approve(req)
			} else {
				h.manager.Deny(req)
				denyReason = "User rejected via web approval: " + result.Reason
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
				denyReason = "User rejected via interactive prompt: " + result.Reason
			}
		} else if h.nonInteractive || !isStdinTerminal() {
			// 非交互模式且没有自定义 prompt：fail-closed（默认拒绝）。
			// 之前的行为是自动批准，这会让 Critical 风险命令在 CI/管道里
			// 静默执行。要求调用方显式配置 web 审批或注入 promptFunc。
			//
			// h.nonInteractive 覆盖"stdin 恰好是终端、但其实是守护进程"的情形
			// （gateway 常从终端启动）：那种情况下 CLI 提示只会阻塞到超时，
			// 且没有人会真的看到它。
			//
			// 血债（2026-09-23 bot 模式"写入被拒绝"）：这里原来只有一条
			// fmt.Printf 到 stdout（服务进程里无人看见、也不进日志文件），
			// 且最终拒绝理由沿用 "User rejected command: ..." —— 用户根本
			// 没被询问过，模型却被误导去向用户道歉并反复澄清。现在：
			// ① 拒绝进日志（log.Warnf），可被 grep 追溯；
			// ② 拒绝理由对模型说真话并给出可行动的修复路径。
			log.Warnf("[Approval] Non-interactive session denied %q (channel=%q, no web approval/promptFunc configured)", command, h.channel)
			approved = false
			denyReason = fmt.Sprintf("Denied automatically: this session cannot show an approval prompt (channel=%q, unattended/background mode). File writes inside the session working directory are auto-approved. To let this channel run such operations, set approval.channel_overrides[%q] to \"auto_narrow\" (low/medium risk allowed, high risk still denied) or \"auto\" in config.json; to approve interactively, run the task via the web UI or CLI.", h.channel, h.channel)
			h.manager.Deny(req)
			h.manager.NotifyApproval(&approval.ApprovalResult{
				Approved: false,
				Strategy: result.Strategy,
				Reason:   "Non-interactive mode denied (fail-closed)",
			}, req)
		} else {
			// CLI interactive prompt with timeout.
			// 每次提示前重新同步一次超时配置：web 设置页热加载改 approval_timeout
			// 时，已存在的 hook 也应立刻用新值，而不是继续用创建时的旧值。
			h.applyManagerConfig()
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
				denyReason = "User rejected via CLI prompt: " + result.Reason
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
				// C1：审批超时按配置的超时策略处理（ResolveTimeout 根据
				// timeout_strategy 决定放行/拒绝，写入审计历史并通知回调）。
				// 不调用 Deny，避免污染 denied pattern 计数。
				timeoutResult := h.manager.ResolveTimeout(req)
				approved = timeoutResult.Approved
				if !approved {
					denyReason = "Approval timed out and was auto-resolved to deny: " + timeoutResult.Reason
				}
			}
		}

		if approved {
			return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
		}
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: fmt.Sprintf("Tool call denied: %s", denyReason),
		}, nil
	}

	// Rejected by policy, notify and return.
	h.manager.NotifyApproval(result, req)
	return call, hooks.HookDecision{
		Action: hooks.HookActionReject,
		Reason: result.Reason,
	}, nil
}

// lowRiskAutoApproveReason 是 smartApprove 对"仅因风险低"而自动放行时给出的
// reason（见 internal/approval 的 smartApprove）。内容类工具与命令清单升级都
// 依赖它区分"自动放行"与"用户显式批准"。
const lowRiskAutoApproveReason = "Low risk command"

// cliConfirmEscalates 判断是否应因 approval.enable_cli_confirm 把一次自动放行
// 升级为"询问用户"。
//
// 只在真正能弹提示的交互式 CLI 通道上生效：Web 有自己的卡片 UI（升级会让
// 每条只读命令都弹卡），无人值守通道没有可问的人（升级只会退化成 fail-closed）。
// 白名单 / 受信 pattern / 只读命令不升级——那是用户已经明确认可过的路径。
func (h *ApprovalHook) cliConfirmEscalates(toolName string, result *approval.ApprovalResult) bool {
	if result == nil || !result.Approved {
		return false
	}
	if h.webMode || h.nonInteractive {
		return false
	}
	if h.channel != "" && h.channel != approval.ChannelCLI {
		return false
	}
	// 必须真的能问到人：TUI 走注入的 promptFunc，普通 CLI 走 stdin。
	// 两者都没有时（纯管道/CI）升级只会退化成 fail-closed，不升级。
	if h.promptFunc == nil && !isStdinTerminal() {
		return false
	}
	if h.manager == nil || !h.manager.GetConfig().EnableCLIConfirm {
		return false
	}
	switch result.Strategy {
	case approval.StrategyWhitelist:
		return false
	}
	switch result.Reason {
	case "Trusted command pattern", "Command is whitelisted", "Read-only command":
		return false
	}
	// 只读命令的放行理由见 smartApprove；用前缀匹配兜住措辞微调。
	if strings.HasPrefix(result.Reason, "Read-only") {
		return false
	}
	return true
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

// isPathWithinWorkdir 判断内容工具（write_file/file_edit）的目标路径是否位于
// 会话工作目录内（C2 范围放行判定）。路径解析方式与文件工具一致：
// 相对路径基于 workdir 解析，绝对路径按原样。
func (h *ApprovalHook) isPathWithinWorkdir(toolName string, args map[string]interface{}, workdir string) bool {
	if workdir == "" {
		return false
	}
	var path string
	switch toolName {
	case "write_file", "file_edit":
		path, _ = args["path"].(string)
	default:
		return false
	}
	if path == "" {
		return false
	}

	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return false
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(absWorkdir, path)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absWorkdir, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// describeComputerUseCall 构造 computer_use 的审批摘要。
//
// 审批层只拿到这段字符串，而 computer_use 的危险恰好藏在参数里：点哪里、
// 往哪个输入框打字。这里把动作和关键参数都摊开，用户在卡片上才能判断
// （尤其是 type —— 可能是在给陌生人发消息或往对话框里粘贴密钥）。
func describeComputerUseCall(args map[string]interface{}) string {
	action := strings.ToLower(strings.TrimSpace(strArg(args, "action")))
	intArg := func(key string) int {
		switch v := args[key].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
		return 0
	}

	switch action {
	case "screenshot":
		return "computer_use screenshot"
	case "click", "move", "scroll":
		detail := ""
		if action == "click" {
			if btn := strArg(args, "button"); btn != "" && btn != "left" {
				detail = " " + btn
			}
			if n := intArg("count"); n > 1 {
				detail += fmt.Sprintf(" x%d", n)
			}
		}
		if action == "scroll" {
			detail = fmt.Sprintf(" by %d", intArg("scroll_y"))
		}
		return fmt.Sprintf("computer_use %s (%d,%d)%s", action, intArg("x"), intArg("y"), detail)
	case "drag":
		return fmt.Sprintf("computer_use drag (%d,%d)->(%d,%d)",
			intArg("x"), intArg("y"), intArg("to_x"), intArg("to_y"))
	case "type":
		return fmt.Sprintf("computer_use type %q", truncateCommand(strArg(args, "text"), 120))
	case "key":
		return "computer_use key " + strArg(args, "key")
	}
	return "computer_use " + action
}

// buildComputerUseContext 给审批卡片补充说明：为什么这一步值得警惕。
func buildComputerUseContext(args map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("This tool drives the real mouse and keyboard of this machine.\n")
	b.WriteString("It can click any button and type into any window — including sending\n")
	b.WriteString("messages, confirming dialogs, or pasting secrets. Approve only if this\n")
	b.WriteString("matches what you actually asked for.")
	if text := strArg(args, "text"); text != "" {
		fmt.Fprintf(&b, "\n\n--- text to type ---\n%s", truncateCommand(text, 600))
	}
	if key := strArg(args, "key"); key != "" {
		fmt.Fprintf(&b, "\n\n--- key ---\n%s", key)
	}
	return b.String()
}

// strArg 安全取字符串参数。
func strArg(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

// buildConnectorCallContext 构造 MCP 连接器工具的审批上下文。
//
// 审批卡片必须能看到"这个工具带什么参数"才能判断：同样是 send_message，
// 发给自己和发到客户群是两件事。参数按 key 排序输出，保证同一个调用每次
// 展示一致（map 遍历顺序随机会让卡片抖动）。
func buildConnectorCallContext(call *hooks.ToolCallHookRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Connector tool: %s\n", call.ToolName)
	if call.ApprovalSummary != "" && call.ApprovalSummary != call.ToolName {
		fmt.Fprintf(&b, "Action: %s\n", call.ApprovalSummary)
	}
	b.WriteString("This call reaches an external system and may change data there.\n")
	b.WriteString("Approve only if this matches what you actually asked for.")

	if len(call.ToolArgs) == 0 {
		return b.String()
	}

	keys := make([]string, 0, len(call.ToolArgs))
	for k := range call.ToolArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("\n\n--- arguments ---")
	for _, k := range keys {
		v := call.ToolArgs[k]
		var rendered string
		if s, ok := v.(string); ok {
			rendered = truncateCommand(s, 400)
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				rendered = fmt.Sprintf("%v", v)
			} else {
				rendered = truncateCommand(string(raw), 400)
			}
		}
		fmt.Fprintf(&b, "\n%s: %s", k, rendered)
	}
	return b.String()
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
//
// 取值优先级：沙箱根（tool.WithWorkDirScope）> 生效工作目录（tool.WithWorkDir）>
// 字符串键 "working_dir"。
//
// 先 scope 后 workdir 是刻意的：网关把每轮的生效目录收窄到
// <gateway 沙箱>/<用户>（相对路径的落点因此按用户隔离），但范围放行必须以整个
// gateway 沙箱为界——模型会沿用上一轮记下的绝对路径，收窄边界会把它们判成"越界"
// → 非交互网关 fail-closed → 重试烧满 maxTurns。
func getWorkingDir(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if scope := tool.WorkDirScopeFromContext(ctx); scope != "" {
		return scope
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

// stdinIsTerminal is a package-level seam so tests can simulate interactive
// and non-interactive stdin without touching the real terminal.
var stdinIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// isStdinTerminal returns true if os.Stdin is a real terminal (interactive).
func isStdinTerminal() bool {
	return stdinIsTerminal()
}
