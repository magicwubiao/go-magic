package tool

import (
	"context"
	"time"
)

// media_backend_builtin.go — 内置媒体后端。
//
// 这一层是**适配器，不是重写**：网络请求、轮询、错误文案全部沿用原有实现
// （image_gen_tool.go 的 generateWithXxx、video_gen_tool.go 的 runXxx），适配器只做
// 参数搬运。这样接口化本身不改变任何线上行为 —— 同一条代码路径，只是入口从
// switch provider 变成 registry 查找。
//
// 后续若要逐个把方法体搬进后端结构体（去掉临时 &ImageGenerationTool{}），可以一次
// 搬一个、每次都能独立跑测试，不必一次性大爆改。

func init() {
	RegisterImageBackend(dalleBackend{})
	RegisterImageBackend(stableDiffusionBackend{})
	RegisterImageBackend(midjourneyBackend{})
	RegisterImageBackend(togetherBackend{})
	RegisterImageBackend(minimaxImageBackend{})

	RegisterVideoBackend(dashscopeVideoBackend{})
	RegisterVideoBackend(volcengineVideoBackend{})
	RegisterVideoBackend(klingVideoBackend{})
	RegisterVideoBackend(runwayVideoBackend{})
	RegisterVideoBackend(minimaxVideoBackend{})

	// 语音：CosyVoice 是第一个进注册表的内置语音后端（其余 4 家仍由 voice.Manager
	// 承载，见 media_provider.go 里关于"不替换既有语音链路"的说明）。
	// 注册表非空后 registeredTTSBackendAvailable() 会开始读语音配置，
	// 所以 speech_backend.go 那边配了 TTL 缓存——顺序别弄反。
	RegisterSpeechBackend(cosyVoiceBackend{})
	RegisterSpeechBackend(minimaxSpeechBackend{})
}

// ---- 图片 ----

type dalleBackend struct{}

func (dalleBackend) Name() string                       { return string(ProviderDALLE) }
func (dalleBackend) Available(cfg *ImageGenConfig) bool { return imageBackendHasKey(cfg) }

func (dalleBackend) Generate(ctx context.Context, cfg *ImageGenConfig, req ImageRequest) (*ImageGenerationResult, error) {
	t := &ImageGenerationTool{config: cfg}
	return t.generateWithDALLE(ctx, req.Prompt, req.NegativePrompt, req.Style, req.Size, req.Count, req.Seed)
}

// Edit 实现 ImageEditor：DALL-E 是 4 个内置后端里唯一支持编辑/变体的。
func (dalleBackend) Edit(ctx context.Context, cfg *ImageGenConfig, req ImageEditRequest) (*ImageGenerationResult, error) {
	return editImageWithDALLE(ctx, cfg, req.Image, req.Prompt, req.MaskPath, req.Size, req.Count, req.EditType)
}

type stableDiffusionBackend struct{}

func (stableDiffusionBackend) Name() string                       { return string(ProviderStableDiffusion) }
func (stableDiffusionBackend) Available(cfg *ImageGenConfig) bool { return imageBackendHasKey(cfg) }

func (stableDiffusionBackend) Generate(ctx context.Context, cfg *ImageGenConfig, req ImageRequest) (*ImageGenerationResult, error) {
	t := &ImageGenerationTool{config: cfg}
	return t.generateWithStableDiffusion(ctx, req.Prompt, req.NegativePrompt, req.Style, req.Size, req.Count, req.Seed)
}

type midjourneyBackend struct{}

// Midjourney 的第三方中转 API 只支持提交、不提供取件轮询，这里返回的是
// Format="pending" 的占位结果（见 generateWithMidjourney 注释）。它属于**已知半成品**，
// 不属于本次接口化的范围；要修就得先找到支持查询的端点，别在这里假装修好了。
func (midjourneyBackend) Name() string                       { return string(ProviderMidjourney) }
func (midjourneyBackend) Available(cfg *ImageGenConfig) bool { return imageBackendHasKey(cfg) }

func (midjourneyBackend) Generate(ctx context.Context, cfg *ImageGenConfig, req ImageRequest) (*ImageGenerationResult, error) {
	t := &ImageGenerationTool{config: cfg}
	return t.generateWithMidjourney(ctx, req.Prompt, req.NegativePrompt, req.Style, req.Size, req.Count, req.Seed)
}

type togetherBackend struct{}

