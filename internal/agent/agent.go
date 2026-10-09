package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
	"github.com/magicwubiao/go-magic/internal/budget"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/complexity"
	"github.com/magicwubiao/go-magic/internal/compress"
	ctxrules "github.com/magicwubiao/go-magic/internal/context"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/redact"
	"github.com/magicwubiao/go-magic/internal/retry"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
	"github.com/magicwubiao/go-magic/pkg/utils"
)

// Tool dependency graph - tools that must run alone (cannot parallelize with same tool or related)
var (
	// Tools that should not run in parallel (to avoid conflicts)
	exclusiveTools = map[string]bool{
		"write_file":      true,
		"execute_command": true,
	}

	// Tools that need sequential execution
	sequentialTools = map[string]bool{
		"read_file":       true,
		"write_file":      true,
		"execute_command": true,
	}
)

// toolGroup defines a group of tools for execution
type toolGroup struct {
	tools      []types.ToolCall
	sequential bool
}

// formatDuration returns a human-readable concise duration string
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ms := d.Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	s := d.Seconds()
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	m := int(d.Minutes())
	s = d.Seconds() - float64(m*60)
	return fmt.Sprintf("%dm%ds", m, int(s))
}

// ToolCallResult holds the result of a tool execution
type ToolCallResult struct {
	ID        string
	Name      string
	Content   string
	Err       error
	Execution time.Duration
}

// Agent handles AI conversation with tool execution
type Agent struct {
	// Mutex for concurrent access protection
	mu sync.RWMutex

	provider    provider.Provider
	registry    ToolRegistry
	tools       []map[string]interface{} // tools schema for provider
	history     []provider.Message
	maxTurns    int
	maxTotalLen int // max BYTES in message history (messageWeight sums len(), not runes)
	maxMsgLen   int // max chars per message
	// skillsCtx 记录当前注入系统提示的技能清单块，供 SetSkillsContext
	// 以替换（而非追加）语义原地刷新，避免重复堆叠。
	skillsCtx string
	// Steering settings
	maxIterations  int
	maxTokenBudget int64

	// Tool call loop detection (protected by mu)
	//
	// toolCallHistory 只记录**本回合**发出的工具调用签名（工具名 + 参数指纹，
	// 见 toolCallSignature），由 resetToolLoopCounters 在每个回合进入循环前清零。
	// 它必须按回合清零：这道检测的语义是"模型在这一轮里卡住了"，跨回合累积会把
	// 后续所有正常轮次误判成死循环 —— 表现是模型只说要做什么、永远不动手
	// （每轮的工具调用都被丢弃，换上一句"不要再调工具，给个总结"）。
	toolCallHistory  []toolLoopRecord
	sameToolLimit    int // 同一（工具 + 参数）最大重复次数
	consecutiveLimit int // 连续无进展（重复签名）次数上限，非调用总量
	// repeatedResourceLimit 是"在没有任何修改的情况下反复访问同一目标资源"的次数上限。
	//
	// 前两条判据计的都是**重复签名**，对"间隔性重复"完全免疫：一个回合里
	// 读A→读B→读C→读A→读B→读C，每一步签名都不同，两条判据都不触发，模型可以
	// 这样空转很久。2026-10-08 线上事故就是这种形态（web 聊天、任务"改顶部导航"）：
	// 模型在 20:58 改完文件后，又在 18 分钟里读了同一批文件 80 余次、一个字没写，
	// 直到 30 分钟回合超时才被砍掉——三道循环闸门一个都没响。
	//
	// 因此这里换一个问法："有没有修改？没有修改的重读能带来新信息吗？"以**最后一次
	// 修改性调用**为分界，只统计其后的资源访问次数。改后验证（读 1~2 次）、分段读
	// 大文件（offset 进了资源键）都不会触发。
	repeatedResourceLimit int

	// maxParallelTools 全局并行工具执行并发上限（跨所有并行组共享）。
	// 默认 4；<=0 视为非法并在运行时兜底为串行。
	maxParallelTools int

	// Memory integration
	memoryEnabled bool

	// 目录级记忆 scope（= 会话工作目录的归一化键）：非空时每轮召回与回合末
	// 沉淀都限定在该目录记忆桶内，实现「同一目录的会话共享记忆、异目录隔离」。
	// 为空时保持旧的全局工作区默认行为。
	memoryScope string

	// 动态记忆注入（P0-1）：每轮新用户输入进入时经 cortex 门面召回一次，
	// turn 内保持稳定；出站消息在头部 system 之后插入。
	// dynamicMemoryKey 用于防止同一次输入重复召回。
	dynamicMemory    string
	dynamicMemoryKey string

	// 静态规则链：ruleDir 非空（= 会话工作目录）时，出站消息在头部 system
	// 之后、动态记忆之前插入从工作目录向上逐级发现的规则文件
	// （AGENTS.md / CLAUDE.md / CONTEXT.md）原文。ruleSig 为规则链的
	// stat 签名——文件新增/修改/删除都会改变签名，从而在下一轮自动重载，
	// 无需重启会话。与目录记忆 scope（memoryScope）互补：动态记忆沉淀
	// 「事实」，规则文件承载「共识/规范」。
	ruleDir     string
	ruleSig     string
	ruleContext string

	// Cortex Agent six-system integration
	cortexManager *cortex.Manager

	// Sub-task delegation (auto-decompose complex tasks)
	subTaskEnabled bool

	// Hooks system
	hooks *hooks.HookManager

	// Approval hook reference (set during registerBuiltinHooks)
	approvalHook *ApprovalHook

	// Event bus for observability
	bus *bus.EventBus

	// Session tracking
	session        string
	iterationCount int

	// Compression settings. 摘要压缩只有一条路径：compressor（由
	// WithCompression 装配阈值，unconditionally 由 maybeCompressContext 驱动）。
	// 曾经的 compressionEnabled / compressionRatio 是 TUI /compress 专用开关，
	// 已在 2026-10-09 删除 —— 见 truncateHistory 里 maybeCompressBeforeTruncate
	// 的注释，那两个字段的"恒真"判定让截断路径退化成了另一套规则式压缩。
	maxMemoryTokens int

	// Token usage tracking
	tokenUsage      int64
	inputTokens     int
	outputTokens    int
	cacheReadTokens int

	// Deadline-aware graceful finish (Hermes-style):
	// 记录每轮实际耗时用于估算下一轮开销；ctx 剩余时间不足一个完整轮次时，
	// 优雅收尾并把进度 checkpoint 落盘到 ~/.magic/checkpoints/。
	turnDurations []time.Duration // 已完成轮次的耗时样本（截断保留最近 32 个）
	turnStartTime time.Time       // 当前轮次开始时刻（零值表示未开始）

	// Secret redaction (default true)
	secretRedaction bool

	// Privacy PII redaction config (nil = use default)
	privacyCfg *privacy.Config

	// Iteration budget for long-running tasks (Hermes-inspired)
	budget *budget.Budget

	// Context compressor for long conversations
	compressor *compress.Compressor

	// Error classifier for intelligent retry
	errorClassifier *retry.Classifier

	// RepeatedFailureDetector escalates when the same equivalent failure
	// recurs N times within a window, closing the Hermes Agent Issue #22112
	// gap where the loop retried silently until the turn cap terminated it.
	failureDetector *retry.RepeatedFailureDetector

	// Self-reflection mechanism (Hermes-inspired)
	reflector      *Reflector
	reflectionCfg  ReflectionConfig
	reflectEnabled bool

	// Trajectory-based learning
	trajInjector *cortex.TrajectoryInjector
	trajEnabled  bool
	trajCfg      cortex.TrajectoryInjectorConfig
	trajStore    *cortex.TrajectoryStore

	// 引导（guide）收件箱：回合进行中用户追加的补充指示。零值即可用；
	// push 由 HTTP handler goroutine 调用，排水只发生在回合 goroutine
	// （迭代顶部）与 server 回合收尾（残留回收）——契约见 guide.go。
	guideInbox guideInbox

	// Smart error recovery for tool execution
	smartRecovery *retry.SmartRecovery
}

// ToolRegistry interface for tool execution
type ToolRegistry interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error)
}

// AgentOption configures the agent
type AgentOption func(*Agent)

// SteeringConfig holds steering configuration
type SteeringConfig struct {
	MaxIterations  int
	MaxTokenBudget int64
}

// defaultCompressThresholdTokens 是上下文压缩的默认 token 阈值（历史字符数 / 4）。
//
// 旧值 8000（≈3.2 万字符）是 8K 上下文时代的遗留：读一个稍大的文件就会越过它，
// 触发压缩把刚读到的内容摘要掉；模型于是重读、再压缩 —— 形成"读完就忘"的正反馈
// 死循环（2026-10-08 事故：一个改顶部导航的简单任务空转到 30 分钟回合墙，283 次
// 调用里 86% 是只读、写入仅 1 次）。
//
// 当前主力模型（deepseek-v4.1 / glm-5.3 等）上下文窗口为 128K 级，32000 tokens
// （≈12.8 万字符）既能让典型工作集留在上下文里，又为输出与系统提示留足余量。
// 可通过 config agent.compress_threshold_tokens 或 WithCompression 覆盖。
const defaultCompressThresholdTokens = 32000

// defaultCompressProtectLastN 是压缩时尾部保护的基准条数（Compress 内还会按
// 历史长度自适应放大到至少 len/4，见 compress.Compressor.Compress）。
const defaultCompressProtectLastN = 8

// NewAIAgent creates a new AI agent
func NewAIAgent(prov provider.Provider, registry ToolRegistry, tools []map[string]interface{}, systemPrompt string) *Agent {
	history := make([]provider.Message, 0)
	if systemPrompt != "" {
		history = append(history, provider.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	agent := &Agent{
		provider: prov,
		registry: registry,
		tools:    tools,
		history:  history,
		// 内置兜底上限：调用方（config / WithMaxTurns）未指定时生效。与
		// config 默认值保持一致，避免"配了 0 反而比默认值更紧"的困惑。
		//
		// 150/200 而非更大的值：单回合受 chatqueue.sessionTurnTimeout
		// （30 分钟）约束，每轮迭代是一次 LLM 调用加工具执行（实测
		// 10~30s），物理可达的迭代数约 60~180。300 落在这个区间之外，
		// 永远先撞时间墙 → 回合上限形同虚设。见 pkg/config/config.go 同项注释。
		maxTurns:      150,
		maxIterations: 200,
		// 单位是**字节**（messageWeight 用 len()），注释里的 "~50K tokens" 按
		// ASCII 4 字节/token 折算（中文 3 字节/字、≈1 token/字，实际更高）。
		// 生效的硬上限由 historyHardLimit() 取"本值"与"压缩触发点 ×5/4"的较大者。
		maxTotalLen:    200000, // 200K bytes max history (~50K tokens ASCII)
		maxMsgLen:      50000,  // 50K bytes per message (~12K tokens ASCII)
		maxTokenBudget: 0,
		// sameToolLimit 比的是"工具名 + 参数指纹"：同一回合里 read_file 读 3 个
		// 不同文件是完全正常的，只有反复发起**同一个调用**才是死循环信号。
		sameToolLimit: 3,
		// consecutiveLimit 是"连续无进展"兜底：从尾部往前连续这么多条调用
		// 全都是**已经出现过的 工具+参数 签名**时，才认定原地打转并收口。
		// 注意它**不是**调用总量上限——旧实现按总数判定，把"二十来步的正常
		// 重构"直接腰斩成"几十轮就停止"。调用多但每步都不同（有进展）永不触发；
		// 真正的总量/时长上限由 maxTurns 与回合超时兜底。
		consecutiveLimit: 25,
		// repeatedResourceLimit 是第三道、也是唯一按**累计**计的兜底：自最后一次
		// 修改性调用以来，同一目标资源（见 toolCallResourceKey）被访问达到这个
		// 次数，就认定在空转。
		//
		// 它补的正是前两条的盲区——"读A→读B→读C→读A→读B→读C"这种间隔性重复，
		// 每一步签名都不同，连续判据永远不会响；而 consecutiveLimit 只要中间插入
		// 一个新签名就清零，同样不响。2026-10-08 线上事故就是这种形态：一个"改顶部
		// 导航"的简单任务空转到 30 分钟回合超时才收场（283 次调用、86% 只读、
		// 0 次写入），三道循环闸门一个都没触发。
		//
		// 取 5 而不是更小：改完之后"为了确认再读一两次"是正常行为，读到第 5 次
		// 且期间一次都没改，才只能是空转。
		repeatedResourceLimit: 5,
		maxParallelTools:      defaultMaxParallelTools,
		subTaskEnabled:        true,
		hooks:                 hooks.NewHookManager(),
		bus:                   bus.NewEventBus(),
		budget:                budget.Preset("parent"),
		compressor:            compress.NewCompressor(defaultCompressThresholdTokens),
		errorClassifier:       retry.NewClassifier(),
		failureDetector:       retry.NewRepeatedFailureDetector(retry.DefaultRepeatedFailureConfig()),
		smartRecovery:         retry.NewSmartRecovery(retry.DefaultSmartRecoveryConfig()),
	}

	agent.registerBuiltinHooks()

	// history 摘要压缩增强：为 compressor 注入 LLM summarizer。
	// 压缩超阈值时中段消息交由主 provider 生成语义摘要；
	// 失败/超时(20s硬上限)由 compressor 内部兜底为规则式摘要。
	if agent.compressor != nil {
		agent.compressor.ProtectLastN = defaultCompressProtectLastN
		agent.compressor.SetSummarizer(newProviderSummarizer(agent))
	}

	return agent
}

// newProviderSummarizer 返回基于 agent.provider 的中段历史摘要函数。
// 输出必须保持事实密度：任务目标、已完成步骤、关键决策、文件路径、
// 报错原文要点、未完成事项与下一步。空返回或错误都会触发规则式兜底。
func newProviderSummarizer(a *Agent) func(ctx context.Context, middle []compress.Message) (string, error) {
	return func(ctx context.Context, middle []compress.Message) (string, error) {
		a.mu.RLock()
		maxMsgLen := a.maxMsgLen
		a.mu.RUnlock()

		var b strings.Builder
		for _, m := range middle {
			content := m.Content
			// 普通消息按 maxMsgLen/8（上限 2000 字符）截断即可；但**工具结果**
			// 要给更大配额 —— 它是模型观察外部世界的唯一渠道，只喂 2000 字符
			// 会让摘要丢掉文件的关键内容，模型压缩后只能重读同一批文件
			// （"读完就忘"），这正是 2026-10-08 空转事故的诱因。
			limit := maxMsgLen / 8
			if limit < 500 {
				limit = 500
			}
			if limit > 2000 {
				limit = 2000
			}
			if m.Role == "tool" {
				limit = 6000
			}
			b.WriteString(m.Role)
			if m.Role == "tool" && m.Name != "" {
				b.WriteString("(")
				b.WriteString(m.Name)
				b.WriteString(")")
			}
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				names := make([]string, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					names = append(names, tc.Name)
				}
				b.WriteString("[called: ")
				b.WriteString(strings.Join(names, ", "))
				b.WriteString("]")
			}
			b.WriteString(": ")
			b.WriteString(utils.TruncateDetailed(content, limit))
			b.WriteString("\n")
		}

		req := []provider.Message{
			{
				Role: "system",
				Content: "You are a conversation-compression assistant. Compress the given conversation " +
					"history into a high-information-density hand-off summary that a later context window can " +
					"use to continue the task. You MUST preserve: (1) the current task goal and the user's " +
					"original request; (2) the key steps already completed and their results; (3) important " +
					"decisions and the reasons behind them; (4) the concrete file paths/commands/data " +
					"involved; (5) the core content of any error messages; (6) unfinished work and the " +
					"suggested next steps; (7) **every file or resource already read/inspected: its path plus " +
					"the key facts and conclusions drawn from it** (especially the parts directly relevant to " +
					"the current task, such as the current state of the code/HTML/config that was modified) " +
					"-- a later window must not re-read these files merely because it forgot about them, so " +
					"write those facts out explicitly instead of just saying \"read some file\". " +
					"Write the summary in the same language the conversation is in. " +
					"Output the summary body directly: no pleasantries, no Markdown headings.",
			},
			{
				Role:      "user",
				Content:   "Here is the conversation history to compress:\n\n" + b.String(),
				Timestamp: time.Now(),
			},
		}
		resp, err := a.provider.Chat(ctx, req)
		if err != nil {
			return "", err
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return "", fmt.Errorf("summarizer returned empty content")
		}
		return resp.Content, nil
	}
}

// NewEnhancedAgent creates an agent with enhanced features
func NewEnhancedAgent(prov provider.Provider, registry ToolRegistry, tools []map[string]interface{}, systemPrompt string, opts ...AgentOption) *Agent {
	agent := NewAIAgent(prov, registry, tools, systemPrompt)

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

// WithSteering configures steering settings
func WithSteering(cfg SteeringConfig) AgentOption {
	return func(a *Agent) {
		if cfg.MaxIterations > 0 {
			a.maxIterations = cfg.MaxIterations
		}
		if cfg.MaxTokenBudget > 0 {
			a.maxTokenBudget = cfg.MaxTokenBudget
		}
	}
}

// WithConvertConfig sets the file conversion configuration.
//
// The type switch that used to live here moved to provider.ApplyConvertConfig
// so the server can refresh the SAME value on the live provider when provider
// settings change (vision toggle) — see Server.refreshConvertConfig. The
// config lives on the provider instance (BaseProvider.ConvertCfg), which every
// cached agent shares, so writing it once is enough for all sessions.
func WithConvertConfig(cfg *provider.ConvertConfig) AgentOption {
	return func(a *Agent) {
		provider.ApplyConvertConfig(a.provider, cfg)
	}
}

// WithLoopLimits configures loop detection limits
// WithLoopLimits configures loop detection limits
func WithLoopLimits(sameToolLimit, consecutiveLimit int) AgentOption {
	return func(a *Agent) {
		if sameToolLimit > 0 {
			a.sameToolLimit = sameToolLimit
		}
		if consecutiveLimit > 0 {
			a.consecutiveLimit = consecutiveLimit
		}
	}
}

// WithMaxTurns overrides the per-turn tool-loop cap (built-in default 300,
// overridable via config agent.max_turns which also defaults to 300).
// Values <= 0 are ignored so callers can pass config straight through.
func WithMaxTurns(n int) AgentOption {
	return func(a *Agent) {
		if n > 0 {
			a.maxTurns = n
		}
	}
}

// WithCompression 调整上下文压缩策略。两项都直接决定"模型会不会因为上下文被
// 摘要掉而反复重读同一批文件"：
//
//   - thresholdTokens：触发压缩的 token 阈值（历史字符数/4）。越高越不容易触发，
//     默认 defaultCompressThresholdTokens(32000)。旧的硬编码 8000 是 8K 上下文
//     时代的遗留，读一个文件就会触发。
//   - protectLastN：压缩时尾部原样保留的消息条数，默认 defaultCompressProtectLastN(8)；
//     Compress 内部还会按历史长度自适应放大到至少 len/4。
//
// 任一项 <= 0 表示保留内置默认，方便调用方把 config 原样透传。
func WithCompression(thresholdTokens, protectLastN int) AgentOption {
	return func(a *Agent) {
		if a.compressor == nil {
			return
		}
		if thresholdTokens > 0 {
			a.compressor.ThresholdTokens = thresholdTokens
		}
		if protectLastN > 0 {
			a.compressor.ProtectLastN = protectLastN
		}
	}
}

// ApplyOption applies one option to an already-constructed agent.
func (a *Agent) ApplyOption(opt AgentOption) {
	if opt != nil {
		opt(a)
	}
}

// WithRepeatedFailureDetector installs a custom repeated-failure detector. Pass
// nil to disable escalation. When enabled, the agent halts with a structured
// diagnostic once the same equivalent failure recurs Threshold times within
// Window, instead of silently retrying until the turn cap (Hermes #22112).
func WithRepeatedFailureDetector(d *retry.RepeatedFailureDetector) AgentOption {
	return func(a *Agent) {
		a.failureDetector = d
	}
}

// WithHooks registers hooks
func WithHooks(hookRegs ...hooks.HookRegistration) AgentOption {
	return func(a *Agent) {
		for _, reg := range hookRegs {
			a.hooks.Register(reg)
		}
	}
}

// WithEventBus sets a custom event bus
func WithEventBus(eventBus *bus.EventBus) AgentOption {
	return func(a *Agent) {
		a.bus = eventBus
	}
}

// WithMemory enables memory integration
func WithMemory(enabled bool) AgentOption {
	return func(a *Agent) {
		a.memoryEnabled = enabled
	}
}

// WithMemoryScope 为 agent 绑定目录级记忆 scope（= 会话工作目录的归一化键）。
// 绑定后每轮动态召回与回合末记忆沉淀都只读写该 scope 的记忆桶：
// 同一工作目录的多个会话共享一份目录记忆，不同目录互不串扰。
// scope 为空时保持旧的全局工作区默认行为。
func WithMemoryScope(scope string) AgentOption {
	return func(a *Agent) {
		a.memoryScope = scope
	}
}

// WithRuleDir 绑定静态规则链的工作目录（= 会话 work_dir）。绑定后出站消息
// 自动注入从该目录向上发现的 AGENTS.md/CLAUDE.md/CONTEXT.md（就近优先），
// 文件变更通过 stat 签名自动感知。dir 为空表示不启用规则注入。
func WithRuleDir(dir string) AgentOption {
	return func(a *Agent) {
		a.SetRuleDir(dir)
	}
}

// WithCortex enables Cortex Agent six-system integration
func WithCortex(mgr *cortex.Manager) AgentOption {
	return func(a *Agent) {
		a.cortexManager = mgr
	}
}

// WithApprovalManager sets an external approval manager for the agent.
func WithApprovalManager(mgr *approval.Manager) AgentOption {
	return func(a *Agent) {
		if mgr != nil {
			ah := NewApprovalHookWithManager(mgr)
			a.approvalHook = ah
			// 先移除 registerBuiltinHooks 注册的默认 approval hook，
			// 避免同一工具调用触发两次审批（默认 manager 与外部 manager 各一次）。
			a.hooks.Unregister("approval")
			// Re-register the approval hook with the new manager
			a.hooks.Register(hooks.HookRegistration{
				Name:   "approval",
				Source: hooks.HookSourceBuiltIn,
				Hook:   ah,
			})
		}
	}
}

// WithSubTask enables or disables automatic sub-task delegation
func WithSubTask(enabled bool) AgentOption {
	return func(a *Agent) {
		a.subTaskEnabled = enabled
	}
}

// WithSecretRedaction enables or disables secret redaction (API keys, tokens, etc.)
func WithSecretRedaction(enabled bool) AgentOption {
	return func(a *Agent) {
		a.secretRedaction = enabled
	}
}

// WithPrivacy sets the PII redaction config for the privacy hook.
// 若 cfg 为 nil 则使用默认配置。注意：此 Option 会重新注册 privacy hook（覆盖默认）。
// 设置 cfg.Enabled=false 可整体关闭 PII 脱敏。
func WithPrivacy(cfg *privacy.Config) AgentOption {
	return func(a *Agent) {
		a.privacyCfg = cfg
		// 重新注册 privacy hook，覆盖 registerBuiltinHooks 中的默认注册
		a.hooks.Unregister("privacy")
		_ = a.hooks.Register(hooks.HookRegistration{
			Name:   "privacy",
			Source: hooks.HookSourceBuiltIn,
			Hook:   hooks.NewPrivacyHook(cfg),
		})
	}
}

// WithReflection configures the self-reflection mechanism
func WithReflection(cfg ReflectionConfig) AgentOption {
	return func(a *Agent) {
		a.reflectionCfg = cfg
		a.reflectEnabled = cfg.Enabled
	}
}

// initReflector initializes the reflector with the current goal
func (a *Agent) initReflector(goal string) {
	if a.reflectEnabled && a.reflector == nil {
		a.reflector = NewReflector(a.reflectionCfg, a.provider, goal)
	}
}

// performReflection runs a self-reflection cycle and injects insights
func (a *Agent) performReflection(ctx context.Context, turn int) error {
	if !a.reflectEnabled || a.reflector == nil {
		return nil
	}
	if !a.reflector.ShouldReflect(turn) {
		return nil
	}

	result, err := a.reflector.Reflect(ctx, a.history, turn)
	if err != nil {
		return err
	}

	// Emit reflection event
	a.Emit(bus.EventKindReflection, map[string]interface{}{
		"turn":     turn,
		"status":   result.Status,
		"progress": result.Progress,
	})

	// Inject reflection insights into history as a user message
	reflectionPrompt := a.reflector.InjectReflectionPrompt(result)
	if reflectionPrompt != "" {
		a.history = append(a.history, provider.Message{
			Role:    "user",
			Content: reflectionPrompt,
		})
	}

	// If stuck or off track, trigger replan by injecting guidance
	if result.Status == ProgressStuck || result.Status == ProgressOffTrack {
		a.history = append(a.history, provider.Message{
			Role:    "user",
			Content: "\n\n⚠ IMPORTANT: You appear to be stuck or off-track. Re-evaluate your approach. Consider:\n1. What is the core goal?\n2. What have you tried that didn't work?\n3. What's a completely different approach you could try?\n4. What information are you missing?\n\nProvide a revised plan and try again with a fresh perspective.",
		})
	}

	return nil
}

// WithTrajectoryLearning enables trajectory-based learning from past executions
func WithTrajectoryLearning(store *cortex.TrajectoryStore, cfg cortex.TrajectoryInjectorConfig) AgentOption {
	return func(a *Agent) {
		a.trajStore = store
		a.trajCfg = cfg
		a.trajEnabled = cfg.Enabled
	}
}

// initTrajectoryInjector initializes the trajectory injector
// 统一数据源：优先复用 cortex.Manager 的 TrajectoryStore，
// 避免与 cortexManager.TrajectoryStore 形成双实例
func (a *Agent) initTrajectoryInjector() {
	if !a.trajEnabled || a.trajInjector != nil {
		return
	}
	store := a.trajStore
	// 若 cortexManager 已有 TrajectoryStore 则优先使用之，统一为单一数据源
	if a.cortexManager != nil && a.cortexManager.GetTrajectoryStore() != nil {
		store = a.cortexManager.GetTrajectoryStore()
	}
	if store == nil {
		return
	}
	// 保持 a.trajStore 与实际使用的存储一致
	a.trajStore = store
	a.trajInjector = cortex.NewTrajectoryInjector(store, a.provider, a.trajCfg)
}

// injectTrajectoryInsights injects trajectory-based learning into the conversation
func (a *Agent) injectTrajectoryInsights(goal string) {
	if a.trajInjector == nil {
		return
	}

	// Inject few-shot examples
	fewShot := a.trajInjector.BuildFewShotPrompt(goal)
	if fewShot != "" {
		a.history = append(a.history, provider.Message{
			Role:    "user",
			Content: fewShot,
		})
	}

	// Inject failure avoidance
	pitfalls := a.trajInjector.BuildFailureAvoidancePrompt(goal)
	if pitfalls != "" {
		a.history = append(a.history, provider.Message{
			Role:    "user",
			Content: pitfalls,
		})
	}

	a.Emit(bus.EventKindTrajectory, map[string]interface{}{
		"action": "injected",
		"goal":   goal,
	})
}

func (a *Agent) registerBuiltinHooks() {
	// Privacy hook（使用 a.privacyCfg；nil 时 NewPrivacyHook 内部回退到 DefaultConfig）
	a.hooks.Register(hooks.HookRegistration{
		Name:   "privacy",
		Source: hooks.HookSourceBuiltIn,
		Hook:   hooks.NewPrivacyHook(a.privacyCfg),
	})
	// Smart approval hook
	ah := NewApprovalHook()
	a.approvalHook = ah
	a.hooks.Register(hooks.HookRegistration{
		Name:   "approval",
		Source: hooks.HookSourceBuiltIn,
		Hook:   ah,
	})
}

// SetSession sets the session ID for event tracking
func (a *Agent) SetSession(session string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = session
}

// SetMemoryScope 动态绑定/更新目录级记忆 scope（例如用户在本会话中途才设置
// 工作目录时由 server 侧调用）。scope 为空回到旧的全局默认桶。目录变更后
// 清掉本 turn 缓存的召回结果，让下一轮按新 scope 重新召回。
func (a *Agent) SetMemoryScope(scope string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.memoryScope = scope
	a.dynamicMemory = ""
	a.dynamicMemoryKey = ""
}

// SetRuleDir 动态绑定/更新静态规则链的工作目录（例如用户在本会话中途才设置
// 工作目录时由 server 侧调用）。变更后清空签名缓存，下一轮消息组装会按新
// 目录重新发现规则文件；dir 为空则关闭规则注入。
func (a *Agent) SetRuleDir(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	dir = strings.TrimSpace(dir)
	if dir == a.ruleDir {
		return
	}
	a.ruleDir = dir
	a.ruleSig = ""
	a.ruleContext = ""
}

// Emit emits an event to the event bus
func (a *Agent) Emit(kind bus.EventKind, data interface{}) {
	a.mu.RLock()
	turn := a.iterationCount
	session := a.session
	a.mu.RUnlock()

	a.bus.Emit(bus.Event{
		Kind:      kind,
		Turn:      turn,
		SessionID: session,
		Data:      data,
	})
}

// addToHistory safely adds a message to history with lock protection
func (a *Agent) addToHistory(msg provider.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	a.history = append(a.history, msg)
}

// addToHistoryMultiple safely adds multiple messages to history
func (a *Agent) addToHistoryMultiple(msgs []provider.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for i := range msgs {
		if msgs[i].Timestamp.IsZero() {
			msgs[i].Timestamp = now
		}
	}
	a.history = append(a.history, msgs...)
}

// getHistory safely returns a copy of history
func (a *Agent) getHistory() []provider.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]provider.Message, len(a.history))
	copy(result, a.history)
	return result
}

