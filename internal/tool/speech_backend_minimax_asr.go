package tool

// speech_backend_minimax_asr.go — MiniMax 语音识别后端（S2T / speech_to_text 接口）。
//
// 与同文件家族的 TTS 后端（speech_backend_minimax.go）共用 minimaxSpeechBackend
// 结构体：同一对象实现 TTSBackend 与 ASRBackend，注册时自动进两张表，不需要
// 新的注册代码。凭据也完全一致（MINIMAX_API_KEY / voice.providers.minimax.api_key）。
//
// 契约要点（https://platform.minimaxi.com/docs/api-reference/speech-to-text）：
//
//	POST {base}/v1/speech_to_text
//	Content-Type: multipart/form-data
//	  model=asr-1.0, response_format=json, file=<音频>
//	Header: Authorization: Bearer <key>
//	        Language: <BCP-47，可选——官方文档明确说 language 走请求头>
//	→ {"text":"...","duration":26.3,"base_resp":{"status_code":0}}
//
// 与 T2A 的两点关键差异：
//  1. **不需要 GroupId**（T2A 的必填查询参数在这里不存在，别把 GroupId 检查搬过来）；
//  2. 响应是普通 JSON 字段 text，不是十六进制音频。
//
// 硬性约束（服务端 400/413 而不是截断）：
//   - 时长 ≤ 500 秒 / 请求
//   - 大小 ≤ 50 MB / 请求；不支持无容器的裸 PCM

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// minimaxASRDefaultModel 语音识别的默认模型。
//
// 注意不能复用 catalog 的 DefaultModel（那是 speech-02-hd，TTS 用的）：
// 同一个厂商条目下两种能力各有各的默认模型，识别这边在方法里自兜底。
const minimaxASRDefaultModel = "asr-1.0"

// minimaxASRMaxAudioBytes 单次请求的音频大小上限（官方 50MB，超出返回 413）。
const minimaxASRMaxAudioBytes = 50 << 20

// minimaxASRTimeout 长音频识别耗时远超普通请求（500 秒音频可能要识别几分钟），
// 共享 client 的 30s 全局超时在这里必然不够，单独给一份。
const minimaxASRTimeout = 5 * time.Minute

// Transcribe 实现 ASRBackend：把音频交给 MiniMax S2T 接口换回文本。
func (minimaxSpeechBackend) Transcribe(ctx context.Context, cfg *voice.VoiceConfig, req TranscriptionRequest) (string, error) {
	if len(req.Audio) == 0 {
		return "", fmt.Errorf("%s: audio is empty", ProviderLabel(MediaKindSpeech, minimaxSpeechProviderName))
	}
	// 服务端对超限文件返回 413 且不截断；在客户端提前拦住并把修复动作讲清楚，
	// 否则模型只会看到一句 "HTTP 413"，不知道该转码还是切段。
	if len(req.Audio) > minimaxASRMaxAudioBytes {
		return "", fmt.Errorf("%s: audio is %d bytes, the limit is %d (50MB); convert to mono 16kHz mp3/aac/opus or split the clip",
			ProviderLabel(MediaKindSpeech, minimaxSpeechProviderName), len(req.Audio), minimaxASRMaxAudioBytes)
	}

	info, _ := ProviderInfo(MediaKindSpeech, minimaxSpeechProviderName)
	base := info.BaseURL(minimaxSpeechBaseURL(cfg))
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = minimaxASRDefaultModel
	}

	// 文件名带扩展名有助于服务端识别容器格式；工具层对 base64/URL 来源传来的
	// 名字可能为空或没有扩展名（"base64 data"），统一回落成 .mp3。
	name := strings.TrimSpace(req.FileName)
	if name == "" || filepath.Ext(name) == "" {
		name = "audio.mp3"
	}

	var body bytes.Buffer
	writer := NewMultipartWriter(&body)
	if err := writer.WriteFile("file", name, req.Audio); err != nil {
		return "", fmt.Errorf("failed to add audio to request: %w", err)
	}
	if err := writer.WriteField("model", model); err != nil {
		return "", fmt.Errorf("failed to add model to request: %w", err)
	}
	// 一次只认 json：srt/vtt/verbose_json 是字幕/分说话人场景，识别工具的
	// 契约是"返回纯文本"，要多格式输出时再扩展 TranscriptionRequest。
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("failed to add response_format to request: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to build multipart body: %w", err)
	}

	endpoint := base + "/v1/speech_to_text"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("Authorization", "Bearer "+minimaxSpeechAPIKey(cfg))
	// language 是请求头不是表单字段（官方 API reference 把它列在请求头一节）；
	// 不传时服务端启用混合语言识别，正好匹配工具层"不填自动检测"的语义。
	if lang := strings.TrimSpace(req.Language); lang != "" {
		httpReq.Header.Set("Language", lang)
	}

	client := &http.Client{Timeout: minimaxASRTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", withProviderContext(err, MediaKindSpeech, minimaxSpeechProviderName, model)
	}
	defer resp.Body.Close()

	// 正常响应是几 KB 的 JSON；上限防的是"网关回一个 HTML 错误页"读爆内存。
	const maxResponseBytes = 8 << 20
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", withProviderContext(&mediaAPIError{
			Status: resp.StatusCode,
			Detail: extractAPIErrorDetail(respBody),
		}, MediaKindSpeech, minimaxSpeechProviderName, model)
	}

	var parsed struct {
		Text     string  `json:"text"`
		Duration float64 `json:"duration"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse %s response: %w (body: %s)",
			info.Label, err, truncateRunes(string(respBody), 300))
	}
	// MiniMax 的业务失败经常包着 HTTP 200：和 T2A 一样必须看 base_resp。
	if parsed.BaseResp.StatusCode != 0 {
		return "", &mediaAPIError{
			Provider: minimaxSpeechProviderName, Label: info.Label, Status: 400,
			Detail: fmt.Sprintf("status_code=%d: %s", parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg),
			Model:  model, Console: info.ConsoleURL,
		}
	}
	if strings.TrimSpace(parsed.Text) == "" {
		return "", fmt.Errorf("%s returned an empty transcription (body: %s)",
			info.Label, truncateRunes(string(respBody), 300))
	}
	return parsed.Text, nil
}
