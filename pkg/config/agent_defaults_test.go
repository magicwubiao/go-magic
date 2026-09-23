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

	exp := DefaultConfig()
	if exp.Agent.MaxTurns != 150 {
		t.Errorf("DefaultConfig().Agent.MaxTurns = %d, want 150", exp.Agent.MaxTurns)
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
