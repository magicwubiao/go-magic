package server

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/approval"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// syncApprovalToMainConfig 是设置页"保存"按钮的落盘路径。它以前用结构体字面量
// 重建 approval 段，只带 PUT 支持的那几个字段——channel_overrides、
// dangerous_patterns、high_risk_patterns 会被一起冲掉，用户点一次保存就静默
// 丢了按通道策略和自定义危险模式。
//
// 这里同时锁住新加的 require_paid_confirm：关掉之后必须写实 false，
// 不能留下 nil 让下次启动又回到"默认开启"。
func TestSyncApprovalToMainConfigKeepsUnmanagedFields(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	cfg := appconfig.DefaultConfig()
	cfg.Approval = appconfig.DefaultApprovalConfig()
	cfg.Approval.ChannelOverrides = map[string]string{"gateway": "auto_narrow"}
	cfg.Approval.DangerousPatterns = []string{`curl\s+http://evil\.`}
	cfg.Approval.HighRiskPatterns = []string{`rm\s+-rf\s+/data`}

	mgr, err := approval.NewManagerFromAppConfig(cfg.Approval)
	if err != nil {
		t.Fatalf("NewManagerFromAppConfig error: %v", err)
	}
	mgr.SetRequirePaidConfirm(false)

	s := &Server{cfg: cfg, magicHome: t.TempDir(), approvalMgr: mgr}
	s.syncApprovalToMainConfig(mgr)

	if s.cfg.Approval == nil {
		t.Fatal("approval section was wiped")
	}
	if got := s.cfg.Approval.ChannelOverrides["gateway"]; got != "auto_narrow" {
		t.Errorf("channel_overrides[gateway] = %q, want auto_narrow (unmanaged fields must survive a save)", got)
	}
	if len(s.cfg.Approval.DangerousPatterns) != 1 || len(s.cfg.Approval.HighRiskPatterns) != 1 {
		t.Errorf("custom patterns were dropped: dangerous=%v high_risk=%v",
			s.cfg.Approval.DangerousPatterns, s.cfg.Approval.HighRiskPatterns)
	}
	if s.cfg.Approval.PaidConfirmRequired() {
		t.Error("require_paid_confirm=false was not persisted — a restart would re-enable it")
	}
}

// 从没表过态（配置文件里没有这一项）时，落盘要保留"默认开启"的语义。
func TestSyncApprovalToMainConfigPaidConfirmDefaultsOn(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	cfg := appconfig.DefaultConfig()
	cfg.Approval = &appconfig.ApprovalConfig{Strategy: "smart"}

	mgr, err := approval.NewManagerFromAppConfig(cfg.Approval)
	if err != nil {
		t.Fatalf("NewManagerFromAppConfig error: %v", err)
	}

	s := &Server{cfg: cfg, magicHome: t.TempDir(), approvalMgr: mgr}
	s.syncApprovalToMainConfig(mgr)

	if !s.cfg.Approval.PaidConfirmRequired() {
		t.Error("unset require_paid_confirm must stay enabled after a save")
	}
}
