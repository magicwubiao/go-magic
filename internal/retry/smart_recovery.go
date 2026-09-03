package retry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// ToolErrorType categorizes tool execution failures
type ToolErrorType string

const (
	ToolErrorNotFound    ToolErrorType = "tool_not_found"
	ToolErrorInvalidArgs ToolErrorType = "invalid_args"
	ToolErrorPermission  ToolErrorType = "permission_denied"
	ToolErrorNetwork     ToolErrorType = "network"
	ToolErrorTimeout     ToolErrorType = "timeout"
	ToolErrorResource    ToolErrorType = "resource_unavailable"
	ToolErrorLogic       ToolErrorType = "logic_error"
	ToolErrorEnvironment ToolErrorType = "environment"
	ToolErrorUnknown     ToolErrorType = "unknown"
)

// ToolErrorInfo contains detailed analysis of a tool failure
type ToolErrorInfo struct {
	ToolName         string
	ErrorType        ToolErrorType
	ErrorMessage     string
	Retryable        bool
	SuggestedFix     string
	AlternativeTools []string
	RootCause        string
	Context          map[string]interface{}
}

// ToolFailureRecord tracks failures for pattern learning
type ToolFailureRecord struct {
	ToolName    string
	ErrorType   ToolErrorType
	Timestamp   time.Time
	Args        string
	Consecutive int
	TotalCount  int
	LastFix     string
	Fixed       bool
}

// SmartRecovery provides intelligent error recovery for tool execution
type SmartRecovery struct {
	mu sync.RWMutex

	// Failure tracking
	failureHistory   map[string][]*ToolFailureRecord
	consecutiveFails map[string]int

	// Tool alternatives (tools that can achieve similar goals)
	toolAlternatives map[string][]string

	// Configuration
	maxConsecutiveFails int
	historySize         int
}

// SmartRecoveryConfig configures the smart recovery system
type SmartRecoveryConfig struct {
	MaxConsecutiveFails int
	HistorySize         int
}

// DefaultSmartRecoveryConfig returns default config
func DefaultSmartRecoveryConfig() SmartRecoveryConfig {
	return SmartRecoveryConfig{
		MaxConsecutiveFails: 3,
		HistorySize:         50,
	}
}

// NewSmartRecovery creates a new smart recovery system
func NewSmartRecovery(cfg SmartRecoveryConfig) *SmartRecovery {
	sr := &SmartRecovery{
		failureHistory:      make(map[string][]*ToolFailureRecord),
		consecutiveFails:    make(map[string]int),
		toolAlternatives:    make(map[string][]string),
		maxConsecutiveFails: cfg.MaxConsecutiveFails,
		historySize:         cfg.HistorySize,
	}

	// Register common tool alternatives
	sr.RegisterAlternatives("web_search", []string{"search", "google_search", "bing_search", "duckduckgo"})
	sr.RegisterAlternatives("read_file", []string{"view_files", "cat", "list_dir"})
	sr.RegisterAlternatives("write_file", []string{"edit_file", "str_replace_editor"})
	sr.RegisterAlternatives("execute_command", []string{"run_command", "bash", "shell"})
	sr.RegisterAlternatives("http_request", []string{"fetch", "curl", "wget", "web_fetch"})
	sr.RegisterAlternatives("create_directory", []string{"mkdir", "write_file"})
	sr.RegisterAlternatives("list_dir", []string{"glob", "find_files"})
	sr.RegisterAlternatives("view_files", []string{"read_file", "cat"})

	return sr
}

// RegisterAlternatives registers alternative tools for a given tool
func (sr *SmartRecovery) RegisterAlternatives(toolName string, alternatives []string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.toolAlternatives[toolName] = alternatives
}

// AnalyzeToolError analyzes a tool execution error and provides recovery info
func (sr *SmartRecovery) AnalyzeToolError(toolName string, err error, args map[string]interface{}) *ToolErrorInfo {
	if err == nil {
		return nil
	}

	errMsg := strings.ToLower(err.Error())
	info := &ToolErrorInfo{
		ToolName:     toolName,
		ErrorMessage: err.Error(),
		Retryable:    true,
		Context:      make(map[string]interface{}),
	}

	// Classify error type
	switch {
	case containsAny(errMsg, []string{"not found", "no such", "unknown tool", "does not exist"}):
		info.ErrorType = ToolErrorNotFound
		info.Retryable = false
		info.SuggestedFix = "Check if the tool name is correct. Consider using an alternative tool."
		info.RootCause = "Tool or resource not found"

	case containsAny(errMsg, []string{"invalid", "bad request", "malformed", "schema", "validation", "missing", "required"}):
		info.ErrorType = ToolErrorInvalidArgs
		info.Retryable = true
		info.SuggestedFix = "Review and correct the tool arguments. Check for missing required parameters."
		info.RootCause = "Invalid or missing tool arguments"

	case containsAny(errMsg, []string{"permission", "denied", "forbidden", "access", "unauthorized"}):
		info.ErrorType = ToolErrorPermission
		info.Retryable = false
		info.SuggestedFix = "Check permissions. You may need to adjust security settings or use a different approach."
		info.RootCause = "Permission or authorization issue"

	case containsAny(errMsg, []string{"network", "connection", "dns", "econnrefused", "no route", "unreachable"}):
		info.ErrorType = ToolErrorNetwork
		info.Retryable = true
		info.SuggestedFix = "Check network connectivity. Consider retrying or using an alternative method."
		info.RootCause = "Network connectivity issue"

	case containsAny(errMsg, []string{"timeout", "timed out", "deadline exceeded"}):
		info.ErrorType = ToolErrorTimeout
		info.Retryable = true
		info.SuggestedFix = "The operation timed out. Consider increasing timeout or breaking the task into smaller steps."
		info.RootCause = "Operation took too long"

	case containsAny(errMsg, []string{"resource", "busy", "locked", "unavailable", "already exists"}):
		info.ErrorType = ToolErrorResource
		info.Retryable = true
		info.SuggestedFix = "Resource is unavailable. Try again later or use a different resource."
		info.RootCause = "Resource conflict or unavailability"

	case containsAny(errMsg, []string{"syntax error", "type error", "nil pointer", "panic", "exception"}):
		info.ErrorType = ToolErrorLogic
		info.Retryable = false
		info.SuggestedFix = "There's a logic error. Review the approach and try a different strategy."
		info.RootCause = "Logical or runtime error"

	default:
		info.ErrorType = ToolErrorUnknown
		info.Retryable = true
		info.SuggestedFix = "Unknown error. Consider trying a different approach."
		info.RootCause = "Unknown error"
	}

	// Find alternative tools
	if alts, ok := sr.toolAlternatives[toolName]; ok {
		info.AlternativeTools = alts
	}

	return info
}

