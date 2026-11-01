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
	"sync"
	"time"

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
	if cfg := loadVoiceConfig(); cfg != nil {
		if p := strings.TrimSpace(cfg.Provider); p != "" {
			return p
		}
	}
	return "openai"
}

// voiceConfigCacheTTL 语音配置缓存的兜底存活时间。
//
// 为什么需要缓存：内置语音后端（cosyvoice）注册进注册表之后，
// registeredTTSBackendAvailable() 每次 ListWithSchemas 都会遍历一遍，而
// config.Load() 是**每次读盘 + 反序列化**。工具清单在每轮对话都要算，
// 不缓存等于把磁盘 IO 放在热路径上（这一点在内置后端加入后从"偶尔"变成"每次"）。
const voiceConfigCacheTTL = 15 * time.Second

// voiceConfigCache 带"文件指纹"判定的语音配置缓存。
//
// 只靠 TTL 不够：TTL 保证的是**时间上**的新鲜，不是**内容上**的新鲜。真实踩到的
// 场景有两个——设置页保存（已由 ResetEnvProbes 显式失效覆盖）、以及测试里临时
// 换掉 magic home 后写一份新 config.json（TTL 内仍读到上一份，于是用例之间互相
// 污染、时绿时红）。所以真正的失效判据是配置文件本身的 mtime+size：
// stat 一次是微秒级，比"读文件 + 反序列化"便宜两个数量级，也足够准确。
type voiceConfigCacheT struct {
	mu    sync.RWMutex
	valid bool
	at    time.Time
	stamp string
	cfg   *voice.VoiceConfig
}

var voiceConfigCache voiceConfigCacheT

// voiceConfigStamp 返回语音配置文件的指纹（路径 + mtime + size；缺失时记 missing）。
//
// 文件不存在也要有稳定指纹：否则"没有 config.json"的机器每次都会判定变化、
// 每次都去读盘，缓存等于没做。
func voiceConfigStamp() string {
	home := config.GetMagicHome()
	if home == "" {
		return "no-home"
	}
	path := filepath.Join(home, config.ConfigFileName)
	st, err := os.Stat(path)
	if err != nil {
		return path + "|missing"
	}
	return fmt.Sprintf("%s|%d|%d", path, st.ModTime().UnixNano(), st.Size())
}

// loadVoiceConfig 读配置里的 voice 段（可能为 nil，调用方需容忍）。
func loadVoiceConfig() *voice.VoiceConfig {
	stamp := voiceConfigStamp()

	voiceConfigCache.mu.RLock()
	if voiceConfigCache.valid && voiceConfigCache.stamp == stamp && time.Since(voiceConfigCache.at) < voiceConfigCacheTTL {
		cached := voiceConfigCache.cfg
		voiceConfigCache.mu.RUnlock()
		return cached
	}
	voiceConfigCache.mu.RUnlock()

	// 指纹变化后可能有多个 goroutine 同时读盘：不做 single-flight，
	// 代价只是几次重复读，比引入全局锁阻塞工具列举划算。
	var loaded *voice.VoiceConfig
	if appCfg, err := config.Load(); err == nil && appCfg != nil {
		loaded = appCfg.Voice
	}

	voiceConfigCache.mu.Lock()
	voiceConfigCache.valid = true
	voiceConfigCache.at = time.Now()
	voiceConfigCache.stamp = stamp
	voiceConfigCache.cfg = loaded
	voiceConfigCache.mu.Unlock()
	return loaded
}

// InvalidateVoiceConfigCache 立即丢弃缓存（设置页保存后由 ResetEnvProbes 调用）。
func InvalidateVoiceConfigCache() {
	voiceConfigCache.mu.Lock()
	voiceConfigCache.valid = false
	voiceConfigCache.cfg = nil
	voiceConfigCache.mu.Unlock()
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
