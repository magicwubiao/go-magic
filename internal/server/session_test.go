package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebSessionManagerPersistReload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".web_sessions.json")

	m1 := newWebSessionManagerFile(file)
	if m1.file != file {
		t.Fatalf("expected manager to use file %q, got %q", file, m1.file)
	}

	tok := m1.create(true)
	if tok == "" {
		t.Fatal("create returned empty token")
	}
	if !m1.validate(tok) {
		t.Fatalf("expected freshly created session %q to validate", tok)
	}

	// Simulate a server restart: build a brand-new manager from the same file.
	m2 := newWebSessionManagerFile(file)
	if !m2.validate(tok) {
		t.Fatal("expected session to survive restart (persisted to disk)")
	}
}

func TestWebSessionManagerPersistRevoke(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".web_sessions.json")

	m1 := newWebSessionManagerFile(file)
	tok := m1.create(false)

	// Logout should remove the session from disk as well.
	m1.revoke(tok)
	if m1.validate(tok) {
		t.Fatal("expected revoked token to be invalid")
	}

	m2 := newWebSessionManagerFile(file)
	if m2.validate(tok) {
		t.Fatal("expected revoked token to remain invalid after reload")
	}

	// revokeAll clears everything.
	tok2 := m1.create(false)
	m1.revokeAll()
	if m1.validate(tok2) {
		t.Fatal("expected token to be invalid after revokeAll")
	}
	m3 := newWebSessionManagerFile(file)
	if m3.validate(tok2) {
		t.Fatal("expected token to remain invalid after revokeAll + restart")
	}
}

func TestWebSessionManagerSkipsExpiredOnLoad(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".web_sessions.json")

	m1 := newWebSessionManagerFile(file)
	tok := m1.create(false)

	// Forcibly expire the session in memory and persist.
	m1.mu.Lock()
	m1.sessions[tok].ExpiresAt = time.Now().Add(-time.Hour)
	m1.mu.Unlock()
	m1.persist()

	if m1.validate(tok) {
		t.Fatal("expected expired token to be invalid")
	}

	// On reload, the expired entry should be dropped entirely.
	m2 := newWebSessionManagerFile(file)
	if m2.validate(tok) {
		t.Fatal("expected expired token to be dropped after reload")
	}
}

func TestWebSessionManagerInMemoryNoFile(t *testing.T) {
	// The no-arg constructor must keep working with zero persistence.
	m := newWebSessionManager()
	if m.file != "" {
		t.Fatalf("expected empty file path, got %q", m.file)
	}
	tok := m.create(false)
	if !m.validate(tok) {
		t.Fatal("expected in-memory token to validate")
	}
	m.revoke(tok)
	if m.validate(tok) {
		t.Fatal("expected revoked in-memory token to be invalid")
	}
}

func TestWebSessionManagerFileUnreadable(t *testing.T) {
	// If the file cannot be read, the manager should start empty rather than panic.
	dir := t.TempDir()
	file := filepath.Join(dir, "no_such_dir", ".web_sessions.json")
	m := newWebSessionManagerFile(file)
	if m.validate("anything") {
		t.Fatal("expected no sessions when file is unreadable")
	}
	// Creating a session should still work and persist (creating parent dirs).
	tok := m.create(true)
	if !m.validate(tok) {
		t.Fatal("expected token to validate even when initial file missing")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("expected persisted file to exist after create, got err: %v", err)
	}
}
