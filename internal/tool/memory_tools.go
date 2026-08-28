package tool

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// 共享的结构化记忆 Store（进程级懒加载单例）。
// memory_store / memory_recall 工具在旧版 .txt 文件的基础上双写/读取该 Store，
// 统一两套记忆系统：SQLite 结构化存储（FTS5 全文检索）+ 兼容旧版 .txt 文件。
var (
	sharedMemoryStore     *memory.Store
	sharedMemoryStoreOnce sync.Once
)

// GetSharedMemoryStore 返回进程级共享的结构化记忆 Store。
// 数据库位于 <magicHome>/memories/memory.db，与 CLI memory 命令共用同一份数据。
// 初始化失败时返回 nil，工具自动降级为纯文件模式。
func GetSharedMemoryStore() *memory.Store {
	sharedMemoryStoreOnce.Do(func() {
		s, err := memory.NewStore(memory.DefaultConfig())
		if err != nil {
			log.Printf("[memory] structured store init failed, falling back to file-only mode: %v", err)
			return
		}
		sharedMemoryStore = s
	})
	return sharedMemoryStore
}

// mapCategoryToType 将工具层的 category 映射为结构化 MemoryType
func mapCategoryToType(category string) memory.MemoryType {
	switch category {
	case "user":
		return memory.TypeUser
	case "project":
		return memory.TypeProject
	case "preference":
		return memory.TypePreference
	default:
		return memory.TypeKnowledge
	}
}

// truncateRunes 按 rune 截断字符串（CJK 安全）
func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

type MemoryStoreTool struct{}

func (t *MemoryStoreTool) Name() string {
	return "memory_store"
}

func (t *MemoryStoreTool) Description() string {
	return "Store a memory for later recall. Writes to the structured memory store (SQLite + FTS) and keeps a legacy .txt file for compatibility. Only use when the user explicitly asks to remember or save something."
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
				"description": "Category (user, project, preference, general)",
				"default":     "general",
			},
			"importance": map[string]interface{}{
				"type":        "number",
				"description": "Importance level 0.0-1.0 (default 0.7)",
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
	if c, ok := args["category"].(string); ok && c != "" {
		category = c
	}
	importance := 0.7
	if f, ok := args["importance"].(float64); ok && f >= 0 && f <= 1 {
		importance = f
	}

	// 1) 结构化存储（主路径）
	memID := ""
	storage := "file"
	if store := GetSharedMemoryStore(); store != nil {
		mem := &memory.Memory{
			Type:       mapCategoryToType(category),
			Content:    value,
			Scope:      key,
			Categories: []string{category},
			Importance: importance,
			Source:     "memory_store",
		}
		if err := store.Store(mem); err != nil {
			log.Printf("[memory] structured store write failed: %v", err)
		} else {
			memID = mem.ID
			storage = "structured"
		}
	}

	// 2) 兼容写入 .txt 文件（尽力而为，保证旧数据/外部读取不中断）
	memDir := filepath.Join(config.GetMagicHome(), "memories", category)
	os.MkdirAll(memDir, 0755)
	memPath := filepath.Join(memDir, key+".txt")
	if err := os.WriteFile(memPath, []byte(value), 0644); err != nil {
		if storage == "file" {
			// 结构化与文件双失败，才算真正失败
			return nil, fmt.Errorf("failed to store memory: %w", err)
		}
		log.Printf("[memory] legacy file write failed (structured copy kept): %v", err)
	} else if storage == "structured" {
		storage = "structured+file"
	}

	return map[string]interface{}{
		"success":  true,
		"key":      key,
		"category": category,
		"path":     memPath,
		"storage":  storage,
		"id":       memID,
	}, nil
}

type MemoryRecallTool struct{}

func (t *MemoryRecallTool) Name() string {
	return "memory_recall"
}

func (t *MemoryRecallTool) Description() string {
	return "Recall a stored memory by key. Searches the structured memory store first (exact key match, then FTS fuzzy search), and falls back to legacy .txt files. Only use when the user explicitly asks to recall or remember something. Do NOT call this automatically."
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
				"description": "Category to search in (user, project, preference, general)",
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
	category := ""
	if c, ok := args["category"].(string); ok {
		category = c
	}

	// 1) 结构化存储优先：精确 Scope 匹配
	store := GetSharedMemoryStore()
	if store != nil {
		memories, err := store.Recall(key, 10)
		if err == nil {
			for _, mem := range memories {
				if mem.Scope != key {
					continue
				}
				if category != "" && !containsString(mem.Categories, category) {
					continue
				}
				memCategory := category
				if len(mem.Categories) > 0 {
					memCategory = mem.Categories[0]
				}
				return map[string]interface{}{
					"found":      true,
					"key":        key,
					"category":   memCategory,
					"value":      mem.Content,
					"source":     "structured",
					"importance": mem.Importance,
				}, nil
			}

			// 精确未命中：FTS 模糊搜索提供相关记忆
			related := collectRelated(store, key, 5)
			if len(related) > 0 {
				return map[string]interface{}{
					"found":   false,
					"key":     key,
					"related": related,
				}, nil
			}
		} else {
			log.Printf("[memory] structured recall failed: %v", err)
		}
	}

	// 2) 兼容读取 .txt 文件
	categories := []string{"general", "user", "project", "preference"}
	if category != "" {
		categories = []string{category}
	}
	memHome := config.GetMagicHome()
	for _, cat := range categories {
		memPath := filepath.Join(memHome, "memories", cat, key+".txt")
		data, err := os.ReadFile(memPath)
		if err == nil {
			return map[string]interface{}{
				"found":    true,
				"key":      key,
				"category": cat,
				"value":    string(data),
				"source":   "file",
			}, nil
		}
	}

	return map[string]interface{}{
		"found": false,
		"key":   key,
	}, nil
}

// collectRelated 通过 FTS 搜索返回与 key 相关、但非精确匹配的记忆摘要
func collectRelated(store *memory.Store, key string, limit int) []map[string]interface{} {
	results, err := store.Search(key, limit+5)
	if err != nil {
		return nil
	}
	related := make([]map[string]interface{}, 0, limit)
	for _, mem := range results {
		if mem.Scope == key {
			continue
		}
		related = append(related, map[string]interface{}{
			"key":     mem.Scope,
			"type":    string(mem.Type),
			"content": truncateRunes(mem.Content, 120),
		})
		if len(related) >= limit {
			break
		}
	}
	return related
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}
