package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/util"
)

// video_gen_tool.go — 视频生成工具（video_generate）。
//
// 历史包袱：旧版 video_generate 有三个 provider（Replicate / FAL / Stability），
// 全是坏的（Replicate 少 version 字段且不轮询、FAL 队列端点已下线、Stability
// 必须有输入图），整只工具被删。本文件是重写版，只接**已核实过官方文档**的
// 异步任务型 API，且提交后由工具内部轮询到出片为止，不再出现"只提交不查询"：
//
//   - dashscope   阿里云百炼（通义万相 wan 系列，文生视频 + 图生视频）
//     POST /api/v1/services/aigc/{video-generation|image2video}/video-synthesis
//     （必须带 X-DashScope-Async: enable），随后轮询 GET /api/v1/tasks/{task_id}；
//   - volcengine  火山方舟（Seedance 系列）
//     POST /api/v3/contents/generations/tasks，轮询 GET .../tasks/{id}。
//
// 两个 provider 都是：提交 → 每 10s 轮询 → 成功后下载 MP4 落盘（temp+rename）。
// 视频 URL 有效期均只有 24 小时，所以必须当场下载，不能只把 URL 还给模型。
//
// 视频生成通常耗时 1~5 分钟，工具超时在 RegisterMediaTools 里设为 600s。

// VideoProvider 视频生成提供商类型
type VideoProvider string

const (
	// ProviderDashScope 阿里云百炼（通义万相 wan 系列）
	ProviderDashScope VideoProvider = "dashscope"
	// ProviderVolcengine 火山方舟（Seedance 系列）
	ProviderVolcengine VideoProvider = "volcengine"
)

// VideoGenConfig 视频生成配置（与 ImageGenConfig 同构：config.json video_gen 段 + 环境变量覆盖）
type VideoGenConfig struct {
	Provider          VideoProvider `json:"provider"`
	APIKey            string        `json:"api_key"`
	BaseURL           string        `json:"base_url"`
	Model             string        `json:"model"`
	DefaultDuration   int           `json:"default_duration"`
	DefaultRatio      string        `json:"default_ratio"`
	DefaultResolution string        `json:"default_resolution"`
	OutputDirectory   string        `json:"output_directory"`
	Timeout           time.Duration `json:"timeout"`
}

// DefaultVideoGenConfig 返回默认视频生成配置
func DefaultVideoGenConfig() *VideoGenConfig {
	return &VideoGenConfig{
		Provider: ProviderDashScope,
		// Model 留空 = 用 catalog 里该 provider 的推荐模型。这里曾经写死
		// wan2.2-t2v-plus：厂商换代后设置页显示 wan2.7、实际请求还发 wan2.2，
		// 而 wan2.2 是固定 5 秒，duration 参数等于空转。默认值只能有一份。
		Model:             "",
		DefaultDuration:   5,
		DefaultRatio:      "16:9",
		DefaultResolution: "720p",
		OutputDirectory:   "./generated_videos",
		Timeout:           60 * time.Second,
	}
}

// VideoGenerationTool 视频生成工具
type VideoGenerationTool struct {
	BaseTool
	config *VideoGenConfig
}

