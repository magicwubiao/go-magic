package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// HandoffRequest represents a session handoff request
type HandoffRequest struct {
	TargetModel       string `json:"target_model"`
	TargetProfile     string `json:"target_profile"`
	TargetPersonality string `json:"target_personality"`
	Reason            string `json:"reason"`
}

// HandoffResult represents the result of a handoff operation
type HandoffResult struct {
	Success      bool      `json:"success"`
	FromModel    string    `json:"from_model"`
	ToModel      string    `json:"to_model"`
	FromProfile  string    `json:"from_profile"`
	ToProfile    string    `json:"to_profile"`
	MessageCount int       `json:"message_count"`
	TokenCount   int       `json:"token_count"`
	HandoffID    string    `json:"handoff_id"`
	Timestamp    time.Time `json:"timestamp"`
	Error        string    `json:"error,omitempty"`
}

// HandoffManager manages session handoffs between models/profiles
type HandoffManager struct {
	handoffs []HandoffRecord
}

// HandoffRecord stores the history of handoffs
type HandoffRecord struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	FromModel    string    `json:"from_model"`
	ToModel      string    `json:"to_model"`
	FromProfile  string    `json:"from_profile"`
	ToProfile    string    `json:"to_profile"`
	MessageCount int       `json:"message_count"`
	Reason       string    `json:"reason"`
	Timestamp    time.Time `json:"timestamp"`
}

// NewHandoffManager creates a new HandoffManager
func NewHandoffManager() *HandoffManager {
	return &HandoffManager{
		handoffs: make([]HandoffRecord, 0),
	}
}

// ExecuteHandoff performs a session handoff
// It transfers the current session context to a new model/profile
func (hm *HandoffManager) ExecuteHandoff(ctx context.Context, sessionID, currentModel, currentProfile string, req HandoffRequest) *HandoffResult {
	result := &HandoffResult{
		HandoffID:   uuid.New().String(),
		Timestamp:   time.Now(),
		FromModel:   currentModel,
		ToModel:     req.TargetModel,
		FromProfile: currentProfile,
		ToProfile:   req.TargetProfile,
	}

	// Determine target values (fallback to current if not specified)
	if req.TargetModel == "" {
		req.TargetModel = currentModel
		result.ToModel = currentModel
	}
	if req.TargetProfile == "" {
		req.TargetProfile = currentProfile
		result.ToProfile = currentProfile
	}

	// Record the handoff
	record := HandoffRecord{
		ID:          result.HandoffID,
		SessionID:   sessionID,
		FromModel:   currentModel,
		ToModel:     req.TargetModel,
		FromProfile: currentProfile,
		ToProfile:   req.TargetProfile,
		Reason:      req.Reason,
		Timestamp:   time.Now(),
	}
	hm.handoffs = append(hm.handoffs, record)

	result.Success = true
	return result
}

// GetHandoffHistory returns the handoff history for a session
func (hm *HandoffManager) GetHandoffHistory(sessionID string) []HandoffRecord {
	var records []HandoffRecord
	for _, h := range hm.handoffs {
		if h.SessionID == sessionID {
			records = append(records, h)
		}
	}
	return records
}

// MarshalHandoffResult returns a JSON string representation of the handoff result
func MarshalHandoffResult(r *HandoffResult) string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
}

// BuildHandoffPrompt builds the continuation prompt after a handoff
// This preserves context while switching to the new model/profile
func BuildHandoffPrompt(result *HandoffResult, lastUserMessage string) string {
	return fmt.Sprintf(
		`[Session Handoff]
This session was handed off from %s (profile: %s) to %s (profile: %s).
Handoff ID: %s
Reason: %s

The full conversation context has been preserved. Continue the task seamlessly.
Last user message: %s`,
		result.FromModel, result.FromProfile,
		result.ToModel, result.ToProfile,
		result.HandoffID,
		"User requested model/profile switch",
		lastUserMessage,
	)
}
