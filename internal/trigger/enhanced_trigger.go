package trigger

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// NudgePriority represents the importance level of a nudge
type NudgePriority int

const (
	NudgePriorityLow    NudgePriority = iota
	NudgePriorityNormal
	NudgePriorityHigh
	NudgePriorityCritical
)

// NudgeType represents the type of nudge
type NudgeType string

const (
	NudgeTypePeriodic       NudgeType = "periodic"
	NudgeTypePattern       NudgeType = "pattern"
	NudgeTypeReminder      NudgeType = "reminder"
	NudgeTypeSuggestion    NudgeType = "suggestion"
	NudgeTypeErrorRecovery NudgeType = "error_recovery"
	NudgeTypeOpportunity   NudgeType = "opportunity"
)

// Nudge represents a single nudge message/action
type Nudge struct {
	ID          string        `json:"id"`
	Type        NudgeType     `json:"type"`
	Priority    NudgePriority `json:"priority"`
	Message     string        `json:"message"`
	Context     string        `json:"context,omitempty"`
	Actions     []NudgeAction `json:"actions,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	ExpiresAt   time.Time     `json:"expires_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NudgeAction represents a possible action for a nudge
type NudgeAction struct {
	Label string `json:"label"`
	Type  string `json:"type"` // "skill", "tool", "link"
	Value string `json:"value"`
}

// UserNudgePreference tracks user preferences for nudges
type UserNudgePreference struct {
	EnabledTypes     map[NudgeType]bool  `json:"enabled_types"`
	FrequencyLimit   time.Duration        `json:"frequency_limit"`
	QuietHoursStart  time.Time            `json:"quiet_hours_start"`
	QuietHoursEnd    time.Time            `json:"quiet_hours_end"`
	LastNudgeTime    time.Time            `json:"last_nudge_time"`
	ResponseRate     float64              `json:"response_rate"` // How often user responds to nudges
}

// NudgeMetrics tracks nudge effectiveness
type NudgeMetrics struct {
	TotalSent       int              `json:"total_sent"`
	TotalShown      int              `json:"total_shown"`
	TotalDismissed  int              `json:"total_dismissed"`
	TotalAccepted   int              `json:"total_accepted"`
	ByType          map[NudgeType]int `json:"by_type"`
	ByPriority      map[NudgePriority]int `json:"by_priority"`
	AvgResponseTime time.Duration    `json:"avg_response_time"`
}

// EnhancedMessageTrigger provides intelligent nudge mechanism
type EnhancedMessageTrigger struct {
	mu             sync.RWMutex
	turnCount      int
	nudgeThreshold int
	nudgeHandlers  []NudgeHandler
	
	// Intelligent threshold tracking
	dynamicThreshold int
	baseThreshold    int
	frequencyHistory []time.Time
	
	// Context awareness
	currentContext    string
	contextHistory    []string
	contextDuration   map[string]time.Duration
	
	// Cooldown mechanism
	lastNudgeTime     time.Time
	minNudgeInterval  time.Duration
	recentNudges      *list.List
	maxRecentNudges   int
	
	// User preference learning
	preferences   *UserNudgePreference
	metrics       *NudgeMetrics
	
	// Scheduled nudges
	scheduledNudges []ScheduledNudge
	timer           *time.Timer

	// Task tracking
	taskStartTime time.Time
	currentTask  string
}

// NudgeHandler is the interface for nudge handlers
type NudgeHandler interface {
	HandleNudge(nudge *Nudge) bool // Returns true if nudge was handled
	GetName() string
}

// SimpleNudgeHandler is a function-based nudge handler
type SimpleNudgeHandler struct {
	name    string
	handler func(*Nudge) bool
}

func (h *SimpleNudgeHandler) HandleNudge(nudge *Nudge) bool {
	return h.handler(nudge)
}

func (h *SimpleNudgeHandler) GetName() string {
	return h.name
}

// ScheduledNudge represents a nudge scheduled for future delivery
type ScheduledNudge struct {
	Nudge     *Nudge
	DeliverAt time.Time
}

// NewEnhancedMessageTrigger creates an enhanced message trigger with smart nudges
func NewEnhancedMessageTrigger() *EnhancedMessageTrigger {
	return &EnhancedMessageTrigger{
		nudgeThreshold:    DefaultNudgeThreshold,
		dynamicThreshold:  DefaultNudgeThreshold,
		baseThreshold:     DefaultNudgeThreshold,
		frequencyHistory:  make([]time.Time, 0),
		contextDuration:   make(map[string]time.Duration),
		recentNudges:      list.New(),
		maxRecentNudges:   10,
		minNudgeInterval: 30 * time.Second,
		preferences: &UserNudgePreference{
			EnabledTypes:  allNudgeTypesEnabled(),
			FrequencyLimit: 5 * time.Minute,
		},
		metrics: &NudgeMetrics{
			TotalSent:     0,
			TotalShown:    0,
			TotalDismissed: 0,
			TotalAccepted:  0,
			ByType:        make(map[NudgeType]int),
			ByPriority:    make(map[NudgePriority]int),
		},
		scheduledNudges: make([]ScheduledNudge, 0),
	}
}

// allNudgeTypesEnabled returns a map with all nudge types enabled
func allNudgeTypesEnabled() map[NudgeType]bool {
	return map[NudgeType]bool{
		NudgeTypePeriodic:       true,
		NudgeTypePattern:        true,
		NudgeTypeReminder:       true,
		NudgeTypeSuggestion:    true,
		NudgeTypeErrorRecovery: true,
		NudgeTypeOpportunity:   true,
	}
}

// OnUserMessage is called when a new user message arrives
// This marks the start of a task and increments turn counter
func (mt *EnhancedMessageTrigger) OnUserMessage(input string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	// Update context
	mt.updateContext(input)
	
	mt.turnCount++
	mt.taskStartTime = time.Now()
	mt.currentTask = input
	
	// Update frequency history
	mt.frequencyHistory = append(mt.frequencyHistory, time.Now())
	if len(mt.frequencyHistory) > 100 {
		mt.frequencyHistory = mt.frequencyHistory[1:]
	}
	
	// Adjust threshold based on conversation frequency
	mt.adjustDynamicThreshold()
	
	// Check if we should trigger a nudge
	if mt.shouldTriggerNudge() {
		mt.triggerNudge(NudgeTypePeriodic, NudgePriorityNormal, "You've been working on this for a while. Want me to summarize progress?")
	}
}

// updateContext extracts and tracks conversation context
func (mt *EnhancedMessageTrigger) updateContext(input string) {
	// Simple context extraction - in production, use NLP
	mt.currentContext = input
	
	// Track context history
	mt.contextHistory = append(mt.contextHistory, input)
	if len(mt.contextHistory) > 20 {
		mt.contextHistory = mt.contextHistory[1:]
	}
	
	// Track context duration
	now := time.Now()
	if mt.taskStartTime.IsZero() {
		mt.taskStartTime = now
	} else {
		mt.contextDuration[mt.currentContext] = now.Sub(mt.taskStartTime)
	}
}

// adjustDynamicThreshold adjusts the nudge threshold based on conversation patterns
func (mt *EnhancedMessageTrigger) adjustDynamicThreshold() {
	if len(mt.frequencyHistory) < 5 {
		return
	}
	
	// Calculate recent conversation frequency
	recentMessages := 0
	since := time.Now().Add(-10 * time.Minute)
	for _, t := range mt.frequencyHistory {
		if t.After(since) {
			recentMessages++
		}
	}
	
	// Adjust threshold: higher frequency = longer interval between nudges
	if recentMessages > 10 {
		mt.dynamicThreshold = int(float64(mt.baseThreshold) * 1.5)
	} else if recentMessages < 3 {
		mt.dynamicThreshold = int(float64(mt.baseThreshold) * 0.7)
	} else {
		mt.dynamicThreshold = mt.baseThreshold
	}
}

// shouldTriggerNudge determines if a nudge should be triggered
func (mt *EnhancedMessageTrigger) shouldTriggerNudge() bool {
	// Check if we've reached the dynamic threshold
	if mt.turnCount%mt.dynamicThreshold != 0 {
		return false
	}
	
	// Check cooldown
	if time.Since(mt.lastNudgeTime) < mt.minNudgeInterval {
		return false
	}
	
	// Check quiet hours
	if mt.isQuietHours() {
		return false
	}
	
	// Check if this nudge type is enabled
	if !mt.preferences.EnabledTypes[NudgeTypePeriodic] {
		return false
	}
	
	return true
}

// isQuietHours checks if current time is in quiet hours
func (mt *EnhancedMessageTrigger) isQuietHours() bool {
	if mt.preferences.QuietHoursStart.IsZero() || mt.preferences.QuietHoursEnd.IsZero() {
		return false
	}
	
	now := time.Now()
	start := mt.preferences.QuietHoursStart
	end := mt.preferences.QuietHoursEnd
	
	// Handle overnight quiet hours (e.g., 22:00 - 07:00)
	if start.After(end) {
		return now.After(start) || now.Before(end)
	}
	return now.After(start) && now.Before(end)
}

// triggerNudge triggers a nudge with specified parameters
func (mt *EnhancedMessageTrigger) triggerNudge(nudgeType NudgeType, priority NudgePriority, message string) {
	nudge := &Nudge{
		ID:        fmt.Sprintf("nudge-%d", time.Now().UnixNano()),
		Type:      nudgeType,
		Priority:  priority,
		Message:   message,
		Context:   mt.currentTask,
		CreatedAt: time.Now(),
		Metadata:  mt.buildNudgeMetadata(),
	}
	
	// Add context-aware actions
	nudge.Actions = mt.buildNudgeActions(nudge)
	
	// Track the nudge
	mt.recordNudge(nudge)
	
	// Notify handlers
	mt.deliverNudge(nudge)
}

// buildNudgeMetadata builds metadata for the nudge
func (mt *EnhancedMessageTrigger) buildNudgeMetadata() map[string]interface{} {
	return map[string]interface{}{
		"turn_count":      mt.turnCount,
		"threshold":       mt.dynamicThreshold,
		"conversation_age": time.Since(mt.taskStartTime).Seconds(),
		"context":         mt.currentContext,
	}
}

// buildNudgeActions builds context-aware actions for the nudge
func (mt *EnhancedMessageTrigger) buildNudgeActions(nudge *Nudge) []NudgeAction {
	actions := make([]NudgeAction, 0)
	
	switch nudge.Type {
	case NudgeTypePeriodic:
		actions = append(actions, NudgeAction{
			Label: "Summarize Progress",
			Type:  "skill",
			Value: "progress-summary",
		})
		actions = append(actions, NudgeAction{
			Label: "Continue Working",
			Type:  "dismiss",
			Value: "continue",
		})
	case NudgeTypePattern:
		actions = append(actions, NudgeAction{
			Label: "Create Skill",
			Type:  "skill",
			Value: "auto-skill-creator",
		})
	case NudgeTypeErrorRecovery:
		actions = append(actions, NudgeAction{
			Label: "View Error Details",
			Type:  "link",
			Value: "/errors/latest",
		})
		actions = append(actions, NudgeAction{
			Label: "Retry Last Step",
			Type:  "tool",
			Value: "retry",
		})
	case NudgeTypeSuggestion:
		actions = append(actions, NudgeAction{
			Label: "Apply Suggestion",
			Type:  "skill",
			Value: "apply",
		})
	}
	
	return actions
}

// recordNudge records a nudge for metrics and cooldown
func (mt *EnhancedMessageTrigger) recordNudge(nudge *Nudge) {
	mt.lastNudgeTime = time.Now()
	mt.metrics.TotalSent++
	mt.metrics.ByType[nudge.Type]++
	mt.metrics.ByPriority[nudge.Priority]++
	
	// Add to recent nudges list
	mt.recentNudges.PushBack(nudge)
	if mt.recentNudges.Len() > mt.maxRecentNudges {
		mt.recentNudges.Remove(mt.recentNudges.Front())
	}
	
	// Update user preferences
	mt.preferences.LastNudgeTime = time.Now()
}

// deliverNudge sends nudge to all registered handlers
func (mt *EnhancedMessageTrigger) deliverNudge(nudge *Nudge) {
	for _, handler := range mt.nudgeHandlers {
		go func(h NudgeHandler) {
			if h.HandleNudge(nudge) {
				mt.mu.Lock()
				mt.metrics.TotalShown++
				mt.mu.Unlock()
			}
		}(handler)
	}
}

// OnPatternDetected triggers a nudge when a pattern is detected
func (mt *EnhancedMessageTrigger) OnPatternDetected(pattern string, frequency int) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	// Only nudge for significant patterns
	if frequency < 2 {
		return
	}
	
	message := fmt.Sprintf("I noticed you perform `%s` frequently. Want me to create a skill for this?", pattern)
	mt.triggerNudgeUnsafe(NudgeTypePattern, NudgePriorityNormal, message)
}

