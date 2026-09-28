package agent

import (
	"context"
	"strings"
	"testing"
	"time"

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
// 审批记录的落盘是异步的（saveHistoryAsync/savePatternsAsync），必须先等
// 它写完再让 t.TempDir() 删除目录，否则 CI 上会偶发
// "unlinkat ...: directory not empty"。

func waitForApprovalFlush(t *testing.T, mgr *approval.Manager) {
	t.Helper()
	// t.Cleanup 是 LIFO：这里晚于 t.Setenv/t.TempDir 注册，因此会先于
	// 目录删除执行。
	t.Cleanup(mgr.FlushPendingSaves)
}

func TestBeforeToolFailClosedDenialIsTruthful(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	waitForApprovalFlush(t, mgr)
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

// gateway / bot 这类守护进程常常是从终端里启动的：stdin 恰好是 tty，但控制台前
// 没有人会看到审批提示。SetNonInteractive 必须让钩子直接 fail-closed，而不是去读
// stdin（那会把整轮对话阻塞到超时，还把"没有人做过的决定"记成用户拒绝）。
func TestBeforeToolNonInteractiveOverridesTerminalStdin(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	waitForApprovalFlush(t, mgr)
	h := NewApprovalHookWithManager(mgr)
	if h.IsNonInteractive() {
		t.Fatal("hook should default to interactive")
	}
	h.SetNonInteractive(true)
	if !h.IsNonInteractive() {
		t.Fatal("SetNonInteractive(true) did not stick")
	}

	// 假装处在交互式终端：非交互标记必须压过这个判定。
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	defer func() { stdinIsTerminal = orig }()

	start := time.Now()
	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "write_file",
		// 工作目录外的路径：拿不到 C2 范围放行，必然要走确认分支。
		ToolArgs: map[string]interface{}{"path": "/definitely/out/of/scope.txt", "content": "x"},
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionReject {
		t.Fatalf("action = %v (reason=%q), want Reject", dec.Action, dec.Reason)
	}
	if !strings.Contains(dec.Reason, "Denied automatically") {
		t.Errorf("reason = %q, want the automatic-denial wording", dec.Reason)
	}
	if strings.Contains(dec.Reason, "User rejected") {
		t.Errorf("reason must not claim a user rejected the call, got %q", dec.Reason)
	}
	// 走 CLI 提示会阻塞到 defaultApprovalTimeout（30s）；这里必须秒回。
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("BeforeTool blocked for %v — it must not read stdin in non-interactive mode", elapsed)
	}
}

func TestBeforeToolPromptFuncDenyAttribution(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := approval.NewManager(&approval.ApprovalConfig{Strategy: approval.StrategyManual})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	waitForApprovalFlush(t, mgr)
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
	waitForApprovalFlush(t, mgr)
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
