package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestFTS(t *testing.T) *FTSStore {
	t.Helper()
	dir := t.TempDir()
	f, err := NewFTSStore(dir)
	if err != nil {
		t.Fatalf("NewFTSStore: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestFTSStoreAddSearch(t *testing.T) {
	// Regression: the previous FTS triggers were wrong for external-content
	// tables, so the index never received the inserted content and Search
	// returned nothing. This proves inserts are now indexed.
	f := newTestFTS(t)
	if err := f.Add(&MemoryRecord{Role: "user", Content: "deploy the api server to production", Importance: 5}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := f.Add(&MemoryRecord{Role: "assistant", Content: "the weather is sunny today", Importance: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := f.Search("deploy production", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "deploy") {
		t.Fatalf("expected to find the deploy record, got %+v", got)
	}
}

func TestFTSStoreSearchSpecialCharsNoPanic(t *testing.T) {
	f := newTestFTS(t)
	f.Add(&MemoryRecord{Role: "user", Content: "error (critical) status: 500"})
	for _, q := range []string{"error (critical)", "status: 500", "error*", "a OR b", "", "!!!"} {
		if _, err := f.Search(q, 5); err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
	}
}

func TestFTSStoreDeleteRemovesFromFTS(t *testing.T) {
	// Regression: the broken DELETE trigger left stale entries in the FTS
	// index, so deleted memories kept matching. Now cleanup must evict them.
	f := newTestFTS(t)
	f.Add(&MemoryRecord{Role: "user", Content: "temporary note about omega", Importance: 1})

	if got, _ := f.Search("omega", 5); len(got) != 1 {
		t.Fatalf("expected 1 match before cleanup, got %d", len(got))
	}

	// importance 1 < 100 and created_at < now -> everything deleted.
	if n, err := f.CleanupOld(0, 100); err != nil || n != 1 {
		t.Fatalf("CleanupOld expected to delete 1, got %d (err=%v)", n, err)
	}
	if got, _ := f.Search("omega", 5); len(got) != 0 {
		t.Fatalf("expected 0 matches after cleanup, got %d", len(got))
	}
}

func TestFTSStoreGetContextBudget(t *testing.T) {
	// Regression: GetContext compared byte length against a token budget,
	// so it always vastly undersized the cutoff. Now it approximates 4
	// chars/token and stops before exceeding the budget.
	f := newTestFTS(t)
	long := strings.Repeat("alphaword ", 200) // ~2000 chars
	f.Add(&MemoryRecord{Role: "user", Content: long})

	out := f.GetContext("alphaword", 10) // ~40 char budget
	if out == "" {
		t.Fatal("expected non-empty context")
	}
	// Must respect the budget (maxTokens*4 = 40 chars) plus the header.
	if len(out) > 200 {
		t.Fatalf("context not truncated to budget, len=%d: %q", len(out), out)
	}
}

func TestFTSStoreGetContextEmpty(t *testing.T) {
	f := newTestFTS(t)
	if out := f.GetContext("nothing", 100); out != "" {
		t.Fatalf("expected empty context for no results, got %q", out)
	}
}

func TestFTSStoreGetStats(t *testing.T) {
	f := newTestFTS(t)
	f.Add(&MemoryRecord{Role: "user", Content: "one"})
	f.Add(&MemoryRecord{Role: "assistant", Content: "two"})
	stats, err := f.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if total, _ := stats["total"].(int64); total != 2 {
		t.Errorf("expected 2 total, got %v", stats["total"])
	}
	_ = time.Now
}
