package agentplugin

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/tool"
)

// media_provider_test.go — 插件媒体后端的契约测试。
//
// 这里锁三件事：
//  1. 声明解析宽松但不静默（写错了要留下可读的 warning）；
//  2. 返回解析宽进严出（认得出就转换，认不出必须说清期望什么）；
//  3. owner 隔离（两个插件声明同名 provider 时，卸载一个不能带走另一个）。

// fakeMediaCaller 替代真实 MCP 连接，让参数映射与结果解析可以单独验证。
type fakeMediaCaller struct {
	connected map[string]bool
	reply     any
	err       error

	lastServer string
	lastTool   string
	lastArgs   map[string]any
}

func (f *fakeMediaCaller) CallTool(_ context.Context, server, toolName string, args map[string]any) (any, error) {
	f.lastServer, f.lastTool, f.lastArgs = server, toolName, args
	return f.reply, f.err
}

func (f *fakeMediaCaller) ServerConnected(server string) bool { return f.connected[server] }

func TestParseMediaProviderSpec(t *testing.T) {
	p := &Plugin{Extensions: map[string]any{
		mediaProviderExtKey: map[string]any{
			"image": []any{
				map[string]any{"name": "ComfyUI", "server": "comfy", "tool": "generate_image"},
				map[string]any{"name": "broken", "server": "x"}, // 缺 tool
				"not-an-object",
				map[string]any{"name": "comfyui", "server": "c2", "tool": "t2"}, // 重复名
			},
			"video": []any{
				map[string]any{"name": "kling", "server": "kling", "tool": "generate_video"},
			},
		},
	}}

	spec, warnings := pluginMediaProviderSpec(p)

	if len(spec.Image) != 1 {
		t.Fatalf("expected 1 valid image binding, got %d (%+v)", len(spec.Image), spec.Image)
	}
	if spec.Image[0].Name != "comfyui" {
		t.Errorf("provider name should be lowercased, got %q", spec.Image[0].Name)
	}
	if spec.Image[0].Server != "comfy" || spec.Image[0].Tool != "generate_image" {
		t.Errorf("binding fields wrong: %+v", spec.Image[0])
	}
	if len(spec.Video) != 1 || spec.Video[0].Name != "kling" {
		t.Errorf("video binding wrong: %+v", spec.Video)
	}
	if len(warnings) != 3 {
		t.Errorf("expected 3 warnings (missing tool, non-object, duplicate), got %d: %v", len(warnings), warnings)
	}
}

func TestParseMediaProviderSpecMissingNamespace(t *testing.T) {
	spec, warnings := pluginMediaProviderSpec(&Plugin{Extensions: map[string]any{"other": 1}})
	if len(spec.Image) != 0 || len(spec.Video) != 0 || len(warnings) != 0 {
		t.Errorf("unrelated extensions must be ignored silently, got %+v %v", spec, warnings)
	}

	// 命名空间存在但不是对象：要留 warning，但不能让插件加载失败
	_, warnings = pluginMediaProviderSpec(&Plugin{Extensions: map[string]any{mediaProviderExtKey: "oops"}})
	if len(warnings) != 1 {
		t.Errorf("non-object extension should produce one warning, got %v", warnings)
	}
}

