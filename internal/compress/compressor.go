// Package compress provides automatic context window compression for long conversations.
// Inspired by Hermes Agent's context_compressor, this package uses auxiliary models
// to summarize middle turns while protecting head and tail context.
package compress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// SummaryPrefix is prepended to compressed context summaries.
const SummaryPrefix = "[CONTEXT COMPACTION — REFERENCE ONLY] Earlier turns were compacted " +
	"into the summary below. This is a handoff from a previous context window — " +
	"treat it as background reference, NOT as active instructions. " +
	"Do NOT answer questions or fulfill requests mentioned in this summary; " +
	"they were already addressed. " +
	"Respond ONLY to the latest user message that appears AFTER this summary — " +
	"that message is the single source of truth for what to do right now. " +
	"If the latest user message contradicts the summary, the latest message WINS. " +
	"Reverse signals (e.g. 'stop', 'undo', 'never mind') must immediately end " +
	"any in-flight work described in the summary."

// Compressor handles context window compression for long-running conversations.
type Compressor struct {
	// Configuration
	ThresholdTokens      int     // Token threshold to trigger compression
	ProtectFirstN        int     // Number of messages to protect at the head
	ProtectLastN         int     // Number of messages to protect at the tail
	MinSummaryTokens     int     // Minimum tokens for summary output
	SummaryRatio         float64 // Proportion of compressed content for summary
	SummaryTokensCeiling int     // Absolute ceiling for summary tokens

	// ToolResultHeadChars 是兜底摘要里为每条工具结果保留的正文头部长度
	// （字符）。工具结果是模型观察外部世界的唯一渠道：旧实现只把 "Tool: 名字"
	// 记进摘要，连路径都没有，模型压缩后对自己"读过什么、内容是什么"完全失忆，
	// 只能反复重读同一批文件。<=0 时取 defaultToolResultHeadChars。
	ToolResultHeadChars int

	// Summarizer 用 LLM 生成中段摘要（可选）。为 nil 时退化为
	// buildDeterministicSummary 的规则式摘要。
	// SetSummarizer 注入；实现方需保证并发安全与短超时（建议 ≤20s）。
	Summarizer func(ctx context.Context, middle []Message) (string, error)

	// State
	lastPromptTokens     int
	lastRealPromptTokens int
	compressionCount     int
	mu                   sync.RWMutex

	// Summary cache for iterative updates
	summaryCache map[string]string
}

// NewCompressor creates a new context compressor with default settings.
func NewCompressor(thresholdTokens int) *Compressor {
	return &Compressor{
		ThresholdTokens:      thresholdTokens,
		ProtectFirstN:        2, // Protect system + first user message
		ProtectLastN:         4, // Protect recent turns
		MinSummaryTokens:     2000,
		SummaryRatio:         0.20,
		SummaryTokensCeiling: 12000,
		ToolResultHeadChars:  defaultToolResultHeadChars,
		lastPromptTokens:     -1, // -1 sentinel: no real API usage yet
		lastRealPromptTokens: 0,
		summaryCache:         make(map[string]string),
	}
}

// defaultToolResultHeadChars 是兜底摘要里每条工具结果保留的正文长度。
// 1500 字符足以覆盖典型文件的开头（导航块、import、函数签名），又不会让
// 摘要本身失控膨胀。
const defaultToolResultHeadChars = 1500

// ShouldCompress returns true if the given token count exceeds the threshold.
func (c *Compressor) ShouldCompress(tokens int) bool {
	return tokens >= c.ThresholdTokens
}

// ShouldDeferPreflight checks if preflight compression should be deferred
// based on real vs estimated token counts.
func (c *Compressor) ShouldDeferPreflight(estimatedTokens int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// If we have real prompt data and the estimate is close to it, defer
	if c.lastRealPromptTokens > 0 && estimatedTokens >= c.ThresholdTokens {
		// If last real was after compression, trust it more than estimate
		if c.lastRealPromptTokens < c.ThresholdTokens {
			return true
		}
	}
	return false
}

