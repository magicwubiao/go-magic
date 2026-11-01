package agentplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/internal/voice"
)

// media_provider.go — 插件贡献「媒体后端」的扩展点。
//
// 为什么是这种形态（不是让插件注册新工具）：
//
//	工具名属于内核。image_gen / video_generate 的 schema、参数、审批、落盘、
//	超时全在内核；插件只能往里加"换个后端"，不能让模型看到 kling_generate
//	这种新工具名——工具一裂变，模型的选择准确率立刻掉。
//
// 为什么复用 MCP 通道（不是另开 HTTP 端口）：
//
//	插件体系已经有 mcp.json（身份、凭据、HTTPS 校验、自动重连、懒连接全齐）。
//	让插件再监听一个端口，等于把 SSRF 面、端口管理、凭据传递全部重讲一遍，
//	而这套东西内核已经写好且被验证过。
//
// 声明方式（plugin.json）：
//
//	"extensions": {
//	  "io.github.magicwubiao.media-provider": {
//	    "image":  [{"name": "comfyui", "server": "comfy", "tool": "generate_image"}],
//	    "video":  [{"name": "kling",   "server": "kling", "tool": "generate_video"}],
//	    "speech": [{"name": "cosyvoice", "server": "tts",
//	                "tts_tool": "synthesize", "asr_tool": "transcribe"}]
//	  }
//	}
//
// 其中 server 是 mcp.json 里的条目名，tool 是该 server 上的工具名。
// 之后用户在 config.json 里写 "image_gen": {"provider": "comfyui"} 即可切过去，
// 凭据仍走插件自己的凭据表单（plugin_secrets），不新立一套引用体系。
//
// 产物契约（工具返回值，内核负责下载落盘）：
//
//	图片：{"url": "..."} / {"urls": [...]} / {"images": [{"url"|"b64": "...", "format": "png"}]}
//	      / {"b64": "..."} / 纯文本 URL / data URI
//	视频：{"url": "..."} / {"video_url": "..."} / {"urls": [...]} / 纯文本 URL
//	语音：{"url"|"audio_url"} / {"b64"|"audio_b64"} / {"audio": "data:audio/mpeg;base64,..."}
//	      / 纯文本 URL 或 data URI；可带 {"format"|"mime"} 指明格式
//
// 为什么要容忍这么多形状：插件作者写 MCP server 时不会去读内核源码，能猜到的
// 就这几种。解析器宽进严出——认不出时必须给出"期望什么"的具体错误，否则调试
// 成本全落在用户身上。

// mediaProviderExtKey 是 plugin.json 里声明媒体后端的扩展命名空间。
// 与 catalogPackExtKey 同风格（反向域名），避免和别人的扩展撞名。
const mediaProviderExtKey = "io.github.magicwubiao.media-provider"

// mediaProviderBinding 把「一个 provider 名字」绑定到「某个 MCP server 上的某个工具」。
type mediaProviderBinding struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Tool   string `json:"tool"`
}

type mediaProviderSpec struct {
	Image  []mediaProviderBinding  `json:"image,omitempty"`
	Video  []mediaProviderBinding  `json:"video,omitempty"`
	Speech []speechProviderBinding `json:"speech,omitempty"`
}

// speechProviderBinding 把「一个语音 provider 名字」绑定到 1~2 个 MCP 工具。
//
// 语音与图片/视频不同：同一个 provider 名可能只提供合成、只提供识别，或两者都提供，
// 而且两个能力是两个独立工具（接受完全不同的参数）。所以这里用两个字段分别绑定，
// 而不是像 image/video 那样一个 tool 走天下。
type speechProviderBinding struct {
	Name    string `json:"name"`
	Server  string `json:"server"`
	TTSTool string `json:"tts_tool,omitempty"`
	ASRTool string `json:"asr_tool,omitempty"`
}

// pluginMediaProviderSpec 解析插件清单里的媒体后端声明。
// 解析失败一律降级为"少注册几个 provider"，绝不让插件加载失败——
// 一个扩展写错了不该让整个插件的 MCP / skills 一起不可用。
func pluginMediaProviderSpec(p *Plugin) (mediaProviderSpec, []string) {
	var spec mediaProviderSpec
	if p == nil || p.Extensions == nil {
		return spec, nil
	}
	raw, ok := p.Extensions[mediaProviderExtKey]
	if !ok {
		return spec, nil
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return spec, []string{"media-provider extension is not an object, ignored"}
	}

	var warnings []string
	spec.Image, warnings = parseBindings(root, "image", warnings)
	spec.Video, warnings = parseBindings(root, "video", warnings)
	spec.Speech, warnings = parseSpeechBindings(root, "speech", warnings)
	return spec, warnings
}

