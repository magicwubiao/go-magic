package approval

import (
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// NewManagerFromAppConfig builds a Manager from the main config file's
// approval section (pkg/config.ApprovalConfig). ac may be nil, in which case
// DefaultConfig applies.
//
// This is the single place that maps app-config approval settings onto the
// approval engine, so every agent-creation path (web server, bot mode,
// gateway, cron, kanban) can share one policy interpretation instead of
// hand-rolling its own. Previously bot mode skipped this entirely and its
// agents silently ran on the agent package's default hook (DefaultConfig →
// smart strategy) with no approval UI available — every write_file /
// file_edit / execute_code was then fail-closed denied in the non-interactive
// server process, which surfaced to users as "写入被拒绝" clarify loops.
func NewManagerFromAppConfig(ac *config.ApprovalConfig) (*Manager, error) {
	approvalCfg := DefaultConfig()
	if ac != nil {
		approvalCfg.Strategy = Strategy(ac.Strategy)
		approvalCfg.TrustThreshold = ac.TrustThreshold
		approvalCfg.EnableLearning = ac.EnableLearning
		approvalCfg.EnableCLIConfirm = ac.EnableCLIConfirm
		// 仅在显式设置时覆盖 timeout_strategy，否则保留 DefaultConfig 的 deny。
		if ac.TimeoutStrategy != "" {
			approvalCfg.TimeoutStrategy = TimeoutStrategy(ac.TimeoutStrategy)
		}
		// 仅在显式设置 (>0) 时覆盖，否则保留 DefaultConfig 的 60s。
		// 若无条件覆盖，配置文件中 approval 段未写 approval_timeout 时，
		// Go 零值 0 会使 pending 审批立即过期，用户来不及点击批准。
		if ac.ApprovalTimeout > 0 {
			approvalCfg.ApprovalTimeout = ac.ApprovalTimeout
		}
	}

	mgr, err := NewManager(approvalCfg)
	if err != nil {
		return nil, err
	}
	// If no persisted config, use main config values (already set above).
	// 保险丝：万一持久化/映射后策略为空，回落到 smart，避免空策略导致
	// RequestApproval 走 default 分支之外的异常行为。
	if mgr.GetConfig().Strategy == "" {
		mgr.SetStrategy(StrategySmart)
	}
	return mgr, nil
}

// NewManagerFromAppConfigOrWarn is a convenience wrapper for callers that
// treat approval-manager init failure as non-fatal: it returns nil on error
// (agent falls back to its default builtin hook) after logging the reason.
func NewManagerFromAppConfigOrWarn(ac *config.ApprovalConfig, context string) *Manager {
	mgr, err := NewManagerFromAppConfig(ac)
	if err != nil {
		log.Warnf("[Approval] %s: failed to init approval manager from config: %v (falling back to builtin default)", context, err)
		return nil
	}
	return mgr
}
