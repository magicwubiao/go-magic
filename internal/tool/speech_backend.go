package tool

// speech_backend.go — TTS / ASR 使用注册表后端的执行路径。
//
// 内置语音能力仍走 voice.Manager（见 tts_tool.go / asr_tool.go 的回落分支），
// 这里只处理"provider 名字命中了注册表"的情况：插件贡献的后端拿不到
// voice.VoiceConfig 也无所谓，它靠自己的 MCP 连接状态判断可用性。
//
// 与图片/视频同一条原则：**落盘属于内核**。后端只回字节，目录、扩展名、
// 格式转换、播放全部在这里做，插件作者不必各自实现一遍。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/voice"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// speechCall 汇总一次 TTS 调用的参数（位置参数已经 10 个，不这样收会读错）。
type speechCall struct {
	provider string
	text     string
	voice    string
	language string
	model    string
	format   string
	speed    float64
	pitch    float64
	play     bool
	outPath  string
}

// voiceDefaultProvider 返回 config.json 的 voice.provider，缺省 openai。
func voiceDefaultProvider() string {
	cfg, err := config.Load()
	if err == nil && cfg != nil && cfg.Voice != nil {
		if p := strings.TrimSpace(cfg.Voice.Provider); p != "" {
			return p
		}
	}
	return "openai"
}

// loadVoiceConfig 读配置里的 voice 段（可能为 nil，调用方需容忍）。
func loadVoiceConfig() *voice.VoiceConfig {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.Voice
}

// LoadVoiceConfig 是 loadVoiceConfig 的导出版本（服务端设置页/诊断用）。
func LoadVoiceConfig() *voice.VoiceConfig { return loadVoiceConfig() }

// synthesizeWithBackend 走注册表后端合成语音并落盘。
func (t *TTSTool) synthesizeWithBackend(ctx context.Context, synth TTSBackend, call speechCall) (interface{}, error) {
	voiceCfg := loadVoiceConfig()
	if !synth.Available(voiceCfg) {
		return map[string]interface{}{
			"status":   "not_configured",
			"provider": call.provider,
			"message":  fmt.Sprintf("Speech backend %q is registered but not available right now.", call.provider),
			"configuration_help": "Check that the plugin providing this backend is installed and its MCP entry " +
				"(or HTTP endpoint) is connected.",
		}, nil
	}

	audio, err := synth.Synthesize(ctx, voiceCfg, SpeechRequest{
		Text:     call.text,
		Voice:    call.voice,
		Language: call.language,
		Model:    call.model,
		Format:   call.format,
		Speed:    call.speed,
		Pitch:    call.pitch,
	})
	if err != nil {
		return nil, fmt.Errorf("TTS failed: %w", err)
	}
	if audio == nil || len(audio.Data) == 0 {
		// 空字节 + nil error 必须当失败：模型会当成"生成成功但文件是 0 字节"，
		// 用户拿到的是一段放不出来的音频。
		return nil, fmt.Errorf("speech backend %q returned empty audio", call.provider)
	}

	data := audio.Data
	actual := strings.ToLower(strings.TrimSpace(audio.Format))
	if actual == "" {
		actual = extensionFromMime(audio.Mime)
	}
	if actual == "" {
		actual = call.format
	}
	outPath := call.outPath
	if actual != call.format {
		if converted, cerr := voice.ConvertAudioFormat(data, call.format); cerr == nil {
			data, actual = converted, call.format
		} else {
			// 转不了就保留真实扩展名，不能把 wav 字节写成 .mp3
			outPath = swapExt(outPath, actual)
		}
	}

	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to save audio: %w", err)
	}
	if call.play {
		// 播放失败不影响产物，与内置路径一致
		_ = voice.PlayAudio(data)
	}

	return map[string]interface{}{
		"status":      "success",
		"text":        call.text,
		"text_length": len([]rune(call.text)),
		"provider":    call.provider,
		"voice":       call.voice,
		"language":    call.language,
		"speed":       call.speed,
		"pitch":       call.pitch,
		"output_path": outPath,
		"format":      actual,
		"file_size":   len(data),
		"played":      call.play,
	}, nil
}

// transcribeWithBackend 走注册表后端做语音识别。
func (t *ASRTool) transcribeWithBackend(ctx context.Context, asr ASRBackend, req TranscriptionRequest, provider, audioSource, detectedFormat string, fileSize int) (interface{}, error) {
	voiceCfg := loadVoiceConfig()
	if !asr.Available(voiceCfg) {
		return map[string]interface{}{
			"status":   "not_configured",
			"provider": provider,
			"message":  fmt.Sprintf("Speech backend %q is registered but not available right now.", provider),
			"configuration_help": "Check that the plugin providing this backend is installed and its MCP entry " +
				"(or HTTP endpoint) is connected.",
		}, nil
	}

	text, err := asr.Transcribe(ctx, voiceCfg, req)
	if err != nil {
		return nil, fmt.Errorf("ASR failed: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("speech backend %q returned empty transcription", provider)
	}

	return map[string]interface{}{
		"status":       "success",
		"text":         text,
		"provider":     provider,
		"language":     req.Language,
		"audio_source": audioSource,
		"audio_format": detectedFormat,
		"file_size":    fileSize,
	}, nil
}

// swapExt 把路径扩展名换成 ext（ext 不含点）。ext 为空时原样返回。
func swapExt(path, ext string) string {
	if ext == "" {
		return path
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + "." + ext
}
