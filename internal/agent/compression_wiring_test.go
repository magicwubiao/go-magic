package agent

import "testing"

// TestWithCompressionWiresSettings 锁死"配置 → compressor"的接线语义：
//
//   - 非零值必须覆盖内置默认；
//   - 0 必须保留内置默认 —— 调用方把 config 原样透传，而 config.Load 对已存在的
//     config.json 直接反序列化到零值、不合并 defaultConfig，所以"用户没写
//     compress_threshold_tokens"必然是 0，绝不能因此把阈值清成 0（等于每轮都压缩）。
//
// 同时兜住回归方向：默认阈值必须停留在现代量级，不能退回 8K 上下文时代的 8000
// （读一个稍大的文件就触发压缩 ⇒ 模型反复重读同一批文件）。
func TestWithCompressionWiresSettings(t *testing.T) {
	prov := &newDistinctCallProvider{}

	ag := newLoopTestAgent(t, prov, WithCompression(12345, 20))
	if ag.compressor == nil {
		t.Fatal("compressor must be constructed")
	}
	if got := ag.compressor.ThresholdTokens; got != 12345 {
		t.Fatalf("threshold not wired: got %d want 12345", got)
	}
	if got := ag.compressor.ProtectLastN; got != 20 {
		t.Fatalf("protectLastN not wired: got %d want 20", got)
	}

	def := newLoopTestAgent(t, prov)
	if def.compressor == nil {
		t.Fatal("compressor must be constructed by default")
	}
	if got := def.compressor.ThresholdTokens; got != defaultCompressThresholdTokens {
		t.Fatalf("built-in threshold = %d, want %d", got, defaultCompressThresholdTokens)
	}
	if got := def.compressor.ProtectLastN; got != defaultCompressProtectLastN {
		t.Fatalf("built-in protectLastN = %d, want %d", got, defaultCompressProtectLastN)
	}

	zero := newLoopTestAgent(t, prov, WithCompression(0, 0))
	if got := zero.compressor.ThresholdTokens; got != defaultCompressThresholdTokens {
		t.Fatalf("WithCompression(0,0) must keep default threshold, got %d", got)
	}
	if got := zero.compressor.ProtectLastN; got != defaultCompressProtectLastN {
		t.Fatalf("WithCompression(0,0) must keep default protectLastN, got %d", got)
	}

	if defaultCompressThresholdTokens < 16000 {
		t.Fatalf("built-in compression threshold %d is too small (8K-context era magnitude); long tasks would compress repeatedly and re-read files",
			defaultCompressThresholdTokens)
	}
}
