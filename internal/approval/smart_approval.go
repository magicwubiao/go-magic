package approval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SmartApprovalConfig holds configuration for smart approval
type SmartApprovalConfig struct {
	// Enable learning from user decisions
	LearnFromDecisions bool
	// Database path for storing approval history
	DBPath string
	// Minimum occurrences to auto-approve
	AutoApproveThreshold int
	// Auto-approve safe commands
	AutoApproveSafe bool
}

// DefaultSmartApprovalConfig returns default configuration
func DefaultSmartApprovalConfig() *SmartApprovalConfig {
	home, _ := os.UserHomeDir()
	return &SmartApprovalConfig{
		LearnFromDecisions:   true,
		DBPath:               filepath.Join(home, ".magic", "approval.db"),
		AutoApproveThreshold: 3,
		AutoApproveSafe:      true,
	}
}

// ApprovalDecision represents an approval decision
type ApprovalDecision struct {
	Approved    bool
	Reason      string
	ExpiresAt   *time.Time
	Trusted     bool
	LearnedFrom string // How this was learned
}

// SmartApproval learns from user decisions and auto-approves safe commands
type SmartApproval struct {
	config   *SmartApprovalConfig
	patterns map[string]*CommandPattern
	mu       sync.RWMutex
}

// NewSmartApproval creates a new smart approval system
func NewSmartApproval(config *SmartApprovalConfig) (*SmartApproval, error) {
	if config == nil {
		config = DefaultSmartApprovalConfig()
	}

	sa := &SmartApproval{
		config:   config,
		patterns: make(map[string]*CommandPattern),
	}

	// Load learned patterns
	if err := sa.loadPatterns(); err != nil {
		// Don't fail on error, just start fresh
		fmt.Printf("Warning: failed to load approval patterns: %v\n", err)
	}

	return sa, nil
}

// ShouldAutoApprove checks if a command should be auto-approved
func (sa *SmartApproval) ShouldAutoApprove(command string) (bool, string) {
	if !sa.config.LearnFromDecisions {
		return false, ""
	}

	hash := sa.hashCommand(command)

	sa.mu.RLock()
	pattern, exists := sa.patterns[hash]
	sa.mu.RUnlock()

	if !exists {
		return false, ""
	}

	// Check if we've seen this enough times
	if pattern.Count >= sa.config.AutoApproveThreshold {
		if pattern.Action == "approved" || pattern.Action == "auto_approved" {
			return true, fmt.Sprintf("Learned from %d approvals", pattern.Count)
		}
	}

	// Auto-approve safe commands if enabled
	if sa.config.AutoApproveSafe && sa.isSafeCommand(command) {
		return true, "Auto-approved safe command"
	}

	return false, ""
}

// RecordDecision records a user's approval decision
func (sa *SmartApproval) RecordDecision(command, action string) error {
	if !sa.config.LearnFromDecisions {
		return nil
	}

	hash := sa.hashCommand(command)

	sa.mu.Lock()
	defer sa.mu.Unlock()

	pattern, exists := sa.patterns[hash]
	if exists {
		pattern.Count++
		pattern.LastSeen = time.Now()
		pattern.Action = action
	} else {
		riskLevel := sa.calculateRiskLevel(command)
		pattern = &CommandPattern{
			Pattern:     truncateCommand(command),
			PatternHash: hash,
			Action:      action,
			Count:       1,
			LastSeen:    time.Now(),
			RiskLevel:   riskLevel,
		}
		sa.patterns[hash] = pattern
	}

	return sa.savePatterns()
}

// EvaluateCommand evaluates if a command should be approved
func (sa *SmartApproval) EvaluateCommand(command string, riskLevel RiskLevel) *ApprovalDecision {
	// First check auto-approval
	if autoApprove, reason := sa.ShouldAutoApprove(command); autoApprove {
		return &ApprovalDecision{
			Approved:    true,
			Reason:      reason,
			Trusted:     true,
			LearnedFrom: "pattern_match",
		}
	}

	// Check risk level
	switch riskLevel {
	case RiskLow:
		if sa.config.AutoApproveSafe {
			return &ApprovalDecision{
				Approved:    true,
				Reason:      "Safe command",
				Trusted:     true,
				LearnedFrom: "risk_assessment",
			}
		}
	case RiskMedium:
		return &ApprovalDecision{
			Approved: false,
			Reason:   "Medium risk - requires user approval",
			Trusted:  false,
		}
	case RiskHigh, RiskCritical:
		return &ApprovalDecision{
			Approved: false,
			Reason:   "High risk - requires explicit user approval",
			Trusted:  false,
		}
	}

	return &ApprovalDecision{
		Approved: false,
		Reason:   "Requires user approval",
		Trusted:  false,
	}
}

