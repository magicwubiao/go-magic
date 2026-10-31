package tool

import (
	"os"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// media_tools.go — 媒体类工具（图片 / 语音 / 音频）的统一注册入口。
//
// 这批工具此前只挂在 Registry.RegisterOptionalTools 上，而那个方法全仓库零调用方
// （web server、CLI、bot、gateway、cron 全都只走 RegisterAll），于是写好的实现一个
// 也到不了模型手里。现在改为在 RegisterAll 末尾统一注册：
//
//   - 需要外部凭证的（image_gen / image_edit / text_to_speech / asr）
//     都实现了 AvailabilityChecker，没配 key 时不会出现在 ListWithSchemas 里，
//     模型看不到，也就等于没注册 —— 所以"无条件注册"不会污染工具清单；
//   - 一旦配上 key（config.json 或环境变量），对应工具立即出现，无需改代码。
//
// 反过来说，**没有可用后端**的工具不该留在这里：video_generate 曾因三个 provider
// 调用全是坏的（Replicate 少 version 字段且不轮询、FAL 队列端点已下线、Stability
// 的 image-to-video 必须有输入图）被整只删除，后按官方文档重写为 dashscope +
// volcengine 两个异步任务型 provider（提交+轮询+下载，见 video_gen_tool.go）。
// send_message 的 GatewaySender 接口在 gateway 包内没有任何实现，仍处删除状态。
// x_search / send_email / send_sms 同理：能力分别由目录里的 X、Gmail、QQ 邮箱类
// 连接器覆盖，内置实现（平台凭据只认环境变量、Web UI 无从配置）已删除。
func (r *Registry) RegisterMediaTools() {
	// 图片生成 / 编辑：同一份配置，键来自 config.json 的 image_gen 段，
	// 环境变量 IMAGE_GEN_* 作为兜底/覆盖。
	imgCfg := LoadImageGenConfig()
	r.Register(NewImageGenerationTool(imgCfg))
	r.SetTimeout("image_gen", 180*time.Second)
	r.Register(NewImageEditTool(imgCfg))
	r.SetTimeout("image_edit", 180*time.Second)

	// 视频生成：配置在 config.json 的 video_gen 段（或 VIDEO_GEN_* 环境变量）。
	// 视频生成通常耗时 1~5 分钟，超时给足 600s，轮询在工具内部完成。
	vidCfg := LoadVideoGenConfig()
	r.Register(NewVideoGenerationTool(vidCfg))
	r.SetTimeout("video_generate", 600*time.Second)

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

// LoadVideoGenConfig 组装视频生成配置：config.json 的 video_gen 段优先，
// 环境变量 VIDEO_GEN_API_KEY / _PROVIDER / _BASE_URL / _MODEL / _OUTPUT_DIR 覆盖单项。
//
// 两者都没有时返回的配置 APIKey 为空，VideoGenerationTool.Available() 为 false，
// 工具不会暴露给模型 —— 与图片生成同门槛。
func LoadVideoGenConfig() *VideoGenConfig {
	cfg := DefaultVideoGenConfig()

	if appCfg, err := config.Load(); err == nil && appCfg != nil && appCfg.VideoGen != nil {
		vg := appCfg.VideoGen
		if vg.APIKey != "" {
			cfg.APIKey = vg.APIKey
		}
		if vg.Provider != "" {
			cfg.Provider = VideoProvider(vg.Provider)
		}
		if vg.BaseURL != "" {
			cfg.BaseURL = vg.BaseURL
		}
		if vg.Model != "" {
			cfg.Model = vg.Model
		}
		if vg.DefaultDuration > 0 {
			cfg.DefaultDuration = vg.DefaultDuration
		}
		if vg.DefaultRatio != "" {
			cfg.DefaultRatio = vg.DefaultRatio
		}
		if vg.DefaultResolution != "" {
			cfg.DefaultResolution = vg.DefaultResolution
		}
		if vg.OutputDirectory != "" {
			cfg.OutputDirectory = vg.OutputDirectory
		}
	}

	// 环境变量覆盖
	if v := os.Getenv("VIDEO_GEN_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("VIDEO_GEN_PROVIDER"); v != "" {
		cfg.Provider = VideoProvider(v)
	}
	if v := os.Getenv("VIDEO_GEN_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("VIDEO_GEN_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("VIDEO_GEN_OUTPUT_DIR"); v != "" {
		cfg.OutputDirectory = v
	}

	return cfg
}
