package cortex

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/execution"
	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/review"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/trigger"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// Manager integrates all Cortex Agent systems with Hermes Agent-inspired features:
// 1. User Message Trigger
// 2. Periodic Nudge Mechanism
// 3. Background Review System
// 4. Dual File Storage (MEMORY.md + USER.md)
// 5. Holographic Memory (SQLite FTS5)
// 6. Memory Manager with Frozen Snapshot
// 7. SOUL.md System Personality (NEW)
// 8. LLM Planner (NEW)
// 9. Prompt Caching (NEW)
// 10. Context Compression (NEW)
// 11. GEPA Self-Evolution Engine (NEW)
type Manager struct {
	baseDir  string
	provider provider.Provider // For LLM features
	enabled  bool              // Whether Cortex system is enabled

	// Core systems
	Snapshot     *memory.SnapshotManager          // System 4: Frozen snapshot memory
	Trigger      *trigger.MessageTrigger          // System 1 + 2: Nudge mechanism
	Review       *review.EnhancedBackgroundReview // System 3: Background review
	Perception   *perception.Parser               // Layer 1: Intent classification
	Cognition    *cognition.Planner               // Layer 2: Planning and decision making
	LLMPlanner   *cognition.LLMPlanner            // LLM-based planning (NEW)
	Execution    *execution.Manager               // Layer 3: Checkpoint + Resume
	FTSMemory    *memory.FTSStore                 // System 5: FTS full-text search
	SkillCreator *skills.EnhancedAutoCreator      // System 6: Auto skill evolution

	// NEW: Hermes-inspired systems
	Soul              *SoulManager       // System personality (SOUL.md)
	UserProfile       *UserProfile       // User preferences (USER.md)
	PromptCache       *PromptCache       // Prompt caching
	ContextCompressor *ContextCompressor // Context compression
	TrajectoryStore   *TrajectoryStore   // Trajectory learning
	GEPAEngine        *GEPAEngine        // Self-evolution engine

	LastPerception *perception.PerceptionResult
	LastDecision   *cognition.Decision   // Last cognition decision
	LastCheckpoint *execution.Checkpoint // Current execution checkpoint

	// Conversation history for memory extraction
	conversationHistory []struct {
		Role    string
		Content string
	}
	// 已抽取/已写日志的消息数水位。conversationHistory 是整个会话的累积
	// 历史，每轮结束都会触发 extractAndLearnFromConversation——没有水位
	// 时同一批消息会被反复送 LLM 抽取、反复写入每日日志（第 N 轮写 N 遍）。
	// 历史被 truncateHistory 收缩时水位对齐到当前长度（跳过当轮抽取，
	// 防止截断后前缀重放导致重复）。
	processedHistoryLen int
	mu                  sync.RWMutex // protects conversationHistory / processedHistoryLen

	// Index of the last tool call that was already fed to SkillCreator.
	// This prevents re-analyzing the same accumulated tool calls on every
	// turn, which would inflate pattern frequencies.
	lastAnalyzedToolIdx int

	// 结构化记忆存储（与 FTSMemory 双写，保持数据一致）
	memoryStore *memory.Store
	// LLM 记忆抽取器（抽取失败时回退到行匹配）
	memoryExtractor *memory.MemoryExtractor

	// 启动期初始化失败的子系统及错误信息（用于 GetSystemStatus 区分未初始化与初始化失败）
	initFailures map[string]string

	// GEPA 最优策略应用循环的取消函数
	gepaApplyCancel context.CancelFunc

	// 记忆库维护循环（清理过期记忆 + 每日蒸馏）的停止信号
	memoryMaintenanceStop chan struct{}
	// 维护循环退出信号：Stop() 关闭 stop 后等待 done，再关闭数据库
	memoryMaintenanceDone chan struct{}

	// 每日对话日志（P1-2，懒初始化）
	dailyLog *memory.DailyLog
}

// ManagerConfig holds configuration for Cortex systems
type ManagerConfig struct {
	// Master switch
	Enabled bool // Enable/disable Cortex system

	// Review settings
	ReviewInterval      time.Duration
	ReviewEnabled       bool
	SkillMinPatternFreq int // Minimum frequency for skill pattern detection

	// Perception settings
	PerceptionConfidenceThreshold float64
	PerceptionMaxHistory          int

	// Cognition settings
	PlanningMaxSteps int
	PlanningTimeout  time.Duration

	// Trigger settings
	NudgeInterval time.Duration
	NudgeEnabled  bool
}

// NewManager creates a new Cortex integration manager
// Initializes all Cortex systems including Hermes Agent-inspired features
func NewManager(baseDir string, prov provider.Provider) *Manager {
	return NewManagerWithProfileAndConfig(baseDir, prov, "", nil)
}

// NewManagerWithProfile creates a new Cortex manager with specific profile
// The profile parameter specifies which profile's user.md to load
func NewManagerWithProfile(baseDir string, prov provider.Provider, profile string) *Manager {
	return NewManagerWithProfileAndConfig(baseDir, prov, profile, nil)
}

