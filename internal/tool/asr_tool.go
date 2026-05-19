package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/voice"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// ASRTool 语音转文字工具
type ASRTool struct {
	BaseTool
	manager *voice.Manager
}

// NewASRTool 创建 ASR 工具
func NewASRTool() *ASRTool {
	return &ASRTool{
		BaseTool: *NewBaseTool(
			"asr",
			"Convert speech/audio to text using various ASR providers (OpenAI Whisper, Azure Speech, Google Speech-to-Text). Transcribe audio files or audio data to text with language support.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"audio_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to audio file to transcribe (either audio_path or audio_data must be provided)",
					},
					"audio_data": map[string]interface{}{
						"type":        "string",
						"description": "Base64-encoded audio data (either audio_path or audio_data must be provided)",
					},
					"audio_url": map[string]interface{}{
						"type":        "string",
						"description": "URL to download audio file from",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "Language code (e.g., en, zh, ja, ko, de, fr, es). Auto-detected if not specified.",
						"default":     "",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "ASR provider to use: openai, azure, google, whisper",
						"enum":        []string{"openai", "azure", "google", "whisper"},
						"default":     "openai",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "ASR model to use (provider-specific, e.g., 'whisper-1' for OpenAI; 'base', 'small', 'medium', 'large' for local Whisper)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Audio format hint: mp3, wav, ogg, m4a, flac (auto-detected if not specified)",
						"enum":        []string{"mp3", "wav", "ogg", "m4a", "flac", "webm", "auto"},
						"default":     "auto",
					},
				},
				"required": []string{},
			},
		),
	}
}

// SetManager sets the voice manager for the tool
func (t *ASRTool) SetManager(manager *voice.Manager) {
	t.manager = manager
}

// ValidateParams 验证参数
func (t *ASRTool) ValidateParams(params map[string]interface{}) error {
	// 检查是否提供了至少一个音频源
	hasSource := false
	if path, ok := params["audio_path"].(string); ok && path != "" {
		hasSource = true
	}
	if data, ok := params["audio_data"].(string); ok && data != "" {
		hasSource = true
	}
	if url, ok := params["audio_url"].(string); ok && url != "" {
		hasSource = true
	}

	if !hasSource {
		return fmt.Errorf("at least one audio source must be provided: audio_path, audio_data, or audio_url")
	}

	return ValidateParams(t.Schema(), params)
}

// Execute 执行 ASR
func (t *ASRTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	// 获取参数
	provider := "openai"
	if p, ok := params["provider"].(string); ok && p != "" {
		provider = p
	}

	language := ""
	if l, ok := params["language"].(string); ok {
		language = l
	}

	model := ""
	if m, ok := params["model"].(string); ok {
		model = m
	}

	format := "auto"
	if f, ok := params["format"].(string); ok && f != "" {
		format = f
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
			Language: language,
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

	// 获取音频数据
	var audioData []byte
	var audioSource string
	var err error

	if audioPath, ok := params["audio_path"].(string); ok && audioPath != "" {
		// 从文件加载
		absPath, err := filepath.Abs(audioPath)
		if err != nil {
			return nil, fmt.Errorf("invalid audio path: %w", err)
		}

		audioData, err = os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read audio file: %w", err)
		}
		audioSource = absPath
	} else if audioBase64, ok := params["audio_data"].(string); ok && audioBase64 != "" {
		// 从 base64 解码
		audioData, err = base64.StdEncoding.DecodeString(audioBase64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode audio data: %w", err)
		}
		audioSource = "base64 data"
	} else if audioURL, ok := params["audio_url"].(string); ok && audioURL != "" {
		// 从 URL 下载
		audioData, err = manager.DownloadAudio(ctx, audioURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download audio: %w", err)
		}
		audioSource = audioURL
	} else {
		return nil, fmt.Errorf("no audio source provided")
	}

	// 验证音频数据
	if len(audioData) == 0 {
		return nil, fmt.Errorf("audio data is empty")
	}

	// 检测音频格式
	detectedFormat := voice.DetectAudioFormat(audioData)
	if detectedFormat == "unknown" && format != "auto" {
		detectedFormat = format
	}

	// 执行 ASR
	var result *voice.VoiceResult
	if provider != "" && provider != manager.GetConfig().Provider {
		// 使用指定的 provider
		result, err = manager.ASRWithProvider(ctx, provider, audioData)
	} else {
		result, err = manager.ASR(ctx, audioData)
	}

	if err != nil {
		return nil, fmt.Errorf("ASR failed: %w", err)
	}

	// 返回结果
	return map[string]interface{}{
		"status":       "success",
		"text":         result.Text,
		"provider":     result.Provider,
		"language":     language,
		"audio_source": audioSource,
		"audio_format": detectedFormat,
		"duration_sec": result.Duration,
		"file_size":    len(audioData),
	}, nil
}