func (togetherBackend) Name() string                       { return string(ProviderTogether) }
func (togetherBackend) Available(cfg *ImageGenConfig) bool { return imageBackendHasKey(cfg) }

func (togetherBackend) Generate(ctx context.Context, cfg *ImageGenConfig, req ImageRequest) (*ImageGenerationResult, error) {
	t := &ImageGenerationTool{config: cfg}
	return t.generateWithTogether(ctx, req.Prompt, req.NegativePrompt, req.Style, req.Size, req.Count, req.Seed)
}

// imageBackendHasKey 内置后端一律要求 API Key。
// 插件后端可以有自己的可用性判定（例如本地 ComfyUI 不需要 key），所以这个判断
// 放在后端里而不是工具里。
func imageBackendHasKey(cfg *ImageGenConfig) bool {
	return cfg != nil && cfg.APIKey != ""
}

// ---- 视频 ----

type dashscopeVideoBackend struct{}

func (dashscopeVideoBackend) Name() string                       { return string(ProviderDashScope) }
func (dashscopeVideoBackend) Available(cfg *VideoGenConfig) bool { return videoBackendHasKey(cfg) }

// Generate 走 wan 系列：文生视频 video-generation 端点，带首帧图时切 image2video。
// dashscope 的 img_url 除了公网地址/OSS，也接受 Base64 data URI
// （data:image/png;base64,...，JPEG/JPG/PNG/BMP/WEBP，wan2.2/2.1 上限 10MB），
// 所以本地图在这里直接内联，不用先传图床。
func (dashscopeVideoBackend) Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error) {
	imageURL := req.ImageURL
	// 同时给了公网地址时以 image_url 为准：本地那份只是调用方的临时占位，
	// 不该因为它的存在把一次本可以成功的调用直接打回。
	if req.ImagePath != "" && req.ImageURL == "" {
		data, mime, err := readInlineImage(ctx, req.ImagePath, dashscopeMaxImageBytes, dashscopeImageExts, "DashScope")
		if err != nil {
			return nil, err
		}
		imageURL = imageDataURI(mime, data)
	}
	t := &VideoGenerationTool{config: cfg}
	videoURL, taskID, err := t.runDashScope(ctx, req.Prompt, req.NegativePrompt, imageURL, req.Model, req.Duration, req.Ratio, req.Resolution)
	if err != nil {
		return nil, err
	}
	return &VideoResult{URL: videoURL, TaskID: taskID}, nil
}

// dashscopePollInterval 百炼的轮询间隔。变量而非常量：测试调到毫秒级，
// 否则每个百炼用例都要真等 10 秒（其它厂商早就这么做了）。
var dashscopePollInterval = 10 * time.Second

// dashscopeMaxImageBytes 内联上限。wan2.2/wanx2.1 是 10MB，wan2.5/2.6 放宽到 20MB；
// 取较保守的 10MB——同一份配置在换模型时不会突然开始报"图片太大"。
const dashscopeMaxImageBytes = 10 << 20

// dashscopeImageExts 官方支持的首帧格式（PNG 不支持透明通道，无法在发请求前判断）。
var dashscopeImageExts = []string{".jpg", ".jpeg", ".png", ".bmp", ".webp"}

type volcengineVideoBackend struct{}

func (volcengineVideoBackend) Name() string                       { return string(ProviderVolcengine) }
func (volcengineVideoBackend) Available(cfg *VideoGenConfig) bool { return videoBackendHasKey(cfg) }

// Generate 走 Seedance：本地首帧图转 base64 data URI（方舟支持），公网地址原样透传。
func (volcengineVideoBackend) Generate(ctx context.Context, cfg *VideoGenConfig, req VideoRequest) (*VideoResult, error) {
	imageURL := req.ImageURL
	if req.ImagePath != "" {
		resolved, err := resolveLocalMedia(ctx, req.ImagePath)
		if err != nil {
			return nil, err
		}
		dataURI, err := localImageToDataURI(resolved)
		if err != nil {
			return nil, err
		}
		imageURL = dataURI
	}

	t := &VideoGenerationTool{config: cfg}
	videoURL, taskID, err := t.runVolcengine(ctx, req.Prompt, imageURL, req.Model, req.Duration, req.Ratio, req.Resolution)
	if err != nil {
		return nil, err
	}
	return &VideoResult{URL: videoURL, TaskID: taskID}, nil
}

func videoBackendHasKey(cfg *VideoGenConfig) bool {
	return cfg != nil && cfg.APIKey != ""
}
