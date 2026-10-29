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

// TestMaxIterationsFollowsDerivedMaxTurns 锁死 maxIterations 与 maxTurns 的联动。
// 此前只放大 maxTurns、固定 maxIterations(200)，第 200 轮就会先行截断，
// 2h 时限推导出的 240 轮形同虚设。
func TestMaxIterationsFollowsDerivedMaxTurns(t *testing.T) {
	// 2h 时限 ⇒ maxTurns=240，maxIterations 必须跟上，否则第 200 轮被截断。
	ag := newLoopTestAgent(t, &scriptedLoopProvider{}, WithTurnTimeout(2*time.Hour))
	if ag.maxTurns != 240 {
		t.Fatalf("maxTurns = %d, want 240", ag.maxTurns)
	}
	if ag.maxIterations < ag.maxTurns {
		t.Fatalf("maxIterations = %d lags maxTurns = %d; the derived cap would be cut at iteration %d",
			ag.maxIterations, ag.maxTurns, ag.maxIterations)
	}
}

// TestExplicitMaxTurnsAlsoLiftsMaxIterations 显式 max_turns 高于 200 时，
// 同样不能被固定的 maxIterations(200) 截断。
func TestExplicitMaxTurnsAlsoLiftsMaxIterations(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{}, WithMaxTurns(500))
	if ag.maxIterations < 500 {
		t.Fatalf("maxIterations = %d, want >= 500 (explicit max_turns=500)", ag.maxIterations)
	}
}

// TestExplicitMaxIterationsIsRespected steering 显式配置的 maxIterations 原样保留，
// 不被 maxTurns 联动覆盖。
func TestExplicitMaxIterationsIsRespected(t *testing.T) {
	ag := newLoopTestAgent(t, &scriptedLoopProvider{},
		WithSteering(SteeringConfig{MaxIterations: 50}),
		WithTurnTimeout(2*time.Hour))
	if ag.maxIterations != 50 {
		t.Fatalf("maxIterations = %d, want explicit 50", ag.maxIterations)
	}
}
