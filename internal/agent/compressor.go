package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// CompressionConfig holds configuration for context compression
type CompressionConfig struct {
	// Threshold ratio to trigger compression (0.0-1.0)
	ThresholdRatio float64
	// Minimum messages to keep
	MinMessages int
	// Keep recent messages count
	KeepRecent int
	// Keep first messages count
	KeepFirst int
	// Preserve tool results
	PreserveToolResults bool
	// Preserve decisions
	PreserveDecisions bool
}

// DefaultCompressionConfig returns default compression configuration
func DefaultCompressionConfig() *CompressionConfig {
	return &CompressionConfig{
		ThresholdRatio:      0.5,
		MinMessages:         4,
		KeepRecent:          4,
		KeepFirst:           2,
		PreserveToolResults: true,
		PreserveDecisions:   true,
	}
}

// CompressionResult holds the result of a compression operation
type CompressionResult struct {
	OriginalCount int
	NewCount      int
	Summary       string
	KeptMessages  []provider.Message
}

// IntelligentCompressor provides smart context compression
type IntelligentCompressor struct {
	config *CompressionConfig
}

// NewIntelligentCompressor creates a new intelligent compressor
func NewIntelligentCompressor(cfg *CompressionConfig) *IntelligentCompressor {
	if cfg == nil {
		cfg = DefaultCompressionConfig()
	}
	return &IntelligentCompressor{config: cfg}
}

// Compress performs intelligent compression on the message history
func (ic *IntelligentCompressor) Compress(history []provider.Message) *CompressionResult {
	result := &CompressionResult{
		OriginalCount: len(history),
		KeptMessages:  make([]provider.Message, 0),
	}

	if len(history) <= ic.config.MinMessages {
		result.KeptMessages = history
		result.NewCount = len(history)
		return result
	}

	// Step 1: Separate messages by type
	var systemMsgs []provider.Message
	var userMsgs []provider.Message
	var assistantMsgs []provider.Message
	var toolMsgs []provider.Message

	for _, msg := range history {
		switch msg.Role {
		case "system":
			systemMsgs = append(systemMsgs, msg)
		case "user":
			userMsgs = append(userMsgs, msg)
		case "assistant":
			assistantMsgs = append(assistantMsgs, msg)
		case "tool":
			toolMsgs = append(toolMsgs, msg)
		}
	}

	// Step 2: Keep system messages
	result.KeptMessages = append(result.KeptMessages, systemMsgs...)

	// Step 3: Keep first user messages (important context)
	keepFirst := ic.config.KeepFirst
	if keepFirst > len(userMsgs) {
		keepFirst = len(userMsgs)
	}
	for i := 0; i < keepFirst; i++ {
		result.KeptMessages = append(result.KeptMessages, userMsgs[i])
	}

	// Step 4: Identify and preserve key decisions
	keyDecisions := ic.extractKeyDecisions(assistantMsgs)
	result.KeptMessages = append(result.KeptMessages, keyDecisions...)

	// Step 5: Keep recent messages
	keepRecent := ic.config.KeepRecent
	if keepRecent > len(userMsgs) {
		keepRecent = len(userMsgs)
	}
	recentStartIdx := len(userMsgs) - keepRecent
	for i := recentStartIdx; i < len(userMsgs); i++ {
		// Check if this message is already kept
		if !ic.messageExists(result.KeptMessages, userMsgs[i]) {
			result.KeptMessages = append(result.KeptMessages, userMsgs[i])
		}
	}

	// Step 6: Preserve tool results if configured
	if ic.config.PreserveToolResults {
		toolResults := ic.extractPreservedToolResults(toolMsgs, assistantMsgs)
		result.KeptMessages = append(result.KeptMessages, toolResults...)
	}

	// Step 7: Generate compression summary
	messagesToSummarize := ic.getMessagesToSummarize(history, result.KeptMessages)
	result.Summary = ic.generateSummary(messagesToSummarize)

	// Step 8: Add summary message
	if result.Summary != "" {
		summaryMsg := provider.Message{
			Role: "system",
			Content: fmt.Sprintf("\n\n[Previous conversation compressed - %d messages summarized]\n\n%s",
				len(messagesToSummarize), result.Summary),
		}
		result.KeptMessages = append(result.KeptMessages, summaryMsg)
	}

	result.NewCount = len(result.KeptMessages)
	return result
}

