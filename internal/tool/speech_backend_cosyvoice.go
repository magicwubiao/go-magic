package tool

// speech_backend_cosyvoice.go — 阿里云 CosyVoice 语音合成后端（DashScope）。
//
// 契约要点（https://help.aliyun.com/zh/model-studio/cosyvoice-tts-http-api）：
//
//	POST {base}/services/aigc/multimodal-generation/generation
//	{"model":"cosyvoice-v2",
//	 "input":{"text":"...","voice":"longxiaochun_v2"},
//	 "parameters":{"format":"mp3","sample_rate":24000,"rate":1.0}}
//	→ {"output":{"audio":{"url":"https://...","expires_at":...}}}
//
// 三个必须记住的约束：
//  1. **同步接口但产物是 URL**（不是字节），URL 有效期 24 小时，必须当场下载。
//     与通义万相共用同一个百炼 API-KEY，所以用户配了一次就能同时用视频和语音。
//  2. **声音 ID 与模型版本绑定**：cosyvoice-v2 用 longxiaochun_v2 这类带 _v2 后缀的
//     声音，v3 系列用 longanhuan 这类。串版本会直接报 voice not found——这不是
//     鉴权问题，但错误信息看起来很像，所以 catalog 里把默认声音和模型成对写死。
//  3. format 只认 mp3 / wav / pcm / opus，传 ogg 会 400。这里统一映射到 mp3，
//     落盘扩展名由内核按实际格式决定（不会出现 wav 字节写成 .mp3）。
//
// 与图片/视频同一条原则：后端只回字节，落盘、播放、格式转换都在内核。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// cosyVoiceEnvKey 允许只用环境变量配置（与 dashscope 视频后端共用一个 key 变量）。
const cosyVoiceEnvKey = "DASHSCOPE_API_KEY"

type cosyVoiceBackend struct{}

func (cosyVoiceBackend) Name() string { return "cosyvoice" }

// Available 报告当前是否具备合成条件。
//
// key 的三个来源按优先级：voice.providers.cosyvoice.api_key（设置页按厂商填）
// → voice.api_key（通用）→ DASHSCOPE_API_KEY（部署环境）。三条都空才算不可用，
// 否则"设置了环境变量但工具不出现"会变成一个很难查的问题。
func (cosyVoiceBackend) Available(cfg *voice.VoiceConfig) bool {
	return cosyVoiceAPIKey(cfg) != ""
}

func (cosyVoiceBackend) Synthesize(ctx context.Context, cfg *voice.VoiceConfig, req SpeechRequest) (*SpeechAudio, error) {
	info, _ := ProviderInfo(MediaKindSpeech, "cosyvoice")
	base := info.BaseURL(voiceBaseURL(cfg))
	model := info.Model(req.Model)

	voiceID := strings.TrimSpace(req.Voice)
	if voiceID == "" && cfg != nil {
		voiceID = strings.TrimSpace(cfg.Voice)
	}
	if voiceID == "" {
		voiceID = info.DefaultVoice
	}

	format := cosyVoiceFormat(req.Format)
	text := strings.TrimSpace(req.Text)
	if text == "" {
		// 空文本 + nil error 会让上层写出一段 0 字节音频，用户拿到放不出来的文件。
		return nil, fmt.Errorf("%s: text is empty", info.Label)
	}

	parameters := map[string]any{
		"format":      format,
		"sample_rate": 24000,
	}
	if req.Speed > 0 && req.Speed != 1.0 {
		// DashScope 用 rate 表达语速，范围 0.5~2.0，与 voice.speed 语义一致。
		parameters["rate"] = req.Speed
	}

	payload := map[string]any{
		"model": model,
		"input": map[string]any{
			"text":  text,
			"voice": voiceID,
		},
		"parameters": parameters,
	}

	body, err := mediaRequest(ctx, 0, "POST", base+"/services/aigc/multimodal-generation/generation", payload,
		map[string]string{"Authorization": "Bearer " + cosyVoiceAPIKey(cfg)})
	if err != nil {
		return nil, withProviderContext(err, MediaKindSpeech, "cosyvoice", model)
	}

	var resp struct {
		Output struct {
			Audio struct {
				URL string `json:"url"`
				ID  string `json:"id"`
			} `json:"audio"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w (body: %s)",
			info.Label, err, truncateRunes(string(body), 300))
	}
	if resp.Code != "" {
		return nil, &mediaAPIError{
			Provider: "cosyvoice", Label: info.Label, Status: 400,
			Detail: fmt.Sprintf("code=%s: %s", resp.Code, resp.Message),
			Model:  model, Console: info.ConsoleURL,
		}
	}
	if resp.Output.Audio.URL == "" {
		return nil, fmt.Errorf("%s returned no audio URL (body: %s)",
			info.Label, truncateRunes(string(body), 300))
	}

	// 音频 URL 24 小时过期（官方文档明确写了 expires_at），所以必须当场取回字节。
	data, err := DownloadMediaBytes(ctx, resp.Output.Audio.URL, maxAudioDownloadBytes)
	if err != nil {
		return nil, fmt.Errorf("%s synthesized audio but download failed: %w", info.Label, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s returned an empty audio file", info.Label)
	}

	return &SpeechAudio{Data: data, Format: format, Mime: mimeForAudioFormat(format)}, nil
}

// cosyVoiceAPIKey 按优先级取出可用的 API Key。
func cosyVoiceAPIKey(cfg *voice.VoiceConfig) string {
	if cfg != nil {
		if cfg.Providers != nil {
			if pc, ok := cfg.Providers["cosyvoice"]; ok {
				if k := strings.TrimSpace(pc.APIKey); k != "" {
					return k
				}
			}
		}
		if k := strings.TrimSpace(cfg.APIKey); k != "" {
			return k
		}
	}
	return strings.TrimSpace(os.Getenv(cosyVoiceEnvKey))
}

// voiceBaseURL 取 voice 配置里的自定义地址（含按厂商覆盖），为空时由 catalog 兜底。
func voiceBaseURL(cfg *voice.VoiceConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.Providers != nil {
		if pc, ok := cfg.Providers["cosyvoice"]; ok && strings.TrimSpace(pc.APIURL) != "" {
			return pc.APIURL
		}
	}
	return cfg.APIURL
}

// cosyVoiceFormat 把期望格式映射到 DashScope 接受的取值。
//
// 不支持的格式统一回落 mp3，而不是报错：用户要 ogg 只是"想要个压缩格式"，
// 给 mp3 满足意图；返回的 SpeechAudio.Format 会如实写 mp3，落盘扩展名跟着它走。
func cosyVoiceFormat(requested string) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "wav", "pcm", "opus", "mp3":
		return strings.ToLower(strings.TrimSpace(requested))
	}
	return "mp3"
}

// mimeForAudioFormat 音频格式 → MIME。
func mimeForAudioFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg", "opus":
		return "audio/ogg"
	case "pcm":
		return "audio/L16"
	}
	return ""
}
