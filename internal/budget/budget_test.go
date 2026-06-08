package budget

import (
	"sync"
	"testing"
	"time"
)

func TestBudgetBasicOperations(t *testing.T) {
	b := New(10)

	if b.MaxTotal() != 10 {
		t.Errorf("expected maxTotal=10, got %d", b.MaxTotal())
	}

	if b.Used() != 0 {
		t.Errorf("expected used=0, got %d", b.Used())
	}

	if b.Remaining() != 10 {
		t.Errorf("expected remaining=10, got %d", b.Remaining())
	}

	if b.IsExhausted() {
		t.Error("budget should not be exhausted initially")
	}
}

func TestBudgetConsume(t *testing.T) {
	b := New(3)

	if !b.Consume() {
		t.Error("first consume should succeed")
	}
	if !b.Consume() {
		t.Error("second consume should succeed")
	}
	if !b.Consume() {
		t.Error("third consume should succeed")
	}
	if b.Consume() {
		t.Error("fourth consume should fail (budget exhausted)")
	}

	if b.Used() != 3 {
		t.Errorf("expected used=3, got %d", b.Used())
	}

	if !b.IsExhausted() {
		t.Error("budget should be exhausted")
	}
}

func TestBudgetConsumeN(t *testing.T) {
	b := New(10)

	consumed := b.ConsumeN(5)
	if consumed != 5 {
		t.Errorf("expected consumed=5, got %d", consumed)
	}

	consumed = b.ConsumeN(10)
	if consumed != 5 {
		t.Errorf("expected consumed=5 (only remaining), got %d", consumed)
	}

	if b.Remaining() != 0 {
		t.Errorf("expected remaining=0, got %d", b.Remaining())
	}
}

func TestBudgetRefund(t *testing.T) {
	b := New(5)

	b.ConsumeN(3)
	if b.Used() != 3 {
		t.Errorf("expected used=3, got %d", b.Used())
	}

	b.Refund()
	if b.Used() != 2 {
		t.Errorf("expected used=2 after refund, got %d", b.Used())
	}

	b.RefundN(5) // Try to refund more than used
	if b.Used() != 0 {
		t.Errorf("expected used=0, got %d", b.Used())
	}
}

func TestBudgetUsagePercent(t *testing.T) {
	b := New(100)

	b.ConsumeN(25)
	if b.UsagePercent() != 25.0 {
		t.Errorf("expected 25%%, got %f", b.UsagePercent())
	}

	b.ConsumeN(25)
	if b.UsagePercent() != 50.0 {
		t.Errorf("expected 50%%, got %f", b.UsagePercent())
	}
}

func TestBudgetStats(t *testing.T) {
	b := New(10)
	b.ConsumeN(3)
	b.Refund()

	stats := b.GetStats()
	if stats.MaxTotal != 10 {
		t.Errorf("expected MaxTotal=10, got %d", stats.MaxTotal)
	}
	if stats.Used != 2 {
		t.Errorf("expected Used=2, got %d", stats.Used)
	}
	if stats.Remaining != 8 {
		t.Errorf("expected Remaining=8, got %d", stats.Remaining)
	}
	if stats.Refunded != 1 {
		t.Errorf("expected Refunded=1, got %d", stats.Refunded)
	}
	if stats.IsExhausted {
		t.Error("should not be exhausted")
	}
	if stats.Duration < 0 {
		t.Error("duration should be non-negative")
	}
}

func TestBudgetConcurrency(t *testing.T) {
	b := New(1000)
	var wg sync.WaitGroup

	// 100 goroutines consuming
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				b.Consume()
			}
		}()
	}

	wg.Wait()

	if b.Used() != 1000 {
		t.Errorf("expected used=1000, got %d", b.Used())
	}

	if !b.IsExhausted() {
		t.Error("budget should be exhausted")
	}
}

func TestBudgetManager(t *testing.T) {
	m := NewManager()

	b1 := m.Create("task1", 50)
	if b1 == nil {
		t.Fatal("expected budget to be created")
	}

	b2, ok := m.Get("task1")
	if !ok {
		t.Error("expected to find budget")
	}
	if b2.MaxTotal() != 50 {
		t.Errorf("expected maxTotal=50, got %d", b2.MaxTotal())
	}

	m.Create("task2", 30)
	ids := m.List()
	if len(ids) != 2 {
		t.Errorf("expected 2 budgets, got %d", len(ids))
	}

	total, used, remaining := m.GetGlobalStats()
	if total != 80 {
		t.Errorf("expected total=80, got %d", total)
	}
	_ = used
	_ = remaining

	m.Delete("task1")
	_, ok = m.Get("task1")
	if ok {
		t.Error("expected budget to be deleted")
	}
}

func TestBudgetPreset(t *testing.T) {
	tests := []struct {
		preset   string
		expected int
	}{
		{"parent", DefaultMaxIterations},
		{"subagent", DefaultSubagentMaxIterations},
		{"simple", DefaultSimpleTaskMaxIterations},
		{"complex", DefaultComplexTaskMaxIterations},
		{"unknown", DefaultMaxIterations},
	}

	for _, tt := range tests {
		b := Preset(tt.preset)
		if b.MaxTotal() != tt.expected {
			t.Errorf("preset %s: expected %d, got %d", tt.preset, tt.expected, b.MaxTotal())
		}
	}
}

func TestBudgetZeroMaxTotal(t *testing.T) {
	b := New(0)
	if !b.IsExhausted() {
		t.Error("budget with 0 max should be exhausted immediately")
	}
	if b.UsagePercent() != 0 {
		t.Error("usage percent should be 0 for zero budget")
	}
}

func TestBudgetTiming(t *testing.T) {
	b := New(10)
	before := time.Now()
	b.Consume()
	time.Sleep(10 * time.Millisecond)
	b.Consume()

	stats := b.GetStats()
	now := time.Now()
	if stats.CreatedAt.Before(before.Add(-time.Second)) || stats.CreatedAt.After(now.Add(time.Second)) {
		t.Errorf("createdAt should be valid: before=%v, createdAt=%v, now=%v", before, stats.CreatedAt, now)
	}
	if stats.LastUsedAt.Before(stats.CreatedAt) {
		t.Error("lastUsedAt should be after createdAt")
	}
}