// OnErrorOccurred triggers a recovery nudge when an error occurs
func (mt *EnhancedMessageTrigger) OnErrorOccurred(errorType string, canRecover bool) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if !canRecover {
		return
	}
	
	priority := NudgePriorityHigh
	if mt.preferences.ResponseRate < 0.3 {
		// User doesn't engage much - make nudge less intrusive
		priority = NudgePriorityNormal
	}
	
	message := fmt.Sprintf("I encountered a %s issue. Should I try a different approach?", errorType)
	mt.triggerNudgeUnsafe(NudgeTypeErrorRecovery, priority, message)
}

// OnOpportunityDetected triggers an opportunity nudge
func (mt *EnhancedMessageTrigger) OnOpportunityDetected(opportunity string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if !mt.preferences.EnabledTypes[NudgeTypeOpportunity] {
		return
	}
	
	message := fmt.Sprintf("I found an opportunity: %s. Want me to explore this?", opportunity)
	mt.triggerNudgeUnsafe(NudgeTypeOpportunity, NudgePriorityLow, message)
}

// triggerNudgeUnsafe triggers nudge without locking (caller must hold lock)
func (mt *EnhancedMessageTrigger) triggerNudgeUnsafe(nudgeType NudgeType, priority NudgePriority, message string) {
	// Skip if in cooldown for same type
	for e := mt.recentNudges.Front(); e != nil; e = e.Next() {
		n := e.Value.(*Nudge)
		if n.Type == nudgeType && time.Since(n.CreatedAt) < mt.minNudgeInterval {
			return
		}
	}
	
	nudge := &Nudge{
		ID:        fmt.Sprintf("nudge-%d", time.Now().UnixNano()),
		Type:      nudgeType,
		Priority:  priority,
		Message:   message,
		Context:   mt.currentTask,
		CreatedAt: time.Now(),
		Metadata:  mt.buildNudgeMetadata(),
	}
	
	mt.recordNudge(nudge)
	go mt.deliverNudge(nudge)
}