// extractKeyDecisions extracts important assistant decisions
func (ic *IntelligentCompressor) extractKeyDecisions(assistantMsgs []provider.Message) []provider.Message {
	var decisions []provider.Message

	for _, msg := range assistantMsgs {
		// Look for messages with tool calls (indicating decisions/actions)
		if len(msg.ToolCalls) > 0 {
			decisions = append(decisions, msg)
			continue
		}

		// Look for messages with important keywords
		content := strings.ToLower(msg.Content)
		decisionKeywords := []string{
			"decided", "chose", "selected", "concluded",
			"implemented", "created", "built", "fixed",
			"analyzed", "found", "discovered", "concluded",
		}
		for _, keyword := range decisionKeywords {
			if strings.Contains(content, keyword) {
				decisions = append(decisions, msg)
				break
			}
		}
	}

	// Limit to last 3 decisions
	if len(decisions) > 3 {
		decisions = decisions[len(decisions)-3:]
	}

	return decisions
}

// extractPreservedToolResults extracts tool results that should be preserved
func (ic *IntelligentCompressor) extractPreservedToolResults(toolMsgs []provider.Message, assistantMsgs []provider.Message) []provider.Message {
	var preserved []provider.Message

	// Create a map of tool call IDs from assistant messages
	toolCallIDs := make(map[string]bool)
	for _, msg := range assistantMsgs {
		for _, tc := range msg.ToolCalls {
			toolCallIDs[tc.ID] = true
		}
	}

	// Keep tool results for tool calls we're preserving
	// Also keep error results (important for debugging)
	for _, msg := range toolMsgs {
		if toolCallIDs[msg.ToolCallID] {
			// Keep tool results for preserved tool calls
			preserved = append(preserved, msg)
		} else if strings.Contains(msg.Content, "Error:") {
			// Keep error results
			if len(preserved) < 5 {
				preserved = append(preserved, msg)
			}
		}
	}

	// Limit total preserved tool results
	if len(preserved) > 10 {
		preserved = preserved[len(preserved)-10:]
	}

	return preserved
}

// getMessagesToSummarize returns messages that should be summarized
func (ic *IntelligentCompressor) getMessagesToSummarize(all, kept []provider.Message) []provider.Message {
	keptMap := make(map[string]bool)
	for _, msg := range kept {
		keptMap[msg.Content] = true
	}

	var toSummarize []provider.Message
	for _, msg := range all {
		if !keptMap[msg.Content] {
			toSummarize = append(toSummarize, msg)
		}
	}
	return toSummarize
}

// generateSummary generates a summary of compressed messages
func (ic *IntelligentCompressor) generateSummary(messages []provider.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var summary strings.Builder
	summary.WriteString("## Summary\n\n")

	// Count message types
	userCount := 0
	toolCount := 0
	assistantCount := 0
	uniqueTools := make(map[string]bool)
	errors := 0

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			userCount++
		case "tool":
			toolCount++
			if strings.Contains(msg.Content, "Error:") {
				errors++
			}
		case "assistant":
			assistantCount++
		}

		// Track unique tool names from content patterns
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				uniqueTools[tc.Name] = true
			}
		}
	}

	// Write summary statistics
	summary.WriteString(fmt.Sprintf("- Total exchanges: %d user requests\n", userCount))
	summary.WriteString(fmt.Sprintf("- Tool operations: %d\n", toolCount))

	if len(uniqueTools) > 0 {
		summary.WriteString("- Tools used: ")
		tools := make([]string, 0, len(uniqueTools))
		for t := range uniqueTools {
			tools = append(tools, t)
		}
		summary.WriteString(strings.Join(tools, ", "))
		summary.WriteString("\n")
	}

	if errors > 0 {
		summary.WriteString(fmt.Sprintf("- Errors encountered: %d\n", errors))
	}

	// Extract key patterns from user messages
	if userCount > 0 {
		summary.WriteString("\n### Key Topics\n")
		topics := ic.extractTopics(messages)
		for _, topic := range topics {
			summary.WriteString(fmt.Sprintf("- %s\n", topic))
		}
	}

	return summary.String()
}