// parseSpeechBindings 解析 speech 声明。规则与 parseBindings 一致（宽进，
// 每条不合格只产一条 warning），区别是要求 name/server + 至少一个能力工具。
func parseSpeechBindings(root map[string]any, kind string, warnings []string) ([]speechProviderBinding, []string) {
	raw, exists := root[kind]
	if !exists || raw == nil {
		return nil, warnings
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, append(warnings, fmt.Sprintf("media-provider.%s is not an array, ignored", kind))
	}

	out := make([]speechProviderBinding, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("media-provider.%s[%d] is not an object, ignored", kind, i))
			continue
		}
		b := speechProviderBinding{
			Name:    strings.ToLower(strings.TrimSpace(stringField(m, "name"))),
			Server:  strings.TrimSpace(stringField(m, "server")),
			TTSTool: strings.TrimSpace(stringField(m, "tts_tool")),
			ASRTool: strings.TrimSpace(stringField(m, "asr_tool")),
		}
		if b.Name == "" || b.Server == "" {
			warnings = append(warnings, fmt.Sprintf(
				"media-provider.%s[%d] ignored: name and server are required", kind, i))
			continue
		}
		if b.TTSTool == "" && b.ASRTool == "" {
			warnings = append(warnings, fmt.Sprintf(
				"media-provider.%s[%d] ignored: at least one of tts_tool / asr_tool is required", kind, i))
			continue
		}
		if seen[b.Name] {
			warnings = append(warnings, fmt.Sprintf(
				"media-provider.%s: duplicate provider %q ignored", kind, b.Name))
			continue
		}
		seen[b.Name] = true
		out = append(out, b)
	}
	return out, warnings
}

func parseBindings(root map[string]any, kind string, warnings []string) ([]mediaProviderBinding, []string) {
	raw, exists := root[kind]
	if !exists || raw == nil {
		return nil, warnings
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, append(warnings, fmt.Sprintf("media-provider.%s is not an array, ignored", kind))
	}

	out := make([]mediaProviderBinding, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("media-provider.%s[%d] is not an object, ignored", kind, i))
			continue
		}
		b := mediaProviderBinding{
			Name:   strings.ToLower(strings.TrimSpace(stringField(m, "name"))),
			Server: strings.TrimSpace(stringField(m, "server")),
			Tool:   strings.TrimSpace(stringField(m, "tool")),
		}
		if b.Name == "" || b.Server == "" || b.Tool == "" {
			warnings = append(warnings, fmt.Sprintf(
				"media-provider.%s[%d] ignored: name, server and tool are all required", kind, i))
			continue
		}
		// 同一个插件里重复声明同名 provider，后者无意义（会互相覆盖），只留第一条。
		if seen[b.Name] {
			warnings = append(warnings, fmt.Sprintf(
				"media-provider.%s: duplicate provider %q ignored", kind, b.Name))
			continue
		}
		seen[b.Name] = true
		out = append(out, b)
	}
	return out, warnings
}

// pluginMediaCaller 是适配器对运行时的最小依赖，抽出来是为了让 Generate 的参数
// 映射与结果解析可以脱离真实 MCP 连接单独测试。
type pluginMediaCaller interface {
	CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error)
	ServerConnected(serverName string) bool
}

// pluginImageBackend 把插件声明的 MCP 工具包装成 tool.ImageBackend。
//
// 它只做两件事：把归一化请求翻译成工具参数、把工具返回值翻译成 ImageInfo。
// 下载、落盘、重试、超时、可用性裁剪全在内核（见 tool/media_provider.go 的边界说明）。
type pluginImageBackend struct {
	binding mediaProviderBinding
	caller  pluginMediaCaller
}

func (b *pluginImageBackend) Name() string { return b.binding.Name }

// Available：MCP 条目当前有活连接才算可用。插件被禁用/连接断开时工具自动消失，
// 不需要额外的开关状态。
func (b *pluginImageBackend) Available(*tool.ImageGenConfig) bool {
	return b.caller.ServerConnected(b.binding.Server)
}

