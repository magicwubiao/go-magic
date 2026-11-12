package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// media_model_refresh_test.go — 模型清单换到新一代之后，参数契约的回归护栏。
//
// 这一组测试守的是"清单里写了什么，请求里就必须发什么"：catalog 与后端脱节
// 过一次（catalog 写 FLUX、代码发 SDXL），症状是设置页能选但一调用就错，
// 排查成本极高。每次更新模型清单，先让这些断言过。

// GPT Image 只接受三档尺寸：旧档位必须吸附过去，而不是原样发出去吃 400。
func TestGPTImageSizeSnapsToSupportedTiers(t *testing.T) {
	cases := map[string]string{
		"1024x1024": "1024x1024",
		"1792x1024": "1536x1024",
		"1024x1792": "1024x1536",
		"1536x1024": "1536x1024",
		"1024x1536": "1024x1536",
		"1:1":       "1024x1024",
		"16:9":      "1536x1024",
		"9:16":      "1024x1536",
		"1280x720":  "1536x1024",
		"720x1280":  "1024x1536",
		"":          "1024x1024",
		"weird":     "1024x1024",
	}
	for in, want := range cases {
		if got := gptImageSize(in); got != want {
			t.Errorf("gptImageSize(%q) = %q, want %q", in, got, want)
		}
	}
}

// 时长区间必须来自 catalog：Seedance 2.5 的 30 秒不能被工具层的 15 砍掉。
func TestVideoDurationRangeFollowsCatalog(t *testing.T) {
	cases := map[VideoProvider][2]int{
		ProviderVolcengine: {4, 30},
		ProviderDashScope:  {2, 15},
		"kling":            {3, 15},
		"runway":           {5, 10},
	}
	for p, want := range cases {
		gotMin, gotMax := videoDurationRange(p)
		if gotMin != want[0] || gotMax != want[1] {
			t.Errorf("videoDurationRange(%q) = %d-%d, want %d-%d",
				p, gotMin, gotMax, want[0], want[1])
		}
	}

	// 未知 provider（插件后端）回落 3-15，而不是 0-0 —— 那会把时长钳成 0 秒。
	if minD, maxD := videoDurationRange("some-plugin"); minD != 3 || maxD != 15 {
		t.Errorf("unknown provider range = %d-%d, want 3-15", minD, maxD)
	}
}

// 百炼的型号命名有两种形态，图生视频两种都得换对。
func TestDashScopeImageModelSwapsT2VForI2V(t *testing.T) {
	cases := map[string]string{
		"wan2.7-t2v":      "wan2.7-i2v",
		"wan2.6-t2v":      "wan2.6-i2v",
		"wan2.2-t2v-plus": "wan2.2-i2v-plus",
		"wan2.6-i2v":      "wan2.6-i2v", // 已经是 i2v，不能被改坏
	}
	for in, want := range cases {
		if got := dashScopeImageModel(in); got != want {
			t.Errorf("dashScopeImageModel(%q) = %q, want %q", in, got, want)
		}
	}
}