// extractTopics extracts key topics from messages
func (ic *IntelligentCompressor) extractTopics(messages []provider.Message) []string {
	// Simple topic extraction based on file paths, URLs, and keywords
	topics := make([]string, 0)
	seen := make(map[string]bool)

	// File path pattern
	filePattern := regexp.MustCompile(`[\w\-\./]+\.\w{1,10}`)
	// URL pattern
	urlPattern := regexp.MustCompile(`https?://[^\s]+`)
	// Code block pattern (reserved for future use)
	// codePattern := regexp.MustCompile("```[\\s\\S]*?```")

	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}

		// Extract file paths
		files := filePattern.FindAllString(msg.Content, -1)
		for _, f := range files {
			if !seen[f] && strings.Contains(f, "/") {
				seen[f] = true
				if len(topics) < 5 {
					topics = append(topics, "File: "+f)
				}
			}
		}

		// Extract URLs
		urls := urlPattern.FindAllString(msg.Content, -1)
		for _, u := range urls {
			if !seen[u] {
				seen[u] = true
				if len(topics) < 5 {
					// Shorten URL for display
					shortURL := u
					if len(shortURL) > 50 {
						shortURL = shortURL[:47] + "..."
					}
					topics = append(topics, "URL: "+shortURL)
				}
			}
		}
	}

	// Limit to 5 topics
	if len(topics) > 5 {
		topics = topics[:5]
	}

	return topics
}

// messageExists checks if a message already exists in the list
func (ic *IntelligentCompressor) messageExists(messages []provider.Message, target provider.Message) bool {
	for _, msg := range messages {
		if msg.Content == target.Content && msg.Role == target.Role {
			return true
		}
	}
	return false
}

// EstimateCompressionSavings estimates the compression savings
func (ic *IntelligentCompressor) EstimateCompressionSavings(history []provider.Message) (originalSize, compressedSize, savingsPercent int) {
	originalSize = 0
	for _, msg := range history {
		originalSize += len(msg.Content)
	}

	result := ic.Compress(history)
	compressedSize = 0
	for _, msg := range result.KeptMessages {
		compressedSize += len(msg.Content)
	}

	if originalSize > 0 {
		savingsPercent = ((originalSize - compressedSize) * 100) / originalSize
	}

	return
}

// CompressWithLLM performs LLM-assisted compression (advanced feature)
// This requires an LLM provider and is more expensive but produces better summaries
func (ic *IntelligentCompressor) CompressWithLLM(history []provider.Message, summaryPrompt string) (*CompressionResult, error) {
	// Basic compression first
	result := ic.Compress(history)

	if summaryPrompt == "" {
		summaryPrompt = "Summarize the key points of this conversation in 2-3 sentences."
	}

	// In a full implementation, this would call the LLM to generate a better summary
	// For now, we use the basic summary
	result.Summary = fmt.Sprintf("%s\n\nNote: Advanced LLM-assisted compression available with provider.", result.Summary)

	return result, nil
}

// CompressRatio calculates the current compression ratio
func (ic *IntelligentCompressor) CompressRatio(history []provider.Message) float64 {
	if len(history) <= ic.config.MinMessages {
		return 0
	}

	result := ic.Compress(history)
	return 1.0 - (float64(result.NewCount) / float64(result.OriginalCount))
}

// ShouldCompress returns true if compression should be triggered
func (ic *IntelligentCompressor) ShouldCompress(history []provider.Message) bool {
	return ic.CompressRatio(history) >= ic.config.ThresholdRatio
}