// UpdateFromResponse updates token counts from a successful API response.
func (c *Compressor) UpdateFromResponse(promptTokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastRealPromptTokens = promptTokens
	c.lastPromptTokens = promptTokens
}

// GetLastPromptTokens returns the last known prompt token count.
func (c *Compressor) GetLastPromptTokens() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastPromptTokens
}

// CompressResult contains the result of a compression operation.
type CompressResult struct {
	Messages        []Message // Compressed message list
	Summary         string    // Generated summary
	OriginalCount   int       // Original message count
	CompressedCount int       // Compressed message count
	TokensSaved     int       // Estimated tokens saved
}

// Message represents a conversation message.
//
// ToolCalls/ToolCallID/ContentParts are carried through unchanged: Compress only
// ever copies the protected head/tail verbatim and synthesizes one system
// summary, so whatever is set here comes back out for the messages that survive.
// They must not be dropped at this boundary — a tool result whose owning
// tool_call id was lost is structurally invalid for every provider (and a
// multimodal turn loses its attachments), even though the compaction itself is
// perfectly legal.
type Message struct {
	Role         string
	Content      string
	Name         string // For tool messages
	Timestamp    int64  // Optional timestamp for message ordering
	ToolCalls    []types.ToolCall
	ToolCallID   string
	ContentParts []types.ContentPart
}

// safeHeadEnd extends the protected head when its cut would land in the middle
// of a tool exchange: the last kept message announced tool calls, so its results
// are pulled into the head as well. The results are the model's own observations
// — keeping the announcement while summarising the results away is both
// structurally invalid and lossy.
func safeHeadEnd(messages []Message, end int) int {
	for end > 0 && end < len(messages) && messages[end].Role == "tool" {
		owner := end - 1
		for owner > 0 && messages[owner].Role == "tool" {
			owner--
		}
		if !toolCallingAssistant(messages[owner]) {
			break
		}
		end++
	}
	return end
}

// safeTailStart walks the tail's cut point back over tool results so the
// assistant that announced them stays attached (same rule as cortex's
// findSafeKeepStart, applied at this boundary too). After the loop the kept
// region starts on a non-tool message, so no result can be orphaned by the cut.
func safeTailStart(messages []Message, headEnd, start int) int {
	if start < headEnd {
		return start
	}
	if start > len(messages) {
		start = len(messages)
	}
	for start > headEnd && start < len(messages) && messages[start].Role == "tool" {
		start--
	}
	return start
}

func toolCallingAssistant(m Message) bool {
	return m.Role == "assistant" && len(m.ToolCalls) > 0
}