// NewManagerWithProfileAndConfig creates a new Cortex manager with profile and custom config
func NewManagerWithProfileAndConfig(baseDir string, prov provider.Provider, profile string, config *ManagerConfig) *Manager {
	// baseDir is already the cortex directory (e.g. <magicHome>/cortex);
	// do NOT add another "cortex" subdirectory here.
	cortexDir := baseDir

	// Determine UserProfile path based on profile
	userProfileDir := cortexDir
	if profile != "" && profile != "default" {
		userProfileDir = filepath.Join(baseDir, "profiles", profile)
	}

	// Apply defaults for nil config
	if config == nil {
		config = &ManagerConfig{
			Enabled:                       true,
			ReviewInterval:                30 * time.Minute,
			ReviewEnabled:                 true,
			SkillMinPatternFreq:           3,
			PerceptionConfidenceThreshold: 0.7,
			PerceptionMaxHistory:          100,
			PlanningMaxSteps:              50,
			PlanningTimeout:               30 * time.Second,
			NudgeInterval:                 15 * time.Minute,
			NudgeEnabled:                  true,
		}
	}

	// Create manager with basic fields
	mgr := &Manager{
		baseDir:  baseDir,
		provider: prov,
		enabled:  config.Enabled,
	}

	// Skip cortex system initialization if disabled
	if !config.Enabled {
		return mgr
	}

	// Create review config from ManagerConfig
	reviewConfig := &review.ReviewConfig{
		ReviewInterval:   config.ReviewInterval,
		MinPatternFreq:   config.SkillMinPatternFreq,
		MaxPatterns:      100,
		AutoSaveEnabled:  true,
		SnapshotInterval: 5,
	}

	// Initialize all Cortex systems
	mgr.Snapshot = memory.NewSnapshotManager(baseDir)
	mgr.Trigger = trigger.NewMessageTrigger()
	mgr.Review = review.NewEnhancedBackgroundReviewWithConfig(filepath.Join(cortexDir, "reviews"), reviewConfig)
	mgr.Perception = perception.NewParser()
	mgr.Cognition = cognition.NewPlanner()
	mgr.Execution = execution.NewManager(baseDir)
	mgr.SkillCreator = skills.NewEnhancedAutoCreator(baseDir)
	// Apply configured minimum pattern frequency (default 5 is too high, use config value)
	if config.SkillMinPatternFreq > 0 {
		mgr.SkillCreator.SetMinFrequency(config.SkillMinPatternFreq)
	}

	// NEW: Hermes-inspired systems
	mgr.Soul = NewSoulManager(cortexDir)
	mgr.UserProfile = NewUserProfile(userProfileDir)
	mgr.PromptCache = nil // Initialized in Start()
	mgr.ContextCompressor = NewContextCompressor(prov, 0, 0)
	mgr.TrajectoryStore = nil // Initialized in Start()

	// Initialize LLM Planner if provider is available
	if prov != nil {
		mgr.LLMPlanner = cognition.NewLLMPlanner(prov)
	}

	// Wire up the connections between systems
	mgr.setupConnections()

	return mgr
}

// Deprecated: Use NewManagerWithProfileAndConfig instead
func NewManagerWithConfig(baseDir string, prov provider.Provider, config *ManagerConfig) *Manager {
	return NewManagerWithProfileAndConfig(baseDir, prov, "", config)
}

// IsEnabled returns whether the Cortex system is enabled
func (m *Manager) IsEnabled() bool {
	return m.enabled
}

// GetTrajectoryStore returns the Manager's TrajectoryStore, or nil if not initialized.
// 外部调用方（如 agent）应优先通过此方法复用 Manager 的 TrajectoryStore，
// 避免创建独立实例导致两套轨迹数据互不可见。
func (m *Manager) GetTrajectoryStore() *TrajectoryStore {
	return m.TrajectoryStore
}

// setupConnections wires the six systems together
func (m *Manager) setupConnections() {
	// Skip if cortex is disabled
	if !m.enabled {
		return
	}

	// Trigger -> Review: Nudge 触发后台评审
	m.Trigger.RegisterNudgeHandler(func() {
		turnCount := m.Trigger.GetTurnCount()
		// In a real implementation, we would pass actual tool call history
		m.Review.TriggerNudgeReview(turnCount, []string{}, nil)

		// Soul <-> UserProfile: 在 nudge 时把高置信度用户偏好同步到 SOUL，
		// 实现两个系统间的偏好数据共享
		m.syncPreferencesToSoul()
	})

	// 注：Trajectory -> GEPA 的接线在 Start() 中通过
	// NewGEPAEngine(cortexDir, provider, TrajectoryStore) 完成（二者在构造期才可用），
	// 此处无法提前接线；最优策略应用循环亦在 Start() 中启动。
}

// syncPreferencesToSoul 将 UserProfile 中高置信度偏好同步到 SOUL.md，实现 Soul↔UserProfile 偏好共享
func (m *Manager) syncPreferencesToSoul() {
	if m.Soul == nil || m.UserProfile == nil {
		return
	}
	prefs := m.UserProfile.GetHighConfidence(0.7)
	if len(prefs) == 0 {
		return
	}
	var sb strings.Builder
	for _, p := range prefs {
		sb.WriteString("- " + p.Key + ": " + p.Value + "\n")
	}
	_ = m.Soul.UpdateFromFeedback(sb.String())
}

// BindSkillsManager connects the cortex skill auto creator to the skills Manager
// so auto-generated skills are visible via /api/skills.
// 同时同步 SkillCreator 的 baseDir 到 Manager 的 autoSkillsDir，避免两者
// 各自维护不同的 auto_skills 路径导致重启后丢失已批准的自动技能。
func (m *Manager) BindSkillsManager(sm *skills.Manager) {
	if m.SkillCreator != nil && sm != nil {
		m.SkillCreator.SetManager(sm)
		// 同步路径：以 Manager 的 autoSkillsDir 为准
		autoDir := sm.GetAutoSkillsDir()
		if autoDir != "" {
			m.SkillCreator.SetBaseDir(autoDir)
		}
	}
}

