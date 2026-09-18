package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 验证 defaultConfig / DefaultConfig 的 Agent 循环上限默认值，
// 确保与 Web 配置界面(ConfigView.vue)以及 agent 内置默认值三者一致。
func TestDefaultConfigAgentDefaults(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Agent.MaxTurns != 300 {
		t.Errorf("defaultConfig().Agent.MaxTurns = %d, want 300", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxIterations != 400 {
		t.Errorf("defaultConfig().Agent.MaxIterations = %d, want 400", cfg.Agent.MaxIterations)
	}

	exp := DefaultConfig()
	if exp.Agent.MaxTurns != 300 {
		t.Errorf("DefaultConfig().Agent.MaxTurns = %d, want 300", exp.Agent.MaxTurns)
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
	if cfg.Agent.MaxTurns != 300 {
		t.Errorf("Load().Agent.MaxTurns = %d, want 300 (default filled)", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxIterations != 400 {
		t.Errorf("Load().Agent.MaxIterations = %d, want 400", cfg.Agent.MaxIterations)
	}
}