// toolLoopRecord 是一次工具调用在循环检测里的记账项。
//
//   - Name 只用于日志与错误信息；
//   - Sig 判"完全相同的一次调用"（工具名 + 全部参数指纹，见 toolCallSignature）；
//   - Key 判"同一个目标资源"（工具名 + 路径/关键词等**去掉装饰性参数后**的对象标识，
//     见 toolCallResourceKey）。两者不可互相替代：Sig 抓"一字不差地重复同一个调用"，
//     Key 抓"换个关键词、换个工具，本质上还是在鼓捣同一个文件"——后者正是
//     2026-10-08 打转事故里逃过所有闸门的形态。
type toolLoopRecord struct {
	Name string
	Sig  string
	Key  string
}

// toolCallSignature 生成循环检测用的调用签名：工具名 + 参数指纹。
//
// 为什么不只比工具名：一个回合里 read_file/list_files/search_in_files 被调用
// 三五次是**正常工作方式**（要多读几个文件），只比名字会把这类回合判成死循环，
// 直接把待执行的调用丢弃。为什么不只比参数：那样又无法定位是哪个工具卡住了。
// 两者结合才同时满足：合法重复（不同目标）不触发、真死循环（同一个调用反复发）
// 立刻触发。
func toolCallSignature(name, args string) string {
	if args == "" {
		return name
	}
	sum := sha256.Sum256([]byte(args))
	return name + "#" + hex.EncodeToString(sum[:4])
}

// resourceKeyFields 是工具参数里"标识这次操作作用在哪个对象上"的字段。
// 命中即拼进资源键（顺序无关，全部命中都拼）。
var resourceKeyFields = []string{
	"path", "file_path", "filepath", "file", "dir", "directory",
	"url", "pattern", "query", "offset", "start_line", "line",
}

// mutatingTools 是**可能改变文件/系统状态**的工具（名字取自 internal/tool 的
// 实际注册名）。
//
// 它们的作用有两个：① 把"此前的读取"作废，所以是统计窗口的分界（最后一次修改
// 之后的重读才叫空转）；② 自身不参与"重复访问"计数。execute_command/terminal/
// execute_code 不是专门的写工具，但能间接改文件（sed、构建、脚本），一律按
// "可能修改"处理——宁可少收口，也不误杀正常的"跑一下看结果"。
var mutatingTools = map[string]bool{
	"write_file":      true,
	"file_edit":       true,
	"edit_file":       true, // 兼容别名
	"batch_file_ops":  true,
	"diff_patch":      true,
	"apply_patch":     true, // 兼容别名
	"execute_command": true,
	"terminal":        true,
	"execute_code":    true,
}

// isMutatingToolCall 判断一次调用是否属于"可能产生修改"的工具。
func isMutatingToolCall(name string) bool { return mutatingTools[name] }

// toolCallResourceKey 提取一次调用的"目标资源"标识。
//
// 与 toolCallSignature（哈希**全部**参数）不同，资源键刻意丢掉"装饰性参数"
// （limit、timeout、encoding、force、dry_run……），只保留"作用在哪个对象上"。
// 这样"换一组无关参数重读同一个文件"仍会被识别成重复访问；而"分段读同一个大
// 文件"（offset 不同）不会被误判成重复。search_in_files 把 pattern 也纳入，
// 因此"换关键词继续搜"不算重复，"反复搜同一个词"才算。
//
// 解析失败（模型给出非法 JSON 或无参数）时退化为工具名，等价于"只看工具名"，
// 与旧行为一致，不引入新的误判。
func toolCallResourceKey(name, args string) string {
	if args == "" {
		return name
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return name
	}
	parts := []string{name}
	for _, k := range resourceKeyFields {
		if v, ok := m[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if len(parts) == 1 {
		return name
	}
	return strings.Join(parts, "|")
}

// resetToolLoopCounters 清零循环检测计数。**每个回合进入工具循环前必须调用。**
//
// 这些计数的语义是"本回合内模型是否卡在重复调用上"，必须按回合隔离。此前只有
// Agent.Reset()（清空整个会话）会清零，于是计数跨回合累积：一个正常会话里某个
// 工具累计用过 sameToolLimit 次之后，**之后每一轮的待执行工具调用都会在判定处
// 被丢弃**，模型被注入"不要再调工具，直接给总结"——用户看到的就是"一直在说
// 计划、永远不执行"（2026-09-29 线上复现，web 聊天主路径）。
func (a *Agent) resetToolLoopCounters() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCallHistory = nil
}

// recordToolCall 记录一次工具调用（无参数变体，供只关心工具名的调用方使用）。
func (a *Agent) recordToolCall(name string) {
	a.recordToolCallSig(name, "")
}

// recordToolCallSig 记录一次工具调用（带参数指纹）。
func (a *Agent) recordToolCallSig(name, args string) {
	if name == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCallHistory = append(a.toolCallHistory, toolLoopRecord{
		Name: name,
		Sig:  toolCallSignature(name, args),
		Key:  toolCallResourceKey(name, args),
	})
}

// emptyToolCallName 是"没有工具名"的调用在循环检测历史里的占位名。
//
// 用占位名而不是空串：recordToolCallSig 对空名直接 return，报错里的
// recentToolCallNames 也需要一个能一眼看出"这是空工具调用"的字样。
const emptyToolCallName = "(empty tool call)"

// recordToolCallsForLoop 把本轮的工具调用记账进循环检测历史。
//
// 空名字的调用**同样必须记账**（用 emptyToolCallName 占位）。此前这里直接
// 跳过，等于给"模型持续返回空名工具调用"开了后门——这类调用三条保护全部
// 失效：不落 registry（因此没有 [TOOL] 日志）、不计入循环阈值
// （detectToolLoop 永远为假）、也不产生 error（executeToolsWithHooks 把空调用
// 转成合成错误结果后仍返回 nil error，lastErr 保持 nil）。结果是四条循环都会
// 一路烧到 maxTurns，最后只回一句裸的 "exceeded maximum turns"，
// 在日志上看起来"什么都没发生过"。
//
// 空名工具调用本身就是异常信号（provider 解析丢字段 / 模型吐坏响应），
// 必须和正常调用一样参与阈值判定，才能被 detectToolLoop 提前收口。
func (a *Agent) recordToolCallsForLoop(toolCalls []types.ToolCall) {
	for i := range toolCalls {
		name := toolCalls[i].GetToolName()
		if name == "" {
			name = emptyToolCallName
		}
		a.recordToolCallSig(name, toolCalls[i].Function.Arguments)
	}
}

// recentToolCallNames 返回最近 n 次工具调用的名字（日志/错误信息用）。
func (a *Agent) recentToolCallNames(n int) []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	start := 0
	if len(a.toolCallHistory) > n {
		start = len(a.toolCallHistory) - n
	}
	names := make([]string, 0, len(a.toolCallHistory)-start)
	for _, rec := range a.toolCallHistory[start:] {
		names = append(names, rec.Name)
	}
	return names
}

// toolCallHistoryLength 返回本回合已记录的工具调用次数。
func (a *Agent) toolCallHistoryLength() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.toolCallHistory)
}

// maxTurnsExhaustedError 是四条工具循环共用的"回合上限耗尽"报错。
//
// 必须共用：入口不同、报错详略不同，会让同一条上层路径（gateway / web / bot）
// 在用户侧表现不一致。此前 cortex 与流式两条入口只回一句裸的
// "exceeded maximum turns (N)"，既没有已完成轮数、也没有本回合调过哪些工具——
// 用户报告"文件都写出来了，却只收到一句 exceeded maximum turns"时，
// 现场完全无法还原（max_turns 与回合超时是两条不同的撞墙路径，见
// NewEnhancedAgent 里的上限说明）。诊断信息统一在这里拼。
func (a *Agent) maxTurnsExhaustedError() error {
	return fmt.Errorf("exceeded maximum turns (%d). Completed %d turns with %d tool calls. Recent tools: %v",
		a.maxTurns, a.iterationCount, a.toolCallHistoryLength(), a.recentToolCallNames(5))
}

// detectToolLoop 依据**本回合**的工具调用历史判定是否已触发循环上限，返回
// (是否触发, 触发原因)。
//
// 四条工具循环 —— RunConversation、RunConversationWithMedia、
// RunConversationStreamWithMedia、RunWithCortex —— 必须共用这一套判定，并且
// **每条入口都要在进入循环前调 resetToolLoopCounters**：
//   - 漏挂保护 ⇒ 工具死循环一路烧到 maxTurns，最后只回一句裸的
//     "exceeded maximum turns"；
//   - 漏了按回合清零 ⇒ 计数跨回合累积，正常会话的后续轮次被整轮误判成死循环，
//     模型被要求"不要再调工具"，表现是只出计划、不执行。
//
// 计数按 toolCallHistory 的出现顺序推进，而不是遍历 map：map 顺序随机，
// 会让同一段历史给出不同的触发原因（日志与测试都不可复现）。
func (a *Agent) detectToolLoop() (bool, string) {
	a.mu.RLock()
	history := make([]toolLoopRecord, len(a.toolCallHistory))
	copy(history, a.toolCallHistory)
	a.mu.RUnlock()

	// 死循环信号是**连续**发起同一个调用（工具名 + 参数指纹全同），不是
	// "本回合累计"重复过几次。
	//
	// 旧实现是 `counts[rec.Sig] >= sameToolLimit`，而 counts 统计的是**整个
	// 回合**的出现次数：同一(工具+参数)在回合里第 3 次出现就收口——无论中间
	// 夹了多少有进展的调用。长任务里"读同一个文件三次以确认改动"、"跑同一条
	// build 命令三次"这类完全正常的动作必然撞上它，concludeAfterToolLoop 立刻
	// 收口并要求模型"不要再调工具，给总结"。用户看到的就是"长任务跑一阵就停、
	// 只能手动说'继续'"——会话里会留下那句合成提示词
	// "Please provide a final summary ... Do not call any more tools"。
	//
	// 连续重复才是卡死；间隔性的合法重复不该触发。"换着工具名交替重复"
	// （A B A B）由下面的连续无进展兜底负责，这里不重复覆盖。
	streak := 0
	prevSig := ""
	for i, rec := range history {
		if i > 0 && rec.Sig == prevSig {
			streak++
		} else {
			streak = 1
		}
		if streak >= a.sameToolLimit {
			return true, fmt.Sprintf("tool %s called %d times in a row with identical arguments", rec.Name, streak)
		}
		prevSig = rec.Sig
	}

	// 总量兜底：只有**连续无进展**才算失控，不能只数总数。
	//
	// 旧实现是 `len(history) >= consecutiveLimit`（默认 25），即单回合内累计
	// 发起 25 次调用就直接收口。这个口径对正常任务是灾难：一次二十来步的重构
	// （读 5~6 个文件 + 多轮编辑 + 验证）轻松超过 25 次**各不相同**的调用，
	// 于是任务在"几十轮"处被腰斩，模型被要求"不要再调工具，给总结"——
	// 用户看到的就是"几十轮就停止"，且与 maxTurns(150) 的死因完全不同。
	//
	// 正确的信号是"有没有推进"：新签名（本回合首次出现的 工具+参数）代表进展，
	// 重复签名代表原地打转。只有从尾部往前连续 consecutiveLimit 条**全部**是
	// 已经出现过的签名时，才认定为失控。这样"调用多但每步都不同"永不被误杀，
	// 而"换着工具名重复同一件事"仍会被提前收口。真正的总上限由 maxTurns 兜底。
	if a.consecutiveLimit > 0 && len(history) >= a.consecutiveLimit {
		seen := make(map[string]bool, len(history))
		noProgress := 0
		for _, rec := range history {
			if seen[rec.Sig] {
				noProgress++
				if noProgress >= a.consecutiveLimit {
					return true, fmt.Sprintf("%d consecutive tool calls without progress (no new tool+args combination)", noProgress)
				}
				continue
			}
			seen[rec.Sig] = true
			noProgress = 0
		}
	}
	// 第三道：同一目标资源的重复访问（**累计**口径，但按"资源"而不是"完整签名"聚合）。
	//
	// 前两道只能识别"重复签名"，对**间隔性重复**完全免疫——每一步换个文件、换个
	// 关键词，看起来每步都在推进，实际上原地打转。这里换一个问法："期间改过东西
	// 吗？没改过的话，重读还能带来新信息吗？"以**最后一次修改性调用**为分界，只统计
	// 其后的访问：改完文件后重读同一个文件达到 repeatedResourceLimit 次且期间一次
	// 没改，就不可能是有效工作。
	if a.repeatedResourceLimit > 0 {
		lastMutation := -1
		for i, rec := range history {
			if isMutatingToolCall(rec.Name) {
				lastMutation = i
			}
		}
		counts := make(map[string]int)
		for _, rec := range history[lastMutation+1:] {
			if isMutatingToolCall(rec.Name) {
				continue
			}
			// 资源键退化成工具名（参数里没有 path/pattern/url 之类的对象标识）时
			// 不参与计数：这类工具的"对象"不可比较，硬数会把"一次并行建 4 个任务"
			// 这类完全正常的批量操作误杀。
			if rec.Key == rec.Name {
				continue
			}
			counts[rec.Key]++
			if counts[rec.Key] >= a.repeatedResourceLimit {
				return true, fmt.Sprintf("resource %q accessed %d times since the last modification without making any change", rec.Key, counts[rec.Key])
			}
		}
	}
	return false, ""
}