// ASRAvailableProvidersTool 获取可用的 ASR 提供商工具
type ASRAvailableProvidersTool struct {
	BaseTool
}

// NewASRAvailableProvidersTool 创建 ASR 可用提供商工具
func NewASRAvailableProvidersTool() *ASRAvailableProvidersTool {
	return &ASRAvailableProvidersTool{
		BaseTool: *NewBaseTool(
			"asr_available_providers",
			"Get list of available ASR providers and their configuration status.",
			map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		),
	}
}

// Execute 执行获取可用提供商
func (t *ASRAvailableProvidersTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	voiceConfig := &voice.VoiceConfig{}
	if cfg.Voice != nil {
		voiceConfig = cfg.Voice
	}

	manager := voice.NewManager(voiceConfig)

	allProviders := manager.GetProviders()
	availableProviders := manager.GetAvailableProviders()

	// 构建提供商信息
	providersInfo := make([]map[string]interface{}, 0)
	for _, name := range allProviders {
		provider, _ := manager.GetProvider(name)
		isAvailable := false
		for _, ap := range availableProviders {
			if ap == name {
				isAvailable = true
				break
			}
		}

		info := map[string]interface{}{
			"name":        name,
			"available":   isAvailable,
			"description": getProviderDescription(name),
		}

		if provider != nil {
			info["supports_tts"] = supportsTTS(name)
			info["supports_asr"] = supportsASR(name)
		}

		providersInfo = append(providersInfo, info)
	}

	return map[string]interface{}{
		"providers":           providersInfo,
		"available_count":     len(availableProviders),
		"total_count":         len(allProviders),
		"active_provider":     voiceConfig.Provider,
		"supported_languages": voice.GetSupportedLanguages(voiceConfig.Provider),
	}, nil
}

func getProviderDescription(name string) string {
	descriptions := map[string]string{
		"openai":     "OpenAI TTS/Whisper - High quality text-to-speech and speech recognition",
		"azure":      "Azure Speech Services - Microsoft's cloud-based speech services with extensive language support",
		"google":     "Google Cloud Text-to-Speech/Speech-to-Text - Google's neural speech services",
		"elevenlabs": "ElevenLabs - High-quality neural text-to-speech with voice cloning",
		"whisper":    "OpenAI Whisper (Local) - Local speech recognition using OpenAI's Whisper model",
	}
	if desc, ok := descriptions[name]; ok {
		return desc
	}
	return "Unknown provider"
}

func supportsTTS(name string) bool {
	switch name {
	case "openai", "azure", "google", "elevenlabs":
		return true
	case "whisper":
		return false
	default:
		return false
	}
}

func supportsASR(name string) bool {
	switch name {
	case "openai", "azure", "google", "whisper":
		return true
	case "elevenlabs":
		return false
	default:
		return false
	}
}

// TTSAvailableVoicesTool 获取可用的 TTS 声音工具
type TTSAvailableVoicesTool struct {
	BaseTool
}

// NewTTSAvailableVoicesTool 创建 TTS 可用声音工具
func NewTTSAvailableVoicesTool() *TTSAvailableVoicesTool {
	return &TTSAvailableVoicesTool{
		BaseTool: *NewBaseTool(
			"tts_available_voices",
			"Get list of available voices for a TTS provider and language.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "TTS provider",
						"enum":        []string{"openai", "azure", "google", "elevenlabs"},
						"default":     "openai",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "Language code",
						"default":     "en",
					},
				},
			},
		),
	}
}

// Execute 执行获取可用声音
func (t *TTSAvailableVoicesTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	provider := "openai"
	if p, ok := params["provider"].(string); ok && p != "" {
		provider = p
	}

	language := "en"
	if l, ok := params["language"].(string); ok && l != "" {
		language = l
	}

	voices := voice.GetAvailableVoices(provider, language)
	supportedLanguages := voice.GetSupportedLanguages(provider)

	return map[string]interface{}{
		"provider":            provider,
		"language":            language,
		"voices":              voices,
		"supported_languages": supportedLanguages,
		"voice_count":         len(voices),
	}, nil
}

// AudioPlayTool 播放音频工具
type AudioPlayTool struct {
	BaseTool
}