// NewVideoGenerationTool 创建视频生成工具
func NewVideoGenerationTool(config *VideoGenConfig) *VideoGenerationTool {
	if config == nil {
		config = DefaultVideoGenConfig()
	}

	return &VideoGenerationTool{
		BaseTool: *NewBaseTool(
			"video_generate",
			"Generate short videos from a text prompt (and optionally a first-frame image) using AI. "+
				"Providers: dashscope (Alibaba wan), volcengine (Seedance), kling (Kuaishou Kling), "+
				"runway (Runway Gen-4), minimax (Hailuo H3). Pick the provider by what the user's account "+
				"has, and by capability: Runway only renders 5 or 10 second clips; Kling and MiniMax need a "+
				"public URL for image-to-video while volcengine and runway accept local files. "+
				"Only the provider configured in Settings has credentials, so do not switch provider "+
				"on your own to work around a capability limit — report the limit instead. "+
				"Generation typically takes 1-5 minutes; the tool polls the provider and saves the MP4 locally. "+
				"Every call is billed per rendered second and needs an explicit user confirmation, so never "+
				"retry with a reworded prompt to explore variations — ask the user first.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Detailed description of the video: subject, motion, camera movement, lighting. Be concrete (e.g. 'slow dolly-in on a kitten running through moonlit grass') instead of grandiose.",
					},
					"image_url": map[string]interface{}{
						"type":        "string",
						"description": "Public http(s) URL of a first-frame image to animate (image-to-video). Optional; omit for text-to-video.",
					},
					"image_path": map[string]interface{}{
						"type":        "string",
						"description": "Local path of a first-frame image for image-to-video. All built-in providers accept local files (base64 inline; kling sends the raw string, the rest send a data URI); providers without an inline path reject it with a hint to pass image_url instead.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Provider model override, e.g. wan2.7-t2v (dashscope), doubao-seedance-2-5-260628 (volcengine), kling-3.0 (kling), gen4.5 (runway), MiniMax-H3 (minimax). Default comes from config.",
					},
					"duration": map[string]interface{}{
						"type":        "number",
						"description": "Video length in seconds. Per-provider range: dashscope 2-30 on wan3.0 and 2-15 on wan2.7/2.6, volcengine 4-30 on Seedance 2.5 and 4-15 on 2.0, kling 3-15 (2.6/2.5-turbo: 5 or 10), runway 5 or 10, minimax 4-15. Out-of-range values are clamped and the actual value is reported back. Default comes from config (video_gen.default_duration).",
					},
					"ratio": map[string]interface{}{
						"type":        "string",
						"description": "Aspect ratio. dashscope supports 16:9, 9:16, 1:1, 4:3, 3:4 and 21:9 on Wan 3.0, 4:3/3:4 are 2.7-only, and 2.6 or earlier only do 16:9/9:16/1:1; volcengine also supports 21:9; kling, runway and minimax support 16:9, 9:16 and 1:1. Omit to use the configured default ratio (Settings > Media). Unsupported values fall back to 16:9 and the actual value is reported back (image-to-video reports adaptive, since the frame decides).",
						"enum":        []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
					},
					"resolution": map[string]interface{}{
						"type":        "string",
						"description": "Resolution tier: 480p, 720p or 1080p. dashscope supports 720p/1080p on Wan 2.6/2.7 and 480p only on Wan 3.0. Omit to use the configured default resolution (Settings > Media). Providers map this to their own tiers (minimax uses 768P/2K) and the actual value is reported back.",
						"enum":        []string{"480p", "720p", "1080p"},
					},
					"negative_prompt": map[string]interface{}{
						"type":        "string",
						"description": "Content to avoid (dashscope only, optional)",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Custom output file path for the downloaded MP4 (optional)",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Override the configured provider. Only useful for plugin backends that carry their own credentials; built-in providers share one credential set, so overriding to a different built-in provider is rejected.",
						// 这个 enum 是**内置后端**的快照，故意不跟注册表联动：工具 schema 一旦
						// 随插件注册变化，模型看到的能力面就不可复现了（同一个 prompt 在装插件
						// 前后表现不一致，排查成本极高）。插件贡献的 provider 走
						// config.json 的 video_gen.provider 指定，不通过这个参数选择。
						"enum": []string{"dashscope", "volcengine", "kling", "runway", "minimax"},
					},
				},
				"required": []string{"prompt"},
			},
		),
		config: config,
	}
}

// ValidateParams 验证参数
func (t *VideoGenerationTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}

