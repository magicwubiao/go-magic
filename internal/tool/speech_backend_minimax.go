package tool

// speech_backend_minimax.go — MiniMax 语音合成后端（T2A v2 同步 HTTP 接口）。
//
// 契约要点（https://platform.minimaxi.com/docs/guides/text-to-speech）：
//
//	POST {base}/v1/t2a_v2?GroupId={group_id}
//	{"model":"speech-02-hd","text":"...","stream":false,
//	 "voice_setting":{"voice_id":"male-qn-qingse","speed":1.0,"vol":1.0,"pitch":0},
//	 "audio_setting":{"sample_rate":32000,"bitrate":128000,"format":"mp3","channel":1}}
//	→ {"data":{"audio":"<hex>"},"base_resp":{"status_code":0}}
//
// 三个必须记住的坑：
//  1. **GroupId 是必填查询参数**（账号组 ID，与 API Key 在控制台同一页拿），
//     缺了报 invalid params——看起来像 key 错，其实不是鉴权问题。
//  2. **data.audio 是十六进制字符串，不是字节**：直接落盘得到的是文本文件。
//     必须 hex 解码后再交给内核（这是 MiniMax 全家接口的统一怪癖）。
//  3. 域名分国内外：默认 api.minimaxi.com（中国大陆账号），海外账号是
//     api.minimax.io，通过 api_url 覆盖——两边 API 形状一致。
//
// 与图片/视频同一条原则：后端只回字节，落盘、播放、格式转换都在内核。

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// minimaxSpeechEnvKey / minimaxGroupEnvKey 允许只用环境变量配置（与 minimax
// 视频/图片后端共用 MINIMAX_API_KEY；GroupId 是语音接口独有的必填项）。
const (
	minimaxSpeechEnvKey = "MINIMAX_API_KEY"
	minimaxGroupEnvKey  = "MINIMAX_GROUP_ID"
	// 注册名与 minimax 图片/视频后端一致，同一个厂商一个名字。
	minimaxSpeechProviderName = "minimax"
)

type minimaxSpeechBackend struct{}

func (minimaxSpeechBackend) Name() string { return minimaxSpeechProviderName }

// Available 报告当前是否具备合成条件。
//
// key 的来源按优先级：voice.providers.minimax.api_key（设置页按厂商填）
// → voice.api_key（通用）→ MINIMAX_API_KEY（部署环境）。
func (minimaxSpeechBackend) Available(cfg *voice.VoiceConfig) bool {
	return minimaxSpeechAPIKey(cfg) != ""
}

