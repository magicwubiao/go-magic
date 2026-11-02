package tool

import "strings"

// media_catalog.go — 内置媒体厂商的**静态元信息**（单一真源）。
//
// 这里回答四个问题，回答一次，三处复用：
//  1. 设置页下拉里显示什么（Label）、有哪些能力（Capabilities）、去哪儿拿 key（ConsoleURL）；
//  2. 后端用什么默认模型 / 默认 base_url（DefaultModel / DefaultBaseURL）；
//  3. 服务端校验该放开哪些画幅、分辨率、时长（Ratios / Resolutions / Min-MaxDuration）；
//  4. 报错时告诉用户"下一步该做什么"（ConsoleURL + KeyHint）。
//
// 为什么不做成 i18n：Label 一律用厂商品牌名（Kling / Runway / MiniMax / CosyVoice），
// 品牌名在中文和英文界面下都成立，翻译反而制造两套对不上的名字。说明性文字留在
// 前端 i18n 里按能力维度写，不按厂商写。
//
// 新增一家厂商的顺序：先在 catalog 里加一条 → 再写后端实现 → 后端从 catalog 取默认值。
// 反过来（先写后端再补 catalog）会出现"设置页没有这个名字但 config 里能用"的裂缝。

// MediaKind 媒体能力分类。与注册表的三类后端一一对应。
type MediaKind string

const (
	MediaKindImage  MediaKind = "image"
	MediaKindVideo  MediaKind = "video"
	MediaKindSpeech MediaKind = "speech"
)

// 能力标记。用于"这个厂商能不能做图生视频/编辑"这类判断，
// 也是设置页上小徽章的数据来源。
const (
	CapTextToImage  = "t2i"
	CapImageEdit    = "edit"
	CapTextToVideo  = "t2v"
	CapImageToVideo = "i2v"
	CapTextToSpeech = "tts"
	CapSpeechToText = "asr"
)

// BuiltinProviderInfo 一个内置厂商的静态元信息。
type BuiltinProviderInfo struct {
	Kind           MediaKind `json:"kind"`
	Name           string    `json:"name"`
	Label          string    `json:"label"`
	DefaultModel   string    `json:"default_model,omitempty"`
	Models         []string  `json:"models,omitempty"`
	DefaultBaseURL string    `json:"default_base_url,omitempty"`
	ConsoleURL     string    `json:"console_url,omitempty"`
	DocsURL        string    `json:"docs_url,omitempty"`
	KeyHint        string    `json:"key_hint,omitempty"`
	Capabilities   []string  `json:"capabilities,omitempty"`
	// Ratios / Resolutions 是该厂商实际接受的取值。设置页据此裁剪下拉项，
	// 服务端据此校验——不要用"全局支持 16:9/9:16/1:1"去套所有厂商。
	Ratios       []string `json:"ratios,omitempty"`
	Resolutions  []string `json:"resolutions,omitempty"`
	MinDuration  int      `json:"min_duration,omitempty"`
	MaxDuration  int      `json:"max_duration,omitempty"`
	DefaultVoice string   `json:"default_voice,omitempty"`
	// Notes 是一句使用约束（英文，进工具描述与文档，不进 UI 正文）。
	Notes string `json:"notes,omitempty"`
}