// Start initializes all Cortex systems
// Systems started in order of dependency:
func (m *Manager) Start() error {
	// Skip if cortex is disabled
	if !m.enabled {
		return nil
	}

	// baseDir is already the cortex directory
	cortexDir := m.baseDir

	// 初始化失败记录表（用于 GetSystemStatus 区分 not_initialized / init_failed）
	m.initFailures = make(map[string]string)

	// System 4: Load frozen snapshot from disk
	if err := m.Snapshot.Load(); err != nil {
		return err
	}

	// System 5: Initialize FTS holographic memory (best effort，失败不再静默吞错)
	if fts, err := memory.NewFTSStore(filepath.Join(cortexDir, "fts")); err == nil {
		m.FTSMemory = fts
	} else {
		log.Warnf("[Cortex] FTS memory init failed: %v", err)
		m.recordInitFailure("fts_memory", err)
	}

	// 初始化结构化记忆 Store（与 FTSStore 双写，保持数据一致）
	memCfg := memory.DefaultConfig()
	memCfg.DBPath = filepath.Join(cortexDir, "memories", "memory.db")
	if ms, err := memory.NewStore(memCfg); err == nil {
		m.memoryStore = ms
	} else {
		log.Warnf("[Cortex] memory Store init failed: %v", err)
		m.recordInitFailure("memory_store", err)
	}

	// 初始化 LLM 记忆抽取器（需要 provider 与 Store，行匹配作为 fallback）
	if m.memoryStore != nil && m.provider != nil {
		m.memoryExtractor = memory.NewMemoryExtractor(m.provider, m.memoryStore, memory.DefaultMemoryExtractorConfig())
	}

	// System 3: Start background review system
	if err := m.Review.Start(); err != nil {
		return err
	}

	// NEW: Load SOUL.md system personality
	if err := m.Soul.Load(); err != nil {
		return err
	}

	// NEW: Load USER.md profile
	if err := m.UserProfile.Load(); err != nil {
		return err
	}

	// NEW: Initialize Prompt Cache（失败不再静默吞错）
	if m.provider != nil {
		pc, err := NewPromptCache(m.provider, filepath.Join(cortexDir, "prompt_cache"))
		if err == nil {
			m.PromptCache = pc
		} else {
			log.Warnf("[Cortex] PromptCache init failed: %v", err)
			m.recordInitFailure("prompt_cache", err)
		}
	}

	// NEW: Initialize Trajectory Store（失败不再静默吞错）
	if ts, err := NewTrajectoryStore(cortexDir); err == nil {
		m.TrajectoryStore = ts
	} else {
		log.Warnf("[Cortex] TrajectoryStore init failed: %v", err)
		m.recordInitFailure("trajectory_store", err)
	}

	// NEW: Initialize GEPA Engine (self-evolution)（Trajectory→GEPA 接线；失败不再静默吞错）
	if m.provider != nil && m.TrajectoryStore != nil {
		gepa := NewGEPAEngine(cortexDir, m.provider, m.TrajectoryStore)
		if err := gepa.Start(nil); err == nil {
			m.GEPAEngine = gepa
		} else {
			log.Warnf("[Cortex] GEPA engine init failed: %v", err)
			m.recordInitFailure("gepa_engine", err)
		}
	}

	// 启动 GEPA 最优策略定期应用循环（将最优策略写入 SOUL.md）
	m.startGEPAStrategyApplier()

	// 启动记忆库维护循环（P2-3 清理 + P1-3 每日蒸馏）
	m.startMemoryMaintenance()

	return nil
}

// Stop 停止 Cortex 各后台循环（GEPA 引擎、最优策略应用循环、记忆维护循环），
// 并关闭记忆库数据库连接（Windows 下未关闭的 sqlite 文件句柄会锁定文件，
// 阻止临时目录清理；生产环境也应在停机时释放连接）。
func (m *Manager) Stop() {
	if m.gepaApplyCancel != nil {
		m.gepaApplyCancel()
		m.gepaApplyCancel = nil
	}
	if m.GEPAEngine != nil {
		m.GEPAEngine.Stop()
	}
	if m.memoryMaintenanceStop != nil {
		close(m.memoryMaintenanceStop)
		m.memoryMaintenanceStop = nil
		// 等维护循环退出后再关数据库，避免 goroutine 还在用已关闭的连接
		if m.memoryMaintenanceDone != nil {
			select {
			case <-m.memoryMaintenanceDone:
			case <-time.After(3 * time.Second):
				log.Warnf("[Cortex] memory maintenance loop did not stop within 3s")
			}
			m.memoryMaintenanceDone = nil
		}
	}
	if m.FTSMemory != nil {
		if err := m.FTSMemory.Close(); err != nil {
			log.Warnf("[Cortex] FTS memory close failed: %v", err)
		}
		m.FTSMemory = nil
	}
	if m.memoryStore != nil {
		if err := m.memoryStore.Close(); err != nil {
			log.Warnf("[Cortex] memory store close failed: %v", err)
		}
		m.memoryStore = nil
	}
}

// dynamicMemoryMaxChars 限制动态记忆注入文本的最大字符数（P0-1）
const dynamicMemoryMaxChars = 800