// Execute 执行视频生成：提交异步任务 → 轮询 → 下载落盘
func (t *VideoGenerationTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	// 图生视频输入：优先 image_url（公网地址），其次本地 image_path。
	imageURL := ""
	if v, ok := params["image_url"].(string); ok {
		imageURL = strings.TrimSpace(v)
	}
	imagePath := ""
	if v, ok := params["image_path"].(string); ok {
		imagePath = strings.TrimSpace(v)
	}

	// 可选参数
	duration := t.config.DefaultDuration
	if d, ok := params["duration"].(float64); ok && d > 0 {
		duration = int(d)
	}

	ratio := t.config.DefaultRatio
	if r, ok := params["ratio"].(string); ok && r != "" {
		ratio = r
	}
	resolution := t.config.DefaultResolution
	if r, ok := params["resolution"].(string); ok && r != "" {
		resolution = r
	}
	negativePrompt := ""
	if np, ok := params["negative_prompt"].(string); ok {
		negativePrompt = np
	}
	model := t.config.Model
	if m, ok := params["model"].(string); ok && m != "" {
		model = m
	}
	outputPath := ""
	if op, ok := params["output_path"].(string); ok {
		outputPath = op
	}

	// 配置里的 provider 才是"这份凭据属于谁"。模型可以通过 provider 参数改选，
	// 但内置后端的 key / base_url / model 是全局单值，换名等于让凭据与接口错配。
	configuredProvider := effectiveVideoProvider(t.config)
	provider := configuredProvider
	overridden := false
	if p, ok := params["provider"].(string); ok {
		if p = strings.TrimSpace(p); p != "" {
			provider = VideoProvider(p)
			overridden = CanonicalProviderName(MediaKindVideo, p) !=
				CanonicalProviderName(MediaKindVideo, string(configuredProvider))
		}
	}
	if overridden {
		// 配置里的模型名属于配置里那一家（runway + MiniMax-H3 就是一个必 404 的组合）。
		// 模型显式传了 model 的仍然尊重，那是调用方明确的意图。
		if m, ok := params["model"].(string); !ok || strings.TrimSpace(m) == "" {
			model = ""
		}
	}

	// 时长区间按 provider 的 catalog 走，不再是全局 3-15：Seedance 2.5 单次能到
	// 30 秒、Wan 只到 15 秒、Runway 只有 5/10 两档。写死全局值会让"支持新模型"
	// 变成"新模型的能力在工具层被砍掉一半"，而模型那边看到的能力面也是错的。
	minDur, maxDur := videoDurationRange(provider)
	duration = clampDuration(duration, minDur, maxDur)
	duration = durationForModel(provider, model, duration)

	// 先查后端：provider 拼错 / 插件已卸载要立刻报"不支持"，而不是误导性的"未配置"。
	// 首帧图的处理（公网 URL 直用 / 本地文件转 data URI / 不支持就报错）属于
	// **后端能力差异**，已下沉到各 backend；这里不再按厂商名分支。
	backend, ok := LookupVideoBackend(string(provider))
	if !ok {
		return nil, fmt.Errorf("unsupported video provider: %s (available: %s)",
			provider, strings.Join(RegisteredVideoProviders(), ", "))
	}

	// 换到另一家**内置**后端时直接拒绝，不发请求。
	//
	// video_gen 段只有一份 key：硬发就是拿 minimax 的密钥去打 runway，再拼出
	// https://api.minimax.cn/v1/videos/text2video 这种不存在的路径，回来一个
	// nginx 404 页；模型读不懂 404，会继续换下一家，一次对话刷出一屏互相矛盾的
	// 错误——用户看到的现象就是"明明配好了，怎么全在报错"。这里把链路断在源头。
	if overridden && isBuiltinVideoBackend(string(provider)) {
		return map[string]interface{}{
			"status":   "not_configured",
			"provider": provider,
			"prompt":   prompt,
			"message": fmt.Sprintf(
				"Only %q is configured with credentials, so this call cannot switch to %q at runtime: "+
					"it would send the %q key to another vendor's endpoint. Retry with provider %q, "+
					"or ask the user to change the provider in Settings > Media.",
				configuredProvider, provider, configuredProvider, configuredProvider),
			"configuration_help": ConfigHelp(MediaKindVideo, string(provider)),
		}, nil
	}

	// 插件后端自带凭据，可以换；但 base_url / model 属于配置里那一家，不能顺手
	// 带过去——否则插件后端会拿着别家的域名和模型名发请求。
	genCfg := t.config
	if overridden {
		clone := *t.config
		clone.BaseURL = ""
		clone.Model = ""
		genCfg = &clone
	}

	// 可用性由后端判定（内置后端要求 key，插件后端可能不需要），不在工具层硬判断。
	if !backend.Available(genCfg) {
		return map[string]interface{}{
			"status":             "not_configured",
			"provider":           provider,
			"prompt":             prompt,
			"message":            fmt.Sprintf("Video generation is not configured for provider %q.", provider),
			"configuration_help": ConfigHelp(MediaKindVideo, string(provider)),
		}, nil
	}

	genResult, err := backend.Generate(ctx, genCfg, VideoRequest{
		Prompt:         prompt,
		NegativePrompt: negativePrompt,
		ImageURL:       imageURL,
		ImagePath:      imagePath,
		Model:          model,
		Duration:       duration,
		Ratio:          ratio,
		Resolution:     resolution,
	})
	if err != nil {
		return nil, fmt.Errorf("video generation failed: %w", err)
	}
	videoURL, taskID := genResult.URL, genResult.TaskID

	// 回显**实际生效**的参数。各厂商会做映射与钳制（Runway 只有 5/10 秒、
	// Kling 没有 480p、MiniMax 用 768P/2K 而不是 720p/1080p），继续回显用户
	// 原始输入等于撒谎：用户看到 "duration: 8" 但拿到 5 秒的片子，只会以为是 bug。
	if genResult.Duration > 0 {
		duration = genResult.Duration
	}
	if genResult.Ratio != "" {
		ratio = genResult.Ratio
	}
	if genResult.Resolution != "" {
		resolution = genResult.Resolution
	}
	if genResult.Model != "" {
		model = genResult.Model
	}

	// 视频 URL 24 小时过期，必须立刻下载保存
	savedPath, err := t.downloadAndSaveVideo(ctx, videoURL, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to save video: %w", err)
	}

	result := map[string]interface{}{
		"status":     "success",
		"provider":   provider,
		"model":      model,
		"task_id":    taskID,
		"prompt":     prompt,
		"duration":   duration,
		"ratio":      ratio,
		"resolution": resolution,
		"video_path": savedPath,
		// 原始 URL 一并返回，方便人工复核；注意 24h 过期
		"video_url": videoURL,
	}
	if imageURL != "" && !strings.HasPrefix(imageURL, "data:") {
		result["image_url"] = imageURL
	}
	return result, nil
}

