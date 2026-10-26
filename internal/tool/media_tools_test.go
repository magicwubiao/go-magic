package tool

import "testing"

// media_tools_test.go — 媒体类工具注册与可用性门槛的回归测试。
//
// 背景：这批工具（图片/语音/音频/X 搜索）原来只挂在 RegisterOptionalTools 上，
// 而该方法零调用方，等于整批死代码。这里锁住"注册得上 + 没 key 时不暴露"。

// namesOf 把 ListAvailable 的结果摊平成名字集合。
func namesOf(tools []Tool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, t := range tools {
		out[t.Name()] = true
	}
	return out
}

func TestRegisterMediaToolsPresent(t *testing.T) {
	r := NewRegistry()
	r.RegisterMediaTools()

	want := []string{
		"image_gen", "image_edit",
		"tts", "asr",
		"tts_available_voices", "asr_available_providers",
		"audio_play", "audio_download",
		"video_analyze", "x_search",
	}
	for _, name := range want {
		if !r.HasTool(name) {
			t.Errorf("media tool %q not registered", name)
		}
	}

	// 已删除的工具不能被注册回来（没有可用后端）
	for _, name := range []string{"video_generate", "send_message"} {
		if r.HasTool(name) {
			t.Errorf("removed tool %q is still registered", name)
		}
	}
}

func TestRegisterAllIncludesMediaTools(t *testing.T) {
	// web/CLI/bot/gateway 都只调 RegisterAll，媒体工具必须在这里面，
	// 否则"注册了但没人调"的老问题会再次出现。
	r := NewRegistry()
	r.RegisterAll(t.TempDir())

	for _, name := range []string{"image_gen", "tts", "asr", "audio_download", "x_search"} {
		if !r.HasTool(name) {
			t.Errorf("RegisterAll did not register %q", name)
		}
	}
}

func TestXSearchHiddenWithoutCredentials(t *testing.T) {
	t.Setenv("X_API_KEY", "")
	t.Setenv("X_BEARER_TOKEN", "")

	r := NewRegistry()
	r.RegisterMediaTools()

	if namesOf(r.ListAvailable())["x_search"] {
		t.Error("x_search must be hidden when no X credentials are configured")
	}
	if !r.HasTool("x_search") {
		t.Error("x_search should still be registered (just unavailable)")
	}
}

func TestXSearchVisibleWithCredentials(t *testing.T) {
	t.Setenv("X_API_KEY", "")
	t.Setenv("X_BEARER_TOKEN", "test-bearer")

	r := NewRegistry()
	r.RegisterMediaTools()

	if !namesOf(r.ListAvailable())["x_search"] {
		t.Error("x_search should be available when X_BEARER_TOKEN is set")
	}
}

func TestLoadImageGenConfigEnvOverrides(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "sk-env")
	t.Setenv("IMAGE_GEN_PROVIDER", "together")
	t.Setenv("IMAGE_GEN_OUTPUT_DIR", "/tmp/imgs")

	cfg := LoadImageGenConfig()
	if cfg.APIKey != "sk-env" {
		t.Errorf("APIKey = %q, want sk-env", cfg.APIKey)
	}
	if cfg.Provider != ProviderTogether {
		t.Errorf("Provider = %q, want together", cfg.Provider)
	}
	if cfg.OutputDirectory != "/tmp/imgs" {
		t.Errorf("OutputDirectory = %q, want /tmp/imgs", cfg.OutputDirectory)
	}
	// 未覆盖的字段保留默认值
	if cfg.DefaultSize == "" || cfg.Timeout == 0 {
		t.Error("defaults should survive partial env override")
	}
}

func TestTweetURL(t *testing.T) {
	cases := []struct {
		handle, id, want string
	}{
		{"jack", "123", "https://x.com/jack/status/123"},
		{"@jack", "123", "https://x.com/jack/status/123"},
		{"", "123", "https://x.com/i/status/123"},
		{"  ", "123", "https://x.com/i/status/123"},
	}
	for _, c := range cases {
		if got := tweetURL(c.handle, c.id); got != c.want {
			t.Errorf("tweetURL(%q, %q) = %q, want %q", c.handle, c.id, got, c.want)
		}
	}
}