// OnToolCall increments the tool call counter for skill creation
func (mt *EnhancedMessageTrigger) OnToolCall(toolName string, args map[string]interface{}) {
	// Track tool calls for pattern detection
}

// OnTaskComplete marks the end of a task
func (mt *EnhancedMessageTrigger) OnTaskComplete() time.Duration {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	duration := time.Since(mt.taskStartTime)
	mt.currentTask = ""
	return duration
}

// OnNudgeResponse handles user response to a nudge
func (mt *EnhancedMessageTrigger) OnNudgeResponse(nudgeID string, accepted bool) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	// Update metrics
	if accepted {
		mt.metrics.TotalAccepted++
	} else {
		mt.metrics.TotalDismissed++
	}
	
	// Update user preference learning
	total := mt.metrics.TotalAccepted + mt.metrics.TotalDismissed
	if total > 0 {
		mt.preferences.ResponseRate = float64(mt.metrics.TotalAccepted) / float64(total)
	}
	
	// Adjust future nudge behavior
	mt.adjustBehaviorFromResponse(accepted)
}

// adjustBehaviorFromResponse adjusts nudge behavior based on user response
func (mt *EnhancedMessageTrigger) adjustBehaviorFromResponse(accepted bool) {
	// If user accepts frequently, show more nudges
	// If user dismisses frequently, show fewer nudges
	if mt.preferences.ResponseRate > 0.7 {
		mt.minNudgeInterval = time.Duration(float64(mt.minNudgeInterval) * 0.8)
		mt.baseThreshold = int(float64(mt.baseThreshold) * 0.9)
	} else if mt.preferences.ResponseRate < 0.3 {
		mt.minNudgeInterval = time.Duration(float64(mt.minNudgeInterval) * 1.2)
		mt.baseThreshold = int(float64(mt.baseThreshold) * 1.1)
	}
	
	// Clamp values
	if mt.minNudgeInterval < 10*time.Second {
		mt.minNudgeInterval = 10 * time.Second
	}
	if mt.minNudgeInterval > 5*time.Minute {
		mt.minNudgeInterval = 5 * time.Minute
	}
	if mt.baseThreshold < 5 {
		mt.baseThreshold = 5
	}
	if mt.baseThreshold > 30 {
		mt.baseThreshold = 30
	}
}

