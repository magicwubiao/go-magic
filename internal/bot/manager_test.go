package bot

import (
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// TestIsEnabledDefaults locks in the "bot mode on by default" semantics:
// a missing bot_mode section must fall back to the default (enabled),
// while explicit true/false in config always wins.
func TestIsEnabledDefaults(t *testing.T) {
	// Missing bot_mode section -> default (enabled).
	if !IsEnabled(&config.Config{}) {
		t.Error("IsEnabled with nil BotMode should default to enabled")
	}

	// Explicit enabled.
	cfgOn := &config.Config{BotMode: &config.BotModeConfig{Enabled: true}}
	if !IsEnabled(cfgOn) {
		t.Error("IsEnabled with explicit Enabled:true should be true")
	}

	// Explicit disabled must win over the default.
	cfgOff := &config.Config{BotMode: &config.BotModeConfig{Enabled: false}}
	if IsEnabled(cfgOff) {
		t.Error("IsEnabled with explicit Enabled:false should be false")
	}

	// DefaultBotModeConfig itself must be enabled.
	if !config.DefaultBotModeConfig().Enabled {
		t.Error("DefaultBotModeConfig().Enabled should be true")
	}

	// defaultConfig should carry the default BotMode section.
	if dc := config.DefaultConfig(); dc.BotMode == nil || !dc.BotMode.Enabled {
		t.Error("defaultConfig() should include an enabled BotMode section")
	}
}

// botWorkDirFor 是 buildBotDeps（创建沙箱目录）与回合 runner（tool.WithWorkDir
// 注入）共用的路径推导函数 —— 两边必须一致，否则文件工具解析出的相对路径会
// 落到审批 hook 范围判定与沙箱设计之外。

func TestBotWorkDirForUsesWorkingDir(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{WorkingDir: base}
	bc := &Config{Name: "Coder Bot!"}

	got := botWorkDirFor(cfg, bc)
	want := filepath.Join(base, "bots", "coder_bot_")
	if got != want {
		t.Errorf("botWorkDirFor = %q, want %q", got, want)
	}
}

func TestBotWorkDirForEmptyWorkingDirFallsBackToCwd(t *testing.T) {
	cfg := &config.Config{}
	bc := &Config{Name: "coder"}

	got := botWorkDirFor(cfg, bc)
	if !filepath.IsAbs(got) {
		t.Fatalf("botWorkDirFor = %q, want absolute path (cwd fallback)", got)
	}
	// 结构：<cwd>/bots/coder
	if filepath.Base(filepath.Dir(got)) != "bots" || filepath.Base(got) != "coder" {
		t.Errorf("botWorkDirFor = %q, want .../bots/coder", got)
	}
}

func TestBotWorkDirForSanitizesName(t *testing.T) {
	cfg := &config.Config{WorkingDir: t.TempDir()}
	cases := map[string]string{
		"":       "bot", // 空名兜底
		"..":     "bot", // 路径穿越兜底
		"Mi-X_1": "mi-x_1",
		"中文 Bot": "___bot",
	}
	for name, want := range cases {
		got := filepath.Base(botWorkDirFor(cfg, &Config{Name: name}))
		if got != want {
			t.Errorf("sanitize(%q) = %q, want %q", name, got, want)
		}
	}
}
