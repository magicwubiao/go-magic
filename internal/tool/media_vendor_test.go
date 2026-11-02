package tool

// media_vendor_test.go — 新接入厂商的后端契约测试。
//
// 这批测试的共同目标：把"厂商 API 的真实形状"钉死在测试里，而不是钉在注释里。
// 注释会过期、被人跳过，测试不会。每个用例都用一个 httptest 假服务端断言
// **请求路径、请求体结构、鉴权头**，因为这三处正是各家差异最大、最容易
// 照抄旧文档写错的地方（Kling 模型名在路径里、Runway 要日期版本头、
// MiniMax 图生视频 ratio 必须 adaptive）。

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// fastMediaPoll 把轮询间隔压到毫秒级。
//
// 不这么做的话，每个厂商用例要真的等 5~10 秒，三家加起来就是二十多秒——
// 测试套件的时长会随"接入了几家厂商"线性增长，最后没人愿意跑。
func fastMediaPoll(t *testing.T) {
	t.Helper()
	prevK, prevR, prevM, prevD := klingPollInterval, runwayPollInterval, minimaxPollInterval, dashscopePollInterval
	klingPollInterval = time.Millisecond
	runwayPollInterval = time.Millisecond
	minimaxPollInterval = time.Millisecond
	dashscopePollInterval = time.Millisecond
	t.Cleanup(func() {
		klingPollInterval, runwayPollInterval, minimaxPollInterval = prevK, prevR, prevM
		dashscopePollInterval = prevD
	})
}

// ---- 可灵 ----

func TestKlingTextToVideoPathBodyAndClamping(t *testing.T) {
	fastMediaPoll(t)

	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	polls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/text-to-video/"):
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode submit body: %v", err)
			}
			io.WriteString(w, `{"code":0,"message":"ok","data":{"id":"t1","status":"submitted"}}`)
		case r.URL.Path == "/tasks":
			polls++
			if got := r.URL.Query().Get("task_ids"); got != "t1" {
				t.Errorf("task_ids = %q, want t1", got)
			}
			io.WriteString(w, `{"code":0,"data":[{"id":"t1","status":"succeeded",
				"outputs":[{"type":"video","url":"https://cdn.example/v.mp4"}]}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "kling-key", BaseURL: srv.URL, Timeout: 5 * time.Second}
	res, err := klingVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{
		Prompt: "a cat", Duration: 20, Ratio: "4:3", Resolution: "480p",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// 模型名必须在路径里：Kling 新版不是 body 传 model。
	if gotPath != "/text-to-video/kling-3.0" {
		t.Errorf("submit path = %q, want /text-to-video/kling-3.0", gotPath)
	}
	if gotAuth != "Bearer kling-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}

	settings, _ := gotBody["settings"].(map[string]any)
	if settings["duration"] != float64(15) {
		t.Errorf("duration = %v, want 15 (clamped from 20)", settings["duration"])
	}
	if settings["resolution"] != "720p" {
		t.Errorf("resolution = %v, want 720p (Kling has no 480p)", settings["resolution"])
	}
	if settings["aspect_ratio"] != "16:9" {
		t.Errorf("aspect_ratio = %v, want 16:9 (4:3 unsupported, falls back)", settings["aspect_ratio"])
	}

	// 回显必须是**实际生效**的值，不是用户输入。
	if res.Duration != 15 || res.Ratio != "16:9" || res.Resolution != "720p" {
		t.Errorf("result echo = %+v, want duration 15 / ratio 16:9 / resolution 720p", res)
	}
	if res.URL != "https://cdn.example/v.mp4" || res.TaskID != "t1" {
		t.Errorf("result = %+v", res)
	}
	if polls == 0 {
		t.Error("expected at least one poll")
	}
}

// Kling 也吃本地图，但必须是**裸** base64：官方明确写了带 data: 前缀会解析失败。
func TestKlingImageToVideoSendsRawBase64(t *testing.T) {
	fastMediaPoll(t)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/image-to-video/"):
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"code":0,"message":"ok","data":{"id":"k1","status":"submitted"}}`)
		case r.URL.Path == "/tasks":
			io.WriteString(w, `{"code":0,"data":[{"id":"k1","status":"succeeded",
				"outputs":[{"type":"video","url":"https://cdn.example/k.mp4"}]}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	frame := filepath.Join(t.TempDir(), "frame.png")
	if err := os.WriteFile(frame, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &VideoGenConfig{APIKey: "k", BaseURL: srv.URL, Timeout: 5 * time.Second}
	if _, err := (klingVideoBackend{}).Generate(context.Background(), cfg, VideoRequest{
		Prompt: "x", ImagePath: frame, Duration: 5, Resolution: "720p",
	}); err != nil {
		t.Fatalf("Generate with local frame: %v", err)
	}

	contents, _ := body["contents"].([]any)
	if len(contents) != 2 {
		t.Fatalf("contents length = %d, want 2", len(contents))
	}
	frameItem, _ := contents[1].(map[string]any)
	ref, _ := frameItem["url"].(string)
	if strings.HasPrefix(ref, "data:") {
		t.Error("kling must receive the raw base64 string, without a data: prefix")
	}
	if ref != "cG5nLWJ5dGVz" { // base64("png-bytes")
		t.Errorf("first frame = %q, want base64 of the file", ref)
	}
}

// 不支持的格式必须在本地就拒绝，别等厂商回一个看不懂的 400。
func TestKlingRejectsUnsupportedFrameFormat(t *testing.T) {
	frame := filepath.Join(t.TempDir(), "anim.gif")
	if err := os.WriteFile(frame, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &VideoGenConfig{APIKey: "k", BaseURL: "https://api.example", Timeout: time.Second}
	_, err := klingVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x", ImagePath: frame})
	if err == nil || !strings.Contains(err.Error(), ".png") {
		t.Errorf("unsupported format should list accepted formats, got: %v", err)
	}
}

// HTTP 200 + code!=0 是 Kling 的常见失败形态，只看状态码会把失败当成功。
func TestKlingBusinessCodeFailsDespiteHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":1000,"message":"Authorization failed"}`)
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "bad", BaseURL: srv.URL, Timeout: 5 * time.Second}
	_, err := klingVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error for code!=0")
	}
	if !strings.Contains(err.Error(), "code=1000") || !strings.Contains(err.Error(), "Authorization failed") {
		t.Errorf("error should carry the provider code and message, got: %v", err)
	}
}