// ---- 阿里云百炼（DashScope / wan 系列）----

// videoDurationRange 返回某 provider 支持的时长区间（秒）。
//
// 数据源是 catalog 的 MinDuration / MaxDuration —— 那是"这家厂商实际接受什么"
// 的唯一真源。查不到（插件后端、名字写错）时回落到 3-15：这个区间几乎所有
// 厂商都能接受，比直接报"不支持"更有用。
func videoDurationRange(provider VideoProvider) (int, int) {
	if info, ok := ProviderInfo(MediaKindVideo, string(provider)); ok {
		minDur, maxDur := info.MinDuration, info.MaxDuration
		if minDur <= 0 {
			minDur = 3
		}
		if maxDur <= 0 {
			maxDur = 15
		}
		return minDur, maxDur
	}
	return 3, 15
}

// durationForModel 在 provider 区间之内再按型号收窄。
//
// catalog 的 Min/MaxDuration 描述的是"这家最强型号"的能力（Seedance 2.5 到
// 30 秒、Kling 3.0 到 15 秒），同一家的旧型号往往到不了。只按厂商上限发出去，
// 表现是一次只有服务端能看懂的 400/任务失败。把型号差异收在这里还有一个好处：
// 钳制发生在工具层，回显给调用方的 duration 与实际发出的完全一致。
func durationForModel(provider VideoProvider, model string, duration int) int {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		// 没指定型号 = 用 catalog 的默认款，provider 区间就是它的区间。
		return duration
	}
	switch provider {
	case ProviderVolcengine:
		// 只有 2.5 支持 30 秒；2.0 / fast / mini 都是 4-15。
		if !strings.Contains(m, "2-5") {
			return clampDuration(duration, 4, 15)
		}
	case "kling":
		// 2.6 / 2.5-turbo 只有 5、10 两档；3.0 才是 3-15 任意值。
		if strings.Contains(m, "2.6") || strings.Contains(m, "2-6") ||
			strings.Contains(m, "2.5") || strings.Contains(m, "2-5") {
			return snapDuration(clampDuration(duration, 5, 10), 5, 10)
		}
	case "minimax":
		// H3-Max 的下限是 5 秒（其余 H3 型号 4 秒起）。匹配 "-max" 而不是
		// "max"：厂商名 minimax 本身就含 max，裸匹配会把每个型号都当成 Max。
		if strings.Contains(m, "-max") {
			return clampDuration(duration, 5, 15)
		}
	case ProviderDashScope:
		// 只有 wan3.0 到 30 秒，2.7 / 2.6 都是 2-15。
		if !isWan3Model(m) {
			return clampDuration(duration, 2, 15)
		}
	}
	return duration
}

