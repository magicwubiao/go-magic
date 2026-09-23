package server

import (
	"testing"
	"time"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 本文件钉住「回合时限来自配置」这一契约。
//
// 背景：这个值原本是 chatqueue.go 里的一个常量（30 分钟），用户改不了——
// 于是"任务确实需要跑更久"这件事在配置层面无处表达，用户唯一能动的旋钮是
// max_turns，而那个旋钮被时间墙挡着根本不起作用。改成可配置后，最容易退化
// 的路径有两个，这里分别守住：
//
//  1. 读不到配置时忘了兜底 → 传 0 进 context.WithTimeout 会**立即超时**，
//     表现为"每个回合刚开始就被取消"，而且没有任何报错指向配置。
//  2. 配置里塞了荒谬的大值 → 一个失控回合永久占住会话队列，用户既等不到
//     结果也发不出新消息。

// 配置为 nil（未初始化 / 测试替身）时必须回落到内置默认，绝不能返回 0：
// 返回 0 会让 context.WithTimeout 立刻到期，所有回合秒断。
func TestTurnTimeoutNilConfigFallsBack(t *testing.T) {
	s := &Server{}
	got := s.turnTimeout()
	if got != sessionTurnTimeout {
		t.Fatalf("nil cfg: got %v, want fallback %v", got, sessionTurnTimeout)
	}
	if got <= 0 {
		t.Fatalf("nil cfg returned non-positive timeout %v; every turn would abort instantly", got)
	}
}

// 字段缺失（0）与负数同样走兜底，不能被当成"用户要求 0 分钟"。
func TestTurnTimeoutZeroAndNegativeFallBack(t *testing.T) {
	for _, v := range []int{0, -1, -600} {
		cfg := appconfig.DefaultConfig()
		cfg.Agent.TurnTimeoutMinutes = v
		s := &Server{cfg: cfg}
		if got := s.turnTimeout(); got != sessionTurnTimeout {
			t.Errorf("TurnTimeoutMinutes=%d: got %v, want fallback %v", v, got, sessionTurnTimeout)
		}
	}
}

// 显式配置的值必须原样生效——这是本次改动的核心目的。
func TestTurnTimeoutHonoursConfig(t *testing.T) {
	cfg := appconfig.DefaultConfig()
	cfg.Agent.TurnTimeoutMinutes = 90
	s := &Server{cfg: cfg}

	want := 90 * time.Minute
	if got := s.turnTimeout(); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := s.turnTimeout(); got <= 30*time.Minute {
		t.Fatalf("configured 90 min but got %v; the whole point is to run longer than the old 30", got)
	}
}

// 荒谬的大值必须被夹取到上限，否则一个失控回合能永久钉死会话队列。
func TestTurnTimeoutClampsAbsurdValues(t *testing.T) {
	cfg := appconfig.DefaultConfig()
	cfg.Agent.TurnTimeoutMinutes = 100000
	s := &Server{cfg: cfg}

	got := s.turnTimeout()
	want := time.Duration(maxTurnTimeoutMinutes) * time.Minute
	if got != want {
		t.Fatalf("got %v, want clamped %v", got, want)
	}
	if got > 24*time.Hour {
		t.Fatalf("clamp produced %v, which exceeds 24h", got)
	}
}

// 边界：恰好等于上限时不应被夹（夹取只作用于"超过"）。
func TestTurnTimeoutAtClampBoundaryUnchanged(t *testing.T) {
	cfg := appconfig.DefaultConfig()
	cfg.Agent.TurnTimeoutMinutes = maxTurnTimeoutMinutes
	s := &Server{cfg: cfg}

	want := time.Duration(maxTurnTimeoutMinutes) * time.Minute
	if got := s.turnTimeout(); got != want {
		t.Fatalf("got %v, want %v (boundary value must pass through)", got, want)
	}
}

// 默认配置（新装用户）解出来的就是 30 分钟，与文档/UI 展示一致。
func TestTurnTimeoutDefaultConfigIs30Minutes(t *testing.T) {
	s := &Server{cfg: appconfig.DefaultConfig()}
	if got := s.turnTimeout(); got != 30*time.Minute {
		t.Fatalf("default config: got %v, want 30m", got)
	}
}
