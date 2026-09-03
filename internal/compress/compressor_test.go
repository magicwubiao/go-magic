package compress

import (
	"strings"
	"testing"
)

func TestNewCompressor(t *testing.T) {
	c := NewCompressor(8000)
	if c.ThresholdTokens != 8000 {
		t.Errorf("expected threshold=8000, got %d", c.ThresholdTokens)
	}
	if c.ProtectFirstN != 2 {
		t.Errorf("expected protectFirstN=2, got %d", c.ProtectFirstN)
	}
	if c.ProtectLastN != 4 {
		t.Errorf("expected protectLastN=4, got %d", c.ProtectLastN)
	}
}

func TestShouldCompress(t *testing.T) {
	c := NewCompressor(1000)
	if !c.ShouldCompress(1000) {
		t.Error("should compress at threshold")
	}
	if !c.ShouldCompress(1500) {
		t.Error("should compress above threshold")
	}
	if c.ShouldCompress(500) {
		t.Error("should not compress below threshold")
	}
}

func TestCompressShortMessages(t *testing.T) {
	c := NewCompressor(1000)
	messages := []Message{
		{Role: "system", Content: "You are an assistant"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	result, err := c.Compress(messages, "system prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Short messages should not be compressed
	if result.OriginalCount != result.CompressedCount {
		t.Errorf("short messages should not be compressed: %d -> %d", result.OriginalCount, result.CompressedCount)
	}
}

func TestCompressLongMessages(t *testing.T) {
	c := NewCompressor(100)
	// Create messages that exceed protected regions
	messages := []Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer with some tool calls"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Second answer"},
		{Role: "user", Content: "Third question"},
		{Role: "assistant", Content: "Third answer"},
		{Role: "user", Content: "Fourth question"},
		{Role: "assistant", Content: "Fourth answer"},
		{Role: "user", Content: "Fifth question"},
		{Role: "assistant", Content: "Fifth answer"},
		{Role: "user", Content: "Latest question"},
	}

	result, err := c.Compress(messages, "system prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should compress (original > protected + 1)
	if result.CompressedCount >= result.OriginalCount {
		t.Errorf("expected compression: %d -> %d", result.OriginalCount, result.CompressedCount)
	}

	// Should have summary
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}

	// Should have system summary message
	hasSummary := false
	for _, m := range result.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "CONTEXT COMPACTION") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Error("expected summary message in compressed output")
	}
}

func TestCompressPreservesHeadAndTail(t *testing.T) {
	c := NewCompressor(100)
	messages := []Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Second answer"},
		{Role: "user", Content: "Third question"},
		{Role: "assistant", Content: "Third answer"},
		{Role: "user", Content: "Fourth question"},
		{Role: "assistant", Content: "Fourth answer"},
		{Role: "user", Content: "Fifth question"},
		{Role: "assistant", Content: "Fifth answer"},
		{Role: "user", Content: "Latest question"},
	}

	result, err := c.Compress(messages, "system prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check head is preserved
	if result.Messages[0].Content != "System prompt" {
		t.Error("head message should be preserved")
	}

	// Check tail is preserved (latest user message)
	lastMsg := result.Messages[len(result.Messages)-1]
	if lastMsg.Content != "Latest question" {
		t.Errorf("tail message should be preserved, got: %s", lastMsg.Content)
	}
}

func TestUpdateFromResponse(t *testing.T) {
	c := NewCompressor(1000)
	c.UpdateFromResponse(5000)

	if c.GetLastPromptTokens() != 5000 {
		t.Errorf("expected 5000 tokens, got %d", c.GetLastPromptTokens())
	}
}

func TestShouldDeferPreflight(t *testing.T) {
	c := NewCompressor(1000)

	// No real data yet
	if c.ShouldDeferPreflight(1500) {
		t.Error("should not defer without real data")
	}

	// Set real data below threshold
	c.UpdateFromResponse(500)
	if !c.ShouldDeferPreflight(1500) {
		t.Error("should defer when real is below threshold but estimate is above")
	}

	// Set real data above threshold
	c.UpdateFromResponse(1500)
	if c.ShouldDeferPreflight(1500) {
		t.Error("should not defer when real is above threshold")
	}
}

