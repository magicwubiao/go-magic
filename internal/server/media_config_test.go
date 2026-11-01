package server

// media_config_test.go — 媒体设置接口的契约测试。
//
// 最该锁的是两条语义：
//  1. GET 永不回传明文 key（只给掩码），否则"看设置页"等于把密钥贴到屏幕上；
//  2. PUT 里 api_key 缺省 = 不改。前端只切 provider 时不该把已配好的 key 洗掉。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/internal/voice"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

func newMediaTestServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	return &Server{cfg: &appconfig.Config{}, magicHome: home}
}

func doMediaRequest(t *testing.T, s *Server, method, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/api/media/config", nil)
	} else {
		req = httptest.NewRequest(method, "/api/media/config", strings.NewReader(body))
	}
	rr := httptest.NewRecorder()
	s.handleMediaConfig(rr, req)

	var decoded map[string]interface{}
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &decoded)
	}
	return rr, decoded
}

func TestMediaConfigGetMasksKey(t *testing.T) {
	s := newMediaTestServer(t)
	s.cfg.ImageGen = &appconfig.ImageGenConfig{APIKey: "sk-super-secret-1234", Provider: "dall-e"}
	s.cfg.VideoGen = &appconfig.VideoGenConfig{APIKey: "vk-abcdefgh", Provider: "dashscope"}
	s.cfg.Voice = nil

	rr, body := doMediaRequest(t, s, http.MethodGet, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	raw := rr.Body.String()
	if strings.Contains(raw, "sk-super-secret-1234") || strings.Contains(raw, "vk-abcdefgh") {
		t.Fatal("GET must never return the plaintext key")
	}
	for _, section := range []string{"image_gen", "video_gen", "voice"} {
		if _, ok := body[section].(map[string]interface{}); !ok {
			t.Errorf("response missing %q section", section)
		}
	}

	img := body["image_gen"].(map[string]interface{})
	if img["api_key_set"] != true {
		t.Error("api_key_set should be true")
	}
	if hint, _ := img["api_key_hint"].(string); !strings.HasSuffix(hint, "1234") {
		t.Errorf("hint should expose the last 4 chars only, got %q", hint)
	}
	providers, _ := img["providers"].([]interface{})
	if len(providers) == 0 {
		t.Fatal("providers list must not be empty (builtin backends are always registered)")
	}
	if path, _ := body["config_path"].(string); path == "" {
		t.Error("config_path should be reported so users can find the file")
	}
}

func TestMediaConfigPutSavesAndHotApplies(t *testing.T) {
	s := newMediaTestServer(t)

	rr, body := doMediaRequest(t, s, http.MethodPut, `{
		"image_gen": {"api_key": "sk-new-key-abcd", "provider": "dall-e", "default_size": "512x512"},
		"video_gen": {"provider": "dashscope", "default_ratio": "9:16", "default_duration": 5},
		"voice": {"provider": "openai", "api_key": "vk-voice-9999", "language": "zh", "speed": 1.2}
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	// 内存配置
	if s.cfg.ImageGen == nil || s.cfg.ImageGen.APIKey != "sk-new-key-abcd" {
		t.Errorf("image_gen not applied: %+v", s.cfg.ImageGen)
	}
	if s.cfg.VideoGen == nil || s.cfg.VideoGen.DefaultRatio != "9:16" || s.cfg.VideoGen.DefaultDuration != 5 {
		t.Errorf("video_gen not applied: %+v", s.cfg.VideoGen)
	}
	if s.cfg.Voice == nil || s.cfg.Voice.Provider != "openai" || s.cfg.Voice.Speed != 1.2 {
		t.Errorf("voice not applied: %+v", s.cfg.Voice)
	}

	// 落盘：重启后必须还在
	data, err := os.ReadFile(filepath.Join(s.magicHome, "config.json"))
	if err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
	if !strings.Contains(string(data), "sk-new-key-abcd") {
		t.Error("api_key was not persisted")
	}

	// 响应同样脱敏
	if strings.Contains(rr.Body.String(), "sk-new-key-abcd") {
		t.Error("PUT response must not echo the plaintext key")
	}
	if img, _ := body["image_gen"].(map[string]interface{}); img["api_key_set"] != true {
		t.Error("response should report the key as set")
	}
}

// 只改 provider 时，已配好的 key 必须保留（缺省 != 清空）。
func TestMediaConfigPutKeepsKeyWhenOmitted(t *testing.T) {
	s := newMediaTestServer(t)
	s.cfg.ImageGen = &appconfig.ImageGenConfig{APIKey: "sk-keep-me", Provider: "dall-e"}

	rr, _ := doMediaRequest(t, s, http.MethodPut, `{"image_gen": {"provider": "together"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.ImageGen.APIKey != "sk-keep-me" {
		t.Errorf("api_key was dropped: %q", s.cfg.ImageGen.APIKey)
	}
	if s.cfg.ImageGen.Provider != "together" {
		t.Errorf("provider not updated: %q", s.cfg.ImageGen.Provider)
	}

	// 显式空串才是清除
	rr, _ = doMediaRequest(t, s, http.MethodPut, `{"image_gen": {"api_key": ""}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if s.cfg.ImageGen.APIKey != "" {
		t.Errorf("explicit empty api_key should clear it, got %q", s.cfg.ImageGen.APIKey)
	}
}

func TestMediaConfigPutRejectsUnknownProvider(t *testing.T) {
	s := newMediaTestServer(t)

	rr, body := doMediaRequest(t, s, http.MethodPut, `{"image_gen": {"provider": "kling-typo"}}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "kling-typo") || !strings.Contains(msg, "dall-e") {
		t.Errorf("error should name the bad value and list alternatives, got %q", msg)
	}

	// 非法枚举同样拒绝：这些值最终会被拼进厂商请求
	if rr, _ := doMediaRequest(t, s, http.MethodPut, `{"video_gen": {"default_ratio": "4:3"}}`); rr.Code != http.StatusBadRequest {
		t.Errorf("bad ratio should be rejected, got %d", rr.Code)
	}
	if rr, _ := doMediaRequest(t, s, http.MethodPut, `{"video_gen": {"default_duration": 99}}`); rr.Code != http.StatusBadRequest {
		t.Errorf("out-of-range duration should be rejected, got %d", rr.Code)
	}
	if rr, _ := doMediaRequest(t, s, http.MethodPut, `{"voice": {"speed": 5}}`); rr.Code != http.StatusBadRequest {
		t.Errorf("out-of-range speed should be rejected, got %d", rr.Code)
	}
}

// 插件贡献的语音后端也要能被写进 voice.provider（否则装了插件也没法切）。
func TestMediaConfigPutAcceptsPluginVoiceProvider(t *testing.T) {
	s := newMediaTestServer(t)
	registerFakeSpeechProvider(t, "cosyvoice")

	rr, _ := doMediaRequest(t, s, http.MethodPut, `{"voice": {"provider": "cosyvoice"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("plugin provider should be accepted, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---- 测试替身 ----

type fakeSpeechProvider struct{ name string }

func (f fakeSpeechProvider) Name() string                      { return f.name }
func (f fakeSpeechProvider) Available(*voice.VoiceConfig) bool { return true }
func (f fakeSpeechProvider) Synthesize(context.Context, *voice.VoiceConfig, tool.SpeechRequest) (*tool.SpeechAudio, error) {
	return &tool.SpeechAudio{Data: []byte("audio")}, nil
}

func registerFakeSpeechProvider(t *testing.T, name string) {
	t.Helper()
	tool.RegisterSpeechBackend(fakeSpeechProvider{name: name})
	t.Cleanup(func() { tool.UnregisterSpeechBackend(name) })
}

// 保存后必须真的改到"正在运行的工具实例"上，而不只是 s.cfg。
// 这是"面板里改了、对话里还是旧后端"的根因，值得一条端到端断言。
func TestMediaConfigPutHotAppliesToLiveTools(t *testing.T) {
	home := t.TempDir()
	reg := tool.NewRegistry()
	reg.Register(tool.NewImageGenerationTool(tool.DefaultImageGenConfig()))
	reg.Register(tool.NewImageEditTool(tool.DefaultImageGenConfig()))
	reg.Register(tool.NewVideoGenerationTool(tool.DefaultVideoGenConfig()))

	s := &Server{cfg: &appconfig.Config{}, magicHome: home, toolReg: reg}

	rr, _ := doMediaRequest(t, s, http.MethodPut, `{
		"image_gen": {"api_key": "sk-live-1", "provider": "together"},
		"video_gen": {"api_key": "vk-live-2", "provider": "volcengine"}
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	if raw, err := reg.Get("image_gen"); err == nil {
		img := raw.(*tool.ImageGenerationTool).GetConfig()
		if img.APIKey != "sk-live-1" || string(img.Provider) != "together" {
			t.Errorf("image_gen tool still holds the old config: %+v", img)
		}
	} else {
		t.Fatalf("image_gen not registered: %v", err)
	}

	if raw, err := reg.Get("image_edit"); err == nil {
		// image_edit 与 image_gen 共用一段配置，必须一起更新
		edit := raw.(*tool.ImageEditTool)
		if got := edit.GetConfig().APIKey; got != "sk-live-1" {
			t.Errorf("image_edit tool not refreshed: %q", got)
		}
		if string(edit.GetConfig().Provider) != "together" {
			t.Errorf("image_edit provider not refreshed: %q", edit.GetConfig().Provider)
		}
	} else {
		t.Fatalf("image_edit not registered: %v", err)
	}

	if raw, err := reg.Get("video_generate"); err == nil {
		vid := raw.(*tool.VideoGenerationTool).GetConfig()
		if vid.APIKey != "vk-live-2" || string(vid.Provider) != "volcengine" {
			t.Errorf("video_generate tool still holds the old config: %+v", vid)
		}
	}
}
