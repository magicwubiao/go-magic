package bot

import (
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// TestBotTurnTimeoutDefaultIsShared 锁死"bot 回合时限默认值与 agent 一致"。
//
// 此前 bot 侧硬编码 5 分钟、pkg/config 与 server 兜底都是 30 分钟：同一个任务在
// 网页里能跑完，到了 Telegram 却在 5 分钟处收到 "deadline approaching"，用户看到
// 的就是"bot 干到一半就不动了"，而且没有任何配置能解释这个差异。
func TestBotTurnTimeoutDefaultIsShared(t *testing.T) {
	want := time.Duration(config.DefaultTurnTimeoutMinutes) * time.Minute
	if defaultTurnTimeout != want {
		t.Fatalf("defaultTurnTimeout = %v, want %v (config.DefaultTurnTimeoutMinutes)", defaultTurnTimeout, want)
	}

	// 未配置（0）时回落到共享默认值。
	m := &Manager{cfg: config.DefaultConfig()}
	if got := m.turnTimeout(); got != want {
		t.Fatalf("unset turn timeout = %v, want shared default %v", got, want)
	}

	// 配置了 bot_mode.turn_timeout_minutes 时仍然以配置为准。
	m.cfg.BotMode.TurnTimeoutMinutes = 2
	if got := m.turnTimeout(); got != 2*time.Minute {
		t.Fatalf("configured turn timeout ignored: got %v, want 2m", got)
	}
}
