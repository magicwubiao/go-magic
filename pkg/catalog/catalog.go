// Package catalog 是全仓库唯一的「供应商 + 模型」目录源（single source of truth）。
//
// 历史背景：同一份数据曾散落在 6 处各自维护 —— pkg/config ListProviders、
// internal/provider defaultModelRegistry、internal/provider/config.go 的
// baseURL/defaultModel 两张表、openai_compatible.go 构造函数兜底 switch、
// cmd/magic/model.go、cmd/magic/setup.go。更新模型要 grep 全库逐份同步，
// 历史上多次漏改：mimo/hunyuan 的 baseURL 两处漂移、vllm 默认模型三处各异、
// 构造函数兜底残留远古模型（command-r-plus、Mixtral-8x7B、glm-4）。
//
// 现在更新模型 = 只改本文件的对应条目，其余消费方全部派生：
//   - pkg/config.ListProviders()      → Web UI 供应商目录/预设
//   - internal/provider.GetDefaultModels / modelRegistryVision → 模型元数据与视觉判定
//   - internal/provider/config.go     → getDefaultModel / getDefaultBaseURL 兜底
//   - internal/provider 各构造函数    → model/baseURL 兜底
//   - cmd/magic model.go / setup.go   → CLI --list、交互式选择、setup 向导
//
// 运行时的"在线获取最新模型"（POST /api/providers/{name}/fetch-models）是
// 用户侧的动态补充，不替代本目录；本目录是拉取失败时的兜底与离线默认值。
package catalog

// Model 目录中的一个模型条目。
type Model struct {
	ID          string
	Name        string // 人类可读名；空则展示时回退 ID
	Description string
	ContextLen  int // 上下文窗口；0 = 未知
	// Vision 三态：nil = 未知（交给 convert.go 的名称启发式判定）；
	// true/false = 已确认，作为权威值短路启发式。只对有证据的模型设置
	// （历史 registry 条目原样保留），不要凭猜测添加。
	Vision *bool
}

// Provider 目录中的一个供应商条目。
type Provider struct {
	Name         string
	Aliases      []string // 兼容别名（kimi→moonshot、doubao→huoshan），配置里用别名同样生效
	DisplayName  string
	Description  string
	BaseURL      string // 官方 API 端点（与构造函数兜底同源）；custom 为空 = 用户自填
	NeedsAPIKey  bool
	NeedsBaseURL bool
	// DefaultModel 默认模型 ID：配置缺省、构造函数兜底、CLI 预选共用。
	//
	// 选值准则：① 必须是 Models 里真实存在的 ID（TestCatalogIntegrity 强制）；
	// ② 取**当前在售最新一代**里"能用、够新、不肉疼"的那个，而非最贵的旗舰
	// （与 Models[0] 不同是常态：目录首条多为旗舰）；
	// ③ 已下线/被取代的旧 ID 一律不用（见 TestNoRetiredModels）。
	//
	// 默认值决定新用户第一次跑起来看到什么，所以宁可跟随最新主力，
	// 也不要把上代或最贵旗舰当作默认。
	DefaultModel string
	Models       []Model
	// Note CLI --list 输出的补充说明（本地部署/聚合网关等场景）。
	Note string
	// Group 供 setup 向导分组展示：recommended / china / local / aggregator / other / custom
	Group string
}

func b(v bool) *bool { return &v }