// isSafeCommand checks if a command is considered safe
func (sa *SmartApproval) isSafeCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))

	// Read-only commands
	safePatterns := []string{
		"^ls", "^dir", "^pwd", "^whoami", "^echo ",
		"^cat ", "^head ", "^tail ", "^grep ",
		"^find ", "^stat ", "^file ",
		"^git status", "^git log",
		"^curl -i", "^curl --head",
		"^npm list", "^pip list",
	}

	for _, pattern := range safePatterns {
		if strings.HasPrefix(command, pattern[1:]) {
			return true
		}
	}

	return false
}

// calculateRiskLevel calculates the risk level of a command
func (sa *SmartApproval) calculateRiskLevel(command string) RiskLevel {
	command = strings.ToLower(command)

	// Critical risk patterns
	criticalPatterns := []string{
		"rm -rf", "rm /", "del /",
		"shutdown", "halt", "reboot",
		":(){ :|:& };:", // Fork bomb
		"mkfs", "dd if=",
	}
	for _, pattern := range criticalPatterns {
		if strings.Contains(command, pattern) {
			return RiskCritical
		}
	}

	// Dangerous patterns
	dangerousPatterns := []string{
		"chmod 777", "chmod +x",
		"sudo ", "su ",
		"wget |", "curl |",
		"> ", ">> ", "2> ",
		"kill -9", "pkill",
		"drop ", "delete from",
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(command, pattern) {
			return RiskHigh
		}
	}

	// Medium risk patterns
	mediumPatterns := []string{
		"npm install", "pip install",
		"apt-get", "yum install", "dnf install",
		"git push", "git commit",
		"docker run", "docker exec",
		"ssh ", "scp ",
	}
	for _, pattern := range mediumPatterns {
		if strings.Contains(command, pattern) {
			return RiskMedium
		}
	}

	return RiskLow
}

// hashCommand creates a hash of the command
func (sa *SmartApproval) hashCommand(command string) string {
	// Use bcrypt for secure hashing
	hash, err := bcrypt.GenerateFromPassword([]byte(command), bcrypt.DefaultCost)
	if err != nil {
		// Fallback to simple hash
		return fmt.Sprintf("%x", hashBytes([]byte(command)))
	}
	return string(hash)
}

// truncateCommand truncates a command for storage
func truncateCommand(command string) string {
	if len(command) > 200 {
		return command[:197] + "..."
	}
	return command
}

// loadPatterns loads learned patterns from disk
func (sa *SmartApproval) loadPatterns() error {
	// Create directory if needed
	dir := filepath.Dir(sa.config.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(sa.config.DBPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var patterns []*CommandPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		return err
	}

	sa.mu.Lock()
	defer sa.mu.Unlock()

	for _, p := range patterns {
		sa.patterns[p.PatternHash] = p
	}

	return nil
}

// savePatterns saves learned patterns to disk
func (sa *SmartApproval) savePatterns() error {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	patterns := make([]*CommandPattern, 0, len(sa.patterns))
	for _, p := range sa.patterns {
		patterns = append(patterns, p)
	}

	data, err := json.MarshalIndent(patterns, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(sa.config.DBPath, data, 0644)
}

// Reset resets learned patterns
func (sa *SmartApproval) Reset() error {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.patterns = make(map[string]*CommandPattern)

	if err := os.Remove(sa.config.DBPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// GetStats returns approval statistics
func (sa *SmartApproval) GetStats() map[string]interface{} {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	total := len(sa.patterns)
	approved := 0
	denied := 0
	autoApproved := 0

	for _, p := range sa.patterns {
		switch p.Action {
		case "approved":
			approved++
		case "denied":
			denied++
		case "auto_approved":
			autoApproved++
		}
	}

	return map[string]interface{}{
		"total_patterns": total,
		"approved":       approved,
		"denied":         denied,
		"auto_approved":  autoApproved,
	}
}

// hashBytes creates a simple hash of bytes
func hashBytes(data []byte) []byte {
	var hash uint32 = 5381
	for _, b := range data {
		hash = ((hash << 5) + hash) + uint32(b)
	}
	return []byte{
		byte(hash >> 24),
		byte(hash >> 16),
		byte(hash >> 8),
		byte(hash),
	}
}
