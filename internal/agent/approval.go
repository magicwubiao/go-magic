package agent

import (
	"context"
	"regexp"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
)

// CommandRiskAnalyzer analyzes command risk levels
type CommandRiskAnalyzer struct {
	dangerousPatterns []*regexp.Regexp
	mediumPatterns    []*regexp.Regexp
	safePatterns      []*regexp.Regexp
}

// NewCommandRiskAnalyzer creates a new command risk analyzer
func NewCommandRiskAnalyzer() *CommandRiskAnalyzer {
	return &CommandRiskAnalyzer{
		dangerousPatterns: []*regexp.Regexp{
			// System modification
			regexp.MustCompile(`(?i)(rm\s+-rf\s+/|dd\s+|mkfs|parted|fdisk)`),
			regexp.MustCompile(`(?i)(chmod\s+777|/etc/passwd|/etc/shadow)`),
			regexp.MustCompile(`(?i)(kill\s+-9\s+1|reboot|shutdown|init\s+0|halt)`),
			// Network modification
			regexp.MustCompile(`(?i)(iptables\s+-F|ufw\s+disable|firewall-cmd)`),
			regexp.MustCompile(`(?i)(route\s+del|ip\s+link\s+delete)`),
			// Package installation without confirmation
			regexp.MustCompile(`(?i)(apt-get\s+install\s+[^\s]+\s+--yes|yum\s+install\s+[^\s]+\s+-y|pacman\s+-S\s+[^\s]+)`),
			// Data destruction
			regexp.MustCompile(`(?i)(drop\s+table|drop\s+database|truncate\s+table|delete\s+from\s+.*\s+where)`),
			regexp.MustCompile(`(?i)(rm\s+.*-r.*\s+/home|rm\s+.*-r.*\s+/var|rm\s+.*-r.*\s+/etc)`),
		},
		mediumPatterns: []*regexp.Regexp{
			// System queries that might indicate reconnaissance
			regexp.MustCompile(`(?i)(uname\s+-a|whoami|id|hostname)`),
			regexp.MustCompile(`(?i)(cat\s+/etc/issue|cat\s+/etc/os-release)`),
			regexp.MustCompile(`(?i)(df\s+-h|mount|lsblk)`),
			regexp.MustCompile(`(?i)(ps\s+aux|top|htop)`),
			regexp.MustCompile(`(?i)(netstat|ss\s+-tulpn)`),
			regexp.MustCompile(`(?i)(curl\s+http|wget\s+http)`),
		},
		safePatterns: []*regexp.Regexp{
			// Read operations
			regexp.MustCompile(`(?i)^(ls|cd|pwd|cat|head|tail|grep|find|which|file)\s`),
			regexp.MustCompile(`(?i)^(echo|printf|wc|sort|uniq|awk|sed)\s`),
			// Safe git operations
			regexp.MustCompile(`(?i)^git\s+(status|log|diff|show|branch)\s`),
		},
	}
}

// Analyze analyzes a command and returns its risk level
func (a *CommandRiskAnalyzer) Analyze(command string) hooks.RiskLevel {
	command = strings.TrimSpace(command)

	// Check dangerous patterns first
	for _, pattern := range a.dangerousPatterns {
		if pattern.MatchString(command) {
			return hooks.RiskLevelDangerous
		}
	}

	// Check medium risk patterns
	for _, pattern := range a.mediumPatterns {
		if pattern.MatchString(command) {
			return hooks.RiskLevelMedium
		}
	}

	// Check safe patterns
	for _, pattern := range a.safePatterns {
		if pattern.MatchString(command) {
			return hooks.RiskLevelSafe
		}
	}

	// Default to medium for unknown commands
	return hooks.RiskLevelMedium
}

// GetRiskDescription returns a description of the risk level
func (a *CommandRiskAnalyzer) GetRiskDescription(level hooks.RiskLevel) string {
	switch level {
	case hooks.RiskLevelSafe:
		return "Safe - read-only operation"
	case hooks.RiskLevelMedium:
		return "Medium - may have side effects, review recommended"
	case hooks.RiskLevelDangerous:
		return "Dangerous - system modification detected, approval required"
	case hooks.RiskLevelCritical:
		return "Critical - destructive operation, immediate rejection"
	default:
		return "Unknown risk level"
	}
}