// RecallForInput 是动态记忆召回门面（P0-1）：以用户输入为查询，从结构化
// 记忆 Store 召回与当前工作区相关的高分记忆，压缩成 ≤800 字符的注入文本。
// agent 主循环每轮调用一次，把返回文本插到头部 system 之后，让静态快照
// 之外的历史记忆真正参与推理（修复「召回断路」）。
// memoryStore 未初始化或无命中时返回空串（调用方直接跳过注入）。
func (m *Manager) RecallForInput(query string) string {
	if m == nil || m.memoryStore == nil {
		return ""
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	// 查询截断：超长输入没有更好的召回效果，反而拖慢逐条评分
	if r := []rune(query); len(r) > 128 {
		query = string(r[:128])
	}

	tops := m.memoryStore.GetTopMemoriesScoped(query, 5, time.Now())
	if len(tops) == 0 {
		return ""
	}

	summary := strings.TrimSpace(memory.SummarizeMemories(tops))
	if r := []rune(summary); len(r) > dynamicMemoryMaxChars {
		summary = string(r[:dynamicMemoryMaxChars]) + "\n...(truncated)"
	}
	return summary
}

// startMemoryMaintenance 启动记忆库维护循环（P2-3 + P1-3）：
// 每 24 小时清理一次过期记忆——结构化 Store 删除「重要度低于 0.3 且
// 30 天未访问」的记录；FTS 会话库删除「重要度 < 4 且 90 天前」的记录。
// 同时驱动每日蒸馏器（P1-3）：超过 30 天的每日日志经 LLM 摘要合并进
// MEMORY.md 后删除（每日至多一次，状态文件幂等）。
// 启动时先跑一次；低频、幂等，失败仅告警不影响主流程。
func (m *Manager) startMemoryMaintenance() {
	if m.memoryMaintenanceStop != nil {
		return // 已在运行
	}
	m.memoryMaintenanceStop = make(chan struct{})
	memoryMaintenanceDone := make(chan struct{})
	m.memoryMaintenanceDone = memoryMaintenanceDone
	go func() {
		defer close(memoryMaintenanceDone)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		// P1-3: 每日蒸馏器（日志层 → MEMORY.md 分节合并）
		distiller := memory.NewDistiller(memory.DistillerConfig{
			LogDir:       filepath.Join(m.baseDir, "daily"),
			MemoryMDPath: filepath.Join(m.baseDir, "MEMORY.md"),
			Retention:    30 * 24 * time.Hour,
		}, m.memoryStore, m.FTSMemory, m.provider)
		// 蒸馏器需要更高频的检查（每日至多一次，内部状态文件幂等），
		// 用独立的 1h ticker 驱动
		distillStop := make(chan struct{})
		defer close(distillStop)
		distiller.StartMaintenance(time.Hour, distillStop)

		runCleanup := func() {
			if m.memoryStore != nil {
				cutoff := time.Now().Add(-30 * 24 * time.Hour)
				if n, err := m.memoryStore.CleanupExpired(cutoff, 0.3); err != nil {
					log.Warnf("[Cortex] memory cleanup failed: %v", err)
				} else if n > 0 {
					log.Infof("[Cortex] memory cleanup removed %d expired entries", n)
				}
			}
			if m.FTSMemory != nil {
				if n, err := m.FTSMemory.CleanupOld(90*24*time.Hour, 4); err != nil {
					log.Warnf("[Cortex] FTS cleanup failed: %v", err)
				} else if n > 0 {
					log.Infof("[Cortex] FTS cleanup removed %d expired entries", n)
				}
			}
		}

		runCleanup()
		for {
			select {
			case <-ticker.C:
				runCleanup()
			case <-m.memoryMaintenanceStop:
				return
			}
		}
	}()
}

// recordInitFailure 记录启动期初始化失败的子系统，供 GetSystemStatus 区分未初始化与初始化失败
func (m *Manager) recordInitFailure(system string, err error) {
	if m.initFailures == nil {
		m.initFailures = make(map[string]string)
	}
	if err != nil {
		m.initFailures[system] = err.Error()
	}
}

// initFailed 判断某子系统是否在启动期初始化失败
func (m *Manager) initFailed(system string) bool {
	_, ok := m.initFailures[system]
	return ok
}

// startGEPAStrategyApplier 启动一个定期循环，将 GEPA 当前最优策略应用到 SOUL.md
func (m *Manager) startGEPAStrategyApplier() {
	if m.GEPAEngine == nil || m.Soul == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.gepaApplyCancel = cancel
	go m.gepaStrategyApplyLoop(ctx)
}

// gepaStrategyApplyLoop 定期把最优策略应用到 SOUL.md
func (m *Manager) gepaStrategyApplyLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.applyGEPABestStrategy()
		}
	}
}

// applyGEPABestStrategy 将 GEPA 最优策略应用到 SOUL.md
func (m *Manager) applyGEPABestStrategy() {
	if m.GEPAEngine == nil || m.Soul == nil {
		return
	}
	current := m.Soul.GetSoul()
	optimized, err := m.GEPAEngine.ApplyBestStrategy(m.Soul, current)
	if err != nil {
		log.Warnf("[Cortex] GEPA ApplyBestStrategy failed: %v", err)
		return
	}
	if optimized != "" && optimized != current {
		if err := m.Soul.SetSoul(optimized); err != nil {
			log.Warnf("[Cortex] SOUL update from GEPA strategy failed: %v", err)
		}
	}
}

// OnUserMessage handles a new user message, triggering:
// - Layer 1: Perception (intent classification, noise detection)
// - Layer 2: Cognition (task planning, memory retrieval hints)
// - Turn counter increment
// - Nudge if threshold reached (async)
// - Skill creation flow initialization
func (m *Manager) OnUserMessage(input string) {
	if !m.enabled || m.Perception == nil {
		return
	}
	// Layer 1: Perception - understand the user's intent
	// This is the first step of Cortex three-layer architecture:
	// Perception → Decision → Execution
	m.LastPerception = m.Perception.Parse(input, nil)

	// Layer 2: Cognition - plan the task execution
	// Create execution plan, set max turns, and retrieve memory hints
	m.LastDecision = m.Cognition.CreatePlan(input, m.LastPerception)

	m.Trigger.OnUserMessage(input)
}

// OnTurnStart is called at the beginning of each LLM turn
// Freezes the memory snapshot for prefix cache protection
func (m *Manager) OnTurnStart() {
	if !m.enabled || m.Snapshot == nil {
		return
	}
	m.Snapshot.OnTurnStart()
}