// runDashScope 提交百炼视频生成任务并轮询到出片。
//
// 百炼有三套并存的图生视频形态，混用就是一次读不懂的 400：
//   - wan2.7：与文生视频共用 video-generation 端点，首帧走 input.media
//     [{type:first_frame,url}]，且**没有 ratio**（画幅由首帧决定）；
//   - wan2.6：同一端点，首帧走 input.img_url，规格用 resolution（无 ratio）；
//   - wan2.2 及更早：旧 image2video 端点 + input.img_url + 像素 size。
func (t *VideoGenerationTool) runDashScope(ctx context.Context, prompt, negativePrompt, imageURL, model string, duration int, ratio, resolution string) (videoURL, taskID string, err error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/api/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// 配置里 model 为空是常态（设置页只显示占位）：这里必须回落 catalog 的
	// 推荐值，否则请求体里就是 "model": ""，回一个看不懂的 400。
	model = infoOrDefaultModel(MediaKindVideo, string(ProviderDashScope), model)

	// 有首帧图时切 i2v 端点；若用户没显式指定 i2v 模型，把 t2v 型号换成 i2v 型号。
	endpoint := "/services/aigc/video-generation/video-synthesis"
	if imageURL != "" {
		model = dashScopeImageModel(model)
		// 3.0/2.7/2.6 的图生视频和文生视频同一个端点；只有更早的 wan2.x 用 image2video。
		if !isWan3Model(model) && !isWan27Model(model) && !isWan26Model(model) {
			endpoint = "/services/aigc/image2video/video-synthesis"
		}
	}

	// effRatio/effRes 是这条请求实际会生效的档位。发请求和回显都用它，
	// 免得"结果里写 480p、实际按 720P 出片"这种回声失真。
	effRatio, effRes := dashScopeEffective(model, ratio, resolution, imageURL != "")

	parameters := map[string]interface{}{}
	input := map[string]interface{}{"prompt": prompt}
	if negativePrompt != "" {
		input["negative_prompt"] = negativePrompt
	}

	switch {
	case isWan3Model(model) || isWan27Model(model):
		// 3.0/2.7：resolution + ratio（图生视频用 media，画幅由首帧决定）。
		parameters["resolution"] = strings.ToUpper(effRes)
		if imageURL != "" {
			input["media"] = []map[string]interface{}{
				{"type": "first_frame", "url": imageURL},
			}
		} else {
			parameters["ratio"] = effRatio
		}
	case imageURL != "":
		// 图生视频（2.6 及更早）：画幅由首帧决定，参数里只有分辨率档位。
		input["img_url"] = imageURL
		parameters["resolution"] = strings.ToUpper(effRes)
	default:
		// 老款文生视频：只认像素 size，不认 resolution/ratio。
		size, serr := dashScopeSize(effRatio, effRes)
		if serr != nil {
			return "", "", serr
		}
		parameters["size"] = size
	}
	// wan2.2 / wanx2.1 是固定 5 秒，传 duration 会被拒（"duration is not supported"）。
	if !isFixedDurationWan(model) {
		parameters["duration"] = duration
	}

	reqBody := map[string]interface{}{
		"model":      model,
		"input":      input,
		"parameters": parameters,
	}

	taskID, err = t.postDashScopeTask(ctx, baseURL+endpoint, reqBody)
	if err != nil {
		return "", "", err
	}

	videoURL, err = t.pollDashScopeTask(ctx, baseURL, taskID)
	return videoURL, taskID, err
}

// wan27Ratios / wan3Ratios / legacyWanRatios 是各代模型实际接受的画幅。
// 4:3 / 3:4 是 2.7 才有的，21:9 是 3.0 才有的；往老款发这些值会被拒，所以逐代收窄。
var (
	wan3Ratios      = []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}
	wan27Ratios     = []string{"16:9", "9:16", "1:1", "4:3", "3:4"}
	legacyWanRatios = []string{"16:9", "9:16", "1:1"}
)

// dashScopeEffective 返回这次请求实际生效的画幅与分辨率（catalog 风格的
// 小写档位）。runDashScope 与后端回显共用它，两条路径不会各说一套。
func dashScopeEffective(model, ratio, resolution string, hasImage bool) (effRatio, effResolution string) {
	if isWan3Model(model) {
		// 3.0 多一个 480P 档和 21:9；图生视频同样是画幅跟随首帧。
		effResolution = dashScopeResolution(resolution, true)
		if hasImage {
			return "adaptive", effResolution
		}
		return pickRatio(ratio, wan3Ratios, "16:9"), effResolution
	}
	if isWan27Model(model) {
		effResolution = dashScopeResolution(resolution, false)
		if hasImage {
			return "adaptive", effResolution
		}
		return pickRatio(ratio, wan27Ratios, "16:9"), effResolution
	}
	effResolution = dashScopeResolution(resolution, true)
	if hasImage {
		return "adaptive", effResolution
	}
	effRatio = pickRatio(ratio, legacyWanRatios, "16:9")
	// 老款 1:1 没有 1080p 档，实际是按 960*960（720p 级）出图。
	if effRatio == "1:1" && effResolution == "1080p" {
		effResolution = "720p"
	}
	return effRatio, effResolution
}

// dashScopeResolution 把统一档位收敛到该代模型支持的档位（小写，与 catalog 一致）。
// allow480 为 false 时（wan2.7）向上取 720p —— 2.7 没有 480p 档，原样发 480P
// 会 422，而错在"配置里存的是上一代的值"，用户很难联想到。
func dashScopeResolution(resolution string, allow480 bool) string {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "1080p":
		return "1080p"
	case "480p":
		if allow480 {
			return "480p"
		}
		return "720p"
	default:
		return "720p"
	}
}

// dashScopeImageModel 把文生视频型号换成对应的图生视频型号。
//
// 百炼有两种命名：wan2.2-t2v-plus（-t2v- 夹在中间）和 wan2.7-t2v（t2v 在结尾）。
// 只匹配 "-t2v-" 会漏掉后者，于是图生视频请求带着文生视频模型发出去 —— 表现是
// 一次莫名其妙的 400，而不是"模型不支持首帧图"这种能自查的错误。
func dashScopeImageModel(model string) string {
	swapped := model
	if i := strings.Index(swapped, "t2v"); i >= 0 {
		swapped = swapped[:i] + "i2v" + swapped[i+3:]
	}
	// 带日期快照的型号不能照搬日期：wan2.7-t2v-2026-06-12 对应的 i2v 快照是
	// 另一个日期（2026-04-25），原样替换会拼出一个不存在的 ID。退回不带日期的
	// 稳定别名 —— 少钉一个快照，好过 404。
	if swapped != model {
		swapped = stripModelDateSuffix(swapped)
	}
	return swapped
}

