package hooks

import (
	"context"
	"fmt"

	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Hook vs EventBus Design Philosophy
// =====================================
//
// This file defines two complementary event systems with distinct responsibilities:
//
// 1. **Hook System** (this file)
//    - Purpose: Intercept and modify data flows (before/after)
//    - Use Case: Transform data, approve/reject actions, inject logic
//    - Characteristics:
//      - Can modify request/response data (returns modified versions)
//      - Supports decisions: Continue, Stop, Reject, Modify
//      - Execution order matters (first hook that rejects stops the chain)
//      - Blocking - hooks run synchronously in the execution path
//
// 2. **EventBus System** (internal/bus/eventbus.go)
//    - Purpose: Observe and monitor events (publish/subscribe)
//    - Use Case: Logging, metrics, monitoring, debugging
//    - Characteristics:
//      - Read-only observation (cannot modify data)
//      - Fire-and-forget, non-blocking
//      - Multiple subscribers can process the same event
//      - Useful for cross-cutting concerns like logging
//
// Integration:
// - EventHook automatically bridges to EventBus for seamless monitoring
// - All hooks can optionally publish to EventBus for observability
// - Both systems are orthogonal and can be used together

// HookAction defines what action to take after a hook runs
type HookAction string

const (
	HookActionContinue HookAction = "continue"
	HookActionStop     HookAction = "stop"
	HookActionReject   HookAction = "reject"
	HookActionModify   HookAction = "modify"
)

// HookDecision represents the decision made by a hook
type HookDecision struct {
	Action HookAction
	Reason string
}

// LLMHookRequest contains data for LLM-related hooks
type LLMHookRequest struct {
	Provider string
	Model    string
	Messages []provider.Message
	Tools    []map[string]interface{}
}

// LLMHookResponse contains the LLM response for hooks
type LLMHookResponse struct {
	Content   string
	ToolCalls []types.ToolCall
	Raw       interface{}
}

// ToolCallHookRequest contains data for tool call hooks
type ToolCallHookRequest struct {
	ToolName string
	ToolArgs map[string]interface{}
	Result   interface{}
	Error    error
}

// ToolResultHookResponse contains the result of tool execution
type ToolResultHookResponse struct {
	ToolName    string
	ToolArgs    map[string]interface{}
	Result      interface{}
	Error       error
	ExecutionMs int64
}

// ToolApprovalRequest contains data for tool approval hooks
type ToolApprovalRequest struct {
	ToolName  string
	ToolArgs  map[string]interface{}
	Command   string
	RiskLevel RiskLevel
	SessionID string
}

// ApprovalDecision represents the approval decision
type ApprovalDecision struct {
	Approved  bool
	Reason    string
	ExpiresAt *int64
}

// RiskLevel defines the risk level of a command
type RiskLevel int

const (
	RiskLevelSafe RiskLevel = iota
	RiskLevelMedium
	RiskLevelDangerous
	RiskLevelCritical
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLevelSafe:
		return "safe"
	case RiskLevelMedium:
		return "medium"
	case RiskLevelDangerous:
		return "dangerous"
	case RiskLevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Hook is the interface for all hooks
type Hook interface {
	Name() string
}

// LLMHook is called before and after LLM calls
type LLMHook interface {
	Hook
	BeforeLLM(ctx context.Context, req *LLMHookRequest) (*LLMHookRequest, HookDecision, error)
	AfterLLM(ctx context.Context, resp *LLMHookResponse) (*LLMHookResponse, HookDecision, error)
}

// ToolHook is called before and after tool execution
type ToolHook interface {
	Hook
	BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error)
	AfterTool(ctx context.Context, result *ToolResultHookResponse) (*ToolResultHookResponse, HookDecision, error)
}

// ApprovalHook is called to approve or reject tool execution
type ApprovalHook interface {
	Hook
	ApproveTool(ctx context.Context, req *ToolApprovalRequest) (ApprovalDecision, error)
}

// EventHook observes events without modifying them
// EventHooks are automatically bridged to the EventBus for monitoring purposes
type EventHook interface {
	Hook
	OnEvent(ctx context.Context, evt bus.Event) error
}

// HookRegistration holds hook configuration
type HookRegistration struct {
	Name   string
	Source HookSource
	Hook   interface{} // LLMHook, ToolHook, ApprovalHook, or EventHook
}

// HookSource indicates where the hook comes from
type HookSource string

const (
	HookSourceBuiltIn HookSource = "built_in"
	HookSourceConfig  HookSource = "config"
	HookSourceProcess HookSource = "process"
)

// HookManager manages all hooks
type HookManager struct {
	llmHooks      []LLMHook
	toolHooks     []ToolHook
	approvalHooks []ApprovalHook
	eventHooks    []EventHook
	eventBus      *bus.EventBus
}

// NewHookManager creates a new hook manager
func NewHookManager() *HookManager {
	return &HookManager{
		llmHooks:      make([]LLMHook, 0),
		toolHooks:     make([]ToolHook, 0),
		approvalHooks: make([]ApprovalHook, 0),
		eventHooks:    make([]EventHook, 0),
	}
}

// SetEventBus sets the event bus for the hook manager
// This enables automatic bridging of EventHooks to the EventBus
func NewHookManagerWithBus(eventBus *bus.EventBus) *HookManager {
	hm := NewHookManager()
	hm.eventBus = eventBus
	return hm
}

// Register registers a hook into all matching hook lists.
// A single hook value may implement multiple interfaces (e.g. both LLMHook and ToolHook);
// each matching list gets its own registration so that no interface is missed by a
// type-switch that only takes the first match.
func (m *HookManager) Register(reg HookRegistration) error {
	h := reg.Hook
	registered := false

	if v, ok := h.(LLMHook); ok {
		m.llmHooks = append(m.llmHooks, v)
		registered = true
	}
	if v, ok := h.(ToolHook); ok {
		m.toolHooks = append(m.toolHooks, v)
		registered = true
	}
	if v, ok := h.(ApprovalHook); ok {
		m.approvalHooks = append(m.approvalHooks, v)
		registered = true
	}
	if v, ok := h.(EventHook); ok {
		m.eventHooks = append(m.eventHooks, v)
		registered = true
		// Auto-bridge to EventBus if available
		if m.eventBus != nil {
			_ = m.eventBus.Subscribe(16)
		}
	}

	if !registered {
		return fmt.Errorf("unsupported hook type: %T", h)
	}
	return nil
}

// Unregister removes all hooks with the given name from every hook list.
// Returns true if at least one hook was removed. This is primarily used to
// replace a built-in hook with an externally injected variant (e.g. swapping
// the default approval hook for one configured with a server-side manager),
// avoiding duplicate execution of same-purpose hooks.
func (m *HookManager) Unregister(name string) bool {
	removed := false
	m.llmHooks, removed = filterHooksByName(m.llmHooks, name, removed)
	m.toolHooks, removed = filterHooksByName(m.toolHooks, name, removed)
	m.approvalHooks, removed = filterHooksByName(m.approvalHooks, name, removed)
	m.eventHooks, removed = filterHooksByName(m.eventHooks, name, removed)
	return removed
}

// filterHooksByName drops every hook whose Name() matches the supplied name.
// The removed flag is OR-ed into the returned value so callers can chain calls
// across multiple hook slices.
func filterHooksByName[T Hook](hooks []T, name string, removed bool) ([]T, bool) {
	if len(hooks) == 0 {
		return hooks, removed
	}
	result := make([]T, 0, len(hooks))
	for _, h := range hooks {
		if h.Name() == name {
			removed = true
			continue
		}
		result = append(result, h)
	}
	return result, removed
}

// BeforeLLM calls all LLM hooks before an LLM call
func (m *HookManager) BeforeLLM(ctx context.Context, req *LLMHookRequest) (*LLMHookRequest, HookDecision, error) {
	currentReq := req
	for _, h := range m.llmHooks {
		newReq, decision, err := h.BeforeLLM(ctx, currentReq)
		if err != nil {
			return nil, HookDecision{Action: HookActionStop, Reason: err.Error()}, err
		}
		if decision.Action != HookActionContinue {
			return newReq, decision, nil
		}
		if newReq != nil {
			currentReq = newReq
		}
	}
	return currentReq, HookDecision{Action: HookActionContinue}, nil
}

// AfterLLM calls all LLM hooks after an LLM call
func (m *HookManager) AfterLLM(ctx context.Context, resp *LLMHookResponse) (*LLMHookResponse, HookDecision, error) {
	currentResp := resp
	for _, h := range m.llmHooks {
		newResp, decision, err := h.AfterLLM(ctx, currentResp)
		if err != nil {
			return nil, HookDecision{Action: HookActionStop, Reason: err.Error()}, err
		}
		if decision.Action != HookActionContinue {
			return newResp, decision, nil
		}
		if newResp != nil {
			currentResp = newResp
		}
	}
	return currentResp, HookDecision{Action: HookActionContinue}, nil
}

// BeforeTool calls all tool hooks before tool execution
func (m *HookManager) BeforeTool(ctx context.Context, call *ToolCallHookRequest) (*ToolCallHookRequest, HookDecision, error) {
	currentCall := call
	for _, h := range m.toolHooks {
		newCall, decision, err := h.BeforeTool(ctx, currentCall)
		if err != nil {
			return nil, HookDecision{Action: HookActionStop, Reason: err.Error()}, err
		}
		if decision.Action != HookActionContinue {
			return newCall, decision, nil
		}
		if newCall != nil {
			currentCall = newCall
		}
	}
	return currentCall, HookDecision{Action: HookActionContinue}, nil
}

// AfterTool calls all tool hooks after tool execution
func (m *HookManager) AfterTool(ctx context.Context, result *ToolResultHookResponse) (*ToolResultHookResponse, HookDecision, error) {
	currentResult := result
	for _, h := range m.toolHooks {
		newResult, decision, err := h.AfterTool(ctx, currentResult)
		if err != nil {
			return nil, HookDecision{Action: HookActionStop, Reason: err.Error()}, err
		}
		if decision.Action != HookActionContinue {
			return newResult, decision, nil
		}
		if newResult != nil {
			currentResult = newResult
		}
	}
	return currentResult, HookDecision{Action: HookActionContinue}, nil
}

// ApproveTool calls all approval hooks for tool execution
func (m *HookManager) ApproveTool(ctx context.Context, req *ToolApprovalRequest) (ApprovalDecision, error) {
	for _, h := range m.approvalHooks {
		decision, err := h.ApproveTool(ctx, req)
		if err != nil {
			return ApprovalDecision{Approved: false, Reason: err.Error()}, err
		}
		if !decision.Approved {
			return decision, nil
		}
	}
	// Default: approve if no hooks reject
	return ApprovalDecision{Approved: true, Reason: "no hooks rejected"}, nil
}

// OnEvent broadcasts an event to all event hooks
func (m *HookManager) OnEvent(ctx context.Context, evt bus.Event) error {
	for _, h := range m.eventHooks {
		if err := h.OnEvent(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}
