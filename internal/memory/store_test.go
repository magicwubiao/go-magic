package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "magic-test", "memories")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(filepath.Dir(tmpDir))

	config := &MemoryConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	}

	store, err := NewStore(config)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Test Store
	mem := &Memory{
		Type:       TypeAgent,
		Content:    "Test memory content",
		Scope:      "/test/project",
		Categories: []string{"test", "example"},
		Importance: 0.8,
	}

	if err := store.Store(mem); err != nil {
		t.Fatalf("Failed to store memory: %v", err)
	}

	if mem.ID == "" {
		t.Error("Memory ID should be set")
	}

	// Test Recall
	memories, err := store.Recall("Test", 10)
	if err != nil {
		t.Fatalf("Failed to recall: %v", err)
	}

	if len(memories) != 1 {
		t.Errorf("Expected 1 memory, got %d", len(memories))
	}

	// Test List
	all, err := store.List("", 10, 0)
	if err != nil {
		t.Fatalf("Failed to list: %v", err)
	}

	if len(all) != 1 {
		t.Errorf("Expected 1 memory in list, got %d", len(all))
	}

	// Test Delete
	if err := store.Delete(mem.ID); err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	all, _ = store.List("", 10, 0)
	if len(all) != 0 {
		t.Errorf("Expected 0 memories after delete, got %d", len(all))
	}
}

func TestMemoryTypes(t *testing.T) {
	types := []MemoryType{TypeAgent, TypeUser, TypeSession, TypeProject, TypeKnowledge, TypePreference}

	for _, typ := range types {
		mem := &Memory{
			Type:    typ,
			Content: string("Test " + typ),
		}

		if mem.Type != typ {
			t.Errorf("MemoryType mismatch: expected %s, got %s", typ, mem.Type)
		}
	}
}

func TestMemoryImportance(t *testing.T) {
	mem := &Memory{
		Type:       TypeAgent,
		Content:    "Test content",
		Importance: 0.75,
	}

	if mem.Importance < 0.0 || mem.Importance > 1.0 {
		t.Errorf("Importance should be between 0.0 and 1.0, got %f", mem.Importance)
	}
}

func TestMemoryCategories(t *testing.T) {
	mem := &Memory{
		Type:       TypeProject,
		Content:    "Project memory",
		Categories: []string{"golang", "ai", "cli"},
	}

	if len(mem.Categories) != 3 {
		t.Errorf("Expected 3 categories, got %d", len(mem.Categories))
	}
}

func TestMemoryScope(t *testing.T) {
	scopes := []string{
		"/infrastructure/database",
		"/project/api",
		"/preferences/ui",
		"",
	}

	for _, scope := range scopes {
		mem := &Memory{
			Type:    TypeAgent,
			Content: "Test",
			Scope:   scope,
		}

		if mem.Scope != scope {
			t.Errorf("Scope mismatch: expected %s, got %s", scope, mem.Scope)
		}
	}
}

func TestAgentUserMemory(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "magic-test", "memories2")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(filepath.Dir(tmpDir))

	config := &MemoryConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	}

	store, err := NewStore(config)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Test AppendAgentMemory
	err = store.AppendAgentMemory("New information added.")
	if err != nil {
		t.Fatalf("Failed to append agent memory: %v", err)
	}

	content, err := store.ReadAgentMemory()
	if err != nil {
		t.Fatalf("Failed to read agent memory: %v", err)
	}

	if !strings.Contains(content, "New information added") {
		t.Error("Appended content not found")
	}

	// Test AppendUserMemory
	err = store.AppendUserMemory("User preference: prefers dark mode.")
	if err != nil {
		t.Fatalf("Failed to append user memory: %v", err)
	}

	userContent, err := store.ReadUserMemory()
	if err != nil {
		t.Fatalf("Failed to read user memory: %v", err)
	}

	if !strings.Contains(userContent, "dark mode") {
		t.Error("User memory content not found")
	}
}

func TestMemoryStats(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "magic-test", "memories4")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(filepath.Dir(tmpDir))

	config := &MemoryConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	}

	store, err := NewStore(config)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Store some memories
	store.Store(&Memory{Type: TypeAgent, Content: "Test 1"})
	store.Store(&Memory{Type: TypeAgent, Content: "Test 2"})
	store.Store(&Memory{Type: TypeUser, Content: "Test 3"})

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.TotalMemories != 3 {
		t.Errorf("Expected 3 memories, got %d", stats.TotalMemories)
	}

	if stats.ByType[TypeAgent] != 2 {
		t.Errorf("Expected 2 agent memories, got %d", stats.ByType[TypeAgent])
	}

	if stats.ByType[TypeUser] != 1 {
		t.Errorf("Expected 1 user memory, got %d", stats.ByType[TypeUser])
	}
}

func TestCommandHistory(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "magic-test", "memories5")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(filepath.Dir(tmpDir))

	config := &MemoryConfig{
		DBPath: filepath.Join(tmpDir, "test.db"),
	}

	store, err := NewStore(config)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Record command actions
	store.RecordCommandAction("rm -rf /tmp/test", "approved", "session-1")
	store.RecordCommandAction("rm -rf /tmp/test", "approved", "session-2")
	store.RecordCommandAction("ls -la", "approved", "session-1")

	// Check trust level
	action, count, err := store.GetCommandTrustLevel(hashCommand("rm -rf /tmp/test"))
	if err != nil {
		t.Fatalf("GetCommandTrustLevel failed: %v", err)
	}

	if count < 1 {
		t.Errorf("Expected count >= 1, got %d", count)
	}

	if action != "approved" {
		t.Errorf("Expected approved, got %s", action)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}

	if len(id1) < 10 {
		t.Error("ID should have reasonable length")
	}
}
