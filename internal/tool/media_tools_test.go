package tool

import "testing"

// media_tools_test.go — 媒体类工具注册与可用性门槛的回归测试。
//
// 背景：这批工具（图片/语音/音频）原来只挂在 RegisterOptionalTools 上，
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
		"image_gen", "image_edit", "video_generate",
		"tts", "asr",
		"tts_available_voices", "asr_available_providers",
		"audio_play", "audio_download",
		"video_analyze",
	}
	for _, name := range want {
		if !r.HasTool(name) {
			t.Errorf("media tool %q not registered", name)
		}
	}

	// 已删除的工具不能被注册回来：没有可用后端，或已改由目录连接器覆盖。
	for _, name := range []string{"send_message", "x_search", "send_email", "send_sms"} {
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

	for _, name := range []string{"image_gen", "tts", "asr", "audio_download"} {
		if !r.HasTool(name) {
			t.Errorf("RegisterAll did not register %q", name)
		}
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

func TestVideoGenerateHiddenWithoutKey(t *testing.T) {
	t.Setenv("VIDEO_GEN_API_KEY", "")

	r := NewRegistry()
	r.RegisterMediaTools()

	if namesOf(r.ListAvailable())["video_generate"] {
		t.Error("video_generate must be hidden when no video API key is configured")
	}
	if !r.HasTool("video_generate") {
		t.Error("video_generate should still be registered (just unavailable)")
	}
}

func TestVideoGenerateVisibleWithKey(t *testing.T) {
	t.Setenv("VIDEO_GEN_API_KEY", "sk-video-test")
	t.Setenv("VIDEO_GEN_PROVIDER", "volcengine")

	r := NewRegistry()
	r.RegisterMediaTools()

	if !namesOf(r.ListAvailable())["video_generate"] {
		t.Error("video_generate should be available when VIDEO_GEN_API_KEY is set")
	}
}

func TestLoadVideoGenConfigEnvOverrides(t *testing.T) {
	t.Setenv("VIDEO_GEN_API_KEY", "sk-env")
	t.Setenv("VIDEO_GEN_PROVIDER", "volcengine")
	t.Setenv("VIDEO_GEN_MODEL", "doubao-seedance-1-0-pro-250528")
	t.Setenv("VIDEO_GEN_OUTPUT_DIR", "/tmp/vids")

	cfg := LoadVideoGenConfig()
	if cfg.APIKey != "sk-env" {
		t.Errorf("APIKey = %q, want sk-env", cfg.APIKey)
	}
	if cfg.Provider != ProviderVolcengine {
		t.Errorf("Provider = %q, want volcengine", cfg.Provider)
	}
	if cfg.Model != "doubao-seedance-1-0-pro-250528" {
		t.Errorf("Model = %q, want doubao-seedance-1-0-pro-250528", cfg.Model)
	}
	if cfg.OutputDirectory != "/tmp/vids" {
		t.Errorf("OutputDirectory = %q, want /tmp/vids", cfg.OutputDirectory)
	}
	// 未覆盖的字段保留默认值
	if cfg.DefaultDuration == 0 || cfg.DefaultRatio == "" || cfg.Timeout == 0 {
		t.Error("defaults should survive partial env override")
	}
}
