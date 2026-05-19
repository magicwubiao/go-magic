package compress

import "testing"

func TestEstimateTokens_Empty(t *testing.T) {
	result := EstimateTokens("")
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestEstimateTokens_English(t *testing.T) {
	// ~4 chars per token for English
	text := "Hello, this is a test message for token estimation."
	result := EstimateTokens(text)
	if result <= 0 {
		t.Errorf("expected positive tokens, got %d", result)
	}
	// 61 chars / 4 = ~15 tokens
	if result > 25 {
		t.Errorf("too many tokens for English text: %d", result)
	}
}

func TestEstimateTokens_CJK(t *testing.T) {
	// CJK characters should use ~1.5 tokens per char
	text := "你好世界这是一个测试"
	result := EstimateTokens(text)
	if result <= 0 {
		t.Errorf("expected positive tokens, got %d", result)
	}
	// 12 CJK chars * 2/3 (integer division) = 8 tokens
	if result < 5 {
		t.Errorf("too few tokens for CJK text: %d", result)
	}
}

func TestEstimateTokens_Mixed(t *testing.T) {
	text := "Hello你好World世界"
	result := EstimateTokens(text)
	if result <= 0 {
		t.Errorf("expected positive tokens, got %d", result)
	}
}

func TestEstimateTokens_SingleChar(t *testing.T) {
	result := EstimateTokens("a")
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestEstimateTokens_Korean(t *testing.T) {
	text := "안녕하세요"
	result := EstimateTokens(text)
	if result <= 0 {
		t.Errorf("expected positive tokens for Korean, got %d", result)
	}
}

func TestEstimateTokens_Japanese(t *testing.T) {
	text := "こんにちは"
	result := EstimateTokens(text)
	if result <= 0 {
		t.Errorf("expected positive tokens for Japanese, got %d", result)
	}
}

func TestCompressSession_NoMessages(t *testing.T) {
	mgr := NewManager("/tmp/test")
	_, _, err := mgr.CompressSession("test", nil, 10)
	if err == nil {
		t.Error("expected error for nil messages")
	}
}

func TestCompressSession_UnderLimit(t *testing.T) {
	mgr := NewManager("/tmp/test")
	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	_, result, err := mgr.CompressSession("test", messages, 10)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 messages (under limit), got %d", len(result))
	}
}

func TestCompressSession_OverLimit(t *testing.T) {
	mgr := NewManager("/tmp/test")
	mgr.maxTokens = 100 // Very low limit to trigger compression

	messages := make([]Message, 50)
	for i := range messages {
		messages[i] = Message{
			Role:      "user",
			Content:   "This is a longer message to trigger compression.",
			Timestamp: int64(i),
		}
	}

	summary, result, err := mgr.CompressSession("test", messages, 5)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if summary == nil {
		t.Error("expected non-nil summary")
	}
	if len(result) >= len(messages) {
		t.Errorf("compressed should be smaller: %d >= %d", len(result), len(messages))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Error("default config should be enabled")
	}
	if cfg.MaxTokens != 128000 {
		t.Errorf("expected MaxTokens 128000, got %d", cfg.MaxTokens)
	}
	if cfg.PreserveRecent != 10 {
		t.Errorf("expected PreserveRecent 10, got %d", cfg.PreserveRecent)
	}
}

func TestAutoCompress_UnderLimit(t *testing.T) {
	mgr := NewManager("/tmp/test")
	messages := []Message{
		{Role: "user", Content: "Short message"},
	}
	result, compressed, err := mgr.AutoCompress("test", messages)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if compressed {
		t.Error("should not compress under limit")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}
}

func TestExtractiveSummarize(t *testing.T) {
	mgr := NewManager("/tmp/test")
	text := "This is the first sentence. This is the second sentence with important result. This is the third."
	result := mgr.extractiveSummarize(text)
	if result == "" {
		t.Error("expected non-empty summary")
	}
}

func TestSplitSentences(t *testing.T) {
	text := "First sentence. Second sentence! Third sentence? Fourth."
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		t.Error("expected sentences")
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("This is important", []string{"important", "key"}) {
		t.Error("should find keyword")
	}
	if containsAny("Hello world", []string{"missing"}) {
		t.Error("should not find keyword")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("short text should not be truncated")
	}
	result := truncate("hello world", 5)
	if result != "hello..." {
		t.Errorf("expected 'hello...', got '%s'", result)
	}
}

func TestGetStats(t *testing.T) {
	mgr := NewManager("/tmp/test")
	stats := mgr.GetStats()
	if stats == nil {
		t.Error("expected non-nil stats")
	}
	if stats.TotalSessions != 0 {
		t.Errorf("expected 0 sessions, got %d", stats.TotalSessions)
	}
}

func TestNewManager(t *testing.T) {
	mgr := NewManager("/tmp/test")
	if mgr == nil {
		t.Error("expected non-nil manager")
	}
	if mgr.maxTokens != 128000 {
		t.Errorf("expected maxTokens 128000, got %d", mgr.maxTokens)
	}
}