func (b *pluginImageBackend) Generate(ctx context.Context, _ *tool.ImageGenConfig, req tool.ImageRequest) (*tool.ImageGenerationResult, error) {
	args := map[string]any{
		"prompt": req.Prompt,
		"count":  req.Count,
		"size":   req.Size,
	}
	if req.NegativePrompt != "" {
		args["negative_prompt"] = req.NegativePrompt
	}
	if req.Style != "" {
		args["style"] = req.Style
	}
	if req.Seed != nil {
		args["seed"] = *req.Seed
	}
	if len(req.InputImage) > 0 {
		args["image_b64"] = base64.StdEncoding.EncodeToString(req.InputImage)
	}

	raw, err := b.caller.CallTool(ctx, b.binding.Server, b.binding.Tool, args)
	if err != nil {
		return nil, fmt.Errorf("plugin media provider %q: %w", b.binding.Name, err)
	}

	images, revised, err := parsePluginImageResult(raw)
	if err != nil {
		return nil, fmt.Errorf("plugin media provider %q (tool %s): %w", b.binding.Name, b.binding.Tool, err)
	}
	return &tool.ImageGenerationResult{Images: images, RevisedPrompts: revised}, nil
}

// pluginVideoBackend 同 pluginImageBackend，产物是一个视频直链。
type pluginVideoBackend struct {
	binding mediaProviderBinding
	caller  pluginMediaCaller
}

func (b *pluginVideoBackend) Name() string { return b.binding.Name }

func (b *pluginVideoBackend) Available(*tool.VideoGenConfig) bool {
	return b.caller.ServerConnected(b.binding.Server)
}

func (b *pluginVideoBackend) Generate(ctx context.Context, _ *tool.VideoGenConfig, req tool.VideoRequest) (*tool.VideoResult, error) {
	args := map[string]any{
		"prompt":     req.Prompt,
		"model":      req.Model,
		"duration":   req.Duration,
		"ratio":      req.Ratio,
		"resolution": req.Resolution,
	}
	if req.NegativePrompt != "" {
		args["negative_prompt"] = req.NegativePrompt
	}
	if req.ImageURL != "" {
		args["image_url"] = req.ImageURL
	}
	if req.ImagePath != "" {
		args["image_path"] = req.ImagePath
	}

	raw, err := b.caller.CallTool(ctx, b.binding.Server, b.binding.Tool, args)
	if err != nil {
		return nil, fmt.Errorf("plugin media provider %q: %w", b.binding.Name, err)
	}

	result, err := parsePluginVideoResult(raw)
	if err != nil {
		return nil, fmt.Errorf("plugin media provider %q (tool %s): %w", b.binding.Name, b.binding.Tool, err)
	}
	return result, nil
}

// pluginTTSBackend 把插件声明的 MCP 工具包装成 tool.TTSBackend。
//
// 只做参数映射与结果解析：格式转换、扩展名兜底、落盘、播放都在内核
// （见 tool/speech_backend.go），插件作者不必各自实现一遍。
type pluginTTSBackend struct {
	binding speechProviderBinding
	caller  pluginMediaCaller
}

func (b *pluginTTSBackend) Name() string { return b.binding.Name }

func (b *pluginTTSBackend) Available(*voice.VoiceConfig) bool {
	return b.caller.ServerConnected(b.binding.Server)
}

func (b *pluginTTSBackend) Synthesize(ctx context.Context, _ *voice.VoiceConfig, req tool.SpeechRequest) (*tool.SpeechAudio, error) {
	args := map[string]any{
		"text":     req.Text,
		"voice":    req.Voice,
		"language": req.Language,
		"model":    req.Model,
		"format":   req.Format,
		"speed":    req.Speed,
	}
	if req.Pitch != 0 {
		args["pitch"] = req.Pitch
	}

	raw, err := b.caller.CallTool(ctx, b.binding.Server, b.binding.TTSTool, args)
	if err != nil {
		return nil, fmt.Errorf("plugin speech provider %q: %w", b.binding.Name, err)
	}
	audio, err := parsePluginSpeechResult(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("plugin speech provider %q (tool %s): %w",
			b.binding.Name, b.binding.TTSTool, err)
	}
	return audio, nil
}

