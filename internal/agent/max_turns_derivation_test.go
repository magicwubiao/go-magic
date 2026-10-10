package agent

import (
	"strings"
	"testing"
	"time"
)

// TestMaxTurnsDerivedFromTurnTimeout 锁死「maxTurns 与回合时限联动」的判据。
//
// 修的是这处错配：maxTurns(150) 与 agent.turn_timeout_minutes（可配到 24h）原本
// 是两处独立写死的常量 —— 用户把时限调到 2h 之后，150 轮会静默成为真瓶颈。
func TestMaxTurnsDerivedFromTurnTimeout(t *testing.T) {
	cases := []struct {
		name       string
		opts       []AgentOption
		wantTurns  int
		wantExplic bool
	}{
		{
			name:      "wide timeout raises the cap (2h / 30s = 240)",
			opts:      []AgentOption{WithTurnTimeout(2 * time.Hour)},
			wantTurns: 240,
		},
		{
			name:      "narrow timeout leaves the default alone (10m -> 20 < 150)",
			opts:      []AgentOption{WithTurnTimeout(10 * time.Minute)},
			wantTurns: 150,
		},
		{
			name:      "default server timeout (30m) keeps 150",
			opts:      []AgentOption{WithTurnTimeout(30 * time.Minute)},
			wantTurns: 150,
		},
		{
			name:       "explicit max_turns always wins over the derived value",
			opts:       []AgentOption{WithTurnTimeout(2 * time.Hour), WithMaxTurns(50)},
			wantTurns:  50,
			wantExplic: true,
		},
		{
			name:      "derived value is capped (100h -> 2000)",
			opts:      []AgentOption{WithTurnTimeout(100 * time.Hour)},
			wantTurns: maxDerivedMaxTurns,
		},
		{
			name:      "no turn timeout leaves the built-in default",
			opts:      nil,
			wantTurns: 150,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ag := newLoopTestAgent(t, &scriptedLoopProvider{}, tc.opts...)
			if ag.maxTurns != tc.wantTurns {
				t.Fatalf("maxTurns = %d, want %d", ag.maxTurns, tc.wantTurns)
			}
			if ag.maxTurnsExplicit != tc.wantExplic {
				t.Fatalf("maxTurnsExplicit = %v, want %v", ag.maxTurnsExplicit, tc.wantExplic)
			}
		})
	}
}

// TestMaxTurnsExhaustedErrorNamesConfigKeys 保证撞上限时的报错能指明可调项。
// 原文案只有一句 "exceeded maximum turns (N)"，用户无从知道该改哪个配置。
func TestMaxTurnsExhaustedErrorNamesConfigKeys(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{})
	msg := ag.maxTurnsExhaustedError().Error()
	for _, want := range []string{"agent.max_turns", "agent.turn_timeout_minutes", "per-turn"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message must mention %q, got: %s", want, msg)
		}
	}
}
