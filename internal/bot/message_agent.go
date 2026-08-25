package bot

import (
	"context"
	"fmt"
	"strings"
)

// messageAgentTool implements the bot-to-bot messaging tool, mirroring
// Hermes' message_agent: a Bot composes its own message and delivers it
// into a teammate's canonical chat queue (fire-and-forget).
type messageAgentTool struct {
	manager *Manager
	sender  string // Mention tag of the sending bot

	// teammates is a snapshot taken at construction time. The roster only
	// changes on gateway restart, and the tool must never take m.mu at
	// runtime because Description()/Schema() are called while the manager
	// lock is held during agent construction.
	teammates string
}

func newMessageAgentTool(m *Manager, senderTag string) *messageAgentTool {
	return &messageAgentTool{
		manager:   m,
		sender:    senderTag,
		teammates: m.tagListLocked(), // Caller holds m.mu; safe snapshot
	}
}

func (t *messageAgentTool) Name() string { return "message_agent" }

func (t *messageAgentTool) Description() string {
	return fmt.Sprintf(
		"Send a direct message to another bot in the fleet. "+
			"Available teammates: %s. "+
			"The message is delivered to that bot's own chat and it will reply later; "+
			"delivery is fire-and-forget. Compose your own words - do not forward user text verbatim. "+
			"Use @user if you need to escalate something to the human instead.",
		t.teammates,
	)
}

func (t *messageAgentTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Teammate mention tag or name (e.g. \"researcher\")",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The message body composed by you",
			},
		},
		"required": []string{"target", "message"},
	}
}

func (t *messageAgentTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	target, _ := params["target"].(string)
	message, _ := params["message"].(string)
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("target is required")
	}
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("message is required")
	}

	target = strings.TrimPrefix(strings.TrimSpace(target), "@")
	if err := t.manager.SendMessageAgent(t.sender, target, message); err != nil {
		return nil, err
	}
	return fmt.Sprintf("Message delivered to @%s. The reply will arrive as a background notification.", target), nil
}
