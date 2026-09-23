package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 验证 defaultConfig / DefaultConfig 的 Agent 循环上限默认值，
// 确保与 Web 配置界面(ConfigView.vue)以及 agent 内置默认值三者一致。
//
// 取值 150/200 而非更大的数：单回合受 chatqueue.sessionTurnTimeout（30 分钟）
// 约束，每轮迭代是一次 LLM 调用加工具执行（实测 10~30s），物理可达的迭代数
// 约 60~180。旧值 300 超在这个区间之外——永远先撞时间墙，上限形同虚设。
func TestDefaultConfigAgentDefaults(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Agent.MaxTurns != 150 {
		t.Errorf("defaultConfig().Agent.MaxTurns = %d, want 150", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxIterations != 200 {
		t.Errorf("defaultConfig().Agent.MaxIterations = %d, want 200", cfg.Agent.MaxIterations)
	}
	// 回合时限默认 30 分钟：与 internal/server/chatqueue.go 的兜底常量一致。
	if cfg.Agent.TurnTimeoutMinutes != DefaultTurnTimeoutMinutes {
		t.Errorf("defaultConfig().Agent.TurnTimeoutMinutes = %d, want %d",
			cfg.Agent.TurnTimeoutMinutes, DefaultTurnTimeoutMinutes)
	}

	exp := DefaultConfig()
	if exp.Agent.MaxTurns != 150 {
		t.Errorf("DefaultConfig().Agent.MaxTurns = %d, want 150", exp.Agent.MaxTurns)
	}
	if exp.Agent.TurnTimeoutMinutes != DefaultTurnTimeoutMinutes {
		t.Errorf("DefaultConfig().Agent.TurnTimeoutMinutes = %d, want %d",
			exp.Agent.TurnTimeoutMinutes, DefaultTurnTimeoutMinutes)
	}
}

// 验证 Load 在磁盘 JSON 缺失 agent.* 字段时，会兜底填充默认值。
func TestLoadFillsAgentDefaults(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".magic")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// 写一个不含 agent 字段的配置文件
	cfgPath := filepath.Join(home, ConfigFileName)
	if err := os.WriteFile(cfgPath, []byte(`{"provider":"deepseek","model":"m"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 临时指向该 home 目录
	old := GetMagicHome()
	t.Setenv("GO_MAGIC_HOME", home)
	_ = old

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Agent.MaxTurns != 150 {
		t.Errorf("Load().Agent.MaxTurns = %d, want 150 (default filled)", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxIterations != 200 {
		t.Errorf("Load().Agent.MaxIterations = %d, want 200", cfg.Agent.MaxIterations)
	}
	if cfg.Agent.TurnTimeoutMinutes != DefaultTurnTimeoutMinutes {
		t.Errorf("Load().Agent.TurnTimeoutMinutes = %d, want %d (default filled)",
			cfg.Agent.TurnTimeoutMinutes, DefaultTurnTimeoutMinutes)
	}
}

// 用户显式配置的值必须原样保留，不能被兜底默认值覆盖。
// 这是"改默认值不影响已配置用户"的守门测试。
func TestLoadRespectsExplicitAgentLimits(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".magic")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, ConfigFileName)
	body := `{"provider":"deepseek","model":"m","agent":{"max_turns":300,"max_iterations":400}}`
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GO_MAGIC_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Agent.MaxTurns != 300 {
		t.Errorf("explicit max_turns was overridden: got %d, want 300", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxIterations != 400 {
		t.Errorf("explicit max_iterations was overridden: got %d, want 400", cfg.Agent.MaxIterations)
	}
}

// 用户显式配置的回合时限必须原样保留且可调大——这正是"任务确实需要跑更久"
// 时用户唯一该动的那一项。默认值（30）只在字段缺失时为 0 才会被填上。
func TestLoadRespectsExplicitTurnTimeout(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".magic")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, ConfigFileName)
	body := `{"provider":"deepseek","model":"m","agent":{"turn_timeout_minutes":120}}`
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GO_MAGIC_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Agent.TurnTimeoutMinutes != 120 {
		t.Errorf("explicit turn_timeout_minutes was overridden: got %d, want 120",
			cfg.Agent.TurnTimeoutMinutes)
	}
	// 相邻字段不受影响：调大时限不会顺手改动两个迭代上限（它们是独立闸门）。
	if cfg.Agent.MaxTurns != 150 {
		t.Errorf("MaxTurns = %d, want 150 (unchanged)", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxIterations != 200 {
		t.Errorf("MaxIterations = %d, want 200 (unchanged)", cfg.Agent.MaxIterations)
	}
}
