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

// maxBotMemoryRunes caps a bot's long-term memory block. Memory is injected
// into the system prompt on EVERY turn and every provider request, so an
// unbounded block slowly turns into a standing token tax on the whole bot.
// At the cap the tool refuses further writes and tells the model to ask the
// user to prune, rather than silently growing forever.
const maxBotMemoryRunes = 8000

// RememberMemory appends a fact to a bot's long-term Memory and hot-reloads
// its agent so the new memory is reflected in the system prompt immediately.
func (m *Manager) RememberMemory(botName, fact string) error {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return fmt.Errorf("memory fact is empty")
	}

	// Serialize load -> append -> save. Two turns of the same bot never run
	// concurrently, but a room turn and the canonical turn (or two different
	// room agents of the same bot) can both call remember_memory at once.
	// Without the lock they load the same Memory, append different facts, and
	// whichever saves last drops the other's fact.
	//
	// cfgMu is released before touching m.mu below, so it never nests.
	m.cfgMu.Lock()
	cfg, err := m.store.Load(botName)
	if err != nil {
		m.cfgMu.Unlock()
		return fmt.Errorf("bot not found: %s", botName)
	}
	// Idempotent: models routinely restate a fact they already stored a few
	// turns ago ("the user prefers concise replies"). Appending it again would
	// grow the prompt for zero information.
	if strings.Contains(cfg.Memory, fact) {
		m.cfgMu.Unlock()
		return nil
	}
	entry := "- [" + time.Now().Format("2006-01-02 15:04") + "] " + fact
	if len([]rune(cfg.Memory))+len([]rune(entry)) > maxBotMemoryRunes {
		m.cfgMu.Unlock()
		return fmt.Errorf("memory is full (%d chars max); ask the user to prune the existing memory block first", maxBotMemoryRunes)
	}
	if cfg.Memory != "" {
		cfg.Memory += "\n\n" + entry
	} else {
		cfg.Memory = entry
	}
	if err := m.store.Save(cfg); err != nil {
		m.cfgMu.Unlock()
		return fmt.Errorf("failed to save memory: %w", err)
	}
	m.cfgMu.Unlock()

	// Hot-reload the agent so the updated Memory is injected next turn.
	// Room agents too, so the new memory is visible everywhere the bot acts.
	// Rebuilding is safe: each room agent restores its persisted history from
	// the session store on next use (getOrCreateAgentLocked -> buildAgent), so
	// in-flight room turns (which already hold their agent reference) continue
	// undisturbed and no durable room context is lost.
	key := strings.ToLower(botName)
	m.mu.Lock()
	if rt, ok := m.bots[key]; ok {
		rt.cfg = cfg
		rt.ag = nil
		rt.roomAgents = nil
	}
	m.mu.Unlock()

	return nil
}
