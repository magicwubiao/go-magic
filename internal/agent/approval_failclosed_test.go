package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
)

// 背景（2026-09-23 bot 模式"写入被拒绝"）：非交互会话（bot / 后台任务）里
// write_file 触发 AskUser 后，若既无 webMode 也无 promptFunc，会走 fail-closed
// 拒绝。旧实现把拒绝理由统一包装成 "User rejected command: ..." —— 用户根本
// 没被询问过，模型被误导去"向用户道歉"并反复澄清。回归锁定两点：
// ① fail-closed 拒绝必须说真话（Denied automatically + 可行动指引）；
// ② 真正的用户拒绝必须如实标注来源（CLI / interactive prompt）。
// NewManager 会写 GetMagicHome()/approval，用 GO_MAGIC_HOME 隔离。

func TestBeforeToolFailClosedDenialIsTruthful(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	h := NewApprovalHookWithManager(mgr)

	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()

	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "write_file",
		ToolArgs: map[string]interface{}{"path": "out.html", "content": "<html></html>"},
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionReject {
		t.Fatalf("action = %v, want Reject", dec.Action)
	}
	if !strings.Contains(dec.Reason, "Denied automatically") {
		t.Errorf("reason should state the denial was automatic (no user asked), got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "working directory") {
		t.Errorf("reason should include actionable guidance, got %q", dec.Reason)
	}
	if strings.Contains(dec.Reason, "User rejected") {
		t.Errorf("reason must not claim a user rejected the call, got %q", dec.Reason)
	}
}

func TestBeforeToolPromptFuncDenyAttribution(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	h := NewApprovalHookWithManager(mgr)
	h.SetPromptFunc(func(command, reason string, riskLevel approval.RiskLevel) bool {
		return false // user actively denies
	})

	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "write_file",
		ToolArgs: map[string]interface{}{"path": "out.html", "content": "<html></html>"},
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionReject {
		t.Fatalf("action = %v, want Reject", dec.Action)
	}
	if !strings.Contains(dec.Reason, "User rejected via interactive prompt") {
		t.Errorf("reason should attribute the denial to the user, got %q", dec.Reason)
	}
}

func TestBeforeToolAutoStrategyApprovesInSession(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	// 用户 config 里的 strategy=auto 经 NewManagerFromAppConfig 接线后，
	// bot 会话的 write_file 不应再被拦（此前 bot 跑在默认 smart 上，
	// 又没有审批 UI，写入必然被拒）。
	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyAutoApprove})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	h := NewApprovalHookWithManager(mgr)

	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "write_file",
		ToolArgs: map[string]interface{}{"path": "pelican_bike.html", "content": "hi"},
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionContinue {
		t.Fatalf("action = %v (reason=%q), want Continue", dec.Action, dec.Reason)
	}
}
