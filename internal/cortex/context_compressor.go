package cortex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// ContextCompressor handles intelligent context compression
// Based on Hermes Agent's context_compressor.py approach
type ContextCompressor struct {
	provider    provider.Provider
	threshold  int         // Token threshold before compression
	ratio      float64     // Compression ratio (0.0-1.0)
	maxSummary int         // Max tokens in summary
}

// CompressionResult contains the compressed context
type CompressionResult struct {
	Messages    []provider.Message
	Summary     string
	Removed     int       // Number of messages removed
	Ratio       float64   // Actual compression ratio
	CompressedAt time.Time
}

// NewContextCompressor creates a new context compressor
func NewContextCompressor(prov provider.Provider, threshold int, ratio float64) *ContextCompressor {
	if threshold <= 0 {
		threshold = 10000 // Default 10k tokens
	}
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5 // Default 50% compression
	}

	return &ContextCompressor{
		provider:   prov,
		threshold:  threshold,
		ratio:      ratio,
		maxSummary: threshold / 2,
	}
}

// ShouldCompress checks if context needs compression
func (cc *ContextCompressor) ShouldCompress(messages []provider.Message) bool {
	totalTokens := cc.estimateTokens(messages)
	return totalTokens > cc.threshold
}

// Compress compresses the middle portion of conversation history
func (cc *ContextCompressor) Compress(ctx context.Context, messages []provider.Message) (*CompressionResult, error) {
	if len(messages) < 6 {
		return &CompressionResult{
			Messages:    messages,
			Summary:     "",
			Removed:     0,
			Ratio:       0,
			CompressedAt: time.Now(),
		}, nil
	}

	// Keep first message (system) and last 3-5 messages
	systemMsg := messages[0]
	keepCount := 4
	if len(messages) < keepCount+1 {
		keepCount = len(messages) - 1
	}
	recentMsgs := messages[len(messages)-keepCount:]

	// Middle messages to compress
	middleMsgs := messages[1 : len(messages)-keepCount]

	if len(middleMsgs) == 0 {
		return &CompressionResult{
			Messages:    messages,
			Summary:     "",
			Removed:     0,
			Ratio:       0,
			CompressedAt: time.Now(),
		}, nil
	}

	// Generate summary using LLM
	summary, err := cc.summarizeMessages(ctx, middleMsgs)
	if err != nil {
		// Fallback: simple truncation
		return cc.simpleCompress(messages, keepCount)
	}

	// Build compressed messages
	compressed := []provider.Message{
		systemMsg,
		{
			Role:    "system",
			Content: fmt.Sprintf("[Previous conversation summarized]\n\n%s", summary),
		},
	}
	compressed = append(compressed, recentMsgs...)

	removed := len(middleMsgs)
	originalTokens := cc.estimateTokens(messages)
	compressedTokens := cc.estimateTokens(compressed)

	return &CompressionResult{
		Messages:    compressed,
		Summary:     summary,
		Removed:     removed,
		Ratio:       1.0 - float64(compressedTokens)/float64(originalTokens),
		CompressedAt: time.Now(),
	}, nil
}

// summarizeMessages uses LLM to summarize conversation history
func (cc *ContextCompressor) summarizeMessages(ctx context.Context, messages []provider.Message) (string, error) {
	// Build conversation text
	var parts []string
	for _, msg := range messages {
		parts = append(parts, fmt.Sprintf("%s: %s", msg.Role, msg.Content))
	}
	conversation := strings.Join(parts, "\n\n")

	// Truncate if too long
	if len(conversation) > 4000 {
		conversation = conversation[:4000] + "..."
	}

	prompt := fmt.Sprintf(`Summarize the following conversation concisely, preserving key information:

%s

Provide a summary covering:
1. Main topics discussed
2. Actions taken or decisions made
3. Any important context or preferences mentioned

Keep the summary under 500 words.`, conversation)

	type openAIlike interface {
		Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
	}

	var resp *provider.ChatResponse
	var err error

	if oa, ok := cc.provider.(openAIlike); ok {
		resp, err = oa.Chat(ctx, []provider.Message{{Role: "user", Content: prompt}})
	} else {
		return "", fmt.Errorf("provider does not support chat")
	}

	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// simpleCompress performs simple truncation fallback
func (cc *ContextCompressor) simpleCompress(messages []provider.Message, keepCount int) (*CompressionResult, error) {
	if len(messages) <= keepCount+1 {
		return &CompressionResult{
			Messages:    messages,
			Summary:     "",
			Removed:     0,
			Ratio:       0,
			CompressedAt: time.Now(),
		}, nil
	}

	systemMsg := messages[0]
	recentMsgs := messages[len(messages)-keepCount:]

	removed := len(messages) - keepCount - 1
	originalTokens := cc.estimateTokens(messages)

	summary := "[Previous conversation truncated]"

	compressed := []provider.Message{
		systemMsg,
		{Role: "system", Content: summary},
	}
	compressed = append(compressed, recentMsgs...)

	compressedTokens := cc.estimateTokens(compressed)

	return &CompressionResult{
		Messages:    compressed,
		Summary:     summary,
		Removed:     removed,
		Ratio:       1.0 - float64(compressedTokens)/float64(originalTokens),
		CompressedAt: time.Now(),
	}, nil
}

// estimateTokens estimates total token count
func (cc *ContextCompressor) estimateTokens(messages []provider.Message) int {
	total := 0
	for _, msg := range messages {
		// Rough estimate: ~4 chars per token
		total += len(msg.Content) / 4
		// Add overhead per message
		total += 4
	}
	return total
}

// FormatCompressedMessages formats messages for display with compression indicators
func (cc *ContextCompressor) FormatCompressedMessages(result *CompressionResult) []map[string]interface{} {
	var formatted []map[string]interface{}

	for _, msg := range result.Messages {
		item := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		formatted = append(formatted, item)
	}

	return formatted
}

// CompressionStats tracks compression history
type CompressionStats struct {
	TotalCompressions int
	TotalRemoved      int
	AvgRatio          float64
	LastCompression   time.Time
}

// GetStats returns compression statistics
func (cc *ContextCompressor) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"threshold":      cc.threshold,
		"ratio":          cc.ratio,
		"max_summary":   cc.maxSummary,
	}
}

// CompressWithStrategy applies different compression strategies
func (cc *ContextCompressor) CompressWithStrategy(
	ctx context.Context,
	messages []provider.Message,
	strategy string,
) (*CompressionResult, error) {
	switch strategy {
	case "aggressive":
		oldRatio := cc.ratio
		cc.ratio = 0.7
		result, err := cc.Compress(ctx, messages)
		cc.ratio = oldRatio
		return result, err
	case "conservative":
		oldRatio := cc.ratio
		cc.ratio = 0.3
		result, err := cc.Compress(ctx, messages)
		cc.ratio = oldRatio
		return result, err
	default:
		return cc.Compress(ctx, messages)
	}
}

// MarshalJSON for CompressionResult
func (r *CompressionResult) MarshalJSON() ([]byte, error) {
	type Alias CompressionResult
	return json.Marshal(&struct {
		*Alias
		CompressedAt string `json:"compressed_at"`
	}{
		Alias:        (*Alias)(r),
		CompressedAt: r.CompressedAt.Format(time.RFC3339),
	})
}