// ApprovalHook provides command approval functionality
type ApprovalHook struct {
	analyzer        *CommandRiskAnalyzer
	approvalStore   CommandApprovalStore
	autoApproveSafe bool
	defaultPolicy   ApprovalPolicy
}

// CommandApprovalStore stores approval decisions
type CommandApprovalStore interface {
	GetApproval(commandHash string) (Approved, bool)
	SetApproval(commandHash string, approved Approved)
}

// Approved represents an approval decision
type Approved struct {
	Approved  bool
	Reason    string
	Timestamp int64
}

// ApprovalPolicy defines default approval policy
type ApprovalPolicy int

const (
	PolicyAsk ApprovalPolicy = iota
	PolicyAutoApproveSafe
	PolicyAutoRejectDangerous
)

// NewApprovalHook creates a new approval hook
func NewApprovalHook() *ApprovalHook {
	return &ApprovalHook{
		analyzer:        NewCommandRiskAnalyzer(),
		autoApproveSafe: true,
		defaultPolicy:   PolicyAutoRejectDangerous,
	}
}

func (h *ApprovalHook) Name() string {
	return "approval"
}

// BeforeTool handles approval for tool execution
func (h *ApprovalHook) BeforeTool(ctx context.Context, call *hooks.ToolCallHookRequest) (*hooks.ToolCallHookRequest, hooks.HookDecision, error) {
	// Only check execute_command tool
	if call.ToolName != "execute_command" {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// Get command from args
	command, _ := call.ToolArgs["command"].(string)
	if command == "" {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// Analyze risk
	riskLevel := h.analyzer.Analyze(command)

	// Auto-approve safe commands if enabled
	if h.autoApproveSafe && riskLevel == hooks.RiskLevelSafe {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// Auto-reject dangerous commands based on policy
	if h.defaultPolicy == PolicyAutoRejectDangerous && riskLevel == hooks.RiskLevelDangerous {
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: "Auto-rejected dangerous command: " + h.analyzer.GetRiskDescription(riskLevel),
		}, nil
	}

	// In a real implementation, this would send to approval system
	// For now, reject medium and dangerous commands
	// approvalReq := &hooks.ToolApprovalRequest{
	// 	ToolName:  call.ToolName,
	// 	ToolArgs:  call.ToolArgs,
	// 	Command:   command,
	// 	RiskLevel: riskLevel,
	// }
	if riskLevel == hooks.RiskLevelMedium {
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: "Medium-risk command requires approval: " + h.analyzer.GetRiskDescription(riskLevel),
		}, nil
	}

	if riskLevel == hooks.RiskLevelDangerous {
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: "Dangerous command rejected: " + h.analyzer.GetRiskDescription(riskLevel),
		}, nil
	}

	return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// AfterTool passes through the result
func (h *ApprovalHook) AfterTool(ctx context.Context, result *hooks.ToolResultHookResponse) (*hooks.ToolResultHookResponse, hooks.HookDecision, error) {
	return result, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// BeforeLLM passes through
func (h *ApprovalHook) BeforeLLM(ctx context.Context, req *hooks.LLMHookRequest) (*hooks.LLMHookRequest, hooks.HookDecision, error) {
	return req, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// AfterLLM passes through
func (h *ApprovalHook) AfterLLM(ctx context.Context, resp *hooks.LLMHookResponse) (*hooks.LLMHookResponse, hooks.HookDecision, error) {
	return resp, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
}

// ApproveTool handles approval request (for approval system integration)
func (h *ApprovalHook) ApproveTool(ctx context.Context, req *hooks.ToolApprovalRequest) (hooks.ApprovalDecision, error) {
	command, _ := req.ToolArgs["command"].(string)
	riskLevel := h.analyzer.Analyze(command)

	return hooks.ApprovalDecision{
		Approved: riskLevel == hooks.RiskLevelSafe,
		Reason:   h.analyzer.GetRiskDescription(riskLevel),
	}, nil
}