func TestPluginImageBackendMapsRequestArgs(t *testing.T) {
	caller := &fakeMediaCaller{
		connected: map[string]bool{"comfy": true},
		reply:     `{"url":"https://x/a.png"}`,
	}
	backend := &pluginImageBackend{
		binding: mediaProviderBinding{Name: "comfyui", Server: "comfy", Tool: "generate_image"},
		caller:  caller,
	}

	seed := 42
	res, err := backend.Generate(context.Background(), nil, tool.ImageRequest{
		Prompt:         "a cat",
		NegativePrompt: "blurry",
		Style:          "anime",
		Size:           "1024x1024",
		Count:          2,
		Seed:           &seed,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(res.Images) != 1 || res.Images[0].URL != "https://x/a.png" {
		t.Fatalf("unexpected images: %+v", res.Images)
	}
	if res.Images[0].Format != "png" {
		t.Errorf("format should be inferred from url, got %q", res.Images[0].Format)
	}

	if caller.lastServer != "comfy" || caller.lastTool != "generate_image" {
		t.Errorf("wrong target: server=%q tool=%q", caller.lastServer, caller.lastTool)
	}
	for _, key := range []string{"prompt", "negative_prompt", "style", "size", "count", "seed"} {
		if _, ok := caller.lastArgs[key]; !ok {
			t.Errorf("argument %q missing from forwarded args", key)
		}
	}
	if caller.lastArgs["count"] != 2 {
		t.Errorf("count should be forwarded as int, got %#v", caller.lastArgs["count"])
	}
}

func TestPluginImageBackendParsesReplyShapes(t *testing.T) {
	cases := []struct {
		name      string
		reply     any
		wantCount int
		wantURL   string
		wantB64   bool
		wantFmt   string
	}{
		{name: "single url object", reply: `{"url":"https://x/a.jpg"}`, wantCount: 1, wantURL: "https://x/a.jpg", wantFmt: "jpg"},
		{name: "urls array", reply: `{"urls":["https://x/a.png","https://x/b.webp"]}`, wantCount: 2, wantURL: "https://x/a.png", wantFmt: "png"},
		{name: "images array", reply: `{"images":[{"url":"https://x/a.png?token=1"},{"b64":"iVBORw0KGgo="}]}`, wantCount: 2, wantURL: "https://x/a.png?token=1", wantFmt: "png"},
		{name: "plain url text", reply: "https://x/a.gif", wantCount: 1, wantURL: "https://x/a.gif", wantFmt: "gif"},
		{name: "data uri", reply: "data:image/webp;base64,UklGRg==", wantCount: 1, wantB64: true, wantFmt: "webp"},
		{name: "top-level array", reply: `[{"url":"https://x/a.png"}]`, wantCount: 1, wantURL: "https://x/a.png", wantFmt: "png"},
		{name: "object map", reply: map[string]any{"url": "https://x/a.png"}, wantCount: 1, wantURL: "https://x/a.png", wantFmt: "png"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			images, _, err := parsePluginImageResult(tc.reply)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(images) != tc.wantCount {
				t.Fatalf("got %d images, want %d", len(images), tc.wantCount)
			}
			first := images[0]
			if tc.wantURL != "" && first.URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", first.URL, tc.wantURL)
			}
			if tc.wantB64 && first.Base64Data == "" {
				t.Error("expected inline base64 payload")
			}
			if first.Format != tc.wantFmt {
				t.Errorf("Format = %q, want %q", first.Format, tc.wantFmt)
			}
		})
	}
}

// 认不出时不能返回"0 张图 + nil error"——那会让模型以为生成成功但什么都没拿到。
func TestPluginImageBackendUnparseableReplyIsActionable(t *testing.T) {
	caller := &fakeMediaCaller{connected: map[string]bool{"comfy": true}, reply: "I could not do that, sorry."}
	backend := &pluginImageBackend{
		binding: mediaProviderBinding{Name: "comfyui", Server: "comfy", Tool: "generate_image"},
		caller:  caller,
	}

	_, err := backend.Generate(context.Background(), nil, tool.ImageRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error for unparseable reply")
	}
	for _, want := range []string{"comfyui", "expected", "got:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err.Error(), want)
		}
	}
}

func TestPluginImageBackendPropagatesToolError(t *testing.T) {
	caller := &fakeMediaCaller{connected: map[string]bool{"comfy": true}, err: context.DeadlineExceeded}
	backend := &pluginImageBackend{
		binding: mediaProviderBinding{Name: "comfyui", Server: "comfy", Tool: "generate_image"},
		caller:  caller,
	}
	if _, err := backend.Generate(context.Background(), nil, tool.ImageRequest{Prompt: "x"}); err == nil {
		t.Fatal("tool error must propagate (otherwise the model sees a silent success)")
	}
}

func TestPluginVideoBackendParsesReplyShapes(t *testing.T) {
	cases := []struct {
		name  string
		reply any
		want  string
	}{
		{name: "url", reply: `{"url":"https://x/v.mp4"}`, want: "https://x/v.mp4"},
		{name: "video_url", reply: `{"video_url":"https://x/v.mp4","task_id":"t1"}`, want: "https://x/v.mp4"},
		{name: "plain text", reply: "https://x/v.mp4", want: "https://x/v.mp4"},
		{name: "object", reply: map[string]any{"output": "https://x/v.mp4"}, want: "https://x/v.mp4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parsePluginVideoResult(tc.reply)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.URL != tc.want {
				t.Errorf("URL = %q, want %q", res.URL, tc.want)
			}
		})
	}

	if _, err := parsePluginVideoResult("done!"); err == nil {
		t.Error("non-url reply must be rejected")
	}
}

func TestPluginBackendAvailabilityFollowsConnection(t *testing.T) {
	caller := &fakeMediaCaller{connected: map[string]bool{"comfy": false}}
	backend := &pluginImageBackend{
		binding: mediaProviderBinding{Name: "comfyui", Server: "comfy", Tool: "generate_image"},
		caller:  caller,
	}
	if backend.Available(nil) {
		t.Error("backend must be unavailable while its MCP entry is disconnected")
	}

	caller.connected["comfy"] = true
	if !backend.Available(nil) {
		t.Error("backend must become available once the entry is connected")
	}
}