// pluginASRBackend 同 pluginTTSBackend，产物是一段文本。
type pluginASRBackend struct {
	binding speechProviderBinding
	caller  pluginMediaCaller
}

func (b *pluginASRBackend) Name() string { return b.binding.Name }

func (b *pluginASRBackend) Available(*voice.VoiceConfig) bool {
	return b.caller.ServerConnected(b.binding.Server)
}

func (b *pluginASRBackend) Transcribe(ctx context.Context, _ *voice.VoiceConfig, req tool.TranscriptionRequest) (string, error) {
	args := map[string]any{
		"audio_b64": base64.StdEncoding.EncodeToString(req.Audio),
		"language":  req.Language,
		"model":     req.Model,
	}
	if req.FileName != "" {
		args["file_name"] = req.FileName
	}

	raw, err := b.caller.CallTool(ctx, b.binding.Server, b.binding.ASRTool, args)
	if err != nil {
		return "", fmt.Errorf("plugin speech provider %q: %w", b.binding.Name, err)
	}
	text, err := parsePluginTranscriptionResult(raw)
	if err != nil {
		return "", fmt.Errorf("plugin speech provider %q (tool %s): %w",
			b.binding.Name, b.binding.ASRTool, err)
	}
	return text, nil
}

// ---- 结果解析 ----

// parsePluginImageResult 从插件工具返回值里提取图片。
// 宽进严出：认不出的形状必须报"期望什么 + 收到了什么"，否则用户只能干瞪眼。
func parsePluginImageResult(raw any) ([]tool.ImageInfo, []string, error) {
	payload, plain, err := normalizeMediaPayload(raw)
	if err != nil {
		return nil, nil, err
	}

	// 纯文本：可能是 URL，也可能是 data URI（有些 server 直接把图内联返回）
	if payload == nil {
		if info, ok := imageInfoFromString(plain); ok {
			return []tool.ImageInfo{info}, nil, nil
		}
		return nil, nil, invalidMediaReply("image", raw)
	}

	var out []tool.ImageInfo
	var revised []string

	// images: [{url|b64...}, ...]
	if list, ok := payload["images"].([]any); ok {
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				if s, ok := item.(string); ok {
					if info, ok := imageInfoFromString(s); ok {
						out = append(out, info)
					}
				}
				continue
			}
			if info, ok := imageInfoFromMap(m); ok {
				out = append(out, info)
			}
			if rp := stringField(m, "revised_prompt"); rp != "" {
				revised = append(revised, rp)
			}
		}
	}

	// 单个对象：{url}/{b64}
	if len(out) == 0 {
		if info, ok := imageInfoFromMap(payload); ok {
			out = append(out, info)
		}
	}

	// urls: [...]
	if urls, ok := payload["urls"].([]any); ok {
		for _, item := range urls {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if info, ok := imageInfoFromString(s); ok {
				out = append(out, info)
			}
		}
	}

	if rp := stringField(payload, "revised_prompt"); rp != "" {
		revised = append(revised, rp)
	}
	if list, ok := payload["revised_prompts"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok && s != "" {
				revised = append(revised, s)
			}
		}
	}

	if len(out) == 0 {
		return nil, nil, invalidMediaReply("image", raw)
	}
	return out, revised, nil
}

func parsePluginVideoResult(raw any) (*tool.VideoResult, error) {
	payload, plain, err := normalizeMediaPayload(raw)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		if LooksLikeMediaURL(plain) {
			return &tool.VideoResult{URL: plain}, nil
		}
		return nil, invalidMediaReply("video", raw)
	}

	for _, key := range []string{"url", "video_url", "video", "output"} {
		if s := stringField(payload, key); s != "" && !strings.HasPrefix(s, "data:") {
			return &tool.VideoResult{URL: s, TaskID: stringField(payload, "task_id")}, nil
		}
	}
	if urls, ok := payload["urls"].([]any); ok && len(urls) > 0 {
		if s, ok := urls[0].(string); ok && s != "" {
			return &tool.VideoResult{URL: s, TaskID: stringField(payload, "task_id")}, nil
		}
	}
	return nil, invalidMediaReply("video", raw)
}

