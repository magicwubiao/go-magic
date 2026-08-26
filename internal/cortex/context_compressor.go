package cortex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// ContextCompressor handles intelligent context compression
// Based on Hermes Agent's context_compressor.py approach
type ContextCompressor struct {
	provider   provider.Provider
	threshold  int     // Token threshold before compression
	ratio      float64 // Compression ratio (0.0-1.0)
	maxSummary int     // Max tokens in summary
}

// CompressionResult contains the compressed context
type CompressionResult struct {
	Messages     []provider.Message
	Summary      string
	Removed      int     // Number of messages removed
	Ratio        float64 // Actual compression ratio
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

// findSafeKeepStart 从末尾向前保留 keepCount 条消息，但保证保留区首条
// 不是孤立的 tool 消息：若首条为 tool，则向前扩展到其调用方 assistant，
// 避免 provider 因 tool 消息与调用方 assistant 配对被切断而返回 400。
func findSafeKeepStart(messages []provider.Message, keepCount int) int {
	n := len(messages)
	if n == 0 || keepCount <= 0 {
		return n
	}
	if keepCount > n {
		keepCount = n
	}
	start := n - keepCount
	// 当 start 处为 tool 消息时，向前回退到其调用方 assistant，
	// 连续的 tool 消息（同一 assistant 的多个 tool_call 产物）会被一并纳入。
	for start > 0 && messages[start].Role == "tool" {
		start--
	}
	return start
}

// Compress compresses the middle portion of conversation history
func (cc *ContextCompressor) Compress(ctx context.Context, messages []provider.Message) (*CompressionResult, error) {
	if len(messages) < 6 {
		return &CompressionResult{
			Messages:     messages,
			Summary:      "",
			Removed:      0,
			Ratio:        0,
			CompressedAt: time.Now(),
		}, nil
	}

	// 校验首条是否为 system 消息，若非 system 则跳过 system 提取逻辑
	hasSystemMsg := len(messages) > 0 && messages[0].Role == "system"
	var systemMsg provider.Message
	if hasSystemMsg {
		systemMsg = messages[0]
	}

	// 保留最后 keepCount 条消息，但以 assistant 为边界避免切断 tool 配对
	keepCount := 4
	if len(messages) < keepCount+1 {
		keepCount = len(messages) - 1
	}
	keepStart := findSafeKeepStart(messages, keepCount)
	recentMsgs := messages[keepStart:]

	// 中间待压缩消息：从 system 之后（若有）到保留区之前
	middleStart := 0
	if hasSystemMsg {
		middleStart = 1
	}
	if keepStart <= middleStart {
		// 没有中间消息可压缩，直接返回原消息
		return &CompressionResult{
			Messages:     messages,
			Summary:      "",
			Removed:      0,
			Ratio:        0,
			CompressedAt: time.Now(),
		}, nil
	}
	middleMsgs := messages[middleStart:keepStart]

	if len(middleMsgs) == 0 {
		return &CompressionResult{
			Messages:     messages,
			Summary:      "",
			Removed:      0,
			Ratio:        0,
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
	var compressed []provider.Message
	if hasSystemMsg {
		compressed = append(compressed, systemMsg)
	}
	compressed = append(compressed, provider.Message{
		Role:    "system",
		Content: fmt.Sprintf("[Previous conversation summarized]\n\n%s", summary),
	})
	compressed = append(compressed, recentMsgs...)

	removed := len(middleMsgs)
	originalTokens := cc.estimateTokens(messages)
	compressedTokens := cc.estimateTokens(compressed)

	return &CompressionResult{
		Messages:     compressed,
		Summary:      summary,
		Removed:      removed,
		Ratio:        1.0 - float64(compressedTokens)/float64(originalTokens),
		CompressedAt: time.Now(),
	}, nil
}

// summarizeMessages uses LLM to summarize conversation history
func (cc *ContextCompressor) summarizeMessages(ctx context.Context, messages []provider.Message) (string, error) {
	// 按消息条数与 token 估算双重限制，避免硬截断 4000 字符对中英文不一视同仁：
	// - 最多 maxMessages 条消息参与摘要，超出时保留首尾以兼顾开头与近期上下文；
	// - 累计 token 估算（字符数 / charsPerToken）超过 maxTokens 则按消息边界停止，
	//   不在单条消息中间截断，避免切断多字节字符或破坏语义。
	const (
		maxMessages   = 20
		maxTokens     = 1500
		charsPerToken = 3
	)

	selected := messages
	dropped := false
	if len(messages) > maxMessages {
		// 保留首 head 条与末尾 tail 条，中间部分不参与摘要
		head := 2
		tail := maxMessages - head
		if tail > len(messages)-head {
			tail = len(messages) - head
		}
		selected = make([]provider.Message, 0, head+tail)
		selected = append(selected, messages[:head]...)
		selected = append(selected, messages[len(messages)-tail:]...)
		dropped = true
	}

	var parts []string
	totalTokens := 0
	truncated := false
	for _, msg := range selected {
		content := fmt.Sprintf("%s: %s", msg.Role, msg.Content)
		msgTokens := len(content) / charsPerToken
		if totalTokens+msgTokens > maxTokens {
			truncated = true
			break
		}
		parts = append(parts, content)
		totalTokens += msgTokens
	}
	conversation := strings.Join(parts, "\n\n")
	if dropped {
		conversation += "\n...(中间部分消息已省略)"
	}
	if truncated {
		conversation += "\n...(后续消息已省略)"
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
			Messages:     messages,
			Summary:      "",
			Removed:      0,
			Ratio:        0,
			CompressedAt: time.Now(),
		}, nil
	}

	// 校验首条是否为 system 消息
	hasSystemMsg := len(messages) > 0 && messages[0].Role == "system"
	var systemMsg provider.Message
	if hasSystemMsg {
		systemMsg = messages[0]
	}

	// 以 assistant 为边界保留，避免孤立 tool 消息
	keepStart := findSafeKeepStart(messages, keepCount)
	recentMsgs := messages[keepStart:]

	middleStart := 0
	if hasSystemMsg {
		middleStart = 1
	}
	if keepStart <= middleStart {
		return &CompressionResult{
			Messages:     messages,
			Summary:      "",
			Removed:      0,
			Ratio:        0,
			CompressedAt: time.Now(),
		}, nil
	}
	middleMsgs := messages[middleStart:keepStart]

	removed := len(middleMsgs)
	originalTokens := cc.estimateTokens(messages)

	// 抽取被压缩消息的关键信息，避免完全丢失上下文
	summary := cc.extractFallbackSummary(middleMsgs)

	var compressed []provider.Message
	if hasSystemMsg {
		compressed = append(compressed, systemMsg)
	}
	compressed = append(compressed, provider.Message{Role: "system", Content: summary})
	compressed = append(compressed, recentMsgs...)

	compressedTokens := cc.estimateTokens(compressed)

	return &CompressionResult{
		Messages:     compressed,
		Summary:      summary,
		Removed:      removed,
		Ratio:        1.0 - float64(compressedTokens)/float64(originalTokens),
		CompressedAt: time.Now(),
	}, nil
}

// extractFallbackSummary 在 LLM 摘要失败时，抽取被压缩消息的首末句与高频关键词，
// 至少保留部分信息，而非完全用 "[Previous conversation truncated]" 顶替。
func (cc *ContextCompressor) extractFallbackSummary(messages []provider.Message) string {
	if len(messages) == 0 {
		return "[Previous conversation truncated]"
	}

	var parts []string
	parts = append(parts, "[Previous conversation summarized]")

	// 首条消息的首句
	if first := firstSentence(messages[0].Content); first != "" {
		parts = append(parts, fmt.Sprintf("开头: %s", first))
	}
	// 末条消息的首句
	if last := firstSentence(messages[len(messages)-1].Content); last != "" {
		parts = append(parts, fmt.Sprintf("结尾: %s", last))
	}

	// 提取高频关键词
	keywords := extractTopKeywords(messages, 5)
	if len(keywords) > 0 {
		parts = append(parts, fmt.Sprintf("关键词: %s", strings.Join(keywords, ", ")))
	}

	if len(parts) == 1 {
		// 未能抽取到任何信息，回退到截断提示
		return "[Previous conversation truncated]"
	}
	return strings.Join(parts, "\n")
}

// firstSentence 返回文本的首句（按中英文句末标点或换行分隔），并按 rune 限长避免中文损坏
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	const maxLen = 120
	delimiters := "。.!?！？\n"
	endIdx := -1
	for i, r := range text {
		if strings.ContainsRune(delimiters, r) {
			endIdx = i
			break
		}
	}
	sentence := text
	if endIdx > 0 {
		sentence = text[:endIdx]
	}
	// 按 rune 截断避免在多字节字符中间断开
	runes := []rune(sentence)
	if len(runes) > maxLen {
		runes = runes[:maxLen]
	}
	return strings.TrimSpace(string(runes))
}

// extractTopKeywords 统计词频，返回出现次数最多的 n 个词
func extractTopKeywords(messages []provider.Message, n int) []string {
	freq := make(map[string]int)
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"to": true, "for": true, "of": true, "in": true, "on": true,
		"and": true, "or": true, "with": true, "by": true, "from": true,
		"it": true, "this": true, "that": true, "be": true, "do": true,
		"i": true, "you": true, "we": true, "he": true, "she": true,
		"的": true, "了": true, "是": true, "在": true, "和": true,
	}
	for _, msg := range messages {
		text := strings.ToLower(msg.Content)
		words := strings.Fields(text)
		for _, w := range words {
			w = strings.Trim(w, ".,!?;:\"'()[]{}。，！？；：")
			if len(w) <= 1 || stopWords[w] {
				continue
			}
			freq[w]++
		}
	}
	type kv struct {
		key string
		cnt int
	}
	var list []kv
	for k, v := range freq {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].cnt > list[j].cnt
	})
	if n > len(list) {
		n = len(list)
	}
	result := make([]string, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, list[i].key)
	}
	return result
}

// estimateTokens estimates total token count
func (cc *ContextCompressor) estimateTokens(messages []provider.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 3
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
		"threshold":   cc.threshold,
		"ratio":       cc.ratio,
		"max_summary": cc.maxSummary,
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
