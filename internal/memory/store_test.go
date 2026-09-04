package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := &MemoryConfig{DBPath: filepath.Join(t.TempDir(), "mem.db")}
	s, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreRecallRoundtrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.Store(&Memory{Type: TypeProject, Content: "the database runs on postgres", Scope: "/infra/db", Importance: 0.8}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := s.Store(&Memory{Type: TypeProject, Content: "frontend uses react", Scope: "/infra/web", Importance: 0.3}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := s.Recall("database postgres", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "postgres") {
		t.Fatalf("expected the postgres memory, got %+v", got)
	}
	// BM25 should rank the matching record first; even if both returned,
	// the postgres one must be first.
	if got[0].Scope != "/infra/db" {
		t.Fatalf("expected /infra/db first, got %s", got[0].Scope)
	}
}

func TestStoreRecallAccessCountBatched(t *testing.T) {
	// Regression: recall used to issue one UPDATE per result (N+1) and trigger
	// the FTS update trigger per row. Now it should be a single UPDATE.
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		if err := s.Store(&Memory{Type: TypeKnowledge, Content: "shared fact about gophers", Scope: "/facts"}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	got, err := s.Recall("gophers", 10)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	for _, m := range got {
		if m.AccessCount != 1 {
			t.Fatalf("expected access_count=1 after one recall, got %d for %s", m.AccessCount, m.ID)
		}
	}
}

func TestStoreRecallSpecialCharsNoPanic(t *testing.T) {
	// Regression: queries with FTS5-reserved characters used to corrupt the
	// MATCH expression and either error or misbehave. Now they must not panic
	// and must return (possibly via fallback).
	s := newTestStore(t)
	if err := s.Store(&Memory{Type: TypeKnowledge, Content: "error (critical) status: 500"}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	for _, q := range []string{
		"error (critical)",
		"status: 500",
		"error*",
		"a OR b",
		"",
		"!!!",
	} {
		if _, err := s.Recall(q, 5); err != nil {
			t.Fatalf("Recall(%q): %v", q, err)
		}
	}
}

func TestStoreRecallEmptyQueryReturnsRecent(t *testing.T) {
	s := newTestStore(t)
	if err := s.Store(&Memory{Type: TypeProject, Content: "low importance", Importance: 0.1}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := s.Store(&Memory{Type: TypeProject, Content: "high importance", Importance: 0.9}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := s.Recall("", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 2 || got[0].Importance < got[1].Importance {
		t.Fatalf("expected high-importance first, got %+v", got)
	}
}

func TestStoreListTypeFilter(t *testing.T) {
	s := newTestStore(t)
	s.Store(&Memory{Type: TypeUser, Content: "user fact"})
	s.Store(&Memory{Type: TypeProject, Content: "project fact"})

	got, err := s.List(TypeUser, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Type != TypeUser {
		t.Fatalf("expected 1 user memory, got %+v", got)
	}
}

func TestStoreGetByScope(t *testing.T) {
	s := newTestStore(t)
	if err := s.Store(&Memory{Type: TypeAgent, Content: "secret value", Scope: "api-key"}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	m, err := s.GetByScope("api-key")
	if err != nil {
		t.Fatalf("GetByScope: %v", err)
	}
	if m.Content != "secret value" {
		t.Fatalf("expected 'secret value', got %q", m.Content)
	}
	if _, err := s.GetByScope("missing"); err == nil {
		t.Fatal("expected error for missing scope")
	}
}

func TestStoreUpdateModifiesFTSIndex(t *testing.T) {
	// Regression: the FTS update trigger must re-index on UPDATE so searching
	// the new content works and searching the old content no longer matches.
	s := newTestStore(t)
	m := &Memory{Type: TypeKnowledge, Content: "original wording alpha"}
	s.Store(m)

	got, _ := s.Recall("alpha", 5)
	if len(got) != 1 {
		t.Fatalf("expected to find 'alpha' before update, got %d", len(got))
	}

	m.Content = "rewritten wording beta"
	if err := s.Update(m); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got, _ := s.Recall("beta", 5); len(got) != 1 {
		t.Fatalf("expected to find 'beta' after update, got %d", len(got))
	}
	if got, _ := s.Recall("alpha", 5); len(got) != 0 {
		t.Fatalf("old term 'alpha' should no longer match after update, got %d", len(got))
	}
}

func TestStoreDeleteRemovesFromFTS(t *testing.T) {
	s := newTestStore(t)
	m := &Memory{Type: TypeKnowledge, Content: "to be deleted gamma"}
	s.Store(m)
	if got, _ := s.Recall("gamma", 5); len(got) != 1 {
		t.Fatalf("expected 1 match before delete, got %d", len(got))
	}
	if err := s.Delete(m.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := s.Recall("gamma", 5); len(got) != 0 {
		t.Fatalf("expected 0 matches after delete, got %d", len(got))
	}
}

func TestHashCommand(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool // whether a and b should hash equally
	}{
		{"numbers normalized", "git commit 123", "git commit 456", true},
		{"ipv4 normalized", "curl http://1.2.3.4/api", "curl http://9.8.7.6/api", true},
		{"email normalized", "send mail to user@example.com now", "send mail to admin@foo.io now", true},
		{"uuid normalized", "deploy 550e8400-e29b-41d4-a716-446655440000 done", "deploy 11111111-2222-3333-4444-555555555555 done", true},
		{"whitespace collapsed", "ls   -la", "ls -la", true},
		{"different commands differ", "git push", "git pull", false},
		{"case insensitive", "Git COMMIT", "git commit", true},
	}
	for _, c := range cases {
		ha, hb := hashCommand(c.a), hashCommand(c.b)
		if (ha == hb) != c.want {
			t.Errorf("%s: hashes equal=%v, want %v (a=%s b=%s)", c.name, ha == hb, c.want, c.a, c.b)
		}
	}
	// Must be a real hash (64 hex chars), not the old len-based stub.
	if h := hashCommand("anything"); len(h) != 64 {
		t.Errorf("expected 64-char sha256 hex, got len %d: %s", len(h), h)
	}
}

func TestGenerateIDUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 5000)
	for i := 0; i < 5000; i++ {
		id := generateID()
		if id == "" {
			t.Fatal("empty id")
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestEscapeFTSQuery(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"a", ""}, // single-char terms dropped
		{"hello", `"hello"*`},
		{"hello world", `"hello" AND "world"*`},
		{"error (critical)", `"error" AND "critical"*`}, // parens stripped, not parsed as operators
		{"status: 500", `"status" AND "500"*`},          // colon stripped
		{"a OR b", `"or" AND "b"*`},                     // OR becomes a quoted term, not the operator
	}
	for _, c := range cases {
		got := escapeFTSQuery(c.in)
		if got != c.want {
			t.Errorf("escapeFTSQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStoreCommandTrust(t *testing.T) {
	s := newTestStore(t)
	cmd := "rm -rf /tmp/123"
	if err := s.RecordCommandAction(cmd, "denied", "sess-1"); err != nil {
		t.Fatalf("RecordCommandAction: %v", err)
	}
	h := HashCommand(cmd)
	action, count, err := s.GetCommandTrustLevel(h)
	if err != nil {
		t.Fatalf("GetCommandTrustLevel: %v", err)
	}
	if action != "denied" || count != 1 {
		t.Fatalf("expected denied/1, got %s/%d", action, count)
	}
	// Record again (different concrete number, same hash) -> count increments.
	if err := s.RecordCommandAction("rm -rf /tmp/999", "denied", "sess-2"); err != nil {
		t.Fatalf("RecordCommandAction 2: %v", err)
	}
	_, count, _ = s.GetCommandTrustLevel(h)
	if count != 2 {
		t.Fatalf("expected count=2 after second record, got %d", count)
	}
}

func TestStoreSummarizeNoProvider(t *testing.T) {
	s := newTestStore(t)
	s.config.LLMProvider = "" // force basic summary, no network
	memories := []*Memory{{Type: TypeKnowledge, Content: "fact one"}, {Type: TypeKnowledge, Content: "fact two"}}
	out, err := s.Summarize(memories)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !strings.Contains(out, "2 relevant") {
		t.Fatalf("expected basic summary to mention 2 memories, got: %s", out)
	}
}

func TestStoreStats(t *testing.T) {
	s := newTestStore(t)
	s.Store(&Memory{Type: TypeUser, Content: "u1", Importance: 0.5})
	s.Store(&Memory{Type: TypeUser, Content: "u2", Importance: 0.7})
	s.Store(&Memory{Type: TypeProject, Content: "p1", Importance: 0.9})

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalMemories != 3 {
		t.Errorf("expected 3 memories, got %d", stats.TotalMemories)
	}
	if stats.ByType[TypeUser] != 2 || stats.ByType[TypeProject] != 1 {
		t.Errorf("unexpected by-type counts: %+v", stats.ByType)
	}
	if stats.AvgImportance <= 0 {
		t.Errorf("expected positive avg importance, got %v", stats.AvgImportance)
	}
	if stats.LastUpdated.IsZero() {
		t.Error("expected LastUpdated to be set")
	}
	_ = time.Now // keep time import if future assertions need it
}
