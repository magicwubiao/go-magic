package cortex

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/execution"
	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/review"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/trigger"
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
// 12. GEPA Self-Evolution Engine (NEW)
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
	cortexDir := filepath.Join(baseDir, "cortex")

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

// setupConnections wires the six systems together
func (m *Manager) setupConnections() {
	// Skip if cortex is disabled
	if !m.enabled {
		return
	}

	// Trigger -> Review: Nudge triggers background review
	m.Trigger.RegisterNudgeHandler(func() {
		turnCount := m.Trigger.GetTurnCount()
		// In a real implementation, we would pass actual tool call history
		m.Review.TriggerNudgeReview(turnCount, []string{}, nil)
	})
}

// BindSkillsManager connects the cortex skill auto creator to the skills Manager
// so auto-generated skills are visible via /api/skills.
func (m *Manager) BindSkillsManager(sm *skills.Manager) {
	if m.SkillCreator != nil && sm != nil {
		m.SkillCreator.SetManager(sm)
	}
}

// Start initializes all Cortex systems
// Systems started in order of dependency:
func (m *Manager) Start() error {
	// Skip if cortex is disabled
	if !m.enabled {
		return nil
	}

	cortexDir := filepath.Join(m.baseDir, "cortex")

	// System 4: Load frozen snapshot from disk
	if err := m.Snapshot.Load(); err != nil {
		return err
	}

	// System 5: Initialize FTS holographic memory (best effort)
	if fts, err := memory.NewFTSStore(filepath.Join(cortexDir, "fts")); err == nil {
		m.FTSMemory = fts
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

	// NEW: Initialize Prompt Cache
	if m.provider != nil {
		pc, err := NewPromptCache(m.provider, filepath.Join(cortexDir, "prompt_cache"))
		if err == nil {
			m.PromptCache = pc
		}
	}

	// NEW: Initialize Trajectory Store
	ts, err := NewTrajectoryStore(cortexDir)
	if err == nil {
		m.TrajectoryStore = ts
	}

	// NEW: Initialize GEPA Engine (self-evolution)
	if m.provider != nil && m.TrajectoryStore != nil {
		gepa := NewGEPAEngine(cortexDir, m.provider, m.TrajectoryStore)
		if err := gepa.Start(nil); err == nil {
			m.GEPAEngine = gepa
		}
	}

	return nil
}

// OnUserMessage handles a new user message, triggering:
// - Layer 1: Perception (intent classification, noise detection)
// - Layer 2: Cognition (task planning, memory retrieval hints)
// - Turn counter increment
// - Nudge if threshold reached (async)
// - Skill creation flow initialization
func (m *Manager) OnUserMessage(input string) {
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
	m.Snapshot.OnTurnStart()
}

// OnTurnEnd is called at the end of each LLM turn
// Triggers mid-turn learning: records tool calls for skill pattern detection
func (m *Manager) OnTurnEnd() {
	// Feed tool call data into Skill Evolution (System 6)
	// This is called after each tool execution round
	tools := m.Trigger.GetToolCalls()
	task := m.Trigger.GetCurrentTask()
	if len(tools) >= 3 && task != "" {
		m.SkillCreator.AnalyzeToolSequence(task, tools)
	}
}

// OnSessionEnd is called when a session completes
// Refreshes the memory snapshot and finalizes skill pattern analysis
// Extracts information from conversation history for memory building
func (m *Manager) OnSessionEnd() {
	// ========== Memory Extraction from Conversation ==========
	// Extract key information and learn from conversation
	if len(m.conversationHistory) > 0 {
		m.extractAndLearnFromConversation()
	}

	// ========== Final skill analysis pass with all accumulated tool calls ==========
	tools := m.Trigger.GetToolCalls()
	task := m.Trigger.GetCurrentTask()
	if len(tools) >= 3 && task != "" {
		m.SkillCreator.AnalyzeToolSequence(task, tools)

		// ========== Check and generate skills from patterns ==========
		// After analyzing, check if any patterns are ready for skill generation
		generatedSkills := m.SkillCreator.GetGeneratedSkills()
		if len(generatedSkills) > 0 {
			log.Printf("[Cortex] Generated %d new skills from session patterns", len(generatedSkills))
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
	m.conversationHistory = history
}

// extractAndLearnFromConversation extracts information from conversation and updates memory
func (m *Manager) extractAndLearnFromConversation() {
	var userOnly strings.Builder
	var nonSystem strings.Builder
	for _, msg := range m.conversationHistory {
		if msg.Role == "system" {
			continue
		}
		nonSystem.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		if msg.Role == "user" {
			userOnly.WriteString(msg.Content + "\n")
		}
	}
	userText := userOnly.String()
	nonSystemText := nonSystem.String()

	if m.Soul != nil && m.provider != nil {
		feedback := m.generateMemoryFeedback(userText)
		if feedback != "" {
			_ = m.Soul.UpdateFromFeedback(feedback)
		}
	}

	if m.UserProfile != nil {
		m.learnUserPreferences(userText)
	}

	if m.FTSMemory != nil {
		m.extractAndStoreMemories(nonSystemText)
	}
}

// generateMemoryFeedback generates feedback for SOUL.md from conversation
func (m *Manager) generateMemoryFeedback(conversation string) string {
	var feedback strings.Builder

	lines := strings.Split(conversation, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "learn from user preferences") ||
			strings.Contains(lower, "[auto-generated from interactions]") ||
			strings.HasPrefix(strings.TrimSpace(line), "## Learned Preferences") {
			continue
		}
		if strings.Contains(lower, "prefer") ||
			strings.Contains(lower, "like") ||
			strings.Contains(lower, "don't like") ||
			strings.Contains(lower, "always") {
			feedback.WriteString(line + "\n")
		}
	}

	return feedback.String()
}

// learnUserPreferences extracts and learns user preferences
func (m *Manager) learnUserPreferences(conversation string) {
	// Simple pattern matching for preferences
	preferencePatterns := []struct {
		pattern string
		key     string
	}{
		{"preferred language", "language"},
		{"like to use", "tool_preference"},
		{"usually work with", "work_style"},
		{"prefer detailed", "response_style"},
		{"like brief", "response_style"},
	}

	for _, p := range preferencePatterns {
		if strings.Contains(strings.ToLower(conversation), p.pattern) {
			// Extract context around the pattern
			idx := strings.Index(strings.ToLower(conversation), p.pattern)
			start := 0
			if idx-50 > 0 {
				start = idx - 50
			}
			end := len(conversation)
			if idx+len(p.pattern)+50 < len(conversation) {
				end = idx + len(p.pattern) + 50
			}
			context := strings.TrimSpace(conversation[start:end])
			_ = m.UserProfile.LearnPreference(p.key, p.pattern, context)
		}
	}
}

// extractAndStoreMemories extracts and stores important information
func (m *Manager) extractAndStoreMemories(conversation string) {
	// Store conversation summary as a memory entry
	memoryTypes := []memory.MemoryType{memory.TypeAgent, memory.TypeKnowledge}

	for _, memType := range memoryTypes {
		// Extract key points (simple implementation)
		lines := strings.Split(conversation, "\n")
		for _, line := range lines {
			// Skip very short lines and system messages
			if len(line) < 20 || strings.HasPrefix(line, "[system]") {
				continue
			}
			// Store significant lines as memories
			if strings.Contains(line, "learned") ||
				strings.Contains(line, "important") ||
				strings.Contains(line, "remember") {
				record := &memory.MemoryRecord{
					Content:     line,
					ContentType: string(memType),
					Importance:  5,
				}
				_ = m.FTSMemory.Add(record)
			}
		}
	}
}

// GetPromptContext returns the memory context to include in system prompt
// Uses the frozen snapshot, not the latest version
func (m *Manager) GetPromptContext() string {
	return m.Snapshot.GetMemoryForPrompt()
}

// GetUserContext returns the user profile for system prompt
// Uses the frozen snapshot
func (m *Manager) GetUserContext() string {
	return m.Snapshot.GetUserForPrompt()
}

// AppendMemory adds a line to the memory file
// Writes to disk immediately but does NOT refresh frozen snapshot
func (m *Manager) AppendMemory(line string) error {
	return m.Snapshot.AppendToMemory(line)
}

// AppendUser adds a line to the user profile
// Writes to disk immediately but does NOT refresh frozen snapshot
func (m *Manager) AppendUser(line string) error {
	return m.Snapshot.AppendToUser(line)
}

// GetTurnCount returns the current turn count
func (m *Manager) GetTurnCount() int {
	return m.Trigger.GetTurnCount()
}

// Reset resets the turn counter for a new session
func (m *Manager) Reset() {
	m.Trigger.Reset()
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

// GetSystemStatus returns status of all six Cortex systems
func (m *Manager) GetSystemStatus() map[string]interface{} {
	status := make(map[string]interface{})

	// Three-Layer Architecture
	status["layer_1_perception"] = "ready"
	status["layer_2_cognition"] = "ready"
	if m.Execution != nil {
		status["layer_3_execution"] = "ready"
	} else {
		status["layer_3_execution"] = "not_initialized"
	}

	// Six Systems
	status["system_1_message_trigger"] = "ready"
	status["system_2_nudge_mechanism"] = "ready"
	status["system_3_background_review"] = "ready"
	status["system_4_frozen_snapshot"] = "ready"
	if m.FTSMemory != nil {
		status["system_5_fts_memory"] = "ready"
	} else {
		status["system_5_fts_memory"] = "optional_disabled"
	}
	status["system_6_skill_evolution"] = "ready"

	// Summary
	totalReady := 0
	for _, v := range status {
		if v == "ready" {
			totalReady++
		}
	}
	status["total_systems_ready"] = totalReady
	status["overall_status"] = fmt.Sprintf("%d/9 ready", totalReady)

	return status
}