// analyzeNewToolCalls feeds only the tool calls that haven't been analyzed yet
// into SkillCreator, preventing frequency inflation from re-counting.
func (m *Manager) analyzeNewToolCalls() {
	if m.Trigger == nil || m.SkillCreator == nil {
		return
	}
	allTools := m.Trigger.GetToolCalls()
	if m.lastAnalyzedToolIdx >= len(allTools) {
		return // No new tool calls since last analysis
	}
	newTools := allTools[m.lastAnalyzedToolIdx:]
	m.lastAnalyzedToolIdx = len(allTools)

	task := m.Trigger.GetCurrentTask()
	if len(newTools) >= 3 && task != "" {
		m.SkillCreator.AnalyzeToolSequence(task, newTools)
	}
}

// OnTurnEnd is called at the end of each LLM turn
// Triggers mid-turn learning: records tool calls for skill pattern detection
func (m *Manager) OnTurnEnd() {
	if !m.enabled || m.Trigger == nil {
		return
	}
	// Feed only NEW tool calls (since last analysis) into SkillCreator
	// to avoid re-counting the same accumulated calls on every turn.
	m.analyzeNewToolCalls()
}

// OnSessionEnd is called when a session completes
// Refreshes the memory snapshot and finalizes skill pattern analysis
// Extracts information from conversation history for memory building
func (m *Manager) OnSessionEnd() {
	if !m.enabled || m.Snapshot == nil {
		return
	}
	// ========== Memory Extraction from Conversation ==========
	// Extract key information and learn from conversation
	m.mu.RLock()
	hasHistory := len(m.conversationHistory) > 0
	m.mu.RUnlock()
	if hasHistory {
		m.extractAndLearnFromConversation()
	}

	// ========== Final skill analysis pass: analyze any remaining new tool calls ==========
	m.analyzeNewToolCalls()

	// Check and generate skills from patterns
	if m.SkillCreator != nil {
		generatedSkills := m.SkillCreator.GetGeneratedSkills()
		if len(generatedSkills) > 0 {
			log.Infof("[Cortex] Generated %d new skills from session patterns", len(generatedSkills))
		}
	}

	// ========== Refresh memory snapshot with latest changes ==========
	m.Snapshot.RefreshSnapshot()
}

// SetConversationHistory sets the conversation history for memory extraction
func (m *Manager) SetConversationHistory(history []struct {
	Role    string
	Content string
}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conversationHistory = history
}

// extractAndLearnFromConversation extracts information from conversation and updates memory
func (m *Manager) extractAndLearnFromConversation() {
	// Snapshot conversation history under lock to avoid holding lock during long operations
	m.mu.RLock()
	history := make([]struct {
		Role    string
		Content string
	}, len(m.conversationHistory))
	copy(history, m.conversationHistory)
	m.mu.RUnlock()

	// 增量处理：只抽取上次水位之后的新消息（P0 修复——历史是累积的，
	// 全量处理会让每轮把同样的内容重复抽取/重复写日志 N 遍）。
	m.mu.Lock()
	start := 0
	switch {
	case m.processedHistoryLen > 0 && m.processedHistoryLen <= len(history):
		start = m.processedHistoryLen
	case m.processedHistoryLen > len(history):
		// 历史被 truncateHistory 收缩：水位对齐当前长度，跳过本轮
		//（被保留的尾部都是已处理过的旧消息，重放会造成重复）。
		log.Debugf("[Cortex] history shrank (%d -> %d), realigning extraction watermark",
			m.processedHistoryLen, len(history))
		start = len(history)
	}
	m.processedHistoryLen = len(history)
	m.mu.Unlock()
	if start >= len(history) {
		return // 没有新消息
	}
	history = history[start:]

	var userOnly strings.Builder
	var nonSystem strings.Builder
	for _, msg := range history {
		if msg.Role == "system" {
			continue
		}
		// NOTE: do NOT wrap role with square brackets like "[user]: ...".
		// GLM mimics this format and starts wrapping every reply in [].
		// Use "Role: content" (no brackets).
		nonSystem.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Content))
		if msg.Role == "user" {
			userOnly.WriteString(msg.Content + "\n")
		}
	}
	userText := userOnly.String()
	nonSystemText := nonSystem.String()

	// 构造 provider.Message 切片供 LLM 记忆抽取器使用
	var provMsgs []provider.Message
	for _, msg := range history {
		if msg.Role == "system" {
			continue
		}
		provMsgs = append(provMsgs, provider.Message{Role: msg.Role, Content: msg.Content})
	}

	if m.Soul != nil && m.provider != nil {
		feedback := m.generateMemoryFeedback(userText)
		if feedback != "" {
			_ = m.Soul.UpdateFromFeedback(feedback)
		}
	}

	if m.UserProfile != nil {
		m.learnUserPreferences(userText)
	}

	if m.FTSMemory != nil || m.memoryStore != nil {
		m.extractAndStoreMemories(provMsgs, nonSystemText)
	}

	// P1-2: 对话追加写入每日日志（append-only，蒸馏器的原始数据源）。
	// best-effort：日志失败绝不影响主流程。
	m.appendDailyLog(history)
}

// appendDailyLog 把本轮对话追加写入当日日志文件（P1-2）。
// 懒构造 DailyLog，首次调用时初始化；system 消息不落日志。
func (m *Manager) appendDailyLog(history []struct {
	Role    string
	Content string
}) {
	if m.dailyLog == nil {
		m.dailyLog = memory.NewDailyLog(filepath.Join(m.baseDir, "daily"))
	}
	for _, msg := range history {
		if msg.Role == "system" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		content := msg.Content
		if r := []rune(content); len(r) > 2000 {
			content = string(r[:2000]) + "..."
		}
		if err := m.dailyLog.Append(msg.Role, content); err != nil {
			log.Debugf("[Cortex] daily log append failed: %v", err)
		}
	}
}

