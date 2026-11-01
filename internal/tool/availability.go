package tool

import (
	"os"
	"os/exec"
	"runtime"
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

// ImageGenerationTool：可用性只有一个真源 —— 当前 provider 的后端。
// 内置后端要求 API Key（与老行为一致），插件后端可以有自己的判定（本地 ComfyUI
// 不需要 key）。工具层不再自己看 APIKey，否则"插件后端判 false 但工具有 key"
// 这种组合会让工具出现在清单里、一调就报未配置。
// provider 未注册（拼错 / 插件已卸载）时视为不可用：配错了本来也用不了。
func (t *ImageGenerationTool) Available() bool {
	if t.config == nil {
		return false
	}
	backend, ok := LookupImageBackend(string(effectiveImageProvider(t.config)))
	return ok && backend.Available(t.config)
}

// ImageEditTool：可用 = 当前 provider 的后端可用 **且** 它实现了 ImageEditor。
// 两个条件缺一不可：后端可用但不会编辑（sd / midjourney / together）时，
// 让 image_edit 出现在清单里只会让模型白试一次。
func (t *ImageEditTool) Available() bool {
	if t.config == nil {
		return false
	}
	backend, editor, ok := LookupImageEditor(string(effectiveImageProvider(t.config)))
	return ok && editor != nil && backend.Available(t.config)
}

// VideoGenerationTool：同图片，可用性交给当前后端判定。
// 旧版曾因三个 provider 全跑不通被删，现按官方文档重写（dashscope / volcengine）。
func (t *VideoGenerationTool) Available() bool {
	if t.config == nil {
		return false
	}
	backend, ok := LookupVideoBackend(string(effectiveVideoProvider(t.config)))
	return ok && backend.Available(t.config)
}

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
	audioPlayerOnce = sync.Once{}
	audioPlayerOK = false
	// 语音配置本身也有一层 TTL 缓存（见 speech_backend.go）：不在这里失效，
	// 设置页刚保存完的 key 仍会被 15 秒内的旧快照挡住。
	InvalidateVoiceConfigCache()
}

// TTSTool：需要显式注入 voice manager、配置了语音 API Key，或注册表里有可用的
// TTS 后端（插件贡献的合成能力不依赖 voice.api_key）。
func (t *TTSTool) Available() bool {
	return t.manager != nil || voiceAPIKeyConfigured() || registeredTTSBackendAvailable()
}

// ASRTool：与 TTS 共用语音配置，外加注册表里的识别后端。
func (t *ASRTool) Available() bool {
	return voiceAPIKeyConfigured() || registeredASRBackendAvailable()
}

// TTSAvailableVoicesTool / ASRAvailableProvidersTool：语音能力查询工具。
// 门槛与 TTS/ASR 一致 —— 一个 provider 都没配时，查出来也全是 available:false，
// 只会浪费一轮对话。
func (t *TTSAvailableVoicesTool) Available() bool {
	return voiceAPIKeyConfigured() || registeredTTSBackendAvailable()
}
func (t *ASRAvailableProvidersTool) Available() bool {
	return voiceAPIKeyConfigured() || registeredASRBackendAvailable()
}

// registeredTTSBackendAvailable 报告是否有任一已注册的 TTS 后端可用。
//
// 只在注册表非空时才读配置：没有插件的情况下（绝大多数用户）这里必须零成本，
// 不能因为一个扩展点就让每次工具清单都多读一次 config.json。
func registeredTTSBackendAvailable() bool {
	names := RegisteredTTSProviders()
	if len(names) == 0 {
		return false
	}
	cfg := loadVoiceConfig()
	for _, name := range names {
		if b, ok := LookupTTSBackend(name); ok && b.Available(cfg) {
			return true
		}
	}
	return false
}

// registeredASRBackendAvailable 同 registeredTTSBackendAvailable，识别侧。
func registeredASRBackendAvailable() bool {
	names := RegisteredASRProviders()
	if len(names) == 0 {
		return false
	}
	cfg := loadVoiceConfig()
	for _, name := range names {
		if b, ok := LookupASRBackend(name); ok && b.Available(cfg) {
			return true
		}
	}
	return false
}

// AudioPlayTool：需要本机存在可用的音频播放器。无头服务器（没有声卡/播放器）
// 上播放只会报 "no audio player found"，不该让模型看到这个工具。
var (
	audioPlayerOnce sync.Once
	audioPlayerOK   bool
)

func audioPlayerAvailable() bool {
	audioPlayerOnce.Do(func() {
		var candidates []string
		switch runtime.GOOS {
		case "darwin":
			candidates = []string{"afplay"}
		case "windows":
			// PlayAudio 走 PowerShell + MediaPlayer。不查 powershell.exe 是因为
			// PATH 里通常是 powershell（无扩展名），exec.LookPath 会补 PATHEXT。
			candidates = []string{"powershell"}
		default:
			candidates = []string{"mpg123", "mpg321", "ffplay", "paplay"}
		}
		for _, bin := range candidates {
			if _, err := exec.LookPath(bin); err == nil {
				audioPlayerOK = true
				return
			}
		}
	})
	return audioPlayerOK
}

func (t *AudioPlayTool) Available() bool { return audioPlayerAvailable() }

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
	_ AvailabilityChecker = (*VideoGenerationTool)(nil)
	_ AvailabilityChecker = (*VideoAnalyzeTool)(nil)
	_ AvailabilityChecker = (*TTSTool)(nil)
	_ AvailabilityChecker = (*ASRTool)(nil)
	_ AvailabilityChecker = (*VisionAnalyzeTool)(nil)
	_ AvailabilityChecker = (*ComputerUseTool)(nil)
)
