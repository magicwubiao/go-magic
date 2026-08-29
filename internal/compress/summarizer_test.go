package compress

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// sumTestMsgs 构造 n 条中段测试消息。
func sumTestMsgs(n int) []Message {
	msgs := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, Message{
			Role:    "user",
			Content: strings.Repeat("重要上下文内容 ", 30),
		})
	}
	return msgs
}

func TestSetSummarizer_LLMUsed(t *testing.T) {
	c := NewCompressor(100)
	c.SetSummarizer(func(ctx context.Context, middle []Message) (string, error) {
		return "LLM摘要：完成了A、B两步，C待办", nil
	})

	messages := append(sumTestMsgs(10),
		Message{Role: "user", Content: "tail1"},
	)
	result, err := c.Compress(messages, "sys")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range result.Messages {
		if strings.Contains(m.Content, "LLM摘要：完成了A、B") {
			found = true
		}
	}
	if !found {
		t.Fatal("LLM summary not used in compressed output")
	}
}

func TestSummarizer_ErrorFallsBackToDeterministic(t *testing.T) {
	c := NewCompressor(100)
	c.SetSummarizer(func(ctx context.Context, middle []Message) (string, error) {
		return "", errors.New("llm down")
	})

	messages := sumTestMsgs(12)
	result, err := c.Compress(messages, "sys")
	if err != nil {
		t.Fatalf("fallback must swallow summarizer error, got: %v", err)
	}
	if len(result.Summary) == 0 {
		t.Fatal("expected deterministic fallback summary")
	}
}

func TestSummarizer_EmptyOutputFallsBack(t *testing.T) {
	c := NewCompressor(100)
	c.SetSummarizer(func(ctx context.Context, middle []Message) (string, error) {
		return "   \n  ", nil // 空白输出视为失败
	})
	result, err := c.Compress(sumTestMsgs(12), "sys")
	if err != nil || result.Summary == "" {
		t.Fatalf("blank LLM output must fall back; err=%v summary=%q", err, result.Summary)
	}
}

func TestClampSummaryTokens(t *testing.T) {
	c := NewCompressor(8000)
	c.SummaryTokensCeiling = 5 // 20 chars

	long := strings.Repeat("长", 15) + "。" + strings.Repeat("尾", 30)
	got := c.clampSummaryTokens(long)
	if len([]rune(got)) > 25+8 { // 允许截断标记的余量
		t.Fatalf("summary not clamped: %d runes", len([]rune(got)))
	}
	if !strings.Contains(got, "(truncated)") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}

func TestSummaryTimeout_Bounded(t *testing.T) {
	if defaultSummaryTimeout > 25*time.Second {
		t.Fatalf("summary timeout too generous for main-loop critical path: %v", defaultSummaryTimeout)
	}
}
