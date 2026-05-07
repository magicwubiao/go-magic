package cortex

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/execution"
	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/review"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/trigger"
)

// Manager integrates all six Cortex Agent systems:
// 1. User Message Trigger
// 2. Periodic Nudge Mechanism
// 3. Background Review System
// 4. Dual File Storage (MEMORY.md + USER.md)
// 5. Holographic Memory (SQLite FTS5 - coming soon)
// 6. Memory Manager with Frozen Snapshot
type Manager struct {
	baseDir        string
	Snapshot       *memory.SnapshotManager  // System 4: Frozen snapshot memory
	Trigger        *trigger.MessageTrigger  // System 1 + 2: Nudge mechanism
	Review         *review.BackgroundReview // System 3: Background review
	Perception     *perception.Parser      // Layer 1: Intent classification
	Cognition      *cognition.Planner       // Layer 2: Planning and decision making
	Execution      *execution.Manager        // Layer 3: Checkpoint + Resume
	FTSMemory      *memory.FTSStore        // System 5: FTS full-text search
	SkillCreator   *skills.EnhancedAutoCreator // System 6: Auto skill evolution
	LastPerception *perception.PerceptionResult
	LastDecision   *cognition.Decision   // Last cognition decision
	LastCheckpoint *execution.Checkpoint // Current execution checkpoint
}

// NewManager creates a new Cortex integration manager
// Initializes all 6 Cortex systems and 3-layer architecture
func NewManager(baseDir string) *Manager {
	// Note: FTSStore may fail if sqlite3 is not available,
	// so we initialize it separately in Start()
	mgr := &Manager{
		baseDir:      baseDir,
		Snapshot:     memory.NewSnapshotManager(baseDir),
		Trigger:      trigger.NewMessageTrigger(),
		Review:       review.NewBackgroundReview(filepath.Join(baseDir, "reviews")),
		Perception:   perception.NewParser(),
		Cognition:    cognition.NewPlanner(),
		Execution:    execution.NewManager(baseDir),
		SkillCreator: skills.NewEnhancedAutoCreator(baseDir),
		// FTSMemory initialized in Start()
	}

	// Wire up the connections between systems
	mgr.setupConnections()

	return mgr
}

// setupConnections wires the six systems together
func (m *Manager) setupConnections() {
	// Trigger -> Review: Nudge triggers background review
	m.Trigger.RegisterNudgeHandler(func() {
		turnCount := m.Trigger.GetTurnCount()
		// In a real implementation, we would pass actual tool call history
		m.Review.TriggerNudgeReview(turnCount, []string{})
	})
}

// Start initializes all six Cortex systems
// Systems started in order of dependency:
//   1. Memory systems (Snapshot, FTS)
//   2. Review system
//   3. Skill evolution system
//   4. Trigger system
func (m *Manager) Start() error {
	// System 4: Load frozen snapshot from disk
	if err := m.Snapshot.Load(); err != nil {
		return err
	}

	// System 5: Initialize FTS holographic memory (best effort)
	if fts, err := memory.NewFTSStore(filepath.Join(m.baseDir, "fts")); err == nil {
		m.FTSMemory = fts
	}

	// System 3: Start background review system
	if err := m.Review.Start(); err != nil {
		return err
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

	// Record perception result in memory for learning
	// The agent can use this to adapt its behavior based on task type
	m.Snapshot.AppendToMemory("- Task type: " + string(m.LastPerception.Intent.Type))
	if m.LastPerception.Intent.Type == perception.IntentTask {
		m.Snapshot.AppendToMemory("- Task complexity: " + string(m.LastPerception.Intent.Complexity))
	}

	// Layer 2: Cognition - plan the task execution
	// Create execution plan, set max turns, and retrieve memory hints
	m.LastDecision = m.Cognition.CreatePlan(input, m.LastPerception)

	// Record cognition decisions
	if m.LastDecision.Plan != nil {
		m.Snapshot.AppendToMemory("- Plan steps: " + strconv.Itoa(len(m.LastDecision.Plan.Steps)))
		m.Snapshot.AppendToMemory("- Estimated turns: " + strconv.Itoa(m.LastDecision.Plan.TotalEstimatedTurns))
	}

	m.Trigger.OnUserMessage(input)
}

// OnTurnStart is called at the beginning of each LLM turn
// Freezes the memory snapshot for prefix cache protection
func (m *Manager) OnTurnStart() {
	m.Snapshot.OnTurnStart()
}

// OnTurnEnd is called at the end of each LLM turn
func (m *Manager) OnTurnEnd() {
	// Can trigger mid-turn learning here if needed
}

// OnSessionEnd is called when a session completes
// Refreshes the memory snapshot with latest changes
func (m *Manager) OnSessionEnd() {
	m.Snapshot.RefreshSnapshot()
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