// owner 隔离：A 先注册 comfyui，B 后注册同名（覆盖），卸载 A 不能把 B 的删掉。
func TestRegisterMediaProvidersIsolatesOwners(t *testing.T) {
	pA := &Plugin{
		Manifest:   &Manifest{Name: "plugin-a"},
		MCPServers: []MCPEntryStatus{{Name: "comfy"}},
		Extensions: map[string]any{
			mediaProviderExtKey: map[string]any{
				"image": []any{map[string]any{"name": "comfyui", "server": "comfy", "tool": "gen"}},
			},
		},
	}
	pB := &Plugin{
		Manifest:   &Manifest{Name: "plugin-b"},
		MCPServers: []MCPEntryStatus{{Name: "comfy"}},
		Extensions: map[string]any{
			mediaProviderExtKey: map[string]any{
				"image": []any{map[string]any{"name": "comfyui", "server": "comfy", "tool": "gen"}},
			},
		},
	}

	rA, rB := NewRuntime(pA), NewRuntime(pB)
	rA.RegisterMediaProviders()
	rB.RegisterMediaProviders()
	t.Cleanup(func() {
		rA.UnregisterMediaProviders()
		rB.UnregisterMediaProviders()
	})

	if _, ok := tool.LookupImageBackend("comfyui"); !ok {
		t.Fatal("provider should be registered")
	}

	// A 卸载：同名条目已被 B 覆盖，不能被带走
	rA.UnregisterMediaProviders()
	if _, ok := tool.LookupImageBackend("comfyui"); !ok {
		t.Error("unregistering A must not remove the provider now owned by B")
	}

	rB.UnregisterMediaProviders()
	if _, ok := tool.LookupImageBackend("comfyui"); ok {
		t.Error("unregistering the owner should remove the provider")
	}
}

// server 名写错是这类扩展最常见的错误：必须留 warning 且不注册，
// 而不是注册一个永远不可用的 provider 让人去猜。
func TestRegisterMediaProvidersRejectsUndeclaredServer(t *testing.T) {
	p := &Plugin{
		Manifest:   &Manifest{Name: "typo-plugin"},
		MCPServers: []MCPEntryStatus{{Name: "comfy"}},
		Extensions: map[string]any{
			mediaProviderExtKey: map[string]any{
				"image": []any{map[string]any{"name": "typo-provider", "server": "commfy", "tool": "gen"}},
			},
		},
	}
	r := NewRuntime(p)
	r.RegisterMediaProviders()
	t.Cleanup(r.UnregisterMediaProviders)

	if _, ok := tool.LookupImageBackend("typo-provider"); ok {
		t.Fatal("provider pointing at an undeclared server must not be registered")
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "commfy") && strings.Contains(w, "not declared") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an actionable warning about the undeclared server, got %v", p.Warnings)
	}
}

// ---- 语音（speech）扩展 ----

func TestParseMediaProviderSpecSpeech(t *testing.T) {
	p := &Plugin{Extensions: map[string]any{
		mediaProviderExtKey: map[string]any{
			"speech": []any{
				map[string]any{"name": "CosyVoice", "server": "tts", "tts_tool": "synthesize"},
				map[string]any{"name": "only-asr", "server": "asr", "asr_tool": "transcribe"},
				map[string]any{"name": "no-tool", "server": "x"},                     // 两个工具都没写
				map[string]any{"name": "no-server", "tts_tool": "s"},                 // 缺 server
				map[string]any{"name": "cosyvoice", "server": "t2", "tts_tool": "s"}, // 重复名
			},
		},
	}}

	spec, warnings := pluginMediaProviderSpec(p)

	if len(spec.Speech) != 2 {
		t.Fatalf("expected 2 valid speech bindings, got %d (%+v)", len(spec.Speech), spec.Speech)
	}
	if spec.Speech[0].Name != "cosyvoice" || spec.Speech[0].TTSTool != "synthesize" || spec.Speech[0].ASRTool != "" {
		t.Errorf("binding fields wrong: %+v", spec.Speech[0])
	}
	if spec.Speech[1].ASRTool != "transcribe" || spec.Speech[1].TTSTool != "" {
		t.Errorf("asr-only binding wrong: %+v", spec.Speech[1])
	}
	if len(warnings) != 3 {
		t.Errorf("expected 3 warnings, got %d: %v", len(warnings), warnings)
	}
}