// RegisterNudgeHandler registers a nudge handler
func (mt *EnhancedMessageTrigger) RegisterNudgeHandler(handler NudgeHandler) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.nudgeHandlers = append(mt.nudgeHandlers, handler)
}

// RegisterNudgeHandlerFunc registers a function as a nudge handler
func (mt *EnhancedMessageTrigger) RegisterNudgeHandlerFunc(name string, handler func(*Nudge) bool) {
	mt.RegisterNudgeHandler(&SimpleNudgeHandler{name: name, handler: handler})
}

// ScheduleNudge schedules a nudge for future delivery
func (mt *EnhancedMessageTrigger) ScheduleNudge(nudge *Nudge, deliverAt time.Time) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.scheduledNudges = append(mt.scheduledNudges, ScheduledNudge{
		Nudge:     nudge,
		DeliverAt: deliverAt,
	})
	
	mt.scheduleNextCheck()
}

// scheduleNextCheck schedules the next check for scheduled nudges
func (mt *EnhancedMessageTrigger) scheduleNextCheck() {
	if len(mt.scheduledNudges) == 0 {
		return
	}
	
	// Find the next nudge time
	soonest := mt.scheduledNudges[0].DeliverAt
	for _, sn := range mt.scheduledNudges {
		if sn.DeliverAt.Before(soonest) {
			soonest = sn.DeliverAt
		}
	}
	
	delay := time.Until(soonest)
	if delay < 0 {
		delay = 0
	}
	
	if mt.timer != nil {
		mt.timer.Stop()
	}
	mt.timer = time.AfterFunc(delay, func() {
		mt.deliverScheduledNudges()
	})
}

