package tool

// speech_backend_test.go — 语音后端扩展点与 image_edit 后端化的回归测试。
//
// 锁住三件事：
//  1. 语音能力按"注册进哪张表"区分（TTS-only 的 provider 不会出现在识别表里）；
//  2. TTS/ASR 命中注册表后端时真的落盘/返回文本，空产物必须报错；
//  3. image_edit 不再按 provider 名字硬编码 switch，而是走 ImageEditor 能力。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// ---- 假后端 ----

type fakeTTSBackend struct {
	name  string
	avail bool
	audio []byte
	// format 为后端自报格式；为空表示"我也不知道"
	format string
	last   SpeechRequest
	calls  int
}

func (f *fakeTTSBackend) Name() string                      { return f.name }
func (f *fakeTTSBackend) Available(*voice.VoiceConfig) bool { return f.avail }
func (f *fakeTTSBackend) Synthesize(_ context.Context, _ *voice.VoiceConfig, req SpeechRequest) (*SpeechAudio, error) {
	f.calls++
	f.last = req
	return &SpeechAudio{Data: f.audio, Format: f.format}, nil
}

type fakeASRBackend struct {
	name  string
	avail bool
	text  string
	last  TranscriptionRequest
}

func (f *fakeASRBackend) Name() string                      { return f.name }
func (f *fakeASRBackend) Available(*voice.VoiceConfig) bool { return f.avail }
func (f *fakeASRBackend) Transcribe(_ context.Context, _ *voice.VoiceConfig, req TranscriptionRequest) (string, error) {
	f.last = req
	return f.text, nil
}

// ---- 注册表语义 ----

func TestSpeechCapabilityTablesAreSeparate(t *testing.T) {
	tts := &fakeTTSBackend{name: "tts-only", avail: true}
	asr := &fakeASRBackend{name: "asr-only", avail: true}
	RegisterSpeechBackend(tts)
	RegisterSpeechBackend(asr)
	t.Cleanup(func() {
		UnregisterSpeechBackend("tts-only")
		UnregisterSpeechBackend("asr-only")
	})

	if _, ok := LookupTTSBackend("tts-only"); !ok {
		t.Error("tts-only backend should be found in the TTS table")
	}
	if _, ok := LookupASRBackend("tts-only"); ok {
		t.Error("tts-only backend must NOT appear in the ASR table")
	}
	if _, ok := LookupASRBackend("asr-only"); !ok {
		t.Error("asr-only backend should be found in the ASR table")
	}
	if _, ok := LookupTTSBackend("asr-only"); ok {
		t.Error("asr-only backend must NOT appear in the TTS table")
	}

	hasTTS, hasASR := SpeechCapability("tts-only")
	if !hasTTS || hasASR {
		t.Errorf("SpeechCapability(tts-only) = (%v,%v), want (true,false)", hasTTS, hasASR)
	}
}

func TestRegisterSpeechBackendOwnerIsolation(t *testing.T) {
	// 两个插件贡献同名 provider：后注册者生效；卸载 A 不能删掉 B 的。
	RegisterSpeechBackendOwned("plugin-a", &fakeTTSBackend{name: "shared-tts", avail: false})
	RegisterSpeechBackendOwned("plugin-b", &fakeTTSBackend{name: "shared-tts", avail: true})
	t.Cleanup(func() { UnregisterSpeechBackendsByOwner("plugin-b") })

	if n := UnregisterSpeechBackendsByOwner("plugin-a"); n != 0 {
		t.Errorf("removing plugin-a should not remove plugin-b's backend, removed %d", n)
	}
	b, ok := LookupTTSBackend("shared-tts")
	if !ok || !b.Available(nil) {
		t.Error("plugin-b's backend must survive plugin-a's unregister")
	}
}

// ---- TTS 执行路径 ----