// generateMemoryFeedback generates feedback for SOUL.md from conversation
// 收紧匹配：仅收集明确表达偏好的语句，排除 "I would like" 等非偏好句式，降低误报率
func (m *Manager) generateMemoryFeedback(conversation string) string {
	var feedback strings.Builder

	// 非偏好句式：包含这些子串的行即便含 like/always 也不应灌入 SOUL
	nonPreferenceMarkers := []string{
		"would like", "i'd like", "id like", "i would like",
		"would you like", "do you like", "if you like", "as you like",
		"feel like", "looks like", "sounds like", "something like",
		"seems like", "like to ask", "like to know", "like to request",
	}

	lines := strings.Split(conversation, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(line)

		// 跳过自动生成标记，避免把已写入 SOUL 的内容再次回灌
		if strings.Contains(lower, "learn from user preferences") ||
			strings.Contains(lower, "[auto-generated from interactions]") ||
			strings.HasPrefix(trimmed, "## Learned Preferences") {
			continue
		}

		// 排除非偏好句式（如 "I would like to..." 这类请求/意愿表达）
		isNonPreference := false
		for _, marker := range nonPreferenceMarkers {
			if strings.Contains(lower, marker) {
				isNonPreference = true
				break
			}
		}
		if isNonPreference {
			continue
		}

		// 收紧匹配：仅当行明确以偏好/喜好表达开头时才收集
		if strings.Contains(lower, "i prefer") ||
			strings.Contains(lower, "i like") ||
			strings.Contains(lower, "i enjoy") ||
			strings.Contains(lower, "i don't like") ||
			strings.Contains(lower, "i do not like") ||
			strings.Contains(lower, "i hate") ||
			strings.Contains(lower, "i always") ||
			strings.Contains(lower, "i usually") ||
			strings.Contains(lower, "i find") && strings.Contains(lower, "helpful") ||
			strings.Contains(lower, "我喜欢") ||
			strings.Contains(lower, "我偏好") ||
			strings.Contains(lower, "我讨厌") ||
			strings.Contains(lower, "我总是") ||
			strings.Contains(lower, "我通常") {
			feedback.WriteString(line + "\n")
		}
	}

	return feedback.String()
}

// learnUserPreferences extracts and learns user preferences
// 支持英文与中文偏好模式匹配
func (m *Manager) learnUserPreferences(conversation string) {
	// 偏好模式（英文 + 中文）
	preferencePatterns := []struct {
		pattern string
		key     string
	}{
		// 英文模式
		{"preferred language", "language"},
		{"like to use", "tool_preference"},
		{"usually work with", "work_style"},
		{"prefer detailed", "response_style"},
		{"like brief", "response_style"},
		{"i prefer", "tool_preference"},
		{"i like to use", "tool_preference"},
		// 中文模式
		{"用户喜欢", "tool_preference"},
		{"用户偏好", "tool_preference"},
		{"习惯使用", "tool_preference"},
		{"喜欢用", "tool_preference"},
		{"偏好语言", "language"},
		{"喜欢详细", "response_style"},
		{"喜欢简洁", "response_style"},
		{"通常使用", "work_style"},
	}

	// ToLower 不影响中文字符，统一用小写做匹配
	lower := strings.ToLower(conversation)
	for _, p := range preferencePatterns {
		if !strings.Contains(lower, p.pattern) {
			continue
		}
		// 提取模式周围的上下文
		idx := strings.Index(lower, p.pattern)
		start := 0
		if idx-50 > 0 {
			start = idx - 50
		}
		end := len(lower)
		if idx+len(p.pattern)+50 < end {
			end = idx + len(p.pattern) + 50
		}
		context := strings.TrimSpace(lower[start:end])
		_ = m.UserProfile.LearnPreference(p.key, p.pattern, context)
	}
}

// extractAndStoreMemories 抽取并存储重要信息
// 优先使用 LLM 抽取器（internal/memory.MemoryExtractor），失败时回退到行匹配；
// 同时写入 Store（结构化）与 FTSStore（全文检索），保持两者数据一致
func (m *Manager) extractAndStoreMemories(messages []provider.Message, conversation string) {
	ctx := context.Background()

	// 优先使用 LLM 抽取器
	if m.memoryExtractor != nil && m.provider != nil && len(messages) > 0 {
		memories, err := m.memoryExtractor.ExtractMemories(ctx, messages, "")
		if err == nil && len(memories) > 0 {
			// 写入 Store（结构化存储，含衰减/检索能力）
			if m.memoryStore != nil {
				_ = m.memoryExtractor.StoreMemories(memories)
			}
			// 同步写入 FTSStore（全文检索）
			if m.FTSMemory != nil {
				for _, mem := range memories {
					_ = m.FTSMemory.Add(memoryRecordFromMemory(mem))
				}
			}
			return
		}
		// LLM 抽取失败则回退到行匹配
		if err != nil {
			log.Warnf("[Cortex] LLM memory extraction failed, fallback to line matching: %v", err)
		}
	}

	// Fallback：简陋行匹配（LLM 不可用或抽取失败时），双写 Store 与 FTSStore
	m.fallbackLineMatchStore(conversation)
}

// fallbackLineMatchStore 用简陋行匹配抽取记忆，双写 Store 与 FTSStore
func (m *Manager) fallbackLineMatchStore(conversation string) {
	lines := strings.Split(conversation, "\n")
	for _, line := range lines {
		// 跳过过短行与系统消息
		// Skip too-short lines and system markers (legacy "[system]" and
		// the new "system:" prefix both should be skipped here).
		if len(line) < 20 || strings.HasPrefix(line, "[system]") || strings.HasPrefix(line, "system:") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "learned") ||
			strings.Contains(lower, "important") ||
			strings.Contains(lower, "remember") {
			// 写入 FTSStore（全文检索）
			if m.FTSMemory != nil {
				record := &memory.MemoryRecord{
					Content:     line,
					ContentType: string(memory.TypeKnowledge),
					Importance:  5,
				}
				_ = m.FTSMemory.Add(record)
			}
			// 写入 Store（结构化存储）
			if m.memoryStore != nil {
				mem := &memory.Memory{
					Type:       memory.TypeKnowledge,
					Content:    line,
					Importance: 0.5,
					Source:     "fallback",
				}
				_ = m.memoryStore.Store(mem)
			}
		}
	}
}

