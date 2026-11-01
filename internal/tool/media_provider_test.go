package tool

import (
	"context"
	"strings"
	"testing"
)

// media_provider_test.go — 媒体后端扩展点的回归测试。
//
// 这把锁要防的是三种退化：
//  1. 内置后端从注册表里掉了（工具从此只能报 unsupported）；
//  2. 第三方后端注册进来后，工具名/可用性语义被绕过（出现 image_gen 之外的
//     新工具名，或"没配 key 就一律不可用"把本地后端也挡了）；
//  3. 同名后端覆盖/卸载失效，插件重连或卸载后留下无法解释的残影。

// fakeImageBackend 模拟一个插件贡献的后端：不需要 API Key，可用性由自己说了算。
type fakeImageBackend struct {
	name  string
	avail bool
}

func (f fakeImageBackend) Name() string                   { return f.name }
func (f fakeImageBackend) Available(*ImageGenConfig) bool { return f.avail }
func (f fakeImageBackend) Generate(context.Context, *ImageGenConfig, ImageRequest) (*ImageGenerationResult, error) {
	return &ImageGenerationResult{Images: []ImageInfo{{Data: []byte("png"), Format: "png"}}}, nil
}

func TestBuiltinImageBackendsRegistered(t *testing.T) {
	for _, want := range []string{"dall-e", "stable-diffusion", "midjourney", "together"} {
		if _, ok := LookupImageBackend(want); !ok {
			t.Errorf("builtin image backend %q not registered", want)
		}
	}
}

func TestBuiltinVideoBackendsRegistered(t *testing.T) {
	for _, want := range []string{"dashscope", "volcengine"} {
		if _, ok := LookupVideoBackend(want); !ok {
			t.Errorf("builtin video backend %q not registered", want)
		}
	}
}

func TestLookupNormalizesProviderName(t *testing.T) {
	// config.json / 环境变量 / 工具参数三处都可能手写成大写或带空格。
	for _, name := range []string{"DALL-E", " dall-e ", "DalL-E", "stable-diffusion"} {
		if _, ok := LookupImageBackend(name); !ok {
			t.Errorf("LookupImageBackend(%q) should normalize and hit the builtin backend", name)
		}
	}
	if _, ok := LookupImageBackend("no-such-provider"); ok {
		t.Error("unknown provider must not resolve")
	}
}

func TestRegisterImageBackendOverridesAndUnregisters(t *testing.T) {
	RegisterImageBackend(fakeImageBackend{name: "demo", avail: true})
	t.Cleanup(func() { UnregisterImageBackend("demo") })

	first, ok := LookupImageBackend("demo")
	if !ok || !first.Available(nil) {
		t.Fatal("registered backend should be found and available")
	}

	// 插件重连/升级：同名后注册者必须覆盖先注册者
	RegisterImageBackend(fakeImageBackend{name: "demo", avail: false})
	second, _ := LookupImageBackend("demo")
	if second.Available(nil) {
		t.Error("re-registering the same name must override the previous backend")
	}

	UnregisterImageBackend("DEMO")
	if _, ok := LookupImageBackend("demo"); ok {
		t.Error("unregister should be case-insensitive and remove the backend")
	}
}

// 关键用例：插件后端说"我能用"，即使没有 API Key，工具也必须可用。
// 否则本地 ComfyUI / SD-WebUI 这类无鉴权后端永远进不了工具清单。
func TestToolAvailabilityDelegatesToBackend(t *testing.T) {
	RegisterImageBackend(fakeImageBackend{name: "local-comfy", avail: true})
	t.Cleanup(func() { UnregisterImageBackend("local-comfy") })

	cfg := DefaultImageGenConfig()
	cfg.Provider = "local-comfy"
	cfg.APIKey = ""
	if !NewImageGenerationTool(cfg).Available() {
		t.Error("tool must be available when its backend reports available, even without an API key")
	}

	// provider 未注册（拼错 / 插件已卸载）→ 不可用
	unknown := DefaultImageGenConfig()
	unknown.Provider = "no-such-provider"
	unknown.APIKey = "sk-still-set"
	if NewImageGenerationTool(unknown).Available() {
		t.Error("tool must be unavailable when its provider has no registered backend")
	}
}

// 无 key 的插件后端要能真正跑完 Execute，而不只是"在清单里可见"。
func TestExecuteRunsWithKeylessBackend(t *testing.T) {
	RegisterImageBackend(fakeImageBackend{name: "local-comfy", avail: true})
	t.Cleanup(func() { UnregisterImageBackend("local-comfy") })

	cfg := DefaultImageGenConfig()
	cfg.Provider = "local-comfy"
	cfg.APIKey = ""
	cfg.OutputDirectory = t.TempDir()

	res, err := NewImageGenerationTool(cfg).Execute(context.Background(), map[string]interface{}{"prompt": "a cat"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if m["status"] != "success" {
		t.Errorf("status = %v, want success (key must not be a hard gate)", m["status"])
	}
	if paths, _ := m["images"].([]string); len(paths) != 1 {
		t.Errorf("expected 1 saved image, got %v", m["images"])
	}
}

// 后端报不可用时，Execute 返回 not_configured 而不是硬失败 —— 这是给模型看的
// 可执行提示（去配置），不是错误堆栈。
func TestExecuteReportsNotConfigured(t *testing.T) {
	cfg := DefaultImageGenConfig()
	cfg.Provider = ProviderDALLE
	cfg.APIKey = ""

	res, err := NewImageGenerationTool(cfg).Execute(context.Background(), map[string]interface{}{"prompt": "a cat"})
	if err != nil {
		t.Fatalf("not-configured path should not return an error: %v", err)
	}
	if m, _ := res.(map[string]interface{}); m["status"] != "not_configured" {
		t.Errorf("status = %v, want not_configured", m["status"])
	}
}

// provider 拼错时要报"不支持 + 可用列表"，不能落到"未配置"把用户带偏。
func TestUnknownImageProviderMessageListsAlternatives(t *testing.T) {
	cfg := DefaultImageGenConfig()
	cfg.Provider = "kling-typo"
	cfg.APIKey = "sk-test"

	_, err := NewImageGenerationTool(cfg).Execute(context.Background(), map[string]interface{}{"prompt": "x"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	for _, want := range []string{"kling-typo", "dall-e", "together"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

func TestUnknownVideoProviderMessageListsAlternatives(t *testing.T) {
	cfg := DefaultVideoGenConfig()
	cfg.Provider = "runway-typo"
	cfg.APIKey = "sk-test"

	_, err := NewVideoGenerationTool(cfg).Execute(context.Background(), map[string]interface{}{"prompt": "x"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	for _, want := range []string{"runway-typo", "dashscope", "volcengine"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}
