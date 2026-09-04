package tool

import (
	"context"
	"fmt"
	"sync"

	"github.com/magicwubiao/go-magic/internal/memory"
)

// MemoryStoreTool stores a value under a key in the persistent semantic memory
// (the same SQLite store used by the Cortex memory systems). This replaces the
// old per-key .txt files, which formed a third, uncoordinated memory system.
type MemoryStoreTool struct{}

func (t *MemoryStoreTool) Name() string {
	return "memory_store"
}

func (t *MemoryStoreTool) Description() string {
	return "Store a memory for later recall. Only use when the user explicitly asks to remember or save something."
}

func (t *MemoryStoreTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Memory key / identifier",
			},
			"value": map[string]interface{}{
				"type":        "string",
				"description": "Memory content to store",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Category (user, project, general)",
				"default":     "general",
			},
		},
		"required": []string{"key", "value"},
	}
}

func (t *MemoryStoreTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	key, _ := args["key"].(string)
	value, _ := args["value"].(string)
	if key == "" || value == "" {
		return nil, fmt.Errorf("key and value are required")
	}
	category := "general"
	if c, _ := args["category"].(string); c != "" {
		category = c
	}

	store, err := getDefaultMemoryStore()
	if err != nil {
		return nil, fmt.Errorf("memory store unavailable: %w", err)
	}

	mem := &memory.Memory{
		Type:       memory.TypeAgent,
		Content:    value,
		Scope:      key,
		Categories: []string{category},
		Importance: 0.5,
		Source:     "memory_store_tool",
	}
	if err := store.Store(mem); err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"key":      key,
		"category": category,
		"id":       mem.ID,
	}, nil
}

// MemoryRecallTool recalls a value previously stored under a key.
type MemoryRecallTool struct{}

func (t *MemoryRecallTool) Name() string {
	return "memory_recall"
}

func (t *MemoryRecallTool) Description() string {
	return "Recall a stored memory by key. Only use when the user explicitly asks to recall or remember something. Do NOT call this automatically."
}

func (t *MemoryRecallTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Memory key to recall",
			},
		},
		"required": []string{"key"},
	}
}

func (t *MemoryRecallTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	key, _ := args["key"].(string)
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}

	store, err := getDefaultMemoryStore()
	if err != nil {
		return nil, fmt.Errorf("memory store unavailable: %w", err)
	}

	mem, err := store.GetByScope(key)
	if err != nil {
		// Not found is a normal, non-error result for recall.
		return map[string]interface{}{
			"found": false,
			"key":   key,
		}, nil
	}

	return map[string]interface{}{
		"found": true,
		"key":   key,
		"value": mem.Content,
		"id":    mem.ID,
	}, nil
}

// --- shared store accessor --------------------------------------------------

// defaultMemoryStore is the process-wide semantic memory store used by the
// memory tools. It is either injected by the Cortex manager (via
// SetMemoryStore) or lazily opened at the default path on first use.
var (
	defaultMemoryStore     *memory.Store
	defaultMemoryStoreMu   sync.Mutex
	defaultMemoryStoreOnce sync.Once
	defaultMemoryStoreErr  error
)

// SetMemoryStore injects the semantic memory store used by the memory tools.
// Should be called once during process startup (after the Cortex manager is
// initialized) so the tools share the same store instance as Cortex.
func SetMemoryStore(s *memory.Store) {
	defaultMemoryStoreMu.Lock()
	defer defaultMemoryStoreMu.Unlock()
	defaultMemoryStore = s
}

// getDefaultMemoryStore returns the shared memory store, opening it lazily at
// the default path if nobody injected one.
func getDefaultMemoryStore() (*memory.Store, error) {
	defaultMemoryStoreMu.Lock()
	if s := defaultMemoryStore; s != nil {
		defaultMemoryStoreMu.Unlock()
		return s, nil
	}
	defaultMemoryStoreMu.Unlock()

	defaultMemoryStoreOnce.Do(func() {
		var s *memory.Store
		s, defaultMemoryStoreErr = memory.NewStore(memory.DefaultConfig())
		if defaultMemoryStoreErr == nil {
			defaultMemoryStoreMu.Lock()
			defaultMemoryStore = s
			defaultMemoryStoreMu.Unlock()
		}
	})
	if defaultMemoryStoreErr != nil {
		return nil, defaultMemoryStoreErr
	}
	defaultMemoryStoreMu.Lock()
	defer defaultMemoryStoreMu.Unlock()
	return defaultMemoryStore, nil
}
