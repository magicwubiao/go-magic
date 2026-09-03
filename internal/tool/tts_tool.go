package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/voice"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// TTSTool 文字转语音工具
type TTSTool struct {
	BaseTool
	manager *voice.Manager
}

// NewTTSTool 创建 TTS 工具
func NewTTSTool() *TTSTool {
	return &TTSTool{
		BaseTool: *NewBaseTool(
			"tts",
			"Convert text to speech using various TTS providers (OpenAI, Azure, Google, ElevenLabs). Generate audio from text with customizable voice, language, speed, and pitch.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to convert to speech (required)",
					},
					"voice": map[string]interface{}{
						"type":        "string",
						"description": "Voice name or ID (e.g., 'alloy', 'echo', 'fable', 'onyx', 'nova', 'shimmer' for OpenAI; voice ID for ElevenLabs; voice name for Azure/Google)",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "Language code (e.g., en, zh, ja, ko, de, fr, es)",
						"default":     "en",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "TTS provider to use: openai, azure, google, elevenlabs",
						"enum":        []string{"openai", "azure", "google", "elevenlabs"},
						"default":     "openai",
					},
					"speed": map[string]interface{}{
						"type":        "number",
						"description": "Speech speed (0.5 - 2.0)",
						"default":     1.0,
						"minimum":     0.5,
						"maximum":     2.0,
					},
					"pitch": map[string]interface{}{
						"type":        "number",
						"description": "Speech pitch adjustment (-10 to 10, supported by Azure/Google)",
						"default":     0,
						"minimum":     -10,
						"maximum":     10,
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Output file path (default: ~/tts_output_<timestamp>.mp3)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Audio format: mp3, wav, ogg",
						"enum":        []string{"mp3", "wav", "ogg"},
						"default":     "mp3",
					},
					"play": map[string]interface{}{
						"type":        "boolean",
						"description": "Play the audio after generation",
						"default":     false,
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "TTS model to use (provider-specific, e.g., 'tts-1', 'tts-1-hd' for OpenAI; 'eleven_multilingual_v2' for ElevenLabs)",
					},
				},
				"required": []string{"text"},
			},
		),
	}
}

// SetManager sets the voice manager for the tool
func (t *TTSTool) SetManager(manager *voice.Manager) {
	t.manager = manager
}

// ValidateParams 验证参数
func (t *TTSTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 执行 TTS
func (t *TTSTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	textVal, ok := params["text"]
	if !ok {
		return nil, fmt.Errorf("text is required")
	}

	text, ok := textVal.(string)
	if !ok || text == "" {
		return nil, fmt.Errorf("text must be a non-empty string")
	}

	// 获取可选参数
	provider := "openai"
	if p, ok := params["provider"].(string); ok && p != "" {
		provider = p
	}

	voiceName := ""
	if v, ok := params["voice"].(string); ok {
		voiceName = v
	}

	language := "en"
	if l, ok := params["language"].(string); ok && l != "" {
		language = l
	}

	speed := 1.0
	if s, ok := params["speed"].(float64); ok {
		speed = s
		if speed < 0.5 {
			speed = 0.5
		}
		if speed > 2.0 {
			speed = 2.0
		}
	}

	pitch := 0.0
	if p, ok := params["pitch"].(float64); ok {
		pitch = p
		if pitch < -10 {
			pitch = -10
		}
		if pitch > 10 {
			pitch = 10
		}
	}

	model := ""
	if m, ok := params["model"].(string); ok {
		model = m
	}

	format := "mp3"
	if f, ok := params["format"].(string); ok && f != "" {
		format = f
	}

	play := false
	if p, ok := params["play"].(bool); ok {
		play = p
	}

	// 获取或创建 voice manager
	manager := t.manager
	if manager == nil {
		// 尝试从配置加载
		cfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}

		voiceConfig := &voice.VoiceConfig{
			Provider: provider,
			Voice:    voiceName,
			Language: language,
			Speed:    speed,
			Pitch:    pitch,
			Model:    model,
		}

		// 从配置中加载 API 密钥
		if cfg.Voice != nil {
			voiceConfig.APIKey = cfg.Voice.APIKey
			voiceConfig.Region = cfg.Voice.Region
			voiceConfig.Providers = cfg.Voice.Providers
		}

		manager = voice.NewManager(voiceConfig)
	}

	// 设置输出路径
	outputPath := ""
	if op, ok := params["output_path"].(string); ok && op != "" {
		outputPath = op
	} else {
		home, _ := os.UserHomeDir()
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		outputPath = filepath.Join(home, fmt.Sprintf("tts_output_%s.%s", timestamp, format))
	}

	// 验证输出路径
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("invalid output path: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// 执行 TTS
	var result *voice.VoiceResult
	if provider != "" && provider != manager.GetConfig().Provider {
		// 使用指定的 provider
		result, err = manager.TTSWithProvider(ctx, provider, text)
	} else {
		result, err = manager.TTS(ctx, text)
	}

	if err != nil {
		return nil, fmt.Errorf("TTS failed: %w", err)
	}

	// 如果需要转换格式
	if format != result.Format {
		converted, err := manager.ConvertAudioFormat(result.Audio, format)
		if err != nil {
			return nil, fmt.Errorf("failed to convert audio format: %w", err)
		}
		result.Audio = converted
		result.Format = format
	}

	// 保存音频文件
	if err := manager.SaveAudio(result.Audio, absPath); err != nil {
		return nil, fmt.Errorf("failed to save audio: %w", err)
	}

	// 如果需要播放
	if play {
		if err := manager.PlayAudio(result.Audio); err != nil {
			// 播放失败不返回错误，只是警告
			fmt.Printf("Warning: failed to play audio: %v\n", err)
		}
	}

	// 返回结果
	return map[string]interface{}{
		"status":       "success",
		"text":         text,
		"text_length":  len(text),
		"word_count":   len(strings.Fields(text)),
		"provider":     result.Provider,
		"voice":        voiceName,
		"language":     language,
		"speed":        speed,
		"pitch":        pitch,
		"output_path":  absPath,
		"format":       result.Format,
		"duration_sec": result.Duration,
		"file_size":    len(result.Audio),
		"played":       play,
	}, nil
}
