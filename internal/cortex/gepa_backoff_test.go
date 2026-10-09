package cortex

import (
	"testing"
	"time"
)

// TestGEPABackoffInterval 锁定退避曲线：5m 起步倍增、封顶 4h。
// 事故背景（2026-10-08）：provider 域名解析不了时，进化循环每 5 分钟原样重试
// 并打一条同样的错误日志，24 小时 288 条噪声——退避后同样故障只剩个位数条。
func TestGEPABackoffInterval(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, gepaBaseInterval},
		{1, 10 * time.Minute},
		{2, 20 * time.Minute},
		{3, 40 * time.Minute},
		{4, 80 * time.Minute},
		{5, 160 * time.Minute},
		{6, gepaMaxInterval},
		{7, gepaMaxInterval},
		{100, gepaMaxInterval},
	}
	for _, tc := range cases {
		if got := gepaBackoffInterval(tc.failures); got != tc.want {
			t.Errorf("gepaBackoffInterval(%d) = %v, want %v", tc.failures, got, tc.want)
		}
	}
}

// TestGEPABackoffIsMonotonicAndBounded 防止后续改动把间隔调成负值/回退。
func TestGEPABackoffIsMonotonicAndBounded(t *testing.T) {
	prev := time.Duration(0)
	for f := 0; f <= 40; f++ {
		got := gepaBackoffInterval(f)
		if got < gepaBaseInterval || got > gepaMaxInterval {
			t.Fatalf("failures=%d 的间隔 %v 越界（应在 [%v, %v]）", f, got, gepaBaseInterval, gepaMaxInterval)
		}
		if got < prev {
			t.Fatalf("退避间隔不应回退：failures=%d 得到 %v，上一步 %v", f, got, prev)
		}
		prev = got
	}
}

// TestGEPAEnabledDefaultsAndOverride 锁定 cortex.gepa_enabled 的语义：
// 未配置（nil）默认开启；显式 false 必须真的关掉。
func TestGEPAEnabledDefaultsAndOverride(t *testing.T) {
	def := NewManagerWithProfileAndConfig(t.TempDir(), nil, "", nil)
	if !def.gepaEnabled {
		t.Fatal("nil config 时 GEPA 应默认开启")
	}

	off := NewManagerWithProfileAndConfig(t.TempDir(), nil, "", &ManagerConfig{
		Enabled:     true,
		GEPAEnabled: false,
	})
	if off.gepaEnabled {
		t.Fatal("显式 GEPAEnabled=false 必须关掉自进化")
	}
	if off.GEPAEngine != nil {
		t.Fatal("关闭状态下不应创建 GEPA 引擎")
	}
}