// Supports 报告是否具备某项能力。
func (i BuiltinProviderInfo) Supports(capability string) bool {
	for _, c := range i.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// BaseURL 返回配置里的地址，为空时回落到 catalog 的官方地址。
func (i BuiltinProviderInfo) BaseURL(configured string) string {
	if v := strings.TrimRight(strings.TrimSpace(configured), "/"); v != "" {
		return v
	}
	return strings.TrimRight(i.DefaultBaseURL, "/")
}

// Model 返回配置里的模型，为空时回落到 catalog 的推荐模型。
func (i BuiltinProviderInfo) Model(configured string) string {
	if v := strings.TrimSpace(configured); v != "" {
		return v
	}
	return i.DefaultModel
}

// builtinProviderCatalog 内置厂商清单。
//
// 顺序即设置页下拉顺序：先列默认推荐、再列其他。新增厂商追加到对应 kind 末尾，
// 不要插到第一位——否则老用户的默认选择会莫名其妙变掉。
var builtinProviderCatalog = []BuiltinProviderInfo{
	// ---- 图片 ----
	{
		Kind: MediaKindImage, Name: string(ProviderDALLE), Label: "OpenAI DALL·E",
		DefaultModel: "dall-e-3", Models: []string{"dall-e-3", "dall-e-2"},
		DefaultBaseURL: "https://api.openai.com/v1",
		ConsoleURL:     "https://platform.openai.com/api-keys",
		DocsURL:        "https://platform.openai.com/docs/guides/images",
		KeyHint:        "sk-... （OpenAI API key）",
		Capabilities:   []string{CapTextToImage, CapImageEdit},
		Ratios:         []string{"1:1", "16:9", "9:16"},
		Notes:          "Only DALL·E supports image editing (inpaint / variation).",
	},
	{
		Kind: MediaKindImage, Name: string(ProviderStableDiffusion), Label: "Stability AI",
		// 这条路走的是 /stable-image/generate/sd3 端点，模型由端点决定，
		// 这里的 model 字段不生效 —— 所以不列模型，避免设置页给出一个改了没用的下拉。
		DefaultBaseURL: "https://api.stability.ai/v2beta",
		ConsoleURL:     "https://platform.stability.ai/account/keys",
		DocsURL:        "https://platform.stability.ai/docs/api-reference",
		KeyHint:        "sk-... （Stability API key）",
		Capabilities:   []string{CapTextToImage},
		Ratios:         []string{"1:1", "16:9", "9:16", "3:2", "2:3"},
		Notes:          "Uses the sd3 endpoint; the model is fixed by the endpoint and cannot be overridden.",
	},
	{
		Kind: MediaKindImage, Name: string(ProviderMidjourney), Label: "Midjourney (relay)",
		DefaultBaseURL: "",
		KeyHint:        "第三方中转服务的 key，需同时填接口地址",
		Capabilities:   []string{CapTextToImage},
		Ratios:         []string{"1:1", "16:9", "9:16"},
		Notes:          "Relay APIs only submit; this backend returns a pending placeholder.",
	},
	{
		Kind: MediaKindImage, Name: string(ProviderTogether), Label: "Together AI",
		DefaultModel:   "black-forest-labs/FLUX.1-schnell",
		Models:         []string{"black-forest-labs/FLUX.1-schnell", "black-forest-labs/FLUX.1-dev", "stabilityai/stable-diffusion-xl-base-1.0"},
		DefaultBaseURL: "https://api.together.xyz/v1",
		ConsoleURL:     "https://api.together.ai/settings/api-keys",
		DocsURL:        "https://docs.together.ai/docs/images-overview",
		KeyHint:        "Together API key",
		Capabilities:   []string{CapTextToImage},
		Ratios:         []string{"1:1", "16:9", "9:16"},
	},
	{
		Kind: MediaKindImage, Name: "minimax", Label: "MiniMax 海螺",
		DefaultModel: "image-01", Models: []string{"image-01", "image-01-live"},
		DefaultBaseURL: "https://api.minimax.io",
		ConsoleURL:     "https://platform.minimax.io/user-center/basic-information/interface-key",
		DocsURL:        "https://platform.minimax.io/docs/api-reference/image-generation-t2i",
		KeyHint:        "MiniMax API key（中国大陆账号把接口地址改成 https://api.minimaxi.com）",
		Capabilities:   []string{CapTextToImage},
		Ratios:         []string{"1:1", "16:9", "9:16", "4:3", "3:4"},
		Notes:          "Chinese prompts are handled well; aspect ratio is chosen by ratio, not pixels.",
	},

	// ---- 视频 ----
	{
		Kind: MediaKindVideo, Name: string(ProviderDashScope), Label: "阿里云百炼 · 通义万相",
		DefaultModel:   "wan2.2-t2v-plus",
		Models:         []string{"wan2.2-t2v-plus", "wan2.2-i2v-plus", "wanx2.1-t2v-turbo"},
		DefaultBaseURL: "https://dashscope.aliyuncs.com/api/v1",
		ConsoleURL:     "https://bailian.console.aliyun.com/",
		DocsURL:        "https://help.aliyun.com/zh/model-studio/text-to-video-api-reference",
		KeyHint:        "sk-... （百炼 API-KEY，与 CosyVoice 通用）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1"},
		Resolutions:    []string{"480p", "720p", "1080p"},
		MinDuration:    3, MaxDuration: 15,
		Notes: "First-frame images accept a public URL or a base64 data URI (JPG/PNG/BMP/WEBP, max 10MB).",
	},
	{
		Kind: MediaKindVideo, Name: string(ProviderVolcengine), Label: "火山方舟 · Seedance",
		DefaultModel:   "doubao-seedance-1-0-pro-250528",
		Models:         []string{"doubao-seedance-1-0-pro-250528", "doubao-seedance-1-0-lite-t2v-250428", "doubao-seedance-1-0-lite-i2v-250428"},
		DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		ConsoleURL:     "https://console.volcengine.com/ark",
		DocsURL:        "https://www.volcengine.com/docs/82379/1520757",
		KeyHint:        "方舟 API Key（注意模型需先在控制台开通）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
		Resolutions:    []string{"480p", "720p", "1080p"},
		MinDuration:    3, MaxDuration: 15,
		Notes: "Accepts local first-frame images via base64 data URI.",
	},
	{
		Kind: MediaKindVideo, Name: "kling", Label: "可灵 Kling",
		DefaultModel:   "kling-3.0",
		Models:         []string{"kling-3.0", "kling-3.0-turbo", "kling-2.6", "kling-2.5-turbo"},
		DefaultBaseURL: "https://api-beijing.klingai.com",
		ConsoleURL:     "https://klingai.com/dev/api-key",
		DocsURL:        "https://app.klingai.com/cn/dev/document-api",
		KeyHint:        "可灵控制台创建的 API Key（Bearer 直传，不需要 AK/SK 签名）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1"},
		Resolutions:    []string{"720p", "1080p"},
		MinDuration:    3, MaxDuration: 15,
		Notes: "Image-to-video accepts a public URL or a raw base64 string (JPG/PNG, max 10MB; the model name is part of the path).",
	},
	{
		Kind: MediaKindVideo, Name: "runway", Label: "Runway",
		DefaultModel: "gen4.5", Models: []string{"gen4.5", "gen4_turbo"},
		DefaultBaseURL: "https://api.dev.runwayml.com",
		ConsoleURL:     "https://dev.runwayml.com/",
		DocsURL:        "https://docs.dev.runwayml.com/",
		KeyHint:        "Runway API secret（控制台 Organization → API keys）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1"},
		Resolutions:    []string{"720p", "1080p"},
		// Gen-4 系列只接受 5 或 10 秒，这里按 5/10 两个档位钳制。
		MinDuration: 5, MaxDuration: 10,
		Notes: "Duration snaps to 5 or 10 seconds; accepts local first-frame images via data URI.",
	},
	{
		Kind: MediaKindVideo, Name: "minimax", Label: "MiniMax 海螺 H3",
		DefaultModel:   "MiniMax-H3",
		Models:         []string{"MiniMax-H3", "MiniMax-H3-Highspeed", "MiniMax-H3-Lite"},
		DefaultBaseURL: "https://api.minimax.io",
		ConsoleURL:     "https://platform.minimax.io/user-center/basic-information/interface-key",
		DocsURL:        "https://platform.minimax.io/docs/guides/video-generation",
		KeyHint:        "MiniMax API key（中国大陆账号把接口地址改成 https://api.minimaxi.com）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1"},
		Resolutions:    []string{"768p", "2k"},
		MinDuration:    5, MaxDuration: 10,
		Notes: "Success returns content.url directly (no file_id exchange). First-frame images accept a public URL or a base64 data URI (JPG/PNG/WEBP/HEIC/HEIF, max 30MB).",
	},

	// ---- 语音 ----
	{
		Kind: MediaKindSpeech, Name: "openai", Label: "OpenAI TTS / Whisper",
		DefaultModel: "tts-1", Models: []string{"tts-1", "tts-1-hd", "whisper-1"},
		DefaultBaseURL: "https://api.openai.com/v1",
		ConsoleURL:     "https://platform.openai.com/api-keys",
		KeyHint:        "sk-... （OpenAI API key）",
		Capabilities:   []string{CapTextToSpeech, CapSpeechToText},
		DefaultVoice:   "alloy",
	},
	{
		Kind: MediaKindSpeech, Name: "elevenlabs", Label: "ElevenLabs",
		DefaultModel: "eleven_multilingual_v2", Models: []string{"eleven_multilingual_v2", "eleven_turbo_v2_5"},
		DefaultBaseURL: "https://api.elevenlabs.io/v1",
		ConsoleURL:     "https://elevenlabs.io/app/settings/api-keys",
		KeyHint:        "xi-api-key",
		Capabilities:   []string{CapTextToSpeech},
		DefaultVoice:   "21m00Tcm4TlvDq8ikWAM",
	},
	{
		Kind: MediaKindSpeech, Name: "azure", Label: "Azure Speech",
		DefaultModel: "neural-tts", DefaultBaseURL: "",
		ConsoleURL:   "https://portal.azure.com/",
		KeyHint:      "Speech 资源的 key + 区域（region 必填，如 eastasia）",
		Capabilities: []string{CapTextToSpeech, CapSpeechToText},
		DefaultVoice: "zh-CN-XiaoxiaoNeural",
		Notes:        "Region is required; without it every request returns 401.",
	},
	{
		Kind: MediaKindSpeech, Name: "google", Label: "Google Cloud TTS / STT",
		DefaultModel: "neural2", DefaultBaseURL: "",
		ConsoleURL:   "https://console.cloud.google.com/apis/credentials",
		KeyHint:      "服务账号 JSON 的路径或内容（不是普通 API key）",
		Capabilities: []string{CapTextToSpeech, CapSpeechToText},
		Notes:        "Requires a service-account credential, not a plain API key.",
	},
	{
		Kind: MediaKindSpeech, Name: "whisper", Label: "OpenAI Whisper (ASR)",
		DefaultModel: "whisper-1", Models: []string{"whisper-1"},
		DefaultBaseURL: "https://api.openai.com/v1",
		ConsoleURL:     "https://platform.openai.com/api-keys",
		KeyHint:        "sk-... （与 OpenAI TTS 共用一个 key）",
		Capabilities:   []string{CapSpeechToText},
		Notes:          "Speech-to-text only; use provider openai for synthesis.",
	},
	{
		Kind: MediaKindSpeech, Name: "cosyvoice", Label: "阿里云 CosyVoice",
		DefaultModel: "cosyvoice-v2", Models: []string{"cosyvoice-v2", "cosyvoice-v3-flash", "cosyvoice-v3-plus"},
		DefaultBaseURL: "https://dashscope.aliyuncs.com/api/v1",
		ConsoleURL:     "https://bailian.console.aliyun.com/",
		DocsURL:        "https://help.aliyun.com/zh/model-studio/cosyvoice-tts-http-api",
		KeyHint:        "sk-... （百炼 API-KEY，与通义万相通用）",
		Capabilities:   []string{CapTextToSpeech},
		DefaultVoice:   "longxiaochun_v2",
		Notes:          "Voice IDs are model-specific: cosyvoice-v2 uses longxiaochun_v2, v3 uses longanhuan.",
	},
	{
		Kind: MediaKindSpeech, Name: "minimax", Label: "MiniMax 语音",
		DefaultModel:   "speech-02-hd",
		Models:         []string{"speech-02-hd", "speech-02-turbo", "speech-01-hd", "speech-01-turbo"},
		DefaultBaseURL: "https://api.minimaxi.com",
		ConsoleURL:     "https://platform.minimaxi.com/user-center/basic-information/interface-key",
		DocsURL:        "https://platform.minimaxi.com/docs/guides/text-to-speech",
		KeyHint:        "MiniMax API key + GroupId（控制台接口密钥页同一页；海外账号把接口地址改成 https://api.minimax.io）",
		Capabilities:   []string{CapTextToSpeech},
		DefaultVoice:   "male-qn-qingse",
		Notes: "GroupId is required (voice.providers.minimax.group_id or MINIMAX_GROUP_ID env); " +
			"response audio is hex-encoded and decoded by the kernel.",
	},
}

