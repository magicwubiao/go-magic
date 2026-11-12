package tool

import (
	"strings"

	"github.com/magicwubiao/go-magic/internal/voice"
)

// media_catalog.go — 内置媒体厂商的**静态元信息**（单一真源）。
//
// 这里回答四个问题，回答一次，三处复用：
//  1. 设置页下拉里显示什么（Label）、有哪些能力（Capabilities）、去哪儿拿 key（ConsoleURL）；
//  2. 后端用什么默认模型 / 默认 base_url（DefaultModel / DefaultBaseURL）；
//  3. 服务端校验该放开哪些画幅、分辨率、时长（Ratios / Resolutions / Min-MaxDuration）；
//  4. 报错时告诉用户"下一步该做什么"（ConsoleURL + KeyHint）。
//
// 一个例外：语音的 openai / elevenlabs 两家，DefaultModel 与 DefaultVoice
// **不在这里定义**，而是引用 internal/voice 的默认值表。因为这两家的请求是 voice 包
// 发的，而依赖方向只能是 tool → voice；把值写在 catalog 就必然要再抄一份到 voice，
// 抄漏一次就变成"设置页写着 A、请求发的是 B"（真发生过）。azure / google 例外：
// 它们的请求体里没有 model 字段，catalog 里那两个模型名只是下拉占位，不参与请求。
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
	// VoiceFollowsLanguage 表示这家没有单一默认音色，默认值由 language 决定
	// （Azure 的音色 ID 自带语言、Google 的默认音色是"语言+档位"）。
	// 这种厂商 DefaultVoice 必须留空：写死一个值就是在说谎，也是前端"预填音色"
	// 会把默认值钉死在上一门语言上的原因。
	VoiceFollowsLanguage bool `json:"voice_follows_language,omitempty"`
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
		// provider 标识仍然是 "dall-e"：config.json 里存的、用户手上配置写的都是这个
		// 值，改名等于让所有存量配置失效。DALL·E 本体已于 2026-05-12 停用，实际调用
		// 的是 GPT Image 系列 —— 所以 Label 与模型清单必须跟着换，标识留着。
		Kind: MediaKindImage, Name: string(ProviderDALLE), Label: "OpenAI GPT Image",
		// 模型 ID 必须写全：gpt-image-2.5 不是一个可调用的名字，真正上线的是
		// gpt-image-2.5-flare（默认，快）和 gpt-image-2.5-sunburst（精修）。
		// 少写后缀就是一次 404，而设置页看不出来——它只是把字符串原样发出去。
		DefaultModel:   "gpt-image-2.5-flare",
		Models:         []string{"gpt-image-2.5-flare", "gpt-image-2.5-sunburst", "gpt-image-2"},
		DefaultBaseURL: "https://api.openai.com/v1",
		ConsoleURL:     "https://platform.openai.com/api-keys",
		DocsURL:        "https://platform.openai.com/docs/guides/image-generation",
		KeyHint:        "sk-... （OpenAI API key）",
		Capabilities:   []string{CapTextToImage, CapImageEdit},
		Ratios:         []string{"1:1", "16:9", "9:16"},
		Notes:          "2.5 models accept quality low/medium/high/xhigh/max and custom WxH sizes (multiples of 16); the UI offers 1024x1024, 1536x1024 and 1024x1536, other values are snapped. DALL-E 2/3 were retired on 2026-05-12.",
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
		DefaultModel: "black-forest-labs/FLUX.2-pro",
		Models: []string{
			"black-forest-labs/FLUX.2-pro",
			"black-forest-labs/FLUX.2-flex",
			"black-forest-labs/FLUX.2-max",
			"black-forest-labs/FLUX.2-dev",
		},
		DefaultBaseURL: "https://api.together.xyz/v1",
		ConsoleURL:     "https://api.together.ai/settings/api-keys",
		DocsURL:        "https://docs.together.ai/docs/images-overview",
		KeyHint:        "Together API key",
		Capabilities:   []string{CapTextToImage},
		Ratios:         []string{"1:1", "16:9", "9:16"},
		Notes:          "FLUX.2 replaces the FLUX.1 line; width/height are passed as pixels, so any ratio works.",
	},
	{
		Kind: MediaKindImage, Name: "minimax", Label: "MiniMax 海螺",
		// 只能列 image-01 / image-01-live：它们是 /v1/image_generation 这个
		// legacy 端点唯一接受的型号（文档里 model 是单值枚举）。新一代
		// MiniMax-H3-Image 换了独立端点，在本后端重写前写进来只会 400。
		DefaultModel: "image-01", Models: []string{"image-01", "image-01-live"},
		DefaultBaseURL: "https://api.minimax.io",
		ConsoleURL:     "https://platform.minimax.io/user-center/basic-information/interface-key",
		DocsURL:        "https://platform.minimax.io/docs/api-reference/image-generation-t2i",
		KeyHint:        "MiniMax API key（中国大陆账号把接口地址改成 https://api.minimaxi.com）",
		Capabilities:   []string{CapTextToImage},
		Ratios:         []string{"1:1", "16:9", "9:16", "4:3", "3:4"},
		Notes:          "Uses the legacy /v1/image_generation endpoint, whose model enum is image-01; image-01-live is the style-preset variant. The newer H3-Image models need a different endpoint and are not wired yet.",
	},

	// ---- 视频 ----
	{
		Kind: MediaKindVideo, Name: string(ProviderDashScope), Label: "阿里云百炼 · 通义万相",
		// 默认留在 wan2.7（成熟、价格稳定）。wan3.0 走的是同一个 video-synthesis
		// 端点：t2v 用 resolution+ratio、i2v 用 input.media，只是时长到 30 秒、
		// 多一个 480P 档和 21:9，所以这里能直接列出来。
		DefaultModel:   "wan2.7-t2v",
		Models:         []string{"wan3.0-video", "wan3.0-video-prime", "wan2.7-t2v", "wan2.7-i2v", "wan2.6-t2v", "wan2.6-i2v"},
		DefaultBaseURL: "https://dashscope.aliyuncs.com/api/v1",
		ConsoleURL:     "https://bailian.console.aliyun.com/",
		DocsURL:        "https://help.aliyun.com/zh/model-studio/text-to-video-api-reference",
		KeyHint:        "sk-... （百炼 API-KEY，与 CosyVoice 通用）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
		// 480p 只有 wan3.0 有，所以排在最后：设置页切换厂商时取第一项当默认，
		// 把 480p 放前面会让默认模型（2.7）拿到一个自己没有的档位。
		Resolutions: []string{"720p", "1080p", "480p"},
		// 上限按 wan3.0 标（2-30 秒）；2.7 / 2.6 只到 15 秒，由 durationForModel
		// 按型号收窄，否则会带着 20 秒去请求一个 15 秒封顶的模型。
		MinDuration: 2, MaxDuration: 30,
		Notes: "Wan 3.0 and 2.7 take resolution + ratio (2.6 t2v takes size). Image-to-video differs too: 3.0/2.7 use input.media[{type:first_frame,url}], 2.6 uses input.img_url. 3.0 supports 480P, 21:9 and up to 30s; 2.7 is 720P/1080P and 2-15s. First-frame images accept a public URL or a base64 data URI (JPG/PNG/BMP/WEBP, max 10MB).",
	},
	{
		Kind: MediaKindVideo, Name: string(ProviderVolcengine), Label: "火山方舟 · Seedance",
		// Seedance 2.5 支持单次 30 秒、全模态参考；2.0 系列按 pro / fast / mini 分档。
		DefaultModel: "doubao-seedance-2-5-260628",
		Models: []string{
			"doubao-seedance-2-5-260628",
			"doubao-seedance-2-0-260128",
			"doubao-seedance-2-0-fast-260128",
			"doubao-seedance-2-0-mini-260615",
		},
		DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		ConsoleURL:     "https://console.volcengine.com/ark",
		DocsURL:        "https://www.volcengine.com/docs/82379/1520757",
		KeyHint:        "方舟 API Key（注意模型需先在控制台开通）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
		Resolutions:    []string{"480p", "720p", "1080p"},
		// 2.5 下限 4 秒（1.0 是 3 秒），上限从 15 秒放宽到 30 秒。
		// 注意上限是"这家最强模型"的上限：2.0 系列只到 15 秒，选 2.0 时由
		// durationForModel 按型号收窄，否则会带着 20 秒去请求一个 15 秒封顶的模型。
		MinDuration: 4, MaxDuration: 30,
		Notes: "Accepts local first-frame images via base64 data URI; with a first frame the ratio must be adaptive. Seedance 2.5 supports up to 30s per generation, the 2.0 series stops at 15s.",
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
		Notes: "Image-to-video accepts a public URL or a raw base64 string (JPG/PNG, max 10MB; the model name is part of the path). Kling 2.6 / 2.5-turbo only render 5 or 10 seconds.",
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
		DefaultModel: "MiniMax-H3",
		// H3 系列共享同一套 content[] 协议，换模型只改 model 字段；差异在分辨率
		// 与时长：H3 支持 768P/2K，Highspeed/Lite 只有 480P/768P，H3-Max 下限 5 秒。
		Models:         []string{"MiniMax-H3", "MiniMax-H3-Max", "MiniMax-H3-Highspeed", "MiniMax-H3-Lite"},
		DefaultBaseURL: "https://api.minimax.io",
		ConsoleURL:     "https://platform.minimax.io/user-center/basic-information/interface-key",
		DocsURL:        "https://platform.minimax.io/docs/guides/video-generation",
		KeyHint:        "MiniMax API key（中国大陆账号把接口地址改成 https://api.minimaxi.com）",
		Capabilities:   []string{CapTextToVideo, CapImageToVideo},
		Ratios:         []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
		Resolutions:    []string{"768p", "2k"},
		// H3 是 4-15 秒（不是上一代 Hailuo 的 5-10）：写 5-10 会把 4 秒和
		// 11-15 秒整段能力砍掉，而模型本身是支持的。
		MinDuration: 4, MaxDuration: 15,
		Notes: "H3 runs 4-15s at 768P/2K; Highspeed/Lite are 480P/768P only and H3-Max starts at 5s. Success returns content.url directly (no file_id exchange). First-frame images accept a public URL or a base64 data URI (JPG/PNG/WEBP/HEIC/HEIF, max 30MB).",
	},

	// ---- 语音 ----
	{
		Kind: MediaKindSpeech, Name: "openai", Label: "OpenAI TTS / Whisper",
		// 默认值与 voice 包共用一张表（见文件头注释）。gpt-4o-mini-tts 支持
		// instructions 控制语气；tts-1 系列仍在但已是上一代；whisper-1 只有识别能力，
		// 走的是 provider openai 的 ASR 通道。
		DefaultModel: voice.DefaultModelFor("openai"),
		Models: []string{
			voice.DefaultModelFor("openai"),
			"tts-1-hd", "tts-1", "whisper-1",
		},
		DefaultBaseURL: "https://api.openai.com/v1",
		ConsoleURL:     "https://platform.openai.com/api-keys",
		KeyHint:        "sk-... （OpenAI API key）",
		Capabilities:   []string{CapTextToSpeech, CapSpeechToText},
		DefaultVoice:   voice.DefaultVoiceFor("openai"),
		Notes:          "gpt-4o-mini-tts accepts a plain-language instructions field; tts-1/tts-1-hd do not.",
	},
	{
		Kind: MediaKindSpeech, Name: "elevenlabs", Label: "ElevenLabs",
		// 默认值与 voice 包共用一张表。eleven_v3 表现力更强，但要求输入较长，
		// 一句话的短文本容易被拒，所以列进 Models 可选、不做默认。
		DefaultModel: voice.DefaultModelFor("elevenlabs"),
		Models: []string{
			voice.DefaultModelFor("elevenlabs"),
			"eleven_v3", "eleven_flash_v2_5",
		},
		DefaultBaseURL: "https://api.elevenlabs.io/v1",
		ConsoleURL:     "https://elevenlabs.io/app/settings/api-keys",
		KeyHint:        "xi-api-key",
		Capabilities:   []string{CapTextToSpeech},
		DefaultVoice:   voice.DefaultVoiceFor("elevenlabs"),
		Notes:          "eleven_v3 is the most expressive but expects longer input; eleven_flash_v2_5 is the low-latency tier for realtime use.",
	},
	{
		Kind: MediaKindSpeech, Name: "azure", Label: "Azure Speech",
		// model 在这个厂商身上不参与请求（Azure 用 SSML 里的 voice 名），
		// 这里只为设置页下拉有个占位，改它不会影响任何请求。
		DefaultModel: "neural-tts", DefaultBaseURL: "",
		ConsoleURL:   "https://portal.azure.com/",
		KeyHint:      "Speech 资源的 key + 区域（region 必填，如 eastasia）",
		Capabilities: []string{CapTextToSpeech, CapSpeechToText},
		// 默认音色跟着 language 走（zh-CN → Xiaoxiao、en-US → Jenny…），
		// 写死一个值只会误导：language 填 en 却在占位里看到中文音色。
		VoiceFollowsLanguage: true,
		Notes:                "Region is required; without it every request returns 401. The default voice follows voice.language.",
	},
	{
		Kind: MediaKindSpeech, Name: "google", Label: "Google Cloud TTS / STT",
		// 同 azure：Google 用 voice.languageCode + name，没有 model 参数，
		// 这里的值只是下拉占位；默认音色是"语言 + -Standard-C"，也随 language 变。
		DefaultModel: "neural2", DefaultBaseURL: "",
		ConsoleURL:           "https://console.cloud.google.com/apis/credentials",
		KeyHint:              "服务账号 JSON 的路径或内容（不是普通 API key）",
		Capabilities:         []string{CapTextToSpeech, CapSpeechToText},
		VoiceFollowsLanguage: true,
		Notes:                "Requires a service-account credential, not a plain API key. The default voice follows voice.language.",
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
		// 语音模型已到 2.8（speech-2.8-hd / turbo 是当前主推），speech-02 是
		// 上一代，仍可用所以留在清单里，但不再做默认。
		DefaultModel: "speech-2.8-hd",
		Models: []string{
			"speech-2.8-hd", "speech-2.8-turbo",
			"speech-2.6-hd", "speech-2.6-turbo",
			"speech-02-hd", "speech-02-turbo",
			"asr-1.0",
		},
		DefaultBaseURL: "https://api.minimaxi.com",
		ConsoleURL:     "https://platform.minimaxi.com/user-center/basic-information/interface-key",
		DocsURL:        "https://platform.minimaxi.com/docs/guides/text-to-speech",
		KeyHint:        "MiniMax API key + GroupId（控制台接口密钥页同一页；海外账号把接口地址改成 https://api.minimax.io）",
		Capabilities:   []string{CapTextToSpeech, CapSpeechToText},
		DefaultVoice:   "male-qn-qingse",
		Notes: "GroupId is required for TTS only (voice.providers.minimax.group_id or MINIMAX_GROUP_ID env); " +
			"TTS response audio is hex-encoded and decoded by the kernel. ASR uses model asr-1.0 via /v1/speech_to_text " +
			"(no GroupId; audio up to 500s and 50MB per request).",
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