// concludeAfterToolLoop 是循环触发后的共用收口动作：把模型已经产出的内容
// 追加进历史，再要一份"不要再调工具"的总结。返回总结文本；第二次调用失败
// 时返回错误（调用方自行决定是返回错误还是静默收场）。
func (a *Agent) concludeAfterToolLoop(ctx context.Context, lastContent string) (string, error) {
	a.history = append(a.history, provider.Message{
		Role:      "assistant",
		Content:   utils.TruncateDetailed(lastContent, a.maxMsgLen),
		Timestamp: time.Now(),
	})
	a.history = append(a.history, provider.Message{
		Role:      "user",
		Content:   "Please provide a final summary of what has been accomplished so far. Do not call any more tools.",
		Timestamp: time.Now(),
	})

	finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages())
	if finalErr != nil {
		return "", finalErr
	}
	summaryText := wrapLLMReasoning(finalResp.ReasoningContent, finalResp.Content)
	a.history = append(a.history, provider.Message{
		Role:      "assistant",
		Content:   utils.TruncateDetailed(summaryText, a.maxMsgLen),
		Timestamp: time.Now(),
	})
	return summaryText, nil
}

// incrementIteration safely increments the iteration count
func (a *Agent) incrementIteration() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.iterationCount++
}

// getIteration safely returns the current iteration count
func (a *Agent) getIteration() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.iterationCount
}

// AddSkillsContext adds skills context to system prompt.
// Kept as the historical entry point; it now delegates to SetSkillsContext so
// the injected block is tracked and can be replaced later.
func (a *Agent) AddSkillsContext(skillsCtx string) {
	a.SetSkillsContext(skillsCtx)
}

// SetSkillsContext 以「替换」语义更新系统提示里的技能清单。
//
// 为什么需要替换而不是追加：agent 是按会话缓存的，技能块此前只在创建时
// 注入一次，用户随后批准/删除自动技能后当前会话仍用旧清单（表现为
// 「批准了却感知不到」）。重建 agent 代价太大——web 聊天不从 DB 回灌历史
// （getOrCreateAgent），重建会丢掉整段会话上下文。故原地替换。
func (a *Agent) SetSkillsContext(skillsCtx string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	old := a.skillsCtx
	a.skillsCtx = skillsCtx

	for i, msg := range a.history {
		if msg.Role != "system" {
			continue
		}
		content := a.history[i].Content
		if old != "" {
			if strings.Contains(content, "\n\n"+old) {
				content = strings.Replace(content, "\n\n"+old, "", 1)
			} else if content == old {
				// 系统提示恰好只由旧技能块构成（无前导分隔符）
				content = ""
			}
		}
		switch {
		case skillsCtx == "":
			// 清空：保持移除后的内容
		case content == "":
			content = skillsCtx
		default:
			content += "\n\n" + skillsCtx
		}
		a.history[i].Content = content
		return
	}

	if skillsCtx != "" {
		a.history = append([]provider.Message{{
			Role:    "system",
			Content: skillsCtx,
		}}, a.history...)
	}
}

// wrapLLMReasoning wraps the provider's reasoning_content in <think> tags
// alongside the normal text content. If the provider sent no reasoning the
// original content is returned unchanged. Used by the non-streaming
// fallbacks so thinking still shows up in the UI and is persisted to
// history exactly like the streaming path does.
//
// 关键不变量（10-04 事故）：content 为空时返回**空串**，不得只返回
// "<think>R</think>"。reasoning 是思考过程，不是回答——把它单独当正文返回，
// 落库的历史就变成"只有思考、没有产出"的残缺轮次，模型下一轮读到自己这份
// 历史只能重新推导、重新确认，症状就是同一个简单任务反复询问用户、几十轮
// 停不下来。流式路径的同一不变量见 finalizeFullContent。
func wrapLLMReasoning(reasoning, content string) string {
	reasoning = neutralizeThinkTags(strings.TrimSpace(reasoning))
	if reasoning == "" {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return "<think>" + reasoning + "</think>\n" + content
}

// neutralizeThinkTags 删掉文本里作为**字面量**出现的 <think> / </think> 标签
// （大小写不敏感），保留标签之外的正文。
//
// 它只用于"即将被包进 <think>...</think> 的 reasoning 文本"。包装方自己会补一对
// 标签，如果 reasoning 内部又带着模型自己写出的标签，落库结果就是嵌套：
// `<think>A<think>B</think>` —— 开闭数量失衡，前端思考块解析错乱，而且这串历史
// 下一轮又被模型看到、继续模仿，每轮净增一个未闭合标签，自增强式膨胀。
//
// 实测线上会话库（最近 40 个会话、381 条 assistant 消息）：96 条嵌套、21 条截断，
// 只有 68 条开闭平衡；单条最长 262,923 字符，绝大部分是重复的思考片段。
//
// 与 provider.StripThinkTrails 的区别：那个是"从开标签丢弃到首个闭标签"，用在
// `A<think>B` 上会把 B 一起吃掉；这里只摘掉标签本身，正文完整保留。
func neutralizeThinkTags(s string) string {
	if s == "" {
		return s
	}
	low := strings.ToLower(s)
	if !strings.Contains(low, "<think") && !strings.Contains(low, "</think") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	cursor := 0
	for cursor < len(s) {
		i := strings.Index(low[cursor:], "<")
		if i < 0 {
			b.WriteString(s[cursor:])
			break
		}
		open := cursor + i
		b.WriteString(s[cursor:open])
		// 标签的大小写变体长度一致，所以可以直接用 low 上的偏移量切开原串。
		switch {
		case strings.HasPrefix(low[open:], "</think>"):
			cursor = open + len("</think>")
		case strings.HasPrefix(low[open:], "<think>"):
			cursor = open + len("<think>")
		default:
			b.WriteByte(s[open])
			cursor = open + 1
		}
	}
	return b.String()
}

// thinkTagLiterals 是需要被中和的字面标签（比较时大小写不敏感）。
var thinkTagLiterals = []string{"<think>", "</think>"}

// thinkStreamNeutralizer 在**流式推送**路径上中和模型自己写出的 <think> 标签。
//
// 为什么不能直接对每个 chunk 调 neutralizeThinkTags：标签会被切在 chunk 边界上
// （"<thi" + "nk>"、"<" + "/think>" 都是常见的 token 切法），逐 chunk 处理会把
// 半截标签推给客户端，拼接后 UI 里照样看到标签。因此这里回退暂存"末尾最长可能
// 是标签前缀"的那几个字符（≤ len("<think>")-1 = 7），等下个 chunk 到齐再一起处理。
//
// 注意：**不能**用它处理包装方自己补的 <think>/</think>（那是给前端渲染思考块的
// 结构标记），所以调用方只把 provider 给的 ReasoningContent / Content 送进来。
type thinkStreamNeutralizer struct {
	pending string
}

// push 送入一个 chunk，返回当前可以安全推给客户端的文本（可能为空串）。
func (n *thinkStreamNeutralizer) push(chunk string) string {
	s := n.pending + chunk
	hold := pendingTagPrefixLen(s)
	if hold > 0 {
		n.pending = s[len(s)-hold:]
		s = s[:len(s)-hold]
	} else {
		n.pending = ""
	}
	return neutralizeThinkTags(s)
}

// flush 吐出暂存的尾部（流结束时调用，避免最后几个字符被丢在缓冲区里）。
func (n *thinkStreamNeutralizer) flush() string {
	s := n.pending
	n.pending = ""
	return neutralizeThinkTags(s)
}

// pendingTagPrefixLen 返回 s 末尾"可能是不完整标签"的最长长度。
// 例："...abc<thi" → 4；"...abc" → 0。
func pendingTagPrefixLen(s string) int {
	low := strings.ToLower(s)
	best := 0
	for _, tag := range thinkTagLiterals {
		max := len(tag) - 1 // 完整标签不会停在末尾（那样它已被中和）
		if len(low) < max {
			max = len(low)
		}
		for l := max; l > 0; l-- {
			if strings.HasSuffix(low, tag[:l]) {
				if l > best {
					best = l
				}
				break
			}
		}
	}
	return best
}

// trySubTaskDelegation checks if the task is complex and delegates to sub-task executor.
// Returns true if delegated and handled, false if should proceed normally.
func (a *Agent) trySubTaskDelegation(ctx context.Context, input string) (bool, string, error) {
	if !a.subTaskEnabled {
		return false, "", nil
	}

	analyzer := complexity.NewAnalyzer()
	if !analyzer.ShouldDecompose(input) {
		return false, "", nil
	}

	// Task is complex - delegate to sub-task executor
	result, err := a.ExecuteComplexTask(ctx, input)
	if err != nil {
		// If delegation fails, fall through to normal conversation
		return false, "", nil
	}

	if result == "" {
		return false, "", nil
	}

	// Add the sub-task results to conversation history
	a.history = append(a.history,
		provider.Message{Role: "user", Content: input},
		provider.Message{Role: "assistant", Content: result + "\n\nBased on the sub-task results above, here is my complete response:"},
	)

	return true, result, nil
}

// RunConversationWithMedia runs a conversation with multimodal input support.
// If contentParts is provided, it takes priority over plain text input.
func (a *Agent) RunConversationWithMedia(ctx context.Context, input string, contentParts []types.ContentPart) (string, error) {
	// 与 RunConversationStreamWithMedia 一致：回合结束时通知观察者（见 defer
	// 在该函数中的说明）。非流式路径同样可能被 web chat 的降级分支调用。
	defer notifyTurnFinished(ctx)

	// If cortex is enabled, use the full cortex integration path.
	// 与 RunConversation 同样的判定：只判 `!= nil` 会在 cortex.enabled=false 时
	// 与 RunWithCortex 的回落分支互递归 → fatal stack overflow。
	if a.cortexManager != nil && a.cortexManager.IsEnabled() {
		return a.RunWithCortex(ctx, input)
	}

	// 回合开始：清零循环检测计数（跨回合累积会把后续正常轮次整轮误判成死循环）。
	a.resetToolLoopCounters()

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Skip sub-task delegation when contentParts are present (e.g., multimodal input with images)
	// This prevents losing the media content when delegating to sub-task executor
	if len(contentParts) == 0 {
		// Auto sub-task delegation for complex tasks
		if delegated, result, err := a.trySubTaskDelegation(ctx, input); delegated {
			if err != nil {
				return "", err
			}
			// Synthesis prompt to summarize sub-task results
			return a.RunConversation(ctx,
				fmt.Sprintf(`I have completed the sub-tasks for the task. The sub-tasks execution results are:

%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`, result))
		}
	}

	// Build user message - use content parts if available, otherwise fall back to plain text
	userMsg := provider.Message{
		Role: "user",
	}
	if len(contentParts) > 0 {
		userMsg.ContentParts = contentParts
	} else {
		userMsg.Content = utils.TruncateDetailed(input, a.maxMsgLen)
	}
	userMsg.Timestamp = time.Now()
	a.history = append(a.history, userMsg)

	// Truncate history to prevent overflow
	a.truncateHistory()

	// P0-1: 新用户输入进入时召回动态记忆（turn 内稳定，出站时注入）
	a.prepareDynamicMemory(input)

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Check if context was cancelled (user pressed /stop)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// Deadline 感知（Hermes-style）：剩余 ctx 寿命不足以再跑一轮完整迭代
		// （LLM 调用 + 工具执行）时，优雅收尾并落盘 checkpoint，而不是让最后
		// 一轮死在半路、以 opaque 的 "context deadline exceeded" 告终。
		if !a.canStartAnotherTurn(ctx) {
			return a.gracefulDeadlineFinish(input), nil
		}
		a.beginTurnTiming()

		// Cortex: OnTurnStart - freezes memory snapshot for prefix cache
		if a.cortexManager != nil {
			a.cortexManager.OnTurnStart()
		}

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			return "", fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			return "", fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
		}

		// Emit turn start event
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn": a.iterationCount,
		})

		// 引导注入：与流式循环一致（见 guide.go）。
		a.drainGuidesIntoHistory()

		// 上下文压缩：位置与 RunConversation 一致（都在 buildLLMMessages
		// 之前）。媒体循环此前漏了这一步，而压缩阈值只有 8000 tokens ⇒
		// 带图会话的历史只增不减：群聊成员每次发言都把整包历史（实测单
		// 成员会话已到 177KB、含多份内联图片）重新发给模型，回合越跑越
		// 慢，最后撞满回合上限、一个字都产不出来。
		a.maybeCompressContext()

		// Build LLM request. buildLLMMessages strips <think> reasoning trails
		// from the outbound copy — see stripThinkContent for why.
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.buildLLMMessages(),
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, decision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		if decision.Action == hooks.HookActionStop {
			return "", fmt.Errorf("hook stopped: %s", decision.Reason)
		}
		if decision.Action == hooks.HookActionReject {
			return "", fmt.Errorf("hook rejected: %s", decision.Reason)
		}

		// Defensive: enforce message alternation before sending to the
		// provider. Providers reject malformed histories (two assistants in a
		// row, a tool message without a preceding assistant tool_call) with
		// opaque 400 errors. Sanitizing here turns those into a clean,
		// best-effort repair so the loop self-corrects instead of burning a
		// retry (Hermes-style pre-provider validation).
		if violations := ValidateMessageAlternation(req.Messages); len(violations) > 0 {
			sample := make([]string, 0, 3)
			for i, v := range violations {
				if i >= 3 {
					break
				}
				sample = append(sample, fmt.Sprintf("#%d(%s):%s", v.Index, v.Role, v.Reason))
			}
			log.Warnf("[Agent] sanitizing %d message-alternation violation(s) before LLM call (sample: %s)",
				len(violations), strings.Join(sample, " | "))
			before := len(req.Messages)
			req.Messages = SanitizeMessageHistory(req.Messages)
			after := len(req.Messages)
			if after < before {
				log.Debugf("[Agent] message-alternation repair dropped %d message(s) (%d -> %d)", before-after, before, after)
			}
		}

		// Use ChatWithTools for OpenAI provider if tools are available
		var resp *provider.ChatResponse
		type openAIlike interface {
			ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
		}
		if oa, ok := a.provider.(openAIlike); ok && len(a.tools) > 0 {
			resp, err = oa.ChatWithTools(ctx, req.Messages, req.Tools)
		} else {
			resp, err = a.provider.Chat(ctx, req.Messages)
		}

		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindError, err.Error())
			// 已死的 ctx 无从恢复：与 RunConversation 一致，立即以明确的
			// 原因收场，而不是继续往一个已经过期的上下文里发请求。
			if cerr := ctx.Err(); cerr != nil {
				return "", fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, cerr)
			}
			// 请求"挂住"吃掉的是一次传输超时（默认 180s），而回合预算通常
			// 只有几分钟：原地重试只会再挂一次，最终整个回合一个字都没有
			// （bot 侧表现为 turn_timeout，群聊里则该成员白白占用整轮）。
			// 直接失败，让调用方立即记录原因并推进。
			if llmRequestTimedOut(err) {
				return "", fmt.Errorf("provider request timed out after %d turn(s): %w", a.iterationCount+1, err)
			}
			continue
		}

		// Track token usage from the response
		if resp.Usage != nil {
			a.inputTokens += resp.Usage.PromptTokens
			a.outputTokens += resp.Usage.CompletionTokens
		}

		// Call AfterLLM hooks. Non-streaming path: wrap any
		// ReasoningContent in <think> tags so the UI displays it the
		// same way as the streaming path.
		llmResp := &hooks.LLMHookResponse{
			Content:   wrapLLMReasoning(resp.ReasoningContent, resp.Content),
			ToolCalls: resp.ToolCalls,
		}
		llmResp, decision, err = a.hooks.AfterLLM(ctx, llmResp)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// No tool calls - return the response
		if len(resp.ToolCalls) == 0 {
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   content,
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			return content, nil
		}

		// Has tool calls - execute them
		toolResults, err := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if err != nil {
			lastErr = err
			continue
		}

		// Add assistant message and tool results to history
		// assistant 消息同样受 maxMsgLen 约束：无上限的思考/长文本
		// 会随每轮请求整包重发，是上下文雪球的主要来源之一。
		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   utils.TruncateDetailed(llmResp.Content, a.maxMsgLen),
			ToolCalls: resp.ToolCalls,
			Timestamp: time.Now(),
		})

		for _, result := range toolResults {
			var resultContent string
			if result.Err != nil {
				resultContent = utils.ErrTruncateDetailed(fmt.Sprintf("Error: %v", result.Err), result.Name+"_error", a.maxMsgLen)
			} else {
				resultContent = utils.TruncateDetailed(result.Content, a.maxMsgLen)
			}
			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    resultContent,
				Timestamp:  time.Now(),
				ToolCallID: result.ID,
			})
		}
		a.endTurnTiming()

		// 工具循环判定走共用实现（四条入口同一套阈值，见 detectToolLoop）。
		a.recordToolCallsForLoop(resp.ToolCalls)
		if loopDetected, loopReason := a.detectToolLoop(); loopDetected {
			log.Warnf("[Agent] tool call loop detected: %s (turn %d)", loopReason, a.iterationCount)
			summaryText, finalErr := a.concludeAfterToolLoop(ctx, resp.Content)
			if finalErr != nil {
				return "", fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			a.endCortexTurn()
			return redact.RedactIfEnabled(summaryText, a.secretRedaction), nil
		}
	}

	// Try to get a summary from the LLM before giving up
	a.history = append(a.history, provider.Message{
		Role:      "user",
		Content:   "You have reached the maximum number of turns. Please provide a brief summary of what you accomplished and what remains incomplete. Do NOT call any more tools.",
		Timestamp: time.Now(),
	})
	if ctx.Err() == nil {
		if finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages()); finalErr == nil {
			turnSummary := wrapLLMReasoning(finalResp.ReasoningContent, finalResp.Content)
			if turnSummary != "" {
				a.history = append(a.history, provider.Message{
					Role:      "assistant",
					Content:   utils.TruncateDetailed(turnSummary, a.maxMsgLen),
					Timestamp: time.Now(),
				})
				return redact.RedactIfEnabled(turnSummary, a.secretRedaction), nil
			}
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", a.maxTurnsExhaustedError()
}

// llmRequestTimedOut reports whether an LLM request failed because of a
// transport-level timeout rather than a response the provider actually
// produced.
//
// Why it matters: the provider's HTTP client aborts a hung request after
// DefaultTimeoutDuration (180s). A bot turn budget is typically only a few
// minutes, so an instant retry after such a hang re-enters the same stall and
// the turn ends with zero output — observed in production as a group-chat
// member burning the whole 5-minute turn timeout and returning nothing, while
// the room round waited on it the entire time. Callers should fail fast
// instead of retrying a request that timed out at the transport layer.
func llmRequestTimedOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// maybeCompressContext summarizes the middle of a.history once the estimated
// token count crosses the compressor's threshold, replacing it with one system
// summary message while keeping the protected head/tail.
//
// The protected head/tail are chosen by MESSAGE COUNT, so the cut can land in
// the middle of a tool exchange. Everything the kept messages need to stay
// structurally valid must therefore survive the round trip through
// compress.Message: dropping tool_calls / tool_call_id here (a) leaves "tool role
// message without its assistant tool_calls" — which providers reject with an
// opaque 400 — and (b) makes the history sanitizers discard those tool results on
// the next load, silently starving the model of its own observations. Dropping
// content_parts likewise throws away multimodal attachments mid-conversation.
//
// Genuinely split pairs (owner summarised away) are still handled downstream by
// sanitizeHistory Pass 1/Pass 4 — the job here is simply not to destroy data.
func (a *Agent) maybeCompressContext() {
	if a.compressor == nil {
		return
	}
	if !a.compressor.ShouldCompress(a.GetHistoryLength() / 4) { // rough token estimate
		return
	}
	a.compressContext()
}

// CompressNow 立即执行一次摘要压缩，**绕过** token 阈值。
//
// 阈值配置只应约束"自动触发"（maybeCompressContext 在每个回合的开始处调用），
// 用户显式敲的 /compress 应当立刻生效，而不是等到上下文涨到阈值才动 —— 这正是
// TUI 旧实现（EnableCompression(true)）没做到的事：它只翻了一个与压缩机制**无关**
// 的布尔量，然后把"Compression enabled"当成成功消息打给用户。
//
// 返回是否真的产生了新摘要（历史变短）。
func (a *Agent) CompressNow() bool {
	if a.compressor == nil {
		return false
	}
	return a.compressContext()
}

// compressContext 无条件跑一次压缩，返回历史是否真的变短。
//
// 拆成独立函数的两个理由：①手动入口（CompressNow）要绕过阈值门；
// ②截断路径（maybeCompressBeforeTruncate）必须知道"压缩到底生效没有"，
// 才能决定是停下、还是继续做破坏性的字节裁剪。
func (a *Agent) compressContext() bool {
	if a.compressor == nil {
		return false
	}
	before := a.GetHistoryLength()

	a.Emit(bus.EventKindTurnStart, map[string]interface{}{
		"type":   "progress",
		"phase":  "compressing",
		"detail": "Context window full, compressing history...",
	})

	msgs := make([]compress.Message, 0, len(a.history))
	for _, msg := range a.history {
		if msg.Role == "system" {
			continue
		}
		msgs = append(msgs, compress.Message{
			Role:         msg.Role,
			Content:      msg.Content,
			ToolCalls:    msg.ToolCalls,
			ToolCallID:   msg.ToolCallID,
			ContentParts: msg.ContentParts,
		})
	}
	result, err := a.compressor.Compress(msgs, "")
	if err != nil || result == nil {
		return false
	}

	// Replace history with the compressed version: system prompts keep their
	// original position at the head, then the compressor's head/summary/tail.
	newHistory := make([]provider.Message, 0, len(a.history))
	for _, msg := range a.history {
		if msg.Role == "system" {
			newHistory = append(newHistory, msg)
		}
	}
	for _, msg := range result.Messages {
		newHistory = append(newHistory, provider.Message{
			Role:         msg.Role,
			Content:      msg.Content,
			ToolCalls:    msg.ToolCalls,
			ToolCallID:   msg.ToolCallID,
			ContentParts: msg.ContentParts,
		})
	}
	a.history = newHistory
	return a.GetHistoryLength() < before
}