// mediaProviderAliases 常见手写变体 → 主名。
//
// 存在的理由：用户从文档/博客抄来的名字经常是模型名或产品名（hailuo、seedance、
// qwen-tts），而不是我们选定的 provider 标识。让 lookup 直接认下来，比在报错里
// 列一堆"available: ..."让用户自己对照要省一轮。
var mediaProviderAliases = map[MediaKind]map[string]string{
	MediaKindImage: {
		"dalle": "dall-e", "dall_e": "dall-e", "openai": "dall-e",
		"sd": "stable-diffusion", "stability": "stable-diffusion",
		"mj": "midjourney", "flux": "together",
		"hailuo": "minimax", "hailuoai": "minimax",
	},
	MediaKindVideo: {
		"wan": "dashscope", "qwen": "dashscope", "tongyi": "dashscope",
		"seedance": "volcengine", "ark": "volcengine", "doubao": "volcengine",
		"keling": "kling", "可灵": "kling",
		"runwayml": "runway",
		"hailuo":   "minimax", "hailuoai": "minimax", "minimax-h3": "minimax",
	},
	MediaKindSpeech: {
		"qwen-tts": "cosyvoice", "qwen_tts": "cosyvoice", "dashscope": "cosyvoice",
		"ali": "cosyvoice", "cosyvoice-v2": "cosyvoice",
		"hailuo": "minimax", "minimaxi": "minimax", "海螺": "minimax",
	},
}