// parsePluginSpeechResult 从插件工具返回值里取出音频。
//
// 与图片解析同一套"宽进严出"：认 data URI / b64 字段 / 直链，认不出时报出
// 期望形状与原文，而不是返回一段空字节让上层当成功。
func parsePluginSpeechResult(ctx context.Context, raw any) (*tool.SpeechAudio, error) {
	payload, plain, err := normalizeMediaPayload(raw)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		if audio, ok := speechAudioFromString(ctx, plain); ok {
			return audio, nil
		}
		return nil, invalidSpeechReply(raw)
	}

	url := firstString(payload, "url", "audio_url", "audio_uri")
	b64 := firstString(payload, "b64", "audio_b64", "audio_base64", "data", "audio")
	mime := firstString(payload, "mime", "mime_type", "content_type")
	format := firstString(payload, "format", "ext")

	// data URI 可能被塞进任何一个字段，统一拆掉
	if b64 != "" {
		if mm, data, ok := SplitDataURI(b64); ok {
			mime, b64 = mm, data
		}
	}
	if url != "" {
		if mm, data, ok := SplitDataURI(url); ok {
			mime, b64, url = mm, data, ""
		}
	}
	if b64 == "" && url == "" {
		return nil, invalidSpeechReply(raw)
	}

	if b64 != "" {
		data, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if derr != nil {
			return nil, fmt.Errorf("audio base64 is invalid: %w", derr)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("audio payload is empty")
		}
		return &tool.SpeechAudio{Data: data, Format: format, Mime: mime}, nil
	}

	if !LooksLikeMediaURL(url) {
		return nil, fmt.Errorf("audio url %q is not an http(s) link", url)
	}
	data, derr := tool.DownloadMediaBytes(ctx, url, tool.MaxAudioDownloadBytes)
	if derr != nil {
		return nil, fmt.Errorf("failed to download audio: %w", derr)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("downloaded audio is empty")
	}
	return &tool.SpeechAudio{Data: data, Format: format, Mime: mime}, nil
}

// speechAudioFromString 处理纯文本形态：data URI 或直链。
func speechAudioFromString(ctx context.Context, s string) (*tool.SpeechAudio, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	if mm, payload, ok := SplitDataURI(s); ok {
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		return &tool.SpeechAudio{Data: data, Mime: mm}, true
	}
	if !LooksLikeMediaURL(s) {
		return nil, false
	}
	data, err := tool.DownloadMediaBytes(ctx, s, tool.MaxAudioDownloadBytes)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return &tool.SpeechAudio{Data: data}, true
}

// parsePluginTranscriptionResult 从插件工具返回值里取出识别文本。
func parsePluginTranscriptionResult(raw any) (string, error) {
	payload, plain, err := normalizeMediaPayload(raw)
	if err != nil {
		return "", err
	}
	if payload == nil {
		if text := strings.TrimSpace(plain); text != "" {
			return text, nil
		}
		return "", invalidASRReply(raw)
	}
	if text := firstString(payload, "text", "transcript", "transcription", "result"); text != "" {
		return text, nil
	}
	// 有些 server 把结果包在 {"data": {"text": ...}} 里
	if nested, ok := payload["data"].(map[string]any); ok {
		if text := firstString(nested, "text", "transcript", "transcription", "result"); text != "" {
			return text, nil
		}
	}
	return "", invalidASRReply(raw)
}

func invalidSpeechReply(raw any) error {
	return fmt.Errorf(`no usable audio in reply: expected {"url"|"audio_url"} / {"b64"|"audio_b64"} / {"audio":"data:audio/...;base64,..."} or a plain URL / data URI, got: %s`, truncateForError(raw))
}

func invalidASRReply(raw any) error {
	return fmt.Errorf(`no transcription text in reply: expected {"text"|"transcript"} or a plain string, got: %s`, truncateForError(raw))
}

// truncateForError 按 rune 截断原文（字节截断会把中文切成乱码，本项目老坑）。
func truncateForError(raw any) string {
	text := fmt.Sprintf("%v", raw)
	runes := []rune(text)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return text
}

// audioFormatFromMime 把音频 MIME 映射成扩展名，未知返回空串（由内核按请求格式兜底）。
func audioFormatFromMime(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	case "audio/webm":
		return "webm"
	}
	return ""
}