// maybeCompressBeforeTruncate 在字节级截断之前给摘要器一次机会，返回"历史
// 是否真的被摘要缩短了"。
//
// 两条路径的语义差是这件事的全部理由：压缩把中段**摘要**成一条能继续接力的
// 记录；truncateHistory 的字节级路径把整段 user 块**直接删掉**、不留任何摘要
// （2026-10-08 "读完就忘"事故的形态）。所以只要摘要器可用，就不该轮到硬截断。
//
// 这里判的是"摘要器是否装配"（compressor != nil），而**不再经过 token 阈值**：
// 调用方只会在历史已越过硬上限时叫到这里，而硬上限恒 ≥ 压缩触发点 × 5/4
// （见 historyHardLimit），所以那一刻阈值本来就必然已经越过；此时多做一次摘要
// 远比"整段删掉 user 块"便宜。真正无内容可压时 Compress 会原样返回（它自己的
// ProtectFirst/Last 门），本函数于是返回 false，调用方照旧走 sanitize / 字节路径。
//
// 旧实现这两处判的是 a.compressionEnabled && a.compressionRatio，两个字段都只由
// TUI 的 /compress 设置，而 compressionRatio 全仓库零赋值（恒为 0）：
//   - web / gateway / bot / cron 走 WithCompression（只设 ThresholdTokens，从不置
//     compressionEnabled）⇒ 这两个兜底在四个入口里**永远不触发**，退化成"sanitize
//     之后把超长历史原样发给模型"；
//   - TUI 一旦敲过 /compress ⇒ `totalLen > limit*0` 恒真 ⇒ **每次** truncateHistory
//     都跑老的规则式 compressHistory()（只留前 2 + 后 4 条 user 消息，与 token 阈值
//     毫无关系），中段每回合都被静默换掉。
func (a *Agent) maybeCompressBeforeTruncate() bool {
	return a.CompressNow()
}

// RunConversation runs a conversation with automatic tool execution
func (a *Agent) RunConversation(ctx context.Context, input string) (string, error) {
	// 回合收尾钩子，与 RunConversationStreamWithMedia 同语义。
	defer notifyTurnFinished(ctx)

	// If cortex is enabled, use the full cortex integration path.
	// 必须同时判 IsEnabled()：用禁用配置构造出来的 Manager 是个空壳（各子系统为 nil），
	// 而 RunWithCortex 在 !IsEnabled() 时会回落到 RunConversation —— 只看 `!= nil`
	// 会让两者无限互递归，最终是**不可 recover 的 fatal stack overflow 直接杀进程**。
	// 现在两边判定一致（见 cortex_integration.go RunWithCortex 开头），禁用时正常走本地路径。
	if a.cortexManager != nil && a.cortexManager.IsEnabled() {
		return a.RunWithCortex(ctx, input)
	}

	// 回合开始：清零循环检测计数（跨回合累积会把后续正常轮次整轮误判成死循环）。
	a.resetToolLoopCounters()

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Auto sub-task delegation for complex tasks
	if delegated, result, err := a.trySubTaskDelegation(ctx, input); delegated {
		if err != nil {
			return "", err
		}
		// Synthesis prompt to summarize sub-task results
		return a.RunConversation(ctx,
			fmt.Sprintf(`I have completed the sub-tasks for the task. The sub-tasks execution results are:

%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`, result))
	}

	// Truncate input
	a.history = append(a.history, provider.Message{
		Role:      "user",
		Content:   utils.TruncateDetailed(input, a.maxMsgLen),
		Timestamp: time.Now(),
	})

	// Initialize self-reflection with the goal
	if a.reflectEnabled {
		a.initReflector(input)
	}

	// Initialize and inject trajectory-based learning
	// 同时考虑 cortexManager 的 TrajectoryStore（统一数据源，避免双实例）
	if a.trajEnabled && (a.trajStore != nil || (a.cortexManager != nil && a.cortexManager.GetTrajectoryStore() != nil)) {
		a.initTrajectoryInjector()
		a.injectTrajectoryInsights(input)
	}

	// Truncate history to prevent overflow
	a.truncateHistory()

	// P0-1: 新用户输入进入时召回动态记忆（turn 内稳定，出站时注入）
	a.prepareDynamicMemory(input)

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Fast-fail when the caller's context is already finished (deadline or
		// cancel). Retrying on a dead context only burns turns/budget and ends
		// with an opaque "context deadline exceeded" — abort immediately.
		if cerr := ctx.Err(); cerr != nil {
			return "", fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount, cerr)
		}

		// Deadline 感知：剩余时间不足以再完成一轮完整迭代时优雅收尾 + checkpoint。
		if !a.canStartAnotherTurn(ctx) {
			return a.gracefulDeadlineFinish(input), nil
		}
		a.beginTurnTiming()

		// Consume budget for this iteration
		if a.budget != nil && !a.budget.Consume() {
			return "", fmt.Errorf("exceeded iteration budget (%d/%d used)", a.budget.Used(), a.budget.MaxTotal())
		}

		// Cortex: OnTurnStart - freezes memory snapshot for prefix cache
		if a.cortexManager != nil {
			a.cortexManager.OnTurnStart()
		}

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			return "", fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			return "", fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
		}

		// Emit turn start event with progress
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn":        a.iterationCount,
			"maxTurns":    a.maxTurns,
			"budgetUsed":  a.budget.Used(),
			"budgetTotal": a.budget.MaxTotal(),
		})

		// 引导注入：与 RunConversationWithMedia 的循环一致（见 guide.go）。
		// 排水必须在 buildLLMMessages 之前，本次 LLM 调用才能看到引导内容；
		// 迭代 0 时引导并入刚追加的本回合输入（合并策略见 applyGuides）。
		a.drainGuidesIntoHistory()

		// Self-reflection check (every N turns)
		if a.reflector != nil {
			if err := a.performReflection(ctx, a.iterationCount); err != nil {
				log.Warnf("[Agent] Reflection failed: %v", err)
			}
		}

		// Check if context compression is needed
		a.maybeCompressContext()

		// Build LLM request. buildLLMMessages strips <think> reasoning trails
		// from the outbound copy — see stripThinkContent for why.
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.buildLLMMessages(),
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, decision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		if decision.Action == hooks.HookActionStop {
			return "", fmt.Errorf("hook stopped: %s", decision.Reason)
		}
		if decision.Action == hooks.HookActionReject {
			return "", fmt.Errorf("hook rejected: %s", decision.Reason)
		}

		// Defensive: enforce message alternation before sending to the
		// provider. Providers reject malformed histories (two assistants in a
		// row, a tool message without a preceding assistant tool_call) with
		// opaque 400 errors. Sanitizing here turns those into a clean,
		// best-effort repair so the loop self-corrects instead of burning a
		// retry (Hermes-style pre-provider validation).
		if violations := ValidateMessageAlternation(req.Messages); len(violations) > 0 {
			sample := make([]string, 0, 3)
			for i, v := range violations {
				if i >= 3 {
					break
				}
				sample = append(sample, fmt.Sprintf("#%d(%s):%s", v.Index, v.Role, v.Reason))
			}
			log.Warnf("[Agent] sanitizing %d message-alternation violation(s) before LLM call (sample: %s)",
				len(violations), strings.Join(sample, " | "))
			before := len(req.Messages)
			req.Messages = SanitizeMessageHistory(req.Messages)
			after := len(req.Messages)
			if after < before {
				log.Debugf("[Agent] message-alternation repair dropped %d message(s) (%d -> %d)", before-after, before, after)
			}
		}

		// Use ChatWithTools for OpenAI provider if tools are available
		var resp *provider.ChatResponse
		type openAIlike interface {
			ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
		}
		if oa, ok := a.provider.(openAIlike); ok && len(a.tools) > 0 {
			log.Infof("[Agent:RunConversationWithMedia] Calling ChatWithTools: provider=%s, messages=%d, tools=%d",
				a.provider.Name(), len(req.Messages), len(a.tools))
			resp, err = oa.ChatWithTools(ctx, req.Messages, req.Tools)
		} else {
			log.Warnf("[Agent:RunConversation] Falling back to Chat (no tools): provider=%s, hasToolIface=%v, toolsCount=%d",
				a.provider.Name(), ok, len(a.tools))
			resp, err = a.provider.Chat(ctx, req.Messages)
		}

		if err != nil {
			lastErr = err
			// A finished context cannot recover: retries and backoff sleeps are
			// pointless. Abort the turn immediately with a clear cause.
			if cerr := ctx.Err(); cerr != nil {
				a.Emit(bus.EventKindError, cerr.Error())
				return "", fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, cerr)
			}
			// Use error classifier for intelligent retry
			var classified *retry.ClassifiedError
			if a.errorClassifier != nil {
				// Provider errors wrap the real HTTP status in the message
				// text (e.g. "stream API returned status 402: ..."). Parse it
				// so the classifier's status-code switchboard fires for
				// 401/402/429 instead of falling back to FailoverUnknown.
				status := retry.ExtractStatusCode(err.Error())
				classified = a.errorClassifier.Classify(err, status, a.provider.Name(), "")
				strategy := retry.GetRecoveryStrategy(classified, 1)
				// Permanent errors must not be re-fed through the loop.
				// strategy.Abort covers the per-reason table; the explicit
				// classified.Retryable check is the belt-and-suspenders so a
				// classifier table oversight (e.g. a new Retryable=false
				// reason without Abort=true) can never silently burn turns
				// until the repeated-failure detector escalates.
				if strategy.Abort || (classified != nil && !classified.Retryable) {
					if isMessagesFormatError(err) {
						log.Warnf("[Agent] permanent message-format rejection: %s", messageShapeSummary(req.Messages))
					}
					return "", fmt.Errorf("non-retryable error: %w (messages shape: %s)", err, messageShapeSummary(req.Messages))
				}
				if strategy.Delay > 0 {
					time.Sleep(strategy.Delay)
				}
			}
			// Repeated-failure escalation: halt with a structured diagnostic
			// instead of silently retrying until the turn cap (Hermes #22112).
			if a.failureDetector != nil {
				if esc := a.failureDetector.Record(classified, "", err.Error()); esc != nil {
					a.Emit(bus.EventKindError, esc.Error())
					return "", esc
				}
			}
			a.Emit(bus.EventKindError, err.Error())
			continue
		}

		// Track token usage from the response
		a.trackUsage(resp)

		// Call AfterLLM hooks
		llmResp := &hooks.LLMHookResponse{
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		llmResp, decision, err = a.hooks.AfterLLM(ctx, llmResp)
		if err != nil {
			return "", fmt.Errorf("hook error: %w", err)
		}
		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// No tool calls - return the response
		if len(resp.ToolCalls) == 0 {
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   content,
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			a.endCortexTurn()

			return redact.RedactIfEnabled(resp.Content, a.secretRedaction), nil
		}

		// 空名工具调用（provider 解析丢字段 / 模型吐坏响应）必须**先**记账：
		// 下面会把它们从待执行列表里过滤掉，但过滤不能连循环检测一起过滤。
		// 此前这条入口在 provider 持续吐空名调用时会静默返回空回答
		// （resp.Content 通常也是空的），既没有 error、也没有日志痕迹，
		// 更不会触发任何阈值判定 —— 与另三条入口的行为不一致。
		a.recordToolCallsForLoop(resp.ToolCalls)
		loopDetected, loopReason := a.detectToolLoop()

		// Tool call loop detection - track tool calls more precisely
		// First, filter out empty tool calls and ensure all have IDs
		validToolCalls := make([]types.ToolCall, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			name := tc.GetToolName()
			if name != "" {
				// Generate ID if empty
				if tc.ID == "" {
					tc.ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
				}
				validToolCalls = append(validToolCalls, tc)
			} else {
				log.Debugf("[TOOL] skipping empty tool call in response (ID: %s)", tc.ID)
			}
		}

		// 响应里带着工具调用、但全都是坏的时候**不能**按"没有工具调用"收尾：
		// 此前这里直接 `return resp.Content`，而这类响应的 Content 通常也是空的
		// ⇒ 用户收到一个空回复、日志上没有任何痕迹。原样交给
		// executeToolsWithHooks，由它落一条"空工具调用"的错误结果进历史
		// （模型下一轮能看见），循环阈值负责兜底收口。
		if len(validToolCalls) > 0 {
			resp.ToolCalls = validToolCalls
		}

		// 工具循环判定走共用实现（四条入口同一套阈值，见 detectToolLoop）。
		if loopDetected {
			log.Warnf("[Agent] tool call loop detected: %s (turn %d)", loopReason, a.iterationCount)
			summaryText, finalErr := a.concludeAfterToolLoop(ctx, resp.Content)
			if finalErr != nil {
				return "", fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			a.endCortexTurn()
			return redact.RedactIfEnabled(summaryText, a.secretRedaction), nil
		}

		// Execute tools and add results to history
		toolResults, execErr := a.executeToolsWithHooks(ctx, resp.ToolCalls)
		if execErr != nil {
			lastErr = execErr
			a.Emit(bus.EventKindToolError, execErr.Error())
			// Continue to add messages - toolResults may contain partial results
		}

		// Record tool calls for self-reflection analysis
		if a.reflector != nil {
			for _, tc := range resp.ToolCalls {
				toolName := tc.GetToolName()
				result, ok := toolResults[tc.ID]
				success := ok && result.Err == nil
				duration := time.Duration(0)
				if ok {
					duration = result.Execution
				}
				a.reflector.RecordToolCall(toolName, success, duration)
			}
		}

		// Add assistant message with tool_calls
		// 同样应用 maxMsgLen 截断（防止思考文本无上限膨胀）
		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   utils.TruncateDetailed(llmResp.Content, a.maxMsgLen),
			ToolCalls: resp.ToolCalls,
			Timestamp: time.Now(),
		})

		// Add tool results - ensure every tool_call has a corresponding tool message
		for _, tc := range resp.ToolCalls {
			tcID := tc.ID
			if tcID == "" {
				tcID = "call_unknown"
			}

			var toolErr error
			var resultContent string
			if result, ok := toolResults[tcID]; ok {
				if result.Err != nil {
					resultContent = utils.ErrTruncateDetailed(fmt.Sprintf("Error: %v", result.Err), result.Name+"_error", a.maxMsgLen)
					toolErr = result.Err
				} else {
					resultContent = utils.TruncateDetailed(result.Content, a.maxMsgLen)
				}
			} else {
				// No result found for this tool call
				if execErr != nil {
					resultContent = utils.TruncateDetailed(fmt.Sprintf("Error: %v", execErr), a.maxMsgLen)
					toolErr = execErr
				} else {
					resultContent = "Error: No result returned for tool call"
				}
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: tcID,
				Timestamp:  time.Now(),
			})

			// Smart recovery: provide guidance for failed tools
			if a.smartRecovery != nil && toolErr != nil {
				toolName := tc.GetToolName()
				errInfo := a.smartRecovery.AnalyzeToolError(toolName, toolErr, nil)
				if errInfo != nil {
					a.smartRecovery.RecordFailure(toolName, errInfo)
					recoveryPrompt := a.smartRecovery.GetRecoveryPrompt(errInfo)
					if recoveryPrompt != "" {
						a.history = append(a.history, provider.Message{
							Role:    "user",
							Content: recoveryPrompt,
						})
					}

					// Record blocker for reflection — but only for non-transient
					// failures. Transient failures (timeouts, rate limits, 5xx)
					// reflect environment conditions, not a flawed approach, and
					// must not be encoded as permanent lessons (Hermes #6051).
					// Tool-level error strings can also wrap the real HTTP status
					// (e.g. downstream provider 400/401/402/429 embedded by the
					// tool adapter). Extract so classification lands on the
					// deterministic status-code switch instead of failing back
					// to FailoverUnknown → Retryable=true.
					status := retry.ExtractStatusCode(toolErr.Error())
					classified := a.errorClassifier.Classify(toolErr, status, a.provider.Name(), "")
					// Permanent / non-retryable tool errors must not be replayed
					// by the outer loop. strategy.Abort covers our explicit
					// classifier table; the classified.Retryable check is the
					// belt-and-suspenders so future Retryable=false reasons
					// never silently burn turns until the escalation detector
					// fires with "reason=unknown" (which is the exact symptom
					// this fix series addresses).
					strategy := retry.GetRecoveryStrategy(classified, 1)
					if strategy.Abort || (classified != nil && !classified.Retryable) {
						a.Emit(bus.EventKindToolError, toolErr.Error())
						return "", fmt.Errorf("non-retryable tool error: %w", toolErr)
					}
					if strategy.Delay > 0 {
						time.Sleep(strategy.Delay)
					}
					if a.reflector != nil && !classified.IsTransient() {
						a.reflector.RecordBlocker(fmt.Sprintf("%s: %s", toolName, errInfo.RootCause))
					}

					// Repeated-failure escalation for tools: halt with a structured
					// diagnostic instead of looping on the same failing tool call.
					if a.failureDetector != nil {
						if esc := a.failureDetector.Record(classified, toolName, errInfo.ErrorMessage); esc != nil {
							a.Emit(bus.EventKindToolError, esc.Error())
							return "", esc
						}
					}
				}
			} else if a.smartRecovery != nil && toolErr == nil {
				a.smartRecovery.RecordSuccess(tc.GetToolName())
				// A successful call resets this tool's failure streak so a later
				// unrelated failure does not unfairly trigger escalation.
				if a.failureDetector != nil {
					a.failureDetector.ResetTool(tc.GetToolName())
				}
			}
		}
		a.endTurnTiming()

		// Truncate history to prevent overflow
		a.truncateHistory()
	}

	// Try to get a summary from the LLM before giving up
	a.history = append(a.history, provider.Message{
		Role:      "user",
		Content:   "You have reached the maximum number of turns. Please provide a brief summary of what you accomplished and what remains incomplete. Do NOT call any more tools.",
		Timestamp: time.Now(),
	})
	if ctx.Err() == nil {
		if finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages()); finalErr == nil {
			turnSummary := wrapLLMReasoning(finalResp.ReasoningContent, finalResp.Content)
			if turnSummary != "" {
				a.history = append(a.history, provider.Message{
					Role:      "assistant",
					Content:   utils.TruncateDetailed(turnSummary, a.maxMsgLen),
					Timestamp: time.Now(),
				})
				return redact.RedactIfEnabled(turnSummary, a.secretRedaction), nil
			}
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", a.maxTurnsExhaustedError()
}

// StreamHandler is called for each streaming chunk
type StreamHandler func(content string, done bool)

// RunConversationStream runs a streaming conversation
func (a *Agent) RunConversationStream(ctx context.Context, input string, handler StreamHandler) error {
	return a.RunConversationStreamWithMedia(ctx, input, nil, handler)
}

