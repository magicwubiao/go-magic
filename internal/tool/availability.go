package tool

import (
	"sync"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// availability.go — 工具可用性检查（hermes-agent check_fn 对齐）。
//
// 实现 AvailabilityChecker 的工具只有在 Available() 返回 true 时才会被
// Registry.ListWithSchemas / ListAvailable 暴露给 LLM，避免模型调用
// 缺少 API Key / 依赖而必然失败的工具。未实现该接口的工具默认始终可用。

// ImageGenerationTool / ImageEditTool：需要图片生成 API Key
func (t *ImageGenerationTool) Available() bool { return t.config != nil && t.config.APIKey != "" }
func (t *ImageEditTool) Available() bool       { return t.config != nil && t.config.APIKey != "" }

// VideoGenerateTool：需要视频生成 API Key
func (t *VideoGenerateTool) Available() bool { return t.config != nil && t.config.APIKey != "" }

// XSearchTool：需要 XAI API Key 或 Bearer Token
func (t *XSearchTool) Available() bool { return t.apiKey != "" || t.bearerToken != "" }

// SendMessageTool：需要已初始化的 gateway
func (t *SendMessageTool) Available() bool { return t.gateway != nil }

// EmailTool / SMSTool：需要相应配置
func (t *EmailTool) Available() bool { return t.config != nil && t.config.SMTPHost != "" }
func (t *SMSTool) Available() bool   { return t.config != nil && t.config.Provider != "" }

// Home Assistant 工具：需要 HASS_URL + HASS_TOKEN
func (t *HATool) Available() bool             { return t.config.IsConfigured() }
func (t *HAGetStateTool) Available() bool     { return t.config.IsConfigured() }
func (t *HAListServicesTool) Available() bool { return t.config.IsConfigured() }
func (t *HACallServiceTool) Available() bool  { return t.config.IsConfigured() }
func (t *HAEventsTool) Available() bool       { return t.config.IsConfigured() }
func (t *HAConfigTool) Available() bool       { return t.config.IsConfigured() }

// VideoAnalyzeTool：依赖 ffmpeg 提取关键帧（结果缓存，避免重复探测 PATH）
var (
	ffmpegOnce   sync.Once
	ffmpegUsable bool
)

func (t *VideoAnalyzeTool) Available() bool {
	ffmpegOnce.Do(func() { ffmpegUsable = isFFmpegAvailable() })
	return ffmpegUsable
}

// 语音配置检查（TTS/ASR 共用，结果缓存）
var (
	voiceCfgOnce sync.Once
	voiceCfgOK   bool
)

func voiceAPIKeyConfigured() bool {
	voiceCfgOnce.Do(func() {
		cfg, err := config.Load()
		if err == nil && cfg.Voice != nil && cfg.Voice.APIKey != "" {
			voiceCfgOK = true
		}
	})
	return voiceCfgOK
}

// TTSTool：需要显式注入 voice manager，或配置了语音 API Key
func (t *TTSTool) Available() bool { return t.manager != nil || voiceAPIKeyConfigured() }

// ASRTool：与 TTS 共用语音配置
func (t *ASRTool) Available() bool { return voiceAPIKeyConfigured() }

// 编译期断言：确保上述类型均实现 AvailabilityChecker
var (
	_ AvailabilityChecker = (*ImageGenerationTool)(nil)
	_ AvailabilityChecker = (*ImageEditTool)(nil)
	_ AvailabilityChecker = (*VideoGenerateTool)(nil)
	_ AvailabilityChecker = (*XSearchTool)(nil)
	_ AvailabilityChecker = (*SendMessageTool)(nil)
	_ AvailabilityChecker = (*EmailTool)(nil)
	_ AvailabilityChecker = (*SMSTool)(nil)
	_ AvailabilityChecker = (*HATool)(nil)
	_ AvailabilityChecker = (*HAGetStateTool)(nil)
	_ AvailabilityChecker = (*HAListServicesTool)(nil)
	_ AvailabilityChecker = (*HACallServiceTool)(nil)
	_ AvailabilityChecker = (*HAEventsTool)(nil)
	_ AvailabilityChecker = (*HAConfigTool)(nil)
	_ AvailabilityChecker = (*VideoAnalyzeTool)(nil)
	_ AvailabilityChecker = (*TTSTool)(nil)
	_ AvailabilityChecker = (*ASRTool)(nil)
)