// memoryRecordFromMemory 将结构化 Memory 转换为 FTSStore 的 MemoryRecord
// Importance 范围 0-1，MemoryRecord.Importance 为 int，映射到 0-10。
// 补齐 SessionID（会话溯源）与 scope/categories→tags（P1-4），
// 保证 FTS 双写副本携带与主库一致的检索维度。
func memoryRecordFromMemory(mem *memory.Memory) *memory.MemoryRecord {
	importance := int(mem.Importance * 10)
	if importance < 0 {
		importance = 0
	}
	tags := append([]string{}, mem.Categories...)
	if mem.Scope != "" {
		tags = append(tags, "scope:"+mem.Scope)
	}
	return &memory.MemoryRecord{
		SessionID:   mem.SessionID,
		Content:     mem.Content,
		ContentType: string(mem.Type),
		Importance:  importance,
		Tags:        tags,
		CreatedAt:   mem.CreatedAt,
	}
}

// GetPromptContext returns the memory context to include in the system prompt.
// Uses the frozen snapshot, not the latest version. A disabled Manager is
// created as an empty shell (Snapshot == nil) and must return "" instead of
// panicking — agents only nil-check the Manager itself.
func (m *Manager) GetPromptContext() string {
	if m == nil || m.Snapshot == nil {
		return ""
	}
	return m.Snapshot.GetMemoryForPrompt()
}

// GetUserContext returns the user profile for the system prompt.
// Uses the frozen snapshot; safe on a disabled (shell) Manager.
func (m *Manager) GetUserContext() string {
	if m == nil || m.Snapshot == nil {
		return ""
	}
	return m.Snapshot.GetUserForPrompt()
}

// AppendMemory adds a line to the memory file
// Writes to disk immediately but does NOT refresh frozen snapshot
func (m *Manager) AppendMemory(line string) error {
	if m == nil || m.Snapshot == nil {
		return fmt.Errorf("cortex memory disabled")
	}
	return m.Snapshot.AppendToMemory(line)
}

// AppendUser adds a line to the user profile
// Writes to disk immediately but does NOT refresh frozen snapshot
func (m *Manager) AppendUser(line string) error {
	if m == nil || m.Snapshot == nil {
		return fmt.Errorf("cortex memory disabled")
	}
	return m.Snapshot.AppendToUser(line)
}

// GetTurnCount returns the current turn count
func (m *Manager) GetTurnCount() int {
	return m.Trigger.GetTurnCount()
}

// Reset resets the turn counter for a new session
func (m *Manager) Reset() {
	if m.Trigger != nil {
		m.Trigger.Reset()
	}
	m.lastAnalyzedToolIdx = 0
}

// GetLastPerception returns the result from the perception layer
// This can be used by the decision layer to:
// - Adjust max turns based on task complexity
// - Change tool selection based on intent
// - Request clarification if noise is detected
func (m *Manager) GetLastPerception() *perception.PerceptionResult {
	return m.LastPerception
}

// GetIntent returns the classified intent type
func (m *Manager) GetIntent() perception.IntentType {
	if m.LastPerception == nil {
		return perception.IntentUnknown
	}
	return m.LastPerception.Intent.Type
}

// GetTaskComplexity returns the estimated task complexity
func (m *Manager) GetTaskComplexity() perception.TaskComplexity {
	if m.LastPerception == nil {
		return perception.ComplexitySimple
	}
	return m.LastPerception.Intent.Complexity
}

// HasNoise returns true if noise was detected in input
func (m *Manager) HasNoise() bool {
	if m.LastPerception == nil {
		return false
	}
	return m.LastPerception.Noise.HasNoise
}

// GetLastDecision returns the last cognition decision
func (m *Manager) GetLastDecision() *cognition.Decision {
	return m.LastDecision
}

// GetExecutionPlan returns the current execution plan
func (m *Manager) GetExecutionPlan() *cognition.ExecutionPlan {
	if m.LastDecision == nil {
		return nil
	}
	return m.LastDecision.Plan
}

// GetRecommendedMaxTurns returns the recommended max turns based on task complexity
func (m *Manager) GetRecommendedMaxTurns() int {
	if m.LastDecision == nil {
		return 10 // Default
	}
	return m.LastDecision.MaxTurns
}

// NeedsClarification returns true if clarification should be requested
func (m *Manager) NeedsClarification() bool {
	if m.LastDecision == nil {
		return false
	}
	return m.LastDecision.ClarificationNeeded
}

// GetClarificationQuestion returns the question to ask user for clarification
func (m *Manager) GetClarificationQuestion() string {
	if m.LastDecision == nil {
		return ""
	}
	return m.LastDecision.ClarificationQuestion
}

// ShouldUseSubAgents returns true if sub-agents should be enabled
func (m *Manager) ShouldUseSubAgents() bool {
	if m.LastDecision == nil {
		return false
	}
	return m.LastDecision.EnableSubAgents
}

// GetRetrievalHints returns hints for memory retrieval
func (m *Manager) GetRetrievalHints() []cognition.RetrievalHint {
	if m.LastDecision == nil {
		return nil
	}
	return m.LastDecision.RetrievalHints
}

// GetMemoryVersion returns the current memory version
func (m *Manager) GetMemoryVersion() int {
	if m == nil || m.Snapshot == nil {
		return 0
	}
	return m.Snapshot.GetVersion()
}

