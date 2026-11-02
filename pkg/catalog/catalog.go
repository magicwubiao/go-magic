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
//
// 文案语言约定：所有面向用户的字符串（Provider.DisplayName / Description /
// Note、Model.Name / Description）以**英文为主**——Web UI 与 CLI 的第一语言
// 是英文，中文由前端 locale 负责；不要在这里混入中文文案。
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
		Description:  "DeepSeek V4.1 — cost-effective, native multimodal",
		BaseURL:      "https://api.deepseek.com",
		NeedsAPIKey:  true,
		DefaultModel: "deepseek-flash",
		Group:        "recommended",
		Models: []Model{
			// 2026-09-10 发布：V4.1-Flash（552B MoE，官方模型名 deepseek-flash），
			// 原生多模态视觉理解；V4-Flash 已下线，旧名临时路由到 V4.1-Flash。
			// 2026-10-17 起官方在 deepseek-flash 名下滚动更新（V4.1-Flash-1017，
			// MIT 开源权重）：端到端完整项目能力增强，API 模型名不变。
			{ID: "deepseek-flash", Name: "DeepSeek V4.1 Flash", Description: "Latest workhorse (1017 update), native vision", ContextLen: 1000000, Vision: b(true)},
			// V4-Pro 官方宣布 9-14 后继续提供服务（应用户需求延长）。
			{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Description: "Previous-gen flagship reasoning", ContextLen: 1000000, Vision: b(false)},
			// 兼容别名：官方临时路由到 V4.1-Flash（多模态），故视觉判定为 true。
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Description: "Legacy name, routed to V4.1", ContextLen: 1000000, Vision: b(true)},
		},
	},
	{
		Name: "openai", DisplayName: "OpenAI",
		Description:  "GPT-6 / 6.1 series (Oct 2026)",
		BaseURL:      "https://api.openai.com/v1",
		NeedsAPIKey:  true,
		DefaultModel: "gpt-6.1-sol",
		Group:        "recommended",
		Models: []Model{
			// 2026-09-29：GPT-6 Sol 的升级版，官方称"近 Astra 能力、约五分之一价格"。
			{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Description: "Upgraded Sol, near-flagship capability", ContextLen: 1050000, Vision: b(true)},
			// 2026-10-26 发布：GPT-6.1 Terra 系列（$1/$5，1M 上下文），
			// 取代 GPT-5.6 世代中端档位，API 价格再降一半。
			{ID: "gpt-6.1-terra-pro", Name: "GPT-6.1 Terra Pro", Description: "Newest 6.1 tier, higher capability", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra", Description: "Newest 6.1 tier, balanced price", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6-astra", Name: "GPT-6 Astra", Description: "Most capable flagship", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6-sol", Name: "GPT-6 Sol", Description: "Previous-gen balanced (superseded by 6.1 Sol)", ContextLen: 1050000, Vision: b(true)},
			{ID: "gpt-6-luna", Name: "GPT-6 Luna", Description: "Fastest and cheapest", ContextLen: 1050000, Vision: b(true)},
		},
	},
	{
		Name: "anthropic", DisplayName: "Anthropic",
		Description:  "Claude 5.5 / Fable 5.1 — strong reasoning",
		BaseURL:      "https://api.anthropic.com",
		NeedsAPIKey:  true,
		DefaultModel: "claude-opus-5-5",
		Group:        "recommended",
		Models: []Model{
			// 2026-09-22 发布：长时间运行的智能体编码与知识工作。
			{ID: "claude-opus-5-5", Name: "Claude Opus 5.5", Description: "Latest flagship, adaptive thinking", ContextLen: 1000000, Vision: b(true)},
			{ID: "claude-fable-5-1", Name: "Claude Fable 5.1", Description: "Fable-class reasoning flagship", ContextLen: 1000000, Vision: b(true)},
			// 2026-09-28 发布：多数工作的主力档（免费版 claude.ai 也可用）。
			{ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5", Description: "Latest balanced tier", ContextLen: 1000000, Vision: b(true)},
			// 2026-10-07 发布：$0.10/$0.50，比 Haiku 4.5 便宜 75%，面向高并发
			// 摘要、子智能体与浏览器操作。
			{ID: "claude-haiku-5-5", Name: "Claude Haiku 5.5", Description: "Fastest and cheapest, built for subagents", ContextLen: 1000000, Vision: b(true)},
		},
	},
	{
		Name: "dashscope", DisplayName: "DashScope (Qwen)",
		Description:  "Alibaba Cloud Qwen model series",
		BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "qwen3.8-flash",
		Group:        "china",
		Models: []Model{
			// 2026-09：Qwen3.8 系列（2.4T MoE，原生视觉，1M 上下文）。
			{ID: "qwen3.8-max", Name: "Qwen 3.8 Max", Description: "Flagship (snapshot 0902)", ContextLen: 1000000, Vision: b(true)},
			{ID: "qwen3.8-flash", Name: "Qwen 3.8 Flash", Description: "Latest multimodal workhorse", ContextLen: 1000000, Vision: b(true)},
			{ID: "qwen3.8-omni-flash", Name: "Qwen 3.8 Omni Flash", Description: "Omni-modal (audio and video)", ContextLen: 1000000, Vision: b(true)},
			{ID: "qwen3.7-plus", Name: "Qwen 3.7 Plus"},
			{ID: "qwen3.7-flash", Name: "Qwen 3.7 Flash"},
		},
	},
	{
		Name: "minimax", DisplayName: "MiniMax",
		Description:  "MiniMax model series",
		BaseURL:      "https://api.minimax.chat/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "MiniMax-M3",
		Group:        "china",
		Models: []Model{
			// M3：428B/23B MoE，1M 上下文，原生多模态（图片+视频）。
			{ID: "MiniMax-M3", Name: "MiniMax M3", Description: "Flagship, native multimodal, 1M context", ContextLen: 1000000, Vision: b(true)},
			// 2026-10-19 发布：M3.1 Flash（$0.15/$0.60），Agentic/编程/
			// Computer Use 代际提升，推理提速近 3 倍，开源权重已上 HuggingFace。
			{ID: "MiniMax-M3.1-Flash", Name: "MiniMax M3.1 Flash", Description: "Latest low-cost agentic model", ContextLen: 1000000},
			{ID: "MiniMax-M2.7", Name: "MiniMax M2.7", Description: "Enhanced coding"},
			{ID: "MiniMax-M2.5", Name: "MiniMax M2.5", Description: "Advanced reasoning"},
		},
	},
	{
		Name: "ollama", DisplayName: "Ollama",
		Description:  "Run open-source models locally",
		BaseURL:      "http://localhost:11434",
		NeedsBaseURL: true,
		DefaultModel: "qwen3.8",
		Note:         "Available models depend on what you have pulled locally",
		Group:        "local",
		Models: []Model{
			{ID: "qwen3.8", Name: "Qwen 3.8", Description: "Alibaba open-source model", ContextLen: 131072, Vision: b(false)},
			{ID: "gpt-oss", Name: "GPT-OSS", Description: "OpenAI open-source model", ContextLen: 131072, Vision: b(false)},
			{ID: "deepseek-r1", Name: "DeepSeek R1", Description: "Reasoning model", ContextLen: 131072, Vision: b(false)},
			{ID: "gemma4", Name: "Gemma 4"},
			{ID: "phi4", Name: "Phi 4"},
		},
	},
	{
		Name: "openrouter", DisplayName: "OpenRouter",
		Description:  "Unified API gateway with many models",
		BaseURL:      "https://openrouter.ai/api/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "openai/gpt-6.1-sol",
		Note:         "Full model list: https://openrouter.ai/models",
		Group:        "aggregator",
		Models: []Model{
			{ID: "openai/gpt-6.1-sol", Name: "GPT-6.1 Sol", Description: "Upgraded Sol (same as the openai default)"},
			{ID: "openai/gpt-6.1-terra", Name: "GPT-6.1 Terra"},
			{ID: "openai/gpt-6-sol", Name: "GPT-6 Sol"},
			{ID: "openai/gpt-5.6", Name: "GPT-5.6 Sol"},
			{ID: "anthropic/claude-opus-5-5", Name: "Claude Opus 5.5"},
			{ID: "anthropic/claude-sonnet-5-5", Name: "Claude Sonnet 5.5"},
			{ID: "google/gemini-4-argon", Name: "Gemini 4 Argon"},
			{ID: "google/gemini-3.8-flash", Name: "Gemini 3.8 Flash"},
			{ID: "deepseek/deepseek-flash", Name: "DeepSeek V4.1 Flash"},
			{ID: "moonshotai/kimi-k3.1-pro", Name: "Kimi K3.1 Pro"},
		},
	},
	{
		Name: "vllm", DisplayName: "vLLM",
		Description:  "High-performance local inference engine",
		BaseURL:      "http://localhost:8000/v1",
		NeedsBaseURL: true,
		DefaultModel: "default",
		Note:         "Available models depend on your vLLM deployment",
		Group:        "local",
		Models: []Model{
			{ID: "default", Name: "default", Description: "Placeholder; the actual model depends on your deployment"},
		},
	},
	{
		Name: "zhipu", DisplayName: "Zhipu AI (GLM)",
		Description:  "Zhipu GLM model series",
		BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "glm-5.3",
		Group:        "china",
		Models: []Model{
			// GLM-5.3（2026-08）：753B/40B，1M 上下文，纯文本输入。
			{ID: "glm-5.3", Name: "GLM-5.3", Description: "Latest flagship, agentic coding", ContextLen: 1000000, Vision: b(false)},
			// GLM-5.3-Flash：320B/18B，GLM-5 系列首个原生多模态成员。
			{ID: "glm-5.3-flash", Name: "GLM-5.3 Flash", Description: "Native multimodal", ContextLen: 1000000, Vision: b(true)},
			{ID: "glm-5.2", Name: "GLM-5.2", Description: "1M context", ContextLen: 1000000, Vision: b(false)},
		},
	},
	{
		Name: "gemini", DisplayName: "Google Gemini",
		Description:  "Google Gemini model series",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "gemini-4-argon",
		Group:        "other",
		Models: []Model{
			// 2026-10-13 发布并 GA：Gemini 4 Argon，迄今最强 Gemini（1M 上下文，
			// 引导价 $2/$10 至 11-30，之后 $4/$20）。同月预告的 Gemini 4 Carbon
			// 首批面向网络防御者，未开放公网 API，暂不登记。
			{ID: "gemini-4-argon", Name: "Gemini 4 Argon", Description: "Latest flagship, most capable Gemini", ContextLen: 1000000, Vision: b(true)},
			{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Description: "Cost-effective workhorse", ContextLen: 1000000, Vision: b(true)},
			{ID: "gemini-3.7-flash", Name: "Gemini 3.7 Flash", Description: "Cost-effective", ContextLen: 1000000, Vision: b(true)},
			{ID: "gemini-3.6-flash", Name: "Gemini 3.6 Flash"},
		},
	},
	{
		Name: "groq", DisplayName: "Groq",
		Description:  "Ultra-fast LLM inference platform",
		BaseURL:      "https://api.groq.com/openai/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "meta-llama/llama-4-maverick-17b-128e-instruct",
		Group:        "aggregator",
		Models: []Model{
			{ID: "meta-llama/llama-4-maverick-17b-128e-instruct", Name: "Llama 4 Maverick", Description: "Latest generation (default), 1M context", ContextLen: 1000000},
			{ID: "llama-3.3-70b-versatile", Name: "Llama 3.3 70B", Description: "Previous generation, kept for compatibility"},
			{ID: "openai/gpt-oss-120b", Name: "GPT-OSS 120B"},
			{ID: "openai/gpt-oss-20b", Name: "GPT-OSS 20B"},
			{ID: "llama-3.1-8b-instant", Name: "Llama 3.1 8B (fastest)"},
		},
	},
	{
		Name: "together", DisplayName: "Together AI",
		Description:  "Open-model hosting platform",
		BaseURL:      "https://api.together.xyz/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "deepseek-ai/DeepSeek-V4.1-Flash",
		Group:        "aggregator",
		Models: []Model{
			{ID: "deepseek-ai/DeepSeek-V4.1-Flash", Name: "DeepSeek V4.1 Flash", Description: "Latest workhorse (default)"},
			{ID: "deepseek-ai/DeepSeek-V4-Pro", Name: "DeepSeek V4 Pro", Description: "Previous-gen flagship reasoning"},
			{ID: "meta-llama/Llama-4-Maverick-17B-128E-Instruct", Name: "Llama 4 Maverick"},
			{ID: "Qwen/Qwen3.8-2.4T-A95B", Name: "Qwen 3.8 Max (open weights)"},
			{ID: "moonshotai/Kimi-K3", Name: "Kimi K3"},
		},
	},
	{
		Name: "mistral", DisplayName: "Mistral AI",
		Description:  "Mistral open-model series",
		BaseURL:      "https://api.mistral.ai/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "mistral-large-4",
		Group:        "other",
		Models: []Model{
			// 2026-10-06：Large 4 公开预览（1.05T/49B MoE，原生多模态，1M 上下文）；
			// 权重已于 2026-10-29 以 Mistral Ecosystem Open-Weight License 发布。
			{ID: "mistral-large-4", Name: "Mistral Large 4", Description: "Flagship, 1M context", ContextLen: 1000000},
			// Mistral Large 确认为纯文本模型（官方文档，2026-09）；窗口按官方 256K
			// （旧值 128000 与 Large 3 的官方口径不符）。
			{ID: "mistral-large-latest", Name: "Mistral Large 3", Description: "GA alias, always points to the latest GA flagship (text only)", ContextLen: 256000, Vision: b(false)},
			{ID: "pixtral-large-latest", Name: "Pixtral Large", Description: "Multimodal", Vision: b(true)},
			{ID: "mistral-medium-3-5", Name: "Mistral Medium 3.5"},
			{ID: "mistral-small-2603", Name: "Mistral Small 4", Description: "Fast", ContextLen: 256000},
			{ID: "magistral-medium-latest", Name: "Magistral Medium", Description: "Reasoning"},
			{ID: "codestral-latest", Name: "Codestral", Description: "Code", ContextLen: 256000},
		},
	},
	{
		Name: "cohere", DisplayName: "Cohere",
		Description:  "Cohere Command model series",
		BaseURL:      "https://api.cohere.ai/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "command-a-plus-05-2026",
		Group:        "other",
		Models: []Model{
			{ID: "command-a-plus-05-2026", Name: "Command A+", Description: "Latest, MoE"},
			{ID: "command-a-reasoning-08-2025", Name: "Command A Reasoning"},
			{ID: "command-a-03-2025", Name: "Command A"},
			{ID: "command-r7b-12-2024", Name: "Command R7B"},
		},
	},
	{
		Name: "perplexity", DisplayName: "Perplexity",
		Description:  "Perplexity search-augmented models",
		BaseURL:      "https://api.perplexity.ai",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "sonar-pro",
		Group:        "aggregator",
		Models: []Model{
			{ID: "sonar-pro", Name: "Sonar Pro"},
			{ID: "sonar-reasoning-pro", Name: "Sonar Reasoning Pro"},
			{ID: "sonar-deep-research", Name: "Sonar Deep Research"},
			{ID: "sonar", Name: "Sonar", Description: "Default"},
		},
	},
	{
		Name: "huoshan", Aliases: []string{"doubao"}, DisplayName: "Volcengine (Doubao)",
		Description:  "ByteDance Doubao models via Volcengine Ark",
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "doubao-seed-2.1-pro",
		Note:         "Doubao is served by Volcengine Ark; `doubao` is a compatibility alias. Ark endpoint IDs (ep-xxx) are also supported.",
		Group:        "china",
		Models: []Model{
			// 2026-09-16：2.1 Pro 更新至 0915 版，多模态 Coding（看图写代码）。
			{ID: "doubao-seed-2.1-pro", Name: "Doubao Seed 2.1 Pro", Description: "Flagship, multimodal coding", Vision: b(true)},
			{ID: "doubao-seed-2.1-turbo", Name: "Doubao Seed 2.1 Turbo", Description: "Balanced"},
			{ID: "doubao-seed-2.0-lite", Name: "Doubao Seed 2.0 Lite", Description: "Omni-modal"},
			{ID: "doubao-seed-2.0-mini", Name: "Doubao Seed 2.0 Mini"},
			// Evolving 与 2.1 Pro 同步更新至 0915 版，无需换接入节点。
			{ID: "doubao-seed-evolving", Name: "Doubao Seed Evolving", Description: "Always points to the newest agent model", Vision: b(true)},
		},
	},
	{
		Name: "wenxin", DisplayName: "Wenxin (ERNIE)",
		Description:  "Baidu ERNIE model series",
		BaseURL:      "https://aip.baidubce.com/rpc/2.0/ai_custom/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true, // BaseURL 字段复用为 secretKey
		DefaultModel: "ernie-5.1",
		Group:        "china",
		Models: []Model{
			{ID: "ernie-5.1", Name: "ERNIE 5.1", Description: "Latest text flagship, cost-effective"},
			// ERNIE 5.0：原生统一多模态（文本/图像/音频/视频）。
			{ID: "ernie-5.0", Name: "ERNIE 5.0", Description: "Native omni-modal", Vision: b(true)},
		},
	},
	{
		Name: "moonshot", Aliases: []string{"kimi"}, DisplayName: "Moonshot (Kimi)",
		Description:  "Moonshot Kimi model series",
		BaseURL:      "https://api.moonshot.cn/v1",
		NeedsAPIKey:  true,
		DefaultModel: "kimi-k3.1-pro",
		Group:        "china",
		Models: []Model{
			// 2026-10-23 发布：K3.1 Pro（2.8T MoE / 104B 活跃，1M 上下文），
			// 代码 / 3D / 金融研究 / 长程 Agent 显著提升，完整权重官方承诺
			// 11-04 前开源。Highspeed 为 2-3 倍出速的服务档（单价约 2 倍）。
			{ID: "kimi-k3.1-pro", Name: "Kimi K3.1 Pro", Description: "Latest flagship, agent and coding", ContextLen: 1000000, Vision: b(true)},
			{ID: "kimi-k3.1-pro-highspeed", Name: "Kimi K3.1 Pro Highspeed", Description: "Latest flagship, 2-3x faster serving tier", ContextLen: 1000000, Vision: b(true)},
			{ID: "kimi-k3", Name: "Kimi K3", Description: "Previous flagship, 2.8T MoE, native vision", ContextLen: 1000000, Vision: b(true)},
		},
	},
	{
		Name: "mimo", DisplayName: "MiMo",
		Description:  "MiMo model series",
		BaseURL:      "https://api.xiaomimimo.com/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "mimo-v2.6-flash",
		Group:        "other",
		Models: []Model{
			// 2026-09-22：V2.6 Pro（1.02T/42B MoE），1M 上下文，原生全模态
			// （文本+图像+视频+音频），Agent 基座。
			{ID: "mimo-v2.6-pro", Name: "MiMo V2.6 Pro", Description: "Latest flagship, native omni-modal", ContextLen: 1000000, Vision: b(true)},
			// 用户实测 mimo-v2.6-flash 接受图片输入（2026-06）。
			{ID: "mimo-v2.6-flash", Name: "MiMo V2.6 Flash", Description: "Latest lightweight model", ContextLen: 1000000, Vision: b(true)},
		},
	},
	{
		Name: "hunyuan", DisplayName: "Hunyuan (Tencent)",
		Description:  "Tencent Hunyuan model series",
		BaseURL:      "https://api.hunyuan.cloud.tencent.com/v1",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "hy4-preview",
		Group:        "china",
		Models: []Model{
			// Hy4 preview（2026-08-28）：770B/49B 紧凑旗舰，1M 上下文。
			{ID: "hy4-preview", Name: "Hunyuan Hy4 Preview", Description: "Latest flagship, 1M context", ContextLen: 1000000},
			{ID: "hy3", Name: "Tencent Hy3", Description: "MoE agent model"},
		},
	},
	{
		Name: "longcat", DisplayName: "LongCat (Meituan)",
		Description:  "Meituan LongCat model series",
		BaseURL:      "https://api.longcat.chat/openai/v1",
		NeedsAPIKey:  true,
		DefaultModel: "LongCat-2.5-Preview",
		Group:        "other",
		Models: []Model{
			// LongCat-2.5-Preview（2026-09-25）：1.6T/48B，1M 上下文，
			// 新增原生图片理解，兼容 OpenAI 与 Anthropic 协议。
			{ID: "LongCat-2.5-Preview", Name: "LongCat 2.5 Preview", Description: "Latest, long-horizon agent + image understanding", ContextLen: 1000000, Vision: b(true)},
			// LongCat-2.0 是文本/代码模型：已确认不支持图片输入（美团，
			// 2026-06 发布）。多模态成员 LongCat-Flash-Omni 随 Flash 系列
			// 于 2026-05-29 停止服务。
			{ID: "LongCat-2.0-Preview", Name: "LongCat 2.0 Preview", Description: "Previous-gen flagship reasoning and agent model", ContextLen: 1000000, Vision: b(false)},
		},
	},
	{
		Name: "meta", DisplayName: "Meta Model API (Muse Spark)",
		Description:  "Muse Spark multimodal reasoning models from Meta Superintelligence Labs",
		BaseURL:      "https://api.meta.ai/v1",
		NeedsAPIKey:  true,
		DefaultModel: "muse-wonder",
		Group:        "other",
		Models: []Model{
			// 2026-10-12 预览：Muse Wonder——Meta 最强的长程智能体/编码托管
			// 模型（Meta Model API + Muse Code）。更高配的 Muse Wonder Full
			// 仅限 Muse Code 订阅内使用，不进 API 目录。
			{ID: "muse-wonder", Name: "Muse Wonder", Description: "Preview flagship for long-running agents and coding", ContextLen: 1048576},
			// 2026-10-15 开源：Muse Spark Open（816B/42B MoE，Muse Model License）。
			{ID: "muse-spark-open", Name: "Muse Spark Open", Description: "Open-weights 816B MoE", ContextLen: 1048576, Vision: b(true)},
			{ID: "muse-spark-1.3", Name: "Muse Spark 1.3", Description: "Flagship multimodal reasoning model", ContextLen: 1048576, Vision: b(true)},
			{ID: "muse-spark-1.2", Name: "Muse Spark 1.2", Description: "Multimodal reasoning model", ContextLen: 1048576, Vision: b(true)},
			{ID: "muse-spark-1.1", Name: "Muse Spark 1.1", Description: "Debut reasoning model", ContextLen: 1048576, Vision: b(true)},
		},
	},
	{
		Name: "custom", DisplayName: "Custom (OpenAI Compatible)",
		Description:  "Custom OpenAI-compatible API endpoint",
		NeedsAPIKey:  true,
		NeedsBaseURL: true,
		DefaultModel: "gpt-4o-mini",
		Group:        "custom",
		Models: []Model{
			{ID: "default", Name: "default", Description: "Placeholder; the real model is configured by the user"},
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