func TestPluginTTSBackendMapsRequestAndParsesDataURI(t *testing.T) {
	caller := &fakeMediaCaller{
		connected: map[string]bool{"tts": true},
		reply:     `{"audio":"data:audio/wav;base64,UklGRg=="}`,
	}
	b := &pluginTTSBackend{
		binding: speechProviderBinding{Name: "cosyvoice", Server: "tts", TTSTool: "synthesize"},
		caller:  caller,
	}

	audio, err := b.Synthesize(context.Background(), nil, tool.SpeechRequest{
		Text: "你好", Voice: "v1", Language: "zh", Format: "wav", Speed: 1.2,
	})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if string(audio.Data) != "RIFF" {
		t.Errorf("decoded audio = %q", string(audio.Data))
	}
	if audio.Mime != "audio/wav" {
		t.Errorf("mime = %q, want audio/wav", audio.Mime)
	}
	if caller.lastServer != "tts" || caller.lastTool != "synthesize" {
		t.Errorf("called %s/%s", caller.lastServer, caller.lastTool)
	}
	if caller.lastArgs["text"] != "你好" || caller.lastArgs["language"] != "zh" {
		t.Errorf("args not mapped: %v", caller.lastArgs)
	}
}

func TestPluginTTSBackendParsesBase64Field(t *testing.T) {
	caller := &fakeMediaCaller{
		connected: map[string]bool{"tts": true},
		reply:     `{"audio_b64":"UklGRg==","format":"wav"}`,
	}
	b := &pluginTTSBackend{
		binding: speechProviderBinding{Name: "p", Server: "tts", TTSTool: "s"},
		caller:  caller,
	}
	audio, err := b.Synthesize(context.Background(), nil, tool.SpeechRequest{Text: "x"})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if string(audio.Data) != "RIFF" || audio.Format != "wav" {
		t.Errorf("unexpected audio: %+v", audio)
	}
}

func TestPluginTTSBackendUnparseableReplyIsActionable(t *testing.T) {
	caller := &fakeMediaCaller{connected: map[string]bool{"tts": true}, reply: "sorry, I can't do that"}
	b := &pluginTTSBackend{
		binding: speechProviderBinding{Name: "p", Server: "tts", TTSTool: "s"},
		caller:  caller,
	}
	_, err := b.Synthesize(context.Background(), nil, tool.SpeechRequest{Text: "x"})
	if err == nil {
		t.Fatal("expected an error for an unparseable reply")
	}
	if !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "sorry") {
		t.Errorf("error should say what was expected and what arrived, got %v", err)
	}
}

func TestPluginASRBackendParsesText(t *testing.T) {
	caller := &fakeMediaCaller{connected: map[string]bool{"asr": true}, reply: `{"transcript":"你好世界"}`}
	b := &pluginASRBackend{
		binding: speechProviderBinding{Name: "p", Server: "asr", ASRTool: "transcribe"},
		caller:  caller,
	}
	text, err := b.Transcribe(context.Background(), nil, tool.TranscriptionRequest{
		Audio: []byte("RIFF"), Language: "zh", FileName: "a.wav",
	})
	if err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if text != "你好世界" {
		t.Errorf("text = %q", text)
	}
	if caller.lastArgs["audio_b64"] != "UklGRg==" {
		t.Errorf("audio not forwarded as base64: %v", caller.lastArgs["audio_b64"])
	}
	if caller.lastArgs["file_name"] != "a.wav" {
		t.Errorf("file_name not forwarded: %v", caller.lastArgs)
	}
}

// 只声明 tts_tool 的 provider 不能出现在识别表里 —— 否则模型会调 asr 然后拿到
// "这个 provider 不支持识别"，白烧一轮。
func TestRegisterMediaProvidersSpeechCapabilities(t *testing.T) {
	p := &Plugin{
		Manifest:   &Manifest{Name: "speech-plugin"},
		MCPServers: []MCPEntryStatus{{Name: "tts"}},
		Extensions: map[string]any{
			mediaProviderExtKey: map[string]any{
				"speech": []any{map[string]any{
					"name": "cosyvoice", "server": "tts", "tts_tool": "synthesize",
				}},
			},
		},
	}
	r := NewRuntime(p)
	r.RegisterMediaProviders()
	t.Cleanup(r.UnregisterMediaProviders)

	if _, ok := tool.LookupTTSBackend("cosyvoice"); !ok {
		t.Fatal("tts backend should be registered")
	}
	if _, ok := tool.LookupASRBackend("cosyvoice"); ok {
		t.Error("tts-only provider must not land in the ASR table")
	}
	hasTTS, hasASR := tool.SpeechCapability("cosyvoice")
	if !hasTTS || hasASR {
		t.Errorf("capability = (%v,%v), want (true,false)", hasTTS, hasASR)
	}

	r.UnregisterMediaProviders()
	if _, ok := tool.LookupTTSBackend("cosyvoice"); ok {
		t.Error("unregistering the owner should remove the speech backend")
	}
}