// CanonicalProviderName 把别名解析成主名；不是别名时原样返回（小写去空白）。
//
// 注意只做**名字**归一，不做可用性判断：别名指向的 provider 若没注册，
// lookup 仍然查不到，报错信息用的是归一后的名字，用户能看出自己写的是别名。
func CanonicalProviderName(kind MediaKind, name string) string {
	key := normalizeBackendName(name)
	if aliases, ok := mediaProviderAliases[kind]; ok {
		if canonical, ok := aliases[key]; ok {
			return canonical
		}
	}
	return key
}

// ProviderInfos 返回某一类的内置厂商元信息（catalog 顺序）。
func ProviderInfos(kind MediaKind) []BuiltinProviderInfo {
	out := make([]BuiltinProviderInfo, 0, len(builtinProviderCatalog))
	for _, info := range builtinProviderCatalog {
		if info.Kind == kind {
			out = append(out, info)
		}
	}
	return out
}

// ProviderInfo 按 kind + 名字（含别名）查元信息。
func ProviderInfo(kind MediaKind, name string) (BuiltinProviderInfo, bool) {
	canonical := CanonicalProviderName(kind, name)
	for _, info := range builtinProviderCatalog {
		if info.Kind == kind && info.Name == canonical {
			return info, true
		}
	}
	return BuiltinProviderInfo{}, false
}