func TestEstimateTokens(t *testing.T) {
	// Roughly 4 chars per token
	text := strings.Repeat("a", 400)
	tokens := EstimateTokens(text)
	if tokens != 100 {
		t.Errorf("expected ~100 tokens for 400 chars, got %d", tokens)
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: strings.Repeat("a", 400)},
		{Role: "assistant", Content: strings.Repeat("b", 400)},
	}
	systemPrompt := strings.Repeat("s", 400)

	tokens := EstimateMessagesTokens(messages, systemPrompt)
	// ~100 for system + ~100 for each message + overhead
	if tokens < 200 {
		t.Errorf("expected >200 tokens, got %d", tokens)
	}
}

func TestExtractPathMentions(t *testing.T) {
	text := "Check /path/to/file.go and ~/another/file.py"
	paths := extractPathMentions(text)
	if len(paths) < 2 {
		t.Errorf("expected at least 2 paths, got %d", len(paths))
	}
}

func TestDedupeStrings(t *testing.T) {
	items := []string{"a", "b", "a", "c", "b"}
	result := dedupeStrings(items)
	if len(result) != 3 {
		t.Errorf("expected 3 unique items, got %d", len(result))
	}
}

func TestTruncateString(t *testing.T) {
	s := "hello world"
	truncated := truncateString(s, 8)
	if truncated != "hello..." {
		t.Errorf("expected 'hello...', got '%s'", truncated)
	}

	// No truncation needed
	truncated = truncateString(s, 20)
	if truncated != s {
		t.Errorf("expected no truncation, got '%s'", truncated)
	}
}

func TestPruneToolOutput(t *testing.T) {
	longOutput := strings.Repeat("tool output data ", 100)
	pruned := PruneToolOutput(longOutput, 0)
	if !strings.Contains(pruned, "cleared") {
		t.Error("expected pruned output")
	}

	shortOutput := "short result"
	pruned = PruneToolOutput(shortOutput, 0)
	if pruned != shortOutput {
		t.Error("short output should not be pruned")
	}
}

func TestCompressorStats(t *testing.T) {
	c := NewCompressor(1000)
	c.UpdateFromResponse(500)

	stats := c.GetStats()
	if stats.ThresholdTokens != 1000 {
		t.Errorf("expected threshold=1000, got %d", stats.ThresholdTokens)
	}
	if stats.LastRealPromptTokens != 500 {
		t.Errorf("expected 500 tokens, got %d", stats.LastRealPromptTokens)
	}
}

func TestClearCache(t *testing.T) {
	c := NewCompressor(100)
	messages := []Message{
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "test2"},
		{Role: "assistant", Content: "response2"},
		{Role: "user", Content: "test3"},
		{Role: "assistant", Content: "response3"},
		{Role: "user", Content: "test4"},
		{Role: "assistant", Content: "response4"},
		{Role: "user", Content: "test5"},
		{Role: "assistant", Content: "response5"},
		{Role: "user", Content: "latest"},
	}

	// First compression populates cache
	_, err := c.Compress(messages, "system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats1 := c.GetStats()
	if stats1.CompressionCount != 1 {
		t.Errorf("expected 1 compression, got %d", stats1.CompressionCount)
	}

	// Clear cache
	c.ClearCache()

	// Second compression should regenerate (cache miss)
	_, err = c.Compress(messages, "system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats2 := c.GetStats()
	if stats2.CompressionCount != 2 {
		t.Errorf("expected 2 compressions, got %d", stats2.CompressionCount)
	}
}

func TestCompressionCount(t *testing.T) {
	c := NewCompressor(100)
	messages := make([]Message, 20)
	for i := range messages {
		messages[i] = Message{
			Role:    "user",
			Content: strings.Repeat("x", 50),
		}
	}

	_, err := c.Compress(messages, "system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := c.GetStats()
	if stats.CompressionCount != 1 {
		t.Errorf("expected compression count 1, got %d", stats.CompressionCount)
	}
}
