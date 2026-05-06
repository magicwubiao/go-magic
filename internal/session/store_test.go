package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestNewStore tests creating a new session store
func TestNewStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	defer store.Close()
}

// TestSessionSaveLoad tests saving and loading a session
func TestSessionSaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	session := &Session{
		ID:       "test_session_1",
		Profile:  "default",
		Platform: "telegram",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	err = store.SaveSession(context.Background(), session)
	if err != nil {
		t.Errorf("failed to save session: %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), "test_session_1")
	if err != nil {
		t.Errorf("failed to load session: %v", err)
	}

	if loaded.ID != session.ID {
		t.Errorf("expected ID '%s', got '%s'", session.ID, loaded.ID)
	}
	if loaded.Profile != session.Profile {
		t.Errorf("expected Profile '%s', got '%s'", session.Profile, loaded.Profile)
	}
	if len(loaded.Messages) != len(session.Messages) {
		t.Errorf("expected %d messages, got %d", len(session.Messages), len(loaded.Messages))
	}
}

// TestSessionList tests listing sessions
func TestSessionList(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create multiple sessions
	for i := 0; i < 5; i++ {
		session := &Session{
			ID:       "session_" + string(rune('a'+i)),
			Profile:  "default",
			Platform: "telegram",
		}
		err = store.SaveSession(context.Background(), session)
		if err != nil {
			t.Fatalf("failed to save session: %v", err)
		}
	}

	sessions, err := store.ListSessions(context.Background(), "default")
	if err != nil {
		t.Errorf("failed to list sessions: %v", err)
	}

	if len(sessions) != 5 {
		t.Errorf("expected 5 sessions, got %d", len(sessions))
	}
}

// TestSessionDelete tests deleting a session
func TestSessionDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	session := &Session{
		ID:       "session_to_delete",
		Profile:  "default",
		Platform: "telegram",
	}

	err = store.SaveSession(context.Background(), session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	err = store.DeleteSession(context.Background(), "session_to_delete")
	if err != nil {
		t.Errorf("failed to delete session: %v", err)
	}

	// Try to load deleted session
	_, err = store.LoadSession(context.Background(), "session_to_delete")
	if err == nil {
		t.Error("expected error when loading deleted session")
	}
}

// TestSessionNotFound tests loading a non-existent session
func TestSessionNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	_, err = store.LoadSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error when loading non-existent session")
	}
}

// TestSessionUpdate tests updating a session
func TestSessionUpdate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	session := &Session{
		ID:       "session_to_update",
		Profile:  "default",
		Platform: "telegram",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	err = store.SaveSession(context.Background(), session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Update with new messages
	session.Messages = append(session.Messages,
		types.Message{Role: "assistant", Content: "Hi!"},
		types.Message{Role: "user", Content: "How are you?"},
	)

	err = store.SaveSession(context.Background(), session)
	if err != nil {
		t.Fatalf("failed to update session: %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), "session_to_update")
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if len(loaded.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(loaded.Messages))
	}
}

// TestSessionPersistence tests that sessions persist after store close
func TestSessionPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create and save session
	store1, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	session := &Session{
		ID:       "persistent_session",
		Profile:  "default",
		Platform: "telegram",
		Messages: []types.Message{
			{Role: "user", Content: "This should persist"},
		},
	}

	err = store1.SaveSession(context.Background(), session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}
	store1.Close()

	// Create new store and load session
	store2, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create new store: %v", err)
	}
	defer store2.Close()

	loaded, err := store2.LoadSession(context.Background(), "persistent_session")
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if len(loaded.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "This should persist" {
		t.Errorf("unexpected message content: %s", loaded.Messages[0].Content)
	}
}

// TestSessionFields tests Session struct fields
func TestSessionFields(t *testing.T) {
	session := &Session{
		ID:       "test_id",
		Profile:  "test_profile",
		Platform: "discord",
		Messages: []types.Message{},
	}

	if session.ID != "test_id" {
		t.Errorf("expected ID 'test_id', got '%s'", session.ID)
	}
	if session.Profile != "test_profile" {
		t.Errorf("expected Profile 'test_profile', got '%s'", session.Profile)
	}
	if session.Platform != "discord" {
		t.Errorf("expected Platform 'discord', got '%s'", session.Platform)
	}
	if len(session.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(session.Messages))
	}
}

// TestSessionMultipleProfiles tests sessions across multiple profiles
func TestSessionMultipleProfiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create sessions in different profiles
	profiles := []string{"profile_a", "profile_b", "profile_a"}
	for i, profile := range profiles {
		session := &Session{
			ID:       "session_" + string(rune('a'+i)),
			Profile:  profile,
			Platform: "telegram",
		}
		err = store.SaveSession(context.Background(), session)
		if err != nil {
			t.Fatalf("failed to save session: %v", err)
		}
	}

	// List sessions for profile_a
	profileASessions, err := store.ListSessions(context.Background(), "profile_a")
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}
	if len(profileASessions) != 2 {
		t.Errorf("expected 2 sessions in profile_a, got %d", len(profileASessions))
	}

	// List sessions for profile_b
	profileBSessions, err := store.ListSessions(context.Background(), "profile_b")
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}
	if len(profileBSessions) != 1 {
		t.Errorf("expected 1 session in profile_b, got %d", len(profileBSessions))
	}
}
