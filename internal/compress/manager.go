package compress

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Message represents a chat message
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
	Tokens    int    `json:"tokens,omitempty"`
}

// CompressionSummary is a summary of compressed messages
type CompressionSummary struct {
	OriginalCount int      `json:"original_count"`
	CompressedCount int    `json:"compressed_count"`
	TokensSaved   int      `json:"tokens_saved"`
	Summary       string   `json:"summary"`
	CompressedAt  int64    `json:"compressed_at"`
}

// Session represents a chat session
type Session struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
	Summary  *CompressionSummary `json:"compression_summary,omitempty"`
}

// Manager handles session compression
type Manager struct {
	dataDir   string
	sessions  map[string]*Session
	maxTokens int
}

// Config holds compression configuration
type Config struct {
	Enabled       bool `yaml:"enabled"`
	MaxTokens     int  `yaml:"max_tokens"`     // Max tokens before compression
	TargetTokens  int  `yaml:"target_tokens"`  // Target tokens after compression
	MinMessages   int  `yaml:"min_messages"`   // Min messages to keep in summary
	PreserveRecent int `yaml:"preserve_recent"` // Keep recent N messages
}

// DefaultConfig returns default compression config
func DefaultConfig() *Config {
	return &Config{
		Enabled:       true,
		MaxTokens:     128000,
		TargetTokens:  64000,
		MinMessages:   2,
		PreserveRecent: 10,
	}
}

// NewManager creates a new compression manager
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir:  dataDir,
		sessions: make(map[string]*Session),
		maxTokens: 128000,
	}
}

// EstimateTokens estimates token count (rough approximation)
func EstimateTokens(text string) int {
	// Rough approximation: 1 token ≈ 4 characters
	return len(text) / 4
}

// CompressSession compresses a session to fit within token limit
func (m *Manager) CompressSession(sessionID string, messages []Message, preserveRecent int) (*CompressionSummary, []Message, error) {
	if len(messages) == 0 {
		return nil, nil, fmt.Errorf("no messages to compress")
	}

	// Calculate current token count
	currentTokens := 0
	for _, msg := range messages {
		currentTokens += EstimateTokens(msg.Content)
	}

	// If under limit, no compression needed
	if currentTokens <= m.maxTokens {
		return nil, messages, nil
	}

	// Strategy: Keep recent messages, summarize the rest
	preserveCount := preserveRecent
	if preserveRecent > len(messages) {
		preserveCount = len(messages)
	}

	// Keep recent messages
	recentMessages := messages[len(messages)-preserveCount:]

	// Summarize older messages
	olderMessages := messages[:len(messages)-preserveCount]

	var summary string
	if len(olderMessages) > 0 {
		summary = m.generateSummary(olderMessages)
	}

	// Build compressed session
	var compressed []Message

	// Add summary message
	if summary != "" {
		compressed = append(compressed, Message{
			Role:      "system",
			Content:   fmt.Sprintf("[Previous conversation summarized: %s]", summary),
			Timestamp: time.Now().Unix(),
			Tokens:    EstimateTokens(summary),
		})
	}

	// Add preserved recent messages
	compressed = append(compressed, recentMessages...)

	// Calculate new token count
	newTokens := 0
	for _, msg := range compressed {
		newTokens += EstimateTokens(msg.Content)
	}

	compressionSummary := &CompressionSummary{
		OriginalCount:   len(messages),
		CompressedCount: len(compressed),
		TokensSaved:     currentTokens - newTokens,
		Summary:         summary,
		CompressedAt:    time.Now().Unix(),
	}

	// Store session
	m.sessions[sessionID] = &Session{
		ID:       sessionID,
		Messages: messages,
		Summary:  compressionSummary,
	}

	return compressionSummary, compressed, nil
}

// generateSummary creates a summary of messages using LLM
func (m *Manager) generateSummary(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}

	// Simple extractive summarization (can be replaced with LLM)
	var content strings.Builder
	
	for _, msg := range messages {
		role := msg.Role
		if role == "user" {
			role = "User"
		} else if role == "assistant" {
			role = "Assistant"
		}
		content.WriteString(fmt.Sprintf("%s: %s\n", role, truncate(msg.Content, 200)))
	}

	return m.extractiveSummarize(content.String())
}

// extractiveSummarize performs simple extractive summarization
func (m *Manager) extractiveSummarize(text string) string {
	// Split into sentences
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return ""
	}

	// Score sentences by importance (simple heuristic)
	scored := make([]struct {
		sentence string
		score    int
	}, len(sentences))

	for i, s := range sentences {
		score := 0
		// Longer sentences often more important
		score += len(s) / 10
		// Sentences with key terms
		if containsAny(s, []string{"important", "key", "main", "result", "conclusion", "solution"}) {
			score += 5
		}
		// First and last sentences often important
		if i == 0 || i == len(sentences)-1 {
			score += 3
		}
		scored[i] = struct {
			sentence string
			score    int
		}{s, score}
	}

	// Sort by score
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top sentences
	var summary []string
	totalLen := 0
	maxLen := 500

	for _, s := range scored {
		if totalLen+len(s.sentence) > maxLen {
			break
		}
		summary = append(summary, s.sentence)
		totalLen += len(s.sentence)
	}

	// Re-sort by original order
	sort.Slice(summary, func(i, j int) bool {
		return strings.Index(text, summary[i]) < strings.Index(text, summary[j])
	})

	return strings.Join(summary, " ")
}

