package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
)

// ApprovalHook provides command approval functionality using the smart approval system
type ApprovalHook struct {
	manager *approval.Manager
}

// NewApprovalHook creates a new approval hook with smart approval
func NewApprovalHook() *ApprovalHook {
	mgr, err := approval.NewManager(nil) // uses DefaultConfig (strategy=auto)
	if err != nil {
		// Fallback: if manager can't be created, create one with safe defaults
		mgr, _ = approval.NewManager(approval.DefaultConfig())
	}
	return &ApprovalHook{
		manager: mgr,
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

	// Build approval request
	req := &approval.ApprovalRequest{
		Command:   command,
		SessionID: getSessionID(ctx),
	}

	// Request approval from manager
	result, err := h.manager.RequestApproval(req)
	if err != nil {
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: fmt.Sprintf("Approval error: %v", err),
		}, nil
	}

	// If approved, continue
	if result.Approved {
		return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
	}

	// If needs user confirmation (AskUser=true), prompt in CLI
	if result.AskUser {
		approved := h.promptUserConfirmation(command, result.Reason)
		if approved {
			// Record approval for learning
			h.manager.Approve(req)
			return call, hooks.HookDecision{Action: hooks.HookActionContinue}, nil
		}
		// Record denial for learning
		h.manager.Deny(req)
		return call, hooks.HookDecision{
			Action: hooks.HookActionReject,
			Reason: fmt.Sprintf("User rejected command: %s", result.Reason),
		}, nil
	}

	// Otherwise reject
	return call, hooks.HookDecision{
		Action: hooks.HookActionReject,
		Reason: result.Reason,
	}, nil
}

// promptUserConfirmation asks the user for confirmation in CLI
func (h *ApprovalHook) promptUserConfirmation(command, reason string) bool {
	fmt.Printf("\n  ⚠ Command Approval Required\n")
	fmt.Printf("  Command: %s\n", command)
	fmt.Printf("  Reason: %s\n", reason)
	fmt.Printf("  Allow? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	return input == "y" || input == "yes"
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

// ApproveTool handles approval request (for gateway integration)
func (h *ApprovalHook) ApproveTool(ctx context.Context, req *hooks.ToolApprovalRequest) (hooks.ApprovalDecision, error) {
	command, _ := req.ToolArgs["command"].(string)
	approvalReq := &approval.ApprovalRequest{
		Command:   command,
		SessionID: getSessionID(ctx),
	}
	result, err := h.manager.RequestApproval(approvalReq)
	if err != nil {
		return hooks.ApprovalDecision{Approved: false, Reason: err.Error()}, err
	}
	return hooks.ApprovalDecision{
		Approved: result.Approved,
		Reason:   result.Reason,
	}, nil
}

// getSessionID extracts session ID from context
func getSessionID(ctx context.Context) string {
	if id, ok := ctx.Value("session_id").(string); ok {
		return id
	}
	return "cli"
}