// RunConversationStreamWithMedia runs a streaming conversation with multimodal input support.
// If contentParts is provided, it takes priority over plain text input.
func (a *Agent) RunConversationStreamWithMedia(ctx context.Context, input string, contentParts []types.ContentPart, handler StreamHandler) error {
	// 回合收尾钩子：defer 保证正常返回、报错、被取消三条路径都会通知观察者。
	// 观察者（server 侧的 TurnFileOpTracker）据此在回合结束时落库"本轮变更的
	// 文件"——回合已与 SSE 连接解耦，不能只依赖当前恰好连着的那条连接。
	defer notifyTurnFinished(ctx)

	// 回合开始：清零循环检测计数。web 聊天与 bot 流式都走这条入口，且每个
	// 用户消息是一次独立的回合 —— 少了这一步，会话里任何工具累计用过
	// sameToolLimit 次之后，后续每一轮的工具调用都会被判成死循环丢弃
	// （症状：模型只说要做什么、永远不动手）。
	a.resetToolLoopCounters()

	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Cortex: inject memory context and adjust behavior.
	// IsEnabled() matters: a Manager created with cortex disabled is a shell
	// with nil subsystems, and touching it here panicked (Connection lost).
	if a.cortexManager != nil && a.cortexManager.IsEnabled() {
		a.cortexManager.OnUserMessage(input)

		// Inject memory and user context into system prompt
		if a.iterationCount == 0 {
			if memCtx := a.cortexManager.GetPromptContext(); memCtx != "" {
				a.AddSystemContext("[Memory Context]\n" + memCtx)
			}
			if userCtx := a.cortexManager.GetUserContext(); userCtx != "" {
				a.AddSystemContext("[User Profile]\n" + userCtx)
			}
		}
	}

	// Skip sub-task delegation when contentParts are present (e.g., multimodal input with images)
	if len(contentParts) == 0 {
		// Auto sub-task delegation for complex tasks
		if delegated, result, err := a.trySubTaskDelegation(ctx, input); delegated {
			if err != nil {
				handler(fmt.Sprintf("\nError delegating sub-tasks: %v\n", err), true)
				return err
			}
			// Stream the delegation result
			handler(fmt.Sprintf("\n(Task Auto-Delegated to Sub-Task Executor)\n"), false)
			handler(result, false)
			handler("\n\n[Synthesizing final response...]\n\n", false)

			// Run synthesis via streaming
			return a.RunConversationStream(ctx,
				fmt.Sprintf(`I have completed the sub-tasks for the task. The sub-tasks execution results are:

%s

Please provide a comprehensive, well-structured final response based on these sub-task results.`, result),
				handler)
		}
	}

	// Build user message - use content parts if available, otherwise fall back to plain text
	userMsg := provider.Message{
		Role: "user",
	}
	if len(contentParts) > 0 {
		userMsg.ContentParts = contentParts
	} else {
		userMsg.Content = utils.TruncateDetailed(input, a.maxMsgLen)
	}
	userMsg.Timestamp = time.Now()
	a.history = append(a.history, userMsg)

	// Truncate history to prevent overflow
	a.truncateHistory()

	// P0-1: 新用户输入进入时召回动态记忆（turn 内稳定，出站时注入）
	a.prepareDynamicMemory(input)

	var lastErr error
	for a.iterationCount = 0; a.iterationCount < a.maxTurns; a.iterationCount++ {
		// Check if context was cancelled (user pressed /stop) or expired
		// (turn deadline reached). Distinguish the two so the user sees why.
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				handler(fmt.Sprintf("\n\n[⏱ Turn timed out after %d turn(s)]\n", a.iterationCount), false)
			} else {
				handler("\n\n[⏹ Stopped by user]\n", false)
			}
			handler("", true)
			return fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount, ctx.Err())
		default:
		}

		// Deadline 感知：剩余时间不足以再完成一轮完整迭代时优雅收尾，
		// 进度 checkpoint 落盘后经 handler 呈现给用户。
		if !a.canStartAnotherTurn(ctx) {
			handler("\n"+a.gracefulDeadlineFinish(input)+"\n", false)
			handler("", true)
			return nil
		}
		a.beginTurnTiming()

		// Cortex: freeze snapshot at turn start
		if a.cortexManager != nil {
			a.cortexManager.OnTurnStart()
		}

		// Check steering limits
		if a.iterationCount >= a.maxIterations {
			return fmt.Errorf("exceeded maximum iterations (%d)", a.maxIterations)
		}
		if a.maxTokenBudget > 0 && a.tokenUsage >= a.maxTokenBudget {
			return fmt.Errorf("exceeded token budget (%d)", a.maxTokenBudget)
		}

		// Emit turn start event
		a.Emit(bus.EventKindTurnStart, map[string]interface{}{
			"turn": a.iterationCount,
		})

		// Notify handler that a new turn is starting (for multi-turn conversations with tool calls)
		if a.iterationCount > 0 {
			handler("\n>>>TURN_START<<<\n", false)
		}

		// 引导注入：排水本迭代积累的用户引导并入历史（见 guide.go）。
		// 必须在 buildLLMMessages 之前，保证本次 LLM 调用就能看到；
		// 迭代 0 时引导会并入刚追加的本回合输入（合并策略见 applyGuides）。
		a.drainGuidesIntoHistory()

		// 上下文压缩：与另外三条循环同位置（buildLLMMessages 之前）。
		// web 聊天带图走的就是这条流式媒体循环，此前同样漏了压缩。
		a.maybeCompressContext()

		// Build LLM request. buildLLMMessages strips <think> reasoning trails
		// from the outbound copy — see stripThinkContent for why.
		req := &hooks.LLMHookRequest{
			Provider: a.provider.Name(),
			Model:    "",
			Messages: a.buildLLMMessages(),
			Tools:    a.tools,
		}

		// Call BeforeLLM hooks
		req, decision, err := a.hooks.BeforeLLM(ctx, req)
		if err != nil {
			return fmt.Errorf("hook error: %w", err)
		}
		if decision.Action == hooks.HookActionStop {
			return fmt.Errorf("hook stopped: %s", decision.Reason)
		}
		if decision.Action == hooks.HookActionReject {
			return fmt.Errorf("hook rejected: %s", decision.Reason)
		}

		// Defensive: enforce message alternation before sending to the
		// provider — same as the non-streaming loop. Strict providers
		// (zhipu/GLM error 1214, etc.) reject malformed histories (two user
		// messages in a row after injected recovery prompts, tool results that
		// lost their assistant tool_call during an aborted turn) with opaque
		// 400 errors on EVERY retry, burning the whole turn budget.
		if violations := ValidateMessageAlternation(req.Messages); len(violations) > 0 {
			sample := make([]string, 0, 3)
			for i, v := range violations {
				if i >= 3 {
					break
				}
				sample = append(sample, fmt.Sprintf("#%d(%s):%s", v.Index, v.Role, v.Reason))
			}
			log.Warnf("[Agent:Stream] sanitizing %d message-alternation violation(s) before LLM call (sample: %s)",
				len(violations), strings.Join(sample, " | "))
			before := len(req.Messages)
			req.Messages = SanitizeMessageHistory(req.Messages)
			after := len(req.Messages)
			if after < before {
				log.Debugf("[Agent:Stream] message-alternation repair dropped %d message(s) (%d -> %d)", before-after, before, after)
			}
		}

		// Try streaming first
		var fullContent string
		var toolCalls []types.ToolCall
		streamed := false

		// Reasoning/thinking process state: wrap reasoning_content in <think> tags
		// so the frontend ReasoningContent component can parse and display it.
		// fullContent includes <think> tags to preserve the thinking in history.
		var reasoningStarted, thinkClosed bool
		var accumulatedReasoning strings.Builder

		// reasoningEmitted 记录"本轮的 <think> 标签是否真的推给过客户端"。
		// handler 必须以单个 chunk 为单位保持自洽：要么整块推、要么整块不推，
		// 绝不能把 "<think>" 推出去、却把内容留在本地——那正是 10-04 事故里
		// 前端"一直转圈 / 显示一个空思考块"的形态（推送层 addPendingThink）。
		var reasoningEmitted bool

		// buildStreamHandlerContent wraps reasoning content with <think> markers
		// and transitions to normal content with </think> closing tag.
		//
		// contentNeutralizer / reasoningNeutralizer 负责把**模型自己写出的**
		// <think> 标签从推送流里摘掉（跨 chunk 的安全网，见
		// thinkStreamNeutralizer）。包装方补的结构标签不受影响。
		contentNeutralizer := &thinkStreamNeutralizer{}
		reasoningNeutralizer := &thinkStreamNeutralizer{}

		buildStreamHandlerContent := func(resp *provider.StreamResponse) string {
			handlerContent := ""
			if resp.ReasoningContent != "" {
				// 原文照常进账（finalizeFullContent 依赖完整 reasoning），
				// 只有推给客户端的那份需要暂存/中和。
				accumulatedReasoning.WriteString(resp.ReasoningContent)
				if text := reasoningNeutralizer.push(resp.ReasoningContent); text != "" {
					if !reasoningStarted {
						handlerContent += "<think>"
						reasoningStarted = true
					}
					handlerContent += text
					reasoningEmitted = true
				}
			}
			if resp.Content != "" {
				if text := contentNeutralizer.push(resp.Content); text != "" {
					if reasoningStarted && !thinkClosed {
						handlerContent += "</think>\n"
						thinkClosed = true
						reasoningEmitted = true
					}
					handlerContent += text
				}
			}
			return handlerContent
		}

		// flushStreamNeutralizers 在流收尾时吐出两个中和器的暂存尾部，
		// 保证被跨 chunk 暂存的那几个字符不会留在缓冲区里。
		flushStreamNeutralizers := func() string {
			handlerContent := ""
			if text := reasoningNeutralizer.flush(); text != "" {
				if !reasoningStarted {
					handlerContent += "<think>"
					reasoningStarted = true
				}
				handlerContent += text
				reasoningEmitted = true
			}
			if text := contentNeutralizer.flush(); text != "" {
				if reasoningStarted && !thinkClosed {
					handlerContent += "</think>\n"
					thinkClosed = true
				}
				handlerContent += text
			}
			return handlerContent
		}

		// finalizeFullContent constructs fullContent with <think> tags from
		// the Done chunk's accumulated reasoning and content.
		//
		// 关键不变量：reasoning **绝不能**在缺正文时被当作正文顶上去。
		//
		// 事故背景（10-04，DeepSeek 长会话"反复询问用户"）：DeepSeek 等
		// reasoning 模型在部分轮次把全部产出放在 reasoning_content、content
		// 留空。旧实现走 `else if reasoning != ""` 分支，把 accumulatedReasoning
		// 直接拼成 fullContent——于是落库的 assistant 消息变成一段**没有闭合
		// 标签、也没有实际回答**的 "<think>User wants to ..."，而 fullContent
		// 里累积的 reasoning 片段还会与前面已拼过的内容重复。
		//
		// 后果是致命的：历史里看不到"这一步已经做完了"，模型下一轮读到自己
		// 残缺的历史只能**重新推导、重新确认**，表现出来就是同一个简单任务
		// 反复询问用户、几十轮停不下来。已确认线上会话 49 条 assistant 中
		// 29 条是这种未闭合的 reasoning 残留。
		//
		// 正确做法：只在 content 非空时以 "<think>R</think>\n" + content 落库；
		// content 为空就**保持为空**，让上层按"无正文"处理（丢弃或补占位），
		// 而不是拿思考过程冒充回答。
		finalizeFullContent := func(resp *provider.StreamResponse) {
			// delta 累积的正文（不含 reasoning）。buildStreamHandlerContent
			// 把 reasoning 写进 handlerContent 但**不**写进 fullContent，
			// 因此这里 fullContent 就是纯正文。
			body := fullContent
			if resp.Content != "" {
				body = resp.Content
			}
			reasoning := neutralizeThinkTags(accumulatedReasoning.String())
			if reasoning == "" {
				fullContent = body
				return
			}
			if strings.TrimSpace(body) == "" {
				// 无正文：不拿 reasoning 冒充回答。留空交由上层判定。
				// 本轮 reasoning 也**必须**从客户端可见输出里撤掉——只推给
				// 前端就作数的话，用户会看到一个只转圈、没有内容的"思考中"
				// 气泡，而模型其实什么都没说。
				fullContent = ""
				// 撤销本轮已经推出去的 <think> 开头（尚未闭合）。
				// false 与后续 err==nil && 被裁剪路径的 _finalFlush 由上层负责清理。
				handler(redact.RedactIfEnabled("", a.secretRedaction), false)
				log.Warnf("[Agent:Stream] turn produced reasoning only (no content) — suppressed reasoning from client output")
				return
			}
			fullContent = "<think>" + reasoning + "</think>\n" + body
		}

		// Check if provider supports streaming
		type streamer interface {
			StreamWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}, handler provider.StreamHandler) error
		}
		type simpleStreamer interface {
			Stream(ctx context.Context, messages []provider.Message, handler provider.StreamHandler) error
		}

		if st, ok := a.provider.(streamer); ok && len(a.tools) > 0 {
			// Streaming with tools
			err = st.StreamWithTools(ctx, req.Messages, req.Tools, func(resp *provider.StreamResponse) {
				if resp.Error != nil {
					lastErr = resp.Error
					return
				}
				// GLM echoes the zero-width placeholder from its own history;
				// strip it before the chunk reaches fullContent or the UI.
				// JSON-decoded chunks always contain complete runes, so
				// per-chunk rune removal is boundary-safe.
				resp.Content = provider.StripZeroWidth(resp.Content)
				if resp.Done {
					// 先把中和器里跨 chunk 暂存的尾部**单独推给客户端**，再走
					// finalizeFullContent：这段文本属于本轮已经产生、应当可见的
					// 输出，必须先于 finalizeFullContent 内部的"reasoning-only
					// 撤销"（handler("", false)）到达，否则撤销会把已推的 <think>
					// 关掉、而暂存尾部又被补在后面，前端看到错乱的思考块。
					if pending := flushStreamNeutralizers(); pending != "" {
						handler(redact.RedactIfEnabled(pending, a.secretRedaction), false)
					}
					// Done chunk 携带的 Content 语义因 provider 而异：
					// - stream.go/anthropic.go: 完整累积内容（覆盖正确）
					// - perplexity/gemini/wenxin: 空字符串（覆盖会清空已累积内容）
					// 仅在非空时覆盖，空时保留已累积的内容
					finalizeFullContent(resp)
					toolCalls = resp.ToolCalls
					for i := range toolCalls {
						if toolCalls[i].ID == "" {
							toolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
						}
						toolCalls[i].Normalize()
					}
					// Track token usage from final stream chunk（含 cache 命中量）
					a.trackUsage(&provider.ChatResponse{Usage: resp.Usage})
					handlerContent := ""
					if reasoningStarted && !thinkClosed {
						handlerContent += "</think>\n"
						thinkClosed = true
					}
					handler(redact.RedactIfEnabled(handlerContent, a.secretRedaction), resp.Done)
				} else {
					fullContent += resp.Content
					handlerContent := buildStreamHandlerContent(resp)
					if handlerContent != "" {
						handler(redact.RedactIfEnabled(handlerContent, a.secretRedaction), resp.Done)
					}
				}
			})
			if err == nil {
				streamed = true
				// reasoning-only 轮次：正文为空、但有 <think> 已经推给客户端。
				// finalizeFullContent 已经用空 chunk 把未闭合的 think 块抹掉，
				// 这里只需记账——不 fallback 非流式（非流式同样只有 reasoning，
				// 再问一次模型只会放大重复），由上层 decision 进入下一轮。
				if reasoningEmitted && fullContent == "" {
					log.Warnf("[Agent:Stream] streaming turn emitted reasoning without content (suppressed)")
				}
				// 流式成功但内容为空且无工具调用，说明流异常（如网络中断导致提前结束）
				// 标记为未流式成功，进入 fallback 非流式重试
				if fullContent == "" && len(toolCalls) == 0 && !reasoningEmitted {
					log.Warnf("[Agent:Stream] Stream succeeded but empty content, falling back to non-streaming")
					streamed = false
					lastErr = fmt.Errorf("stream returned empty content")
				}
				// Fallback: retry with non-streaming ChatWithTools ONLY when the
				// content looks like a tool-call payload the stream parser failed
				// to extract. Unconditionally retrying every text-only answer
				// wasted a second LLM call per final response and re-prompted a
				// deliberating model on the same history — a repetition-loop
				// amplifier.
				if streamed && len(toolCalls) == 0 && looksLikeUnparsedToolCall(fullContent) {
					log.Debugf("[WARN] Stream returned no tool calls, falling back to ChatWithTools")
					type openAIlikeFallback interface {
						ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
					}
					if oa, ok := a.provider.(openAIlikeFallback); ok {
						nonStreamResp, nsErr := oa.ChatWithTools(ctx, req.Messages, req.Tools)
						if nsErr == nil && len(nonStreamResp.ToolCalls) > 0 {
							log.Debugf("[WARN] ChatWithTools returned %d tool calls (stream parser bug)", len(nonStreamResp.ToolCalls))
							toolCalls = nonStreamResp.ToolCalls
							fullContent = nonStreamResp.Content
							// Track usage from fallback response
							a.trackUsage(nonStreamResp)
						}
					}
				}
			} else {
				lastErr = err
				// Dead context: abort instead of falling into the non-streaming
				// fallback which would fail identically.
				if cerr := ctx.Err(); cerr != nil {
					a.Emit(bus.EventKindError, cerr.Error())
					return fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, cerr)
				}
				log.Warnf("[Agent:Stream] StreamWithTools failed: %v (provider=%s, messages=%d, tools=%d)%s",
					err, a.provider.Name(), len(req.Messages), len(a.tools),
					shapeSuffix(req.Messages, err))
				// Repeated-failure escalation: a permanently failing stream
				// (e.g. zhipu 1214 on every attempt) must not burn the whole
				// turn budget before the non-streaming fallback also fails
				// identically each iteration.
				if a.failureDetector != nil {
					var classified *retry.ClassifiedError
					if a.errorClassifier != nil {
						status := retry.ExtractStatusCode(err.Error())
						classified = a.errorClassifier.Classify(err, status, a.provider.Name(), "")
						// Permanent / non-retryable failure: surface the raw
						// error immediately instead of letting the stream
						// path burn turns until the failure detector kicks
						// in (the stream-with-tools failure path previously
						// had NO strategy check at all).
						strategy := retry.GetRecoveryStrategy(classified, 1)
						if strategy.Abort || (classified != nil && !classified.Retryable) {
							if isMessagesFormatError(err) {
								log.Warnf("[Agent:Stream] permanent message-format rejection: %s", messageShapeSummary(req.Messages))
							}
							a.sanitizeHistory()
							handler("", true)
							return fmt.Errorf("non-retryable error: %w (messages shape: %s)", err, messageShapeSummary(req.Messages))
						}
						if strategy.Delay > 0 {
							time.Sleep(strategy.Delay)
						}
					}
					if esc := a.failureDetector.Record(classified, "", err.Error()); esc != nil {
						a.Emit(bus.EventKindError, esc.Error())
						a.sanitizeHistory()
						handler("", true)
						return fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, esc)
					}
				}
			}
		} else if ss, ok := a.provider.(simpleStreamer); ok {
			// 与上面 StreamWithTools 分支逐字一致的 reasoning 处理：simple 路径
			// 按构造只会在没有注册工具时命中，但**同一条聊天入口**（web / bot 流式）
			// 在会话早期就可能落到这里，两个分支的行为必须等价——否则"没有工具时
			// 正常、注册了工具就出问题"这类入口不等价缺陷会再次出现。
			err = ss.Stream(ctx, req.Messages, func(resp *provider.StreamResponse) {
				if resp.Error != nil {
					lastErr = resp.Error
					return
				}
				// GLM echoes the zero-width placeholder from its own history;
				// strip it before the chunk reaches fullContent or the UI.
				// JSON-decoded chunks always contain complete runes, so
				// per-chunk rune removal is boundary-safe.
				resp.Content = provider.StripZeroWidth(resp.Content)
				if resp.Done {
					// 先把中和器暂存的尾部单独推出去，理由同 StreamWithTools 分支
					// （必须早于 finalizeFullContent 内部的 reasoning-only 撤销）。
					if pending := flushStreamNeutralizers(); pending != "" {
						handler(redact.RedactIfEnabled(pending, a.secretRedaction), false)
					}
					// Done chunk 的 Content 可能为空（perplexity/gemini/wenxin），
					// 仅在非空时覆盖，避免清空已累积的内容
					finalizeFullContent(resp)
					// Track token usage from final stream chunk（含 cache 命中量）
					a.trackUsage(&provider.ChatResponse{Usage: resp.Usage})
					// Close think tag if still open — 只在 <think> 真的推给过
					// 客户端时才补闭合标签，否则会凭空产生一个 "</think>"。
					handlerContent := ""
					if reasoningStarted && !thinkClosed && reasoningEmitted {
						handlerContent += "</think>\n"
						thinkClosed = true
					}
					handler(redact.RedactIfEnabled(handlerContent, a.secretRedaction), resp.Done)
				} else {
					fullContent += resp.Content
					handlerContent := buildStreamHandlerContent(resp)
					if handlerContent != "" {
						handler(redact.RedactIfEnabled(handlerContent, a.secretRedaction), resp.Done)
					}
				}
			})
			if err == nil {
				streamed = true
				// reasoning-only 轮次：见 StreamWithTools 分支同名说明。
				if reasoningEmitted && fullContent == "" {
					log.Warnf("[Agent:Stream] simple-stream turn emitted reasoning without content (suppressed)")
				}
				// 流式成功但内容为空，进入 fallback 非流式重试
				if fullContent == "" && !reasoningEmitted {
					log.Warnf("[Agent:Stream] Simple stream succeeded but empty content, falling back")
					streamed = false
					lastErr = fmt.Errorf("stream returned empty content")
				}
			} else {
				lastErr = err
				if cerr := ctx.Err(); cerr != nil {
					a.Emit(bus.EventKindError, cerr.Error())
					return fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, cerr)
				}
			}
		}

		// Fall back to non-streaming if streaming failed
		if !streamed {
			var resp *provider.ChatResponse
			type openAIlike interface {
				ChatWithTools(ctx context.Context, messages []provider.Message, tools []map[string]interface{}) (*provider.ChatResponse, error)
			}
			if oa, ok := a.provider.(openAIlike); ok && len(a.tools) > 0 {
				log.Infof("[Agent:Stream] Fallback ChatWithTools: provider=%s, messages=%d, tools=%d",
					a.provider.Name(), len(req.Messages), len(a.tools))
				resp, err = oa.ChatWithTools(ctx, req.Messages, req.Tools)
			} else {
				resp, err = a.provider.Chat(ctx, req.Messages)
			}

			if err != nil {
				lastErr = err
				// Dead context: no point sanitizing and looping — abort now.
				if cerr := ctx.Err(); cerr != nil {
					a.Emit(bus.EventKindError, cerr.Error())
					return fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, cerr)
				}
				a.Emit(bus.EventKindError, err.Error())
				// Repeated-failure escalation: sanitizeHistory+continue on a
				// permanent error (e.g. zhipu 1214) loops until maxTurns with
				// two API calls per iteration. Halt after the detector's
				// threshold instead.
				if a.failureDetector != nil {
					var classified *retry.ClassifiedError
					if a.errorClassifier != nil {
						status := retry.ExtractStatusCode(err.Error())
						classified = a.errorClassifier.Classify(err, status, a.provider.Name(), "")
						strategy := retry.GetRecoveryStrategy(classified, 1)
						if strategy.Abort || (classified != nil && !classified.Retryable) {
							if isMessagesFormatError(err) {
								log.Warnf("[Agent:Stream] permanent message-format rejection (non-stream fallback): %s", messageShapeSummary(req.Messages))
							}
							a.sanitizeHistory()
							handler("", true)
							return fmt.Errorf("non-retryable error: %w (messages shape: %s)", err, messageShapeSummary(req.Messages))
						}
						if strategy.Delay > 0 {
							time.Sleep(strategy.Delay)
						}
					}
					if esc := a.failureDetector.Record(classified, "", err.Error()); esc != nil {
						a.Emit(bus.EventKindError, esc.Error())
						a.sanitizeHistory()
						handler("", true)
						return fmt.Errorf("conversation aborted after %d turn(s): %w", a.iterationCount+1, esc)
					}
				}
				// Clean up orphaned tool messages from history so the next
				// iteration doesn't send an invalid message sequence to the API.
				a.sanitizeHistory()
				handler("", true)
				continue
			}

			// Track token usage from the response
			a.trackUsage(resp)

			// 最终非流式兜底路径：必须和常规 RunConversation 一致地把
			// ReasoningContent 包进 <think> 标签，否则 UI 端
			// ReasoningContent 组件解析不到思考过程（DashScope
			// qwen3.x thinking / zhipu GLM thinking 等都依赖该包裹）。
			wrapped := provider.SanitizeAssistantContent(wrapLLMReasoning(resp.ReasoningContent, resp.Content))
			fullContent = utils.TruncateDetailed(wrapped, a.maxMsgLen)
			toolCalls = resp.ToolCalls
			for i := range toolCalls {
				if toolCalls[i].ID == "" {
					toolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
				}
				toolCalls[i].Normalize()
			}
			handler(redact.RedactIfEnabled(wrapped, a.secretRedaction), true)
		}

		// Call AfterLLM hooks
		llmResp := &hooks.LLMHookResponse{
			Content:   fullContent,
			ToolCalls: toolCalls,
		}
		llmResp, decision, err = a.hooks.AfterLLM(ctx, llmResp)
		if err != nil {
			return fmt.Errorf("hook error: %w", err)
		}

		// Emit LLM response event
		a.Emit(bus.EventKindLLMResponse, map[string]interface{}{
			"content": llmResp.Content,
		})

		// No tool calls - return the response
		if len(toolCalls) == 0 {
			content := provider.SanitizeAssistantContent(utils.TruncateDetailed(llmResp.Content, a.maxMsgLen))
			// reasoning-only 轮次（content 为空、只有 reasoning_content）
			// 不是回答：把它当最终答复收尾就是**静默空回复**——历史里只留
			// 一段没有正文的思考，客户端拿到空串。此时应当直接进入下一轮，
			// 让模型基于（不含该空轮的）历史重新产出。
			//
			// 这正是 10-04 长会话"反复询问用户"的流式侧半边：DeepSeek 在
			// 若干轮把全部产出放进 reasoning_content、content 留空，此前
			// fullContent 会被 reasoning 顶替（finalizeFullContent 旧实现），
			// 落库成未闭合的 "<think>...";修掉顶替之后，这里必须同时改成
			// "不把它当回答"，否则等于把"拿思考冒充回答"换成"直接空回复"。
			if strings.TrimSpace(provider.StripThinkTrails(content)) == "" {
				log.Warnf("[Agent:Stream] reasoning-only turn (no content, no tool calls) — continuing loop (turn %d)", a.iterationCount)
				a.Emit(bus.EventKindLLMResponse, map[string]interface{}{"content": ""})
				handler("", true)
				a.history = append(a.history, provider.Message{
					Role:      "assistant",
					Content:   content,
					Timestamp: time.Now(),
				})
				a.sanitizeHistory()
				continue
			}
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   content,
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// Cortex: feed history + extract (see endCortexTurn)
			a.endCortexTurn()

			return nil
		}

		// 工具循环判定走共用实现（四条入口同一套阈值，见 detectToolLoop）。
		// 流式路径此前完全没有这道保护：工具死循环会一路烧到 maxTurns，最后
		// 只回一句裸的 "exceeded maximum turns"，而模型已经把上下文全花在
		// 重复调用上（web 聊天与 bot 流式都走这里，是影响面最大的缺口）。
		a.recordToolCallsForLoop(toolCalls)
		if loopDetected, loopReason := a.detectToolLoop(); loopDetected {
			log.Warnf("[Agent:Stream] tool call loop detected: %s (turn %d)", loopReason, a.iterationCount)
			summaryText, finalErr := a.concludeAfterToolLoop(ctx, fullContent)
			if finalErr != nil {
				a.sanitizeHistory()
				return fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// Cortex: feed history + extract (see endCortexTurn)
			a.endCortexTurn()

			// 客户端已经看过本轮的流式增量，收口文本必须再推一次才会显示；
			// 沿用增量语义（done 由 runQueue 收尾广播）。
			handler(redact.RedactIfEnabled(summaryText, a.secretRedaction), false)
			return nil
		}

		// Store tool calls for history
		tcs := make([]types.ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			name := tc.GetToolName()
			tcs[i] = types.ToolCall{
				ID:       tc.ID,
				Name:     name,
				Type:     "function",
				Function: tc.Function,
			}
		}

		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			Content:   provider.SanitizeAssistantContent(utils.TruncateDetailed(fullContent, a.maxMsgLen)),
			ToolCalls: tcs,
			Timestamp: time.Now(),
		})

		// Execute tools with hooks
		// First, notify the handler about each tool call starting
		for _, tc := range toolCalls {
			toolName := tc.GetToolName()
			argsSummary := ""
			if tc.Function.Arguments != "" {
				// Truncate arguments for display (rune-safe)
				argsSummary = utils.TruncateDetailed(tc.Function.Arguments, 200)
			}
			handler(fmt.Sprintf("\n>>>TOOL_START|%s|%s<<<\n", toolName, argsSummary), false)
		}

		results, err := a.executeToolsWithHooks(ctx, toolCalls)
		if err != nil {
			lastErr = err
			a.Emit(bus.EventKindToolError, err.Error())
			for _, tc := range toolCalls {
				errContent := fmt.Sprintf("Error: %v", err)
				a.history = append(a.history, provider.Message{
					Role:       "tool",
					Content:    utils.TruncateDetailed(errContent, a.maxMsgLen),
					ToolCallID: tc.ID,
					Timestamp:  time.Now(),
				})
				toolName := tc.GetToolName()
				handler(fmt.Sprintf("\n>>>TOOL_RESULT_START|%s|false|0s<<<%s\n>>>TOOL_RESULT_END<<<\n", toolName, err), false)
			}
			continue
		}

		// Add results to history
		for _, tc := range toolCalls {
			result := results[tc.ID]
			content := result.Content
			if result.Err != nil {
				content = utils.ErrTruncateDetailed(fmt.Sprintf("Error: %v", result.Err), tc.GetToolName()+"_error", a.maxMsgLen)
			} else {
				content = utils.TruncateDetailed(content, a.maxMsgLen)
			}

			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: tc.ID,
				Timestamp:  time.Now(),
			})
			a.endTurnTiming()

			toolName := tc.GetToolName()
			success := result.Err == nil
			duration := result.Execution
			// Format duration concisely
			durStr := formatDuration(duration)
			handler(fmt.Sprintf("\n>>>TOOL_RESULT_START|%s|%v|%s<<<\n%s\n>>>TOOL_RESULT_END<<<\n",
				toolName, success, durStr, redact.RedactIfEnabled(content, a.secretRedaction)), false)

			// Cortex: record tool call for review/pattern detection
			if a.cortexManager != nil {
				a.cortexManager.Trigger.OnToolCall(toolName, nil)
			}
		}

		// Check context after tool execution
		a.truncateHistory()
		a.Emit(bus.EventKindTurnEnd, nil)

		// Cortex: analyze tool sequence for skill evolution
		if a.cortexManager != nil {
			a.cortexManager.OnTurnEnd()
		}
	}

	a.Emit(bus.EventKindAgentEnd, nil)

	// Cortex: feed history + extract (see endCortexTurn)
	a.endCortexTurn()

	if lastErr != nil {
		// Persisted history must not end malformed for the next turn (bot
		// mode saves ag.GetHistory() after every outcome).
		a.sanitizeHistory()
		return lastErr
	}
	// Max turns exhausted: nothing new to call — sanitize so the stored tail
	// is a legal sequence, then report.
	a.sanitizeHistory()
	return a.maxTurnsExhaustedError()
}