// catalog 是唯一的目录数据。顺序即 Web UI / CLI 的展示顺序，不要随意调整。
var catalog = []Provider{
	{
		Name: "deepseek", DisplayName: "DeepSeek",
		Description:  "DeepSeek V4.1 - 高性价比，原生多模态",
		BaseURL:      "https://api.deepseek.com",
		NeedsAPIKey:  true,
		DefaultModel: "deepseek-flash",
		Group:        "recommended",
		Models: []Model{
			// 2026-09-10 发布：V4.1-Flash（552B MoE，官方模型名 deepseek-flash），
			// 原生多模态视觉理解；V4-Flash 已下线，旧名临时路由到 V4.1-Flash。
			{ID: "deepseek-flash", Name: "DeepSeek V4.1 Flash", Description: "最新主力，原生视觉", ContextLen: 1000000, Vision: b(true)},
			// V4-Pro 官方宣布 9-14 后继续提供服务（应用户需求延长）。
			{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Description: "上代旗舰推理", ContextLen: 1000000, Vision: b(false)},
			// 兼容别名：官方临时路由到 V4.1-Flash（多模态），故视觉判定为 true。
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Description: "旧名，路由到 V4.1", ContextLen: 1000000, Vision: b(true)},
		},
	},
	{
		Name: "openai", DisplayName: "OpenAI",
		Description:  "GPT-6 系列（2026-09）",
		BaseURL:      "https://api.openai.com/v1",
		NeedsAPIKey:  true,
		DefaultModel: "gpt-6.1-sol",
		Group:        "recommended",
		Models: []Model{
			// 2026-09-29：GPT-6 Sol 的升级版，官方称"近 Astra 能力、约五分之一价格"。
			{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Description: "升级版 Sol，近旗舰能力", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6-astra", Name: "GPT-6 Astra", Description: "最强旗舰", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6-sol", Name: "GPT-6 Sol", Description: "上代均衡（已被 6.1 Sol 取代）", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6-luna", Name: "GPT-6 Luna", Description: "最快最便宜", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-5.6", Name: "GPT-5.6 Sol", Description: "上代旗舰", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Description: "上代均衡", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Description: "上代轻量", ContextLen: 1050000, Vision: b(true)},
			{ID: "o3-mini", Name: "o3 Mini", Description: "o3 推理系列（纯文本）"},
		},
	},
	{
		Name: "anthropic", DisplayName: "Anthropic",
		Description:  "Claude 5.5 / Fable 5.1 - 强推理能力",
		BaseURL:      "https://api.anthropic.com",
		NeedsAPIKey:  true,
		DefaultModel: "claude-opus-5-5",
		Group:        "recommended",
		Models: []Model{
			// 2026-09-22 发布：长时间运行的智能体编码与知识工作。
			{ID: "claude-opus-5-5", Name: "Claude Opus 5.5", Description: "最新旗舰，自适应思考", ContextLen: 1000000, Vision: b(true)},
			{ID: "claude-fable-5-1", Name: "Claude Fable 5.1", Description: "Fable 级推理旗舰", ContextLen: 1000000, Vision: b(true)},
			{ID: "claude-opus-5", Name: "Claude Opus 5", Description: "上代旗舰", ContextLen: 1000000, Vision: b(true)},
			{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Description: "速度与智能均衡", ContextLen: 1000000, Vision: b(true)},
			{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", Description: "最快模型", ContextLen: 200000, Vision: b(true)},
		},
	},
	{
		Name: "dashscope", DisplayName: "DashScope (通义千问)",
		Description:  "阿里云通义千问大模型",
		BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "qwen3.8-flash",
		Group:        "china",
		Models: []Model{
			// 2026-09：Qwen3.8 系列（2.4T MoE，原生视觉，1M 上下文）。
			{ID: "qwen3.8-max", Name: "Qwen 3.8 Max", Description: "旗舰（快照 0902）", ContextLen: 1000000, Vision: b(true)},
			{ID: "qwen3.8-flash", Name: "Qwen 3.8 Flash", Description: "最新多模态主力", ContextLen: 1000000, Vision: b(true)},
			{ID: "qwen3.8-omni-flash", Name: "Qwen 3.8 Omni Flash", Description: "全模态（音视频）", ContextLen: 1000000, Vision: b(true)},
			{ID: "qwen3.7-plus", Name: "Qwen 3.7 Plus"},
			{ID: "qwen3.7-flash", Name: "Qwen 3.7 Flash"},
			{ID: "qwen3.5-omni-plus", Name: "Qwen 3.5 Omni Plus", Description: "多模态"},
			{ID: "qwen-long", Name: "Qwen Long", ContextLen: 1000000},
		},
	},
	{
		Name: "minimax", DisplayName: "MiniMax",
		Description:  "MiniMax 大模型",
		BaseURL:      "https://api.minimax.chat/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "MiniMax-M3",
		Group:        "china",
		Models: []Model{
			// M3：428B/23B MoE，1M 上下文，原生多模态（图片+视频）。
			{ID: "MiniMax-M3", Name: "MiniMax M3", Description: "旗舰，原生多模态，1M 上下文", ContextLen: 1000000, Vision: b(true)},
			{ID: "MiniMax-M2.7", Name: "MiniMax M2.7", Description: "增强编码"},
			{ID: "MiniMax-M2.5", Name: "MiniMax M2.5", Description: "高级推理"},
		},
	},
	{
		Name: "ollama", DisplayName: "Ollama",
		Description:  "本地部署的开源大模型",
		BaseURL:      "http://localhost:11434",
		NeedsBaseURL: true,
		DefaultModel: "qwen3.8",
		Note:         "模型取决于本地 Ollama 已拉取的模型列表",
		Group:        "local",
		Models: []Model{
			{ID: "qwen3.8", Name: "Qwen 3.8", Description: "阿里开源模型", ContextLen: 131072, Vision: b(false)},
			{ID: "gpt-oss", Name: "GPT-OSS", Description: "OpenAI 开源模型", ContextLen: 131072, Vision: b(false)},
			{ID: "deepseek-r1", Name: "DeepSeek R1", Description: "推理模型", ContextLen: 131072, Vision: b(false)},
			{ID: "gemma4", Name: "Gemma 4"},
			{ID: "phi4", Name: "Phi 4"},
		},
	},
	{
		Name: "openrouter", DisplayName: "OpenRouter",
		Description:  "统一 API 网关，支持多种模型",
		BaseURL:      "https://openrouter.ai/api/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "openai/gpt-6.1-sol",
		Note:         "完整列表见 https://openrouter.ai/models",
		Group:        "aggregator",
		Models: []Model{
			{ID: "openai/gpt-6.1-sol", Name: "GPT-6.1 Sol", Description: "升级版 Sol（与 openai 目录默认一致）"},
			{ID: "openai/gpt-6-sol", Name: "GPT-6 Sol"},
			{ID: "openai/gpt-5.6", Name: "GPT-5.6 Sol"},
			{ID: "anthropic/claude-opus-5-5", Name: "Claude Opus 5.5"},
			{ID: "anthropic/claude-sonnet-5", Name: "Claude Sonnet 5"},
			{ID: "google/gemini-3.8-flash", Name: "Gemini 3.8 Flash"},
			{ID: "deepseek/deepseek-flash", Name: "DeepSeek V4.1 Flash"},
		},
	},
	{
		Name: "vllm", DisplayName: "vLLM",
		Description:  "本地部署的高性能推理引擎",
		BaseURL:      "http://localhost:8000/v1",
		NeedsBaseURL: true,
		DefaultModel: "default",
		Note:         "模型取决于 vLLM 服务部署配置",
		Group:        "local",
		Models: []Model{
			{ID: "default", Name: "default", Description: "占位；以服务端实际部署为准"},
		},
	},
	{
		Name: "zhipu", DisplayName: "智谱 AI (Zhipu)",
		Description:  "智谱 GLM 系列大模型",
		BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "glm-5.3",
		Group:        "china",
		Models: []Model{
			// GLM-5.3（2026-08）：753B/40B，1M 上下文，纯文本输入。
			{ID: "glm-5.3", Name: "GLM-5.3", Description: "最新旗舰，Agentic 编码", ContextLen: 1000000, Vision: b(false)},
			// GLM-5.3-Flash：320B/18B，GLM-5 系列首个原生多模态成员。
			{ID: "glm-5.3-flash", Name: "GLM-5.3 Flash", Description: "原生多模态", ContextLen: 1000000, Vision: b(true)},
			{ID: "glm-5.2", Name: "GLM-5.2", Description: "1M 上下文", ContextLen: 1000000, Vision: b(false)},
		},
	},
	{
		Name: "gemini", DisplayName: "Google Gemini",
		Description:  "Google Gemini 系列模型",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "gemini-3.8-flash",
		Group:        "other",
		Models: []Model{
			{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Description: "最新主力模型", ContextLen: 1000000, Vision: b(true)},
			{ID: "gemini-3.7-flash", Name: "Gemini 3.7 Flash", Description: "高性价比", ContextLen: 1000000, Vision: b(true)},
			{ID: "gemini-3.6-flash", Name: "Gemini 3.6 Flash"},
			{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", Description: "最强推理", ContextLen: 1000000, Vision: b(true)},
			{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
			{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
		},
	},
	{
		Name: "groq", DisplayName: "Groq",
		Description:  "超高速 LLM 推理平台",
		BaseURL:      "https://api.groq.com/openai/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "meta-llama/llama-4-maverick-17b-128e-instruct",
		Group:        "aggregator",
		Models: []Model{
			{ID: "meta-llama/llama-4-maverick-17b-128e-instruct", Name: "Llama 4 Maverick", Description: "最新一代（默认），1M 上下文", ContextLen: 1000000},
			{ID: "llama-3.3-70b-versatile", Name: "Llama 3.3 70B", Description: "上代，保留兼容"},
			{ID: "openai/gpt-oss-120b", Name: "GPT-OSS 120B"},
			{ID: "openai/gpt-oss-20b", Name: "GPT-OSS 20B"},
			{ID: "llama-3.1-8b-instant", Name: "Llama 3.1 8B (最快)"},
		},
	},
	{
		Name: "together", DisplayName: "Together AI",
		Description:  "开源模型托管平台",
		BaseURL:      "https://api.together.xyz/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "deepseek-ai/DeepSeek-V4.1-Flash",
		Group:        "aggregator",
		Models: []Model{
			{ID: "deepseek-ai/DeepSeek-V4.1-Flash", Name: "DeepSeek V4.1 Flash", Description: "最新主力（默认）"},
			{ID: "deepseek-ai/DeepSeek-V4-Pro", Name: "DeepSeek V4 Pro", Description: "上代旗舰推理"},
			{ID: "meta-llama/Llama-4-Maverick-17B-128E-Instruct", Name: "Llama 4 Maverick"},
			{ID: "Qwen/Qwen3.8-2.4T-A95B", Name: "Qwen 3.8 Max 开源权重"},
			{ID: "moonshotai/Kimi-K3", Name: "Kimi K3"},
		},
	},
	{
		Name: "mistral", DisplayName: "Mistral AI",
		Description:  "Mistral 系列开源模型",
		BaseURL:      "https://api.mistral.ai/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "mistral-large-4",
		Group:        "other",
		Models: []Model{
			// 2026-10-06：Large 4 公开预览（1.05T/49B MoE，原生多模态，1M 上下文）；
			// 官方称权重 10 月底发布，故此处按 API 预览登记。
			{ID: "mistral-large-4", Name: "Mistral Large 4", Description: "预览版旗舰，1M 上下文", ContextLen: 1000000},
			// Mistral Large 确认为纯文本模型（官方文档，2026-09）；窗口按官方 256K
			// （旧值 128000 与 Large 3 的官方口径不符）。
			{ID: "mistral-large-latest", Name: "Mistral Large 3", Description: "GA 别名，始终指向最新 GA 旗舰（纯文本）", ContextLen: 256000, Vision: b(false)},
			{ID: "pixtral-large-latest", Name: "Pixtral Large", Description: "多模态", Vision: b(true)},
			{ID: "mistral-medium-3-5", Name: "Mistral Medium 3.5"},
			{ID: "mistral-small-2603", Name: "Mistral Small 4", Description: "快速", ContextLen: 256000},
			{ID: "magistral-medium-latest", Name: "Magistral Medium", Description: "推理"},
			{ID: "codestral-latest", Name: "Codestral", Description: "代码", ContextLen: 256000},
		},
	},
	{
		Name: "cohere", DisplayName: "Cohere",
		Description:  "Cohere Command 系列模型",
		BaseURL:      "https://api.cohere.ai/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "command-a-plus-05-2026",
		Group:        "other",
		Models: []Model{
			{ID: "command-a-plus-05-2026", Name: "Command A+", Description: "最新，MoE"},
			{ID: "command-a-reasoning-08-2025", Name: "Command A Reasoning"},
			{ID: "command-a-03-2025", Name: "Command A"},
			{ID: "command-r7b-12-2024", Name: "Command R7B"},
		},
	},
	{
		Name: "perplexity", DisplayName: "Perplexity",
		Description:  "Perplexity 在线搜索增强模型",
		BaseURL:      "https://api.perplexity.ai",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "sonar-pro",
		Group:        "aggregator",
		Models: []Model{
			{ID: "sonar-pro", Name: "Sonar Pro"},
			{ID: "sonar-reasoning-pro", Name: "Sonar Reasoning Pro"},
			{ID: "sonar-deep-research", Name: "Sonar Deep Research"},
			{ID: "sonar", Name: "Sonar", Description: "默认"},
		},
	},
	{
		Name: "huoshan", Aliases: []string{"doubao"}, DisplayName: "Volcengine (Doubao)",
		Description:  "ByteDance Doubao models via Volcengine Ark",
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "doubao-seed-2.1-pro",
		Note:         "豆包由火山引擎提供，doubao 为兼容别名，也支持火山方舟 endpoint ID (ep-xxx)",
		Group:        "china",
		Models: []Model{
			// 2026-09-16：2.1 Pro 更新至 0915 版，多模态 Coding（看图写代码）。
			{ID: "doubao-seed-2.1-pro", Name: "Doubao Seed 2.1 Pro", Description: "旗舰，多模态 Coding", Vision: b(true)},
			{ID: "doubao-seed-2.1-turbo", Name: "Doubao Seed 2.1 Turbo", Description: "均衡"},
			{ID: "doubao-seed-2.0-lite", Name: "Doubao Seed 2.0 Lite", Description: "全模态"},
			{ID: "doubao-seed-2.0-mini", Name: "Doubao Seed 2.0 Mini"},
			// Evolving 与 2.1 Pro 同步更新至 0915 版，无需换接入节点。
			{ID: "doubao-seed-evolving", Name: "Doubao Seed Evolving", Description: "始终指向最新 Agent 模型", Vision: b(true)},
		},
	},
	{
		Name: "wenxin", DisplayName: "文心一言 (Wenxin)",
		Description:  "百度 ERNIE 系列大模型",
		BaseURL:      "https://aip.baidubce.com/rpc/2.0/ai_custom/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true, // BaseURL 字段复用为 secretKey
		DefaultModel: "ernie-5.1",
		Group:        "china",
		Models: []Model{
			{ID: "ernie-5.1", Name: "ERNIE 5.1", Description: "最新文本旗舰，效价比"},
			// ERNIE 5.0：原生统一多模态（文本/图像/音频/视频）。
			{ID: "ernie-5.0", Name: "ERNIE 5.0", Description: "原生全模态", Vision: b(true)},
			{ID: "ernie-4.5-turbo-128k", Name: "ERNIE 4.5 Turbo 128K"},
			{ID: "ernie-x1.1-preview", Name: "ERNIE X1.1", Description: "深度推理"},
		},
	},
	{
		Name: "moonshot", Aliases: []string{"kimi"}, DisplayName: "Moonshot (Kimi)",
		Description:  "月之暗面 Kimi 大模型",
		BaseURL:      "https://api.moonshot.cn/v1",
		NeedsAPIKey:  true,
		DefaultModel: "kimi-k3",
		Group:        "china",
		Models: []Model{
			{ID: "kimi-k3", Name: "Kimi K3", Description: "旗舰，2.8T MoE，原生视觉", ContextLen: 1000000, Vision: b(true)},
			{ID: "kimi-k2.6", Name: "Kimi K2.6", Description: "多模态+思考", ContextLen: 262144, Vision: b(true)},
			{ID: "kimi-k2.7-code", Name: "Kimi K2.7 Code", Description: "编码", ContextLen: 262144, Vision: b(true)},
			{ID: "kimi-k2.7-code-highspeed", Name: "Kimi K2.7 Code Highspeed", Description: "编码（加速版）", ContextLen: 262144, Vision: b(true)},
		},
	},
	{
		Name: "mimo", DisplayName: "MiMo",
		Description:  "MiMo 大模型",
		BaseURL:      "https://api.xiaomimimo.com/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "mimo-v2.6-flash",
		Group:        "other",
		Models: []Model{
			// 2026-09-22：V2.6 Pro（1.02T/42B MoE），1M 上下文，原生全模态
			// （文本+图像+视频+音频），Agent 基座。
			{ID: "mimo-v2.6-pro", Name: "MiMo V2.6 Pro", Description: "最新旗舰，原生全模态", ContextLen: 1000000, Vision: b(true)},
			// 用户实测 mimo-v2.6-flash 接受图片输入（2026-06）。
			{ID: "mimo-v2.6-flash", Name: "MiMo V2.6 Flash", Description: "最新轻量模型", ContextLen: 1000000, Vision: b(true)},
			// V2.5 Pro：1T/42B MoE，1M 上下文，原生视觉+音频推理。
			{ID: "mimo-v2.5-pro", Name: "MiMo V2.5 Pro", Description: "旗舰推理", ContextLen: 1000000, Vision: b(true)},
			{ID: "mimo-v2-flash", Name: "MiMo V2 Flash", Description: "快速"},
			{ID: "mimo-v2-pro", Name: "MiMo V2 Pro", Description: "推理"},
			{ID: "mimo-v2-omni", Name: "MiMo V2 Omni", Description: "多模态"},
		},
	},
	{
		Name: "hunyuan", DisplayName: "混元 (Hunyuan)",
		Description:  "腾讯混元大模型",
		BaseURL:      "https://api.hunyuan.cloud.tencent.com/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "hy4-preview",
		Group:        "china",
		Models: []Model{
			// Hy4 preview（2026-08-28）：770B/49B 紧凑旗舰，1M 上下文。
			{ID: "hy4-preview", Name: "Hunyuan Hy4 Preview", Description: "最新旗舰，1M 上下文", ContextLen: 1000000},
			{ID: "hy3", Name: "Tencent Hy3", Description: "MoE Agent 模型"},
			{ID: "hy-2.0-think", Name: "HY 2.0 Think", Description: "深度推理"},
			{ID: "hy-2.0-instruct", Name: "HY 2.0 Instruct"},
			{ID: "hunyuan-turbos", Name: "Hunyuan TurboS", Description: "快速"},
		},
	},
	{
		Name: "longcat", DisplayName: "LongCat (美团龙猫)",
		Description:  "美团 LongCat 大模型",
		BaseURL:      "https://api.longcat.chat/openai/v1",
		NeedsAPIKey:  true,
		DefaultModel: "LongCat-2.5-Preview",
		Group:        "other",
		Models: []Model{
			// LongCat-2.5-Preview（2026-09-25）：1.6T/48B，1M 上下文，
			// 新增原生图片理解，兼容 OpenAI 与 Anthropic 协议。
			{ID: "LongCat-2.5-Preview", Name: "LongCat 2.5 Preview", Description: "最新，长程 Agent + 图片理解", ContextLen: 1000000, Vision: b(true)},
			// LongCat-2.0 是文本/代码模型：已确认不支持图片输入（美团，
			// 2026-06 发布）。多模态成员 LongCat-Flash-Omni 随 Flash 系列
			// 于 2026-05-29 停止服务。
			{ID: "LongCat-2.0-Preview", Name: "LongCat 2.0 Preview", Description: "上代旗舰推理与 Agent 模型", ContextLen: 1000000, Vision: b(false)},
		},
	},
	{
		Name: "meta", DisplayName: "Meta Model API (Muse Spark)",
		Description:  "Meta 超级智能实验室 Muse Spark 多模态推理模型",
		BaseURL:      "https://api.meta.ai/v1",
		NeedsAPIKey:  true,
		DefaultModel: "muse-spark-1.3",
		Group:        "other",
		Models: []Model{
			{ID: "muse-spark-1.3", Name: "Muse Spark 1.3", Description: "旗舰多模态推理模型", ContextLen: 1048576, Vision: b(true)},
			{ID: "muse-spark-1.2", Name: "Muse Spark 1.2", Description: "多模态推理模型", ContextLen: 1048576, Vision: b(true)},
			{ID: "muse-spark-1.1", Name: "Muse Spark 1.1", Description: "首发推理模型", ContextLen: 1048576, Vision: b(true)},
		},
	},
	{
		Name: "custom", DisplayName: "Custom (OpenAI Compatible)",
		Description:  "自定义 OpenAI 兼容 API",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "gpt-4o-mini",
		Group:        "custom",
		Models: []Model{
			{ID: "default", Name: "default", Description: "占位；实际模型由用户配置"},
		},
	},
}

// nameIndex 小写名/别名 → 目录下标，init 时构建。
var nameIndex = func() map[string]int {
	m := make(map[string]int, len(catalog)*2)
	for i, p := range catalog {
		m[p.Name] = i
		for _, a := range p.Aliases {
			m[a] = i
		}
	}
	return m
}()

// All 返回完整目录（只读，调用方不得修改）。
func All() []Provider { return catalog }

// Find 按名称或别名查找供应商（大小写不敏感）。别名 kimi/doubao 命中
// moonshot/huoshan 的目录条目。
func Find(name string) (Provider, bool) {
	if i, ok := nameIndex[lower(name)]; ok {
		return catalog[i], true
	}
	return Provider{}, false
}

// Models 返回供应商的模型条目列表；未知供应商返回 nil。
func Models(name string) []Model {
	if p, ok := Find(name); ok {
		return p.Models
	}
	return nil
}

// ModelIDs 返回供应商的模型 ID 列表；未知供应商返回 nil。
func ModelIDs(name string) []string {
	p, ok := Find(name)
	if !ok {
		return nil
	}
	ids := make([]string, len(p.Models))
	for i, m := range p.Models {
		ids[i] = m.ID
	}
	return ids
}

// DefaultModel 返回供应商的默认模型 ID；未知供应商返回 ""。
func DefaultModel(name string) string {
	if p, ok := Find(name); ok {
		return p.DefaultModel
	}
	return ""
}

// BaseURL 返回供应商的官方 API 端点；未知/custom 返回 ""。
func BaseURL(name string) string {
	if p, ok := Find(name); ok {
		return p.BaseURL
	}
	return ""
}

// DisplayName 返回供应商展示名；未知返回原名。
func DisplayName(name string) string {
	if p, ok := Find(name); ok {
		return p.DisplayName
	}
	return name
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
