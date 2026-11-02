package tool

// video_gen_override_test.go — 运行期改选 provider 的回归测试。
//
// 起因是一次真实的对话事故：用户配好 minimax 之后让模型做推广视频，模型把本地
// 首帧图当 image_path 传进来（minimax 只收公网 URL），失败后**自己换 provider**
// 依次去试 dashscope / kling / runway。而 video_gen 段只有一份 key 和一份
// base_url，于是请求变成了 https://api.minimax.cn/services/aigc/... 这类不存在的
// 路径，回来一串 nginx 404 / "404 page not found"，一次对话刷出一屏互相矛盾的
// 错误。用户看到的现象是"明明配好了，怎么全在报错"。
//
// 约定：内置厂商的凭据是共用的，所以运行期换到另一家内置必须当场拒绝；插件后端
// 自带凭据可以换，但配置里那家的 base_url / model 不能跟着带过去。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// 配置的是 dashscope，参数里要 minimax —— 必须回 not_configured，且完全不发请求。
func TestVideoGenerateRejectsBuiltinProviderOverride(t *testing.T) {
	tool := NewVideoGenerationTool(&VideoGenConfig{
		Provider:          ProviderDashScope,
		APIKey:            "sk-test",
		BaseURL:           "https://dashscope.aliyuncs.com/api/v1",
		Model:             "wan2.2-t2v-plus",
		DefaultDuration:   5,
		DefaultRatio:      "16:9",
		DefaultResolution: "720p",
		Timeout:           time.Second,
	})

	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt":   "a cat walking through grass",
		"provider": "minimax",
	})
	if err != nil {
		t.Fatalf("override must not hit the network, got error: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if m["status"] != "not_configured" {
		t.Fatalf("status = %v, want not_configured", m["status"])
	}
	msg, _ := m["message"].(string)
	for _, want := range []string{"dashscope", "minimax"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should name %q so the model stops guessing: %q", want, msg)
		}
	}
}

// 传配置那家的别名（hailuo → minimax）不算"换 provider"，必须照常发请求。
func TestVideoGenerateKeepsAliasOfConfiguredProvider(t *testing.T) {
	tool := NewVideoGenerationTool(&VideoGenConfig{
		Provider:          "minimax",
		APIKey:            "sk-test",
		BaseURL:           "http://127.0.0.1:1", // 必然是传输层失败，证明确实发了请求
		Model:             "MiniMax-H3",
		DefaultDuration:   5,
		DefaultRatio:      "16:9",
		DefaultResolution: "768p",
		Timeout:           2 * time.Second,
	})

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt":   "a cat",
		"provider": "hailuo",
	})
	if err == nil {
		t.Fatal("expected a transport error from the unreachable base_url")
	}
	if strings.Contains(err.Error(), "not_configured") {
		t.Fatalf("alias of the configured provider must not be rejected: %v", err)
	}
}

// 插件后端自带凭据，允许运行期换过去；但 base_url / model 属于配置里那一家，
// 不能一起带过去（否则插件会拿着别家的域名和模型名发请求）。
type recordingVideoBackend struct {
	got *VideoGenConfig
}

func (b *recordingVideoBackend) Name() string                   { return "plugin-video" }
func (b *recordingVideoBackend) Available(*VideoGenConfig) bool { return true }
func (b *recordingVideoBackend) Generate(_ context.Context, cfg *VideoGenConfig, _ VideoRequest) (*VideoResult, error) {
	clone := *cfg
	b.got = &clone
	return nil, errors.New("probe backend invoked")
}

func TestVideoGeneratePassesCleanConfigToPluginBackend(t *testing.T) {
	rec := &recordingVideoBackend{}
	RegisterVideoBackendOwned("test-plugin", rec)
	t.Cleanup(func() { UnregisterVideoBackendsByOwner("test-plugin") })

	tool := NewVideoGenerationTool(&VideoGenConfig{
		Provider:          "minimax",
		APIKey:            "sk-test",
		BaseURL:           "https://api.minimax.cn",
		Model:             "MiniMax-H3",
		DefaultDuration:   5,
		DefaultRatio:      "16:9",
		DefaultResolution: "768p",
		Timeout:           time.Second,
	})

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt":   "a cat",
		"provider": "plugin-video",
	})
	if err == nil || !strings.Contains(err.Error(), "probe backend invoked") {
		t.Fatalf("plugin backend should have been called, got: %v", err)
	}
	if rec.got == nil {
		t.Fatal("backend did not receive a config")
	}
	if rec.got.BaseURL != "" {
		t.Errorf("base_url leaked to plugin backend: %q", rec.got.BaseURL)
	}
	if rec.got.Model != "" {
		t.Errorf("model leaked to plugin backend: %q", rec.got.Model)
	}
	if rec.got.APIKey != "sk-test" {
		t.Errorf("api key should still be passed, got %q", rec.got.APIKey)
	}
}