func (minimaxSpeechBackend) Synthesize(ctx context.Context, cfg *voice.VoiceConfig, req SpeechRequest) (*SpeechAudio, error) {
	info, _ := ProviderInfo(MediaKindSpeech, minimaxSpeechProviderName)
	base := info.BaseURL(minimaxSpeechBaseURL(cfg))
	model := info.Model(req.Model)

	voiceID := strings.TrimSpace(req.Voice)
	if voiceID == "" && cfg != nil {
		voiceID = strings.TrimSpace(cfg.Voice)
	}
	if voiceID == "" {
		voiceID = info.DefaultVoice
	}

	groupID := strings.TrimSpace(minimaxSpeechGroupID(cfg))
	if groupID == "" {
		// GroupId 缺了接口必挂，而且报错文案完全不提 GroupId——在客户端侧
		// 提前拦住，把修复动作直接告诉模型/用户。
		return nil, &mediaAPIError{
			Provider: minimaxSpeechProviderName, Label: info.Label, Status: 400,
			Detail: "GroupId is required: set voice.providers.minimax.group_id in config.json " +
				"or the MINIMAX_GROUP_ID environment variable (find it next to the API key in the MiniMax console)",
			Console: info.ConsoleURL,
		}
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		// 空文本 + nil error 会让上层写出一段 0 字节音频。
		return nil, fmt.Errorf("%s: text is empty", info.Label)
	}

	// voice_setting：speed 范围 [0.5,2] 与工具语义一致；pitch 范围 [-12,12]，
	// 工具层已钳到 [-10,10]，原样透传即可。
	voiceSetting := map[string]any{
		"voice_id": voiceID,
		"vol":      1.0,
	}
	if req.Speed > 0 {
		voiceSetting["speed"] = req.Speed
	}
	if req.Pitch != 0 {
		voiceSetting["pitch"] = int(req.Pitch)
	}

	format := minimaxSpeechFormat(req.Format)
	payload := map[string]any{
		"model":         model,
		"text":          text,
		"stream":        false,
		"voice_setting": voiceSetting,
		"audio_setting": map[string]any{
			"sample_rate": 32000,
			"bitrate":     128000,
			"format":      format,
			"channel":     1,
		},
	}

	endpoint := base + "/v1/t2a_v2?GroupId=" + url.QueryEscape(groupID)
	body, err := mediaRequest(ctx, 0, "POST", endpoint, payload,
		map[string]string{"Authorization": "Bearer " + minimaxSpeechAPIKey(cfg)})
	if err != nil {
		return nil, withProviderContext(err, MediaKindSpeech, minimaxSpeechProviderName, model)
	}

	var resp struct {
		Data struct {
			Audio string `json:"audio"`
		} `json:"data"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w (body: %s)",
			info.Label, err, truncateRunes(string(body), 300))
	}
	if resp.BaseResp.StatusCode != 0 {
		return nil, &mediaAPIError{
			Provider: minimaxSpeechProviderName, Label: info.Label, Status: 400,
			Detail: fmt.Sprintf("status_code=%d: %s", resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg),
			Model:  model, Console: info.ConsoleURL,
		}
	}
	if resp.Data.Audio == "" {
		return nil, fmt.Errorf("%s returned no audio (body: %s)",
			info.Label, truncateRunes(string(body), 300))
	}

	// data.audio 是十六进制字符串——不解码落盘就是一坨文本。
	data, err := hex.DecodeString(strings.TrimSpace(resp.Data.Audio))
	if err != nil {
		return nil, fmt.Errorf("%s returned malformed hex audio: %w", info.Label, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s returned an empty audio file", info.Label)
	}

	return &SpeechAudio{Data: data, Format: format, Mime: mimeForAudioFormat(format)}, nil
}

// minimaxSpeechAPIKey 按优先级取出可用的 API Key。
func minimaxSpeechAPIKey(cfg *voice.VoiceConfig) string {
	if cfg != nil {
		if pc, ok := cfg.Providers[minimaxSpeechProviderName]; ok {
			if k := strings.TrimSpace(pc.APIKey); k != "" {
				return k
			}
		}
		if k := strings.TrimSpace(cfg.APIKey); k != "" {
			return k
		}
	}
	return strings.TrimSpace(os.Getenv(minimaxSpeechEnvKey))
}

// minimaxSpeechGroupID 取账号组 ID：providers.minimax.group_id → MINIMAX_GROUP_ID。
func minimaxSpeechGroupID(cfg *voice.VoiceConfig) string {
	if cfg != nil {
		if pc, ok := cfg.Providers[minimaxSpeechProviderName]; ok {
			if g := strings.TrimSpace(pc.GroupID); g != "" {
				return g
			}
		}
	}
	return strings.TrimSpace(os.Getenv(minimaxGroupEnvKey))
}

// minimaxSpeechBaseURL 取 voice 配置里的自定义地址（含按厂商覆盖）。
// 注意不能复用 voiceBaseURL：那个按 cosyvoice 键取厂商覆盖。
func minimaxSpeechBaseURL(cfg *voice.VoiceConfig) string {
	if cfg != nil {
		if pc, ok := cfg.Providers[minimaxSpeechProviderName]; ok && strings.TrimSpace(pc.APIURL) != "" {
			return pc.APIURL
		}
		if strings.TrimSpace(cfg.APIURL) != "" {
			return cfg.APIURL
		}
	}
	// catalog 的 DefaultBaseURL 是海外站；中国大陆账号的默认端点不同，
	// 这里按国内用户为主选 minimaxi.com，海外账号用 api_url 覆盖。
	return "https://api.minimaxi.com"
}

// minimaxSpeechFormat 把期望格式映射到 T2A v2 接受的取值。
//
// 同步接口稳定支持 mp3 / pcm / flac；wav 官方文档只在部分入口列出，
// 统一回落 mp3 而不是报错——SpeechAudio.Format 如实写 mp3，扩展名跟着走。
func minimaxSpeechFormat(requested string) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "mp3", "pcm", "flac":
		return strings.ToLower(strings.TrimSpace(requested))
	}
	return "mp3"
}