// RunConversationStreamWithOutput runs streaming and returns output builder
func (a *Agent) RunConversationStreamWithOutput(ctx context.Context, input string) (*strings.Builder, error) {
	var output strings.Builder
	err := a.RunConversationStream(ctx, input, func(content string, done bool) {
		output.WriteString(content)
	})
	return &output, err
}

// toolCallIDSeq is a process-wide monotonic counter used to synthesize unique
// IDs for tool calls that arrive without one (see executeToolsWithHooks).
var toolCallIDSeq uint64

// executeToolsWithHooks executes tools with hook support
func (a *Agent) executeToolsWithHooks(ctx context.Context, toolCalls []types.ToolCall) (map[string]ToolCallResult, error) {
	// Inject the agent's session ID into the context so that hooks (e.g. ApprovalHook)
	// can read the real session ID via tool.SessionIDFromContext. Without this, the
	// approval hook falls back to "cli" and cannot match the SSE handler registered
	// under the real session ID, causing approval cards to never appear in the chat.
	a.mu.RLock()
	sessionID := a.session
	a.mu.RUnlock()
	if sessionID != "" {
		ctx = tool.WithSessionID(ctx, sessionID)
	}

	results := make(map[string]ToolCallResult)
	var mu sync.Mutex

	// Filter out empty tool calls (name is empty)
	validToolCalls := make([]types.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		toolName := tc.GetToolName()
		if toolName == "" {
			// Skip empty tool call - this can happen when AI returns malformed response
			log.Debugf("[TOOL] skipping empty tool call (ID: %s)", tc.ID)
			// Add an error result for this empty tool call
			results[tc.ID] = ToolCallResult{
				ID:      tc.ID,
				Name:    "",
				Content: "Error: Empty tool call - no tool name provided",
				Err:     fmt.Errorf("empty tool call: no tool name provided"),
			}
			continue
		}
		validToolCalls = append(validToolCalls, tc)
	}

	if len(validToolCalls) == 0 {
		return results, nil
	}

	// First, ensure all tool calls have an ID (modify in place)
	for i := range validToolCalls {
		if validToolCalls[i].ID == "" {
			// Atomic counter instead of UnixNano: Windows clock granularity
			// is ~15ms, so a tight loop over N id-less tool calls produced
			// DUPLICATE IDs, and the results map (keyed by ID) then collided
			// -- one result was served for every call in the batch.
			validToolCalls[i].ID = fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), atomic.AddUint64(&toolCallIDSeq, 1))
		}
	}

	groups := a.groupToolsForExecution(validToolCalls)

	for _, group := range groups {
		if group.sequential {
			for _, tc := range group.tools {
				select {
				case <-ctx.Done():
					return results, ctx.Err()
				default:
				}
				result := a.executeSingleToolWithHooks(ctx, tc)
				mu.Lock()
				results[tc.ID] = result
				mu.Unlock()
			}
		} else {
			var wg sync.WaitGroup

			// 全局并发上限：信号量限制同时在飞的并行工具数量，防止 LLM 一次
			// 吐出大量调用时打爆下游（网络/进程/文件系统），同时保留吞吐收益。
			sem := make(chan struct{}, a.maxParallelConcurrency())

			for _, tc := range group.tools {
				tc := tc
				wg.Add(1)
				go func() {
					defer wg.Done()
					// Acquire semaphore (ctx-aware): on cancel, record a result
					// so downstream message assembly never sees a missing ID.
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-ctx.Done():
						mu.Lock()
						results[tc.ID] = ToolCallResult{
							ID:   tc.ID,
							Name: tc.GetToolName(),
							Err:  ctx.Err(),
						}
						mu.Unlock()
						return
					}
					result := a.executeSingleToolWithHooks(ctx, tc)
					mu.Lock()
					results[tc.ID] = result
					mu.Unlock()
					// Per-call failure event: keep UI/backend informed without
					// collapsing the whole batch into a single global error.
					if result.Err != nil {
						a.Emit(bus.EventKindToolError, result.Err.Error())
					}
				}()
			}

			wg.Wait()

			// NOTE: do NOT collapse per-call failures into one global error
			// here. Every failing call already carries its own Err in
			// `results`; returning the first error made the callers stamp
			// "Error: <first failure>" onto ALL tool calls in the batch
			// (observed as: 10 parallel todo deletes, one duplicate already-
			// deleted ID failing, and all 10 results reporting that same
			// "todo not found" even though the other 9 succeeded).
		}
	}

	return results, nil
}

// executeSingleToolWithHooks executes a single tool with hooks
func (a *Agent) executeSingleToolWithHooks(ctx context.Context, tc types.ToolCall) ToolCallResult {
	start := time.Now()

	toolName := tc.GetToolName()

	var toolArgs map[string]interface{}
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &toolArgs); err != nil {
			// Malformed arguments JSON on a large tool call almost always
			// means the model hit its per-response output limit mid-JSON
			// (typical for write_file with big `content`). Surface it loudly
			// instead of silently degrading to {"input": ...}, which made
			// the tool fail with an opaque "xxx argument is required".
			log.Warnf("[TOOL] malformed arguments JSON for tool=%s (len=%d): %v — likely truncated by model output limit; args head=%q tail=%q",
				toolName, len(tc.Function.Arguments), err,
				utils.TruncateDetailed(tc.Function.Arguments, 120),
				utils.TailString(tc.Function.Arguments, 120))
			toolArgs = map[string]interface{}{"input": tc.Function.Arguments}
		}
	} else if tc.Arguments != nil {
		toolArgs = tc.Arguments
	} else {
		toolArgs = map[string]interface{}{}
	}

	a.Emit(bus.EventKindToolBefore, map[string]interface{}{
		"tool": toolName,
		"args": toolArgs,
	})

	callReq := &hooks.ToolCallHookRequest{
		ToolName: toolName,
		ToolArgs: toolArgs,
	}
	callReq, decision, err := a.hooks.BeforeTool(ctx, callReq)
	if err != nil {
		return ToolCallResult{
			ID:      tc.ID,
			Name:    toolName,
			Content: fmt.Sprintf("Hook error: %v", err),
			Err:     err,
		}
	}
	if decision.Action == hooks.HookActionReject {
		// 调用 AfterTool 以允许 hook 清理资源（例如审批 hook 记录拒绝结果、
		// 释放会话级锁等）。传入一个合成的、表示被拒绝的 result。
		rejectErr := fmt.Errorf("rejected: %s", decision.Reason)
		rejectResp := &hooks.ToolResultHookResponse{
			ToolName: toolName,
			ToolArgs: toolArgs,
			Result:   nil,
			Error:    rejectErr,
		}
		a.hooks.AfterTool(ctx, rejectResp)
		return ToolCallResult{
			ID:      tc.ID,
			Name:    toolName,
			Content: fmt.Sprintf("Rejected by hook: %s", decision.Reason),
			Err:     rejectErr,
		}
	}

	// 工具执行前：把完整参数交给观察者做"写前快照"。此处在 registry.Execute
	// 之前，文件尚未被本工具修改，是快照唯一可靠时机。观察者经 ctx 按请求
	// 注入，nil 时无额外开销；被审批拒绝的工具不会走到这里。
	if obs := toolOpsFromCtx(ctx); obs != nil {
		obs.ToolStarting(ctx, toolName, callReq.ToolArgs)
	}

	result, err := a.registry.Execute(ctx, toolName, callReq.ToolArgs)
	elapsed := time.Since(start)

	// 工具执行完成：把真实成败交给观察者（文件操作统计/写后确认）。
	// 观察者经 ctx 按请求注入，nil 时无额外开销。
	if obs := toolOpsFromCtx(ctx); obs != nil {
		obs.ToolFinished(ctx, toolName, callReq.ToolArgs, err)
	}

	if err != nil {
		log.Debugf("[TOOL] %s error after %v: %v", toolName, elapsed, err)
	} else {
		log.Debugf("[TOOL] %s success after %v", toolName, elapsed)
	}

	content := ""
	if err != nil {
		content = fmt.Sprintf("Error: %v", err)
	} else {
		switch v := result.(type) {
		case string:
			content = v
		case nil:
			content = ""
		default:
			if jsonBytes, jsonErr := json.Marshal(result); jsonErr == nil {
				content = string(jsonBytes)
			} else {
				content = fmt.Sprintf("%v", result)
			}
		}
	}

	resultResp := &hooks.ToolResultHookResponse{
		ToolName:    toolName,
		ToolArgs:    toolArgs,
		Result:      result,
		Error:       err,
		ExecutionMs: elapsed.Milliseconds(),
	}
	resultResp, _, _ = a.hooks.AfterTool(ctx, resultResp)

	a.Emit(bus.EventKindToolAfter, map[string]interface{}{
		"tool":  toolName,
		"error": err,
		"ms":    elapsed.Milliseconds(),
	})

	return ToolCallResult{
		ID:        tc.ID,
		Name:      toolName,
		Content:   content,
		Err:       err,
		Execution: elapsed,
	}
}

// groupToolsForExecution groups tools by whether they can be executed in parallel
func (a *Agent) groupToolsForExecution(toolCalls []types.ToolCall) []toolGroup {
	var parallel []types.ToolCall
	var sequential []types.ToolCall

	for _, tc := range toolCalls {
		toolName := tc.GetToolName()
		if exclusiveTools[toolName] || sequentialTools[toolName] {
			sequential = append(sequential, tc)
		} else {
			parallel = append(parallel, tc)
		}
	}

	var groups []toolGroup
	if len(parallel) > 0 {
		groups = append(groups, toolGroup{tools: parallel, sequential: false})
	}
	if len(sequential) > 0 {
		groups = append(groups, toolGroup{tools: sequential, sequential: true})
	}
	return groups
}

// defaultMaxParallelTools 并行工具执行的默认全局并发上限。
const defaultMaxParallelTools = 4

// WithMaxParallelTools overrides the global concurrency cap for parallel
// tool execution (default 4). Values <= 0 fall back to serial execution.
func WithMaxParallelTools(n int) AgentOption {
	return func(a *Agent) {
		if n < 1 {
			n = 1 // 退化为串行而非无界并发，安全兜底
		}
		a.mu.Lock()
		a.maxParallelTools = n
		a.mu.Unlock()
	}
}

// maxParallelConcurrency 返回当前生效的并行工具并发上限。
func (a *Agent) maxParallelConcurrency() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.maxParallelTools <= 0 {
		return 1
	}
	return a.maxParallelTools
}

// deadlineGracefulMargin 时间余量：剩余时间 < 估算轮次耗时 × 该系数时判定
// "再来一轮大概率跑不完"，触发优雅收尾。
const deadlineGracefulMargin = 1.25

// defaultTurnDuration 无历史样本时的默认单轮耗时估算。
const defaultTurnDuration = 30 * time.Second

// beginTurnTiming 标记一轮开始。每个 agent loop 迭代开头调用。
func (a *Agent) beginTurnTiming() {
	a.mu.Lock()
	a.turnStartTime = time.Now()
	a.mu.Unlock()
}

// endTurnTiming 记录一轮完成耗时，样本保留最近 32 个。
func (a *Agent) endTurnTiming() {
	a.mu.Lock()
	if !a.turnStartTime.IsZero() {
		a.turnDurations = append(a.turnDurations, time.Since(a.turnStartTime))
		if len(a.turnDurations) > 32 {
			a.turnDurations = a.turnDurations[len(a.turnDurations)-32:]
		}
	}
	a.mu.Unlock()
}

// estimateTurnDuration 预估下一轮完整耗时（LLM + 工具执行）：
// 取最近样本的 P90（保守估计），无样本时用默认值 30s。
func (a *Agent) estimateTurnDuration() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	n := len(a.turnDurations)
	if n == 0 {
		return defaultTurnDuration
	}
	sorted := make([]time.Duration, n)
	copy(sorted, a.turnDurations)
	for i := 1; i < n; i++ { // 小样本插入排序足够
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	idx := (n*90 + 99) / 100 // ceil(0.9*n), 范围 [1, n]
	return sorted[idx-1]
}

// canStartAnotherTurn 判断是否还能安全启动一轮新的完整迭代：
// ctx 有 deadline 且剩余时间 < P90轮次耗时 × margin 时返回 false。
func (a *Agent) canStartAnotherTurn(ctx context.Context) bool {
	dl, ok := ctx.Deadline()
	if !ok {
		return true // 无 deadline 约束
	}
	limit := time.Duration(float64(a.estimateTurnDuration()) * deadlineGracefulMargin)
	return time.Until(dl) >= limit
}

// DeadlineCheckpoint 是优雅收尾时落盘的进度快照。
type DeadlineCheckpoint struct {
	Session      string             `json:"session"`
	Task         string             `json:"task"`
	Reason       string             `json:"reason"`
	Completed    int                `json:"completed_turns"`
	ToolCalls    int                `json:"tool_calls"`
	InputTokens  int                `json:"input_tokens"`
	OutputTokens int                `json:"output_tokens"`
	SavedAt      time.Time          `json:"saved_at"`
	LastMessages []CheckpointMsgDTO `json:"last_messages,omitempty"`
}

