package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/magicwubiao/go-magic/internal/agent/hooks"
	"github.com/magicwubiao/go-magic/internal/approval"
	"github.com/magicwubiao/go-magic/internal/budget"
	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/complexity"
	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/redact"
	"github.com/magicwubiao/go-magic/internal/retry"
	"github.com/magicwubiao/go-magic/internal/tool"
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
	maxTotalLen int // max chars in message history
	maxMsgLen   int // max chars per message
	// Steering settings
	maxIterations  int
	maxTokenBudget int64

	// Tool call loop detection (protected by mu)
	toolCallHistory  []string       // 记录已调用的工具名
	toolCallCount    map[string]int // 每个工具被调用的次数
	sameToolLimit    int            // 同一工具最大调用次数
	consecutiveLimit int            // 连续 tool call 最大次数

	// maxParallelTools 全局并行工具执行并发上限（跨所有并行组共享）。
	// 默认 4；<=0 视为非法并在运行时兜底为串行。
	maxParallelTools int

	// Memory integration
	memoryEnabled bool

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

	// Compression settings
	compressionEnabled bool
	compressionRatio   float64
	maxMemoryTokens    int

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

	// Plan-guided execution
	planExecutor *PlanExecutor
	planEnabled  bool
	planCfg      PlanExecutorConfig
	failStreak   int

	// Trajectory-based learning
	trajInjector *cortex.TrajectoryInjector
	trajEnabled  bool
	trajCfg      cortex.TrajectoryInjectorConfig
	trajStore    *cortex.TrajectoryStore

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
		provider:         prov,
		registry:         registry,
		tools:            tools,
		history:          history,
		maxTurns:         150,
		maxIterations:    200,
		maxTotalLen:      200000, // 200K chars max history (~50K tokens)
		maxMsgLen:        50000,  // 50K chars per message (~12K tokens)
		maxTokenBudget:   0,
		sameToolLimit:    3,
		consecutiveLimit: 10,
		maxParallelTools: defaultMaxParallelTools,
		toolCallCount:    make(map[string]int),
		subTaskEnabled:   true,
		hooks:            hooks.NewHookManager(),
		bus:              bus.NewEventBus(),
		budget:           budget.Preset("parent"),
		compressor:       compress.NewCompressor(8000),
		errorClassifier:  retry.NewClassifier(),
		failureDetector:  retry.NewRepeatedFailureDetector(retry.DefaultRepeatedFailureConfig()),
		smartRecovery:    retry.NewSmartRecovery(retry.DefaultSmartRecoveryConfig()),
	}

	agent.registerBuiltinHooks()

	// history 摘要压缩增强：为 compressor 注入 LLM summarizer。
	// 压缩超阈值时中段消息交由主 provider 生成语义摘要；
	// 失败/超时(20s硬上限)由 compressor 内部兜底为规则式摘要。
	if agent.compressor != nil {
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
			limit := maxMsgLen / 8
			if limit < 500 {
				limit = 500
			}
			if limit > 2000 {
				limit = 2000
			}
			b.WriteString(m.Role)
			b.WriteString(": ")
			b.WriteString(utils.TruncateDetailed(content, limit))
			b.WriteString("\n")
		}

		req := []provider.Message{
			{
				Role: "system",
				Content: "你是对话压缩助手。将给定历史对话压缩为高信息密度的接力摘要，供后续上下文窗口继续任务使用。" +
					"必须保留：①当前任务目标与原始用户诉求；②已完成的关键步骤及其结果；③重要决策及理由；" +
					"④涉及的具体文件路径/命令/数据；⑤报错信息的核心内容；⑥未完成事项与建议的下一步。" +
					"直接输出摘要正文，不要客套话，不要 Markdown 标题。",
			},
			{
				Role:      "user",
				Content:   "以下是需要压缩的历史对话：\n\n" + b.String(),
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

// WithConvertConfig sets the file conversion configuration
func WithConvertConfig(cfg *provider.ConvertConfig) AgentOption {
	return func(a *Agent) {
		if a.provider != nil {
			// Try to access BaseProvider if available
			switch p := a.provider.(type) {
			case *provider.OpenAICompatibleProvider:
				p.BaseProvider.WithConvertConfig(cfg)
			case *provider.DashScopeProvider:
				if p.BaseProvider != nil {
					p.BaseProvider.WithConvertConfig(cfg)
				}
			case *provider.DeepSeekProvider:
				p.OpenAICompatibleProvider.BaseProvider.WithConvertConfig(cfg)
			default:
				// Try to access SetConvertConfig method via interface
				if ccp, ok := a.provider.(interface{ SetConvertConfig(*provider.ConvertConfig) }); ok {
					ccp.SetConvertConfig(cfg)
				}
			}
		}
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

// WithMaxTurns overrides the per-turn tool-loop cap (default 60). Values <= 0
// are ignored so callers can pass config straight through.
func WithMaxTurns(n int) AgentOption {
	return func(a *Agent) {
		if n > 0 {
			a.maxTurns = n
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

// WithPlanExecution enables plan-guided agent execution
func WithPlanExecution(cfg PlanExecutorConfig) AgentOption {
	return func(a *Agent) {
		a.planCfg = cfg
		a.planEnabled = true
	}
}

// initPlanExecutor initializes the plan executor
func (a *Agent) initPlanExecutor(ctx context.Context, goal string) error {
	if !a.planEnabled || a.planExecutor != nil {
		return nil
	}

	a.planExecutor = NewPlanExecutor(a.provider, a.planCfg)
	plan, err := a.planExecutor.CreatePlan(ctx, goal)
	if err != nil {
		return err
	}

	// Emit plan event
	a.Emit(bus.EventKindPlanUpdate, map[string]interface{}{
		"action": "created",
		"steps":  len(plan.Steps),
	})

	// Inject plan into conversation history
	planPrompt := a.planExecutor.GetPlanPrompt()
	if planPrompt != "" {
		a.history = append(a.history, provider.Message{
			Role:    "user",
			Content: planPrompt,
		})
	}

	return nil
}

// updatePlanProgress checks for step completion and updates the plan
func (a *Agent) updatePlanProgress(ctx context.Context) {
	if a.planExecutor == nil {
		return
	}

	currentStep := a.planExecutor.GetCurrentStep()
	if currentStep == nil {
		return
	}

	// Detect if current step is complete
	if a.planExecutor.DetectStepCompletion(a.history, nil) {
		summary := a.planExecutor.GenerateStepSummary(ctx, currentStep.ID, a.history)
		a.planExecutor.MarkStepComplete(currentStep.ID)

		// Record achievement for reflection
		if a.reflector != nil {
			a.reflector.RecordAchievement(summary)
		}

		a.Emit(bus.EventKindPlanUpdate, map[string]interface{}{
			"action":    "step_complete",
			"step_id":   currentStep.ID,
			"step_desc": currentStep.Description,
			"progress":  a.planExecutor.GetProgress(),
			"summary":   summary,
		})

		// If plan is complete, inject completion message
		if a.planExecutor.IsPlanComplete() {
			a.history = append(a.history, provider.Message{
				Role:    "user",
				Content: "\n\n📋 All plan steps have been completed. Please provide a comprehensive final summary of everything that was accomplished.",
			})
		}
	}
}

// handlePlanFailure handles a tool execution failure in plan context
func (a *Agent) handlePlanFailure(ctx context.Context, errMsg string) {
	if a.planExecutor == nil {
		return
	}

	a.failStreak++

	currentStep := a.planExecutor.GetCurrentStep()
	if currentStep != nil {
		a.planExecutor.MarkStepFailed(currentStep.ID, errMsg)

		if a.reflector != nil {
			a.reflector.RecordBlocker(errMsg)
		}
	}

	// Check if we need to replan
	if a.planExecutor.ShouldReplan(a.failStreak) {
		adjustment, err := a.planExecutor.Replan(ctx, errMsg)
		if err == nil && adjustment != nil {
			a.failStreak = 0
			a.Emit(bus.EventKindPlanUpdate, map[string]interface{}{
				"action":     "replan",
				"adjustment": adjustment,
			})

			// Inject replan notification
			a.history = append(a.history, provider.Message{
				Role: "user",
				Content: fmt.Sprintf("\n\n🔄 Plan has been adjusted: %s\nNew plan steps:\n%s",
					adjustment.Reason, a.planExecutor.GetPlanPrompt()),
			})
		}
	}
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

// recordToolCall safely records a tool call for loop detection
func (a *Agent) recordToolCall(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCallHistory = append(a.toolCallHistory, name)
	a.toolCallCount[name]++
}

// getToolCallCount safely returns the count for a specific tool
func (a *Agent) getToolCallCount(name string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.toolCallCount[name]
}

// getToolCallHistoryLength safely returns the length of tool call history
func (a *Agent) getToolCallHistoryLength() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.toolCallHistory)
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

// AddSkillsContext adds skills context to system prompt
func (a *Agent) AddSkillsContext(skillsCtx string) {
	if skillsCtx == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for i, msg := range a.history {
		if msg.Role == "system" {
			a.history[i].Content += "\n\n" + skillsCtx
			return
		}
	}

	a.history = append([]provider.Message{{
		Role:    "system",
		Content: skillsCtx,
	}}, a.history...)
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
	// If cortex is enabled, use the full cortex integration path
	if a.cortexManager != nil {
		return a.RunWithCortex(ctx, input)
	}

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
			log.Warnf("[Agent] sanitizing %d message-alternation violation(s) before LLM call", len(violations))
			req.Messages = SanitizeMessageHistory(req.Messages)
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
			continue
		}

		// Track token usage from the response
		if resp.Usage != nil {
			a.inputTokens += resp.Usage.PromptTokens
			a.outputTokens += resp.Usage.CompletionTokens
		}

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

		// Check for tool call loops
		if len(a.toolCallHistory) > 0 && a.toolCallHistory[len(a.toolCallHistory)-1] == "unknown" {
			// Replace last if unknown
			a.toolCallHistory = a.toolCallHistory[:len(a.toolCallHistory)-1]
		}
		for _, tc := range resp.ToolCalls {
			name := tc.GetToolName()
			a.toolCallHistory = append(a.toolCallHistory, name)
		}

		// Detect loops
		loopDetected := false
		loopReason := ""
		toolCounts := make(map[string]int)
		for _, name := range a.toolCallHistory {
			toolCounts[name]++
			if toolCounts[name] > a.sameToolLimit {
				loopDetected = true
				loopReason = fmt.Sprintf("tool %s called %d times", name, toolCounts[name])
				break
			}
		}

		// Check consecutive tool calls limit
		if len(a.toolCallHistory) >= a.consecutiveLimit {
			loopDetected = true
			if loopReason == "" {
				loopReason = fmt.Sprintf("%d consecutive tool calls", len(a.toolCallHistory))
			}
		}

		if loopDetected {
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(resp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			a.history = append(a.history, provider.Message{
				Role:      "user",
				Content:   "Please provide a final summary of what has been accomplished so far. Do not call any more tools.",
				Timestamp: time.Now(),
			})
			finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages())
			if finalErr != nil {
				return "", fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}
			return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
		}
	}

	// Try to get a summary from the LLM before giving up
	a.history = append(a.history, provider.Message{
		Role:      "user",
		Content:   "You have reached the maximum number of turns. Please provide a brief summary of what you accomplished and what remains incomplete. Do NOT call any more tools.",
		Timestamp: time.Now(),
	})
	if ctx.Err() == nil {
		if finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages()); finalErr == nil && finalResp.Content != "" {
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	recentTools := []string{}
	if len(a.toolCallHistory) > 0 {
		recentCount := len(a.toolCallHistory)
		start := 0
		if recentCount > 5 {
			start = recentCount - 5
		}
		recentTools = a.toolCallHistory[start:]
	}
	return "", fmt.Errorf("exceeded maximum turns (%d). Completed %d turns with %d tool calls. Recent tools: %v",
		a.maxTurns, a.iterationCount, len(a.toolCallHistory), recentTools)
}

// RunConversation runs a conversation with automatic tool execution
func (a *Agent) RunConversation(ctx context.Context, input string) (string, error) {
	// If cortex is enabled, use the full cortex integration path
	if a.cortexManager != nil {
		return a.RunWithCortex(ctx, input)
	}

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

	// Initialize plan executor
	if a.planEnabled {
		if err := a.initPlanExecutor(ctx, input); err != nil {
			log.Warnf("[Agent] Plan executor init failed: %v", err)
		}
	}

	// Initialize and inject trajectory-based learning
	// 同时考虑 cortexManager 的 TrajectoryStore（统一数据源，避免双实例）
	if a.trajEnabled && (a.trajStore != nil || (a.cortexManager != nil && a.cortexManager.GetTrajectoryStore() != nil)) {
		a.initTrajectoryInjector()
		a.injectTrajectoryInsights(input)
	}

	// Truncate history to prevent overflow
	a.truncateHistory()

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

		// Self-reflection check (every N turns)
		if a.reflector != nil {
			if err := a.performReflection(ctx, a.iterationCount); err != nil {
				log.Warnf("[Agent] Reflection failed: %v", err)
			}
		}

		// Check if context compression is needed
		if a.compressor != nil {
			totalChars := 0
			for _, msg := range a.history {
				totalChars += len(msg.Content)
			}
			if a.compressor.ShouldCompress(totalChars / 4) { // rough token estimate
				a.Emit(bus.EventKindTurnStart, map[string]interface{}{
					"type":   "progress",
					"phase":  "compressing",
					"detail": "Context window full, compressing history...",
				})
				msgs := make([]compress.Message, 0, len(a.history))
				for _, msg := range a.history {
					if msg.Role != "system" {
						msgs = append(msgs, compress.Message{
							Role:    msg.Role,
							Content: msg.Content,
						})
					}
				}
				result, err := a.compressor.Compress(msgs, "")
				if err == nil && result != nil {
					// Replace history with compressed version
					newHistory := make([]provider.Message, 0)
					for _, msg := range a.history {
						if msg.Role == "system" {
							newHistory = append(newHistory, msg)
						}
					}
					for _, msg := range result.Messages {
						newHistory = append(newHistory, provider.Message{
							Role:    msg.Role,
							Content: msg.Content,
						})
					}
					a.history = newHistory
				}
			}
		}

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
			log.Warnf("[Agent] sanitizing %d message-alternation violation(s) before LLM call", len(violations))
			req.Messages = SanitizeMessageHistory(req.Messages)
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
			log.Warnf("[Agent:RunConversationWithMedia] Falling back to Chat (no tools): provider=%s, hasToolIface=%v, toolsCount=%d",
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
					return "", fmt.Errorf("non-retryable error: %w", err)
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

			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

			return redact.RedactIfEnabled(resp.Content, a.secretRedaction), nil
		}

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
				a.toolCallCount[name]++
				a.toolCallHistory = append(a.toolCallHistory, name)
			} else {
				log.Debugf("[TOOL] skipping empty tool call in response (ID: %s)", tc.ID)
			}
		}

		// If no valid tool calls, return the response directly
		if len(validToolCalls) == 0 {
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(resp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

			return redact.RedactIfEnabled(resp.Content, a.secretRedaction), nil
		}

		// Replace resp.ToolCalls with valid ones for further processing
		resp.ToolCalls = validToolCalls

		// Check if same tool called too many times (with more context)
		loopDetected := false
		loopReason := ""
		for name, count := range a.toolCallCount {
			if count >= a.sameToolLimit {
				loopDetected = true
				loopReason = fmt.Sprintf("tool %s called %d times", name, count)
				break
			}
		}

		// Check consecutive tool calls limit
		if len(a.toolCallHistory) >= a.consecutiveLimit {
			loopDetected = true
			if loopReason == "" {
				loopReason = fmt.Sprintf("%d consecutive tool calls", len(a.toolCallHistory))
			}
		}

		if loopDetected {
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(resp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			a.history = append(a.history, provider.Message{
				Role:      "user",
				Content:   "Please provide a final summary of what has been accomplished so far. Do not call any more tools.",
				Timestamp: time.Now(),
			})
			finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages())
			if finalErr != nil {
				return "", fmt.Errorf("exceeded maximum iterations (%d): tool call loop detected", a.maxIterations)
			}
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}
			return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
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

					// Handle plan failure
					if a.planExecutor != nil {
						a.handlePlanFailure(ctx, errInfo.ErrorMessage)
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

		// Update plan progress at end of iteration
		a.updatePlanProgress(ctx)

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
		if finalResp, finalErr := a.provider.Chat(ctx, a.buildLLMMessages()); finalErr == nil && finalResp.Content != "" {
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   utils.TruncateDetailed(finalResp.Content, a.maxMsgLen),
				Timestamp: time.Now(),
			})
			return redact.RedactIfEnabled(finalResp.Content, a.secretRedaction), nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	recentTools := []string{}
	if len(a.toolCallHistory) > 0 {
		recentCount := len(a.toolCallHistory)
		start := 0
		if recentCount > 5 {
			start = recentCount - 5
		}
		recentTools = a.toolCallHistory[start:]
	}
	return "", fmt.Errorf("exceeded maximum turns (%d). Completed %d turns with %d tool calls. Recent tools: %v",
		a.maxTurns, a.iterationCount, len(a.toolCallHistory), recentTools)
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
	// Emit agent start event
	a.Emit(bus.EventKindAgentStart, nil)

	// Cortex: inject memory context and adjust behavior
	if a.cortexManager != nil {
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
			handler(fmt.Sprintf("\n[⚡ Task Auto-Delegated to Sub-Task Executor]\n"), false)
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
			log.Warnf("[Agent:Stream] sanitizing %d message-alternation violation(s) before LLM call", len(violations))
			req.Messages = SanitizeMessageHistory(req.Messages)
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

		// buildStreamHandlerContent wraps reasoning content with <think> markers
		// and transitions to normal content with </think> closing tag.
		buildStreamHandlerContent := func(resp *provider.StreamResponse) string {
			handlerContent := ""
			if resp.ReasoningContent != "" {
				accumulatedReasoning.WriteString(resp.ReasoningContent)
				if !reasoningStarted {
					handlerContent += "<think>"
					reasoningStarted = true
				}
				handlerContent += resp.ReasoningContent
			}
			if resp.Content != "" {
				if reasoningStarted && !thinkClosed {
					handlerContent += "</think>\n"
					thinkClosed = true
				}
				handlerContent += resp.Content
			}
			return handlerContent
		}

		// finalizeFullContent constructs fullContent with <think> tags from
		// the Done chunk's accumulated reasoning and content.
		finalizeFullContent := func(resp *provider.StreamResponse) {
			reasoning := accumulatedReasoning.String()
			if resp.Content != "" {
				if reasoning != "" {
					fullContent = "<think>" + reasoning + "</think>\n" + resp.Content
				} else {
					fullContent = resp.Content
				}
			} else if reasoning != "" {
				// Some providers send empty Content at Done; prepend reasoning
				// to the delta-accumulated fullContent.
				fullContent = "<think>" + reasoning + "</think>\n" + fullContent
			}
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
				if resp.Done {
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
					// Track token usage from final stream chunk
					if resp.Usage != nil {
						a.inputTokens += resp.Usage.PromptTokens
						a.outputTokens += resp.Usage.CompletionTokens
					}
					// Close think tag if still open (for handler completeness)
					handlerContent := ""
					if reasoningStarted && !thinkClosed {
						handlerContent = "</think>\n"
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
				// 流式成功但内容为空且无工具调用，说明流异常（如网络中断导致提前结束）
				// 标记为未流式成功，进入 fallback 非流式重试
				if fullContent == "" && len(toolCalls) == 0 {
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
				log.Warnf("[Agent:Stream] StreamWithTools failed: %v (provider=%s, messages=%d, tools=%d)",
					err, a.provider.Name(), len(req.Messages), len(a.tools))
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
							a.sanitizeHistory()
							handler("", true)
							return fmt.Errorf("non-retryable error: %w", err)
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
			err = ss.Stream(ctx, req.Messages, func(resp *provider.StreamResponse) {
				if resp.Error != nil {
					lastErr = resp.Error
					return
				}
				if resp.Done {
					// Done chunk 的 Content 可能为空（perplexity/gemini/wenxin），
					// 仅在非空时覆盖，避免清空已累积的内容
					finalizeFullContent(resp)
					// Track token usage from final stream chunk
					if resp.Usage != nil {
						a.inputTokens += resp.Usage.PromptTokens
						a.outputTokens += resp.Usage.CompletionTokens
					}
					// Close think tag if still open
					handlerContent := ""
					if reasoningStarted && !thinkClosed {
						handlerContent = "</think>\n"
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
				// 流式成功但内容为空，进入 fallback 非流式重试
				if fullContent == "" {
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
							a.sanitizeHistory()
							handler("", true)
							return fmt.Errorf("non-retryable error: %w", err)
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

			// 最终非流式兜底路径同样限制长度（截断超长思考文本）
			fullContent = utils.TruncateDetailed(resp.Content, a.maxMsgLen)
			toolCalls = resp.ToolCalls
			for i := range toolCalls {
				if toolCalls[i].ID == "" {
					toolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
				}
				toolCalls[i].Normalize()
			}
			handler(redact.RedactIfEnabled(resp.Content, a.secretRedaction), true)
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
			content := utils.TruncateDetailed(llmResp.Content, a.maxMsgLen)
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				Content:   content,
				Timestamp: time.Now(),
			})
			a.Emit(bus.EventKindTurnEnd, nil)
			a.Emit(bus.EventKindAgentEnd, nil)

			// Cortex: refresh snapshot at session end
			if a.cortexManager != nil {
				a.cortexManager.OnSessionEnd()
			}

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
			Content:   utils.TruncateDetailed(fullContent, a.maxMsgLen),
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

	// Cortex: refresh snapshot at session end
	if a.cortexManager != nil {
		a.cortexManager.OnSessionEnd()
	}

	if lastErr != nil {
		// Persisted history must not end malformed for the next turn (bot
		// mode saves ag.GetHistory() after every outcome).
		a.sanitizeHistory()
		return lastErr
	}
	// Max turns exhausted: nothing new to call — sanitize so the stored tail
	// is a legal sequence, then report.
	a.sanitizeHistory()
	return fmt.Errorf("exceeded maximum turns (%d)", a.maxTurns)
}

// RunConversationStreamWithOutput runs streaming and returns output builder
func (a *Agent) RunConversationStreamWithOutput(ctx context.Context, input string) (*strings.Builder, error) {
	var output strings.Builder
	err := a.RunConversationStream(ctx, input, func(content string, done bool) {
		output.WriteString(content)
	})
	return &output, err
}

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
			validToolCalls[i].ID = fmt.Sprintf("call_%d", time.Now().UnixNano()%100000000)
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
			errCh := make(chan error, len(group.tools))

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
					if result.Err != nil {
						errCh <- result.Err
					}
					mu.Unlock()
				}()
			}

			wg.Wait()
			close(errCh)

			// Collect all errors but don't return early - all results must be processed
			var execErrors []error
			for err := range errCh {
				if err != nil {
					execErrors = append(execErrors, err)
				}
			}
			if len(execErrors) > 0 {
				// Return first error but still return all results
				return results, execErrors[0]
			}
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

	result, err := a.registry.Execute(ctx, toolName, callReq.ToolArgs)
	elapsed := time.Since(start)

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

	home, err := os.UserHomeDir()
	if err != nil {
		log.Warnf("[Agent] checkpoint skipped: resolve home dir failed: %v", err)
		return ""
	}
	dir := filepath.Join(home, ".magic", "checkpoints")
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
	a.toolCallHistory = nil
	a.toolCallCount = make(map[string]int)
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

// GetHistory returns the conversation history
func (a *Agent) GetHistory() []provider.Message {
	return a.history
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
func (a *Agent) SetHistory(history []provider.Message) {
	a.history = history
}

// GetHistoryLength returns the current history length in characters
func (a *Agent) GetHistoryLength() int {
	total := 0
	for _, m := range a.history {
		total += len(m.Content)
	}
	return total
}

// truncateHistory truncates message history to prevent overflow
func (a *Agent) truncateHistory() {
	if a.maxTotalLen <= 0 {
		return
	}

	totalLen := a.GetHistoryLength()

	if a.compressionEnabled && totalLen > int(float64(a.maxTotalLen)*a.compressionRatio) {
		a.compressHistory()
		return
	}

	if totalLen < a.maxTotalLen {
		return
	}

	systemIdx := -1
	for i, m := range a.history {
		if m.Role == "system" {
			systemIdx = i
			break
		}
	}

	const maxSystemLen = 50000
	if systemIdx >= 0 && len(a.history[systemIdx].Content) > maxSystemLen {
		truncated := a.history[systemIdx].Content[:maxSystemLen]
		lastNewline := strings.LastIndex(truncated, "\n")
		if lastNewline > maxSystemLen/2 {
			truncated = truncated[:lastNewline]
		}
		truncated += "\n\n[...system prompt truncated...]"
		totalLen -= len(a.history[systemIdx].Content) - len(truncated)
		a.history[systemIdx].Content = truncated
		log.Warnf("[Agent] System prompt truncated from %d to %d chars (maxSystemLen=%d)",
			len(a.history[systemIdx].Content), len(truncated), maxSystemLen)
	}

	if totalLen < a.maxTotalLen {
		return
	}

	for totalLen > a.maxTotalLen && len(a.history) > 1 {
		idx := 0
		if systemIdx == 0 {
			idx = 1
		}

		if a.history[idx].Role == "tool" {
			found := false
			for j := idx - 1; j >= 0; j-- {
				if a.history[j].Role == "assistant" && len(a.history[j].ToolCalls) > 0 {
					removeStart := j
					removeEnd := j + 1
					for removeEnd < len(a.history) && a.history[removeEnd].Role == "tool" {
						totalLen -= len(a.history[removeEnd].Content)
						removeEnd++
					}
					totalLen -= len(a.history[j].Content)
					a.history = append(a.history[:removeStart], a.history[removeEnd:]...)
					found = true
					break
				}
			}
			if found {
				systemIdx = -1
				for i, m := range a.history {
					if m.Role == "system" {
						systemIdx = i
						break
					}
				}
				continue
			}
		}

		if a.history[idx].Role == "assistant" && len(a.history[idx].ToolCalls) > 0 {
			removeEnd := idx + 1
			for removeEnd < len(a.history) && a.history[removeEnd].Role == "tool" {
				totalLen -= len(a.history[removeEnd].Content)
				removeEnd++
			}
			totalLen -= len(a.history[idx].Content)
			a.history = append(a.history[:idx], a.history[removeEnd:]...)
		} else {
			totalLen -= len(a.history[idx].Content)
			a.history = append(a.history[:idx], a.history[idx+1:]...)
		}

		if systemIdx == idx {
			systemIdx = -1
			for i, m := range a.history {
				if m.Role == "system" {
					systemIdx = i
					break
				}
			}
		}
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
func stripThinkContent(s string) string {
	if s == "" || !strings.Contains(s, "<think") {
		return s
	}
	low := strings.ToLower(s)
	var b strings.Builder
	cursor := 0
	for {
		relIdx := strings.Index(low[cursor:], "<think>")
		if relIdx == -1 {
			b.WriteString(s[cursor:])
			break
		}
		openIdx := cursor + relIdx
		b.WriteString(s[cursor:openIdx])
		closeRel := strings.Index(low[openIdx:], "</think>")
		if closeRel == -1 {
			// Unterminated <think>: drop everything from the opening tag on.
			break
		}
		cursor = openIdx + closeRel + len("</think>")
		// Skip a single newline right after the closing tag (the agent adds
		// one when wrapping reasoning) to avoid stacking blank lines.
		if cursor < len(s) && s[cursor] == '\n' {
			cursor++
		}
	}
	return strings.TrimSpace(b.String())
}

// thinkPlaceholder is used when stripping would leave an assistant message
// with no tool calls and empty content — some strict providers reject
// assistant messages that are empty without tool calls.
const thinkPlaceholder = "(thinking omitted)"

// buildLLMMessages returns a sanitized copy of the history to send to the
// LLM: <think> reasoning trails are stripped from assistant messages. The
// stored history (and therefore the UI) keeps the full content untouched.
func (a *Agent) buildLLMMessages() []provider.Message {
	msgs := make([]provider.Message, len(a.history))
	copy(msgs, a.history)
	for i := range msgs {
		if msgs[i].Role != "assistant" {
			continue
		}
		stripped := stripThinkContent(msgs[i].Content)
		if truncated, rep := truncateRepetition(stripped); rep {
			log.Warnf("[Agent] truncated repetitive content in assistant history (len %d -> %d)",
				len(stripped), len(truncated))
			stripped = truncated
		}
		// RC4 (26-turn 1214 root cause): the old guard below had three
		// conjuncts (stripped=="" AND original content != "" AND no
		// ToolCalls), which meant that an assistant message whose content
		// was truly "" in a.history (e.g. the provider emitted no text in
		// stream mode) was copied straight through — Zhipu/GLM treats that
		// as 1214 regardless of ToolCalls. The new rule is: NEVER send an
		// assistant with empty/whitespace content; if stripping produced
		// "" AND we have tool_calls (provider sometimes tolerates that as
		// the pure-tool-call form), leave a single-space placeholder;
		// otherwise fall back to thinkPlaceholder.
		if strings.TrimSpace(stripped) == "" {
			if len(msgs[i].ToolCalls) > 0 {
				stripped = " "
			} else {
				stripped = thinkPlaceholder
			}
		}
		msgs[i].Content = stripped
	}
	return msgs
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
	for i, m := range a.history {
		if m.Role == "tool" {
			hasCaller := false
			for j := i - 1; j >= 0; j-- {
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

	// Pass 2.5: repair empty assistant content. Zhipu (GLM) rejects assistant
	// messages with null / whitespace-only content as error 1214 ("messages
	// 参数非法") **regardless of whether the assistant also carries tool_calls**.
	// The previous version only patched the tool_calls-omitted case, which is
	// why deeply nested turn 26 still produced 1214 after truncateHistory left
	// a streaming-in-progress assistant with ToolCalls but empty content.
	// A single space is the safest placeholder: providers treat it as visible
	// content without introducing any new text for the model to reason about.
	repaired := 0
	for i := range final {
		if final[i].Role == "assistant" && strings.TrimSpace(final[i].Content) == "" {
			final[i].Content = " "
			repaired++
		}
	}
	if repaired > 0 {
		log.Warnf("[Agent] Filled %d empty assistant content message(s) with whitespace placeholder", repaired)
	}
	a.history = final

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

// compressHistory performs smart context compression
func (a *Agent) compressHistory() {
	var userMsgs []provider.Message
	var assistantMsgs []provider.Message

	for _, m := range a.history {
		switch m.Role {
		case "user":
			userMsgs = append(userMsgs, m)
		case "assistant":
			assistantMsgs = append(assistantMsgs, m)
		}
	}

	totalMsgs := len(userMsgs)
	keepRecent := 4
	keepFirst := 2

	if totalMsgs <= keepRecent+keepFirst {
		return
	}

	// Build a set of indices to keep (system + first N user msgs + last N user msgs + their assistant replies)
	keepIndices := make(map[int]bool)

	// Always keep system messages
	for i, m := range a.history {
		if m.Role == "system" {
			keepIndices[i] = true
		}
	}

	// Keep first N user messages and their adjacent assistant/tool messages
	kept := 0
	for i, m := range a.history {
		if m.Role == "user" && kept < keepFirst {
			keepIndices[i] = true
			kept++
			// Also keep the assistant reply and any tool messages that follow
			for j := i + 1; j < len(a.history); j++ {
				if a.history[j].Role == "assistant" || a.history[j].Role == "tool" {
					keepIndices[j] = true
				} else {
					break
				}
			}
		}
	}

	// Keep last N user messages and their adjacent assistant/tool messages
	kept = 0
	for i := len(a.history) - 1; i >= 0 && kept < keepRecent; i-- {
		if a.history[i].Role == "user" {
			keepIndices[i] = true
			kept++
			// Also keep the assistant reply and any tool messages that follow
			for j := i + 1; j < len(a.history); j++ {
				if a.history[j].Role == "assistant" || a.history[j].Role == "tool" {
					keepIndices[j] = true
				} else {
					break
				}
			}
		}
	}

	// Generate summary of removed messages
	var removedMsgs []provider.Message
	for i, m := range a.history {
		if !keepIndices[i] && m.Role == "user" {
			removedMsgs = append(removedMsgs, m)
		}
	}

	var newHistory []provider.Message
	for i, m := range a.history {
		if keepIndices[i] {
			newHistory = append(newHistory, m)
		}
	}

	// Insert summary after system messages
	if len(removedMsgs) > 0 {
		summary := a.generateCompressionSummary(removedMsgs)
		summaryMsg := provider.Message{
			Role: "system",
			Content: fmt.Sprintf("\n\n[Previous conversation summary (%d messages summarized)]\n%s",
				len(removedMsgs), summary),
		}
		// Insert after the last system message
		insertIdx := 0
		for i, m := range newHistory {
			if m.Role == "system" {
				insertIdx = i + 1
			}
		}
		newHistory = append(newHistory[:insertIdx], append([]provider.Message{summaryMsg}, newHistory[insertIdx:]...)...)
	}

	a.history = newHistory
}

// generateCompressionSummary generates a summary of old messages
func (a *Agent) generateCompressionSummary(messages []provider.Message) string {
	var summary strings.Builder
	summary.WriteString("Summary of earlier conversation:\n")

	userCount := 0
	actionCount := 0

	for _, m := range messages {
		if m.Role == "user" {
			userCount++
			content := m.Content
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			if userCount <= 3 {
				summary.WriteString(fmt.Sprintf("- User: %s\n", content))
			}
		} else if m.Role == "tool" && !strings.Contains(m.Content, "Error:") {
			actionCount++
			if actionCount <= 3 {
				if len(m.Content) > 50 {
					summary.WriteString(fmt.Sprintf("- Tool result: %s...\n", m.Content[:50]))
				} else {
					summary.WriteString(fmt.Sprintf("- Tool result: %s\n", m.Content))
				}
			}
		}
	}

	if userCount > 3 {
		summary.WriteString(fmt.Sprintf("- ... and %d more exchanges\n", userCount-3))
	}

	return summary.String()
}

// EnableCompression enables/disables context compression
func (a *Agent) EnableCompression(enabled bool) {
	a.compressionEnabled = enabled
}

// SetCompressionRatio sets the threshold ratio for compression
func (a *Agent) SetCompressionRatio(ratio float64) {
	if ratio > 0.3 && ratio <= 1.0 {
		a.compressionRatio = ratio
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