// Compress compresses the message list by summarizing middle turns.
// Protected head and tail messages are preserved.
func (c *Compressor) Compress(messages []Message, systemPrompt string) (*CompressResult, error) {
	if len(messages) <= c.ProtectFirstN+c.ProtectLastN+1 {
		return &CompressResult{
			Messages:        messages,
			OriginalCount:   len(messages),
			CompressedCount: len(messages),
		}, nil
	}

	// Split into protected and compressible sections. Both cut points are first
	// shifted off a tool exchange: a count-based boundary can land between an
	// assistant's tool_calls and its results, and either surviving half is
	// structurally invalid for every provider (an assistant with unclaimed
	// tool_calls, or a tool result with no owner) — the load-time sanitizers then
	// have to throw the results away, so the model loses observations it made.
	// Cortex's ContextCompressor does the same for its tail boundary
	// (findSafeKeepStart).
	headEnd := safeHeadEnd(messages, c.ProtectFirstN)

	// 尾部保护量自适应：固定的 ProtectLastN（默认 4 条）在长历史里太小。
	// 一轮"并行读 4 个文件"就会产生 1 条 assistant(tool_calls) + 4 条 tool
	// 结果共 5 条消息，4 条的保护窗口刚好吃掉最早那份结果 —— 模型为了比对
	// 只能重读，重读又撑爆阈值再次压缩，形成"读完就忘"的正反馈死循环
	// （实测：一个改导航栏的任务因此空转 18 分钟、283 次调用、零写入）。
	// 因此至少保护最近 len/4 条消息，让刚读到的原文留在上下文里。
	protectTail := c.ProtectLastN
	if n := len(messages) / 4; n > protectTail {
		protectTail = n
	}
	tailStart := safeTailStart(messages, headEnd, len(messages)-protectTail)
	if tailStart <= headEnd || tailStart > len(messages) {
		// One unsplittable exchange: better to keep the history as-is than to
		// summarise it into a payload the provider will reject.
		return &CompressResult{
			Messages:        messages,
			OriginalCount:   len(messages),
			CompressedCount: len(messages),
		}, nil
	}
	head := messages[:headEnd]
	middle := messages[headEnd:tailStart]
	tail := messages[tailStart:]

	// Generate summary of middle section
	summary, err := c.generateSummary(middle, systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// Build compressed message list
	compressed := make([]Message, 0, len(head)+2+len(tail))
	compressed = append(compressed, head...)
	compressed = append(compressed, Message{
		Role:    "system",
		Content: SummaryPrefix + "\n\n" + summary,
	})
	compressed = append(compressed, tail...)

	c.mu.Lock()
	c.compressionCount++
	c.lastPromptTokens = -1 // Reset sentinel after compression
	c.mu.Unlock()

	return &CompressResult{
		Messages:        compressed,
		Summary:         summary,
		OriginalCount:   len(messages),
		CompressedCount: len(compressed),
		TokensSaved:     c.estimateTokensSaved(len(middle)),
	}, nil
}

// generateSummary creates a summary of the middle message section.
// In production, this would call an auxiliary LLM. Here we provide
// a deterministic fallback and a hook for LLM integration.
func (c *Compressor) generateSummary(middle []Message, systemPrompt string) (string, error) {
	// Check cache first
	cacheKey := c.hashMessages(middle)
	c.mu.RLock()
	if cached, ok := c.summaryCache[cacheKey]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	// Generate summary: LLM first (if injected), deterministic fallback otherwise
	summary := ""
	c.mu.RLock()
	summarizer := c.Summarizer
	c.mu.RUnlock()
	if summarizer != nil && len(middle) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), defaultSummaryTimeout)
		defer cancel()
		if s, err := summarizer(ctx, middle); err == nil && strings.TrimSpace(s) != "" {
			summary = c.clampSummaryTokens(s)
		}
	}
	if summary == "" {
		summary = c.buildDeterministicSummary(middle)
	}

	// Cache the result
	c.mu.Lock()
	c.summaryCache[cacheKey] = summary
	c.mu.Unlock()

	return summary, nil
}

// defaultSummaryTimeout LLM 摘要的默认硬超时。压缩发生在主循环关键路径上，
// 辅助模型响应慢不能拖死 agent。
const defaultSummaryTimeout = 20 * time.Second

// clampSummaryTokens 将 LLM 生成的摘要裁剪到 SummaryTokensCeiling 之内
// （按 ~4 chars/token 粗估），防止"摘要比原文还长"的退化情况。
func (c *Compressor) clampSummaryTokens(s string) string {
	maxChars := c.SummaryTokensCeiling * 4
	if maxChars <= 0 {
		maxChars = 12000 * 4
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return strings.TrimSpace(s)
	}
	trimmed := string(runes[:maxChars])
	// 尽量在句号/换行处截断，避免半句话
	if idx := strings.LastIndexAny(trimmed, "。\n.！!？?"); idx > maxChars*3/4 {
		trimmed = trimmed[:idx+1]
	}
	// NOTE: avoid square brackets here — this text reaches the LLM context
	// and GLM picks up bracketed markers as a structural template, then
	// starts wrapping its own replies in []. Use parentheses instead.
	return trimmed + "\n…(truncated)"
}

