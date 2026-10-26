package tool

import (
	"os"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// media_tools.go — 媒体类工具（图片 / 语音 / 音频 / X 搜索）的统一注册入口。
//
// 这批工具此前只挂在 Registry.RegisterOptionalTools 上，而那个方法全仓库零调用方
// （web server、CLI、bot、gateway、cron 全都只走 RegisterAll），于是写好的实现一个
// 也到不了模型手里。现在改为在 RegisterAll 末尾统一注册：
//
//   - 需要外部凭证的（image_gen / image_edit / text_to_speech / asr / x_search）
//     都实现了 AvailabilityChecker，没配 key 时不会出现在 ListWithSchemas 里，
//     模型看不到，也就等于没注册 —— 所以"无条件注册"不会污染工具清单；
//   - 一旦配上 key（config.json 或环境变量），对应工具立即出现，无需改代码。
//
// 反过来说，**没有可用后端**的工具不该留在这里：video_generate 的三个 provider
// 调用都是坏的（Replicate 少 version 字段且不轮询、FAL 队列端点已下线、Stability
// 的 image-to-video 必须有输入图），send_message 的 GatewaySender 接口在 gateway
// 包内没有任何实现，两者已删除，见 git 历史。
func (r *Registry) RegisterMediaTools() {
	// 图片生成 / 编辑：同一份配置，键来自 config.json 的 image_gen 段，
	// 环境变量 IMAGE_GEN_* 作为兜底/覆盖。
	imgCfg := LoadImageGenConfig()
	r.Register(NewImageGenerationTool(imgCfg))
	r.SetTimeout("image_gen", 180*time.Second)
	r.Register(NewImageEditTool(imgCfg))
	r.SetTimeout("image_edit", 180*time.Second)

	// 语音：TTS/ASR 共用 config.json 的 voice 段（voice.api_key 或
	// voice.providers.<name>.api_key）。两个 *available_* 查询工具同门槛，
	// 否则模型会先查一次"有哪些 provider"再发现一个都没配。
	r.Register(NewTTSTool())
	r.SetTimeout("tts", 120*time.Second)
	r.Register(NewASRTool())
	r.SetTimeout("asr", 180*time.Second)
	r.Register(NewTTSAvailableVoicesTool())
	r.Register(NewASRAvailableProvidersTool())

	// 本地音频：不依赖任何云端凭证。播放门槛是系统播放器，下载门槛是网络。
	r.Register(NewAudioPlayTool())
	r.SetTimeout("audio_play", 120*time.Second)
	r.Register(NewAudioDownloadTool())
	r.SetTimeout("audio_download", 120*time.Second)

	// 视频分析：ffmpeg 抽关键帧（可用性门槛就是 ffmpeg 在不在），再把帧交给
	// 视觉模型。与 vision_analyze 配套使用。
	r.Register(NewVideoAnalyzeTool())
	r.SetTimeout("video_analyze", 180*time.Second)

	// X/Twitter 搜索：走 X API v2 recent search，需要 app bearer token
	// （或 OAuth2 用户令牌）。密钥此前只写在工具的错误信息里、没人读环境变量，
	// 于是即使设了 X_BEARER_TOKEN 也永远不可用。
	r.Register(NewXSearchTool(os.Getenv("X_API_KEY"), os.Getenv("X_BEARER_TOKEN")))
	r.SetTimeout("x_search", 60*time.Second)
}

// LoadImageGenConfig 组装图片生成配置：config.json 的 image_gen 段优先，
// 环境变量 IMAGE_GEN_API_KEY / _PROVIDER / _BASE_URL / _OUTPUT_DIR 覆盖单项。
//
// 两者都没有时返回的配置 APIKey 为空，ImageGenerationTool.Available() 为 false，
// 工具不会暴露给模型 —— 这正是"没配就别让模型乱试"的预期行为。
func LoadImageGenConfig() *ImageGenConfig {
	cfg := DefaultImageGenConfig()

	if appCfg, err := config.Load(); err == nil && appCfg != nil && appCfg.ImageGen != nil {
		ig := appCfg.ImageGen
		if ig.APIKey != "" {
			cfg.APIKey = ig.APIKey
		}
		if ig.Provider != "" {
			cfg.Provider = ImageProvider(ig.Provider)
		}
		if ig.BaseURL != "" {
			cfg.BaseURL = ig.BaseURL
		}
		if ig.DefaultSize != "" {
			cfg.DefaultSize = ig.DefaultSize
		}
		if ig.DefaultStyle != "" {
			cfg.DefaultStyle = ig.DefaultStyle
		}
		if ig.OutputDirectory != "" {
			cfg.OutputDirectory = ig.OutputDirectory
		}
	}

	// 环境变量覆盖：部署时改一份 env 就能切 provider，不用碰 config.json。
	if v := os.Getenv("IMAGE_GEN_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("IMAGE_GEN_PROVIDER"); v != "" {
		cfg.Provider = ImageProvider(v)
	}
	if v := os.Getenv("IMAGE_GEN_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("IMAGE_GEN_OUTPUT_DIR"); v != "" {
		cfg.OutputDirectory = v
	}

	return cfg
}