// deliverScheduledNudges delivers all due scheduled nudges
func (mt *EnhancedMessageTrigger) deliverScheduledNudges() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	now := time.Now()
	remaining := make([]ScheduledNudge, 0)
	
	for _, sn := range mt.scheduledNudges {
		if sn.DeliverAt.Before(now) || sn.DeliverAt.Equal(now) {
			mt.recordNudge(sn.Nudge)
			go mt.deliverNudge(sn.Nudge)
		} else {
			remaining = append(remaining, sn)
		}
	}
	
	mt.scheduledNudges = remaining
	mt.scheduleNextCheck()
}

// GetTurnCount returns the current turn count
func (mt *EnhancedMessageTrigger) GetTurnCount() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.turnCount
}

// SetNudgeThreshold sets the nudge threshold
func (mt *EnhancedMessageTrigger) SetNudgeThreshold(threshold int) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if threshold > 0 {
		mt.baseThreshold = threshold
		mt.dynamicThreshold = threshold
		mt.nudgeThreshold = threshold
	}
}

// SetQuietHours sets quiet hours when nudges should be suppressed
func (mt *EnhancedMessageTrigger) SetQuietHours(start, end time.Time) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.preferences.QuietHoursStart = start
	mt.preferences.QuietHoursEnd = end
}