// NewAudioPlayTool 创建音频播放工具
func NewAudioPlayTool() *AudioPlayTool {
	return &AudioPlayTool{
		BaseTool: *NewBaseTool(
			"audio_play",
			"Play audio file using system audio player.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"audio_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to audio file to play",
					},
					"audio_data": map[string]interface{}{
						"type":        "string",
						"description": "Base64-encoded audio data to play",
					},
					"audio_url": map[string]interface{}{
						"type":        "string",
						"description": "URL to download and play audio from",
					},
				},
				"required": []string{},
			},
		),
	}
}

// Execute 执行播放音频
func (t *AudioPlayTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	var audioData []byte
	var audioSource string
	var err error

	manager := voice.NewManager(nil)

	if audioPath, ok := params["audio_path"].(string); ok && audioPath != "" {
		absPath, err := filepath.Abs(audioPath)
		if err != nil {
			return nil, fmt.Errorf("invalid audio path: %w", err)
		}

		audioData, err = os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read audio file: %w", err)
		}
		audioSource = absPath
	} else if audioBase64, ok := params["audio_data"].(string); ok && audioBase64 != "" {
		audioData, err = base64.StdEncoding.DecodeString(audioBase64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode audio data: %w", err)
		}
		audioSource = "base64 data"
	} else if audioURL, ok := params["audio_url"].(string); ok && audioURL != "" {
		audioData, err = manager.DownloadAudio(ctx, audioURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download audio: %w", err)
		}
		audioSource = audioURL
	} else {
		return nil, fmt.Errorf("no audio source provided (audio_path, audio_data, or audio_url)")
	}

	// 播放音频
	if err := voice.PlayAudio(audioData); err != nil {
		// 提供更详细的错误信息
		errMsg := err.Error()
		if strings.Contains(errMsg, "no audio player found") {
			return nil, fmt.Errorf("no audio player found. Please install one of: mpg123, mpg321, ffplay, or paplay (Linux); afplay (macOS)")
		}
		return nil, fmt.Errorf("failed to play audio: %w", err)
	}

	format := voice.DetectAudioFormat(audioData)
	duration := voice.GetAudioDuration(audioData, format)

	return map[string]interface{}{
		"status":       "success",
		"audio_source": audioSource,
		"format":       format,
		"duration_sec": duration,
		"file_size":    len(audioData),
	}, nil
}

// AudioDownloadTool 下载音频工具
type AudioDownloadTool struct {
	BaseTool
}

// NewAudioDownloadTool 创建音频下载工具
func NewAudioDownloadTool() *AudioDownloadTool {
	return &AudioDownloadTool{
		BaseTool: *NewBaseTool(
			"audio_download",
			"Download audio from URL and save to file.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to download audio from (required)",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Output file path (default: auto-detected from URL or ~/download_audio.<ext>)",
					},
					"play": map[string]interface{}{
						"type":        "boolean",
						"description": "Play the audio after download",
						"default":     false,
					},
				},
				"required": []string{"url"},
			},
		),
	}
}

// Execute 执行下载音频
func (t *AudioDownloadTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	url, ok := params["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url is required")
	}

	manager := voice.NewManager(nil)

	// 下载音频
	audioData, err := manager.DownloadAudio(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to download audio: %w", err)
	}

	// 检测格式
	format := voice.DetectAudioFormat(audioData)
	if format == "unknown" {
		format = "mp3" // 默认使用 mp3
	}

	// 确定输出路径
	outputPath := ""
	if op, ok := params["output_path"].(string); ok && op != "" {
		outputPath = op
	} else {
		// 尝试从 URL 提取文件名
		urlParts := strings.Split(url, "/")
		filename := urlParts[len(urlParts)-1]
		if filename == "" || !strings.Contains(filename, ".") {
			home, _ := os.UserHomeDir()
			filename = fmt.Sprintf("download_audio.%s", format)
			outputPath = filepath.Join(home, filename)
		} else {
			home, _ := os.UserHomeDir()
			outputPath = filepath.Join(home, filename)
		}
	}

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("invalid output path: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// 保存文件
	if err := os.WriteFile(absPath, audioData, 0644); err != nil {
		return nil, fmt.Errorf("failed to save audio: %w", err)
	}

	// 如果需要播放
	play := false
	if p, ok := params["play"].(bool); ok {
		play = p
	}

	if play {
		if err := voice.PlayAudio(audioData); err != nil {
			fmt.Printf("Warning: failed to play audio: %v\n", err)
		}
	}

	duration := voice.GetAudioDuration(audioData, format)

	return map[string]interface{}{
		"status":       "success",
		"url":          url,
		"output_path":  absPath,
		"format":       format,
		"duration_sec": duration,
		"file_size":    len(audioData),
		"played":       play,
	}, nil
}