// ---- Runway ----

func TestRunwaySnapsDurationAndMapsRatio(t *testing.T) {
	fastMediaPoll(t)

	var body map[string]any
	var version, auth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/text_to_video":
			version = r.Header.Get("X-Runway-Version")
			auth = r.Header.Get("Authorization")
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"id":"task_r1"}`)
		case r.URL.Path == "/v1/tasks/task_r1":
			io.WriteString(w, `{"status":"SUCCEEDED","output":["https://cdn.example/r.mp4"]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "rw", BaseURL: srv.URL, Timeout: 5 * time.Second}
	res, err := runwayVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{
		Prompt: "x", Duration: 8, Ratio: "16:9", Resolution: "1080p",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// 版本头是日期字符串，缺了会 400；这是最容易抄错的一个字段。
	if version != runwayAPIVersion {
		t.Errorf("X-Runway-Version = %q, want %q", version, runwayAPIVersion)
	}
	if auth != "Bearer rw" {
		t.Errorf("Authorization = %q", auth)
	}
	if body["duration"] != float64(10) {
		t.Errorf("duration = %v, want 10 (snapped from 8)", body["duration"])
	}
	// ratio 是像素比，不是 "16:9"。
	if body["ratio"] != "1920:1080" {
		t.Errorf("ratio = %v, want 1920:1080", body["ratio"])
	}
	if res.Duration != 10 || res.Ratio != "1920:1080" {
		t.Errorf("result echo = %+v", res)
	}
}

