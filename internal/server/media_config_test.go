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

// 清除整段：只清指定段，别的段不动；voice 的分厂商凭据（group_id）也要一起清。
func TestMediaConfigDeleteResetsSection(t *testing.T) {
	s := newMediaTestServer(t)
	s.cfg.ImageGen = &appconfig.ImageGenConfig{APIKey: "sk-keep-me", Provider: "dall-e"}
	s.cfg.VideoGen = &appconfig.VideoGenConfig{
		APIKey: "vk-drop-me", Provider: "minimax", DefaultResolution: "768p",
	}
	s.cfg.Voice = &voice.VoiceConfig{
		Provider: "minimax",
		Providers: map[string]voice.ProviderCredentials{
			"minimax": {APIKey: "vk-voice-drop", GroupID: "grp_1"},
		},
	}

	rr := httptest.NewRecorder()
	s.handleMediaConfig(rr, httptest.NewRequest(http.MethodDelete, "/api/media/config?section=video", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.VideoGen.APIKey != "" || s.cfg.VideoGen.Provider != "" || s.cfg.VideoGen.DefaultResolution != "" {
		t.Errorf("video section not reset: %+v", s.cfg.VideoGen)
	}
	if s.cfg.ImageGen.APIKey != "sk-keep-me" {
		t.Error("image section must be untouched when clearing video")
	}
	if s.cfg.Voice.Providers["minimax"].GroupID != "grp_1" {
		t.Error("voice section must be untouched when clearing video")
	}

	// 落盘：重启后不能又冒出旧值
	data, err := os.ReadFile(filepath.Join(s.magicHome, "config.json"))
	if err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
	if strings.Contains(string(data), "vk-drop-me") {
		t.Error("cleared key must not remain in config.json")
	}

	// voice：整段归零，连分厂商凭据一起清（PUT 空串清不掉这层）
	rr = httptest.NewRecorder()
	s.handleMediaConfig(rr, httptest.NewRequest(http.MethodDelete, "/api/media/config?section=voice", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("voice reset status = %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.Voice.Provider != "" || len(s.cfg.Voice.Providers) != 0 {
		t.Errorf("voice section not reset: %+v", s.cfg.Voice)
	}
	if s.cfg.Voice.Speed != 1 {
		t.Errorf("voice speed should fall back to 1.0, got %v", s.cfg.Voice.Speed)
	}

	// 非法/缺失 section 必须 400，不能静默清掉全部
	for _, body := range []string{"/api/media/config", "/api/media/config?section=everything"} {
		rr = httptest.NewRecorder()
		s.handleMediaConfig(rr, httptest.NewRequest(http.MethodDelete, body, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s should be rejected with 400, got %d", body, rr.Code)
		}
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

// 档位校验必须按厂商目录判定：minimax 的分辨率是 768p/2k，
// 曾经写死成 480p/720p/1080p，把设置页合法的 768p 打回 400。
func TestMediaConfigPutAcceptsProviderNativeResolution(t *testing.T) {
	s := newMediaTestServer(t)

	rr, _ := doMediaRequest(t, s, http.MethodPut, `{
		"video_gen": {"provider": "minimax", "default_resolution": "768p", "default_duration": 6}
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("minimax 768p should be accepted, got %d: %s", rr.Code, rr.Body.String())
	}
	if s.cfg.VideoGen.DefaultResolution != "768p" {
		t.Errorf("resolution not applied: %q", s.cfg.VideoGen.DefaultResolution)
	}

	// 通用档位对该厂商非法（1080p 不在 minimax 目录里）
	rr, body := doMediaRequest(t, s, http.MethodPut, `{
		"video_gen": {"provider": "minimax", "default_resolution": "1080p"}
	}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("minimax 1080p should be rejected, got %d", rr.Code)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "768p") {
		t.Errorf("error should list the provider's tiers, got %q", msg)
	}

	// 厂商时长区间同样按目录钳（minimax 5~10 秒）
	if rr, _ := doMediaRequest(t, s, http.MethodPut, `{"video_gen": {"provider": "minimax", "default_duration": 3}}`); rr.Code != http.StatusBadRequest {
		t.Errorf("minimax duration 3 should be rejected, got %d", rr.Code)
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

// MiniMax 的 GroupId 存进按厂商凭据段：缺省不动、显式空串清除。
func TestApplyVoicePatchGroupID(t *testing.T) {
	c := &voice.VoiceConfig{}

	g := "grp_1"
	applyVoicePatch(c, &voicePatch{GroupID: &g})
	if got := c.Providers["minimax"].GroupID; got != "grp_1" {
		t.Fatalf("group = %q, want grp_1", got)
	}

	empty := ""
	applyVoicePatch(c, &voicePatch{GroupID: &empty})
	if got := c.Providers["minimax"].GroupID; got != "" {
		t.Fatalf("group = %q, want cleared", got)
	}

	// 缺省 = 不动：清除后重新填上，再发一个不带 group_id 的补丁。
	g2 := "grp_2"
	applyVoicePatch(c, &voicePatch{GroupID: &g2})
	applyVoicePatch(c, &voicePatch{})
	if got := c.Providers["minimax"].GroupID; got != "grp_2" {
		t.Fatalf("group = %q, want untouched grp_2", got)
	}
}
