package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
)

// video_generate 是"付费生成"工具：按渲染秒数计费，单次还要跑 1~5 分钟。
// 它以前走的是通用分支——合成命令里没有"要花钱"这层语义，smart 策略判定
// 低风险后静默放行，模型换个 prompt 再生成一次用户也拦不住。
//
// 这三条用例锁住边界：
//  1. smart 策略下必须把"仅因低风险放行"升级成询问；
//  2. 无人值守通道（gateway/bot/cron）没有可问的人 → fail-closed，且理由说真话；
//  3. 同一条路径不能顺手把普通媒体工具（image_gen）也变成每次弹窗。

func newPaidApprovalTestHook(t *testing.T, cfg *approval.ApprovalConfig) *ApprovalHook {
	t.Helper()
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
	if cfg == nil {
		cfg = approval.DefaultConfig()
	}
	mgr, err := approval.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	waitForApprovalFlush(t, mgr)
	return NewApprovalHookWithManager(mgr)
}

func videoGenArgs() map[string]interface{} {
	return map[string]interface{}{
		"prompt":     "slow dolly-in on a kitten in moonlit grass",
		"duration":   float64(10),
		"resolution": "1080p",
	}
}

func TestVideoGenerateEscalatesLowRiskAutoApproval(t *testing.T) {
	h := newPaidApprovalTestHook(t, nil) // 默认 strategy=smart

	var asked string
	h.SetPromptFunc(func(command, reason string, risk approval.RiskLevel) bool {
		asked = command
		if risk != approval.RiskHigh {
			t.Errorf("risk = %v, want high (paid call)", risk)
		}
		return false
	})

	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "video_generate",
		ToolArgs: videoGenArgs(),
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if asked == "" {
		t.Fatal("video_generate was never shown to the user — smart strategy silently approved a paid call")
	}
	// 计费口径必须出现在卡片上：时长 + 分辨率。
	if !strings.Contains(asked, "10s") || !strings.Contains(asked, "1080p") {
		t.Errorf("approval summary %q is missing the billing parameters (duration/resolution)", asked)
	}
	if dec.Action != hooks.HookActionReject {
		t.Errorf("action = %v, want Reject after the user declined", dec.Action)
	}
}

func TestVideoGenerateFailsClosedWhenNobodyCanBeAsked(t *testing.T) {
	h := newPaidApprovalTestHook(t, nil)
	h.SetNonInteractive(true) // 网关/守护进程：没有任何审批入口

	start := time.Now()
	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "video_generate",
		ToolArgs: videoGenArgs(),
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if dec.Action != hooks.HookActionReject {
		t.Fatalf("action = %v, want Reject (nobody can approve a paid call here)", dec.Action)
	}
	if !strings.Contains(dec.Reason, "bills per rendered second") {
		t.Errorf("reason = %q, want the paid-generation wording instead of the generic file-write hint", dec.Reason)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("approval took %v — it must not block on a prompt", elapsed)
	}
}

func TestVideoGenerateEscalatesUnattendedAutoNarrow(t *testing.T) {
	// auto_narrow 对低/中风险是自动放行的；视频生成不能借用这条豁免。
	h := newPaidApprovalTestHook(t, &approval.ApprovalConfig{Strategy: approval.StrategyAutoNarrow})

	prompted := false
	h.SetPromptFunc(func(command, reason string, risk approval.RiskLevel) bool {
		prompted = true
		return false
	})

	_, _, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "video_generate",
		ToolArgs: videoGenArgs(),
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if !prompted {
		t.Error("auto_narrow auto-approved a paid generation without asking anyone")
	}
}

func TestOtherMediaToolsKeepAutoApproval(t *testing.T) {
	h := newPaidApprovalTestHook(t, nil)

	prompted := false
	h.SetPromptFunc(func(command, reason string, risk approval.RiskLevel) bool {
		prompted = true
		return true
	})

	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName: "image_gen",
		ToolArgs: map[string]interface{}{"prompt": "a red apple"},
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if prompted {
		t.Error("image_gen should not be escalated — only per-second billed generation is")
	}
	if dec.Action != hooks.HookActionContinue {
		t.Errorf("action = %v, want Continue", dec.Action)
	}
}

// 设置页把"付费生成需确认"关掉之后，video_generate 必须回到普通工具的待遇：
// 不再弹提示，也不再因为工具自己声明的 RequiresApproval 被兜底拦下来。
func TestVideoGenerateSkipsConfirmationWhenSwitchIsOff(t *testing.T) {
	cfg := approval.DefaultConfig()
	off := false
	cfg.RequirePaidConfirm = &off
	h := newPaidApprovalTestHook(t, cfg)

	prompted := false
	h.SetPromptFunc(func(command, reason string, risk approval.RiskLevel) bool {
		prompted = true
		return false
	})

	// 工具自己在 agent 侧会被断言成 RequiresApproval=true（ToolRegistry 路径），
	// 这里显式带上，验证钩子会把这条声明按开关降级。
	_, dec, err := h.BeforeTool(context.Background(), &hooks.ToolCallHookRequest{
		ToolName:         "video_generate",
		ToolArgs:         videoGenArgs(),
		RequiresApproval: true,
		ApprovalSummary:  "video_generate (paid video generation)",
	})
	if err != nil {
		t.Fatalf("BeforeTool error: %v", err)
	}
	if prompted {
		t.Error("require_paid_confirm=false must not prompt")
	}
	if dec.Action != hooks.HookActionContinue {
		t.Errorf("action = %v (reason=%q), want Continue", dec.Action, dec.Reason)
	}
}