// SetNudgeTypeEnabled enables or disables a specific nudge type
func (mt *EnhancedMessageTrigger) SetNudgeTypeEnabled(nudgeType NudgeType, enabled bool) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.preferences.EnabledTypes[nudgeType] = enabled
}

// GetMetrics returns current nudge metrics
func (mt *EnhancedMessageTrigger) GetMetrics() *NudgeMetrics {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	// Return a copy
	metrics := *mt.metrics
	return &metrics
}

// GetPreferences returns current user preferences
func (mt *EnhancedMessageTrigger) GetPreferences() *UserNudgePreference {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	// Return a copy
	pref := *mt.preferences
	return &pref
}

// SetPreferences sets user preferences
func (mt *EnhancedMessageTrigger) SetPreferences(pref *UserNudgePreference) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.preferences = pref
}

// Reset resets the trigger state
func (mt *EnhancedMessageTrigger) Reset() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.turnCount = 0
	mt.currentTask = ""
	mt.dynamicThreshold = mt.baseThreshold
	mt.taskStartTime = time.Time{}
	
	if mt.timer != nil {
		mt.timer.Stop()
	}
	mt.timer = nil
}

// GetRecentNudges returns recent nudges
func (mt *EnhancedMessageTrigger) GetRecentNudges(limit int) []*Nudge {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	nudges := make([]*Nudge, 0)
	
	e := mt.recentNudges.Back()
	for i := 0; i < limit && e != nil; i++ {
		nudges = append(nudges, e.Value.(*Nudge))
		e = e.Prev()
	}
	
	return nudges
}

// GetContext returns the current conversation context
func (mt *EnhancedMessageTrigger) GetContext() string {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.currentContext
}

// GetContextHistory returns recent conversation contexts
func (mt *EnhancedMessageTrigger) GetContextHistory(limit int) []string {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	if len(mt.contextHistory) <= limit {
		return mt.contextHistory
	}
	return mt.contextHistory[len(mt.contextHistory)-limit:]
}

// GetDynamicThreshold returns the current dynamic threshold
func (mt *EnhancedMessageTrigger) GetDynamicThreshold() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.dynamicThreshold
}

// PredictiveNudge triggers a nudge based on predictive analysis
func (mt *EnhancedMessageTrigger) PredictiveNudge(predictor func() (string, bool)) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if !mt.preferences.EnabledTypes[NudgeTypeSuggestion] {
		return
	}
	
	suggestion, shouldNudge := predictor()
	if shouldNudge {
		mt.triggerNudgeUnsafe(NudgeTypeSuggestion, NudgePriorityLow, suggestion)
	}
}

// BatchNudge sends multiple nudges at once
func (mt *EnhancedMessageTrigger) BatchNudge(nudges []*Nudge) {
	for _, nudge := range nudges {
		go func(n *Nudge) {
			mt.mu.RLock()
			mt.recordNudge(n)
			mt.mu.RUnlock()
			mt.deliverNudge(n)
		}(nudge)
	}
}

// AddCustomNudgeType registers a new nudge type
func (mt *EnhancedMessageTrigger) AddCustomNudgeType(nudgeType NudgeType) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if mt.preferences.EnabledTypes == nil {
		mt.preferences.EnabledTypes = make(map[NudgeType]bool)
	}
	
	// New custom types are disabled by default
	mt.preferences.EnabledTypes[nudgeType] = false
}

// SetMinNudgeInterval sets the minimum interval between nudges
func (mt *EnhancedMessageTrigger) SetMinNudgeInterval(interval time.Duration) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.minNudgeInterval = interval
}

// GetMinNudgeInterval returns the minimum interval between nudges
func (mt *EnhancedMessageTrigger) GetMinNudgeInterval() time.Duration {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.minNudgeInterval
}
