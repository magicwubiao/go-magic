package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
)

// 连接器工具（mcp_*）在 2026-10-27 之前完全绕过审批：BeforeTool 的 default
// 分支直接放行，于是暴露 delete_file 的 MCP server 可以被模型直接调用，
// 而同样的操作走 bash 一定弹窗。这里锁定两条边界：
//   1. 工具自己声明 RequiresApproval 时，必须进审批链路（非交互环境 fail-closed）；
//   2. 没声明时保持原样放行，不能把每个连接器工具都变成确认弹窗。
func TestBeforeToolHonorsToolDeclaredApproval(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	waitForApprovalFlush(t, mgr)
	h := NewApprovalHookWithManager(mgr)
	h.SetNonInteractive(true)

	start := time.Now()
	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName:         "mcp_github_delete_repo",
		ToolArgs:         map[string]interface{}{"repo": "acme/prod"},
		RequiresApproval: true,
		ApprovalSummary:  "MCP github → delete_repo",
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionReject {
		t.Fatalf("action = %v (reason=%q), want Reject", dec.Action, dec.Reason)
	}
	if !strings.Contains(dec.Reason, "Denied automatically") {
		t.Errorf("reason = %q, want the fail-closed wording", dec.Reason)
	}
	// 走 CLI 提示会阻塞到 defaultApprovalTimeout；非交互必须秒回。
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("approval took %v — it must not block on a prompt", elapsed)
	}
}

func TestBeforeToolPassesThroughConnectorWithoutApproval(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	waitForApprovalFlush(t, mgr)
	h := NewApprovalHookWithManager(mgr)
	h.SetNonInteractive(true)

	call, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "mcp_filesystem_read_file",
		ToolArgs: map[string]interface{}{"path": "/tmp/a.txt"},
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionContinue {
		t.Fatalf("action = %v, want Continue for a read-only connector tool", dec.Action)
	}
	if call == nil {
		t.Fatal("call should be returned unchanged")
	}
}

func TestBuildConnectorCallContextShowsArgs(t *testing.T) {
	ctx := buildConnectorCallContext(&hooks.ToolCallHookRequest{
		ToolName:        "mcp_slack_send_message",
		ApprovalSummary: "MCP slack → send_message",
		ToolArgs: map[string]interface{}{
			"channel": "#general",
			"text":    "deploy finished",
		},
	})

	for _, want := range []string{"mcp_slack_send_message", "MCP slack → send_message", "#general", "deploy finished"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context missing %q:\n%s", want, ctx)
		}
	}
	// 参数按 key 排序输出，保证同一调用每次展示一致。
	if strings.Index(ctx, "channel") > strings.Index(ctx, "text") {
		t.Errorf("arguments should be sorted by key:\n%s", ctx)
	}
}
