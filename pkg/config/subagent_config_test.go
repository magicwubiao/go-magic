package config

import (
	"encoding/json"
	"testing"
	"time"
)

// 回归：SubAgentConfig.Timeout 是 time.Duration，裸 JSON 直解期望纳秒——
// 用户写 "timeout": 300 会被当成 300 纳秒，子任务瞬间超时。
// 现在数字=秒、字符串=Go duration，两者必须都解析正确。
func TestSubAgentConfigUnmarshalTimeout(t *testing.T) {
	// 数字视为秒
	var c1 SubAgentConfig
	if err := json.Unmarshal([]byte(`{"timeout": 300}`), &c1); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if c1.Timeout != 300*time.Second {
		t.Errorf("timeout = %v, want 300s (数字必须按秒解析，不能按纳秒)", c1.Timeout)
	}

	// 字符串视为 Go duration
	var c2 SubAgentConfig
	if err := json.Unmarshal([]byte(`{"timeout": "5m30s"}`), &c2); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if c2.Timeout != 5*time.Minute+30*time.Second {
		t.Errorf("timeout = %v, want 5m30s", c2.Timeout)
	}

	// 不写 timeout 不报错，也不设默认（默认由 DefaultSubAgentConfig / buildSubagentConfig 兜底）
	var c3 SubAgentConfig
	if err := json.Unmarshal([]byte(`{"max_depth": 3}`), &c3); err != nil {
		t.Fatalf("unmarshal without timeout: %v", err)
	}
	if c3.Timeout != 0 {
		t.Errorf("timeout = %v, want 0 (未配置时保持零值)", c3.Timeout)
	}

	// 非法字符串报错而不是静默吞掉
	var c4 SubAgentConfig
	if err := json.Unmarshal([]byte(`{"timeout": "soon"}`), &c4); err == nil {
		t.Error("expected error for invalid duration string")
	}

	// 其余字段不受影响
	var c5 SubAgentConfig
	if err := json.Unmarshal([]byte(`{"max_concurrent": 5, "max_depth": 2, "timeout": 60}`), &c5); err != nil {
		t.Fatalf("unmarshal full: %v", err)
	}
	if c5.MaxConcurrent != 5 || c5.MaxDepth != 2 {
		t.Errorf("max_concurrent=%d max_depth=%d, want 5/2", c5.MaxConcurrent, c5.MaxDepth)
	}
}