// ProviderLabel 返回用于错误提示的展示名，查不到就回落成标识本身。
func ProviderLabel(kind MediaKind, name string) string {
	if info, ok := ProviderInfo(kind, name); ok && info.Label != "" {
		return info.Label
	}
	return name
}

// ConfigHelp 返回"这个 provider 还没配好时该怎么做"的完整指引。
//
// 为什么要用 catalog 生成而不是各工具手写一句话：用户看到 not_configured 时
// 唯一想知道的是"去哪拿 key、填到哪个字段"。手写文案只能说到一半（"add an
// image_gen section"），而 console 地址、provider 标识、环境变量名都在这里，
// 拼一次就够，四处复用时也不会各写各的。
func ConfigHelp(kind MediaKind, name string) string {
	info, ok := ProviderInfo(kind, name)
	section, envPrefix, keyField := mediaConfigTarget(kind)

	if !ok {
		return "Set a provider in config.json (" + section + ") or through the media settings page."
	}

	where := ""
	if info.ConsoleURL != "" {
		where = "Get a key at " + info.ConsoleURL + ". "
	}
	return where + "Then save it in config.json as " +
		`"` + section + `": {"provider": "` + info.Name + `", "` + keyField + `": "..."}` +
		" (or set " + envPrefix + "), or use Settings → Media in the web UI."
}

