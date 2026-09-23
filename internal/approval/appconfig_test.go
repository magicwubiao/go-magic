package approval

import (
	"testing"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// 本文件覆盖 NewManagerFromAppConfig：主配置 approval 段 → 审批引擎配置的
// 唯一映射入口（web / bot / gateway 等所有 agent 创建路径共用）。
// NewManager 会把 patterns/history 落到 GetMagicHome()/approval —— 必须用
// GO_MAGIC_HOME 隔离，禁止碰真实 ~/.magic。

func TestNewManagerFromAppConfigNilFallsBackToDefault(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := NewManagerFromAppConfig(nil)
	if err != nil {
		t.Fatalf("NewManagerFromAppConfig(nil) error: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if got := mgr.GetConfig().Strategy; got != StrategySmart {
		t.Errorf("strategy = %q, want default %q", got, StrategySmart)
	}
}

func TestNewManagerFromAppConfigMapsStrategy(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	mgr, err := NewManagerFromAppConfig(&config.ApprovalConfig{
		Strategy:         "auto",
		TrustThreshold:   3,
		EnableLearning:   true,
		EnableCLIConfirm: false,
		ApprovalTimeout:  90,
		TimeoutStrategy:  "allow_all",
	})
	if err != nil {
		t.Fatalf("NewManagerFromAppConfig error: %v", err)
	}
	got := mgr.GetConfig()
	if got.Strategy != StrategyAutoApprove {
		t.Errorf("strategy = %q, want %q", got.Strategy, StrategyAutoApprove)
	}
	if got.TimeoutStrategy != TimeoutStrategyAllowAllAudit {
		t.Errorf("timeout_strategy = %q, want %q", got.TimeoutStrategy, TimeoutStrategyAllowAllAudit)
	}
	if got.ApprovalTimeout != 90 {
		t.Errorf("approval_timeout = %d, want 90", got.ApprovalTimeout)
	}
}

func TestNewManagerFromAppConfigZeroValuesKeepDefaults(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())

	// 配置段存在但 approval_timeout / timeout_strategy 未写：Go 零值不得
	// 覆盖默认值（0 会让 pending 审批立即过期，"" 会丢掉 deny 默认）。
	mgr, err := NewManagerFromAppConfig(&config.ApprovalConfig{Strategy: "manual"})
	if err != nil {
		t.Fatalf("NewManagerFromAppConfig error: %v", err)
	}
	got := mgr.GetConfig()
	if got.ApprovalTimeout != 60 {
		t.Errorf("approval_timeout = %d, want default 60", got.ApprovalTimeout)
	}
	if got.TimeoutStrategy != TimeoutStrategyDeny {
		t.Errorf("timeout_strategy = %q, want default %q", got.TimeoutStrategy, TimeoutStrategyDeny)
	}
}