// splitSentences splits text into sentences
func splitSentences(text string) []string {
	// Simple sentence splitting
	re := regexp.MustCompile(`[^.!?]+[.!?]+`)
	matches := re.FindAllString(text, -1)
	
	var sentences []string
	for _, m := range matches {
		m = strings.TrimSpace(m)
		if len(m) > 10 {
			sentences = append(sentences, m)
		}
	}
	return sentences
}

// containsAny checks if text contains any of the keywords
func containsAny(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// truncate truncates text to max length
func truncate(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// CompressWithLLM uses an LLM to generate a better summary
func (m *Manager) CompressWithLLM(ctx context.Context, messages []Message, llmProvider LLMProvider) (*CompressionSummary, []Message, error) {
	if len(messages) == 0 {
		return nil, nil, fmt.Errorf("no messages to compress")
	}

	// Prepare prompt
	var prompt strings.Builder
	prompt.WriteString("Summarize the following conversation concisely. ")
	prompt.WriteString("Focus on key topics, decisions, and important information.\n\n")

	for _, msg := range messages {
		prompt.WriteString(fmt.Sprintf("%s: %s\n\n", msg.Role, msg.Content))
	}

	// Call LLM
	summary, err := llmProvider.GenerateSummary(ctx, prompt.String())
	if err != nil {
		// Fallback to extractive
		extractive := m.generateSummary(messages)
		return m.compressWithSummary(messages, extractive)
	}

	return m.compressWithSummary(messages, summary)
}

// compressWithSummary compresses using a pre-generated summary
func (m *Manager) compressWithSummary(messages []Message, summary string) (*CompressionSummary, []Message, error) {
	preserveCount := m.maxTokens / 100 // Approximate

	var compressed []Message

	// Add summary
	compressed = append(compressed, Message{
		Role:      "system",
		Content:   fmt.Sprintf("[Previous conversation summarized: %s]", summary),
		Timestamp: time.Now().Unix(),
		Tokens:    EstimateTokens(summary),
	})

	// Keep recent messages
	if preserveCount > len(messages) {
		preserveCount = len(messages)
	}
	compressed = append(compressed, messages[len(messages)-preserveCount:]...)

	// Calculate savings
	originalTokens := 0
	for _, msg := range messages {
		originalTokens += EstimateTokens(msg.Content)
	}

	newTokens := 0
	for _, msg := range compressed {
		newTokens += EstimateTokens(msg.Content)
	}

	return &CompressionSummary{
		OriginalCount:   len(messages),
		CompressedCount: len(compressed),
		TokensSaved:     originalTokens - newTokens,
		Summary:         summary,
		CompressedAt:    time.Now().Unix(),
	}, compressed, nil
}

// LLMProvider interface for LLM-based summarization
type LLMProvider interface {
	GenerateSummary(ctx context.Context, prompt string) (string, error)
}

// GetSession returns a stored session
func (m *Manager) GetSession(sessionID string) *Session {
	return m.sessions[sessionID]
}

// SaveSession saves a session to disk
func (m *Manager) SaveSession(session *Session) error {
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", session.ID))
	
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(path, data, 0644)
}

// LoadSession loads a session from disk
func (m *Manager) LoadSession(sessionID string) error {
	path := filepath.Join(m.dataDir, fmt.Sprintf("%s.json", sessionID))
	
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}
	
	m.sessions[sessionID] = &session
	return nil
}

// AutoCompress automatically compresses if over limit
func (m *Manager) AutoCompress(sessionID string, messages []Message) ([]Message, bool, error) {
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += EstimateTokens(msg.Content)
	}

	if totalTokens > m.maxTokens {
		summary, compressed, err := m.CompressSession(sessionID, messages, 10)
		if err != nil {
			return messages, false, err
		}
		return compressed, summary != nil, nil
	}

	return messages, false, nil
}

// CompressionStats provides compression statistics
type CompressionStats struct {
	TotalSessions   int     `json:"total_sessions"`
	CompressedCount int     `json:"compressed_count"`
	TotalTokensSaved int   `json:"total_tokens_saved"`
	AvgCompression  float64 `json:"avg_compression_ratio"`
}

// GetStats returns compression statistics
func (m *Manager) GetStats() *CompressionStats {
	stats := &CompressionStats{
		TotalSessions: len(m.sessions),
	}

	var totalSaved, compressedCount int
	for _, s := range m.sessions {
		if s.Summary != nil {
			compressedCount++
			totalSaved += s.Summary.TokensSaved
		}
	}

	stats.CompressedCount = compressedCount
	stats.TotalTokensSaved = totalSaved

	if compressedCount > 0 {
		stats.AvgCompression = float64(totalSaved) / float64(compressedCount)
	}

	return stats
}