// RecordFailure records a tool failure for pattern tracking
func (sr *SmartRecovery) RecordFailure(toolName string, errInfo *ToolErrorInfo) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	record := &ToolFailureRecord{
		ToolName:  toolName,
		ErrorType: errInfo.ErrorType,
		Timestamp: time.Now(),
		LastFix:   errInfo.SuggestedFix,
	}

	// Add to history
	sr.failureHistory[toolName] = append(sr.failureHistory[toolName], record)

	// Trim history
	if len(sr.failureHistory[toolName]) > sr.historySize {
		sr.failureHistory[toolName] = sr.failureHistory[toolName][len(sr.failureHistory[toolName])-sr.historySize:]
	}

	// Update consecutive fail counter
	sr.consecutiveFails[toolName]++

	log.Warnf("[SmartRecovery] Tool %s failed (type: %s, consecutive: %d): %s",
		toolName, errInfo.ErrorType, sr.consecutiveFails[toolName], errInfo.RootCause)
}

// RecordSuccess records a tool success (resets consecutive failures)
func (sr *SmartRecovery) RecordSuccess(toolName string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.consecutiveFails[toolName] > 0 {
		log.Debugf("[SmartRecovery] Tool %s recovered after %d consecutive failures",
			toolName, sr.consecutiveFails[toolName])
	}
	sr.consecutiveFails[toolName] = 0
}

// GetConsecutiveFails returns the number of consecutive failures for a tool
func (sr *SmartRecovery) GetConsecutiveFails(toolName string) int {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.consecutiveFails[toolName]
}

// ShouldTryAlternative returns whether we should try an alternative tool
func (sr *SmartRecovery) ShouldTryAlternative(toolName string) bool {
	return sr.GetConsecutiveFails(toolName) >= sr.maxConsecutiveFails
}

// GetAlternative returns the next alternative tool to try
func (sr *SmartRecovery) GetAlternative(toolName string) string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	alts, ok := sr.toolAlternatives[toolName]
	if !ok || len(alts) == 0 {
		return ""
	}

	// Return the first alternative (could be smarter based on failure history)
	return alts[0]
}

// GetRecoveryPrompt generates a prompt for the LLM with recovery guidance
func (sr *SmartRecovery) GetRecoveryPrompt(errInfo *ToolErrorInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n\n=== Tool Error Recovery: %s ===\n", errInfo.ToolName))
	sb.WriteString(fmt.Sprintf("Error type: %s\n", errInfo.ErrorType))
	sb.WriteString(fmt.Sprintf("Error: %s\n", errInfo.ErrorMessage))
	sb.WriteString(fmt.Sprintf("Root cause: %s\n", errInfo.RootCause))
	sb.WriteString(fmt.Sprintf("Suggested fix: %s\n", errInfo.SuggestedFix))

	if len(errInfo.AlternativeTools) > 0 {
		sb.WriteString(fmt.Sprintf("Alternative tools: %s\n", strings.Join(errInfo.AlternativeTools, ", ")))
	}

	if errInfo.Retryable {
		sb.WriteString("\nYou can retry with corrected parameters or try an alternative approach.\n")
	} else {
		sb.WriteString("\nThis error is not retryable with the same approach. Try a different strategy.\n")
	}

	return sb.String()
}

// GetFailurePatterns returns observed failure patterns for analysis
func (sr *SmartRecovery) GetFailurePatterns() map[string]int {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	patterns := make(map[string]int)
	for tool, fails := range sr.consecutiveFails {
		if fails > 0 {
			patterns[tool] = fails
		}
	}
	return patterns
}

// GetFailureStats returns statistics about tool failures
func (sr *SmartRecovery) GetFailureStats(toolName string) map[string]interface{} {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	history, ok := sr.failureHistory[toolName]
	if !ok {
		return map[string]interface{}{
			"total_failures": 0,
			"consecutive":    0,
		}
	}

	typeStats := make(map[string]int)
	for _, f := range history {
		typeStats[string(f.ErrorType)]++
	}

	return map[string]interface{}{
		"total_failures": len(history),
		"consecutive":    sr.consecutiveFails[toolName],
		"error_types":    typeStats,
	}
}

// AdaptiveRetry performs an adaptive retry with backoff
func (sr *SmartRecovery) AdaptiveRetry(ctx context.Context, toolName string, attempt int, exec func() error) error {
	if attempt >= sr.maxConsecutiveFails {
		return fmt.Errorf("max retries exceeded for %s", toolName)
	}

	// Calculate backoff delay
	delay := JitteredBackoff(attempt)

	// Wait for backoff or context cancellation
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return ctx.Err()
	}

	return exec()
}

// Helper
func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
