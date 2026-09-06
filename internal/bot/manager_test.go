package bot

import (
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
