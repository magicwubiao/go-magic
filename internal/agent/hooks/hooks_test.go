package hooks

import (
	"context"
	"testing"
)

// multiHook implements LLMHook, ToolHook and ApprovalHook simultaneously
type multiHook struct{}

func (m *multiHook) Name() string { return "multi" }

func (m *multiHook) BeforeLLM(ctx context.Context, req *LLMHookRequest) (*LLMHookRequest, HookDecision, error) {
	return req, HookDecision{Action: HookActionContinue}, nil
}
func (m *multiHook) AfterLLM(ctx context.Context, resp *LLMHookResponse) (*LLMHookResponse, HookDecision, error) {
	return resp, HookDecision{Action: HookActionContinue}, nil
}
func (m *multiHook) BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error) {
	return call, HookDecision{Action: HookActionContinue}, nil
}
func (m *multiHook) AfterTool(ctx context.Context, result *ToolResultHookResponse) (*ToolResultHookResponse, HookDecision, error) {
	return result, HookDecision{Action: HookActionContinue}, nil
}
func (m *multiHook) ApproveTool(ctx context.Context, req *ToolApprovalRequest) (ApprovalDecision, error) {
	return ApprovalDecision{Approved: true}, nil
}

func TestRegisterMultiInterfaceHook(t *testing.T) {
	hm := NewHookManager()
	mh := &multiHook{}

	err := hm.Register(HookRegistration{Name: "multi", Source: HookSourceBuiltIn, Hook: mh})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if len(hm.llmHooks) != 1 {
		t.Errorf("Expected 1 LLMHook, got %d", len(hm.llmHooks))
	}
	if len(hm.toolHooks) != 1 {
		t.Errorf("Expected 1 ToolHook, got %d", len(hm.toolHooks))
	}
	if len(hm.approvalHooks) != 1 {
		t.Errorf("Expected 1 ApprovalHook, got %d", len(hm.approvalHooks))
	}
}

func TestBeforeToolCallsToolHooks(t *testing.T) {
	hm := NewHookManager()
	mh := &multiHook{}
	hm.Register(HookRegistration{Name: "multi", Source: HookSourceBuiltIn, Hook: mh})

	ctx := context.Background()
	req := &ToolCallHookRequest{ToolName: "execute_command", ToolArgs: map[string]interface{}{"command": "ls"}}

	_, decision, err := hm.BeforeTool(ctx, req)
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("Expected Continue, got %v", decision.Action)
	}
}

func TestApproveToolCallsApprovalHooks(t *testing.T) {
	hm := NewHookManager()
	mh := &multiHook{}
	hm.Register(HookRegistration{Name: "multi", Source: HookSourceBuiltIn, Hook: mh})

	ctx := context.Background()
	req := &ToolApprovalRequest{ToolName: "execute_command", ToolArgs: map[string]interface{}{"command": "ls"}}

	decision, err := hm.ApproveTool(ctx, req)
	if err != nil {
		t.Fatalf("ApproveTool error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected Approved=true")
	}
}
