package tool

import (
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// TestVisionToolBecomesAvailableAfterEnvLoad 是"面板保存 → 工具立即可用"的端到端断言。
//
// 这条链路曾经是断的：面板把 VISION_API_KEY 写进 .env，但没有任何代码把它
// 加载进进程环境，于是 visionBackendConfigured 永远返回 false，
// vision_analyze 在工具列表里根本不出现。用户看到的现象是"填了 key 也没用"。
func TestVisionToolBecomesAvailableAfterEnvLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)
	const key = "VISION_API_KEY"
	_ = os.Unsetenv(key)
	// 清掉可能由外部环境提供的视觉后端判定。
	_ = os.Unsetenv("OPENAI_API_KEY")
	appconfig.ProcessEnv().Clear()
	ResetEnvProbes()
	t.Cleanup(func() {
		appconfig.ProcessEnv().Clear()
		ResetEnvProbes()
		_ = os.Unsetenv(key)
	})

	tool := &VisionAnalyzeTool{}
	if tool.Available() {
		t.Fatalf("vision tool should start unavailable without a key")
	}

	// 面板写入 + 立即生效（不重启进程）。
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(key+"=sk-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if applied := appconfig.LoadEnvFor(home); applied != 1 {
		t.Fatalf("expected 1 applied var, got %d", applied)
	}
	ResetEnvProbes()

	if !tool.Available() {
		t.Fatalf("vision tool should be available after .env is loaded")
	}

	// 删除后必须重新变回不可用，否则面板会继续展示一个必然失败的工具。
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	appconfig.ProcessEnv().Clear()
	ResetEnvProbes()
	if tool.Available() {
		t.Fatalf("vision tool should be unavailable again after the key is revoked")
	}
}