// stripModelDateSuffix 去掉 "-YYYY-MM-DD" 形式的版本后缀；没有就原样返回。
func stripModelDateSuffix(model string) string {
	if len(model) < 11 || model[len(model)-11] != '-' {
		return model
	}
	tail := model[len(model)-10:]
	if tail[4] != '-' || tail[7] != '-' {
		return model
	}
	for i, c := range tail {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return model
		}
	}
	return model[:len(model)-11]
}

// isWan3Model 报告是否为 Wan 3.0 系列（同一个 video-synthesis 端点，
// 但多 480P/21:9，时长到 30 秒）。
func isWan3Model(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "wan3")
}

// isWan27Model 报告是否为 Wan 2.7 系列（新协议：resolution + ratio / media）。
func isWan27Model(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "wan2.7")
}

// isWan26Model 报告是否为 Wan 2.6 系列（图生视频的过渡协议：同一端点，
// img_url + resolution，但文生视频仍是像素 size）。
func isWan26Model(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "wan2.6")
}

// isFixedDurationWan 报告是否为时长固定 5 秒的老款模型（不接受 duration 参数）。
func isFixedDurationWan(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "wan2.2") ||
		strings.HasPrefix(m, "wan2.1") ||
		strings.HasPrefix(m, "wanx2.1")
}

// dashScopeSize 把 ratio+resolution 映射成百炼要求的 "宽*高" 字符串。
// 百炼只认具体像素（如 1280*720），不认 16:9 或 "720P" 这类写法。
func dashScopeSize(ratio, resolution string) (string, error) {
	switch {
	case ratio == "16:9" && resolution == "480p":
		return "832*480", nil
	case ratio == "16:9" && resolution == "720p":
		return "1280*720", nil
	case ratio == "16:9" && resolution == "1080p":
		return "1920*1080", nil
	case ratio == "9:16" && resolution == "480p":
		return "480*832", nil
	case ratio == "9:16" && resolution == "720p":
		return "720*1280", nil
	case ratio == "9:16" && resolution == "1080p":
		return "1080*1920", nil
	case ratio == "1:1" && resolution == "720p":
		return "960*960", nil
	// 老款 1:1 没有 1080p 档。与其让整次调用死在参数校验上，不如按 720p 出图
	// ——1:1 表达的是画幅意图，不是精确像素。
	case ratio == "1:1" && resolution == "1080p":
		return "960*960", nil
	}
	return "", fmt.Errorf(
		"unsupported ratio/resolution for dashscope: %s/%s (supported: 16:9 and 9:16 at 480p/720p/1080p, 1:1 at 720p)",
		ratio, resolution)
}

// postDashScopeTask 提交任务，返回 task_id。必须带 X-DashScope-Async: enable，
// 否则报 "current user api does not support synchronous calls"。
func (t *VideoGenerationTool) postDashScopeTask(ctx context.Context, url string, body map[string]interface{}) (string, error) {
	reqJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)
	req.Header.Set("X-DashScope-Async", "enable")

	respBody, err := t.doHTTP(ctx, req)
	if err != nil {
		return "", err
	}

	var resp struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("failed to parse submit response: %w (body: %s)", err, truncateForLog(respBody))
	}
	if resp.Output.TaskID == "" {
		return "", fmt.Errorf("task submit rejected (code=%s): %s", resp.Code, resp.Message)
	}
	return resp.Output.TaskID, nil
}

