package bot

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// botMemoryTool implements the per-bot long-term memory tool. A bot can call
// remember_memory to persist facts it learns into its own Memory block, which
// is injected into its system prompt on subsequent turns — the equivalent of
// Hermes' bots writing to their MEMORY.md at runtime. Unlike the global
// memory_store tool, this memory is scoped to the bot and travels with it.
type botMemoryTool struct {
	manager *Manager
	botName string // Mention tag / name of the owning bot
}

func newBotMemoryTool(m *Manager, botName string) *botMemoryTool {
	return &botMemoryTool{manager: m, botName: botName}
}

func (t *botMemoryTool) Name() string { return "remember_memory" }

func (t *botMemoryTool) Description() string {
	return "Persist a fact about yourself, the task, or your preferences into your own long-term memory. " +
		"This memory is injected into your system prompt on every future turn, so you will remember it " +
		"across sessions. Use it to retain things you learn while working (user preferences, project " +
		"facts, decisions). Compose the fact as a concise, standalone sentence."
}

func (t *botMemoryTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"memory": map[string]interface{}{
				"type":        "string",
				"description": "The fact to remember, written as a concise standalone note (e.g. \"The user prefers concise English replies.\")",
			},
		},
		"required": []string{"memory"},
	}
}

func (t *botMemoryTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	memory, _ := args["memory"].(string)
	if strings.TrimSpace(memory) == "" {
		return nil, fmt.Errorf("memory argument is required")
	}
	if err := t.manager.RememberMemory(t.botName, strings.TrimSpace(memory)); err != nil {
		return nil, err
	}
	return "Memory saved. You will remember this in future conversations.", nil
}

// RememberMemory appends a fact to a bot's long-term Memory and hot-reloads
// its agent so the new memory is reflected in the system prompt immediately.
func (m *Manager) RememberMemory(botName, fact string) error {
	cfg, err := m.store.Load(botName)
	if err != nil {
		return fmt.Errorf("bot not found: %s", botName)
	}

	// Append to the existing memory block with a timestamped entry.
	if cfg.Memory != "" {
		cfg.Memory += "\n\n- [" + time.Now().Format("2006-01-02 15:04") + "] " + fact
	} else {
		cfg.Memory = "- [" + time.Now().Format("2006-01-02 15:04") + "] " + fact
	}
	if err := m.store.Save(cfg); err != nil {
		return fmt.Errorf("failed to save memory: %w", err)
	}

	// Hot-reload the agent so the updated Memory is injected next turn.
	// Room agents too, so the new memory is visible everywhere the bot acts.
	key := strings.ToLower(botName)
	m.mu.Lock()
	if rt, ok := m.bots[key]; ok {
		rt.cfg = cfg
		rt.ag = nil
		rt.loaded = false
		rt.roomAgents = nil
		rt.roomLoaded = nil
	}
	m.mu.Unlock()

	return nil
}