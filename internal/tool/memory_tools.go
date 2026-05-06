package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type MemoryStoreTool struct{}

func (t *MemoryStoreTool) Name() string {
	return "memory_store"
}

func (t *MemoryStoreTool) Description() string {
	return "Store a memory for later recall"
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
	key, ok := args["key"].(string)
	if !ok {
		return nil, fmt.Errorf("key argument is required")
	}
	value, ok := args["value"].(string)
	if !ok {
		return nil, fmt.Errorf("value argument is required")
	}
	category := "general"
	if c, ok := args["category"].(string); ok {
		category = c
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	memDir := filepath.Join(home, ".magic", "memories", category)
	os.MkdirAll(memDir, 0755)

	memPath := filepath.Join(memDir, key+".txt")
	err = os.WriteFile(memPath, []byte(value), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"key":      key,
		"category": category,
		"path":     memPath,
	}, nil
}

type MemoryRecallTool struct{}

func (t *MemoryRecallTool) Name() string {
	return "memory_recall"
}

func (t *MemoryRecallTool) Description() string {
	return "Recall a stored memory by key"
}

func (t *MemoryRecallTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Memory key to recall",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Category to search in",
			},
		},
		"required": []string{"key"},
	}
}

func (t *MemoryRecallTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	key, ok := args["key"].(string)
	if !ok {
		return nil, fmt.Errorf("key argument is required")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Search in all categories or specific one
	categories := []string{"general", "user", "project"}
	if cat, ok := args["category"].(string); ok {
		categories = []string{cat}
	}

	for _, cat := range categories {
		memPath := filepath.Join(home, ".magic", "memories", cat, key+".txt")
		data, err := os.ReadFile(memPath)
		if err == nil {
			return map[string]interface{}{
				"found":    true,
				"key":      key,
				"category": cat,
				"value":    string(data),
			}, nil
		}
	}

	return map[string]interface{}{
		"found": false,
		"key":   key,
	}, nil
}
