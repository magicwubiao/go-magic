package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		ProviderDashScope:  {2, 30},
		"kling":            {3, 15},
		"runway":           {5, 10},
		// H3 是 4-15。曾经照抄上一代 Hailuo 的 5-10，把 4 秒和 11-15 秒砍掉。
		"minimax": {4, 15},
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
		// 日期快照不跟着换：i2v 的快照日期和 t2v 不同，照搬会拼出不存在的 ID。
		"wan2.7-t2v-2026-06-12": "wan2.7-i2v",
		"wan2.7-i2v-2026-04-25": "wan2.7-i2v-2026-04-25",
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
	for _, name := range []string{"openai", "elevenlabs"} {
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

// 本地 whisper CLI 已从内置清单移除：它没有可填的凭据（设置页永远显示"未配置"），
// 可用与否取决于本机是否装了命令，托管版 Whisper 走 provider openai 即可完全覆盖。
// 这条断言防止有人"顺手加回来"——加回来会重新出现在语音卡片列表里当噪音。
func TestLocalWhisperCLIIsNotAdvertised(t *testing.T) {
	if _, ok := ProviderInfo(MediaKindSpeech, "whisper"); ok {
		t.Error("catalog still advertises the local whisper CLI; use provider openai for hosted Whisper")
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

// 图形模型 ID 必须是能调用的全名：gpt-image-2.5 只是"2.5 代"的简称，
// 真实 ID 是 -flare / -sunburst。少写后缀 = 设置页能选、一调用 404。
func TestOpenAIImageModelsAreCallableIDs(t *testing.T) {
	info, ok := ProviderInfo(MediaKindImage, "dall-e")
	if !ok {
		t.Fatal("catalog is missing image provider dall-e")
	}
	for _, m := range info.Models {
		if m == "gpt-image-2.5" {
			t.Errorf("模型清单里出现了不可调用的简称 %q（应为 gpt-image-2.5-flare / -sunburst）", m)
		}
	}
	if strings.HasSuffix(info.DefaultModel, "gpt-image-2.5") {
		t.Errorf("default_model = %q 不是可调用的模型 ID", info.DefaultModel)
	}
}

// Wan 2.7 图生视频：共用 video-generation 端点，首帧走 input.media，且不能带 ratio。
func TestDashScopeWan27ImageToVideoUsesMediaArray(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"https://example.com/f.png", "wan2.7-t2v", 6, "16:9", "720p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	input, _ := (*bodyPtr)["input"].(map[string]any)
	media, _ := input["media"].([]any)
	if len(media) != 1 {
		t.Fatalf("input.media = %v, want one first_frame entry", input["media"])
	}
	item, _ := media[0].(map[string]any)
	if item["type"] != "first_frame" || item["url"] != "https://example.com/f.png" {
		t.Errorf("media[0] = %v, want first_frame + url", item)
	}
	if _, ok := input["img_url"]; ok {
		t.Errorf("wan2.7 must not send input.img_url, got %v", input["img_url"])
	}
	params, _ := (*bodyPtr)["parameters"].(map[string]any)
	if _, ok := params["ratio"]; ok {
		t.Errorf("wan2.7 image-to-video takes no ratio, got %v", params["ratio"])
	}
}

// Wan 2.6 图生视频还是 img_url + resolution，但它和文生视频共用端点
// （旧的 image2video 端点只属于 wan2.2 及更早）。
func TestDashScopeWan26ImageToVideoUsesImgURL(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"https://example.com/f.png", "wan2.6-t2v", 8, "16:9", "720p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	input, _ := (*bodyPtr)["input"].(map[string]any)
	if input["img_url"] != "https://example.com/f.png" {
		t.Errorf("input.img_url = %v, want the first frame", input["img_url"])
	}
	if _, ok := input["media"]; ok {
		t.Errorf("wan2.6 must not send input.media, got %v", input["media"])
	}
	params, _ := (*bodyPtr)["parameters"].(map[string]any)
	if params["resolution"] != "720P" {
		t.Errorf("resolution = %v, want 720P", params["resolution"])
	}
}

// 480p 是配置里的历史值，wan2.7 没有这一档：必须向上取 720P 而不是原样发。
func TestDashScopeWan27Snaps480pUp(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"", "wan2.7-t2v", 5, "16:9", "480p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}
	params, _ := (*bodyPtr)["parameters"].(map[string]any)
	if params["resolution"] != "720P" {
		t.Errorf("resolution = %v, want 720P (2.7 has no 480P tier)", params["resolution"])
	}
}

// 方舟首帧任务：ratio 必须 adaptive，图片项必须标 role=first_frame。
func TestVolcengineImageToVideoUsesAdaptiveRatio(t *testing.T) {
	origPoll := volcenginePollInterval
	t.Cleanup(func() { volcenginePollInterval = origPoll })
	volcenginePollInterval = time.Millisecond

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"id":"ark1"}`)
		default:
			io.WriteString(w, `{"status":"succeeded","content":{"video_url":"https://cdn.example/a.mp4"}}`)
		}
	}))
	t.Cleanup(srv.Close)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "ak", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runVolcengine(context.Background(), "x",
		"https://example.com/f.png", "doubao-seedance-2-5-260628", 6, "16:9", "720p"); err != nil {
		t.Fatalf("runVolcengine: %v", err)
	}

	content, _ := body["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %v, want text + first_frame", content)
	}
	item, _ := content[1].(map[string]any)
	if item["role"] != "first_frame" {
		t.Errorf("image role = %v, want first_frame", item["role"])
	}
	textItem, _ := content[0].(map[string]any)
	if text, _ := textItem["text"].(string); !strings.Contains(text, "--ratio adaptive") {
		t.Errorf("text flags = %q, want --ratio adaptive for a first-frame task", text)
	}
}

// 同一家的旧型号能力更小：只按厂商上限发，会带着 20 秒去请求一个 15 秒封顶的模型。
func TestDurationForModelNarrowsByModel(t *testing.T) {
	cases := []struct {
		provider VideoProvider
		model    string
		in, want int
	}{
		{ProviderVolcengine, "doubao-seedance-2-5-260628", 25, 25},
		{ProviderVolcengine, "doubao-seedance-2-0-260128", 25, 15},
		{"kling", "kling-3.0", 12, 12},
		{"kling", "kling-2.6", 12, 10},
		{"kling", "kling-2.5-turbo", 3, 5},
		{"minimax", "MiniMax-H3", 4, 4},
		{"minimax", "MiniMax-H3-Max", 4, 5},
		// 百炼的上限按 wan3.0 标（30 秒），2.7 只到 15。
		{ProviderDashScope, "wan3.0-video", 25, 25},
		{ProviderDashScope, "wan2.7-t2v", 25, 15},
	}
	for _, c := range cases {
		if got := durationForModel(c.provider, c.model, c.in); got != c.want {
			t.Errorf("durationForModel(%q, %q, %d) = %d, want %d",
				c.provider, c.model, c.in, got, c.want)
		}
	}
}

// 配置里 model 为空是常态（设置页只给占位）。若原样发 "model": ""，用户会拿到
// 一个 400 而完全看不出是"没填模型"——必须回落到 catalog 的推荐值。
func TestDashScopeEmptyModelFallsBackToCatalog(t *testing.T) {
	srv, bodyPtr := captureDashScope(t)

	tool := &VideoGenerationTool{config: &VideoGenConfig{
		APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second,
	}}
	if _, _, err := tool.runDashScope(context.Background(), "x", "",
		"", "", 5, "16:9", "720p"); err != nil {
		t.Fatalf("runDashScope: %v", err)
	}

	want := "wan2.7-t2v"
	if info, ok := ProviderInfo(MediaKindVideo, "dashscope"); ok {
		want = info.DefaultModel
	}
	if got := (*bodyPtr)["model"]; got != want {
		t.Errorf("model = %v, want catalog default %s", got, want)
	}
}

// Wan 3.0 虽然和 2.7 共用端点，但多 480P 和 21:9；2.7 发这两个值会被拒。
func TestDashScopeEffectivePerGeneration(t *testing.T) {
	cases := []struct {
		model, ratio, res, wantRatio, wantRes string
	}{
		{"wan3.0-video", "21:9", "480p", "21:9", "480p"},
		{"wan2.7-t2v", "21:9", "480p", "16:9", "720p"}, // 2.7 没有 21:9 / 480P
		{"wan2.7-t2v", "4:3", "1080p", "4:3", "1080p"},
		{"wan2.6-t2v", "4:3", "480p", "16:9", "480p"}, // 2.6 有 480P 但没有 4:3
	}
	for _, c := range cases {
		gotRatio, gotRes := dashScopeEffective(c.model, c.ratio, c.res, false)
		if gotRatio != c.wantRatio || gotRes != c.wantRes {
			t.Errorf("dashScopeEffective(%q,%q,%q) = %q/%q, want %q/%q",
				c.model, c.ratio, c.res, gotRatio, gotRes, c.wantRatio, c.wantRes)
		}
	}
}