// pollDashScopeTask 轮询百炼任务直到 SUCCEEDED/FAILED（PENDING → RUNNING → SUCCEEDED/FAILED）
func (t *VideoGenerationTool) pollDashScopeTask(ctx context.Context, baseURL, taskID string) (string, error) {
	url := baseURL + "/tasks/" + taskID
	return pollUntilDone(ctx, dashscopePollInterval, func() (done bool, videoURL string, err error) {
		req, rerr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if rerr != nil {
			return true, "", fmt.Errorf("failed to create poll request: %w", rerr)
		}
		req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

		respBody, rerr := t.doHTTP(ctx, req)
		if rerr != nil {
			return true, "", rerr
		}

		var resp struct {
			Output struct {
				TaskStatus string `json:"task_status"`
				VideoURL   string `json:"video_url"`
				Message    string `json:"message"`
				Code       string `json:"code"`
			} `json:"output"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse poll response: %w (body: %s)", err, truncateForLog(respBody))
		}

		switch resp.Output.TaskStatus {
		case "SUCCEEDED":
			if resp.Output.VideoURL == "" {
				return true, "", fmt.Errorf("task succeeded but no video_url returned")
			}
			return true, resp.Output.VideoURL, nil
		case "FAILED", "CANCELED", "UNKNOWN":
			return true, "", fmt.Errorf("task ended with status %s (code=%s): %s",
				resp.Output.TaskStatus, resp.Output.Code, resp.Output.Message)
		default: // PENDING / RUNNING：继续轮询
			return false, "", nil
		}
	})
}

// ---- 火山方舟（Volcengine Ark / Seedance 系列）----

// runVolcengine 提交方舟视频生成任务并轮询到出片。
// prompt 里的时长/画幅/分辨率按官方约定以 "--flag" 形式附加在 text 里。
func (t *VideoGenerationTool) runVolcengine(ctx context.Context, prompt, imageURL, model string, duration int, ratio, resolution string) (videoURL, taskID string, err error) {
	baseURL := t.config.BaseURL
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// 同 dashscope：空 model 必须回落 catalog，不能原样发空字符串。
	model = infoOrDefaultModel(MediaKindVideo, string(ProviderVolcengine), model)

	// 首帧/首尾帧任务方舟要求 ratio 必须是 adaptive（输出跟着首帧走）；再带一个
	// 具体画幅会被判成参数冲突，任务提交后才异步失败。首帧素材还必须显式标
	// role=first_frame —— 2.5 的 content 里有 reference_image 等多种角色，缺省
	// 不再等于首帧。
	if imageURL != "" {
		ratio = "adaptive"
	}

	text := fmt.Sprintf("%s --duration %d --ratio %s --resolution %s --watermark false", prompt, duration, ratio, resolution)
	content := []map[string]interface{}{
		{"type": "text", "text": text},
	}
	if imageURL != "" {
		content = append(content, map[string]interface{}{
			"type":      "image_url",
			"role":      "first_frame",
			"image_url": map[string]string{"url": imageURL},
		})
	}

	reqBody := map[string]interface{}{
		"model":   model,
		"content": content,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/contents/generations/tasks", bytes.NewReader(reqJSON))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	respBody, err := t.doHTTP(ctx, req)
	if err != nil {
		return "", "", err
	}

	var submitResp struct {
		ID    string `json:"id"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &submitResp); err != nil {
		return "", "", fmt.Errorf("failed to parse submit response: %w (body: %s)", err, truncateForLog(respBody))
	}
	if submitResp.ID == "" {
		msg := ""
		if submitResp.Error != nil {
			msg = fmt.Sprintf(" (code=%s): %s", submitResp.Error.Code, submitResp.Error.Message)
		}
		return "", "", fmt.Errorf("task submit rejected%s", msg)
	}
	taskID = submitResp.ID

	videoURL, err = t.pollVolcengineTask(ctx, baseURL, taskID)
	return videoURL, taskID, err
}

// volcenginePollInterval 轮询间隔。变量而非常量：测试调到毫秒级，
// 否则每个方舟用例都要真等 10 秒。
var volcenginePollInterval = 10 * time.Second

// pollVolcengineTask 轮询方舟任务：queued/running → succeeded/failed/expired/cancelled
func (t *VideoGenerationTool) pollVolcengineTask(ctx context.Context, baseURL, taskID string) (string, error) {
	url := baseURL + "/contents/generations/tasks/" + taskID
	return pollUntilDone(ctx, volcenginePollInterval, func() (done bool, videoURL string, err error) {
		req, rerr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if rerr != nil {
			return true, "", fmt.Errorf("failed to create poll request: %w", rerr)
		}
		req.Header.Set("Authorization", "Bearer "+t.config.APIKey)

		respBody, rerr := t.doHTTP(ctx, req)
		if rerr != nil {
			return true, "", rerr
		}

		var resp struct {
			Status  string `json:"status"`
			Content *struct {
				VideoURL string `json:"video_url"`
			} `json:"content"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return true, "", fmt.Errorf("failed to parse poll response: %w (body: %s)", err, truncateForLog(respBody))
		}

		switch resp.Status {
		case "succeeded":
			if resp.Content == nil || resp.Content.VideoURL == "" {
				return true, "", fmt.Errorf("task succeeded but no video_url returned")
			}
			return true, resp.Content.VideoURL, nil
		case "failed", "expired", "cancelled":
			msg := ""
			if resp.Error != nil {
				msg = fmt.Sprintf(" (code=%s): %s", resp.Error.Code, resp.Error.Message)
			}
			return true, "", fmt.Errorf("task ended with status %s%s", resp.Status, msg)
		default: // queued / running：继续轮询
			return false, "", nil
		}
	})
}

// ---- 公共辅助 ----

// pollUntilDone 每 interval 调一次 check，直到 done 或 ctx 取消/超时。
// 首次查询前先等一个 interval：任务刚提交必然是排队态，立刻查没有意义。
func pollUntilDone(ctx context.Context, interval time.Duration, check func() (done bool, videoURL string, err error)) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("video generation polling aborted (context: %w)", ctx.Err())
		case <-time.After(interval):
		}
		done, videoURL, err := check()
		if done {
			return videoURL, err
		}
	}
}

// doHTTP 执行请求并校验状态码，返回响应体
func (t *VideoGenerationTool) doHTTP(ctx context.Context, req *http.Request) ([]byte, error) {
	client := util.GetHTTPClient()
	if t.config.Timeout > 0 {
		client = &http.Client{Timeout: t.config.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, truncateForLog(body))
	}
	return body, nil
}

// downloadAndSaveVideo 下载生成的 MP4 到输出目录（temp+rename 原子写）
// maxVideoDownloadBytes 单个视频的下载上限。
// 同 maxImageDownloadBytes 的理由：URL 来自后端（可能是插件），不能无限信任。
// 512MB 足够覆盖 1 分钟 1080p 的成片。
const maxVideoDownloadBytes = 512 << 20

func (t *VideoGenerationTool) downloadAndSaveVideo(ctx context.Context, videoURL, customPath string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", videoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	client := util.GetHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoDownloadBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read video data: %w", err)
	}
	if int64(len(data)) > maxVideoDownloadBytes {
		return "", fmt.Errorf("video exceeds %d bytes", maxVideoDownloadBytes)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("downloaded video is empty")
	}

	// 相对输出目录按会话工作目录解析（与 image_gen 同一套规则，别按进程
	// cwd 落盘——桌面版会把视频写进安装目录）；自定义路径走 resolvePath 沙箱。
	outputDir, err := ResolveMediaOutputDir(ctx, t.config.OutputDirectory, "generated_videos")
	if err != nil {
		return "", fmt.Errorf("failed to resolve output directory: %w", err)
	}
	filePath := customPath
	if filePath != "" {
		abs, perr := resolvePath(ctx, filePath)
		if perr != nil {
			return "", fmt.Errorf("invalid output path: %w", perr)
		}
		filePath = abs
	} else {
		filename := fmt.Sprintf("generated_video_%s.mp4", time.Now().Format("20060102_150405"))
		filePath = filepath.Join(outputDir, filename)
	}
	outputDir = filepath.Dir(filePath)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// temp+rename 原子写，避免半截 MP4 被当成完整文件
	tmpFile, err := os.CreateTemp(outputDir, ".video_tmp_*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write video data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to flush video data: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to finalize video file: %w", err)
	}

	return filePath, nil
}

// localImageToDataURI 读取本地图片并转成 base64 data URI（volcengine 支持）
func localImageToDataURI(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read image file: %w", err)
	}
	mime := "image/png"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// truncateForLog 截断响应体，避免错误信息里塞进整个大 JSON
func truncateForLog(body []byte) string {
	s := string(body)
	if len(s) > 512 {
		return s[:512] + "...(truncated)"
	}
	return s
}

// SetConfig 设置视频生成配置
func (t *VideoGenerationTool) SetConfig(config *VideoGenConfig) { t.config = config }

// GetConfig 获取当前配置
func (t *VideoGenerationTool) GetConfig() *VideoGenConfig { return t.config }

// RequiresApproval 实现 agent 侧的 approvalAware：视频生成**每次**都要人工确认。
//
// 判定依据是钱，不是破坏性：内置 provider 都按渲染秒数计费（10 秒 1080p 通常在
// 几元到十几元），单次调用还要跑 1~5 分钟。模型最典型的失败模式是"不满意就换个
// prompt 再生成一次"，一次对话能叠出好几倍账单，而用户根本没机会拦。所以这里
// 用恒真声明，不参与任何风险推断。
//
// 注意：RequiresApproval 与 ApprovalSummary 必须成对实现，agent 的 approvalAware
// 是两方法接口，少一个断言就失败 → 静默变成"不用确认"。
func (t *VideoGenerationTool) RequiresApproval() bool { return true }

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
// 计费相关的参数（时长/分辨率/模型/prompt）由 agent 侧的审批钩子从调用参数里
// 摊开，这里只负责"这是什么工具"。
func (t *VideoGenerationTool) ApprovalSummary() string {
	return "video_generate (paid video generation)"
}