// captureDashScope 起一个假的百炼：记录提交体，轮询一次就返回成功。
func captureDashScope(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	fastMediaPoll(t)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/services/aigc/video-generation/video-synthesis",
			r.URL.Path == "/services/aigc/image2video/video-synthesis":
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"output":{"task_id":"ds1","task_status":"PENDING"}}`)
		case r.URL.Path == "/tasks/ds1":
			io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","video_url":"https://cdn.example/wan.mp4"}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

// Wan 2.7 用 resolution + ratio 表达规格，再发像素 size 会被 422 打回。
func TestDashScopeWan27UsesResolutionAndRatio(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"", "wan2.7-t2v", 12, "4:3", "1080p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	params, _ := (*bodyPtr)["parameters"].(map[string]any)
	if params["resolution"] != "1080P" {
		t.Errorf("resolution = %v, want 1080P", params["resolution"])
	}
	if params["ratio"] != "4:3" {
		t.Errorf("ratio = %v, want 4:3", params["ratio"])
	}
	if _, ok := params["size"]; ok {
		t.Errorf("wan2.7 must not send pixel size, got %v", params["size"])
	}
	if params["duration"] != float64(12) {
		t.Errorf("duration = %v, want 12", params["duration"])
	}
}

// Wan 2.6 及更早反过来：只认像素 size，不能带 ratio。
func TestDashScopeWan26UsesSizeOnly(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"", "wan2.6-t2v", 8, "16:9", "720p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	params, _ := (*bodyPtr)["parameters"].(map[string]any)
	if params["size"] != "1280*720" {
		t.Errorf("size = %v, want 1280*720", params["size"])
	}
	if _, ok := params["ratio"]; ok {
		t.Errorf("wan2.6 must not send ratio, got %v", params["ratio"])
	}
}

// 老款 wan2.2 固定 5 秒，带 duration 会被拒。
func TestDashScopeFixedDurationModelOmitsDuration(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"", "wan2.2-t2v-plus", 5, "16:9", "720p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	params, _ := (*bodyPtr)["parameters"].(map[string]any)
	if _, ok := params["duration"]; ok {
		t.Errorf("fixed-duration model must not send duration, got %v", params["duration"])
	}
}

// 首帧图必须把 t2v 型号换成 i2v，否则请求带着文生视频模型去图生视频端点。
func TestDashScopeImageToVideoSwapsModel(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"https://example.com/f.png", "wan2.7-t2v", 5, "16:9", "720p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	if (*bodyPtr)["model"] != "wan2.7-i2v" {
		t.Errorf("model = %v, want wan2.7-i2v", (*bodyPtr)["model"])
	}
}

// 语音的默认模型 / 音色只允许有一个来源（internal/voice）：catalog 必须引用它。
// 抄成字符串的那一刻就埋下了"设置页写 A、请求发 B"的分叉——真发生过一次
// （catalog gpt-4o-mini-tts，voice 回落 tts-1）。
func TestSpeechDefaultsComeFromVoicePackage(t *testing.T) {
	for _, name := range []string{"openai", "elevenlabs", "whisper"} {
		info, ok := ProviderInfo(MediaKindSpeech, name)
		if !ok {
			t.Fatalf("catalog is missing speech provider %q", name)
		}
		if want := voice.DefaultModelFor(name); info.DefaultModel != want {
			t.Errorf("%s default_model = %q, want %q (voice.DefaultModelFor)", name, info.DefaultModel, want)
		}
		if want := voice.DefaultVoiceFor(name); info.DefaultVoice != want {
			t.Errorf("%s default_voice = %q, want %q (voice.DefaultVoiceFor)", name, info.DefaultVoice, want)
		}
		if info.DefaultModel == "" {
			continue
		}
		// 默认值必须在可选清单里，否则设置页选中后再也回不到默认。
		found := false
		for _, m := range info.Models {
			if m == info.DefaultModel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s models %v does not include default %q", name, info.Models, info.DefaultModel)
		}
	}
}

// whisper 是本地 CLI：不能出现在需要 API key 的形态里（有 base_url 或 key 提示），
// 否则用户填了 key 却仍在等一个本机二进制。
func TestLocalWhisperIsNotAdvertisedAsHostedAPI(t *testing.T) {
	info, ok := ProviderInfo(MediaKindSpeech, "whisper")
	if !ok {
		t.Fatal("catalog is missing speech provider whisper")
	}
	if info.DefaultBaseURL != "" || info.KeyHint != "" || info.ConsoleURL != "" {
		t.Errorf("whisper looks like a hosted API: base_url=%q key_hint=%q console_url=%q",
			info.DefaultBaseURL, info.KeyHint, info.ConsoleURL)
	}
	if info.Supports(CapTextToSpeech) {
		t.Error("whisper must not advertise TTS")
	}
}

// 随语言变默认音色的厂商（azure / google）不能在 catalog 里写死 DefaultVoice：
// 写死值就是"language 填 en、占位却显示中文音色"的来源，也会让设置页把音色
// 预填成那一刻的语言。这两个字段必须成对出现。
func TestLanguageDependentVoiceHasNoFixedDefault(t *testing.T) {
	for _, name := range []string{"azure", "google"} {
		info, ok := ProviderInfo(MediaKindSpeech, name)
		if !ok {
			t.Fatalf("catalog is missing speech provider %q", name)
		}
		if !info.VoiceFollowsLanguage {
			t.Errorf("%s must set VoiceFollowsLanguage (its default voice depends on language)", name)
		}
		if info.DefaultVoice != "" {
			t.Errorf("%s has a fixed default_voice %q; the snapshot derives it from language instead",
				name, info.DefaultVoice)
		}
	}
}