// ========== Phase 4: Execution Layer (Layer 3) ==========

// StartExecution begins a new execution with checkpoint support
func (m *Manager) StartExecution(task string) *execution.Progress {
	if m.LastDecision == nil || m.LastDecision.Plan == nil {
		// No plan available, create a simple one
		return nil
	}

	// Start checkpoint
	m.LastCheckpoint = m.Execution.StartCheckpoint("", m.LastDecision.Plan)

	// Return initial progress
	return m.Execution.GetProgress(m.LastCheckpoint)
}

// UpdateExecutionStep updates the current step
func (m *Manager) UpdateExecutionStep(stepID int, stepName string) {
	if m.LastCheckpoint != nil {
		m.Execution.UpdateCheckpoint(m.LastCheckpoint, stepID, stepName)
	}
}

// GetExecutionProgress returns the current execution progress
func (m *Manager) GetExecutionProgress() *execution.Progress {
	if m.LastCheckpoint == nil {
		return nil
	}
	return m.Execution.GetProgress(m.LastCheckpoint)
}

// FindResumableTask checks if there's a resumable checkpoint
func (m *Manager) FindResumableTask(description string) *execution.Checkpoint {
	return m.Execution.FindCheckpoint(description)
}

// CompleteExecution marks execution as successfully completed
func (m *Manager) CompleteExecution() {
	if m.LastCheckpoint != nil {
		m.Execution.CompleteCheckpoint(m.LastCheckpoint)
	}
}

// SuggestRecoveryAction suggests what to do after failure
func (m *Manager) SuggestRecoveryAction(err error) execution.RecoveryAction {
	if m.LastCheckpoint == nil {
		return execution.RecoveryAbort
	}
	return m.Execution.SuggestRecoveryAction(m.LastCheckpoint, err)
}

// ========== Phase 4: FTS Memory (System 5) ==========

// SearchMemory performs full-text search across all conversation history
func (m *Manager) SearchMemory(query string, limit int) []memory.SearchResult {
	if m.FTSMemory == nil {
		return nil
	}
	results, _ := m.FTSMemory.Search(query, limit)
	return results
}

// AddMemoryInsight stores a learned insight in FTS memory
func (m *Manager) AddMemoryInsight(insight string, importance int) error {
	if m.FTSMemory == nil {
		return nil
	}
	return m.FTSMemory.AddInsight("", insight, importance)
}

// GetMemoryStats returns statistics about the memory store
func (m *Manager) GetMemoryStats() map[string]interface{} {
	if m.FTSMemory == nil {
		return nil
	}
	stats, _ := m.FTSMemory.GetStats()
	return stats
}

// ========== Phase 4: Skill Evolution (System 6) ==========

// AnalyzeToolSequence analyzes a tool sequence for pattern recognition
func (m *Manager) AnalyzeToolSequence(task string, tools []string) {
	m.SkillCreator.AnalyzeToolSequence(task, tools)
}

// GetDetectedPatterns returns all currently detected patterns
func (m *Manager) GetDetectedPatterns() []skills.Pattern {
	return m.SkillCreator.GetPatterns()
}

// GetGeneratedSkills returns all auto-generated skills
func (m *Manager) GetGeneratedSkills() []string {
	return m.SkillCreator.GetGeneratedSkills()
}

// GetSkillEvolutionStats returns statistics about skill generation
func (m *Manager) GetSkillEvolutionStats() map[string]interface{} {
	return m.SkillCreator.GetStats()
}

// ========== Full System Health Check ==========

// GetSystemStatus returns status of all Cortex systems
func (m *Manager) GetSystemStatus() map[string]interface{} {
	status := make(map[string]interface{})
	totalSystems := 0
	totalReady := 0

	// Three-Layer Architecture
	totalSystems++
	if m.Perception != nil {
		status["layer_1_perception"] = "ready"
		totalReady++
	} else {
		status["layer_1_perception"] = "not_initialized"
	}
	totalSystems++
	if m.Cognition != nil {
		status["layer_2_cognition"] = "ready"
		totalReady++
	} else {
		status["layer_2_cognition"] = "not_initialized"
	}
	totalSystems++
	if m.Execution != nil {
		status["layer_3_execution"] = "ready"
		totalReady++
	} else {
		status["layer_3_execution"] = "not_initialized"
	}

	// Six Systems
	totalSystems++
	if m.Trigger != nil {
		status["system_1_message_trigger"] = "ready"
		totalReady++
	} else {
		status["system_1_message_trigger"] = "not_initialized"
	}
	totalSystems++
	if m.Trigger != nil {
		status["system_2_nudge_mechanism"] = "ready"
		totalReady++
	} else {
		status["system_2_nudge_mechanism"] = "not_initialized"
	}
	totalSystems++
	if m.Review != nil {
		status["system_3_background_review"] = "ready"
		totalReady++
	} else {
		status["system_3_background_review"] = "not_initialized"
	}
	totalSystems++
	if m.Snapshot != nil {
		status["system_4_frozen_snapshot"] = "ready"
		totalReady++
	} else {
		status["system_4_frozen_snapshot"] = "not_initialized"
	}
	totalSystems++
	if m.FTSMemory != nil {
		status["system_5_fts_memory"] = "ready"
		totalReady++
	} else if m.initFailed("fts_memory") {
		status["system_5_fts_memory"] = "init_failed"
	} else {
		status["system_5_fts_memory"] = "not_initialized"
	}
	totalSystems++
	if m.SkillCreator != nil {
		status["system_6_skill_evolution"] = "ready"
		totalReady++
	} else {
		status["system_6_skill_evolution"] = "not_initialized"
	}

	// Summary
	status["total_systems_ready"] = totalReady
	status["overall_status"] = fmt.Sprintf("%d/%d ready", totalReady, totalSystems)

	return status
}