func TestTTSToolUsesRegisteredBackend(t *testing.T) {
	backend := &fakeTTSBackend{name: "demo-tts", avail: true, audio: []byte("ID3fake"), format: "wav"}
	RegisterSpeechBackend(backend)
	t.Cleanup(func() { UnregisterSpeechBackend("demo-tts") })

	out := filepath.Join(t.TempDir(), "hello.wav")
	res, err := NewTTSTool().Execute(context.Background(), map[string]interface{}{
		"text":        "你好世界",
		"provider":    "demo-tts",
		"format":      "wav",
		"output_path": out,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["status"] != "success" {
		t.Fatalf("status = %v, want success", m["status"])
	}
	if m["provider"] != "demo-tts" {
		t.Errorf("provider = %v, want demo-tts", m["provider"])
	}
	data, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("audio not written: %v", rerr)
	}
	if string(data) != "ID3fake" {
		t.Errorf("written bytes = %q", string(data))
	}
	if backend.last.Text != "你好世界" {
		t.Errorf("request text = %q", backend.last.Text)
	}
	// text_length 必须按字符数算（中文 4 个字，不是 12 字节）
	if m["text_length"] != 4 {
		t.Errorf("text_length = %v, want 4", m["text_length"])
	}
}

func TestTTSToolRejectsEmptyAudioFromBackend(t *testing.T) {
	RegisterSpeechBackend(&fakeTTSBackend{name: "empty-tts", avail: true})
	t.Cleanup(func() { UnregisterSpeechBackend("empty-tts") })

	_, err := NewTTSTool().Execute(context.Background(), map[string]interface{}{
		"text":        "x",
		"provider":    "empty-tts",
		"output_path": filepath.Join(t.TempDir(), "x.mp3"),
	})
	if err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Fatalf("expected empty-audio error, got %v", err)
	}
}

func TestTTSToolBackendUnavailableIsNotConfigured(t *testing.T) {
	RegisterSpeechBackend(&fakeTTSBackend{name: "offline-tts", avail: false})
	t.Cleanup(func() { UnregisterSpeechBackend("offline-tts") })

	res, err := NewTTSTool().Execute(context.Background(), map[string]interface{}{
		"text":        "x",
		"provider":    "offline-tts",
		"output_path": filepath.Join(t.TempDir(), "x.mp3"),
	})
	if err != nil {
		t.Fatalf("unavailable backend should return a hint, not an error: %v", err)
	}
	if m := res.(map[string]interface{}); m["status"] != "not_configured" {
		t.Errorf("status = %v, want not_configured", m["status"])
	}
}