func TestRunwayImageToVideoSendsDataURI(t *testing.T) {
	fastMediaPoll(t)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/image_to_video":
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"id":"task_i1"}`)
		case r.URL.Path == "/v1/tasks/task_i1":
			io.WriteString(w, `{"status":"SUCCEEDED","output":["https://cdn.example/i.mp4"]}`)
		}
	}))
	defer srv.Close()

	src := filepath.Join(t.TempDir(), "frame.png")
	if err := os.WriteFile(src, []byte("PNGDATA"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &VideoGenConfig{APIKey: "rw", BaseURL: srv.URL, Timeout: 5 * time.Second}
	if _, err := (runwayVideoBackend{}).Generate(context.Background(), cfg, VideoRequest{
		Prompt: "x", ImagePath: src, Duration: 5, Ratio: "9:16", Resolution: "720p",
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	promptImage, _ := body["promptImage"].(string)
	if !strings.HasPrefix(promptImage, "data:image/png;base64,") {
		t.Errorf("promptImage should be a data URI, got %q", promptImage)
	}
	if body["ratio"] != "720:1280" {
		t.Errorf("ratio = %v, want 720:1280", body["ratio"])
	}
}

func TestRunwayRejectsOversizeFirstFrame(t *testing.T) {
	src := filepath.Join(t.TempDir(), "huge.png")
	// 只写稀疏文件头不够：这里需要一个真的超过 5MB 的文件判断分支。
	if err := os.WriteFile(src, make([]byte, runwayMaxDataURIBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := runwayFirstFrameDataURI(src, "Runway")
	if err == nil {
		t.Fatal("expected an error for an oversize image")
	}
	if !strings.Contains(err.Error(), "image_url") {
		t.Errorf("error should suggest using image_url instead, got: %v", err)
	}
}

// ---- 阿里云百炼 ----

// 百炼的 img_url 也吃 base64 data URI：本地图不该逼用户先传 OSS。
func TestDashScopeImageToVideoSendsDataURI(t *testing.T) {
	fastMediaPoll(t)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/services/aigc/image2video/video-synthesis"):
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"output":{"task_id":"ds1","task_status":"PENDING"}}`)
		case r.URL.Path == "/tasks/ds1":
			io.WriteString(w, `{"output":{"task_status":"SUCCEEDED","video_url":"https://cdn.example/wan.mp4"}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	frame := filepath.Join(t.TempDir(), "frame.png")
	if err := os.WriteFile(frame, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &VideoGenConfig{APIKey: "sk", BaseURL: srv.URL, Timeout: 5 * time.Second}
	res, err := dashscopeVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{
		Prompt: "x", ImagePath: frame, Duration: 5, Ratio: "16:9", Resolution: "720p",
	})
	if err != nil {
		t.Fatalf("Generate with local frame: %v", err)
	}
	if res.URL != "https://cdn.example/wan.mp4" {
		t.Errorf("result url = %q", res.URL)
	}

	input, _ := body["input"].(map[string]any)
	imgURL, _ := input["img_url"].(string)
	if !strings.HasPrefix(imgURL, "data:image/png;base64,") {
		t.Errorf("img_url = %.60q, want a png data URI", imgURL)
	}
}

// ---- MiniMax ----

func TestMinimaxVideoUsesAdaptiveRatioForFirstFrame(t *testing.T) {
	fastMediaPoll(t)

	var body map[string]any
	var queryPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/video_generation":
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"task_id":"mm1"}`)
		case strings.HasPrefix(r.URL.Path, "/v2/query/video_generation/"):
			queryPath = r.URL.Path
			io.WriteString(w, `{"task":{"id":"mm1","status":"succeeded",
				"content":{"url":"https://cdn.example/mm.mp4"}}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "mm", BaseURL: srv.URL, Timeout: 5 * time.Second}
	res, err := minimaxVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{
		Prompt: "x", ImageURL: "https://example.com/a.png", Duration: 5, Ratio: "16:9", Resolution: "1080p",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	content, _ := body["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content length = %d, want 2 (text + first_frame)", len(content))
	}
	second, _ := content[1].(map[string]any)
	if second["role"] != "first_frame" {
		t.Errorf("role = %v, want first_frame", second["role"])
	}
	// 图生视频时必须 adaptive：画幅由首帧图决定，传 16:9 会被拒。
	if body["ratio"] != "adaptive" {
		t.Errorf("ratio = %v, want adaptive", body["ratio"])
	}
	if body["resolution"] != "2K" {
		t.Errorf("resolution = %v, want 2K (mapped from 1080p)", body["resolution"])
	}
	if queryPath != "/v2/query/video_generation/mm1" {
		t.Errorf("query path = %q", queryPath)
	}
	if res.URL != "https://cdn.example/mm.mp4" || res.Ratio != "adaptive" {
		t.Errorf("result = %+v", res)
	}
}

// 本地首帧图必须能直接用：MiniMax v2 的 image_url 支持 base64 data URI，
// 逼用户先把图传到图床是没必要的门槛（曾经这里直接 400 劝模型换 provider）。
func TestMinimaxImageToVideoSendsDataURI(t *testing.T) {
	fastMediaPoll(t)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/video_generation":
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"task_id":"mm3"}`)
		case strings.HasPrefix(r.URL.Path, "/v2/query/video_generation/"):
			io.WriteString(w, `{"task":{"id":"mm3","status":"succeeded",
				"content":{"url":"https://cdn.example/mm.mp4"}}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	frame := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(frame, []byte("fake-jpeg-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &VideoGenConfig{APIKey: "mm", BaseURL: srv.URL, Timeout: 5 * time.Second}
	if _, err := (minimaxVideoBackend{}).Generate(context.Background(), cfg, VideoRequest{
		Prompt: "x", ImagePath: frame, Duration: 5, Resolution: "768p",
	}); err != nil {
		t.Fatalf("Generate with local frame: %v", err)
	}

	content, _ := body["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content length = %d, want 2 (text + first_frame)", len(content))
	}
	second, _ := content[1].(map[string]any)
	imageURL, _ := second["image_url"].(map[string]any)
	ref, _ := imageURL["url"].(string)
	// jpg 必须报 image/jpeg：报 image/jpg 会被厂商当成未知格式。
	if !strings.HasPrefix(ref, "data:image/jpeg;base64,") {
		t.Errorf("first frame ref = %.60q, want a jpeg data URI", ref)
	}
	if body["ratio"] != "adaptive" {
		t.Errorf("ratio = %v, want adaptive", body["ratio"])
	}
}

// 超限/不支持的格式要在本地直接拒绝，别把 30MB 的 base64 发出去再等厂商报错。
func TestMinimaxRejectsOversizeOrUnsupportedFrame(t *testing.T) {
	prev := minimaxMaxImageBytes
	minimaxMaxImageBytes = 8
	t.Cleanup(func() { minimaxMaxImageBytes = prev })

	cfg := &VideoGenConfig{APIKey: "mm", BaseURL: "https://api.example", Timeout: time.Second}
	dir := t.TempDir()

	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := minimaxVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x", ImagePath: big})
	if err == nil || !strings.Contains(err.Error(), "MB") {
		t.Errorf("oversize frame should be rejected with a size hint, got: %v", err)
	}

	gif := filepath.Join(dir, "anim.gif")
	if err := os.WriteFile(gif, []byte("GIF89a"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = minimaxVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x", ImagePath: gif})
	if err == nil || !strings.Contains(err.Error(), ".heic") {
		t.Errorf("unsupported format should list accepted formats, got: %v", err)
	}
}

func TestMinimaxVideoTerminalStateIsNotRetried(t *testing.T) {
	fastMediaPoll(t)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/video_generation" {
			io.WriteString(w, `{"task_id":"mm2"}`)
			return
		}
		calls++
		io.WriteString(w, `{"task":{"id":"mm2","status":"failed","error":{"message":"content moderated"}}}`)
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "mm", BaseURL: srv.URL, Timeout: 5 * time.Second}
	_, err := minimaxVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x", Duration: 5})
	if err == nil {
		t.Fatal("expected an error for a failed task")
	}
	if !strings.Contains(err.Error(), "content moderated") {
		t.Errorf("error should carry the reason, got: %v", err)
	}
	// 终态只查一次：失败的任务反复轮询只会白烧配额。
	if calls != 1 {
		t.Errorf("polled %d times on a terminal state, want 1", calls)
	}
}

func TestMinimaxImageMapsSizeToAspectRatio(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/image_generation" {
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"data":{"image_urls":["https://cdn.example/i1.png"]},"base_resp":{"status_code":0}}`)
	}))
	defer srv.Close()

	cfg := &ImageGenConfig{APIKey: "mm", BaseURL: srv.URL, Timeout: 5 * time.Second}
	res, err := minimaxImageBackend{}.Generate(context.Background(), cfg, ImageRequest{
		Prompt: "x", Size: "1280x720", Count: 2,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if body["aspect_ratio"] != "16:9" {
		t.Errorf("aspect_ratio = %v, want 16:9", body["aspect_ratio"])
	}
	if body["n"] != float64(2) {
		t.Errorf("n = %v, want 2", body["n"])
	}
	if body["model"] != "image-01" {
		t.Errorf("model = %v, want image-01 (catalog default)", body["model"])
	}
	if len(res.Images) != 1 || res.Images[0].URL == "" {
		t.Errorf("images = %+v", res.Images)
	}
}

// 配置里的 model 必须能覆盖 catalog 默认值，否则"设置页能选模型"是假的。
func TestMinimaxImageHonoursConfiguredModel(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"data":{"image_urls":["https://cdn.example/i.png"]},"base_resp":{"status_code":0}}`)
	}))
	defer srv.Close()

	cfg := &ImageGenConfig{APIKey: "mm", BaseURL: srv.URL, Model: "image-01-live", Timeout: 5 * time.Second}
	if _, err := (minimaxImageBackend{}).Generate(context.Background(), cfg, ImageRequest{Prompt: "x", Count: 1}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if body["model"] != "image-01-live" {
		t.Errorf("model = %v, want image-01-live", body["model"])
	}
}

// ---- CosyVoice ----

func TestCosyVoiceSynthesizesAndDownloads(t *testing.T) {
	var body map[string]any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/aigc/multimodal-generation/generation":
			if got := r.Header.Get("Authorization"); got != "Bearer dash-key" {
				t.Errorf("Authorization = %q", got)
			}
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"output":{"audio":{"url":"`+srv.URL+`/out.wav","id":"a1"}}}`)
		case "/out.wav":
			io.WriteString(w, "RIFFaudio")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &voice.VoiceConfig{APIKey: "dash-key", APIURL: srv.URL}
	audio, err := cosyVoiceBackend{}.Synthesize(context.Background(), cfg, SpeechRequest{
		Text: "你好", Format: "wav", Speed: 1.25,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	input, _ := body["input"].(map[string]any)
	if input["text"] != "你好" {
		t.Errorf("input.text = %v", input["text"])
	}
	// 声音必须成对出现：cosyvoice-v2 只认 longxiaochun_v2 这类带 _v2 的 ID。
	if input["voice"] != "longxiaochun_v2" {
		t.Errorf("input.voice = %v, want the catalog default for cosyvoice-v2", input["voice"])
	}
	if body["model"] != "cosyvoice-v2" {
		t.Errorf("model = %v", body["model"])
	}
	params, _ := body["parameters"].(map[string]any)
	if params["format"] != "wav" {
		t.Errorf("parameters.format = %v", params["format"])
	}
	if params["rate"] != 1.25 {
		t.Errorf("parameters.rate = %v, want 1.25", params["rate"])
	}

	// 产物是 URL 必须当场取回：24 小时后链接就失效了。
	if string(audio.Data) != "RIFFaudio" {
		t.Errorf("audio data = %q", string(audio.Data))
	}
	if audio.Format != "wav" || audio.Mime != "audio/wav" {
		t.Errorf("audio format/mime = %q/%q", audio.Format, audio.Mime)
	}
}

func TestCosyVoiceAvailabilityKeySources(t *testing.T) {
	t.Setenv(cosyVoiceEnvKey, "")

	if (cosyVoiceBackend{}).Available(nil) {
		t.Error("no key anywhere should mean unavailable")
	}
	if !(cosyVoiceBackend{}).Available(&voice.VoiceConfig{APIKey: "k"}) {
		t.Error("voice.api_key should be enough")
	}
	// 设置页按厂商填的凭据优先于通用 key。
	cfg := &voice.VoiceConfig{
		APIKey:    "generic",
		Providers: map[string]voice.ProviderCredentials{"cosyvoice": {APIKey: "per-provider"}},
	}
	if got := cosyVoiceAPIKey(cfg); got != "per-provider" {
		t.Errorf("key = %q, want per-provider", got)
	}

	// 部署场景只设环境变量也该能用。
	t.Setenv(cosyVoiceEnvKey, "env-key")
	if got := cosyVoiceAPIKey(nil); got != "env-key" {
		t.Errorf("key from env = %q", got)
	}
}

// ---- 交叉一致性 ----

// catalog 里列出的内置厂商必须有真后端，否则设置页会出现一个选了就报错的名字。
func TestCatalogEntriesHaveBackends(t *testing.T) {
	for _, info := range ProviderInfos(MediaKindImage) {
		if _, ok := LookupImageBackend(info.Name); !ok {
			t.Errorf("image provider %q is in the catalog but not registered", info.Name)
		}
	}
	for _, info := range ProviderInfos(MediaKindVideo) {
		if _, ok := LookupVideoBackend(info.Name); !ok {
			t.Errorf("video provider %q is in the catalog but not registered", info.Name)
		}
	}
	if _, ok := LookupTTSBackend("cosyvoice"); !ok {
		t.Error("cosyvoice is in the catalog but not registered as a TTS backend")
	}
}

// 别名解析：用户从文档抄来的名字（hailuo / seedance / dalle）必须能直接查到后端。
func TestProviderAliasesResolveToBackends(t *testing.T) {
	videoAliases := map[string]string{
		"hailuo": "minimax", "seedance": "volcengine", "wan": "dashscope", "runwayml": "runway",
	}
	for alias, want := range videoAliases {
		backend, ok := LookupVideoBackend(alias)
		if !ok {
			t.Errorf("video alias %q not resolvable", alias)
			continue
		}
		if backend.Name() != want {
			t.Errorf("video alias %q → %q, want %q", alias, backend.Name(), want)
		}
	}

	if backend, ok := LookupImageBackend("dalle"); !ok || backend.Name() != "dall-e" {
		t.Errorf("image alias dalle → %v (%v)", backend, ok)
	}
	if backend, ok := LookupTTSBackend("qwen-tts"); !ok || backend.Name() != "cosyvoice" {
		t.Errorf("speech alias qwen-tts → %v (%v)", backend, ok)
	}
}

// 默认模型的唯一真源是 catalog：请求里用的模型必须等于 catalog 的 DefaultModel，
// 除非配置显式覆盖。（历史上 catalog 写 FLUX、代码发 SDXL 就是这条没人守。）
func TestCatalogDefaultModelIsActuallyUsed(t *testing.T) {
	info, ok := ProviderInfo(MediaKindImage, "together")
	if !ok || info.DefaultModel == "" {
		t.Fatal("together should have a catalog default model")
	}
	if got := infoOrDefaultModel(MediaKindImage, "together", ""); got != info.DefaultModel {
		t.Errorf("default model = %q, want %q", got, info.DefaultModel)
	}
	if got := infoOrDefaultModel(MediaKindImage, "together", "custom/model"); got != "custom/model" {
		t.Errorf("configured model should win, got %q", got)
	}
}

// 401 的报错必须同时给出"哪家"和"去哪儿处理"，只写 HTTP 401 帮不上用户。
func TestMediaAPIErrorIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"invalid api key"}`)
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "bad", BaseURL: srv.URL, Timeout: 5 * time.Second}
	_, err := klingVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"Kling", "rejected the API key", "klingai.com/dev/api-key", "invalid api key"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
}

// 429 的指引与 401 不同：不是去换 key，而是等一等。
func TestMediaAPIErrorRateLimitGuidance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()

	cfg := &VideoGenConfig{APIKey: "k", BaseURL: srv.URL, Timeout: 5 * time.Second}
	_, err := runwayVideoBackend{}.Generate(context.Background(), cfg, VideoRequest{Prompt: "x", Duration: 5})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Wait a minute") {
		t.Errorf("rate-limit error should tell the user to retry later, got: %v", err)
	}
}

// ---- MiniMax 语音 ----

// T2A v2 的三个契约：GroupId 必须拼在 query 上、鉴权是 Bearer、
// data.audio 是十六进制字符串必须解码后交给内核。
func TestMinimaxSpeechSynthesizesHexAudio(t *testing.T) {
	wantBytes := []byte("ID3fake-mp3-bytes")
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/t2a_v2" {
			t.Errorf("path = %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("GroupId"); got != "grp_123" {
			t.Errorf("GroupId = %q, want grp_123", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mm-key" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"data":{"audio":"`+hex.EncodeToString(wantBytes)+`"},"base_resp":{"status_code":0}}`)
	}))
	defer srv.Close()

	cfg := &voice.VoiceConfig{
		Providers: map[string]voice.ProviderCredentials{
			"minimax": {APIKey: "mm-key", GroupID: "grp_123"},
		},
		APIURL: srv.URL,
	}
	audio, err := minimaxSpeechBackend{}.Synthesize(context.Background(), cfg, SpeechRequest{
		Text: "你好世界", Format: "mp3", Speed: 1.1, Pitch: -1,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if body["model"] != "speech-02-hd" {
		t.Errorf("model = %v, want catalog default speech-02-hd", body["model"])
	}
	vs, _ := body["voice_setting"].(map[string]any)
	if vs["voice_id"] != "male-qn-qingse" {
		t.Errorf("voice_id = %v, want catalog default", vs["voice_id"])
	}
	if got, want := vs["speed"], float64(1.1); got != want {
		t.Errorf("speed = %v, want %v", got, want)
	}
	if got, want := vs["pitch"], float64(-1); got != want {
		t.Errorf("pitch = %v, want %v", got, want)
	}
	as, _ := body["audio_setting"].(map[string]any)
	if as["format"] != "mp3" || as["sample_rate"] != float64(32000) {
		t.Errorf("audio_setting = %v", as)
	}

	// 十六进制必须解码：内核拿到的是真实音频字节。
	if string(audio.Data) != string(wantBytes) {
		t.Errorf("audio data = %q, want %q", audio.Data, wantBytes)
	}
	if audio.Format != "mp3" || audio.Mime != "audio/mpeg" {
		t.Errorf("format/mime = %q/%q", audio.Format, audio.Mime)
	}
}

// base_resp.status_code 非 0 必须报错并把 status_msg 带出来（MiniMax 的
// 失败都走这个字段，HTTP 状态码永远是 200）。
func TestMinimaxSpeechAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"base_resp":{"status_code":1004,"status_msg":"invalid voice id"}}`)
	}))
	defer srv.Close()

	cfg := &voice.VoiceConfig{
		Providers: map[string]voice.ProviderCredentials{"minimax": {APIKey: "k", GroupID: "g"}},
		APIURL:    srv.URL,
	}
	_, err := minimaxSpeechBackend{}.Synthesize(context.Background(), cfg, SpeechRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "1004") || !strings.Contains(err.Error(), "invalid voice id") {
		t.Fatalf("err = %v, want status_code 1004 with message", err)
	}
}

// 缺 GroupId 要在客户端侧被拦下，并告诉模型/用户去哪配——MiniMax 服务端
// 对缺 GroupId 的报错完全不提 GroupId，靠它自己纠不了错。
func TestMinimaxSpeechRequiresGroupID(t *testing.T) {
	t.Setenv(minimaxGroupEnvKey, "")

	cfg := &voice.VoiceConfig{APIKey: "k"}
	_, err := minimaxSpeechBackend{}.Synthesize(context.Background(), cfg, SpeechRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "GroupId") {
		t.Fatalf("err = %v, want a GroupId hint", err)
	}

	// 环境变量兜底也算配了。
	t.Setenv(minimaxGroupEnvKey, "env-grp")
	if got := minimaxSpeechGroupID(cfg); got != "env-grp" {
		t.Fatalf("group = %q, want env-grp", got)
	}
}

func TestMinimaxSpeechAvailabilityKeySources(t *testing.T) {
	t.Setenv(minimaxSpeechEnvKey, "")

	if (minimaxSpeechBackend{}).Available(nil) {
		t.Error("no key anywhere should mean unavailable")
	}
	if !(minimaxSpeechBackend{}).Available(&voice.VoiceConfig{APIKey: "k"}) {
		t.Error("voice.api_key should be enough")
	}
	cfg := &voice.VoiceConfig{
		APIKey:    "generic",
		Providers: map[string]voice.ProviderCredentials{"minimax": {APIKey: "per-provider"}},
	}
	if got := minimaxSpeechAPIKey(cfg); got != "per-provider" {
		t.Errorf("key = %q, want per-provider", got)
	}

	t.Setenv(minimaxSpeechEnvKey, "env-key")
	if got := minimaxSpeechAPIKey(nil); got != "env-key" {
		t.Errorf("env key = %q", got)
	}
}

// wav 不在同步接口的稳定支持列表里，应回落 mp3 而不是报错。
func TestMinimaxSpeechFormatFallback(t *testing.T) {
	if got := minimaxSpeechFormat("wav"); got != "mp3" {
		t.Errorf("wav -> %q, want mp3", got)
	}
	if got := minimaxSpeechFormat("flac"); got != "flac" {
		t.Errorf("flac -> %q, want flac", got)
	}
}
