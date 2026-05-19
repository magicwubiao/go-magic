package session

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

func TestNewStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestSaveAndLoadSession(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	sess := &Session{
		ID:        "test-session-1",
		Profile:   "default",
		Platform:  "web",
		Model:     "gpt-4",
		InputTokens:  100,
		OutputTokens: 200,
	}

	err = store.SaveSession(context.Background(), sess)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), "test-session-1")
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loaded.ID != "test-session-1" {
		t.Errorf("expected ID 'test-session-1', got '%s'", loaded.ID)
	}
	if loaded.Profile != "default" {
		t.Errorf("expected profile 'default', got '%s'", loaded.Profile)
	}
	if loaded.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", loaded.Model)
	}
	if loaded.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", loaded.InputTokens)
	}
	if loaded.OutputTokens != 200 {
		t.Errorf("expected output tokens 200, got %d", loaded.OutputTokens)
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create multiple sessions
	for i := 0; i < 3; i++ {
		sess := &Session{
			ID:       fmt.Sprintf("session-%d", i),
			Profile:  "default",
			Platform: "web",
			Model:    "gpt-4",
		}
		if err := store.SaveSession(context.Background(), sess); err != nil {
			t.Fatalf("failed to save session %d: %v", i, err)
		}
	}

	sessions, err := store.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}

	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
}

func TestListSessionsByProfile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create sessions with different profiles
	sess1 := &Session{ID: "s1", Profile: "work", Platform: "web"}
	sess2 := &Session{ID: "s2", Profile: "personal", Platform: "cli"}
	store.SaveSession(context.Background(), sess1)
	store.SaveSession(context.Background(), sess2)

	sessions, err := store.ListSessions(context.Background(), "work")
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 session for profile 'work', got %d", len(sessions))
	}
}

func TestDeleteSession(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	sess := &Session{ID: "to-delete", Profile: "default", Platform: "web"}
	store.SaveSession(context.Background(), sess)

	err = store.DeleteSession(context.Background(), "to-delete")
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	_, err = store.LoadSession(context.Background(), "to-delete")
	if err == nil {
		t.Error("expected error loading deleted session")
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	err = store.DeleteSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error deleting nonexistent session")
	}
}

func TestSaveSessionWithMessages(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	sess := &Session{
		ID:       "msg-session",
		Profile:  "default",
		Platform: "web",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	err = store.SaveSession(context.Background(), sess)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), "msg-session")
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if len(loaded.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", loaded.Messages[0].Content)
	}
}