// normalizeMediaPayload 把 MCP 工具的返回值归一成"一个对象 或 一段纯文本"。
//
// mcp.Manager.CallTool 对文本型结果会把 content 拼成一个字符串返回，所以最常见的
// 入参是 string（里面装着 JSON）。对象形态则是为了兼容直接返回结构体的情况。
func normalizeMediaPayload(raw any) (map[string]any, string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, "", fmt.Errorf("media provider returned empty result")
	case map[string]any:
		return v, "", nil
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil, "", fmt.Errorf("media provider returned empty result")
		}
		if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			var obj any
			if err := json.Unmarshal([]byte(text), &obj); err == nil {
				switch parsed := obj.(type) {
				case map[string]any:
					return parsed, "", nil
				case []any:
					// 顶层就是数组：当作 images 列表
					return map[string]any{"images": parsed}, "", nil
				}
			}
			// JSON 解析失败也往下走纯文本逻辑：有些 server 会返回带引号的裸 URL
		}
		return nil, text, nil
	case []any:
		return map[string]any{"images": v}, "", nil
	default:
		return nil, "", fmt.Errorf("media provider returned unsupported %T (expected text or object)", raw)
	}
}

// invalidMediaReply 构造"认不出来"的错误，附带截断后的原文。
// 截断按 rune，避免把中文/emoji 切成乱码（本项目的老坑）。
func invalidMediaReply(kind string, raw any) error {
	text := fmt.Sprintf("%v", raw)
	runes := []rune(text)
	if len(runes) > 200 {
		text = string(runes[:200]) + "..."
	}
	if kind == "image" {
		return fmt.Errorf(`no usable image in reply: expected {"url"} / {"urls":[]} / {"images":[{"url"|"b64"}]} / {"b64"} or a plain URL, got: %s`, text)
	}
	return fmt.Errorf(`no usable video url in reply: expected {"url"} / {"video_url"} / {"urls":[]} or a plain URL, got: %s`, text)
}

// imageInfoFromString 处理纯文本 / data URI 形态。
func imageInfoFromString(s string) (tool.ImageInfo, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return tool.ImageInfo{}, false
	}
	if mime, payload, ok := SplitDataURI(s); ok {
		return tool.ImageInfo{Base64Data: payload, Mime: mime, Format: imageFormatFromMime(mime)}, true
	}
	if LooksLikeMediaURL(s) {
		return tool.ImageInfo{URL: s, Format: imageFormatFromURL(s)}, true
	}
	return tool.ImageInfo{}, false
}

func imageInfoFromMap(m map[string]any) (tool.ImageInfo, bool) {
	url := firstString(m, "url", "image_url", "image", "src")
	b64 := firstString(m, "b64", "base64", "b64_json", "data")
	mime := firstString(m, "mime", "mime_type", "content_type")
	format := firstString(m, "format", "ext")

	if b64 != "" {
		if mm, payload, ok := SplitDataURI(b64); ok {
			b64, mime = payload, mm
		}
	}
	if url != "" {
		// 有些 server 把 data URI 塞在 url 字段里
		if mm, payload, ok := SplitDataURI(url); ok {
			b64, mime, url = payload, mm, ""
		}
	}

	if url == "" && b64 == "" {
		return tool.ImageInfo{}, false
	}
	if format == "" {
		if url != "" {
			format = imageFormatFromURL(url)
		} else {
			format = imageFormatFromMime(mime)
		}
	}
	return tool.ImageInfo{URL: url, Base64Data: b64, Format: format, Mime: mime}, true
}

func imageFormatFromURL(u string) string {
	lower := strings.ToLower(u)
	// 去查询串后再比后缀，避免 "x.png?token=..." 判不出来
	if i := strings.IndexByte(lower, '?'); i >= 0 {
		lower = lower[:i]
	}
	for _, ext := range []string{"png", "jpg", "jpeg", "webp", "gif", "bmp"} {
		if strings.HasSuffix(lower, "."+ext) {
			if ext == "jpeg" {
				return "jpg"
			}
			return ext
		}
	}
	return "png"
}

func imageFormatFromMime(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	}
	return "png"
}

// SplitDataURI 拆 "data:image/png;base64,xxxx"。
func SplitDataURI(s string) (mime, payload string, ok bool) {
	if !strings.HasPrefix(s, "data:") {
		return "", "", false
	}
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return "", "", false
	}
	meta, data := s[len("data:"):comma], s[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	mime = strings.TrimSuffix(meta, ";base64")
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	if data == "" {
		return "", "", false
	}
	return mime, data, true
}