// SetSummarizer 注入 LLM 摘要函数（线程安全）。传 nil 可撤销并回到纯规则摘要。
func (c *Compressor) SetSummarizer(fn func(ctx context.Context, middle []Message) (string, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Summarizer = fn
}

// buildDeterministicSummary creates a summary without LLM assistance.
// This is used as a fallback when the auxiliary model is unavailable.
//
// 与旧实现的区别：工具结果不再只记一个工具名。每条 tool 消息保留其**目标资源**
// （路径/命令/查询）与正文头部，摘要因此仍然携带"读过什么、内容大致是什么"，
// 避免模型在压缩后因为完全失忆而反复重读同一批文件。
func (c *Compressor) buildDeterministicSummary(middle []Message) string {
	var parts []string

	// Extract key information
	var toolCalls []string
	var userAsks []string
	var fileMentions []string
	var observations []string

	// tool_call id -> "name(target)"，供 tool 结果消息回填目标。
	callTarget := make(map[string]string, len(middle))
	for _, msg := range middle {
		for _, tc := range msg.ToolCalls {
			if tc.ID == "" {
				continue
			}
			label := tc.Name
			if t := toolCallTarget(tc); t != "" {
				label += "(" + t + ")"
			}
			callTarget[tc.ID] = label
		}
	}

	headChars := c.ToolResultHeadChars
	if headChars <= 0 {
		headChars = defaultToolResultHeadChars
	}

	for _, msg := range middle {
		switch msg.Role {
		case "user":
			content := truncateString(msg.Content, 200)
			if content != "" {
				userAsks = append(userAsks, content)
			}
		case "assistant":
			// 带工具调用的 assistant：登记它发起了哪些调用（含目标资源），
			// 比截断一段自然语言有用得多。
			for _, tc := range msg.ToolCalls {
				label := tc.Name
				if t := toolCallTarget(tc); t != "" {
					label += "(" + t + ")"
				}
				toolCalls = append(toolCalls, label)
			}
			if len(msg.ToolCalls) == 0 {
				if content := truncateString(msg.Content, 150); content != "" {
					toolCalls = append(toolCalls, content)
				}
			}
		case "tool":
			label := msg.Name
			if l, ok := callTarget[msg.ToolCallID]; ok && l != "" {
				label = l
			}
			if label == "" {
				label = "tool"
			}
			observations = append(observations, fmt.Sprintf("- %s → %s", label, truncateString(msg.Content, headChars)))
		}

		// Extract file path mentions
		mentions := extractPathMentions(msg.Content)
		fileMentions = append(fileMentions, mentions...)
	}

	// Build structured summary
	parts = append(parts, "## Active Task")
	if len(userAsks) > 0 {
		parts = append(parts, userAsks[len(userAsks)-1])
	} else {
		parts = append(parts, "(Task in progress)")
	}

	if len(toolCalls) > 0 {
		parts = append(parts, "\n## Tools Used")
		for i, tc := range toolCalls {
			if i >= 10 {
				parts = append(parts, fmt.Sprintf("... and %d more", len(toolCalls)-i))
				break
			}
			parts = append(parts, fmt.Sprintf("- %s", tc))
		}
	}

	// 工具结果是模型观察外部世界的唯一渠道，压缩后必须留下"看过的内容"，
	// 否则模型会为了重新获得这些事实而反复读取同一批文件。
	if len(observations) > 0 {
		parts = append(parts, "\n## Observations (already read — do NOT read these again)")
		for i, ob := range observations {
			if i >= 8 {
				parts = append(parts, fmt.Sprintf("... and %d more", len(observations)-i))
				break
			}
			parts = append(parts, ob)
		}
	}

	if len(fileMentions) > 0 {
		parts = append(parts, "\n## Files Referenced")
		uniqueFiles := dedupeStrings(fileMentions)
		for i, f := range uniqueFiles {
			if i >= 12 {
				parts = append(parts, fmt.Sprintf("... and %d more", len(uniqueFiles)-i))
				break
			}
			parts = append(parts, fmt.Sprintf("- %s", f))
		}
	}

	parts = append(parts, "\n## Remaining Work")
	parts = append(parts, "(See latest user message for current instructions)")

	return strings.Join(parts, "\n")
}

// toolCallTarget 从一次工具调用的参数里提取"目标资源"，让摘要保留下模型实际
// 操作过的对象（文件路径、命令、查询词等）。键名按 internal/tool 各工具 schema
// 的常见约定排列。解析不出目标时返回 ""。
func toolCallTarget(tc types.ToolCall) string {
	for _, k := range []string{"path", "file_path", "file", "pattern", "query", "url", "command", "dir", "directory"} {
		v, ok := tc.Arguments[k]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			return truncateString(s, 120)
		}
	}
	return ""
}