// CheckpointMsgDTO 历史消息的精简载体（截断内容防巨型文件）。
type CheckpointMsgDTO struct {
	Role       string   `json:"role"`
	Content    string   `json:"content,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
	ToolNames  []string `json:"tool_names,omitempty"`
}

// writeDeadlineCheckpoint 将当前进度写入 ~/.magic/checkpoints/，
// 返回文件路径；失败返回空串并打日志（不中断收尾流程）。
// 内嵌最后 8 条历史精简版，便于恢复现场或人工排查。
func (a *Agent) writeDeadlineCheckpoint(task, reason string) string {
	a.mu.RLock()
	cp := DeadlineCheckpoint{
		Session:      a.session,
		Task:         utils.TruncateDetailed(task, 2000),
		Reason:       reason,
		Completed:    a.iterationCount,
		ToolCalls:    len(a.toolCallHistory),
		InputTokens:  a.inputTokens,
		OutputTokens: a.outputTokens,
		SavedAt:      time.Now(),
	}
	start := len(a.history) - 8
	if start < 0 {
		start = 0
	}
	for _, m := range a.history[start:] {
		dto := CheckpointMsgDTO{Role: m.Role}
		content := m.Content
		if content == "" && len(m.ContentParts) > 0 {
			var parts []string
			for _, p := range m.ContentParts {
				parts = append(parts, p.Type+":...")
			}
			content = strings.Join(parts, ",")
		}
		dto.Content = utils.TruncateDetailed(content, 1500)
		dto.ToolCallID = m.ToolCallID
		for _, tc := range m.ToolCalls {
			dto.ToolNames = append(dto.ToolNames, tc.GetToolName())
		}
		cp.LastMessages = append(cp.LastMessages, dto)
	}
	historyCount := len(a.history)
	a.mu.RUnlock()

	// 用 config.GetMagicHome() 统一解析（GO_MAGIC_HOME → HOME → UserHomeDir），
	// 之前自行 os.UserHomeDir() 会在 Windows 上无视测试设置的 HOME，把
	// checkpoint 写进真实用户目录（GO_MAGIC_HOME 隔离失效）。
	dir := filepath.Join(config.GetMagicHome(), "checkpoints")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warnf("[Agent] checkpoint dir create failed: %v", err)
		return ""
	}
	name := fmt.Sprintf("%s_%s_turn%d.json",
		time.Now().Format("20060102_150405"), SanitizeAgentSlug(cp.Session), cp.Completed)
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(cp, "", "  ")
	if err == nil {
		err = os.WriteFile(path, data, 0o600)
	}
	if err != nil {
		log.Warnf("[Agent] checkpoint write failed: %v", err)
		return ""
	}
	log.Infof("[Agent] checkpoint saved (%d history msgs at cutoff): %s", historyCount, path)
	return path
}

// SanitizeAgentSlug 把 session 名清理为安全文件名片段。
func SanitizeAgentSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "session"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// gracefulDeadlineFinish deadline 不足时的优雅收尾：
// 不再发起 LLM 请求（时间已不够完成一轮），写入 checkpoint 并返回
// 结构化的进度说明，调用方可据此向用户呈现或续跑。
func (a *Agent) gracefulDeadlineFinish(task string) string {
	path := a.writeDeadlineCheckpoint(task, "deadline imminent: remaining time below estimated per-turn duration")
	est := a.estimateTurnDuration()
	a.mu.RLock()
	completed := a.iterationCount
	toolCalls := len(a.toolCallHistory)
	a.mu.RUnlock()
	msg := fmt.Sprintf(
		"[Deadline approaching] Remaining context lifetime is insufficient for another full turn (~%s needed). "+
			"Work stopped gracefully after %d completed turn(s), %d tool call(s). %s",
		formatDuration(est), completed, toolCalls,
		func() string {
			if path == "" {
				return "(checkpoint unavailable)"
			}
			return "Progress saved to checkpoint: " + path
		}(),
	)
	a.Emit(bus.EventKindWarning, map[string]interface{}{
		"reason":        "deadline_graceful_finish",
		"checkpoint":    path,
		"completed":     completed,
		"turn_estimate": est.String(),
	})
	return msg
}

// Reset clears the conversation history
func (a *Agent) Reset() {
	a.history = a.history[:1] // Keep system prompt
	a.tokenUsage = 0
	a.inputTokens = 0
	a.outputTokens = 0
	a.cacheReadTokens = 0
	a.resetToolLoopCounters()
	// Clear the repeated-failure memory so prior-task failures do not poison a
	// new conversation's escalation decisions.
	if a.failureDetector != nil {
		a.failureDetector.Reset()
	}
	// 清理审批 hook 的会话级 skip 列表，避免上个会话跳过的命令
	// 在新会话中继续被静默跳过。
	if a.approvalHook != nil {
		a.approvalHook.ClearAllSessionSkip()
	}
}

// GetHistory returns a copy of the conversation history. The copy is
// returned (not a live slice reference) because a running conversation
// may append to history concurrently from the worker goroutine.
func (a *Agent) GetHistory() []provider.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]provider.Message, len(a.history))
	copy(result, a.history)
	return result
}

// GetTokenStats returns the token usage statistics
func (a *Agent) GetTokenStats() (inputTokens, outputTokens, cacheReadTokens int) {
	return a.inputTokens, a.outputTokens, a.cacheReadTokens
}

// trackUsage accumulates token usage from an LLM response
func (a *Agent) trackUsage(resp *provider.ChatResponse) {
	if resp != nil && resp.Usage != nil {
		a.inputTokens += resp.Usage.PromptTokens
		a.outputTokens += resp.Usage.CompletionTokens
		a.cacheReadTokens += resp.Usage.CacheReadTokens
	}
}

// TokenUsage represents token usage statistics for tracking
type TokenUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
}

// GetTokenUsage returns the usage statistics as a TokenUsage struct
func (a *Agent) GetTokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:     a.inputTokens,
		OutputTokens:    a.outputTokens,
		CacheReadTokens: a.cacheReadTokens,
	}
}

// SetHistory sets the conversation history
// SetHistory replaces the conversation history. Guarded by the agent mutex:
// callers may run concurrently with a worker goroutine executing a turn.
func (a *Agent) SetHistory(history []provider.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = history
}

// dataURLWeight estimates the "size" of a media URL for context accounting.
// For data URLs only the base64 payload counts (the prefix is negligible);
// everything else is counted as-is. This makes multi-MB inline images
// visible to the history budget instead of silently inflating requests.
func dataURLWeight(u string) int {
	if i := strings.Index(u, ","); i >= 0 {
		return len(u) - i - 1
	}
	return len(u)
}

// messageWeight returns the approximate character weight of a history
// message: its text content plus every media payload embedded in
// ContentParts (image/file data URLs). Plain len(Content) accounting is
// blind to base64 media, which lets a few screenshots push a request far
// past the context limit with no truncation ever triggering.
func messageWeight(m provider.Message) int {
	total := len(m.Content)
	for _, p := range m.ContentParts {
		switch p.Type {
		case "image_url", "video_url", "audio_url":
			var u *types.MediaURL
			switch p.Type {
			case "image_url":
				u = p.ImageURL
			case "video_url":
				u = p.VideoURL
			case "audio_url":
				u = p.AudioURL
			}
			if u != nil {
				total += dataURLWeight(u.URL)
			}
		case "file":
			if p.File != nil {
				total += dataURLWeight(p.File.Contents)
			}
		}
	}
	return total
}

// GetHistoryLength returns the current history length in characters
func (a *Agent) GetHistoryLength() int {
	total := 0
	for _, m := range a.history {
		total += messageWeight(m)
	}
	return total
}

// hardCapSlackNum / hardCapSlackDen 是"历史硬上限"相对压缩触发点保留的余量
// （5/4 = 25%）。
//
// 两条路径的语义完全不同：压缩（compressor 的 LLM 摘要，或 agent 内的
// compressHistory）把中段**摘要**成一条能继续接力的记录；而 truncateHistory
// 的字节级路径是把整段 user 块**直接删掉** —— 不生成任何摘要，模型永久失去
// 这段上下文（2026-10-08 "读完就忘"事故的形态）。所以硬截断必须在语义上
// 永远**晚于**压缩触发：只要还有压缩可用，就轮不到它动手。
//
// 但两条阈值的口径本来不同步——压缩触发点是 token 配置
// （agent.compress_threshold_tokens → compressor.ThresholdTokens，按
// 4 字节/token 折算），硬上限却是 NewAIAgent 里固定写死的 200000 字节
// （注释记作 ~50K tokens）。默认配置（32K tokens ⇒ 128K 字节触发点 < 200K
// 上限）下顺序是对的，但这是**隐式**的：管理员把阈值调到 60000（⇒ 240K 字节
// 触发点）就变成"先硬截断、后压缩"，而且那条路径不留摘要。
// historyHardLimit 把"硬上限 ≥ 压缩触发点"从隐式假设变成结构性保证。
const (
	hardCapSlackNum = 5
	hardCapSlackDen = 4
)

// historyHardLimit 返回本回合生效的历史硬上限（字节）。
//
// 恒 ≥ maxTotalLen；当压缩触发点（ThresholdTokens × 4，与
// maybeCompressContext 里 totalChars/4 的估算口径一致）比它更高时，跟着抬起来
// 并留出 hardCapSlack 的余量。compressor 为 nil（未接线压缩的调用方）时退化为
// maxTotalLen 本身，即"硬上限是唯一防线"的老行为。
func (a *Agent) historyHardLimit() int {
	limit := a.maxTotalLen
	if a.compressor != nil && a.compressor.ThresholdTokens > 0 {
		trigger := a.compressor.ThresholdTokens * 4
		if need := trigger * hardCapSlackNum / hardCapSlackDen; need > limit {
			limit = need
		}
	}
	return limit
}

// truncateHistory truncates message history to prevent overflow.
//
// The structural contract here is that the LAST user-role message must
// survive: every public entry point (Chat, ChatWithTools, cortex_integration,
// in-loop tool truncation) appends a fresh user message right before
// calling truncateHistory. Stripping it leaves the trailing tool result
// dangling with no caller, which makes the next provider call look like
// "tool result with no preceding assistant tool_calls" — a structural
// 1214 trigger on Zhipu/GLM and silently broken behaviour elsewhere.
//
// To keep that invariant we always treat the final user-role message and
// every message that follows it as a single protected "tail block" that
// we will NEVER touch (not even individually). Truncation therefore
// proceeds by removing whole EARLIER user blocks — i.e. each user message
// and everything between it and the next user message (assistant tool_calls
// + trailing tool results). Because that path destroys context outright, it
// only ever runs after the summarising compressor (maybeCompressBeforeTruncate
// → compressContext, the threshold-driven LLM-assisted path) has had its turn.
func (a *Agent) truncateHistory() {
	// 用 limit 而不是裸的 a.maxTotalLen：硬上限必须恒 ≥ 压缩触发点，见
	// historyHardLimit 的注释。否则把 agent.compress_threshold_tokens 抬到
	// 50000 以上时，这条"整段删 user 块"的路径会先于压缩触发。
	limit := a.historyHardLimit()
	if limit <= 0 {
		return
	}

	totalLen := a.GetHistoryLength()
	if totalLen < limit {
		return
	}

	// 已经越过硬上限：先让摘要器接管。这里换掉了旧的
	// `compressionEnabled && totalLen > limit*compressionRatio` —— ratio 恒为 0
	// 使该判定恒真（每回合无条件重写历史），而四个非 TUI 入口又从不置
	// compressionEnabled（兜底永不生效），两头都是错的。详见
	// maybeCompressBeforeTruncate 的注释。
	if a.maybeCompressBeforeTruncate() {
		if totalLen = a.GetHistoryLength(); totalLen < limit {
			return
		}
		// 摘要后仍在上限之上（例如某条工具结果单独就顶满了整个预算）：
		// 继续做字节级裁剪。此时历史里至少已经留下一条可接力的摘要，
		// 模型不会像旧行为那样永久失忆。
	}

	systemIdx := -1
	for i, m := range a.history {
		if m.Role == "system" {
			systemIdx = i
			break
		}
	}

	// 系统提示词自身的上限（**字节**）。这里是字节切分点，所以必须落在 UTF-8
	// rune 边界上：裸的 Content[:maxSystemLen] 一旦切在多字节 rune 中间，得到的
	// 就是非法 UTF-8 —— 严格些的 provider 直接 400，宽松的静默替换成 U+FFFD，
	// 系统提示词尾部变成乱码字符还不报错。中文提示词（3 字节/字）必踩。
	const maxSystemLen = 50000
	if systemIdx >= 0 && len(a.history[systemIdx].Content) > maxSystemLen {
		orig := a.history[systemIdx].Content
		truncated := truncateBytesOnRuneBoundary(orig, maxSystemLen)
		if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > maxSystemLen/2 {
			truncated = truncated[:lastNewline]
		}
		truncated += "\n\n[...system prompt truncated...]"
		a.history[systemIdx].Content = truncated
		totalLen -= len(orig) - len(truncated)
		log.Warnf("[Agent] System prompt truncated from %d to %d bytes (maxSystemLen=%d)",
			len(orig), len(truncated), maxSystemLen)
	}

	if totalLen < limit {
		return
	}

	// Locate the protected tail block: the index of the last user message,
	// and from there the boundary every deletion must respect.
	lastUserIdx := -1
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	tailBlockSize := 0
	if lastUserIdx >= 0 {
		for i := lastUserIdx; i < len(a.history); i++ {
			tailBlockSize += messageWeight(a.history[i])
		}
	}
	// If even the protected tail alone fits under the cap, every earlier
	// reduction candidate is by definition larger; there is nothing we can
	// responsibly remove without destroying user context, so we ask the
	// summariser to take over, and otherwise just run the sanitiser.
	if lastUserIdx >= 0 && tailBlockSize >= limit {
		log.Warnf("[Agent] truncateHistory: protected tail block (%d msgs, %d chars) >= history cap=%d — asking the summariser to take over",
			len(a.history)-lastUserIdx, tailBlockSize, limit)
		if a.maybeCompressBeforeTruncate() && a.GetHistoryLength() < limit {
			return
		}
		// Summarisation unavailable or insufficient: fall through and at
		// least run sanitiser so Pass 4/5 mask any structural damage the
		// caller is about to ship.
		a.sanitizeHistory()
		return
	}

	// removeRange deletes [start, end) from a.history, decrements totalLen
	// by the summed content length of that range, and keeps systemIdx in
	// sync so subsequent iterations still skip the head system message.
	removeRange := func(start, end int) {
		if start >= end {
			return
		}
		for k := start; k < end; k++ {
			totalLen -= messageWeight(a.history[k])
		}
		a.history = append(a.history[:start], a.history[end:]...)
		if systemIdx >= start && systemIdx < end {
			systemIdx = -1
		} else if systemIdx >= end {
			systemIdx -= (end - start)
		}
	}

	// Safety net: never run more iterations than the original history
	// length × 2. Before the fix this loop was safe-guarded by
	// `len(a.history) > 1`, but the protected-tail invariant intentionally
	// preserves one user message we are NOT allowed to remove — if we did
	// keep iterating, we could spin forever on a history whose only
	// removable content is already exhausted. The cap is large enough that
	// every legitimate cleanup runs to completion (each iteration deletes
	// at least one message), so reaching it means we genuinely cannot
	// make further progress.
	maxIters := 0
	for i := 0; i < len(a.history); i++ {
		maxIters++
		_ = i
	}
	maxIters *= 2
	if maxIters < 8 {
		maxIters = 8
	}

	deletedAny := false
	for totalLen > limit && len(a.history) > 1 && maxIters > 0 {
		maxIters--

		// Pick the victim index, always skipping a leading system message.
		idx := 0
		if systemIdx == 0 {
			idx = 1
		}
		if idx >= len(a.history) {
			break
		}
		if len(a.history)-1 < lastUserIdx {
			// lastUserIdx fell off the end (only system+protected tail remain
			// in some shrunken form). Re-locate so the protection check below
			// still triggers correctly.
			lastUserIdx = -1
			for i := len(a.history) - 1; i >= 0; i-- {
				if a.history[i].Role == "user" {
					lastUserIdx = i
					break
				}
			}
			if lastUserIdx < 0 {
				break
			}
		}

		role := a.history[idx].Role

		switch role {
		case "tool":
			// Reverse-find the most recent assistant-with-tool-calls that
			// is the caller of these tool results, then strip the entire
			// assistant+tool_calls header AND every trailing tool message
			// that follows it. This keeps tool result↔tool_call IDs paired
			// (no orphan tool result) and is the symmetric counterpart of
			// the user-block removal below.
			foundCaller := false
			for j := idx - 1; j >= 0; j-- {
				if a.history[j].Role == "assistant" && len(a.history[j].ToolCalls) > 0 {
					removeEnd := j + 1
					for removeEnd < len(a.history) && a.history[removeEnd].Role == "tool" {
						removeEnd++
					}
					removeRange(j, removeEnd)
					if lastUserIdx >= j && lastUserIdx < removeEnd {
						// The protected tail got erased by a tool-headed
						// removal. This should not happen because idx is
						// always < lastUserIdx here (idx sits inside an
						// earlier user block), but guard anyway.
						lastUserIdx = -1
						for i := len(a.history) - 1; i >= 0; i-- {
							if a.history[i].Role == "user" {
								lastUserIdx = i
								break
							}
						}
					}
					foundCaller = true
					break
				}
			}
			if foundCaller {
				deletedAny = true
				continue
			}
			// Head-position tool with no caller (already-sanitised history
			// in theory, but be defensive): drop it alone.
			removeRange(idx, idx+1)
			deletedAny = true
			continue

		case "assistant":
			if len(a.history[idx].ToolCalls) > 0 {
				// Discard the assistant+tool_calls header plus any trailing
				// tool results that belong to it.
				removeEnd := idx + 1
				for removeEnd < len(a.history) && a.history[removeEnd].Role == "tool" {
					removeEnd++
				}
				removeRange(idx, removeEnd)
			} else {
				// Plain assistant reply (no tool calls) — safe to drop
				// alone. SanitizeHistory's Pass 4/5 will reconnect any
				// neighbouring damage.
				removeRange(idx, idx+1)
			}
			deletedAny = true
			continue

		case "user":
			// Tail-block protection: if idx is the protected tail user, we
			// cannot remove any further user block. Stop iterating and let
			// the summariser produce a summary-based reduction; when it is
			// not available (or does not help), just sanitise.
			if idx == lastUserIdx {
				if !deletedAny {
					// Nothing has been deleted yet on this pass — a single
					// protected tail block already fills the budget. This
					// is the same edge case as the early exit above but
					// reached after partial cleanup; still no further
					// safe moves available without summarising.
					log.Warnf("[Agent] truncateHistory reached protected tail user without further droppable blocks")
				}
				if a.maybeCompressBeforeTruncate() && a.GetHistoryLength() < limit {
					return
				}
				a.sanitizeHistory()
				return
			}
			// Drop the entire EARLIER user block: idx (the user) plus every
			// assistant/tool/assistant message that follows it until just
			// before the NEXT user message (or until the protected tail,
			// whichever comes first). The protected tail is structurally
			// unreached because idx < lastUserIdx guarantees the search
			// terminates at lastUserIdx.
			end := idx + 1
			for end < len(a.history) && a.history[end].Role != "user" {
				end++
			}
			// Defensive: refuse to cross the protected tail boundary even
			// if idx somehow equals lastUserIdx (handled above, but cheap
			// to double-check).
			if end > lastUserIdx {
				end = lastUserIdx
			}
			removeRange(idx, end)
			deletedAny = true
			continue

		default:
			// Unknown / unmodelled role — drop alone and let sanitiser
			// re-classify on the way out.
			removeRange(idx, idx+1)
			deletedAny = true
			continue
		}
	}

	// If we exited the loop because of the safety cap or because the
	// protected tail made further byte-level removal impossible, try the
	// summarisation path one more time before handing the noisy payload to
	// the provider layer. It is a no-op when the history is below the
	// compressor's own token gate, which is the correct response for tails
	// that are large in bytes but cheap in tokens.
	if totalLen > limit && a.maybeCompressBeforeTruncate() && a.GetHistoryLength() < limit {
		return
	}

	// RC3 (26-turn 1214 root cause): truncateHistory is the #1 producer of
	// malformed message sequences — it slices entire prefixes of the
	// history based on byte length alone, which can (a) drop tool results
	// from the TAIL of a parallel-tool-call block while leaving the
	// assistant+ToolCalls header intact (orphan assistant), (b) drop all
	// leading system+user messages and expose an assistant/tool head, (c)
	// cut in the middle of a tool-call block. These pass through the old
	// Validate + SanitizeMessageHistory *silently* because the validator
	// didn't flag them (RC1/RC2), turning any 26-turn length breach into
	// an immediate provider 1214. Running sanitizeHistory at the END of
	// every truncateHistory applies the Pass 2.5 / Pass 4 / Pass 5 fixes
	// (empty-content placeholders, orphan toolcall stripping, leading role
	// normalisation) BEFORE any caller gets a chance to buildLLMMessages —
	// which is exactly where RC3 says the loop used to go straight into
	// the API call with no pre-sanitize.
	a.sanitizeHistory()
}

// shapeSuffix returns ", shape=..." when the error looks like a permanent
// message-format rejection (1214 family), empty otherwise. Keeps the common
// failure log line compact while still surfacing the payload shape for the
// cases where it matters.
func shapeSuffix(msgs []provider.Message, err error) string {
	if !isMessagesFormatError(err) {
		return ""
	}
	return ", shape=" + messageShapeSummary(msgs)
}

// isMessagesFormatError reports whether err is a provider rejection of the
// message payload shape itself (the Zhipu/GLM 1214 "messages 参数非法" family
// and its English/localized variants, plus generic format rejections from
// other strict providers). These are the errors where surfacing the outbound
// message shape pays off — the payload is bad, and the shape tells us why
// (system-only array, empty/whitespace content, orphan tool block, etc.).
func isMessagesFormatError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "[1214]") ||
		strings.Contains(msg, "api error 1214") ||
		strings.Contains(msg, "错误码 1214") ||
		strings.Contains(msg, "参数非法") ||
		strings.Contains(msg, "messages parameter") ||
		strings.Contains(msg, "the messages parameter is illegal") ||
		strings.Contains(msg, "invalid messages")
}

// messageShapeSummary renders a compact one-line structural profile of a
// message slice without leaking content: role letters with byte lengths,
// tagging messages whose content is empty (∅), whitespace/zero-width-only
// (ws), or part of a tool round-trip (parts/tc/noID). It is embedded into the
// surfaced error on 1214-class permanent aborts so the offending shape is
// visible in the UI/CLI error text itself, not only in server logs.
func messageShapeSummary(msgs []provider.Message) string {
	total := len(msgs)
	if total == 0 {
		return "messages=[] (EMPTY ARRAY)"
	}
	const maxShow = 16
	start := 0
	if total > maxShow {
		start = total - maxShow
	}
	userCnt, sysCnt, toolCnt := 0, 0, 0
	for _, m := range msgs {
		switch m.Role {
		case "user":
			userCnt++
		case "system":
			sysCnt++
		case "tool":
			toolCnt++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "messages=%d[", total)
	for i, m := range msgs {
		if i < start {
			continue
		}
		if i == start && start > 0 {
			b.WriteString("…")
		}
		var ch string
		switch m.Role {
		case "system":
			ch = "s"
		case "user":
			ch = "u"
		case "assistant":
			ch = "a"
		case "tool":
			ch = "t"
		default:
			ch = "?" + m.Role
		}
		cl := len(m.Content)
		flag := ""
		switch {
		case cl == 0 && len(m.ContentParts) == 0:
			flag = "∅"
		case provider.IsEmptyAssistantContent(m.Content):
			flag = "ws"
		}
		if len(m.ContentParts) > 0 {
			flag += fmt.Sprintf("parts%d", len(m.ContentParts))
		}
		if n := len(m.ToolCalls); n > 0 {
			flag += fmt.Sprintf("/tc%d", n)
		}
		if m.Role == "tool" && m.ToolCallID == "" {
			flag += "/noID"
		}
		fmt.Fprintf(&b, "%s%s%d ", ch, flag, cl)
	}
	fmt.Fprintf(&b, "] s=%d u=%d t=%d", sysCnt, userCnt, toolCnt)
	return strings.TrimSpace(b.String())
}

// looksLikeUnparsedToolCall reports whether streamed content appears to be a
// tool-call payload the stream parser failed to extract (JSON carrying
// tool-call markers) rather than a legitimate final answer. Only such content
// justifies the extra non-streaming ChatWithTools retry; retrying on every
// text-only answer doubles latency/tokens and re-prompts a deliberating
// model, amplifying repetition loops.
func looksLikeUnparsedToolCall(content string) bool {
	c := strings.TrimSpace(stripThinkContent(content))
	if c == "" {
		return false
	}
	low := strings.ToLower(c)
	hasName := strings.Contains(low, "\"name\"")
	hasArgs := strings.Contains(low, "\"arguments\"")
	hasToolCall := strings.Contains(low, "\"tool_call") || strings.Contains(low, "<tool_call>")
	if hasToolCall {
		return true
	}
	jsonish := strings.HasPrefix(c, "{") || strings.HasPrefix(c, "[") ||
		strings.HasPrefix(low, "```json") || strings.HasPrefix(c, "```")
	return jsonish && hasName && hasArgs
}

// Repetition-degeneration detection tuning.
const (
	repNGramWords    = 4    // words per shingle
	repNGramMinHits  = 8    // occurrences that indicate degeneration
	repTailScanRunes = 8192 // only scan the tail of long content
	repMinContentLen = 200  // don't bother below this length (runes)
)

// truncateRepetition detects degenerate repetition in the tail of content —
// e.g. a reasoning model stuck re-emitting "look at the args. GO!" dozens of
// times — and cuts the repetitive tail, replacing it with a short marker.
// Near-identical loops (same key phrases recurring with cosmetic variation,
// like escalating exclamation marks) are caught via word 4-gram frequency,
// which exact-match comparison would miss. Returns (truncated content, true)
// when degeneration was detected.
func truncateRepetition(content string) (string, bool) {
	runes := []rune(content)
	if len(runes) < repMinContentLen {
		return content, false
	}
	start := 0
	if len(runes) > repTailScanRunes {
		start = len(runes) - repTailScanRunes
	}
	tailRunes := runes[start:]
	n := len(tailRunes)

	// Tokenize into normalized words with rune offsets (for cutting later).
	type repWord struct {
		s string
		o int // rune offset within tail
	}
	fields := make([]repWord, 0, 256)
	i := 0
	for i < n {
		for i < n && unicode.IsSpace(tailRunes[i]) {
			i++
		}
		if i >= n {
			break
		}
		wStart := i
		for i < n && !unicode.IsSpace(tailRunes[i]) {
			i++
		}
		w := normalizeRepWord(string(tailRunes[wStart:i]))
		if w != "" {
			fields = append(fields, repWord{s: w, o: wStart})
		}
	}
	if len(fields) < repNGramWords*repNGramMinHits {
		return content, false
	}

	// Word 4-gram frequency; locate the degenerate run (a tail region where a
	// shingle recurs back-to-back) and cut inside it, keeping the first two
	// occurrences of the run for context.
	hits := make(map[string][]int)
	for j := 0; j+repNGramWords <= len(fields); j++ {
		var b strings.Builder
		for k := 0; k < repNGramWords; k++ {
			b.WriteString(fields[j+k].s)
			b.WriteByte(' ')
		}
		key := b.String()
		hits[key] = append(hits[key], j)
	}
	cutAt := -1
	for _, idxs := range hits {
		N := len(idxs)
		if N < repNGramMinHits {
			continue
		}
		// Walk backwards from the last occurrence to find the contiguous
		// degenerate run (occurrences separated by no more than a few words
		// of filler count as contiguous).
		runStart := N - 1
		for k := N - 1; k > 0; k-- {
			if idxs[k]-idxs[k-1] > 4*repNGramWords {
				break
			}
			runStart = k - 1
		}
		if N-runStart >= repNGramMinHits {
			off := fields[idxs[runStart+2]].o // 3rd occurrence in the run
			if cutAt == -1 || off < cutAt {
				cutAt = off
			}
		}
	}
	if cutAt == -1 {
		return content, false
	}
	out := string(runes[:start]) + string(tailRunes[:cutAt]) +
		"\n\n[repetitive content truncated by go-magic]"
	return out, true
}

// normalizeRepWord keeps only letters/digits (lowercased) so punctuation
// drift ("GO!" vs "GO!!!") does not break repetition matching. CJK counts as
// letters and survives intact.
func normalizeRepWord(w string) string {
	var b strings.Builder
	for _, r := range w {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// stripThinkContent removes <think>...</think> reasoning blocks (and any
// unterminated <think> tail) from message content. Reasoning trails are kept
// in history for UI display, but feeding them back to the LLM verbatim makes
// reasoning models imitate and progressively amplify their own deliberation,
// which degenerates into repetitive "thinking loops" across turns. Stripping
// them from the outbound request cuts that feedback loop.
//
// Implementation lives in the provider package so non-agent callers (the bot
// group chat broadcasts each member's reply into a shared room log) strip
// with exactly the same rules.
func stripThinkContent(s string) string {
	return provider.StripThinkTrails(s)
}

// thinkPlaceholder is used when stripping would leave an assistant message
// with no tool calls and empty content — some strict providers reject
// assistant messages that are empty without tool calls.
const thinkPlaceholder = "(thinking omitted)"

// buildLLMMessages returns a sanitized copy of the history to send to the
// LLM: 0x12 reasoning trails are stripped from assistant messages. The
// stored history (and therefore the UI) keeps the full content untouched.
//
// Empty-content assistant messages are handled in two ways (best practice
// distilled from open-webui #25083, Hermes #66429, AstrBot #7202):
//   - Empty content + NO tool_calls: dropped from the outbound copy. Such
//     messages carry no semantic payload and only poison the model's
//     self-history — Hermes #66429 showed that a model seeing itself
//     emit 4-12 consecutive empty turns stops calling tools and degrades
//     into "I'll read the summaries now." intent statements. Dropping
//     them also removes the surface where a placeholder like "[no content]"
//     could be echoed back as a structural template (inspect_ai #3603).
//   - Empty content + WITH tool_calls: kept (the tool_calls anchor the
//     following tool results), and content is patched to a non-whitespace
//     placeholder. GLM/Zhipu 1214 rejects empty/whitespace content even
//     when tool_calls are present, so we must fill with "..." — a marker
//     with NO opening/closing pair that the model could mimic (unlike
//     "[no content]" or "(empty message)" which GLM began wrapping
//     around its own replies).
func (a *Agent) buildLLMMessages() []provider.Message {
	msgs := make([]provider.Message, 0, len(a.history))
	droppedEmpty := 0
	for i := range a.history {
		m := a.history[i]
		if m.Role != "assistant" {
			msgs = append(msgs, m)
			continue
		}
		stripped := stripThinkContent(m.Content)
		if truncated, rep := truncateRepetition(stripped); rep {
			log.Warnf("[Agent] truncated repetitive content in assistant history (len %d -> %d)",
				len(stripped), len(truncated))
			stripped = truncated
		}
		// Best practice: drop empty assistant messages that carry no
		// tool_calls. They have zero semantic value and poison the
		// model's self-view (Hermes #66429). Filtering them out here
		// also prevents any placeholder string from leaking into the
		// outbound payload at all — eliminating the mimic surface
		// entirely for this class of message. IsEmptyAssistantContent
		// additionally treats the legacy "No response content" phrase as
		// empty, so histories contaminated before the placeholder became
		// invisible self-heal instead of re-sending the phrase.
		if provider.IsEmptyAssistantContent(stripped) && len(m.ToolCalls) == 0 {
			droppedEmpty++
			continue
		}
		// Assistant with tool_calls but empty content: keep it (the
		// tool_calls anchor the following tool results) and fill with the
		// invisible non-whitespace placeholder so GLM 1214 and
		// ValidateMessageAlternation both pass.
		if provider.IsEmptyAssistantContent(stripped) {
			stripped = emptyAssistantPlaceholder
		}
		m.Content = stripped
		msgs = append(msgs, m)
	}
	if droppedEmpty > 0 {
		log.Warnf("[Agent] dropped %d empty assistant message(s) with no tool_calls from outbound payload", droppedEmpty)
	}
	// Dropping empty assistant turns can place two user/system messages
	// back-to-back. Collapse consecutive same-role non-tool messages so
	// ValidateMessageAlternation still passes (keep the newest, which is
	// the real user input). Tool blocks are never merged.
	msgs = collapseConsecutive(msgs)
	// P0-1: 在头部 system 之后注入静态规则链 + 本 turn 动态记忆（必须在
	// collapse 之后，否则注入的 system 会被合并逻辑吞掉）
	return a.withContextBlocks(msgs)
}

// prepareDynamicMemory 在新用户输入进入主循环时召回一次动态记忆（P0-1）。
// 以输入文本为查询经 cortex 门面检索结构化记忆库，结果缓存到
// a.dynamicMemory，供 buildLLMMessages 在同一 turn 内反复注入。
// 缓存键包含 history 长度，防止同一次输入被重复召回；召回失败或
// cortex 未启用时静默跳过（零依赖）。
func (a *Agent) prepareDynamicMemory(input string) {
	if a.cortexManager == nil || !a.memoryEnabled {
		return
	}
	key := fmt.Sprintf("%d:%s", len(a.history), input)
	if key == a.dynamicMemoryKey {
		return
	}
	if a.memoryScope != "" {
		// 目录级共享记忆：只从当前会话工作目录的记忆桶召回
		a.dynamicMemory = a.cortexManager.RecallForInputScope(a.memoryScope, input)
	} else {
		a.dynamicMemory = a.cortexManager.RecallForInput(input)
	}
	a.dynamicMemoryKey = key
}

// ensureRuleContext 按需重载静态规则链。ruleDir 为空时不做任何事；否则用
// stat 签名判断规则文件是否变化，变了才重新发现+格式化（避免每个 provider
// 调用都重读磁盘）。无规则文件时 ruleContext 保持空串，出站不插入。
func (a *Agent) ensureRuleContext() {
	if a.ruleDir == "" {
		return
	}
	sig := ctxrules.RuleChainSignature(a.ruleDir, ctxrules.DefaultRuleNames)
	if sig == a.ruleSig {
		return
	}
	files := ctxrules.LoadRuleChain(a.ruleDir, ctxrules.DefaultRuleNames)
	a.ruleContext = ctxrules.FormatRuleContext(files, 0)
	a.ruleSig = sig
}

// withContextBlocks 把静态规则链与动态记忆作为 system 消息插入出站消息
// 头部 system 之后（顺序：规则 → 记忆；无 system 时置于最前）。消息系统已
// 容忍连续 system（messages.go），且 Zhipu 1214 约束只要求 system 之后紧跟
// user，注入位置满足两端约束。两者都为空时原样返回。
func (a *Agent) withContextBlocks(msgs []provider.Message) []provider.Message {
	if len(msgs) == 0 {
		return msgs
	}
	a.ensureRuleContext()
	hasRule := a.ruleContext != ""
	hasMemory := a.dynamicMemory != ""
	// 工作目录 ground truth：模型此前只能靠召回的记忆推断「我在哪个项目」，
	// 一旦记忆被别的项目污染就会选择错误路径（记忆串事故）。这里每轮注入
	// 当前目录并声明其他路径的记忆不具权威性。memoryScope 即会话目录的
	// 归一化键，随 SetMemoryScope 更新，不会像 system prompt 那样过期。
	hasWorkspace := a.memoryScope != ""
	if !hasRule && !hasMemory && !hasWorkspace {
		return msgs
	}

	var extras []provider.Message
	if hasWorkspace {
		extras = append(extras, provider.Message{Role: "system", Content: fmt.Sprintf(
			"[Workspace]\nCurrent working directory: %s\nResolve every file, command and repository operation against this directory. Memory entries that reference other project paths are not authoritative for this workspace.",
			a.memoryScope)})
	}
	if hasRule {
		extras = append(extras, provider.Message{Role: "system", Content: a.ruleContext})
	}
	if hasMemory {
		extras = append(extras, provider.Message{Role: "system", Content: a.dynamicMemory})
	}

	out := make([]provider.Message, 0, len(msgs)+len(extras))
	if msgs[0].Role == "system" {
		out = append(out, msgs[0])
		out = append(out, extras...)
		out = append(out, msgs[1:]...)
	} else {
		out = append(out, extras...)
		out = append(out, msgs...)
	}
	return out
}

// truncateRunes 按 rune 边界安全截断（不含省略号后缀）。
// 字节截断（s[:500]）会把多字节 UTF-8 字符切成两半，中文内容直接变成
// 乱码（U+FFFD）——web 端工具参数/历史展示乱码的根因。max 按字符计。
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// truncateBytesOnRuneBoundary 把 s 截到至多 maxBytes 个字节，且不会把一个多
// 字节 UTF-8 rune 劈成两半。
//
// 与 truncateRunes 的区别是**口径**：那个按字符数限长，这个按字节数限长。凡是
// 上游用字节数下发的预算（例如 truncateHistory 的 maxSystemLen，它限制的是发给
// provider 的 payload 体积）都必须用这个版本 —— 裸的 s[:maxBytes] 一旦切在 rune
// 中间，产出的字符串就是非法 UTF-8：严格的 provider 直接 400，宽松的静默替换成
// U+FFFD，中文提示词的尾部会变成一个乱码字符且没有任何报错。
func truncateBytesOnRuneBoundary(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	// 回退到最近的 rune 起始字节（0b0xxxxxxx / 0b11xxxxxx）；
	// len(s) > maxBytes 保证 end 始终是合法下标。
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// collapseConsecutive merges runs of consecutive same-role messages that
// are not tool messages, keeping the LAST one in each run. This repairs
// alternation breakage introduced by dropping empty assistant turns
// (user → assistant-emptied → user becomes user → user). Tool messages
// are passed through untouched because they have their own alternation
// rules tied to tool_call_id matching.
func collapseConsecutive(msgs []provider.Message) []provider.Message {
	if len(msgs) <= 1 {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role == "tool" {
			out = append(out, m)
			continue
		}
		// Peek ahead: if the next message has the same role, skip this
		// one (we keep the last of the run).
		if i+1 < len(msgs) && msgs[i+1].Role == m.Role && msgs[i+1].Role != "tool" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// sanitizeHistory repairs malformed history so the next provider call
// succeeds. Two classes of damage can accumulate:
//
//  1. Orphaned tool messages (a ChatWithTools failure after tools ran leaves
//     tool results without their assistant tool_calls header).
//  2. General alternation violations — most importantly consecutive user
//     messages. These arise when a turn aborts right after an injected
//     "recovery prompt" or summary-request user message is appended: the
//     stored history then ends with that injected prompt, and the user's
//     real input becomes the second consecutive user message. Strict
//     providers (zhipu/GLM 1214) reject such payloads permanently.
//
// For consecutive users we keep the LATEST one (the real user input) and
// drop the earlier injected ones — the reverse of the generic sanitizer,
// which would silently discard the user's actual message.
func (a *Agent) sanitizeHistory() {
	// Pass 1: drop orphaned tool messages (existing behavior).
	cleaned := make([]provider.Message, 0, len(a.history))
	for _, m := range a.history {
		if m.Role == "tool" {
			hasCaller := false
			// Walk the messages KEPT SO FAR, never the source slice: cleaned
			// shrinks every time an orphan is dropped, so an index taken from
			// the source (i-1) can be past cleaned's end and panic — same crash
			// as the bot-side sanitizer (index out of range [9] with length 9).
			for j := len(cleaned) - 1; j >= 0; j-- {
				if cleaned[j].Role == "assistant" && len(cleaned[j].ToolCalls) > 0 {
					for _, tc := range cleaned[j].ToolCalls {
						if tc.ID == m.ToolCallID {
							hasCaller = true
							break
						}
					}
				}
				if hasCaller {
					break
				}
			}
			if !hasCaller {
				log.Warnf("[Agent] Dropping orphaned tool message (tool_call_id=%s)", m.ToolCallID)
				continue
			}
		}
		cleaned = append(cleaned, m)
	}
	a.history = cleaned

	// Pass 2: collapse runs of consecutive user messages to the last one.
	final := make([]provider.Message, 0, len(a.history))
	dropped := 0
	for _, m := range a.history {
		if m.Role == "user" && len(final) > 0 && final[len(final)-1].Role == "user" {
			// Keep the newer message: replace the tail.
			final[len(final)-1] = m
			dropped++
			continue
		}
		final = append(final, m)
	}
	if dropped > 0 {
		log.Warnf("[Agent] Collapsed %d consecutive duplicate user message(s)", dropped)
	}
	a.history = final

	// Pass 2.5 removed: empty assistant content is no longer patched in the
	// stored history. Patching here wrote [no content] into a.history, which
	// the UI renders verbatim. Empty content is fixed only in the outbound
	// copy (buildLLMMessages + convert.go) before sending to the provider.

	// Pass 4: repair assistant messages with orphan tool_calls. An assistant
	// that announces tool_calls must be followed by one or more `tool` role
	// replies matching each tool call ID. If truncateHistory dropped the
	// tool-role results (e.g. because it removed the tail of a completed
	// exchange mid-pair) the orphan assistant is structurally invalid:
	// providers like Zhipu/Minimax throw 1214, and OpenAI silently drops
	// the unclaimed tool_call array. The safest fix here is to STRIP the
	// unclaimed ToolCalls slice from the assistant (keeping content so we
	// don't trigger Pass 2.5) — which turns it back into a plain
	// assistant reply message the user effectively "saw" before the tool
	// calls got truncated away.
	orphanToolCalls := 0
	strippedAssistants := 0
	for i, m := range a.history {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		// Walk forward collecting which call IDs actually have a matching tool
		// reply before we hit the next non-tool boundary.
		claimed := make(map[string]bool, len(m.ToolCalls))
		for j := i + 1; j < len(a.history) && a.history[j].Role == "tool"; j++ {
			if id := strings.TrimSpace(a.history[j].ToolCallID); id != "" {
				claimed[id] = true
			}
		}
		// Filter the assistant ToolCalls: keep only IDs that have a result.
		kept := make([]types.ToolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if claimed[tc.ID] {
				kept = append(kept, tc)
			} else {
				orphanToolCalls++
			}
		}
		if len(kept) != len(m.ToolCalls) {
			a.history[i].ToolCalls = kept
			strippedAssistants++
		}
	}
	if orphanToolCalls > 0 {
		log.Warnf("[Agent] Stripped %d orphan tool_call(s) from %d assistant message(s) — the preceding tool results were lost by truncation", orphanToolCalls, strippedAssistants)
	}

	// Pass 5: drop leading non-system messages until the head is legal. Some
	// providers (Zhipu, Minimax) reject histories that start with an
	// assistant role (OpenAI tolerates it, so ValidateMessageAlternation
	// intentionally lets it through — but we're being strict here to stop
	// 1214 mid-conversation after truncateHistory drops system+user).
	for len(a.history) > 0 {
		r := a.history[0].Role
		if r == "system" || r == "user" {
			break
		}
		// Don't drop tool messages in place; drop them one at a time.
		log.Warnf("[Agent] Dropping leading illegal role=%q message from history head", r)
		a.history = a.history[1:]
	}

	// Pass 3: general best-effort repair for anything left (assistant pairs,
	// stray roles). Generic sanitizer drops the *offending* message; good
	// enough as a last resort.
	if violations := ValidateMessageAlternation(a.history); len(violations) > 0 {
		log.Warnf("[Agent] sanitizeHistory repairing %d residual violation(s)", len(violations))
		a.history = SanitizeMessageHistory(a.history)
	}
}

// SetMaxIterations sets the maximum iterations
func (a *Agent) SetMaxIterations(max int) {
	if max > 0 {
		a.maxIterations = max
	}
}

// AddSystemContext appends context to the system prompt message.
// If no system message exists, creates one.
func (a *Agent) AddSystemContext(ctx string) {
	if len(a.history) == 0 || a.history[0].Role != "system" {
		a.history = append([]provider.Message{{Role: "system", Content: ctx}}, a.history...)
		return
	}
	a.history[0].Content += "\n\n" + ctx
}

// GetProvider returns the agent's provider for use by other components
func (a *Agent) GetProvider() provider.Provider {
	return a.provider
}

// GetApprovalHook returns the agent's approval hook for web API access.
// Returns nil if the approval hook is not available.
func (a *Agent) GetApprovalHook() *ApprovalHook {
	return a.approvalHook
}