// 后端报的格式与请求不同、且无法转换时，落盘扩展名必须跟着真实格式走，
// 否则用户拿到的是"名字叫 .mp3、内容其实是 wav"的文件。
func TestTTSToolKeepsRealFormatWhenConversionFails(t *testing.T) {
	// 用一个 ffmpeg 一定不认识的格式，保证转换失败分支被覆盖
	RegisterSpeechBackend(&fakeTTSBackend{
		name: "raw-tts", avail: true, audio: []byte("RIFffake"), format: "wav",
	})
	t.Cleanup(func() { UnregisterSpeechBackend("raw-tts") })

	out := filepath.Join(t.TempDir(), "clip.opus")
	res, err := NewTTSTool().Execute(context.Background(), map[string]interface{}{
		"text":        "x",
		"provider":    "raw-tts",
		"format":      "opus",
		"output_path": out,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	got := res.(map[string]interface{})["output_path"].(string)
	if filepath.Ext(got) != ".wav" {
		t.Errorf("output_path = %q, want a .wav extension (real format)", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("audio should exist at %q: %v", got, err)
	}
}

// ---- ASR 执行路径 ----

func TestASRToolUsesRegisteredBackend(t *testing.T) {
	backend := &fakeASRBackend{name: "demo-asr", avail: true, text: "你好"}
	RegisterSpeechBackend(backend)
	t.Cleanup(func() { UnregisterSpeechBackend("demo-asr") })

	audio := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(audio, []byte("RIFF....WAVE"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := NewASRTool().Execute(context.Background(), map[string]interface{}{
		"audio_path": audio,
		"provider":   "demo-asr",
		"language":   "zh",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["text"] != "你好" || m["provider"] != "demo-asr" {
		t.Errorf("unexpected result: %v", m)
	}
	if backend.last.Language != "zh" {
		t.Errorf("language not forwarded: %q", backend.last.Language)
	}
	if len(backend.last.Audio) == 0 {
		t.Error("audio bytes were not forwarded to the backend")
	}
}

func TestASRToolRequiresTextFromBackend(t *testing.T) {
	RegisterSpeechBackend(&fakeASRBackend{name: "silent-asr", avail: true, text: "  "})
	t.Cleanup(func() { UnregisterSpeechBackend("silent-asr") })

	audio := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(audio, []byte("RIFF....WAVE"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewASRTool().Execute(context.Background(), map[string]interface{}{
		"audio_path": audio,
		"provider":   "silent-asr",
	})
	if err == nil || !strings.Contains(err.Error(), "empty transcription") {
		t.Fatalf("expected empty-transcription error, got %v", err)
	}
}

// ---- image_edit 后端化 ----

type fakeImageEditor struct {
	name  string
	avail bool
	last  ImageEditRequest
}

func (f *fakeImageEditor) Name() string                   { return f.name }
func (f *fakeImageEditor) Available(*ImageGenConfig) bool { return f.avail }
func (f *fakeImageEditor) Generate(context.Context, *ImageGenConfig, ImageRequest) (*ImageGenerationResult, error) {
	return &ImageGenerationResult{Images: []ImageInfo{{Data: []byte("png"), Format: "png"}}}, nil
}
func (f *fakeImageEditor) Edit(_ context.Context, _ *ImageGenConfig, req ImageEditRequest) (*ImageGenerationResult, error) {
	f.last = req
	return &ImageGenerationResult{Images: []ImageInfo{{Data: []byte("edited"), Format: "png"}}}, nil
}

func TestImageEditGoesThroughBackend(t *testing.T) {
	backend := &fakeImageEditor{name: "edit-demo", avail: true}
	RegisterImageBackend(backend)
	t.Cleanup(func() { UnregisterImageBackend("edit-demo") })

	dir := t.TempDir()
	src := filepath.Join(dir, "in.png")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultImageGenConfig()
	cfg.Provider = "edit-demo"
	cfg.OutputDirectory = dir

	res, err := NewImageEditTool(cfg).Execute(context.Background(), map[string]interface{}{
		"image_path": src,
		"prompt":     "make it blue",
		"edit_type":  "inpaint",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["status"] != "success" {
		t.Fatalf("status = %v, want success", m["status"])
	}
	if string(backend.last.Image) != "source" {
		t.Errorf("source image bytes not forwarded: %q", string(backend.last.Image))
	}
	if backend.last.EditType != "inpaint" || backend.last.Prompt != "make it blue" {
		t.Errorf("edit request not forwarded: %+v", backend.last)
	}
	if paths, _ := m["images"].([]string); len(paths) != 1 {
		t.Errorf("expected 1 saved image, got %v", m["images"])
	}
}

// 内置只有 dall-e 支持编辑：切到 stable-diffusion 时 image_edit 必须不可用，
// 且错误里要列出"哪些 provider 能编辑"。
func TestImageEditAvailabilityFollowsEditorCapability(t *testing.T) {
	cfg := DefaultImageGenConfig()
	cfg.APIKey = "sk-test"
	cfg.Provider = ProviderStableDiffusion
	if NewImageEditTool(cfg).Available() {
		t.Error("image_edit must be unavailable for a provider without ImageEditor")
	}

	src := filepath.Join(t.TempDir(), "in.png")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewImageEditTool(cfg).Execute(context.Background(), map[string]interface{}{
		"image_path": src,
		"prompt":     "x",
	})
	if err == nil {
		t.Fatal("expected an error when the provider cannot edit")
	}
	if !strings.Contains(err.Error(), "dall-e") {
		t.Errorf("error should list editing providers, got %v", err)
	}

	// dalle 有 key 时必须可用（这里只断言可用性，不发真实请求）
	dalle := DefaultImageGenConfig()
	dalle.APIKey = "sk-test"
	dalle.Provider = ProviderDALLE
	if !NewImageEditTool(dalle).Available() {
		t.Error("image_edit must be available for dall-e with a key")
	}
}

func TestBuiltinImageEditorsOnlyDalle(t *testing.T) {
	editors := RegisteredImageEditors()
	if len(editors) != 1 || editors[0] != string(ProviderDALLE) {
		t.Errorf("RegisteredImageEditors() = %v, want [dall-e]", editors)
	}
}