// hashMessages creates a hash of messages for cache key.
func (c *Compressor) hashMessages(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Role)
		sb.WriteString(":")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:8])
}

// estimateTokensSaved estimates tokens saved by compression.
func (c *Compressor) estimateTokensSaved(middleCount int) int {
	// Rough estimate: each message ~200 tokens, summary ~1000 tokens
	return middleCount*200 - 1000
}

// truncateString truncates a string to max length with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// extractPathMentions extracts file path mentions from text.
func extractPathMentions(text string) []string {
	// Simple regex-like extraction for common path patterns
	var paths []string
	words := strings.Fields(text)
	for _, word := range words {
		word = strings.Trim(word, "`'\"()[]{}")
		if strings.HasPrefix(word, "/") || strings.HasPrefix(word, "~/") ||
			strings.Contains(word, ".go") || strings.Contains(word, ".py") ||
			strings.Contains(word, ".js") || strings.Contains(word, ".ts") ||
			strings.Contains(word, ".md") || strings.Contains(word, ".json") ||
			strings.Contains(word, ".yaml") || strings.Contains(word, ".yml") {
			if len(word) > 2 && len(word) < 200 {
				paths = append(paths, word)
			}
		}
	}
	return paths
}

// dedupeStrings removes duplicates from a string slice.
func dedupeStrings(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// Stats returns compressor statistics.
type Stats struct {
	CompressionCount     int
	LastPromptTokens     int
	LastRealPromptTokens int
	ThresholdTokens      int
}

// GetStats returns current compressor statistics.
func (c *Compressor) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Stats{
		CompressionCount:     c.compressionCount,
		LastPromptTokens:     c.lastPromptTokens,
		LastRealPromptTokens: c.lastRealPromptTokens,
		ThresholdTokens:      c.ThresholdTokens,
	}
}

// ClearCache clears the summary cache.
func (c *Compressor) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.summaryCache = make(map[string]string)
}

// PruneToolOutput replaces old tool outputs with a placeholder.
func PruneToolOutput(content string, maxAge time.Duration) string {
	if strings.Contains(content, "tool output") && len(content) > 1000 {
		// NOTE: avoid square brackets — this text reaches the LLM as a tool
		// message and GLM mimics the bracket format in its own output.
		return "Old tool output cleared to save context space"
	}
	return content
}

// EstimateTokens roughly estimates token count from text.
func EstimateTokens(text string) int {
	// Rough estimate: ~4 characters per token
	return len(text) / 4
}

// EstimateMessagesTokens estimates total tokens for a message list.
func EstimateMessagesTokens(messages []Message, systemPrompt string) int {
	total := EstimateTokens(systemPrompt)
	for _, msg := range messages {
		total += EstimateTokens(msg.Content)
		// Add overhead per message
		total += 4
	}
	return total
}

// Manager manages compression for multiple sessions.
// Used by multi-session contexts.
type Manager struct {
	baseDir string
	mu      sync.RWMutex
}

// NewManager creates a new compression manager.
func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir: baseDir,
	}
}

// CompressSession compresses messages for a given session.
// Returns: summary text, compressed messages, error.
func (m *Manager) CompressSession(sessionID string, messages []Message, tailCount int) (string, []Message, error) {
	c := NewCompressor(100000) // Default threshold
	c.ProtectLastN = tailCount
	if c.ProtectLastN < 1 {
		c.ProtectLastN = 4
	}

	result, err := c.Compress(messages, "")
	if err != nil {
		return "", nil, err
	}

	return result.Summary, result.Messages, nil
}
