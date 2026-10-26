package tool

import (
	"os"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/provider"
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

// ResetEnvProbes 让所有"读一次环境/配置就永久缓存"的可用性判定重新计算。
//
// 面板保存/删除环境变量后必须调用它：这些缓存是为了避免每次
// ListWithSchemas 都读盘，但代价是刚配好的 VISION_API_KEY/语音 key 在
// 工具列表里依然不可见，用户唯一的感受是"填了没用"。
//
// sync.Once 无法重置，所以直接换新实例 —— 这些变量只在同一 goroutine 的
// 判定路径上被读，重置窗口内的并发读最多多做一次探测，不会读到坏数据。
func ResetEnvProbes() {
	visionCfgOnce = sync.Once{}
	visionCfgOK = false
	voiceCfgOnce = sync.Once{}
	voiceCfgOK = false
}

// TTSTool：需要显式注入 voice manager，或配置了语音 API Key
func (t *TTSTool) Available() bool { return t.manager != nil || voiceAPIKeyConfigured() }

// ASRTool：与 TTS 共用语音配置
func (t *ASRTool) Available() bool { return voiceAPIKeyConfigured() }

// VisionAnalyzeTool：需要视觉后端 —— 独立 VISION_API_KEY / OPENAI_API_KEY，
// 或当前对话模型本身具备视觉能力（与 Execute 的回退链保持一致）。两者都没有
// 时该工具只能返回图片元数据，属于占位工具，不应暴露给 LLM。
func (t *VisionAnalyzeTool) Available() bool { return visionBackendConfigured() }

// ComputerUseTool：需要平台驱动齐备。Windows 恒可用；macOS 依赖 cliclick；
// Linux 需要 xdotool + 任一截图工具，且不能是 Wayland 会话。
// driver 内部用 sync.Once 缓存探测结果，这里的调用不会每次读盘/扫 PATH。
func (t *ComputerUseTool) Available() bool {
	return t.driver != nil && t.driver.Available() == nil
}

// 视觉后端探测（与 voice 一致：结果缓存，避免每次 ListWithSchemas 都读盘）
var (
	visionCfgOnce sync.Once
	visionCfgOK   bool
)

func visionBackendConfigured() bool {
	visionCfgOnce.Do(func() {
		if os.Getenv("VISION_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" {
			visionCfgOK = true
			return
		}
		cfg, err := config.Load()
		if err != nil || cfg == nil || cfg.Provider == "" {
			return
		}
		provCfg, ok := cfg.Providers[cfg.Provider]
		if !ok {
			return
		}
		// 与 createProviderForName 同规则：Models[0] > Model。
		modelName := provCfg.Model
		if len(provCfg.Models) > 0 {
			modelName = provCfg.Models[0]
		}
		visionCfgOK = strings.TrimSpace(modelName) != "" && provider.ModelSupportsVision(modelName)
	})
	return visionCfgOK
}

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
	_ AvailabilityChecker = (*VisionAnalyzeTool)(nil)
	_ AvailabilityChecker = (*ComputerUseTool)(nil)
)