// mediaConfigTarget 返回配置段名 / 环境变量前缀 / 密钥字段名。
func mediaConfigTarget(kind MediaKind) (section, envPrefix, keyField string) {
	switch kind {
	case MediaKindImage:
		return "image_gen", "IMAGE_GEN_API_KEY", "api_key"
	case MediaKindVideo:
		return "video_gen", "VIDEO_GEN_API_KEY", "api_key"
	case MediaKindSpeech:
		return "voice", "VOICE_API_KEY", "api_key"
	}
	return "config.json", "", "api_key"
}

// infoOrDefaultModel 返回配置里的模型；为空时用 catalog 的推荐值。
//
// 后端不该各自写死模型名：写死之后"设置页能选模型"就是假的，而 catalog 里的
// DefaultModel 也会与实际请求脱节（曾经就出现过 catalog 写 FLUX、代码发 SDXL）。
func infoOrDefaultModel(kind MediaKind, name, configured string) string {
	if v := strings.TrimSpace(configured); v != "" {
		return v
	}
	if info, ok := ProviderInfo(kind, name); ok {
		return info.DefaultModel
	}
	return ""
}

// clampDuration 把时长钳到厂商支持的区间；区间为 0 时不限制。
//
// 为什么不报错：用户写 duration=8 而厂商只支持 5/10，最合理的行为是取最近的
// 档位并**在结果里回显实际值**，而不是让一次 3 分钟的任务死在参数校验上。
// 回显由调用方负责（VideoResult 之外的 result map 带 duration 字段）。
func clampDuration(d, min, max int) int {
	if min > 0 && d < min {
		d = min
	}
	if max > 0 && d > max {
		d = max
	}
	return d
}

// snapDuration 把时长吸附到指定档位里最接近的一个（Runway 只认 5/10）。
func snapDuration(d int, steps ...int) int {
	if len(steps) == 0 {
		return d
	}
	best := steps[0]
	bestDiff := absInt(d - best)
	for _, s := range steps[1:] {
		if diff := absInt(d - s); diff < bestDiff {
			best, bestDiff = s, diff
		}
	}
	return best
}

// resolutionFor 把统一的 480p/720p/1080p 映射成厂商自己的分辨率标识，
// 未命中时回落 fallback。
func resolutionFor(mapping map[string]string, resolution, fallback string) string {
	if v, ok := mapping[strings.ToLower(strings.TrimSpace(resolution))]; ok {
		return v
	}
	return fallback
}