// LooksLikeMediaURL 判断是否像个能下载的直链。
// 只排除明显不是 URL 的文本，不做协议白名单——本地 ComfyUI 返回 127.0.0.1
// 的图片地址是正常场景，禁私网会把本地后端一起打死。
func LooksLikeMediaURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \n\t") {
		return false
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringField(m, k); s != "" {
			return s
		}
	}
	return ""
}

// ---- 生命周期挂钩 ----

// RegisterMediaProviders 注册本插件声明的媒体后端（Start 时调用）。
//
// 注册是幂等的：重复 Start（reload）只是用新实例覆盖同名条目。注册失败不影响
// 插件其余部分——扩展声明的错误只进 Warnings。
//
// 这里会校验 binding.Server 确实是 mcp.json 里声明过的条目：写错 server 名是
// 这类扩展最常见的错误，而它的表现是"provider 注册了但永远不可用"，比直接报
// 一个 warning 难查得多。
func (r *Runtime) RegisterMediaProviders() {
	spec, warnings := pluginMediaProviderSpec(r.plugin)
	for _, w := range warnings {
		r.appendWarning(w)
	}
	if len(spec.Image) == 0 && len(spec.Video) == 0 && len(spec.Speech) == 0 {
		return
	}

	owner := r.pluginNameOrEmpty()
	if owner == "" {
		return
	}

	declared := r.declaredMCPServers()
	for _, binding := range spec.Image {
		if !declared[binding.Server] {
			r.appendWarning(fmt.Sprintf(
				"media-provider.image: server %q is not declared in mcp.json, provider %q ignored",
				binding.Server, binding.Name))
			continue
		}
		tool.RegisterImageBackendOwned(owner, &pluginImageBackend{
			binding: binding,
			caller:  r,
		})
	}
	for _, binding := range spec.Video {
		if !declared[binding.Server] {
			r.appendWarning(fmt.Sprintf(
				"media-provider.video: server %q is not declared in mcp.json, provider %q ignored",
				binding.Server, binding.Name))
			continue
		}
		tool.RegisterVideoBackendOwned(owner, &pluginVideoBackend{
			binding: binding,
			caller:  r,
		})
	}
	for _, binding := range spec.Speech {
		if !declared[binding.Server] {
			r.appendWarning(fmt.Sprintf(
				"media-provider.speech: server %q is not declared in mcp.json, provider %q ignored",
				binding.Server, binding.Name))
			continue
		}
		// 能力按绑定分对象注册：只声明 tts_tool 的 provider 不会出现在识别表里，
		// 分辨率靠的是"注册进哪张表"，而不是方法里再判空。
		if binding.TTSTool != "" {
			tool.RegisterSpeechBackendOwned(owner, &pluginTTSBackend{binding: binding, caller: r})
		}
		if binding.ASRTool != "" {
			tool.RegisterSpeechBackendOwned(owner, &pluginASRBackend{binding: binding, caller: r})
		}
	}
}

// declaredMCPServers 返回插件在 mcp.json 里声明过的条目名集合。
func (r *Runtime) declaredMCPServers() map[string]bool {
	known := make(map[string]bool)
	if r.plugin == nil {
		return known
	}
	for _, ms := range r.plugin.MCPServers {
		known[ms.Name] = true
	}
	return known
}

// UnregisterMediaProviders 移除本插件贡献的后端（Stop 时调用）。
// 按 owner 删，不会误伤其他插件的同名 provider。
func (r *Runtime) UnregisterMediaProviders() {
	owner := r.pluginNameOrEmpty()
	if owner == "" {
		return
	}
	tool.UnregisterImageBackendsByOwner(owner)
	tool.UnregisterVideoBackendsByOwner(owner)
	tool.UnregisterSpeechBackendsByOwner(owner)
}

// ServerConnected 报告某个条目是否有活连接（供插件媒体后端判断可用性）。
func (r *Runtime) ServerConnected(serverName string) bool {
	mgr := r.manager()
	if mgr == nil {
		return false
	}
	return mgr.IsConnected(r.key(serverName))
}

// appendWarning 去重后追加到插件警告列表（reload 会重复走 Start）。
func (r *Runtime) appendWarning(msg string) {
	if r.plugin == nil || msg == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.plugin.Warnings {
		if existing == msg {
			return
		}
	}
	r.plugin.Warnings = append(r.plugin.Warnings, msg)
}
